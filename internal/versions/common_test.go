package versions

import (
	context "context"
	"fmt"
	"sync"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

var _ = Describe("NewHandler", func() {
	validateNewHandler := func(releaseImages models.ReleaseImages) error {
		_, err := NewHandler(common.GetTestLog(), nil, releaseImages, nil, "", nil, nil, nil, true, nil)
		return err
	}

	It("succeeds if no release images are specified", func() {
		releaseImages := models.ReleaseImages{}
		Expect(validateNewHandler(releaseImages)).To(Succeed())
	})

	It("succeeds with valid release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
				Version:          swag.String("4.9-candidate"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9_arm64"),
				Version:          swag.String("4.9-candidate_arm64"),
			},
		}
		Expect(validateNewHandler(releaseImages)).To(Succeed())
	})

	It("fails when missing CPUArchitecture in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
				Version:          swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cpu_architecture"))
	})

	It("fails when missing URL in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				Version:          swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("url"))
	})

	It("fails when missing Version in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("version"))
	})

	It("fails when missing OpenshiftVersion in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				URL:             swag.String("release_4.9"),
				Version:         swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("version"))
	})
})

var _ = Describe("validateReleaseImageForRHCOS", func() {
	log := common.GetTestLog()
	releaseImages := models.ReleaseImages{
		&models.ReleaseImage{
			CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
			CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
			OpenshiftVersion: swag.String("4.11.1"),
			URL:              swag.String("release_4.11.1"),
			Default:          false,
			Version:          swag.String("4.11.1-chocobomb-for-test"),
		},
		&models.ReleaseImage{
			CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
			CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
			OpenshiftVersion: swag.String("4.12"),
			URL:              swag.String("release_4.12"),
			Default:          false,
			Version:          swag.String("4.12"),
		},
	}

	It("validates successfuly using exact match", func() {
		Expect(validateReleaseImageForRHCOS(log, "4.11.1", common.X86CPUArchitecture, releaseImages)).To(Succeed())
	})
	It("validates successfuly using major.minor", func() {
		Expect(validateReleaseImageForRHCOS(log, "4.11", common.X86CPUArchitecture, releaseImages)).To(Succeed())
	})
	It("validates successfuly using major.minor using default architecture", func() {
		Expect(validateReleaseImageForRHCOS(log, "4.11", "", releaseImages)).To(Succeed())
	})
	It("validates successfuly using major.minor.patch-something", func() {
		Expect(validateReleaseImageForRHCOS(log, "4.12.2-chocobomb", common.X86CPUArchitecture, releaseImages)).To(Succeed())
	})
	It("fails validation using non-existing major.minor.patch-something", func() {
		Expect(validateReleaseImageForRHCOS(log, "9.9.9-chocobomb", common.X86CPUArchitecture, releaseImages)).NotTo(Succeed())
	})
	It("fails validation using multiarch", func() {
		// This test is supposed to fail because there exists no RHCOS image that supports
		// multiple architectures.
		Expect(validateReleaseImageForRHCOS(log, "4.11", common.MultiCPUArchitecture, releaseImages)).NotTo(Succeed())
	})
	It("fails validation using invalid version", func() {
		Expect(validateReleaseImageForRHCOS(log, "invalid", common.X86CPUArchitecture, releaseImages)).NotTo(Succeed())
	})
})

var _ = Describe("GetMustGatherImages", func() {
	var (
		log              = common.GetTestLog()
		imagesLock       = sync.Mutex{}
		ctrl             *gomock.Controller
		mockRelease      *oc.MockRelease
		cpuArchitecture  = common.TestDefaultConfig.CPUArchitecture
		pullSecret       = "test_pull_secret"
		ocpVersion       = "4.8.0-fc.1"
		mirror           = "release-mirror"
		imagesKey        = fmt.Sprintf("4.8-%s", cpuArchitecture)
		mustgatherImages MustGatherVersions
	)

	BeforeEach(func() {
		var err error
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		Expect(err).ShouldNot(HaveOccurred())
		mustgatherImages = MustGatherVersions{
			imagesKey: MustGatherVersion{
				"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
				"odf": "registry.redhat.io/ocs4/odf-must-gather-rhel8",
				"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	getReleaseImageMock := func(_ context.Context, openshiftVersion, cpuArch, _ string) (*models.ReleaseImage, error) {
		if openshiftVersion == ocpVersion && cpuArch == cpuArchitecture {
			return &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.8"),
				URL:              swag.String("release_4.8"),
				Version:          swag.String("4.8-candidate"),
			}, nil
		}

		return nil, errors.New("No release image found")
	}

	verifyOcpVersion := func(images MustGatherVersion, size int) {
		Expect(len(images)).To(Equal(size))
		Expect(images["ocp"]).To(Equal("blah"))
	}

	It("happy flow", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).ShouldNot(HaveOccurred())

		verifyOcpVersion(images, 4)
		Expect(images["lso"]).To(Equal(mustgatherImages[imagesKey]["lso"]))
	})

	It("unsupported_key", func() {
		images, err := getMustGatherImages(
			log,
			"unsupported",
			cpuArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})

	It("caching", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)

		images, err = getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)
	})

	It("properly handles separate images for multiple architectures of the same version", func() {
		mockRelease.EXPECT().GetMustGatherImage(
			gomock.Any(), "release_4.12.999-multi", mirror, pullSecret).
			Return("must-gather-multi", nil).
			AnyTimes()
		mockRelease.EXPECT().GetMustGatherImage(
			gomock.Any(), "release_4.12.999-x86_64", mirror, pullSecret).
			Return("must-gather-x86", nil).
			AnyTimes()

		getReleaseImageMock = func(_ context.Context, openshiftVersion, cpuArch, _ string) (*models.ReleaseImage, error) {
			if openshiftVersion == "4.12-multi" && cpuArch == common.MultiCPUArchitecture {
				return &models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
					OpenshiftVersion: swag.String("4.12-multi"),
					URL:              swag.String("release_4.12.999-multi"),
					Version:          swag.String("4.12.999-rc.4"),
				}, nil
			}

			if openshiftVersion == "4.12" && cpuArch == common.X86CPUArchitecture {
				return &models.ReleaseImage{
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					OpenshiftVersion: swag.String("4.12"),
					URL:              swag.String("release_4.12.999-x86_64"),
					Version:          swag.String("4.12.999-rc.4"),
				}, nil
			}

			return nil, errors.New("No release image found")
		}

		images, err := getMustGatherImages(
			log,
			"4.12-multi",
			common.MultiCPUArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-multi"))

		images, err = getMustGatherImages(
			log,
			"4.12",
			common.X86CPUArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-x86"))
	})

	It("missing release image", func() {
		images, err := getMustGatherImages(
			log,
			"4.7",
			cpuArchitecture,
			pullSecret,
			mirror,
			mustgatherImages,
			getReleaseImageMock,
			mockRelease,
			&imagesLock,
		)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})
})
