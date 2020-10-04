package s3wrapper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	logutil "github.com/openshift/assisted-service/pkg/log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/go-openapi/swag"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const awsEndpointSuffix = ".amazonaws.com"
const BaseObjectName = "livecd.iso"

//go:generate mockgen -source=client.go -package=s3wrapper -destination=mock_s3wrapper.go
//go:generate mockgen -package s3wrapper -destination mock_s3iface.go github.com/aws/aws-sdk-go/service/s3/s3iface S3API
type API interface {
	IsAwsS3() bool
	CreateBucket() error
	Upload(ctx context.Context, data []byte, objectName string) error
	UploadStream(ctx context.Context, reader io.Reader, objectName string) error
	UploadFile(ctx context.Context, filePath, objectName string) error
	UploadISO(ctx context.Context, ignitionConfig, objectPrefix string) error
	Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
	DoesObjectExist(ctx context.Context, objectName string) (bool, error)
	DeleteObject(ctx context.Context, objectName string) error
	GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error)
	GeneratePresignedDownloadURL(ctx context.Context, objectName string, downloadFilename string, duration time.Duration) (string, error)
	UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error)
	ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string))
	ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error)
}

var _ API = &S3Client{}

type S3Client struct {
	log         logrus.FieldLogger
	session     *session.Session
	client      s3iface.S3API
	cfg         *Config
	isoUploader ISOUploaderAPI
}

type Config struct {
	S3EndpointURL      string `envconfig:"S3_ENDPOINT_URL"`
	Region             string `envconfig:"S3_REGION"`
	S3Bucket           string `envconfig:"S3_BUCKET"`
	AwsAccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY"`
}

const timestampTagKey = "create_sec_since_epoch"

// NewS3Client creates new s3 client using default config along with defined env variables
func NewS3Client(cfg *Config, logger logrus.FieldLogger) *S3Client {
	awsSession, err := newS3Session(cfg)
	if err != nil {
		return nil
	}

	client := s3.New(awsSession)
	if client == nil {
		return nil
	}

	isoUploader := NewISOUploader(logger, client, cfg.S3Bucket)
	return &S3Client{client: client, session: awsSession, cfg: cfg, log: logger, isoUploader: isoUploader}
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

func (c *S3Client) IsAwsS3() bool {
	// If AWS, URL should be empty or like s3.us-east-1.amazonaws.com
	if c.cfg.S3EndpointURL == "" || strings.HasSuffix(c.cfg.S3EndpointURL, awsEndpointSuffix) {
		return true
	}
	return false
}

func (c *S3Client) CreateBucket() error {
	if _, err := c.client.CreateBucket(&s3.CreateBucketInput{
		Bucket: swag.String(c.cfg.S3Bucket),
	}); err != nil {
		return errors.Wrapf(err, "Failed to create S3 bucket %s", c.cfg.S3Bucket)
	}
	return nil
}

func (c *S3Client) UploadStream(ctx context.Context, reader io.Reader, objectName string) error {
	log := logutil.FromContext(ctx, c.log)
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

func (c *S3Client) UploadFile(ctx context.Context, filePath, objectName string) error {
	log := logutil.FromContext(ctx, c.log)
	file, err := os.Open(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to open file %s for upload", filePath)
		log.Error(err)
		return err
	}

	reader := bufio.NewReader(file)
	return c.UploadStream(ctx, reader, objectName)
}

func (c *S3Client) UploadISO(ctx context.Context, ignitionConfig, objectPrefix string) error {
	objectName := fmt.Sprintf("%s.iso", objectPrefix)
	return c.isoUploader.UploadISO(ctx, ignitionConfig, objectName)
}

func (c *S3Client) Upload(ctx context.Context, data []byte, objectName string) error {
	reader := bytes.NewReader(data)
	return c.UploadStream(ctx, reader, objectName)
}

func (c *S3Client) Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Downloading %s from bucket %s", objectName, c.cfg.S3Bucket)

	contentLength, err := c.GetObjectSizeBytes(ctx, objectName)
	if err != nil {
		if transformed, transformedError := c.transformErrorIfNeeded(err, objectName); transformed {
			return nil, 0, transformedError
		}

		err = errors.Wrapf(err, "Failed to fetch metadata for object %s in bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return nil, 0, err
	}

	getResp, err := c.client.GetObject(&s3.GetObjectInput{
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
	log.Debugf("Verifying if %s exists in %s", objectName, c.cfg.S3Bucket)
	_, err := c.client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to get %s from bucket %s (code %s)", objectName, c.cfg.S3Bucket, aerr.Code()))
		}
	}
	return true, nil
}

func (c *S3Client) DeleteObject(ctx context.Context, objectName string) error {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Deleting object %s from %s", objectName, c.cfg.S3Bucket)

	_, err := c.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				log.Warnf("Object %s does not exist in bucket %s", objectName, c.cfg.S3Bucket)
				return nil
			}
			return errors.Wrap(err, fmt.Sprintf("Failed to delete object %s from bucket %s (code %s)", objectName, c.cfg.S3Bucket, aerr.Code()))
		}
	}

	log.Infof("Deleted object %s from bucket %s", objectName, c.cfg.S3Bucket)
	return nil
}

func (c *S3Client) UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Updating timestamp of object %s", objectName)
	_, err := c.client.PutObjectTagging(&s3.PutObjectTaggingInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
		Tagging: &s3.Tagging{
			TagSet: []*s3.Tag{
				{
					Key:   aws.String(timestampTagKey),
					Value: aws.String(strconv.FormatInt(time.Now().Unix(), 10)),
				},
			},
		},
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			// S3 returns MethodNotAllowed if an object existed but was deleted
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" || aerr.Code() == "MethodNotAllowed" {
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("Failed to update tags on object %s from bucket %s (code %s)", objectName, c.cfg.S3Bucket, aerr.Code()))
		}
	}
	return true, nil
}

func (c *S3Client) GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error) {
	log := logutil.FromContext(ctx, c.log)
	headResp, err := c.client.HeadObject(&s3.HeadObjectInput{
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

func (c *S3Client) GeneratePresignedDownloadURL(ctx context.Context, objectName string, downloadFilename string, duration time.Duration) (string, error) {
	log := logutil.FromContext(ctx, c.log)
	req, _ := c.client.GetObjectRequest(&s3.GetObjectInput{
		Bucket:                     aws.String(c.cfg.S3Bucket),
		Key:                        aws.String(objectName),
		ResponseContentDisposition: aws.String(fmt.Sprintf("attachment;filename=%s", downloadFilename)),
	})
	urlStr, err := req.Presign(duration)
	if err != nil {
		err = errors.Wrapf(err, "Failed to create presigned download URL for object %s in bucket %s", objectName, c.cfg.S3Bucket)
		log.Error(err)
		return "", err
	}
	return urlStr, nil
}

func (c S3Client) transformErrorIfNeeded(err error, objectName string) (bool, error) {
	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
			return true, NotFound(objectName)
		}
	}
	return false, err
}

func (c *S3Client) ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration,
	callback func(ctx context.Context, log logrus.FieldLogger, objectName string)) {
	log := logutil.FromContext(ctx, c.log)
	now := time.Now()

	log.Info("Checking for expired objects...")
	err := c.client.ListObjectsPages(&s3.ListObjectsInput{Bucket: &c.cfg.S3Bucket, Prefix: &prefix},
		func(page *s3.ListObjectsOutput, lastPage bool) bool {
			for _, object := range page.Contents {
				c.handleObject(ctx, log, object, now, deleteTime, callback)
			}
			return !lastPage
		})
	if err != nil {
		log.WithError(err).Error("Error listing objects")
		return
	}
}

func (c *S3Client) handleObject(ctx context.Context, log logrus.FieldLogger, object *s3.Object, now time.Time,
	deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string)) {
	// The timestamp that we really want is stored in a tag, but we check this one first as a cost optimization
	if now.Before(object.LastModified.Add(deleteTime)) {
		return
	}
	objectTags, err := c.client.GetObjectTagging(&s3.GetObjectTaggingInput{Bucket: &c.cfg.S3Bucket, Key: object.Key})
	if err != nil {
		log.WithError(err).Errorf("Error getting tags for object %s", *object.Key)
		return
	}
	for _, tag := range objectTags.TagSet {
		if *tag.Key == timestampTagKey {
			objTime, _ := strconv.ParseInt(*tag.Value, 10, 64)
			if now.After(time.Unix(objTime, 0).Add(deleteTime)) {
				if err := c.DeleteObject(ctx, *object.Key); err != nil {
					log.Errorf("Error deleting expired object %s", *object.Key)
					continue
				}
				log.Infof("Deleted expired object %s", *object.Key)
				callback(ctx, log, *object.Key)
			}
		}
	}
}

func (c *S3Client) ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	log := logutil.FromContext(ctx, c.log)
	var objects []string
	log.Info("Listing objects by with prefix %s", prefix)
	resp, err := c.client.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		err = errors.Wrapf(err, "Error listing objects for prefix %s", prefix)
		log.Error(err)
		return nil, err
	}
	for _, key := range resp.Contents {
		objects = append(objects, *key.Key)
	}
	return objects, nil
}
