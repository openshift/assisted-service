package s3wrapper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/renameio"
	"github.com/moby/moby/pkg/ioutils"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/versions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// fsBaseISOName is the filename for the base ISO
	fsBaseISOName = "livecd.iso"
	// fsMinimalBaseISOName is the filename for the minimal base ISO
	fsMinimalBaseISOName = "livecd-minimal.iso"
)

type FSClient struct {
	log              logrus.FieldLogger
	basedir          string
	versionsHandler  versions.Handler
	isoEditorFactory isoeditor.Factory
}

func NewFSClient(basedir string, logger logrus.FieldLogger, versionsHandler versions.Handler, isoEditorFactory isoeditor.Factory) *FSClient {
	return &FSClient{log: logger, basedir: basedir, versionsHandler: versionsHandler, isoEditorFactory: isoEditorFactory}
}

func (f *FSClient) IsAwsS3() bool {
	return false
}

func (f *FSClient) CreateBucket() error {
	return nil
}

func (f *FSClient) CreatePublicBucket() error {
	return nil
}

func (f *FSClient) Upload(ctx context.Context, data []byte, objectName string) error {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	if err := os.MkdirAll(path.Dir(filePath), 0755); err != nil {
		err = errors.Wrapf(err, "Unable to create directory for file data %s", filePath)
		log.Error(err)
		return err
	}
	if err := renameio.WriteFile(filePath, data, 0600); err != nil {
		err = errors.Wrapf(err, "Unable to write data to file %s", filePath)
		log.Error(err)
		return err
	}
	log.Infof("Successfully uploaded file %s", objectName)
	return nil
}

func (f *FSClient) UploadFile(ctx context.Context, filePath, objectName string) error {
	log := logutil.FromContext(ctx, f.log)
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to open file %s for upload", filePath)
		log.Error(err)
		return err
	}
	return f.Upload(ctx, data, objectName)
}

func (f *FSClient) UploadFileToPublicBucket(ctx context.Context, filePath, objectName string) error {
	return f.UploadFile(ctx, filePath, objectName)
}

func (f *FSClient) UploadISO(ctx context.Context, ignitionConfig, srcObject, destObjectPrefix string) error {
	log := logutil.FromContext(ctx, f.log)
	resultFile := filepath.Join(f.basedir, fmt.Sprintf("%s.iso", destObjectPrefix))
	baseFile := filepath.Join(f.basedir, srcObject)
	err := os.Remove(resultFile)
	if err != nil && !os.IsNotExist(err) {
		log.Error("error attempting to remove any pre-existing ISO")
		return err
	}

	installerCommand := filepath.Join(f.basedir, "coreos-installer")
	cmd := exec.Command(installerCommand, "iso", "ignition", "embed", "-o", resultFile, baseFile, "-f")
	var out bytes.Buffer
	cmd.Stdin = strings.NewReader(ignitionConfig)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err != nil {
		err = errors.Wrapf(err, "coreos-installer failed: %s", out.String())
		log.Error(err)
		return err
	}
	return nil
}

func (f *FSClient) UploadStream(ctx context.Context, reader io.Reader, objectName string) error {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	if err := os.MkdirAll(path.Dir(filePath), 0755); err != nil {
		err = errors.Wrapf(err, "Unable to create directory for file data %s", filePath)
		log.Error(err)
		return err
	}
	buffer := make([]byte, 1024)

	t, err := renameio.TempFile("", filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to create a temp file for %s", filePath)
		log.Error(err)
		return err
	}

	defer func() {
		if err := t.Cleanup(); err != nil {
			log.Errorf("Unable to clean up temp file %s", t.Name())
		}
	}()

	for {
		length, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			err = errors.Wrapf(err, "Unable to read data for upload to file %s", filePath)
			log.Error(err)
			return err
		}
		if length > 0 {
			if _, writeErr := t.Write(buffer[0:length]); writeErr != nil {
				writeErr = errors.Wrapf(err, "Unable to write data to temp file %s", t.Name())
				log.Error(writeErr)
				return writeErr
			}
		}

		if err == io.EOF {
			break
		}
	}

	if err := t.CloseAtomicallyReplace(); err != nil {
		err = errors.Wrapf(err, "Unable to atomically replace %s with temp file %s", filePath, t.Name())
		log.Error(err)
		return err
	}

	log.Infof("Successfully uploaded file %s", objectName)
	return nil
}

func (f *FSClient) UploadStreamToPublicBucket(ctx context.Context, reader io.Reader, objectName string) error {
	return f.UploadStream(ctx, reader, objectName)
}

func (f *FSClient) Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	fp, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, NotFound(objectName)
		}
		err = errors.Wrapf(err, "Unable to open file %s", filePath)
		log.Error(err)
		return nil, 0, err
	}
	info, err := fp.Stat()
	if err != nil {
		fp.Close()
		err = errors.Wrapf(err, "Unable to stat file %s", filePath)
		log.Error(err)
		return nil, 0, err
	}
	return ioutils.NewReadCloserWrapper(fp, fp.Close), info.Size(), nil
}

func (f *FSClient) DownloadPublic(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	return f.Download(ctx, objectName)
}

func (f *FSClient) DoesObjectExist(ctx context.Context, objectName string) (bool, error) {
	filePath := filepath.Join(f.basedir, objectName)
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "failed to get file %s", filePath)
	}
	if info.IsDir() {
		return false, errors.New(fmt.Sprintf("Expected %s to be a file but found as directory", objectName))
	}
	return true, nil
}

func (f *FSClient) DoesPublicObjectExist(ctx context.Context, objectName string) (bool, error) {
	return f.DoesObjectExist(ctx, objectName)
}

func (f *FSClient) DeleteObject(ctx context.Context, objectName string) (bool, error) {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "Failed to delete file %s", filePath)
	}

	log.Infof("Deleted file %s", filePath)
	return true, nil
}

func (f *FSClient) GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error) {
	filePath := filepath.Join(f.basedir, objectName)
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get file %s", filePath)
	}
	return info.Size(), nil
}

func (f *FSClient) GeneratePresignedDownloadURL(ctx context.Context, objectName string, downloadFilename string, duration time.Duration) (string, error) {
	return "", nil
}

func (f *FSClient) UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error) {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	log.Infof("Updating timestamp of file %s", filePath)
	now := time.Now()
	if err := os.Chtimes(filePath, now, now); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "Failed to update timestamp for file %s", filePath)
	}
	return true, nil
}

func (f *FSClient) ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string)) {
	log := logutil.FromContext(ctx, f.log)
	now := time.Now()

	log.Info("Checking for expired files...")
	err := filepath.Walk(f.basedir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.Base(path), prefix) && !info.IsDir() {
			f.handleFile(ctx, log, path, info, now, deleteTime, callback)
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Error listing files")
		return
	}
}

func (f *FSClient) handleFile(ctx context.Context, log logrus.FieldLogger, filePath string, fileInfo os.FileInfo, now time.Time,
	deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string)) {
	if now.Before(fileInfo.ModTime().Add(deleteTime)) {
		return
	}
	err := os.Remove(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.WithError(err).Errorf("Failed to delete file %s", filePath)
		}
		return
	}
	log.Infof("Deleted expired file %s", filePath)
	callback(ctx, log, filePath)
}

func (f *FSClient) ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	log := logutil.FromContext(ctx, f.log)
	var matches []string
	prefixWithBase := filepath.Join(f.basedir, prefix)
	err := filepath.Walk(f.basedir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, prefixWithBase) && !info.IsDir() {
			relative, err := filepath.Rel(f.basedir, path)
			if err != nil {
				return err
			}

			matches = append(matches, relative)
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Error listing files")
		return nil, err
	}
	return matches, nil
}

func (f *FSClient) UploadBootFiles(ctx context.Context, openshiftVersion, serviceBaseURL string, haveLatestMinimalTemplate bool) error {
	log := logutil.FromContext(ctx, f.log)
	isoObjectName, err := f.GetBaseIsoObject(openshiftVersion)
	if err != nil {
		return err
	}

	minimalIsoObject, err := f.GetMinimalIsoObjectName(openshiftVersion)
	if err != nil {
		return err
	}

	rhcosImageURL, err := f.versionsHandler.GetRHCOSImage(openshiftVersion)
	if err != nil {
		return err
	}

	baseExists, err := f.DoAllBootFilesExist(ctx, isoObjectName)
	if err != nil {
		return err
	}
	minimalExists, err := f.DoesPublicObjectExist(ctx, minimalIsoObject)
	if err != nil {
		return err
	}
	if baseExists && minimalExists {
		return nil
	}

	existsInBucket, err := f.DoesObjectExist(ctx, isoObjectName)
	if err != nil {
		return err
	}
	if !existsInBucket {
		err = UploadFromURLToPublicBucket(ctx, isoObjectName, rhcosImageURL, f)
		if err != nil {
			return err
		}
		log.Infof("Successfully uploaded object %s", isoObjectName)
	}

	isoFilePath := filepath.Join(f.basedir, isoObjectName)

	if !baseExists {
		if err = ExtractBootFilesFromISOAndUpload(ctx, log, isoFilePath, isoObjectName, rhcosImageURL, f); err != nil {
			return err
		}
	}

	if !minimalExists {
		if err = CreateAndUploadMinimalIso(ctx, log, isoFilePath, minimalIsoObject, openshiftVersion, serviceBaseURL, f, f.isoEditorFactory); err != nil {
			return err
		}
	}

	return nil
}

func (f *FSClient) DoAllBootFilesExist(ctx context.Context, isoObjectName string) (bool, error) {
	return DoAllBootFilesExist(ctx, isoObjectName, f)
}

func (f *FSClient) DownloadBootFile(ctx context.Context, isoObjectName, fileType string) (io.ReadCloser, string, int64, error) {
	objectName := BootFileTypeToObjectName(isoObjectName, fileType)
	reader, contentLength, err := f.Download(ctx, objectName)
	return reader, objectName, contentLength, err
}

func (f *FSClient) GetS3BootFileURL(isoObjectName, fileType string) string {
	return ""
}

func (f *FSClient) GetBaseIsoObject(openshiftVersion string) (string, error) {
	// TODO: MGMT-3621 Need to support different versions
	return fsBaseISOName, nil
}

func (f *FSClient) GetMinimalIsoObjectName(openshiftVersion string) (string, error) {
	// TODO: MGMT-3621 Need to support different versions
	return fsMinimalBaseISOName, nil
}
