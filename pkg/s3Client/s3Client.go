package s3Client

import (
	"bytes"
	"context"
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
	DownloadFileFromS3(ctx context.Context, fileName string, s3Bucket string) (io.ReadCloser, error)
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

func (s s3Client) PushDataToS3(ctx context.Context, data []byte, fileName string, s3Bucket string) error {
	log := logutil.FromContext(ctx, s.log)
	// create a reader from data in memory
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(s3Bucket, fileName, reader, reader.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		err = errors.Wrapf(err, "Unable to upload %s to %s", fileName, s3Bucket)
		log.Error(err)
		return err
	}
	s.log.Infof("Successfully uploaded %s to %s", fileName, s3Bucket)
	return nil
}

func (s s3Client) DownloadFileFromS3(ctx context.Context, fileName string, s3Bucket string) (io.ReadCloser, error) {
	log := logutil.FromContext(ctx, s.log)
	log.Infof("Downloading %s from bucket %s", fileName, s3Bucket)
	resp, err := s.client.GetObject(s3Bucket, fileName, minio.GetObjectOptions{})
	if err != nil {
		log.WithError(err).Errorf("Failed to get %s file", fileName)
		return nil, err
	}
	return resp, nil
}
