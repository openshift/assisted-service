package assistedserviceiso

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/golang/mock/gomock"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi/operations/assisted_service_iso"
	"github.com/pkg/errors"
)

var _ = Describe("AssistedServiceISO", func() {
	var (
		ctrl                 *gomock.Controller
		mockS3Client         *s3wrapper.MockAPI
		mockSecretValidator  *validations.MockPullSecretValidator
		api                  *assistedServiceISOApi
		cfg                  Config
		ctx                  = context.Background()
		srcIsoName           = "rhcos"
		destIsoName          = fmt.Sprintf("%s%s", imgexpirer.AssistedServiceLiveISOPrefix, "admin")
		isoNameWithExtension = fmt.Sprintf("%s%s", destIsoName, ".iso")
		pullSecret           = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}" // #nosec
		sshPublicKey         = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDgj9Pc6dmIAZvxvC1q4K05lUqd/Qy73JEGP/THZEdlLif825SPyMe9NGe8UxNiS4AvYJoLcplMVztQjInVf6s3C0EtlvyrfzdoCCONNBtgItU0gxG+GxneNJs/MKhlUBh6QWg52cBwiaTIxrGlbM/qLfzSX6k5WtZV/yH1TVVrFOpDxtOfR5RZ/GmI97pJIOhxEdw9aT3FydbFtuNwTyNxo0YGMk6Mp89qlUx20u4aK1HXn67I3+2xtpzPSiH6TwRPX3vb/qdWJ4/YaKOHwf/FnIg3FXQXVxRCBijDF0cCUmKWcdrs59JopGMFKDXwHHCdfMjtnfBvA/WOlBs0NKpoFIEuufL3gBuahBRvMKnOXD1gwD8WkaOa+B5BxutZ+/zXAPX3faXRdMGPfHRDam+rNR8KkbYl+3Y2C/W1APMLopLt5kKit64E4rHTwbYwB1Si770O+I/KTcAwnRo1j0K9m7ahz2YXK3fiqieh7awhkiosTsDHLAZDs+YTi9tfBQ8= me@tester"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockSecretValidator = validations.NewMockPullSecretValidator(ctrl)
		cfg.IgnitionConfigBaseFilename = "../../config/onprem-iso-config.ign"
		logger := common.GetTestLog()
		api = NewAssistedServiceISOApi(mockS3Client, auth.NewNoneAuthenticator(logger), logger, mockSecretValidator, cfg)
	})

	uploadIsoSuccess := func() {
		mockS3Client.EXPECT().IsAwsS3().Return(false).Times(1)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockS3Client.EXPECT().GetBaseIsoObject(common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture).Return(srcIsoName, nil).Times(1)
		mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), srcIsoName, destIsoName).Times(1)
	}

	Context("CreateISOAndUploadToS3", func() {
		BeforeEach(func() {
			uploadIsoSuccess()
		})

		It("success", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey:     sshPublicKey,
				PullSecret:       pullSecret,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}
			generateReply := api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3Created()))
		})

		It("failure when pull-secret is missing", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey: sshPublicKey,
				PullSecret:   "",
			}
			generateReply := api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3BadRequest()))
		})

		It("failure when pull-secret is wrong format", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("bad pull secret")).Times(1)
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey: sshPublicKey,
				PullSecret:   "not-correct-format",
			}
			generateReply := api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3BadRequest()))
		})

		It("ssh public key is not required and request should be successful if it is missing", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey:     "",
				PullSecret:       pullSecret,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}
			generateReply := api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3Created()))
		})

		It("multiple creates", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey:     sshPublicKey,
				PullSecret:       pullSecret,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}
			// first
			generateReply := api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3Created()))
			// second
			uploadIsoSuccess()
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			generateReply = api.CreateISOAndUploadToS3(ctx, assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(assisted_service_iso.NewCreateISOAndUploadToS3Created()))
		})
	})

	Context("DownloadISO", func() {
		It("success", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().Download(ctx, isoNameWithExtension)
			generateReply := api.DownloadISO(ctx, assisted_service_iso.DownloadISOParams{})
			Expect(generateReply).Should(Equal(filemiddleware.NewResponder(assisted_service_iso.NewDownloadISOOK().WithPayload(nil), isoNameWithExtension, 0)))
		})

		It("download from s3 failed", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			// internal system error
			mockS3Client.EXPECT().Download(ctx, isoNameWithExtension).Return(nil, int64(0), errors.Errorf("internal system error"))
			generateReply := api.DownloadISO(ctx, assisted_service_iso.DownloadISOParams{})
			Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusInternalServerError)))

			// ISO hasn't been generated and not found in object store
			mockS3Client.EXPECT().Download(ctx, isoNameWithExtension).Return(nil, int64(0), errors.Errorf("NotFound 404"))
			generateReply = api.DownloadISO(ctx, assisted_service_iso.DownloadISOParams{})
			Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})
	})

	Context("GetPresignedForAssistedServiceISO", func() {
		It("backend not aws", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			generateReply := api.GetPresignedForAssistedServiceISO(ctx, assisted_service_iso.GetPresignedForAssistedServiceISOParams{})
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
		})

		It("ISO not found", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, destIsoName, isoNameWithExtension, gomock.Any()).Return("", errors.Errorf("NotFound 404"))
			generateReply := api.GetPresignedForAssistedServiceISO(ctx, assisted_service_iso.GetPresignedForAssistedServiceISOParams{})

			Expect(generateReply).Should(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})

		It("happy flow", func() {
			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, destIsoName, isoNameWithExtension, gomock.Any()).Return("url", nil)
			generateReply := api.GetPresignedForAssistedServiceISO(ctx, assisted_service_iso.GetPresignedForAssistedServiceISOParams{})

			Expect(generateReply).Should(BeAssignableToTypeOf(&assisted_service_iso.GetPresignedForAssistedServiceISOOK{}))
			replyPayload := generateReply.(*assisted_service_iso.GetPresignedForAssistedServiceISOOK).Payload
			Expect(*replyPayload.URL).Should(Equal("url"))
		})

	})

})

func TestAssistedServiceISO(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AssistedServiceISO test Suite")
}
