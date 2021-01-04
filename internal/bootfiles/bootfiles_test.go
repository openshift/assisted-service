package bootfiles_test

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/go-openapi/runtime/middleware"
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

var (
	defaultURL     = "http://foo.com/file"
	defaultBaseIso = "baseiso"
)

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

	downloadBootFiles := func(isAws bool, fileType string) middleware.Responder {
		mockS3Client.EXPECT().IsAwsS3().Return(isAws).Times(1)
		mockS3Client.EXPECT().GetBaseIsoObject(common.DefaultTestOpenShiftVersion).Return(defaultBaseIso).Times(1)

		if isAws {
			mockS3Client.EXPECT().GetS3BootFileURL(defaultBaseIso, fileType).Return(defaultURL).Times(1)
		} else {
			mockS3Client.EXPECT().DownloadBootFile(ctx, defaultBaseIso, fileType).Times(1)
		}

		return bootfilesAPI.DownloadBootFiles(ctx, operations.DownloadBootFilesParams{
			FileType: fileType, OpenshiftVersion: common.DefaultTestOpenShiftVersion,
		})
	}

	Context("DownloadBootFiles", func() {
		It("download initrd aws", func() {
			response := downloadBootFiles(true, "initrd.img")
			Expect(response).To(BeAssignableToTypeOf(&operations.DownloadBootFilesTemporaryRedirect{}))
			responsePayload := response.(*operations.DownloadBootFilesTemporaryRedirect)
			Expect(responsePayload.Location).To(Equal(defaultURL))
			//Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0))))
		})
		It("download vmlinuz onprem", func() {
			response := downloadBootFiles(false, "vmlinuz")
			Expect(response).Should(BeAssignableToTypeOf(filemiddleware.NewResponder(nil, "", int64(0))))
		})
		It("download failed", func() {
			fileType := "vmlinuz"
			baseIso := "livecd.iso"
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetBaseIsoObject(common.DefaultTestOpenShiftVersion).Return(baseIso)
			mockS3Client.EXPECT().DownloadBootFile(ctx, baseIso, fileType).Return(nil, "", int64(0), errors.New("Whoops"))
			response := bootfilesAPI.DownloadBootFiles(ctx, operations.DownloadBootFilesParams{
				FileType: fileType, OpenshiftVersion: common.DefaultTestOpenShiftVersion,
			})
			apiErr, ok := response.(*common.ApiErrorResponse)
			Expect(ok).Should(BeTrue())
			Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusInternalServerError)))
		})
	})
})
