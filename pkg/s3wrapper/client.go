package s3wrapper

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
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
	"github.com/cavaliercoder/go-cpio"
	"github.com/go-openapi/swag"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/ulikunitz/xz"
)

const awsEndpointSuffix = ".amazonaws.com"
const baseObjectName = "livecd.iso"

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
	GeneratePresignedDownloadURL(ctx context.Context, objectName string, duration time.Duration) (string, error)
	UpdateObjectTimestamp(ctx context.Context, objectName string) (bool, error)
	ExpireObjects(ctx context.Context, prefix string, deleteTime time.Duration, callback func(ctx context.Context, log logrus.FieldLogger, objectName string))
}

var _ API = &S3Client{}

type S3Client struct {
	log     *logrus.Logger
	session *session.Session
	client  s3iface.S3API
	cfg     *Config
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
func NewS3Client(cfg *Config, logger *logrus.Logger) *S3Client {
	awsSession, err := newS3Session(cfg)
	if err != nil {
		return nil
	}

	client := s3.New(awsSession)
	if client == nil {
		return nil
	}

	return &S3Client{client: client, session: awsSession, cfg: cfg, log: logger}
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
	log := logutil.FromContext(ctx, c.log)
	objectName := fmt.Sprintf("%s.iso", objectPrefix)

	// Get info from the ISO's header
	areaOffsetBytes, areaLengthBytes, err := c.getISOHeaderInfo(log, baseObjectName)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch base ISO information")
		log.Error(err)
		return err
	}
	log.Infof("areaOffsetBytes: %d, areaLengthBytes: %d", areaOffsetBytes, areaLengthBytes)

	baseObjectSize, err := c.GetObjectSizeBytes(ctx, baseObjectName)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch base ISO size")
		log.Error(err)
		return err
	}

	multiOut, err := c.client.CreateMultipartUpload(&s3.CreateMultipartUploadInput{Bucket: aws.String(c.cfg.S3Bucket), Key: aws.String(objectName)})
	if err != nil {
		err = errors.Wrapf(err, "Failed to start upload for %s", objectName)
		log.Error(err)
		return err
	}
	uploadID := multiOut.UploadId
	var completedParts []*s3.CompletedPart

	// Copy the bulk of the live ISO, until the ignition area
	completedPart, err := c.uploadPartCopy(log, aws.Int64(1), uploadID, baseObjectName, objectName, 0, areaOffsetBytes-1)
	if err != nil {
		return err
	}
	completedParts = append(completedParts, completedPart)

	// Download the range of the live ISO starting from the ignition area
	getRest, err := c.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(baseObjectName),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", areaOffsetBytes, baseObjectSize)),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch end of live ISO %s", objectName)
		log.Error(err)
		return err
	}
	origContents, err := ioutil.ReadAll(getRest.Body)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch body from end of live ISO %s", objectName)
		log.Error(err)
		return err
	}

	// Compress the ignition config, pad it with zeroes, and upload it
	completedPart, err = c.uploadIgnition(log, aws.Int64(2), uploadID, objectName, ignitionConfig, origContents, areaLengthBytes)
	if err != nil {
		return err
	}
	completedParts = append(completedParts, completedPart)

	_, err = c.client.CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(c.cfg.S3Bucket),
		Key:      aws.String(objectName),
		UploadId: uploadID,
		MultipartUpload: &s3.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to complete upload for %s", objectName)
		log.Error(err)
		return err
	}
	return nil
}

func (c *S3Client) getISOHeaderInfo(log logrus.FieldLogger, baseObjectName string) (int64, int64, error) {
	// Download header of the live ISO (last 24 bytes of the first 32KB)
	getResp, err := c.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(c.cfg.S3Bucket),
		Key:    aws.String(baseObjectName),
		Range:  aws.String("bytes=32744-32768"),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to get header of object %s from bucket %s", baseObjectName, c.cfg.S3Bucket)
		return 0, 0, err
	}
	headerString, err := ioutil.ReadAll(getResp.Body)
	if err != nil {
		log.WithError(err).Errorf("Failed to read header of object %s from bucket %s", baseObjectName, c.cfg.S3Bucket)
		return 0, 0, err
	}

	res := bytes.Compare(headerString[0:8], []byte("coreiso+"))
	if res != 0 {
		err := errors.New("Could not find magic string in object header")
		log.WithError(err).Errorf("Failed to read header of object %s from bucket %s", baseObjectName, c.cfg.S3Bucket)
		return 0, 0, err
	}

	offset := int64(binary.LittleEndian.Uint64(headerString[8:16]))
	length := int64(binary.LittleEndian.Uint64(headerString[16:24]))
	return offset, length, nil
}

func (c *S3Client) uploadPartCopy(log logrus.FieldLogger, partNum *int64, uploadID *string, sourceObjectKey string, destObjectKey string,
	sourceStartBytes int64, sourceEndBytes int64) (*s3.CompletedPart, error) {
	completedPartCopy, err := c.client.UploadPartCopy(&s3.UploadPartCopyInput{
		Bucket:          aws.String(c.cfg.S3Bucket),
		Key:             aws.String(destObjectKey),
		CopySource:      aws.String(fmt.Sprintf("/%s/%s", c.cfg.S3Bucket, sourceObjectKey)),
		CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", sourceStartBytes, sourceEndBytes)),
		PartNumber:      partNum,
		UploadId:        uploadID,
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to copy part %d for file %s", partNum, destObjectKey)
		log.Error(err)
		return nil, err
	}
	return &s3.CompletedPart{ETag: completedPartCopy.CopyPartResult.ETag, PartNumber: partNum}, nil
}

func (c *S3Client) uploadIgnition(log logrus.FieldLogger, partNum *int64, uploadID *string, objectName,
	ignitionConfig string, origContents []byte, areaLengthBytes int64) (*s3.CompletedPart, error) {
	ignitionBytes := []byte(ignitionConfig)

	// Create CPIO archive
	archiveBuffer := new(bytes.Buffer)
	log.Info("Creating CPIO archive")
	cpioWriter := cpio.NewWriter(archiveBuffer)
	if err := cpioWriter.WriteHeader(&cpio.Header{Name: "config.ign", Mode: 0o100_644, Size: int64(len(ignitionBytes))}); err != nil {
		log.WithError(err).Errorf("Failed to write CPIO header")
		return nil, err
	}
	if _, err := cpioWriter.Write(ignitionBytes); err != nil {
		log.WithError(err).Errorf("Failed to write CPIO archive")
		return nil, err
	}
	if err := cpioWriter.Close(); err != nil {
		log.WithError(err).Errorf("Failed to close CPIO archive")
		return nil, err
	}
	log.Info("Created CPIO archive, length %d", len(archiveBuffer.Bytes()))

	// Run xz compression
	compressedBuffer := new(bytes.Buffer)
	log.Info("Creating xz archive")
	xzWriter, err := xz.NewWriter(compressedBuffer)
	if err != nil {
		log.WithError(err).Errorf("Failed to create compression writer")
		return nil, err
	}
	xzWriter.WriterConfig.CheckSum = xz.CRC32
	if _, err = xzWriter.Write(archiveBuffer.Bytes()); err != nil {
		err = errors.Wrapf(err, "Failed to gzip ignition config")
		log.Error(err)
		return nil, err
	}
	if err = xzWriter.Close(); err != nil {
		err = errors.Wrapf(err, "Failed to gzip ignition config")
		log.Error(err)
		return nil, err
	}
	log.Info("Created xz archive, length %d", len(compressedBuffer.Bytes()))

	if int64(len(compressedBuffer.Bytes())) > areaLengthBytes {
		err = errors.New(fmt.Sprintf("Ignition is too long to be embedded (%d > %d)", len(archiveBuffer.Bytes()), areaLengthBytes))
		log.Error(err)
		return nil, err
	}

	copy(origContents, compressedBuffer.Bytes())
	log.Info("Copied compressed archive to original contents")

	contentLength := int64(len(origContents))
	completedPartCopy, err := c.client.UploadPart(&s3.UploadPartInput{
		Bucket:        aws.String(c.cfg.S3Bucket),
		Key:           aws.String(objectName),
		PartNumber:    partNum,
		UploadId:      uploadID,
		Body:          bytes.NewReader(origContents),
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to upload ignition for file %s", objectName)
		log.Error(err)
		return nil, err
	}
	log.Info("Uploaded ignition part")

	return &s3.CompletedPart{ETag: completedPartCopy.ETag, PartNumber: partNum}, nil
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

func (c *S3Client) GeneratePresignedDownloadURL(ctx context.Context, objectName string, duration time.Duration) (string, error) {
	log := logutil.FromContext(ctx, c.log)
	req, _ := c.client.GetObjectRequest(&s3.GetObjectInput{
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
