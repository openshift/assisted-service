package s3wrapper

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/cavaliercoder/go-cpio"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/service/s3"
)

const minimumPartSizeBytes = 5 * 1024 * 1024 // 5MB
const coreISOMagic = "coreiso+"

type ISOUploaderAPI interface {
	UploadISO(ctx context.Context, ignitionConfig, objectName string) error
}

var _ ISOUploaderAPI = &ISOUploader{}

type ISOUploader struct {
	log       logrus.FieldLogger
	s3client  s3iface.S3API
	bucket    string
	infoCache []isoInfo
}

type isoInfo struct {
	etag            string
	baseObjectSize  int64
	areaOffsetBytes int64
	areaLengthBytes int64
}

func NewISOUploader(logger logrus.FieldLogger, s3Client s3iface.S3API, bucket string) *ISOUploader {
	return &ISOUploader{log: logger, s3client: s3Client, bucket: bucket}
}

func (u *ISOUploader) UploadISO(ctx context.Context, ignitionConfig, objectName string) error {
	log := logutil.FromContext(ctx, u.log)
	log.Debugf("Started upload of ISO %s", objectName)

	baseISOInfo, origContents, err := u.getISOInfo(BaseObjectName, log)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch base ISO information")
		log.Error(err)
		return err
	}

	log.Debugf("Creating multi-part upload of ISO %s", objectName)
	multiOut, err := u.s3client.CreateMultipartUpload(&s3.CreateMultipartUploadInput{Bucket: aws.String(u.bucket), Key: aws.String(objectName)})
	if err != nil {
		err = errors.Wrapf(err, "Failed to start upload for %s", objectName)
		log.Error(err)
		return err
	}
	uploadID := multiOut.UploadId
	var completedParts []*s3.CompletedPart

	defer func() {
		if err != nil {
			_, abortErr := u.s3client.AbortMultipartUpload(&s3.AbortMultipartUploadInput{UploadId: uploadID, Bucket: aws.String(u.bucket), Key: aws.String(objectName)})
			if abortErr != nil {
				log.WithError(abortErr).Warnf("Failed to abort failed multipart upload with ID %s", *uploadID)
			}
		}
	}()

	// First part: copy the first part of the live ISO, until the embedded area
	log.Debugf("Creating part 1 of multi-part upload of ISO %s", objectName)
	completedPart, err := u.uploadPartCopy(log, 1, uploadID, BaseObjectName, objectName, 0, baseISOInfo.areaOffsetBytes-1)
	if err != nil {
		return err
	}
	completedParts = append(completedParts, completedPart)

	// Second part: The embedded area containing the compressed ignition config.
	log.Debugf("Uploading part 2 (ignition) of multi-part upload of ISO %s", objectName)
	completedPart, err = u.uploadIgnition(log, 2, uploadID, objectName, ignitionConfig, *origContents, baseISOInfo.areaLengthBytes)
	if err != nil {
		return err
	}
	completedParts = append(completedParts, completedPart)

	// Third part: copy the last part of the live ISO, after the embedded area
	log.Debugf("Creating part 3 of multi-part upload of ISO %s", objectName)
	completedPart, err = u.uploadPartCopy(log, 3, uploadID, BaseObjectName, objectName, baseISOInfo.areaOffsetBytes+minimumPartSizeBytes, baseISOInfo.baseObjectSize-1)
	if err != nil {
		return err
	}
	completedParts = append(completedParts, completedPart)

	log.Debugf("Completing multi-part upload of ISO %s", objectName)
	_, err = u.s3client.CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(u.bucket),
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
	log.Debugf("Completed upload of ISO %s", objectName)
	return nil
}

func (u *ISOUploader) getISOInfo(baseObjectName string, log logrus.FieldLogger) (*isoInfo, *[]byte, error) {
	var info isoInfo
	var origContents []byte

	// Get ETag from S3
	headResp, err := u.s3client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(baseObjectName),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to fetch metadata for base object %s", baseObjectName)
		return nil, nil, err
	}

	// See if the ISO info is cached
	var found *isoInfo
	for i := range u.infoCache {
		if u.infoCache[i].etag == *headResp.ETag {
			found = &u.infoCache[i]
		}
	}
	cachePath := filepath.Join("/tmp", *headResp.ETag)

	if found == nil {
		// Add to cache if not found
		log.Infof("Did not find ISO info for %s in cache, will add", baseObjectName)
		var offset, length int64
		offset, length, err = u.getISOHeaderInfo(log, baseObjectName, *headResp.ContentLength)
		if err != nil {
			err = errors.Wrapf(err, "Failed to get base ISO info for %s from S3", baseObjectName)
			log.Error(err)
			return nil, nil, err
		}
		info.etag = *headResp.ETag
		info.baseObjectSize = *headResp.ContentLength
		info.areaLengthBytes = length
		info.areaOffsetBytes = offset

		var getRest *s3.GetObjectOutput
		getRest, err = u.s3client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(u.bucket),
			Key:    aws.String(baseObjectName),
			Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+minimumPartSizeBytes-1)),
		})
		if err != nil {
			err = errors.Wrapf(err, "Failed to fetch embedded area of live ISO %s", baseObjectName)
			log.Error(err)
			return nil, nil, err
		}
		origContents, err = ioutil.ReadAll(getRest.Body)
		if err != nil {
			err = errors.Wrapf(err, "Failed to fetch body from embedded area of live ISO %s", baseObjectName)
			log.Error(err)
			return nil, nil, err
		}

		err = ioutil.WriteFile(cachePath, origContents, 0600)
		if err != nil {
			// If we fail here, continue without adding to cache rather than failing the entire operation
			log.WithError(err).Errorf("Failed to cache embedded area to file %s", cachePath)
		} else {
			u.infoCache = append(u.infoCache, info)
		}
	} else {
		// Return from cache
		log.Debugf("Found ISO info for %s in cache", baseObjectName)
		origContents, err = ioutil.ReadFile(cachePath)
		if err != nil {
			err = errors.Wrapf(err, "Failed to fetch embedded area of live ISO %s from disk cache", baseObjectName)
			log.Error(err)
			return nil, nil, err
		}
		info = *found
	}
	return &info, &origContents, nil
}

func (u *ISOUploader) getISOHeaderInfo(log logrus.FieldLogger, baseObjectName string, baseObjectSize int64) (offset int64, length int64, err error) {
	// Download header of the live ISO (last 24 bytes of the first 32KB)
	getResp, err := u.s3client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(baseObjectName),
		Range:  aws.String("bytes=32744-32767"),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to get header of object %s from bucket %s", baseObjectName, u.bucket)
		return
	}
	headerString, err := ioutil.ReadAll(getResp.Body)
	if err != nil {
		log.WithError(err).Errorf("Failed to read header of object %s from bucket %s", baseObjectName, u.bucket)
		return
	}

	res := bytes.Compare(headerString[0:8], []byte(coreISOMagic))
	if res != 0 {
		err = errors.New(fmt.Sprintf("Could not find magic string in object header (%s)", headerString[0:8]))
		return
	}

	offset = int64(binary.LittleEndian.Uint64(headerString[8:16]))
	length = int64(binary.LittleEndian.Uint64(headerString[16:24]))

	// For now we assume that the embedded area is less than 5MB, which is the minimum S3 part size
	if length > int64(minimumPartSizeBytes) {
		err = errors.New("ISO embedded area is larger than what is currently supported")
		return
	}

	if offset+minimumPartSizeBytes > baseObjectSize {
		err = errors.New("Embedded area is too close to the end of the file, which is currently not handled")
		return
	}
	return
}

func (u *ISOUploader) uploadPartCopy(log logrus.FieldLogger, partNum int64, uploadID *string, sourceObjectKey string, destObjectKey string,
	sourceStartBytes int64, sourceEndBytes int64) (*s3.CompletedPart, error) {
	completedPartCopy, err := u.s3client.UploadPartCopy(&s3.UploadPartCopyInput{
		Bucket:          aws.String(u.bucket),
		Key:             aws.String(destObjectKey),
		CopySource:      aws.String(fmt.Sprintf("/%s/%s", u.bucket, sourceObjectKey)),
		CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", sourceStartBytes, sourceEndBytes)),
		PartNumber:      aws.Int64(partNum),
		UploadId:        uploadID,
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to copy part %d for file %s", partNum, destObjectKey)
		log.Error(err)
		return nil, err
	}
	return &s3.CompletedPart{ETag: completedPartCopy.CopyPartResult.ETag, PartNumber: aws.Int64(partNum)}, nil
}

func (u *ISOUploader) uploadIgnition(log logrus.FieldLogger, partNum int64, uploadID *string, objectName,
	ignitionConfig string, origContents []byte, areaLengthBytes int64) (*s3.CompletedPart, error) {
	ignitionBytes := []byte(ignitionConfig)

	// Create CPIO archive
	archiveBuffer := new(bytes.Buffer)
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

	// Run gzip compression
	compressedBuffer := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(compressedBuffer)
	if _, err := gzipWriter.Write(archiveBuffer.Bytes()); err != nil {
		err = errors.Wrapf(err, "Failed to gzip ignition config")
		log.Error(err)
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		err = errors.Wrapf(err, "Failed to gzip ignition config")
		log.Error(err)
		return nil, err
	}

	if int64(len(compressedBuffer.Bytes())) > areaLengthBytes {
		err := errors.New(fmt.Sprintf("Ignition is too long to be embedded (%d > %d)", len(compressedBuffer.Bytes()), areaLengthBytes))
		log.Error(err)
		return nil, err
	}

	copy(origContents, compressedBuffer.Bytes())

	contentLength := int64(len(origContents))
	completedPartCopy, err := u.s3client.UploadPart(&s3.UploadPartInput{
		Bucket:        aws.String(u.bucket),
		Key:           aws.String(objectName),
		PartNumber:    aws.Int64(partNum),
		UploadId:      uploadID,
		Body:          bytes.NewReader(origContents),
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to upload ignition for file %s", objectName)
		log.Error(err)
		return nil, err
	}

	return &s3.CompletedPart{ETag: completedPartCopy.ETag, PartNumber: aws.Int64(partNum)}, nil
}
