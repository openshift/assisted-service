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

	logutil "github.com/openshift/assisted-service/pkg/log"

	"github.com/moby/moby/pkg/ioutils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type FSClient struct {
	log     logrus.FieldLogger
	basedir string
}

func NewFSClient(basedir string, logger logrus.FieldLogger) *FSClient {
	return &FSClient{log: logger, basedir: basedir}
}

func (f *FSClient) IsAwsS3() bool {
	return false
}

func (f *FSClient) CreateBucket() error {
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
	if err := ioutil.WriteFile(filePath, data, 0600); err != nil {
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

func (f *FSClient) UploadISO(ctx context.Context, ignitionConfig, objectPrefix string) error {
	log := logutil.FromContext(ctx, f.log)
	resultFile := filepath.Join(f.basedir, fmt.Sprintf("%s.iso", objectPrefix))
	baseFile := filepath.Join(f.basedir, BaseObjectName)
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
	fo, err := os.Create(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to open file for writing %s", filePath)
		log.Error(err)
		return err
	}
	defer func() {
		if err := fo.Close(); err != nil {
			log.Error("Unable to close file %s", filePath)
		}
	}()
	for {
		length, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			err = errors.Wrapf(err, "Unable to read data for upload to file %s", filePath)
			log.Error(err)
			return err
		}
		if _, err := fo.Write(buffer[0:length]); err != nil {
			err = errors.Wrapf(err, "Unable to write data to file %s", filePath)
			log.Error(err)
			return err
		}
	}

	log.Infof("Successfully uploaded file %s", objectName)
	return nil
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

func (f *FSClient) DeleteObject(ctx context.Context, objectName string) error {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrapf(err, "Failed to delete file %s", filePath)
	}

	log.Infof("Deleted file %s", filePath)
	return nil
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

			matches = append(matches, filepath.Join("/", relative))
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Error listing files")
		return nil, err
	}
	return matches, nil
}
