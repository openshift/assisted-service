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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/s3/s3manager/s3manageriface"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/versions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	awsEndpointSuffix          = ".amazonaws.com"
	rhcosObjectTemplate        = "rhcos-%s.iso"
	rhcosMinimalObjectTemplate = "rhcos-%s-minimal.iso"
	DiscoveryImageTemplate     = "discovery-image-%s"
)

//go:generate mockgen -package=s3wrapper -destination=mock_s3wrapper.go . API
//go:generate mockgen -package s3wrapper -destination mock_s3iface.go github.com/aws/aws-sdk-go/service/s3/s3iface S3API
//go:generate mockgen -package s3wrapper -destination mock_s3manageriface.go github.com/aws/aws-sdk-go/service/s3/s3manager/s3manageriface UploaderAPI
type API interface {
	IsAwsS3() bool
	CreateBucket() error
	Upload(ctx context.Context, data []byte, objectName string) error
	UploadStream(ctx context.Context, reader io.Reader, objectName string) error
	UploadFile(ctx context.Context, filePath, objectName string) error
	UploadISO(ctx context.Context, ignitionConfig, srcObject, destObjectPrefix string) error
	Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
	DoesObjectExist(ctx context.Context, objectName string) (bool, error)
	DeleteObject(ctx context.Context, objectName string) (bool, error)
	GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error)
	GeneratePresignedDownloadURL(ctx context.Context, objectName string, downloadFilename string, duration time.Duration) (string, error)
	UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error)
	ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string))
	ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error)
	UploadISOs(ctx context.Context, openshiftVersion string, haveLatestMinimalTemplate bool) error
	GetBaseIsoObject(openshiftVersion string) (string, error)
	GetMinimalIsoObjectName(openshiftVersion string) (string, error)

	CreatePublicBucket() error
	UploadStreamToPublicBucket(ctx context.Context, reader io.Reader, objectName string) error
	UploadFileToPublicBucket(ctx context.Context, filePath, objectName string) error
	DoesPublicObjectExist(ctx context.Context, objectName string) (bool, error)
	DownloadPublic(ctx context.Context, objectName string) (io.ReadCloser, int64, error)
}

var _ API = &S3Client{}

type S3Client struct {
	log              logrus.FieldLogger
	session          *session.Session
	client           s3iface.S3API
	uploader         s3manageriface.UploaderAPI
	publicSession    *session.Session
	publicClient     s3iface.S3API
	publicUploader   s3manageriface.UploaderAPI
	cfg              *Config
	isoUploader      ISOUploaderAPI
	versionsHandler  versions.Handler
	isoEditorFactory isoeditor.Factory
}

type Config struct {
	S3EndpointURL      string `envconfig:"S3_ENDPOINT_URL"`
	Region             string `envconfig:"S3_REGION"`
	S3Bucket           string `envconfig:"S3_BUCKET"`
	AwsAccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID"`
	AwsSecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY"`

	// Warning - the files stored in this bucket are publicly viewable and therefore
	// should only be used for storing RHCOS image files that are readily available on the Internet
	PublicS3EndpointURL      string `envconfig:"S3_ENDPOINT_URL_PUBLIC"`
	PublicRegion             string `envconfig:"S3_REGION_PUBLIC"`
	PublicS3Bucket           string `envconfig:"S3_BUCKET_PUBLIC"`
	PublicAwsAccessKeyID     string `envconfig:"AWS_ACCESS_KEY_ID_PUBLIC"`
	PublicAwsSecretAccessKey string `envconfig:"AWS_SECRET_ACCESS_KEY_PUBLIC"`
}

const timestampTagKey = "create_sec_since_epoch"

// NewS3Client creates new s3 client using default config along with defined env variables
func NewS3Client(cfg *Config, logger logrus.FieldLogger, versionsHandler versions.Handler, isoEditorFactory isoeditor.Factory) *S3Client {
	awsSession, err := newS3Session(cfg.AwsAccessKeyID, cfg.AwsSecretAccessKey, cfg.Region, cfg.S3EndpointURL)
	if err != nil {
		logger.WithError(err).Error("failed to create s3 session")
		return nil
	}
	client := s3.New(awsSession)
	if client == nil {
		return nil
	}
	uploader := s3manager.NewUploader(awsSession)

	publicAwsSession, err := newS3Session(cfg.PublicAwsAccessKeyID, cfg.PublicAwsSecretAccessKey, cfg.PublicRegion, cfg.PublicS3EndpointURL)
	if err != nil {
		logger.WithError(err).Error("failed to create s3 public session")
		return nil
	}
	publicClient := s3.New(publicAwsSession)
	if publicClient == nil {
		return nil
	}
	publicUploader := s3manager.NewUploader(publicAwsSession)

	isoUploader := NewISOUploader(logger, client, cfg.S3Bucket, cfg.PublicS3Bucket)
	return &S3Client{client: client, session: awsSession, uploader: uploader,
		publicClient: publicClient, publicSession: publicAwsSession, publicUploader: publicUploader,
		cfg: cfg, log: logger, isoUploader: isoUploader, versionsHandler: versionsHandler,
		isoEditorFactory: isoEditorFactory}
}

func newS3Session(accessKeyID, secretAccessKey, region, endpointURL string) (*session.Session, error) {
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
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // true to enable use s3 with ip address (minio)
	}
	creds := credentials.NewStaticCredentials(accessKeyID, secretAccessKey, "")

	awsConfig := &aws.Config{
		Region:               aws.String(region),
		Endpoint:             aws.String(endpointURL),
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

func (c *S3Client) createBucket(client s3iface.S3API, bucket string) error {
	// assume an error from HeadBucket means the bucket does not exist
	if _, err := client.HeadBucket(&s3.HeadBucketInput{
		Bucket: swag.String(bucket),
	}); err == nil {
		return nil
	}

	if _, err := client.CreateBucket(&s3.CreateBucketInput{
		Bucket: swag.String(bucket),
	}); err != nil {
		return errors.Wrapf(err, "Failed to create S3 bucket %s", bucket)
	}
	return nil
}

func (c *S3Client) CreateBucket() error {
	return c.createBucket(c.client, c.cfg.S3Bucket)
}

func (c *S3Client) CreatePublicBucket() error {
	return c.createBucket(c.publicClient, c.cfg.PublicS3Bucket)
}

func (c *S3Client) uploadStream(ctx context.Context, reader io.Reader, objectName, bucket string, uploader s3manageriface.UploaderAPI) error {
	log := logutil.FromContext(ctx, c.log)
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectName),
		Body:   reader,
	})
	if err != nil {
		err = errors.Wrapf(err, "Unable to upload %s to bucket %s", objectName, bucket)
		log.Error(err)
		return err
	}
	log.Infof("Successfully uploaded %s to bucket %s", objectName, bucket)
	return err
}

func (c *S3Client) UploadStream(ctx context.Context, reader io.Reader, objectName string) error {
	return c.uploadStream(ctx, reader, objectName, c.cfg.S3Bucket, c.uploader)
}

func (c *S3Client) UploadStreamToPublicBucket(ctx context.Context, reader io.Reader, objectName string) error {
	return c.uploadStream(ctx, reader, objectName, c.cfg.PublicS3Bucket, c.publicUploader)
}

func (c *S3Client) uploadFile(ctx context.Context, filePath, objectName, bucket string, uploader s3manageriface.UploaderAPI) error {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Uploading file %s as object %s to bucket %s", filePath, objectName, bucket)
	file, err := os.Open(filePath)
	if err != nil {
		err = errors.Wrapf(err, "Unable to open file %s for upload", filePath)
		log.Error(err)
		return err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	return c.uploadStream(ctx, reader, objectName, bucket, uploader)
}

func (c *S3Client) UploadFile(ctx context.Context, filePath, objectName string) error {
	return c.uploadFile(ctx, filePath, objectName, c.cfg.S3Bucket, c.uploader)
}

func (c *S3Client) UploadFileToPublicBucket(ctx context.Context, filePath, objectName string) error {
	return c.uploadFile(ctx, filePath, objectName, c.cfg.PublicS3Bucket, c.publicUploader)
}

func (c *S3Client) UploadISO(ctx context.Context, ignitionConfig, srcObject, destObjectPrefix string) error {
	destObjectName := fmt.Sprintf("%s.iso", destObjectPrefix)
	return c.isoUploader.UploadISO(ctx, ignitionConfig, srcObject, destObjectName)
}

func (c *S3Client) Upload(ctx context.Context, data []byte, objectName string) error {
	reader := bytes.NewReader(data)
	return c.UploadStream(ctx, reader, objectName)
}

func (c *S3Client) download(ctx context.Context, objectName, bucket string, client s3iface.S3API) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Downloading %s from bucket %s", objectName, bucket)

	contentLength, err := c.getObjectSizeBytes(ctx, objectName, bucket, client)
	if err != nil {
		if transformed, transformedError := c.transformErrorIfNeeded(err, objectName); transformed {
			return nil, 0, transformedError
		}

		err = errors.Wrapf(err, "Failed to fetch metadata for object %s in bucket %s", objectName, bucket)
		log.Error(err)
		return nil, 0, err
	}

	getResp, err := client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to get %s object from bucket %s", objectName, bucket)
		return nil, 0, err
	}

	return getResp.Body, contentLength, nil
}

func (c *S3Client) Download(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	return c.download(ctx, objectName, c.cfg.S3Bucket, c.client)
}

func (c *S3Client) DownloadPublic(ctx context.Context, objectName string) (io.ReadCloser, int64, error) {
	return c.download(ctx, objectName, c.cfg.PublicS3Bucket, c.client)
}

func (c *S3Client) doesObjectExist(ctx context.Context, objectName, bucket string, client s3iface.S3API) (bool, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Debugf("Verifying if %s exists in %s", objectName, bucket)
	_, err := client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to get %s from bucket %s (code %s)", objectName, bucket, aerr.Code()))
		}
	}
	return true, nil
}

func (c *S3Client) DoesObjectExist(ctx context.Context, objectName string) (bool, error) {
	return c.doesObjectExist(ctx, objectName, c.cfg.S3Bucket, c.client)
}

func (c *S3Client) DoesPublicObjectExist(ctx context.Context, objectName string) (bool, error) {
	return c.doesObjectExist(ctx, objectName, c.cfg.PublicS3Bucket, c.publicClient)
}

func (c *S3Client) DeleteObject(ctx context.Context, objectName string) (bool, error) {
	log := logutil.FromContext(ctx, c.log)
	log.Infof("Deleting object %s from %s", objectName, c.cfg.S3Bucket)

	_, err := c.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey || aerr.Code() == "NotFound" {
				log.Infof("Object %s does not exist in bucket %s", objectName, c.cfg.S3Bucket)
				return false, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("Failed to delete object %s from bucket %s (code %s)", objectName, c.cfg.S3Bucket, aerr.Code()))
		}
	}

	log.Infof("Deleted object %s from bucket %s", objectName, c.cfg.S3Bucket)
	return true, nil
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

func (c *S3Client) getObjectSizeBytes(ctx context.Context, objectName, bucket string, client s3iface.S3API) (int64, error) {
	log := logutil.FromContext(ctx, c.log)
	headResp, err := client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectName),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch metadata for object %s in bucket %s", objectName, bucket)
		log.Error(err)
		return 0, err
	}
	return *headResp.ContentLength, nil
}

func (c *S3Client) GetObjectSizeBytes(ctx context.Context, objectName string) (int64, error) {
	return c.getObjectSizeBytes(ctx, objectName, c.cfg.S3Bucket, c.client)
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
			return true, common.NotFound(objectName)
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
	// By default we use the object creation time - tags only exist if the same image was created more than once
	creationTime := *object.LastModified
	// If this is too new, there is no point in checking tags
	if now.Before(creationTime.Add(deleteTime)) {
		return
	}

	objectTags, err := c.client.GetObjectTagging(&s3.GetObjectTaggingInput{Bucket: &c.cfg.S3Bucket, Key: object.Key})
	if err != nil {
		log.WithError(err).Errorf("Error getting tags for object %s", *object.Key)
		return
	}

	// If no tag was created, then the TagSet is an empty list
	for _, tag := range objectTags.TagSet {
		if *tag.Key == timestampTagKey {
			objTime, _ := strconv.ParseInt(*tag.Value, 10, 64)
			creationTime = time.Unix(objTime, 0)
		}
	}

	if now.After(creationTime.Add(deleteTime)) {
		_, err := c.DeleteObject(ctx, *object.Key)
		if err != nil {
			log.WithError(err).Errorf("Error deleting expired object %s", *object.Key)
			return
		}
		log.Infof("Deleted expired object %s", *object.Key)
		callback(ctx, log, *object.Key)
	}
}

func (c *S3Client) ListObjectsByPrefix(ctx context.Context, prefix string) ([]string, error) {
	log := logutil.FromContext(ctx, c.log)
	var objects []string
	log.Infof("Listing objects by with prefix %s", prefix)
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

func (c *S3Client) UploadISOs(ctx context.Context, openshiftVersion string, haveLatestMinimalTemplate bool) error {
	rhcosImage, err := c.versionsHandler.GetRHCOSImage(openshiftVersion)
	if err != nil {
		return err
	}

	baseIsoObject, err := c.GetBaseIsoObject(openshiftVersion)
	if err != nil {
		return err
	}

	minimalIsoObject, err := c.GetMinimalIsoObjectName(openshiftVersion)
	if err != nil {
		return err
	}

	return c.uploadISOs(ctx, baseIsoObject, minimalIsoObject, rhcosImage, openshiftVersion, haveLatestMinimalTemplate)
}

func (c *S3Client) uploadISOs(ctx context.Context, isoObjectName, minimalIsoObject, isoURL, openshiftVersion string, haveLatestMinimalTemplate bool) error {
	log := logutil.FromContext(ctx, c.log)

	baseExists, err := c.DoesPublicObjectExist(ctx, isoObjectName)
	if err != nil {
		return err
	}

	var minimalExists bool
	if !haveLatestMinimalTemplate {
		// Should update minimal ISO template
		minimalExists = false
	} else {
		minimalExists, err = c.DoesPublicObjectExist(ctx, minimalIsoObject)
		if err != nil {
			return err
		}
	}

	if baseExists && minimalExists {
		return nil
	}

	log.Infof("Starting Base ISO download for %s", isoObjectName)
	baseIsoPath, err := DownloadURLToTemporaryFile(isoURL)
	if err != nil {
		log.Error(err)
		return err
	}
	defer os.Remove(baseIsoPath)

	if !baseExists {
		err = c.UploadFileToPublicBucket(ctx, baseIsoPath, isoObjectName)
		if err != nil {
			return err
		}
		log.Infof("Successfully uploaded object %s", isoObjectName)
	}

	if !minimalExists {
		rootFSURL, err := c.versionsHandler.GetRHCOSRootFS(openshiftVersion)
		if err != nil {
			return err
		}
		if err = CreateAndUploadMinimalIso(ctx, log, baseIsoPath, minimalIsoObject, rootFSURL, c, c.isoEditorFactory); err != nil {
			return err
		}
	}

	return nil
}

func (c *S3Client) GetBaseIsoObject(openshiftVersion string) (string, error) {
	rhcosVersion, err := c.versionsHandler.GetRHCOSVersion(openshiftVersion)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(rhcosObjectTemplate, rhcosVersion), nil
}

func (c *S3Client) GetMinimalIsoObjectName(openshiftVersion string) (string, error) {
	rhcosVersion, err := c.versionsHandler.GetRHCOSVersion(openshiftVersion)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(rhcosMinimalObjectTemplate, rhcosVersion), nil
}
