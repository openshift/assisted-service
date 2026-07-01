package versions

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
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

var _ = Describe("OsImageVersion", func() {
	It("returns RHCOS version when set", func() {
		openshiftVersion := "4.10"
		rhcosVersion := "410.84.202201251210-0"
		osImage := models.OsImage{OpenshiftVersion: &openshiftVersion, Version: &rhcosVersion}
		v, err := OsImageVersion(&osImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(v).To(Equal(rhcosVersion))
	})

	It("falls back to OpenShift version when RHCOS version is empty", func() {
		openshiftVersion := "4.10"
		emptyVersion := ""
		osImage := models.OsImage{OpenshiftVersion: &openshiftVersion, Version: &emptyVersion}
		v, err := OsImageVersion(&osImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(v).To(Equal(openshiftVersion))
	})
})
