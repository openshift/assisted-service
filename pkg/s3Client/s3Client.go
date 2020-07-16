package s3Client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/minio/minio-go/v6"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=s3Client.go -package=s3Client -destination=mock_s3client.go
type S3Client interface {
	PushDataToS3(ctx context.Context, data []byte, fileName string, s3Bucket string) error
	DownloadFileFromS3(ctx context.Context, fileName string, s3Bucket string) (io.ReadCloser, int64, error)
	DoesObjectExist(ctx context.Context, fileName string, s3Bucket string) (bool, error)
	UpdateObjectTag(ctx context.Context, objectName, s3Bucket, key, value string) (bool, error)
	DeleteFileFromS3(ctx context.Context, fileName string, s3Bucket string) error
}

type s3Client struct {
	log    *logrus.Logger
	client *minio.Client
}

func NewS3Client(s3EndpointURL string, awsAccessKeyID string, awsSecretAccessKey string, logger *logrus.Logger) (S3Client, error) {
	client, err := minio.New(strings.Replace(s3EndpointURL, "http://", "", 1), awsAccessKeyID, awsSecretAccessKey, false)
	if err != nil {
		return nil, errors.Wrapf(err, "Unable to create aws client to %s", s3EndpointURL)
	}
	return &s3Client{logger, client}, nil
}

func (s s3Client) PushDataToS3(ctx context.Context, data []byte, objectName string, s3Bucket string) error {
	log := logutil.FromContext(ctx, s.log)
	// create a reader from data in memory
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(s3Bucket, objectName, reader, reader.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		err = errors.Wrapf(err, "Unable to upload %s to %s", objectName, s3Bucket)
		log.Error(err)
		return err
	}
	s.log.Infof("Successfully uploaded %s to %s", objectName, s3Bucket)
	return nil
}

func (s s3Client) DownloadFileFromS3(ctx context.Context, fileName string, s3Bucket string) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, s.log)
	log.Infof("Downloading %s from bucket %s", fileName, s3Bucket)
	stat, err := s.client.StatObject(s3Bucket, fileName, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			log.Warnf("%s doesn't exists in bucket %s", fileName, s3Bucket)
			return nil, 0, errors.Errorf("%s doesn't exist", fileName)
		}
		return nil, 0, err
	}
	contentLength := stat.Size

	resp, err := s.client.GetObject(s3Bucket, fileName, minio.GetObjectOptions{})
	if err != nil {
		log.WithError(err).Errorf("Failed to get %s file", fileName)
		return nil, 0, err
	}

	return resp, contentLength, nil
}

func (s s3Client) DoesObjectExist(ctx context.Context, objectName string, s3Bucket string) (bool, error) {
	log := logutil.FromContext(ctx, s.log)
	log.Infof("Verifying if %s exists in %s", objectName, s3Bucket)
	_, err := s.client.StatObject(s3Bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		return false, errors.Wrap(err, fmt.Sprintf("failed to get %s from %s", objectName, s3Bucket))
	}
	return true, nil
}

func (s s3Client) DeleteFileFromS3(ctx context.Context, fileName string, s3Bucket string) error {
	log := logutil.FromContext(ctx, s.log)
	log.Infof("Deleting file if %s exists in %s", fileName, s3Bucket)
	err := s.client.RemoveObject(s3Bucket, fileName)
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			log.Warnf("File %s does not exists in %s", fileName, s3Bucket)
			return nil
		}
		return errors.Wrap(err, fmt.Sprintf("failed to delete %s from %s", fileName, s3Bucket))
	}
	log.Infof("Deleted file %s from %s", fileName, s3Bucket)
	return nil
}

func (s s3Client) UpdateObjectTag(ctx context.Context, objectName, s3Bucket, key, value string) (bool, error) {
	log := logutil.FromContext(ctx, s.log)
	log.Infof("Adding tag: %s - %s", key, value)
	tags := map[string]string{key: value}
	err := s.client.PutObjectTagging(s3Bucket, objectName, tags)
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		log.Errorf("Updating object tag failed: %s", errResponse.Code)
		return false, errors.Wrap(err, fmt.Sprintf("failed to update tags on %s/%s", s3Bucket, objectName))
	}
	return true, nil
}
