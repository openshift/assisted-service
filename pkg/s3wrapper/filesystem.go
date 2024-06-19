package s3wrapper

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/google/renameio"
	"github.com/moby/moby/pkg/ioutils"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/metrics"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/pkg/xattr"
	"github.com/sirupsen/logrus"
	syscall "golang.org/x/sys/unix"
)

type FSClient struct {
	log     logrus.FieldLogger
	basedir string
}

var _ API = &FSClient{}

func NewFSClient(basedir string, logger logrus.FieldLogger, metricsAPI metrics.API, fsThreshold int) *FSClientDecorator {
	return &FSClientDecorator{
		log:        logger,
		metricsAPI: metricsAPI,
		fsClient: FSClient{
			log:     logger,
			basedir: basedir,
		},
		fsUsageThreshold:              fsThreshold,
		timeFSUsageLog:                time.Now().Add(-1 * time.Hour),
		loggingIntervalBelowThreshold: 1 * int64(time.Hour),
		loggingIntervalAboveThreshold: 5 * int64(time.Minute),
	}
}

func (f *FSClient) IsAwsS3() bool {
	return false
}

func (f *FSClient) CreateBucket() error {
	return nil
}

const xattrUserAttributePrefix = "user."

func (f *FSClient) writeFileMetadata(filePath string, metadata map[string]string) error {
	for attributeName, attributeValue := range metadata {
		err := xattr.Set(filePath, strings.ToLower(fmt.Sprintf("%s%s", xattrUserAttributePrefix, attributeName)), []byte(attributeValue))
		if err != nil {
			return errors.Wrapf(err, "unable to store metadata key %s = %s", attributeName, attributeValue)
		}
	}
	return nil
}

func (f *FSClient) getFileMetadata(filePath string) (map[string]string, error) {
	attributes := make(map[string]string)
	attributeNames, err := xattr.List(filePath)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to obtain extended file attributes while retrieving file metadata")
	}
	for _, attributeName := range attributeNames {
		if !strings.HasPrefix(attributeName, xattrUserAttributePrefix) {
			continue
		}
		attributeByteValue, err := xattr.Get(filePath, attributeName)
		if err != nil {
			return nil, errors.Wrap(err, "Unable to obtain extended file attributes while retrieving file metadata")
		}
		attributeValue := string(attributeByteValue)
		attributes[strings.TrimPrefix(attributeName, xattrUserAttributePrefix)] = attributeValue
	}
	return attributes, nil
}

func (f *FSClient) Upload(ctx context.Context, data []byte, objectName string) error {
	return f.upload(ctx, data, objectName, nil)
}

func (f *FSClient) UploadWithMetadata(ctx context.Context, data []byte, objectName string, metadata map[string]string) error {
	return f.upload(ctx, data, objectName, metadata)
}

func (f *FSClient) upload(ctx context.Context, data []byte, objectName string, metadata map[string]string) error {

	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	if err := os.MkdirAll(path.Dir(filePath), 0755); err != nil {
		err = errors.Wrapf(err, "Unable to create directory for file data %s", filePath)
		log.Error(err)
		return err
	}
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

	if bytesWritten, err := t.Write(data); err != nil || bytesWritten != len(data) {
		err = errors.Wrapf(err, "Unable to write data to file %s", filePath)
		log.Error(err)
		return err
	}
	if err := f.writeFileMetadata(t.Name(), metadata); err != nil {
		err = errors.Wrapf(err, "Unable to write file metadata for file %s", filePath)
		log.Error(err)
		return err
	}
	if err := t.CloseAtomicallyReplace(); err != nil {
		err = errors.Wrapf(err, "Unable to atomically replace %s with temp file %s", filePath, t.Name())
		log.Error(err)
		return err
	}

	log.Infof("Successfully uploaded file %s", objectName)
	return nil
}

func (f *FSClient) UploadFile(ctx context.Context, filePath, objectName string) error {
	return f.uploadFile(ctx, filePath, objectName, nil)
}

func (f *FSClient) UploadFileWithMetadata(ctx context.Context, filePath, objectName string, metadata map[string]string) error {
	return f.uploadFile(ctx, filePath, objectName, metadata)
}

func (f *FSClient) uploadFile(ctx context.Context, filePath, objectName string, metadata map[string]string) error {
	log := logutil.FromContext(ctx, f.log)
	file, err := os.Open(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to open file %s for upload", filePath)
		log.Error(err)
		return err
	}

	defer file.Close()

	return f.uploadStream(ctx, file, objectName, metadata)
}

func (f *FSClient) UploadStream(ctx context.Context, reader io.Reader, objectName string) error {
	return f.uploadStream(ctx, reader, objectName, nil)
}

func (f *FSClient) UploadStreamWithMetadata(ctx context.Context, reader io.Reader, objectName string, metadata map[string]string) error {
	return f.uploadStream(ctx, reader, objectName, metadata)
}

func (f *FSClient) uploadStream(ctx context.Context, reader io.Reader, objectName string, metadata map[string]string) error {
	log := logutil.FromContext(ctx, f.log)
	filePath := filepath.Join(f.basedir, objectName)
	if err := os.MkdirAll(path.Dir(filePath), 0755); err != nil {
		err = errors.Wrapf(err, "Unable to create directory for file data %s", filePath)
		log.Error(err)
		return err
	}
	buffer := make([]byte, units.MiB)

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
	if err := f.writeFileMetadata(t.Name(), metadata); err != nil {
		err = errors.Wrapf(err, "Unable to write file metadata for file %s", filePath)
		log.Error(err)
		return err
	}

	if err := t.CloseAtomicallyReplace(); err != nil {
		err = errors.Wrapf(err, "Unable to atomically replace %s with temp file %s", filePath, t.Name())
		log.Error(err)
		return err
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
			return nil, 0, common.NotFound(objectName)
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

func (f *FSClient) ListObjectsByPrefixWithMetadata(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	log := logutil.FromContext(ctx, f.log)
	var matches []ObjectInfo
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

			metadata, err := f.getFileMetadata(path)
			if err != nil {
				return err
			}

			matches = append(matches, ObjectInfo{Path: relative, Metadata: metadata})
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Error listing files")
		return nil, err
	}
	return matches, nil
}

type FSClientDecorator struct {
	log                           logrus.FieldLogger
	fsClient                      FSClient
	metricsAPI                    metrics.API
	fsUsageThreshold              int
	lastFSUsage                   float64
	timeFSUsageLog                time.Time
	loggingIntervalBelowThreshold int64
	loggingIntervalAboveThreshold int64
}

var _ API = &FSClientDecorator{}

func (d *FSClientDecorator) shouldLog() bool {
	var pauseBetweenLogs int64
	if d.fsUsageThreshold < int(d.lastFSUsage) {
		pauseBetweenLogs = d.loggingIntervalAboveThreshold
	} else {
		pauseBetweenLogs = d.loggingIntervalBelowThreshold
	}
	return int64(time.Since(d.timeFSUsageLog)) > pauseBetweenLogs
}

func (d *FSClientDecorator) conditionalLog(msg string, logLevel logrus.Level, fixedPercentage float64) {
	if d.shouldLog() {
		switch logLevel {
		case logrus.WarnLevel:
			d.log.Warn(msg)
		default:
			d.log.Info(msg)
		}
		d.lastFSUsage = fixedPercentage
		d.timeFSUsageLog = time.Now()
	}
}

func (d *FSClientDecorator) reportFilesystemUsageMetrics() {
	basedir := d.fsClient.basedir
	stat := syscall.Statfs_t{}
	err := syscall.Statfs(basedir, &stat)
	if err != nil {
		d.fsClient.log.WithError(err).Errorf("Failed to collect filesystem stats for %s", basedir)
		return
	}
	percentage := (float64(stat.Blocks-stat.Bfree) / float64(stat.Blocks)) * 100
	fixedPercentage := math.Floor(percentage*10) / 10
	if fixedPercentage >= float64(d.fsUsageThreshold) {
		msg := fmt.Sprintf("Filesystem '%s' usage is %.1f%% which exceeds threshold %d%%", basedir, fixedPercentage, d.fsUsageThreshold)
		d.conditionalLog(msg, logrus.WarnLevel, fixedPercentage)
	} else {
		msg := fmt.Sprintf("Filesystem '%s' usage is %.1f%%", basedir, fixedPercentage)
		d.conditionalLog(msg, logrus.InfoLevel, fixedPercentage)
	}
	d.metricsAPI.FileSystemUsage(fixedPercentage)
}

func (d *FSClientDecorator) IsAwsS3() bool {
	return d.fsClient.IsAwsS3()
}

func (d *FSClientDecorator) CreateBucket() error {
	return d.fsClient.CreateBucket()
}

func (d *FSClientDecorator) Upload(ctx context.Context, data []byte, objectName string) error {
	err := d.fsClient.Upload(ctx, data, objectName)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) UploadWithMetadata(ctx context.Context, data []byte, objectName string, metadata map[string]string) error {
	err := d.fsClient.UploadWithMetadata(ctx, data, objectName, metadata)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) UploadStream(ctx context.Context, reader io.Reader, objectName string) error {
	err := d.fsClient.UploadStream(ctx, reader, objectName)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) UploadStreamWithMetadata(ctx context.Context, reader io.Reader, objectName string, metadata map[string]string) error {
	err := d.fsClient.UploadStreamWithMetadata(ctx, reader, objectName, metadata)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) UploadFile(ctx context.Context, filePath, objectName string) error {
	err := d.fsClient.UploadFile(ctx, filePath, objectName)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) UploadFileWithMetadata(ctx context.Context, filePath, objectName string, metadata map[string]string) error {
	err := d.fsClient.UploadFileWithMetadata(ctx, filePath, objectName, metadata)
	if err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return err
}

func (d *FSClientDecorator) Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	return d.fsClient.Download(ctx, objectName)
}

func (d *FSClientDecorator) DoesObjectExist(ctx context.Context, objectName string) (bool, error) {
	return d.fsClient.DoesObjectExist(ctx, objectName)
}

func (d *FSClientDecorator) DeleteObject(ctx context.Context, objectName string) (bool, error) {
	exists, err := d.fsClient.DeleteObject(ctx, objectName)
	if exists && err == nil {
		d.reportFilesystemUsageMetrics()
	}
	return exists, err
}

func (d *FSClientDecorator) GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error) {
	return d.fsClient.GetObjectSizeBytes(ctx, objectName)
}

func (d *FSClientDecorator) GeneratePresignedDownloadURL(ctx context.Context, objectName string, downloadFilename string, duration time.Duration) (string, error) {
	return d.fsClient.GeneratePresignedDownloadURL(ctx, objectName, downloadFilename, duration)
}

func (d *FSClientDecorator) UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error) {
	return d.fsClient.UpdateObjectTimestamp(ctx, objectName)
}

func (d *FSClientDecorator) ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string)) {
	d.fsClient.ExpireObjects(ctx, prefix, deleteTime, callback)
	d.reportFilesystemUsageMetrics()
}

func (d *FSClientDecorator) ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	return d.fsClient.ListObjectsByPrefix(ctx, prefix)
}

func (d *FSClientDecorator) ListObjectsByPrefixWithMetadata(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	return d.fsClient.ListObjectsByPrefixWithMetadata(ctx, prefix)
}
