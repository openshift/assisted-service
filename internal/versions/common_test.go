package versions

import (
	"fmt"
	"strings"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
<<<<<<< HEAD
	"github.com/openshift/assisted-service/models"
=======
	"github.com/openshift/assisted-service/internal/oc"
	models "github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	gomock "go.uber.org/mock/gomock"
>>>>>>> 8fed8a5f6 (mockgen deprecated: use uber-go/mock instead)
)

var _ = Describe("NewHandler", func() {
	validateNewHandler := func(releaseImages models.ReleaseImages) error {
		_, err := NewHandler(common.GetTestLog(), nil, releaseImages, NewMustGatherVersionCache(), "", nil, nil, nil, true, nil)
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

	It("Normalizes CPU architecture", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("release_4.14"),
			},
			&models.ReleaseImage{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.AARCH64CPUArchitecture),
				URL:              swag.String("release_4.14"),
			},
		}

		_, err := NewHandler(common.GetTestLog(), nil, releaseImages, NewMustGatherVersionCache(), "", nil, nil, nil, false, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(*releaseImages[0].CPUArchitecture).To(Equal(common.X86CPUArchitecture))
		Expect(*releaseImages[1].CPUArchitecture).To(Equal(common.ARM64CPUArchitecture))
	})

	It("Validates CPU architecture", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.AMD64CPUArchitecture),
				URL:              swag.String("release_4.14"),
			},
		}

		_, err := NewHandler(common.GetTestLog(), nil, releaseImages, NewMustGatherVersionCache(), "", nil, nil, nil, false, nil)
		Expect(err).To(HaveOccurred())
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

	It("advises on suitable Openshift versions if unable to find release image for architecture", func() {
		err := validateReleaseImageForRHCOS(log, "9.9.9-chocobomb", common.X86CPUArchitecture, releaseImages)
		Expect(err).ToNot(BeNil())
		errorMessage := err.Error()
		Expect(errorMessage).To(ContainSubstring(fmt.Sprintf("The requested RHCOS version (%s, arch: %s) does not have a matching OpenShift release image.", "9.9", common.X86CPUArchitecture)))
		Expect(errorMessage).To(ContainSubstring(fmt.Sprintf("These are the OCP versions for which a matching release image has been found for arch %s", common.X86CPUArchitecture)))
		splitErrorMessage := strings.Split(errorMessage, ":")
		Expect(len(splitErrorMessage)).To(Equal(3))
		versionsSplit := strings.Split(strings.Trim(splitErrorMessage[2], " "), ",")
		Expect(versionsSplit).To(ContainElement("4.11.1"))
		Expect(versionsSplit).To(ContainElement("4.12"))

	})

	It("advises that no Openshift versions are available in release images if none found for architecture", func() {
		err := validateReleaseImageForRHCOS(log, "9.9.9-chocobomb", common.PowerCPUArchitecture, releaseImages)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("There are no OCP versions available in release images for arch %s", common.PowerCPUArchitecture)))
	})
})
