package versions

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models"
)

var _ = Describe("NewOSImages", func() {
	It("should fail when missing OpenshiftVersion", func() {
		osImages := models.OsImages{
			{
				Version:         swag.String("4.14.213113"),
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				URL:             swag.String("foobar-4.14"),
			},
		}

		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})

	It("should fail when missing Version", func() {
		osImages := models.OsImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("foobar-4.14"),
			},
		}

		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})

	It("should fail when missing CPU architecture", func() {
		osImages := models.OsImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.213113"),
				URL:              swag.String("foobar-4.14"),
			},
		}

		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})

	It("should fail when missing URL", func() {
		osImages := models.OsImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.213113"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
			},
		}

		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})

	It("should fail when CPU architecture is not valid", func() {
		osImages := models.OsImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.213113"),
				CPUArchitecture:  swag.String(common.AMD64CPUArchitecture),
			},
		}

		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("GetOsImage", func() {
	var (
		images OSImages
	)

	Context("with default images", func() {
		BeforeEach(func() {
			var err error
			images, err = NewOSImages(defaultOsImages)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("fails for an unsupported version", func() {
			image, err := images.GetOsImage("unsupported", common.TestDefaultConfig.CPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(image).Should(BeNil())
		})

		It("fails for an unsupported cpuArchitecture", func() {
			image, err := images.GetOsImage(common.TestDefaultConfig.OpenShiftVersion, "unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(image).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("isn't specified in OS images list"))
		})

		It("empty architecture fallback to default", func() {
			image, err := images.GetOsImage("4.9", "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image.CPUArchitecture).To(HaveValue(Equal(common.DefaultCPUArchitecture)))
		})

		It("multiarch returns error", func() {
			image, err := images.GetOsImage("4.11", common.MultiCPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(image).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("isn't specified in OS images list"))
		})

		It("fetch OS image by major.minor", func() {
			image, err := images.GetOsImage("4.9", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image.OpenshiftVersion).To(HaveValue(Equal("4.9")))
		})

		It("With normalizing the CPU architecture", func() {
			image, err := images.GetOsImage("4.9", common.AARCH64CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image.Version).To(HaveValue(Equal("version-49.123-0_arm64")))
			Expect(*image.CPUArchitecture).To(Equal(common.ARM64CPUArchitecture))
		})

		It("parses the image list correctly", func() {
			for _, version := range images.GetOpenshiftVersions() {
				architectures := images.GetCPUArchitectures(version)

				for _, architecture := range architectures {
					image, err := images.GetOsImage(version, architecture)
					Expect(err).ShouldNot(HaveOccurred())

					for _, rhcos := range defaultOsImages {
						if *rhcos.OpenshiftVersion == version && *rhcos.CPUArchitecture == architecture {
							Expect(image).Should(Equal(rhcos))
						}
					}
				}
			}
		})
	})

	Context("with patch versions", func() {
		BeforeEach(func() {
			var err error
			patchVersionOsImages := models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					OpenshiftVersion: swag.String("4.10.10"),
					URL:              swag.String("rhcos_4.10.2"),
					Version:          swag.String("version-4102.123-0"),
				},
				&models.OsImage{
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					OpenshiftVersion: swag.String("4.10.9"),
					URL:              swag.String("rhcos_4.10.1"),
					Version:          swag.String("version-4101.123-0"),
				},
			}
			images, err = NewOSImages(patchVersionOsImages)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("finds latest patch version by X.Y when given X.Y.Z", func() {
			image, err := images.GetOsImage("4.10.1", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image.OpenshiftVersion).To(HaveValue(Equal("4.10.10")))
		})

		It("finds latest patch version by X.Y when given X.Y", func() {
			image, err := images.GetOsImage("4.10", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(image.OpenshiftVersion).To(HaveValue(Equal("4.10.10")))
		})
	})
})

var _ = Describe("GetLatestOsImage", func() {
	It("only one OS image", func() {
		images, err := NewOSImages(defaultOsImages[0:1])
		Expect(err).ShouldNot(HaveOccurred())

		osImage, err := images.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*osImage.OpenshiftVersion).Should(Equal("4.11.1"))
		Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("Multiple OS images", func() {
		images, err := NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())

		osImage, err := images.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*osImage.OpenshiftVersion).Should(Equal("4.11.1"))
		Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("fails to get OS images for multiarch", func() {
		images, err := NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())

		osImage, err := images.GetLatestOsImage(common.MultiCPUArchitecture)
		Expect(err).Should(HaveOccurred())
		Expect(osImage).Should(BeNil())
		Expect(err.Error()).To(ContainSubstring("No OS images are available"))
	})
})

var _ = Describe("GetOsImageOrLatest", func() {
	var (
		images OSImages
	)

	BeforeEach(func() {
		var err error
		images, err = NewOSImages(defaultOsImages)
		Expect(err).To(BeNil())
	})

	It("successfully gets an OS image with a valid openshift version and cpu architecture", func() {
		image, err := images.GetOsImageOrLatest("4.9", common.TestDefaultConfig.CPUArchitecture)
		Expect(err).To(BeNil())
		Expect(*image.OpenshiftVersion).Should(Equal("4.9"))
		Expect(*image.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("successfully gets the latest OS image with a valid cpu architecture", func() {
		image, err := images.GetOsImageOrLatest("", common.TestDefaultConfig.CPUArchitecture)
		Expect(err).To(BeNil())
		Expect(*image.OpenshiftVersion).Should(Equal("4.11.1"))
		Expect(*image.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("fails to get OS images for invalid cpu architecture and valid openshift version", func() {
		image, err := images.GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "x866")
		Expect(err).ToNot(BeNil())
		Expect(image).Should(BeNil())
	})

	It("fails to get OS images for invalid cpu architecture and invalid openshift version", func() {
		image, err := images.GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "x866")
		Expect(err).ToNot(BeNil())
		Expect(image).Should(BeNil())
	})

	It("fails to get OS images for invalid cpu architecture and no openshift version", func() {
		image, err := images.GetOsImageOrLatest("", "x866")
		Expect(err).ToNot(BeNil())
		Expect(image).Should(BeNil())
	})
})

var _ = Describe("GetCPUArchitectures", func() {
	var (
		images OSImages
	)

	Context("with default images", func() {
		BeforeEach(func() {
			var err error
			images, err = NewOSImages(defaultOsImages)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("unsupported version", func() {
			Expect(images.GetCPUArchitectures("unsupported")).To(BeEmpty())
		})

		It("returns multiple CPU architectures for X.Y versions", func() {
			expected := []string{common.TestDefaultConfig.CPUArchitecture, common.ARM64CPUArchitecture}
			Expect(images.GetCPUArchitectures("4.9")).Should(Equal(expected))
		})

		It("returns multiple CPU architectures for X.Y.Z versions", func() {
			expected := []string{common.TestDefaultConfig.CPUArchitecture, common.ARM64CPUArchitecture}
			Expect(images.GetCPUArchitectures("4.9.1")).Should(Equal(expected))
		})
	})
})

var _ = Describe("NewOSImages", func() {
	validateImages := func(osImages models.OsImages) error {
		_, err := NewOSImages(osImages)
		return err
	}
	It("succeeds for a valid image list", func() {
		osImages := models.OsImages{
			&models.OsImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("rhcos_4.9"),
				Version:          swag.String("version-49.123-0"),
			},
			&models.OsImage{
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("rhcos_4.9_arm64"),
				Version:          swag.String("version-49.123-0_arm64"),
			},
		}
		Expect(validateImages(osImages)).To(Succeed())
	})

	It("fails when no images are provided", func() {
		Expect(validateImages(models.OsImages{})).NotTo(Succeed())
	})
	It("fails when url field is missing", func() {
		osImages := models.OsImages{
			&models.OsImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				Version:          swag.String("version-49.123-0"),
			},
		}
		Expect(validateImages(osImages)).NotTo(Succeed())
	})
	It("fails when version field is missing", func() {
		osImages := models.OsImages{
			&models.OsImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("rhcos_4.9"),
			},
		}
		Expect(validateImages(osImages)).NotTo(Succeed())
	})
	It("fails when openshift version field is missing", func() {
		osImages := models.OsImages{
			&models.OsImage{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				URL:             swag.String("rhcos_4.9"),
				Version:         swag.String("version-49.123-0"),
			},
		}
		Expect(validateImages(osImages)).NotTo(Succeed())
	})

	It("CPU architecture is not valid", func() {
		osImages := models.OsImages{
			&models.OsImage{
				CPUArchitecture:  swag.String(""),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("rhcos_4.9"),
				Version:          swag.String("version-49.123-0"),
			},
		}
		_, err := NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())

		osImages = models.OsImages{
			&models.OsImage{
				CPUArchitecture:  nil,
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("rhcos_4.9"),
				Version:          swag.String("version-49.123-0"),
			},
		}
		_, err = NewOSImages(osImages)
		Expect(err).Should(HaveOccurred())
	})
})
