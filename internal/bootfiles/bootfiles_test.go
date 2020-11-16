package bootfiles_test

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	_ "github.com/jinzhu/gorm/dialects/postgres"

	"github.com/openshift/assisted-service/internal/bootfiles"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/bootfiles"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "bootfiles_test")
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

var _ = Describe("BootFilesTests", func() {
	var (
		bootfilesAPI *bootfiles.BootFiles
		ctx          = context.Background()
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)

		bootfilesAPI = bootfiles.NewBootFilesAPI(getTestLog(), mockS3Client)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("DownloadBootFiles", func() {
		It("download initrd aws", func() {
			fileType := "initrd.img"
			url := "http://foo.com/file"
			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().GetS3BootFileURL(fileType).Return(url)
			response := bootfilesAPI.DownloadBootFiles(ctx, operations.DownloadBootFilesParams{FileType: fileType})
			Expect(response).To(BeAssignableToTypeOf(&operations.DownloadBootFilesTemporaryRedirect{}))
			responsePayload := response.(*operations.DownloadBootFilesTemporaryRedirect)
			Expect(responsePayload.Location).To(Equal(url))
			//Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0))))
		})
		It("download vmlinuz onprem", func() {
			fileType := "vmlinuz"
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().DownloadBootFile(ctx, fileType)
			response := bootfilesAPI.DownloadBootFiles(ctx, operations.DownloadBootFilesParams{FileType: fileType})
			Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0))))
		})
		It("download failed", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().DownloadBootFile(ctx, "vmlinuz").Return(nil, "", int64(0), errors.New("Whoops"))
			response := bootfilesAPI.DownloadBootFiles(ctx, operations.DownloadBootFilesParams{FileType: "vmlinuz"})
			apiErr, ok := response.(*common.ApiErrorResponse)
			Expect(ok).Should(BeTrue())
			Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusInternalServerError)))
		})
	})
})
