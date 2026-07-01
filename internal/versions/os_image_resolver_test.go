package versions

import (
	"context"
	"fmt"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	gomock "go.uber.org/mock/gomock"
)

const testPullSecret = "test-pull-secret"

var _ = Describe("OsImageResolver", func() {
	var (
		ctrl            *gomock.Controller
		mockRelease     *oc.MockRelease
		mockHandler     *MockHandler
		mockOSImages    *MockOSImages
		resolver        OsImageResolver
		ctx             context.Context
		releaseImageURL string
		releaseImage    *models.ReleaseImage
		osImage         *models.OsImage
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		mockHandler = NewMockHandler(ctrl)
		mockOSImages = NewMockOSImages(ctrl)
		resolver = NewOsImageResolver(logrus.New(), mockRelease, mockHandler, mockOSImages, "")
		ctx = context.Background()
		releaseImageURL = common.TestDefaultConfig.ReleaseImageUrl
		releaseImage = &models.ReleaseImage{
			URL:              swag.String(releaseImageURL),
			OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
		}
		osImage = common.TestDefaultConfig.OsImage
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("GetOsImageForRelease", func() {
		It("returns OS image resolved by default RHCOS version", func() {
			mockRelease.EXPECT().GetDefaultRhcosVersion(
				gomock.Any(), releaseImageURL, "", testPullSecret, common.TestDefaultConfig.CPUArchitecture,
			).Return(*common.TestDefaultConfig.OsImage.Version, nil).Times(1)
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForRelease(
				releaseImage,
				common.TestDefaultConfig.CPUArchitecture,
				testPullSecret,
			)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})

		It("falls back to OpenShift version when RHCOS extraction fails", func() {
			mockRelease.EXPECT().GetDefaultRhcosVersion(
				gomock.Any(), releaseImageURL, "", testPullSecret, common.TestDefaultConfig.CPUArchitecture,
			).Return("", fmt.Errorf("extraction failed")).Times(1)
			mockOSImages.EXPECT().GetOsImageByOpenshiftVersion(
				common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForRelease(
				releaseImage,
				common.TestDefaultConfig.CPUArchitecture,
				testPullSecret,
			)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})

		It("falls back when RHCOS version has no matching OS image", func() {
			mockRelease.EXPECT().GetDefaultRhcosVersion(
				gomock.Any(), releaseImageURL, "", testPullSecret, common.TestDefaultConfig.CPUArchitecture,
			).Return(*common.TestDefaultConfig.OsImage.Version, nil).Times(1)
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(nil, fmt.Errorf("not found")).Times(1)
			mockOSImages.EXPECT().GetOsImageByOpenshiftVersion(
				common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForRelease(
				releaseImage,
				common.TestDefaultConfig.CPUArchitecture,
				testPullSecret,
			)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})
	})

	Context("GetOsImageForVersion", func() {
		It("returns OS image resolved by RHCOS version", func() {
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForVersion(
				ctx,
				*common.TestDefaultConfig.OsImage.Version,
				common.TestDefaultConfig.CPUArchitecture,
				testPullSecret,
			)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})

		It("resolves an OpenShift version via the release image", func() {
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture,
			).Return(nil, fmt.Errorf("not found")).Times(1)
			mockHandler.EXPECT().GetReleaseImage(
				ctx, common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture, testPullSecret,
			).Return(releaseImage, nil).Times(1)
			mockRelease.EXPECT().GetDefaultRhcosVersion(
				gomock.Any(), releaseImageURL, "", testPullSecret, common.TestDefaultConfig.CPUArchitecture,
			).Return(*common.TestDefaultConfig.OsImage.Version, nil).Times(1)
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForVersion(
				ctx,
				common.TestDefaultConfig.OpenShiftVersion,
				common.TestDefaultConfig.CPUArchitecture,
				testPullSecret,
			)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})
	})

	Context("GetOsImageForInfraEnv", func() {
		It("resolves an explicit RHCOS version as-is", func() {
			infraEnv := &common.InfraEnv{
				InfraEnv: models.InfraEnv{
					OpenshiftVersion: *common.TestDefaultConfig.OsImage.Version,
					CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
				},
			}

			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForInfraEnv(ctx, infraEnv)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})

		It("resolves an OpenShift version to the default RHCOS version from the release image", func() {
			infraEnv := &common.InfraEnv{
				InfraEnv: models.InfraEnv{
					OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
					CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
				},
				PullSecret: testPullSecret,
			}

			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture,
			).Return(nil, fmt.Errorf("not found")).Times(1)
			mockHandler.EXPECT().GetReleaseImage(
				ctx, common.TestDefaultConfig.OpenShiftVersion, common.TestDefaultConfig.CPUArchitecture, testPullSecret,
			).Return(releaseImage, nil).Times(1)
			mockRelease.EXPECT().GetDefaultRhcosVersion(
				gomock.Any(), releaseImageURL, "", testPullSecret, common.TestDefaultConfig.CPUArchitecture,
			).Return(*common.TestDefaultConfig.OsImage.Version, nil).Times(1)
			mockOSImages.EXPECT().GetOsImageByRhcosVersion(
				*common.TestDefaultConfig.OsImage.Version, common.TestDefaultConfig.CPUArchitecture,
			).Return(osImage, nil).Times(1)

			image, err := resolver.GetOsImageForInfraEnv(ctx, infraEnv)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image).Should(Equal(osImage))
		})
	})
})
