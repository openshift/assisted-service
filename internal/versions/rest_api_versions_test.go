package versions

import (
	"context"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("GetReleaseImage", func() {
	var (
		handler    *restAPIVersionsHandler
		db         *gorm.DB
		dbName     string
		pullSecret string = "{}"
		ctx               = context.TODO()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		handler = &restAPIVersionsHandler{
			log:                      common.GetTestLog(),
			releaseHandler:           nil,
			mustGatherVersions:       nil,
			ignoredOpenshiftVersions: nil,
			db:                       db,
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("with unsupported openshiftVersion should return an error", func() {
		releaseImage, err := handler.GetReleaseImage(ctx, "unsupported", common.TestDefaultConfig.CPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("with unsupported cpuArchitecture should return an error", func() {
		releaseImage, err := handler.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "unsupported", pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("with empty openshiftVersion should return an error", func() {
		releaseImage, err := handler.GetReleaseImage(ctx, "", common.TestDefaultConfig.CPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("with empty cpuArchitecture should return an error", func() {
		releaseImage, err := handler.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "", pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("with major openshiftVersion should return an error", func() {
		releaseImage, err := handler.GetReleaseImage(ctx, "4", "", pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("gets the latest matching release image with major.minor openshiftVersion", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.3"),
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.3-aarch64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.15"),
				Version:          swag.String("4.15.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.15.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2"))
	})

	It("gets the exact matching release image with major.minor.patch / prerelease openshiftVersion", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0-ec.3"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.0-ec.3-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0-ec.3"),
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.0-ec.3-aarch64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-aarch64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.3"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.3-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14.0-ec.3", common.DefaultCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.0-ec.3"))

		releaseImage, err = handler.GetReleaseImage(ctx, "4.14.2", common.DefaultCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2"))
	})

	It("gets release image successfully with major.minor.patch openshiftVersion and old syntax", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14.2", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2"))
	})

	It("gets release image successfully with major.minor openshiftVersion and old syntax", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2"))
	})

	It("gets successfully image using major.minor.patch openshiftVersion in multiarch query - with multi suffix", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.1-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14.1-multi", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.1-multi"))
	})

	It("gets successfully image using major.minor.patch openshiftVersion in multiarch query - without multi suffix", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.1-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14.1", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.1-multi"))
	})

	It("gets successfully image using major.minor openshiftVersion in multiarch query - with multi suffix", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.1-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14-multi", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2-multi"))
	})

	It("gets successfully image using major.minor openshiftVersion in multiarch query - without multi suffix", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.1-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.14.2-multi"))
	})

	It("returns an error when using major.minor.patch openshiftVersion but no exact match found", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := handler.GetReleaseImage(ctx, "4.14.3", common.MultiCPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).To(BeNil())
	})

	It("filters ignored release images successfully with major.minor ignored versions", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.15"),
				Version:          swag.String("4.15.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.15.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		handler.ignoredOpenshiftVersions = []string{"4.14"}
		releaseImage, err := handler.GetReleaseImage(ctx, "4.14", common.X86CPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).To(BeNil())

		releaseImage, err = handler.GetReleaseImage(ctx, "4.15", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).Should(Equal("4.15.1"))
	})

	It("filters ignored release images successfully with exact ignored version", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14-multi"),
				Version:          swag.String("4.14.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.15"),
				Version:          swag.String("4.15.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.15.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err := db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())

		handler.ignoredOpenshiftVersions = []string{"4.14.2"}
		releaseImage, err := handler.GetReleaseImage(ctx, "4.14", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).To(Equal("4.14.1"))
	})
})

var _ = Describe("GetReleaseImageByURL", func() {
	var (
		handler *restAPIVersionsHandler
		db      *gorm.DB
		dbName  string
		err     error
		ctx     = context.TODO()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		handler = &restAPIVersionsHandler{
			log:                      common.GetTestLog(),
			releaseHandler:           nil,
			mustGatherVersions:       nil,
			ignoredOpenshiftVersions: nil,
			db:                       db,
		}

		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				Default:          false,
			},
		}

		err = db.Create(releaseImages).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("gets release image successfully with valid URL", func() {
		releaseImage, err := handler.GetReleaseImageByURL(ctx, "quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64", "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.Version).To(Equal("4.14.1"))
	})

	It("fails when no release image exists with the given URL", func() {
		releaseImage, err := handler.GetReleaseImageByURL(ctx, "quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64", "")
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).To(BeNil())
	})

	It("fails when given release image URL refers to an ignored version", func() {
		handler.ignoredOpenshiftVersions = []string{"4.14.1"}

		releaseImage, err := handler.GetReleaseImageByURL(ctx, "quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64", "")
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).To(BeNil())
	})
})
