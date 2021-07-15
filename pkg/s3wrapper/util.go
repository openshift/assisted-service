package s3wrapper

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	minimalTemplatesVersionFileName = "minimal_templates_version.json"
	minimalTemplatesVersionLatest   = 4 // increase if templates update is needed
)

type templatesVersion struct {
	Version int
}

func FixEndpointURL(endpoint string) (string, error) {
	_, err := url.ParseRequestURI(endpoint)
	if err == nil {
		return endpoint, nil
	}

	prefix := "http://"
	if os.Getenv("S3_USE_SSL") == "true" {
		prefix = "https://"
	}

	new_url := prefix + endpoint
	_, err = url.ParseRequestURI(new_url)
	if err != nil {
		return "", err
	}
	return new_url, nil
}

func DownloadURLToTemporaryFile(url string) (string, error) {
	tmpfile, err := ioutil.TempFile("", "isodownload")
	if err != nil {
		return "", errors.Wrap(err, "Error creating temporary file")
	}
	defer tmpfile.Close()

	resp, err := http.Get(url)
	if err != nil {
		return "", errors.Wrapf(err, "Failed fetching from URL %s", url)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("Failed fetching from URL %s: Received %s", url, resp.Status)
	}

	_, err = io.Copy(tmpfile, resp.Body)
	if err != nil {
		return "", errors.Wrapf(err, "Failed downloading file from %s to %s", url, tmpfile.Name())
	}

	return tmpfile.Name(), nil
}

func UploadFromURLToPublicBucket(ctx context.Context, objectName, url string, api API) error {
	resp, err := http.Get(url)
	if err != nil {
		return errors.Wrapf(err, "Failed fetching from URL %s", url)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Failed fetching from URL %s: Received %s", url, resp.Status)
	}

	err = api.UploadStreamToPublicBucket(ctx, resp.Body, objectName)
	if err != nil {
		return errors.Wrapf(err, "Failed uploading to %s", objectName)
	}

	return nil
}

func CreateAndUploadMinimalIso(ctx context.Context, log logrus.FieldLogger, isoPath, minimalIsoObject, rootFSURL string,
	api API, editorFactory isoeditor.Factory) error {

	log.Infof("Extracting rhcos ISO (%s)", isoPath)
	var minimalIsoPath string
	err := editorFactory.WithEditor(ctx, isoPath, log, func(editor isoeditor.Editor) error {
		var createError error
		minimalIsoPath, createError = editor.CreateMinimalISOTemplate(rootFSURL)
		return createError
	})
	if err != nil {
		log.Errorf("Error extracting rhcos ISO (%v)", err)
		return err
	}
	defer os.Remove(minimalIsoPath)

	// upload the minimal iso
	log.Infof("Uploading minimal ISO (%s)", minimalIsoPath)
	if err := api.UploadFileToPublicBucket(ctx, minimalIsoPath, minimalIsoObject); err != nil {
		return err
	}

	// Update version file in bucket
	return updateISOTemplatesVersion(ctx, log, api)
}

// HaveLatestMinimalTemplate Returns true if latest version already exists in bucket; otherwise, false.
func HaveLatestMinimalTemplate(ctx context.Context, log logrus.FieldLogger, api API) bool {
	versionFromBucket, err := getISOTemplatesVersion(ctx, log, api)
	if err != nil {
		log.WithError(err).Warning("ISO template version metadata not found, uploading latest")
		// Assume error is due to missing file and upload the latest
		return false
	}

	// We assume that the code contains the latest version
	if versionFromBucket.Version < minimalTemplatesVersionLatest {
		log.Warnf("Templates version is stale: %d", versionFromBucket.Version)
		return false
	}

	// No need to update
	return true
}

func getISOTemplatesVersion(ctx context.Context, log logrus.FieldLogger, api API) (*templatesVersion, error) {
	// Fetch version file from bucket
	reader, _, err := api.Download(ctx, minimalTemplatesVersionFileName)
	if err != nil {
		return nil, err
	}

	// Read and parse version
	bytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var version templatesVersion
	err = json.Unmarshal(bytes, &version)
	if err != nil {
		return nil, err
	}

	return &version, nil
}

func updateISOTemplatesVersion(ctx context.Context, log logrus.FieldLogger, api API) error {
	currentVersion := &templatesVersion{
		Version: minimalTemplatesVersionLatest,
	}
	b, err := json.Marshal(currentVersion)
	if err != nil {
		return err
	}
	reader := bytes.NewReader(b)
	return api.UploadStream(ctx, reader, minimalTemplatesVersionFileName)
}

// continueOnError is set when running as stream, error is doing nothing when it happens cause we in the middle of stream
// and 200 was already returned
func CreateTar(ctx context.Context, w io.Writer, files, tarredFilenames []string, client API, continueOnError bool) error {
	var rdr io.ReadCloser
	tarWriter := tar.NewWriter(w)
	defer func() {
		if rdr != nil {
			rdr.Close()
		}
		tarWriter.Close()

	}()
	var err error
	var objectSize int64

	// Create tar headers from s3 files
	for i, file := range files {
		// Read file from S3, log any errors
		rdr, objectSize, err = client.Download(ctx, file)
		if err != nil {
			if continueOnError {
				continue
			}
			return errors.Wrapf(err, "Failed to open reader for %s", file)
		}

		header := tar.Header{
			Name:    tarredFilenames[i],
			Size:    objectSize,
			Mode:    0644,
			ModTime: time.Now(),
		}
		err = tarWriter.WriteHeader(&header)
		if err != nil && !continueOnError {
			return errors.Wrapf(err, "Failed to write tar header with file %s details", file)
		}
		_, err = io.Copy(tarWriter, rdr)
		if err != nil && !continueOnError {
			return errors.Wrapf(err, "Failed to write file %s to tar", file)
		}
		_ = rdr.Close()
	}

	return nil
}

// Tar given files in s3 bucket.
// We open pipe for reading from aws and writing archived back to it while archiving them.
// It creates stream by using io.pipe
func TarAwsFiles(ctx context.Context, tarName string, files, tarredFilenames []string, client API, log logrus.FieldLogger) error {
	// Create pipe
	var err error
	pr, pw := io.Pipe()
	wg := sync.WaitGroup{}
	// Wait for downloader and uploader
	wg.Add(2)
	// Run 'downloader'
	go func() {
		defer func() {
			wg.Done()
			// closing pipe will stop uploading
			pw.Close()
		}()
		downloadError := CreateTar(ctx, pw, files, tarredFilenames, client, false)
		if downloadError != nil && err == nil {
			err = errors.Wrapf(downloadError, "Failed to download files while creating archive %s", tarName)
			log.Error(err)
		}
	}()
	go func() {
		defer func() {
			wg.Done()
			// if upload fails close pipe
			// will fail download too
			pr.Close()
		}()
		// Upload the file, body is `io.Reader` from pipe
		uploadError := client.UploadStream(ctx, pr, tarName)
		if uploadError != nil && err == nil {
			err = errors.Wrapf(uploadError, "Failed to upload archive %s", tarName)
			log.Error(err)
		}
	}()
	wg.Wait()
	return err
}
