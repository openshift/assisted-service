package s3wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Util")
}

const (
	defaultTestOpenShiftVersion = "4.6"
	defaultTestRhcosURL         = "rhcosURL"
	defaultTestServiceBaseURL   = "http://1.1.1.1:6000"
)

var (
	defaultTestRhcosVersion       = fmt.Sprintf("%s.00.000000000000-0", strings.ReplaceAll(defaultTestOpenShiftVersion, ".", ""))
	defaultTestRhcosObject        = fmt.Sprintf("rhcos-%s.iso", defaultTestRhcosVersion)
	defaultTestRhcosObjectMinimal = fmt.Sprintf("rhcos-%s-minimal.iso", defaultTestRhcosVersion)
)

var _ = Describe("FixEndpointURL", func() {
	It("returns the original string with a valid http URL", func() {
		endpoint := "http://example.com/stuff"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("http://example.com/stuff"))
	})

	It("returns the original string with a valid https URL", func() {
		endpoint := "https://example.com/stuff"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("https://example.com/stuff"))
	})

	It("prefixes an invalid endpoint with http:// when S3_USE_SSL is not set", func() {
		endpoint := "example.com"
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("http://example.com"))
	})

	It("prefixes and invalid endpoint with https:// when S3_USE_SSL is \"true\"", func() {
		endpoint := "example.com"
		os.Setenv("S3_USE_SSL", "true")
		defer os.Unsetenv("S3_USE_SSL")
		result, err := FixEndpointURL(endpoint)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal("https://example.com"))
	})

	It("returns an error when a prefix does not produce a valid URL", func() {
		endpoint := ":example.com"
		result, err := FixEndpointURL(endpoint)
		Expect(result).To(Equal(""))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("UploadBootFiles", func() {
	var (
		ctx          = context.Background()
		log          logrus.FieldLogger
		ctrl         *gomock.Controller
		mockS3Client *MockAPI
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = NewMockAPI(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("all files already uploaded", func() {
		mockS3Client.EXPECT().DoesPublicObjectExist(ctx, BootFileTypeToObjectName(defaultTestRhcosObject, "initrd.img")).Return(true, nil)
		mockS3Client.EXPECT().DoesPublicObjectExist(ctx, BootFileTypeToObjectName(defaultTestRhcosObject, "rootfs.img")).Return(true, nil)
		mockS3Client.EXPECT().DoesPublicObjectExist(ctx, BootFileTypeToObjectName(defaultTestRhcosObject, "vmlinuz")).Return(true, nil)
		err := ExtractBootFilesFromISOAndUpload(ctx, log, "/unused/file", defaultTestRhcosObject, defaultTestRhcosURL, mockS3Client)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("HaveLatestMinimalTemplate", func() {
	var (
		ctx            = context.Background()
		log            logrus.FieldLogger
		ctrl           *gomock.Controller
		isoUploader    *ISOUploader
		client         *S3Client
		mockAPI        *MockS3API
		publicMockAPI  *MockS3API
		uploader       *MockUploaderAPI
		publicUploader *MockUploaderAPI
		mockVersions   *versions.MockHandler

		bucket       string
		publicBucket string
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = NewMockS3API(ctrl)
		publicMockAPI = NewMockS3API(ctrl)
		uploader = NewMockUploaderAPI(ctrl)
		publicUploader = NewMockUploaderAPI(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		bucket = "test"
		publicBucket = "pub-test"
		editorFactory := isoeditor.NewFactory(isoeditor.Config{ConcurrentEdits: 10}, nil)
		cfg := Config{S3Bucket: bucket, PublicS3Bucket: publicBucket}
		isoUploader = &ISOUploader{log: log, bucket: bucket, publicBucket: publicBucket, s3client: mockAPI}
		client = &S3Client{log: log, session: nil, client: mockAPI, publicClient: publicMockAPI, uploader: uploader,
			publicUploader: publicUploader, cfg: &cfg, isoUploader: isoUploader, versionsHandler: mockVersions,
			isoEditorFactory: editorFactory}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("latest version already exists", func() {
		mockTemplatesVersions(mockAPI, bucket, minimalTemplatesVersionLatest)
		latestExists := HaveLatestMinimalTemplate(ctx, log, client)
		Expect(latestExists).To(Equal(true))
	})

	It("latest version missing", func() {
		mockTemplatesVersions(mockAPI, bucket, minimalTemplatesVersionLatest-1)
		latestExists := HaveLatestMinimalTemplate(ctx, log, client)
		Expect(latestExists).To(Equal(false))
	})

	It("version file missing", func() {
		mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{
			Bucket: &bucket,
			Key:    aws.String(minimalTemplatesVersionFileName)}).
			Return(nil, awserr.New("NotFound", "NotFound", errors.New("NotFound")))

		latestExists := HaveLatestMinimalTemplate(ctx, log, client)
		Expect(latestExists).To(Equal(false))
	})
})

func getMockTemplatesVersion(version int) ([]byte, error) {
	versionInBucket := &templatesVersion{
		Version: version,
	}
	b, err := json.Marshal(versionInBucket)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func mockTemplatesVersions(mockAPI *MockS3API, bucket string, version int) {
	templatesVersions, err := getMockTemplatesVersion(version)
	Expect(err).ToNot(HaveOccurred())
	mockAPI.EXPECT().HeadObject(&s3.HeadObjectInput{
		Bucket: &bucket,
		Key:    aws.String(minimalTemplatesVersionFileName)}).
		Return(&s3.HeadObjectOutput{
			ETag:          aws.String("etag"),
			ContentLength: aws.Int64(int64(len(templatesVersions)))}, nil)
	mockAPI.EXPECT().GetObject(&s3.GetObjectInput{
		Bucket: &bucket,
		Key:    aws.String(minimalTemplatesVersionFileName)}).
		Return(&s3.GetObjectOutput{
			Body: ioutil.NopCloser(bytes.NewReader(templatesVersions))}, nil)
}
