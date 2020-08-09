package s3wrapper

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	logutil "github.com/openshift/assisted-service/pkg/log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/go-openapi/swag"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=client.go -package=s3wrapper -destination=mock_s3wrapper.go
type API interface {
	CreateBucket() error
	Upload(ctx context.Context, data []byte, objectName string) error
	Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
	DoesObjectExist(ctx context.Context, objectName string) (bool, error)
	DeleteObject(ctx context.Context, objectName string) error
	UpdateObjectTag(ctx context.Context, objectName, key, value string) (bool, error)
	GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error)
	GeneratePresignedDownloadURL(ctx context.Context, objectName string, duration time.Duration) (string, error)
}

var _ API = &S3Client{}

type S3Client struct {
	log     *logrus.Logger
	session *session.Session
	Client  *s3.S3
	cfg     *Config
}

type Config struct {
	S3EndpointURL      string `envconfig:"S3_ENDPOINT_URL"`
	Region             string `envconfig:"S3_REGION"`
	S3Bucket           string `envconfig:"S3_BUCKET"`
	AwsAccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY"`
}

// NewS3Client creates new s3 client using default config along with defined env variables
func NewS3Client(cfg *Config, logger *logrus.Logger) *S3Client {
	awsSession, err := newS3Session(cfg)
	if err != nil {
		return nil
	}

	client := s3.New(awsSession)
	if client == nil {
		return nil
	}

	return &S3Client{Client: client, session: awsSession, cfg: cfg, log: logger}
}

func newS3Session(cfg *Config) (*session.Session, error) {
	HTTPTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 0,
		MaxIdleConnsPerHost:   4096,
		MaxIdleConns:          0,
		IdleConnTimeout:       time.Minute,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // true to enable use s3 with ip address (scality)
	}
	creds := credentials.NewStaticCredentials(cfg.AwsAccessKeyID, cfg.AwsSecretAccessKey, "")

	awsConfig := &aws.Config{
		Region:               aws.String(cfg.Region),
		Endpoint:             aws.String(cfg.S3EndpointURL),
		Credentials:          creds,
		DisableSSL:           aws.Bool(true),
		S3ForcePathStyle:     aws.Bool(true),
		S3Disable100Continue: aws.Bool(true),
		HTTPClient:           &http.Client{Transport: HTTPTransport},
	}
	awsSession, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create s3 session")
	}

	return awsSession, nil
}

func (c *S3Client) CreateBucket() error {
	if _, err := c.Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: swag.String(c.cfg.S3Bucket),
	}); err != nil {
		return errors.Wrapf(err, "Failed to create S3 bucket %s", c.cfg.S3Bucket)
	}
	return nil
}

func (c *S3Client) Upload(ctx context.Context, data []byte, objectName string) error {
	log := logutil.FromContext(ctx, c.log)
	reader := bytes.NewReader(data)
	uploader := s3manager.NewUploader(c.session)
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
		Body:   reader,
	})
	if err != nil {
		err = errors.Wrapf(err, "Unable to upload %s to bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return err
	}
	log.Infof("Successfully uploaded %s to bucket %s", objectName, c.cfg.S3Bucket)
	return err
}

func (c *S3Client) Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Downloading %s from bucket %s", objectName, c.cfg.S3Bucket)

	contentLength, err := c.GetObjectSizeBytes(ctx, objectName)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch metadata for object %s in bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return nil, 0, err
	}

	getResp, err := c.Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to get %s object from bucket %s", objectName, c.cfg.S3Bucket)
		return nil, 0, err
	}

	return getResp.Body, contentLength, nil
}

func (c *S3Client) DoesObjectExist(ctx context.Context, objectName string) (bool, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Verifying if %s exists in %s", objectName, c.cfg.S3Bucket)
	_, err := c.Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to get %s from bucket %s", objectName, c.cfg.S3Bucket))
		}
	}
	return true, nil
}

func (c *S3Client) DeleteObject(ctx context.Context, objectName string) error {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Deleting object %s from %s", objectName, c.cfg.S3Bucket)

	_, err := c.Client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				log.Warnf("Object %s does not exist in bucket %s", objectName, c.cfg.S3Bucket)
				return nil
			}
			return errors.Wrap(err, fmt.Sprintf("Failed to delete object %s from bucket %s", objectName, c.cfg.S3Bucket))
		}
	}

	log.Infof("Deleted object %s from bucket %s", objectName, c.cfg.S3Bucket)
	return nil
}

func (c *S3Client) UpdateObjectTag(ctx context.Context, objectName, key, value string) (bool, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Adding tag to object %s: %s - %s", objectName, key, value)
	_, err := c.Client.PutObjectTagging(&s3.PutObjectTaggingInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
		Tagging: &s3.Tagging{
			TagSet: []*s3.Tag{
				{
					Key:   aws.String(key),
					Value: aws.String(value),
				},
			},
		},
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey {
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("Failed to update tags object %s from bucket %s", objectName, c.cfg.S3Bucket))
		}
	}
	return true, nil
}

func (c *S3Client) GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error) {
	log := logutil.FromContext(ctx, c.log)
	headResp, err := c.Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch metadata for object %s in bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return 0, err
	}
	return *headResp.ContentLength, nil
}

func (c *S3Client) GeneratePresignedDownloadURL(ctx context.Context, objectName string, duration time.Duration) (string, error) {
	log := logutil.FromContext(ctx, c.log)
	req, _ := c.Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	urlStr, err := req.Presign(duration)
	if err != nil {
		err = errors.Wrapf(err, "Failed to create presigned download URL for object %s in bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return "", err
	}
	return urlStr, nil
}
