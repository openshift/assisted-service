package s3wrapper

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/openshift/assisted-service/internal/isoeditor"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const minimumPartSizeBytes = 5 * 1024 * 1024    // 5MB
const copyPartChunkSizeBytes = 64 * 1024 * 1024 // 64MB
const coreISOMagic = "coreiso+"

type ISOUploaderAPI interface {
	UploadISO(ctx context.Context, ignitionConfig, srcObjectName, destObjectName string) error
}

var _ ISOUploaderAPI = &ISOUploader{}

type ISOUploader struct {
	log          logrus.FieldLogger
	s3client     s3iface.S3API
	bucket       string
	publicBucket string
	infoCache    []isoInfo
}

type isoInfo struct {
	etag            string
	baseObjectSize  int64
	areaOffsetBytes int64
	areaLengthBytes int64
}

func NewISOUploader(logger logrus.FieldLogger, s3Client s3iface.S3API, bucket, publicBucket string) *ISOUploader {
	return &ISOUploader{log: logger, s3client: s3Client, bucket: bucket, publicBucket: publicBucket}
}

func (u *ISOUploader) UploadISO(ctx context.Context, ignitionConfig, srcObjectName, destObjectName string) error {
	log := logutil.FromContext(ctx, u.log)
	log.Debugf("Started upload of ISO %s", destObjectName)

	baseISOInfo, origContents, err := u.getISOInfo(srcObjectName, log)
	if err != nil {
		err = errors.Wrapf(err, "Failed to fetch base ISO information")
		log.Error(err)
		return err
	}

	upload := multiUpload{
		ctx:             ctx,
		log:             log,
		uploader:        u,
		isoInfo:         baseISOInfo,
		origContents:    origContents,
		sourceObjectKey: srcObjectName,
		destObjectKey:   destObjectName,
	}
	err = upload.Upload(ignitionConfig)
	if err != nil {
		err = errors.Wrapf(err, "Failed to create ISO %s", destObjectName)
		log.Error(err)
		return err
	}
	return nil
}

func (u *ISOUploader) getISOInfo(baseObjectName string, log logrus.FieldLogger) (*isoInfo, *[]byte, error) {
	var info isoInfo
	var origContents []byte

	// Get ETag from S3
	headResp, err := u.s3client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(u.publicBucket),
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
			Bucket: aws.String(u.publicBucket),
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
		Bucket: aws.String(u.publicBucket),
		Key:    aws.String(baseObjectName),
		Range:  aws.String("bytes=32744-32767"),
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to get header of object %s from bucket %s", baseObjectName, u.publicBucket)
		return
	}
	headerString, err := ioutil.ReadAll(getResp.Body)
	if err != nil {
		log.WithError(err).Errorf("Failed to read header of object %s from bucket %s", baseObjectName, u.publicBucket)
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

type multiUpload struct {
	ctx             context.Context
	log             logrus.FieldLogger
	uploader        *ISOUploader
	isoInfo         *isoInfo
	origContents    *[]byte
	wg              sync.WaitGroup
	mutex           sync.Mutex
	err             error
	uploadID        string
	sourceObjectKey string
	destObjectKey   string
	parts           completedParts
}

type chunk struct {
	partNum          int64
	sourceStartBytes int64
	sourceEndBytes   int64
}

// completedParts is a wrapper to make parts sortable by their part number,
// since S3 required this list to be sent in sorted order.
type completedParts []*s3.CompletedPart

func (a completedParts) Len() int           { return len(a) }
func (a completedParts) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a completedParts) Less(i, j int) bool { return *a[i].PartNumber < *a[j].PartNumber }

func (m *multiUpload) Upload(ignitionConfig string) error {
	m.log.Debugf("Creating multi-part upload of ISO %s", m.destObjectKey)
	multiOut, err := m.uploader.s3client.CreateMultipartUploadWithContext(
		m.ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(m.uploader.bucket), Key: aws.String(m.destObjectKey)})
	if err != nil {
		err = errors.Wrapf(err, "Failed to start upload for %s", m.destObjectKey)
		m.log.Error(err)
		return err
	}
	m.uploadID = *multiOut.UploadId

	m.log.Debugf("Starting goroutines copying chunks for ISO %s", m.destObjectKey)
	maxChunks := int(m.isoInfo.baseObjectSize/copyPartChunkSizeBytes) + 1
	ch := make(chan chunk, maxChunks)
	for i := 0; i < maxChunks; i++ {
		m.wg.Add(1)
		go m.copyChunk(ch)
	}

	m.log.Debugf("Providing work for goroutines copying chunks for ISO %s", m.destObjectKey)
	embeddedAreaPartNum := m.generateWorkForRange(ch, int64(1), 0, m.isoInfo.areaOffsetBytes-1)
	m.generateWorkForRange(ch, embeddedAreaPartNum+1, m.isoInfo.areaOffsetBytes+minimumPartSizeBytes, m.isoInfo.baseObjectSize-1)
	close(ch)

	m.log.Debugf("Uploading embedded area (compressed ignition config) for ISO %s", m.destObjectKey)
	err = m.uploadIgnition(m.log, embeddedAreaPartNum, ignitionConfig)
	if err != nil {
		m.log.Error(err)
		m.seterr(err)
	} else {
		m.log.Debugf("Completed upload of embedded area for ISO %s, waiting for async copies", m.destObjectKey)
	}

	// We now finished sending the chunks to copy as well as uploading the embedded area, wait for copies to finish
	m.wg.Wait()
	m.complete()

	if err := m.geterr(); err != nil {
		return err
	}
	m.log.Debugf("Completed upload of ISO %s", m.destObjectKey)
	return nil
}

func (m *multiUpload) copyChunk(ch chan chunk) {
	defer m.wg.Done()
	for {
		data, ok := <-ch

		if !ok {
			break
		}

		if m.geterr() == nil {
			if err := m.uploadPartCopy(data); err != nil {
				m.seterr(err)
			}
		}
	}
}

func (m *multiUpload) generateWorkForRange(ch chan chunk, partNum int64, startOffsetBytes int64, endOffsetBytes int64) int64 {
	var (
		currentChunkEnd int64
		nextChunkStart  int64
		offsetCounter   = startOffsetBytes
		partCounter     = partNum
	)
	for m.geterr() == nil && offsetCounter < endOffsetBytes {
		currentChunkEnd = offsetCounter + copyPartChunkSizeBytes - 1
		nextChunkStart = currentChunkEnd + 1
		// We need to check two conditions which will cause this to be the last part:
		// 1. If we're at the end of the specified range, we need to copy less than the chunk size
		// 2. If the next chunk will be less than 5MB, we need to fold it into this one
		if (currentChunkEnd > endOffsetBytes-1) || (nextChunkStart+minimumPartSizeBytes-1 > endOffsetBytes) {
			currentChunkEnd = endOffsetBytes
			nextChunkStart = endOffsetBytes
		}

		ch <- chunk{partNum: partCounter, sourceStartBytes: offsetCounter, sourceEndBytes: currentChunkEnd}
		offsetCounter = nextChunkStart
		partCounter++
	}
	return partCounter
}

func (m *multiUpload) geterr() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.err
}

func (m *multiUpload) seterr(e error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.err = e
}

func (m *multiUpload) appendCompletedPart(completed *s3.CompletedPart) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.parts = append(m.parts, completed)
}

func (m *multiUpload) complete() {
	err := m.geterr()
	if err != nil {
		m.fail()
		return
	}

	// Parts must be sorted in PartNumber order.
	sort.Sort(m.parts)

	params := &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(m.uploader.bucket),
		Key:             aws.String(m.destObjectKey),
		UploadId:        aws.String(m.uploadID),
		MultipartUpload: &s3.CompletedMultipartUpload{Parts: m.parts},
	}
	_, err = m.uploader.s3client.CompleteMultipartUploadWithContext(m.ctx, params)
	if err != nil {
		err = errors.Wrapf(err, "Failed to complete upload for %s", m.destObjectKey)
		m.log.Error(err)
		m.seterr(err)
		m.fail()
	}
}

func (m *multiUpload) fail() {
	// using new context because m.ctx may be canceled and it will fail the operation
	_, err := m.uploader.s3client.AbortMultipartUploadWithContext(context.Background(), &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(m.uploader.bucket),
		Key:      aws.String(m.destObjectKey),
		UploadId: aws.String(m.uploadID),
	})
	if err != nil {
		m.log.WithError(err).Warnf("Failed to abort failed multipart upload with ID %s", m.uploadID)
	}
}

func (m *multiUpload) uploadPartCopy(c chunk) error {
	completedPartCopy, err := m.uploader.s3client.UploadPartCopyWithContext(m.ctx, &s3.UploadPartCopyInput{
		Bucket:          aws.String(m.uploader.bucket),
		Key:             aws.String(m.destObjectKey),
		CopySource:      aws.String(fmt.Sprintf("/%s/%s", m.uploader.publicBucket, m.sourceObjectKey)),
		CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", c.sourceStartBytes, c.sourceEndBytes)),
		PartNumber:      aws.Int64(c.partNum),
		UploadId:        aws.String(m.uploadID),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to copy part %d for file %s", c.partNum, m.destObjectKey)
		m.log.Error(err)
		return err
	}
	m.appendCompletedPart(&s3.CompletedPart{ETag: completedPartCopy.CopyPartResult.ETag, PartNumber: aws.Int64(c.partNum)})
	return nil
}

func (m *multiUpload) uploadIgnition(log logrus.FieldLogger, partNum int64, ignitionConfig string) error {
	imageBytes, err := isoeditor.IgnitionImageArchive(ignitionConfig)
	if err != nil {
		m.log.Error(err)
		return err
	}

	if int64(len(imageBytes)) > m.isoInfo.areaLengthBytes {
		err = errors.New(fmt.Sprintf("Ignition is too long to be embedded (%d > %d)", len(imageBytes), m.isoInfo.areaLengthBytes))
		m.log.Error(err)
		return err
	}

	copy(*m.origContents, imageBytes)

	contentLength := int64(len(*m.origContents))
	completedPartCopy, err := m.uploader.s3client.UploadPart(&s3.UploadPartInput{
		Bucket:        aws.String(m.uploader.bucket),
		Key:           aws.String(m.destObjectKey),
		PartNumber:    aws.Int64(partNum),
		UploadId:      aws.String(m.uploadID),
		Body:          bytes.NewReader(*m.origContents),
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		err = errors.Wrapf(err, "Failed to upload ignition for file %s", m.destObjectKey)
		m.log.Error(err)
		return err
	}

	m.appendCompletedPart(&s3.CompletedPart{ETag: completedPartCopy.ETag, PartNumber: aws.Int64(partNum)})
	return nil
}
