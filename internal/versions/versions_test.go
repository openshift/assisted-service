package versions

import (
	context "context"
	"fmt"
	"testing"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandlerVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "versions")
}

var defaultOsImages = models.OsImages{
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.11.1"),
		URL:              swag.String("rhcos_4.11"),
		Version:          swag.String("version-411.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("rhcos_4.10.1"),
		Version:          swag.String("version-4101.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.10.2"),
		URL:              swag.String("rhcos_4.10.2"),
		Version:          swag.String("version-4102.123-0"),
	},
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
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("rhcos_4.91"),
		Version:          swag.String("version-491.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("rhcos_4.8"),
		Version:          swag.String("version-48.123-0"),
	},
}

var defaultReleaseImages = models.ReleaseImages{
	// Two images below represent a scenario when the same OpenShift Version (as reported by the
	// CVO) is provided by more than a single release image. This is a scenario when for the same
	// OCP version we have single-arch and multi-arch image. This happens because starting from
	// OCP 4.12 CSV returns the same value no matter the architecture-ness of the release image.
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.12"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.999-rc.4-x86_64"),
		Version:          swag.String("4.12.999-rc.4"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
		OpenshiftVersion: swag.String("4.12-multi"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.999-rc.4-multi"),
		Version:          swag.String("4.12.999-rc.4-multi"),
	},
	&models.ReleaseImage{
		// This image uses a syntax with missing "cpu_architectures". It is crafted
		// in order to make sure the change in MGMT-11494 is backwards-compatible.
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{},
		OpenshiftVersion: swag.String("4.11.2"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
		Version:          swag.String("4.11.2"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
		OpenshiftVersion: swag.String("4.11.1-multi"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-multi"),
		Version:          swag.String("4.11.1-multi"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.10.1-x86_64"),
		Version:          swag.String("4.10.1"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.10.2"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.10.2-x86_64"),
		Version:          swag.String("4.10.2"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.9.0-x86_64"),
		Version:          swag.String("4.9.3"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
		Default:          true,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
		CPUArchitectures: []string{common.ARM64CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.9.0-aarch64"),
		Version:          swag.String("4.9.2"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.9.1-x86_64"),
		Version:          swag.String("4.9.1"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.8.0-x86_64"),
		Version:          swag.String("4.8.1"),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	},
}

func createSupportLevelTable(db *gorm.DB) error {
	supportLevels := []common.OpenshiftVersionSupportLevel{
		{
			OpenshiftVersion: "4.15",
			SupportLevel:     "beta",
		},
		{
			OpenshiftVersion: "4.14",
			SupportLevel:     "production",
		},
		{
			OpenshiftVersion: "4.13",
			SupportLevel:     "production",
		},
		{
			OpenshiftVersion: "4.12",
			SupportLevel:     "maintenance",
		},
		{
			OpenshiftVersion: "4.11",
			SupportLevel:     "maintenance",
		},
		{
			OpenshiftVersion: "4.10",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.9",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.8",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.7",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.6",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.5",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.4",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.3",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.2",
			SupportLevel:     "end of life",
		},
		{
			OpenshiftVersion: "4.1",
			SupportLevel:     "end of life",
		},
	}

	return db.Create(&supportLevels).Error
}

var _ = Describe("GetReleaseImage", func() {
	var (
		h           *handler
		ctx         = context.Background()
		pullSecret  = "mypullsecret"
		ctrl        *gomock.Controller
		mockRelease *oc.MockRelease
		db          *gorm.DB
		dbName      string
	)

	BeforeEach(func() {
		var err error
		db, dbName = common.PrepareTestDB()
		Expect(err).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		h, err = NewHandler(NewVersionHandlerParams{
			Log:            common.GetTestLog(),
			ReleaseHandler: mockRelease,
			ReleaseImages:  defaultReleaseImages,
			DB:             db,
		})
		Expect(err).ShouldNot(HaveOccurred())
		err = createSupportLevelTable(db)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Without db releases", func() {
		It("unsupported openshiftVersion", func() {
			releaseImage, err := h.GetReleaseImage(ctx, "unsupported", common.TestDefaultConfig.CPUArchitecture, pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("unsupported cpuArchitecture", func() {
			releaseImage, err := h.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "unsupported", pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("is not a valid release image CPU architecture"))
		})

		It("empty openshiftVersion", func() {
			releaseImage, err := h.GetReleaseImage(ctx, "", common.TestDefaultConfig.CPUArchitecture, pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("empty cpuArchitecture", func() {
			releaseImage, err := h.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "", pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("fetch release image by major.minor", func() {
			releaseImage, err := h.GetReleaseImage(ctx, "4.9", common.DefaultCPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
			Expect(*releaseImage.Version).Should(Equal("4.9.3"))
		})

		It("gets successfuly image with old syntax", func() {
			releaseImage, err := h.GetReleaseImage(ctx, "4.11.2", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.2"))
			Expect(*releaseImage.Version).Should(Equal("4.11.2"))
		})

		It("gets successfuly image with new syntax", func() {
			releaseImage, err := h.GetReleaseImage(ctx, "4.10.1", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.10.1"))
			Expect(*releaseImage.Version).Should(Equal("4.10.1"))
		})
	})

	Context("With DB releases", func() {
		dbReleases := []*common.ReleaseImage{
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.9.12"),
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.9.12", common.X86CPUArchitecture)),
				},
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.9.13"),
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.9.13", common.X86CPUArchitecture)),
				},
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.6.11"),
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.6.11", common.X86CPUArchitecture)),
				},
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.6.12"),
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.6.12", common.X86CPUArchitecture)),
				},
			},
		}

		It("Get release image from DB when it is not in the configuration", func() {
			err := db.Create(&dbReleases).Error
			Expect(err).ToNot(HaveOccurred())

			release, err := h.GetReleaseImage(ctx, "4.9.12", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*release.Version).To(Equal("4.9.12"))
		})

		It("Can't get release image from DB as well as the configuration with wildcards", func() {
			err := db.Create(&dbReleases).Error
			Expect(err).ToNot(HaveOccurred())

			err = db.Create(
				&common.ReleaseImage{

					ReleaseImage: models.ReleaseImage{
						Version:         swag.String("4.6.10-%"),
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.16.10-%", common.X86CPUArchitecture)),
					},
				},
			).Error
			Expect(err).ToNot(HaveOccurred())

			release, err := h.GetReleaseImage(ctx, "%", common.X86CPUArchitecture, pullSecret)
			Expect(release).To(BeNil())
			Expect(err).Should(HaveOccurred())

			release, err = h.GetReleaseImage(ctx, "_", common.X86CPUArchitecture, pullSecret)
			Expect(release).To(BeNil())
			Expect(err).Should(HaveOccurred())

			release, err = h.GetReleaseImage(ctx, "/", common.X86CPUArchitecture, pullSecret)
			Expect(release).To(BeNil())
			Expect(err).Should(HaveOccurred())
		})

		It("Can't get release image from neither DB nor configuration, should get the latest major.minor from configuration", func() {
			err := db.Create(&dbReleases).Error
			Expect(err).ToNot(HaveOccurred())

			release, err := h.GetReleaseImage(ctx, "4.9.17", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*release.Version).To(Equal("4.9.3"))
		})

		It("Can't get release image from neither DB nor configuration, should get the latest major.minor from DB", func() {
			err := db.Create(&dbReleases).Error
			Expect(err).ToNot(HaveOccurred())

			release, err := h.GetReleaseImage(ctx, "4.6.7", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*release.Version).To(Equal("4.6.12"))
		})

		It("Can't get release image from neither DB nor configuration, should not find a release and return an error", func() {
			err := db.Create(&dbReleases).Error
			Expect(err).ToNot(HaveOccurred())

			release, err := h.GetReleaseImage(ctx, "4.7.2", common.X86CPUArchitecture, pullSecret)
			Expect(release).To(BeNil())
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("with both configuration and DB releases, different scenarios", func() {
		configurationReleaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.14-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-multi"),
				Version:          swag.String("4.14.2-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.14"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
				Version:          swag.String("4.14.2"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.14.1-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-multi"),
				Version:          swag.String("4.14.1-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.14.1"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
				Version:          swag.String("4.14.1"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.13-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.2-multi"),
				Version:          swag.String("4.13.2-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.2-x86_64"),
				Version:          swag.String("4.13.2"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.13.1-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.1-multi"),
				Version:          swag.String("4.13.1-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13.1"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.1-x86_64"),
				Version:          swag.String("4.13.1"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.12-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-multi"),
				Version:          swag.String("4.12.2-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-x86_64"),
				Version:          swag.String("4.12.2"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.12.1-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.1-multi"),
				Version:          swag.String("4.12.1-multi"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12.1"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.1-x86_64"),
				Version:          swag.String("4.12.1"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			},
		}

		dbReleaseImages := []*common.ReleaseImage{
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.3"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.3", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.3"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.3", common.ARM64CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.3"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.MultiCPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.3", common.MultiCPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.4"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.4", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.4"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.4", common.ARM64CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.4"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.MultiCPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.4", common.MultiCPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.5"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.5", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.5"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.5", common.ARM64CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.6"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.6", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.15.6"),
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					Default:         false,
					CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.6", common.ARM64CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
		}

		BeforeEach(func() {
			err := db.Create(&dbReleaseImages).Error
			Expect(err).ToNot(HaveOccurred())
			h.releaseImages = configurationReleaseImages
		})

		Context("REST API mode", func() {
			Context("With full openshift version", func() {
				Context("Should find the release in the configuration", func() {
					It("By OpenshiftVersion", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.2", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2"))
					})

					It("By Version", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.1", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.1"))
					})

					It("By OpenshiftVersion - multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.2-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2-multi"))
					})

					It("By Version - multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.1-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.1-multi"))
					})
				})

				Context("Should Find the release in the DB", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15.3", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.3"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15.3", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.3"))
					})
				})

				Context("Should fallback to latest major.minor from configuration", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.0", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14.0", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2-multi"))
					})
				})

				Context("Should fallback to latest major.minor from DB", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15.2", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.6"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15.2", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.4"))
					})
				})

				Context("Should fail when no release matches", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.17.2", common.X86CPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.17.2", common.MultiCPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})
				})
			})

			Context("With major.minor openshift version", func() {
				Context("Should get the latest major.minor from configuration", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.14-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2-multi"))
					})
				})

				Context("Should get the latest major.minor from DB", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.6"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.15-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.15.4"))
					})
				})

				Context("Should fail when no release matches", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.17", common.X86CPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4.17", common.MultiCPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})
				})
			})

			Context("With major openshift version", func() {
				Context("Should get the latest major.minor from configuration", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "4", common.X86CPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2"))
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "4-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).ShouldNot(HaveOccurred())
						Expect(*release.Version).To(Equal("4.14.2-multi"))
					})
				})

				Context("Should fail when no release matches", func() {
					It("Single arch", func() {
						release, err := h.GetReleaseImage(ctx, "3", common.X86CPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})

					It("Multi arch", func() {
						release, err := h.GetReleaseImage(ctx, "3-multi", common.MultiCPUArchitecture, pullSecret)
						Expect(err).Should(HaveOccurred())
						Expect(release).To(BeNil())
					})
				})
			})
		})
	})

	Context("with a kube client", func() {
		var (
			client client.Client
		)
		BeforeEach(func() {
			schemes := runtime.NewScheme()
			utilruntime.Must(hivev1.AddToScheme(schemes))
			client = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
			h.kubeClient = client
		})

		It("returns no image when no clusterimageset matches", func() {
			image, err := h.GetReleaseImage(ctx, "4.20.0", common.X86CPUArchitecture, pullSecret)
			Expect(err).To(HaveOccurred())
			Expect(image).To(BeNil())
		})

		It("returns an existing image from the configuration", func() {
			image, err := h.GetReleaseImage(ctx, "4.11.1-multi", common.MultiCPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal("quay.io/openshift-release-dev/ocp-release:4.11.1-multi")))
		})

		It("adds a release to the configuration from a clusterimageset when no image in the configuration matches", func() {
			releaseImageURL := "example.com/openshift-release-dev/ocp-release:4.13.999"
			cis := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: "new-release"},
				Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: releaseImageURL},
			}
			Expect(client.Create(ctx, cis)).To(Succeed())

			mockRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), releaseImageURL, "", pullSecret).Return("4.13.999", nil).Times(1)
			mockRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), releaseImageURL, "", pullSecret).Return([]string{common.X86CPUArchitecture}, nil).Times(1)

			image, err := h.GetReleaseImage(ctx, "4.13.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
			image, err = h.GetReleaseImage(ctx, "4.13.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
		})

		It("doesn't re-add existing releases", func() {
			for _, rel := range defaultReleaseImages {
				cis := &hivev1.ClusterImageSet{
					ObjectMeta: metav1.ObjectMeta{Name: *rel.URL},
					Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: *rel.URL},
				}
				Expect(client.Create(ctx, cis)).To(Succeed())
			}
			releaseImageURL := "example.com/openshift-release-dev/ocp-release:4.13.999"
			cis := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: "new-release"},
				Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: releaseImageURL},
			}
			Expect(client.Create(ctx, cis)).To(Succeed())

			mockRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), releaseImageURL, "", pullSecret).Return("4.13.999", nil).Times(1)
			mockRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), releaseImageURL, "", pullSecret).Return([]string{common.X86CPUArchitecture}, nil).Times(1)

			image, err := h.GetReleaseImage(ctx, "4.13.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
		})
	})
})

var _ = Describe("ValidateReleaseImageForRHCOS", func() {
	var h *handler

	BeforeEach(func() {
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
		var err error
		h, err = NewHandler(NewVersionHandlerParams{
			Log:           common.GetTestLog(),
			ReleaseImages: releaseImages,
		})
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("validates successfuly using exact match", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11.1", common.X86CPUArchitecture)).To(Succeed())
	})
	It("validates successfuly using major.minor", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11", common.X86CPUArchitecture)).To(Succeed())
	})
	It("validates successfuly using major.minor using default architecture", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11", "")).To(Succeed())
	})
	It("validates successfuly using major.minor.patch-something", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.12.2-chocobomb", common.X86CPUArchitecture)).To(Succeed())
	})
	It("fails validation using non-existing major.minor.patch-something", func() {
		Expect(h.ValidateReleaseImageForRHCOS("9.9.9-chocobomb", common.X86CPUArchitecture)).NotTo(Succeed())
	})
	It("fails validation using multiarch", func() {
		// This test is supposed to fail because there exists no RHCOS image that supports
		// multiple architectures.
		Expect(h.ValidateReleaseImageForRHCOS("4.11", common.MultiCPUArchitecture)).NotTo(Succeed())
	})
	It("fails validation using invalid version", func() {
		Expect(h.ValidateReleaseImageForRHCOS("invalid", common.X86CPUArchitecture)).NotTo(Succeed())
	})
})

var _ = Describe("GetDefaultReleaseImageByCPUArchitecture", func() {

	var (
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Default release image exists in configuration but not in DB", func() {
		h, err := NewHandler(NewVersionHandlerParams{
			Log:           common.GetTestLog(),
			ReleaseImages: defaultReleaseImages,
			DB:            db,
		})
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := h.GetDefaultReleaseImageByCPUArchitecture(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(releaseImage.Default).Should(Equal(true))
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("Default release image exists in DB but not in configuration", func() {
		dbReleases := []*common.ReleaseImage{
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.11.2"),
					Default:         false,
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.11.2", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.11.3"),
					Default:         true,
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.11.3", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
		}

		err := db.Create(&dbReleases).Error
		Expect(err).ShouldNot(HaveOccurred())

		h, err := NewHandler(NewVersionHandlerParams{
			Log: common.GetTestLog(),
			DB:  db,
		})
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := h.GetDefaultReleaseImageByCPUArchitecture(common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(releaseImage.Default).Should(Equal(true))
		Expect(*releaseImage.Version).Should(Equal("4.11.3"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.X86CPUArchitecture))
	})

	It("Default is missing in both configuration and DB", func() {
		h, err := NewHandler(NewVersionHandlerParams{
			Log: common.GetTestLog(),
			DB:  db,
		})
		Expect(err).ShouldNot(HaveOccurred())

		_, err = h.GetDefaultReleaseImageByCPUArchitecture(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(Equal("Default release image is not available"))
	})

	It("Default is exists in both configuration and DB - configuration has precedence", func() {
		dbReleases := []*common.ReleaseImage{
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.11.2"),
					Default:         false,
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.11.12", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
			{
				ReleaseImage: models.ReleaseImage{
					Version:         swag.String("4.11.3"),
					Default:         true,
					SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.11.13", common.X86CPUArchitecture)),
				},
				Channel: common.OpenshiftReleaseChannelStable,
			},
		}

		configReleases := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.12"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				Default:          true,
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
				Version:          swag.String("4.12.2"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-x86_64"),
			},
		}

		err := db.Create(&dbReleases).Error
		Expect(err).ShouldNot(HaveOccurred())

		h, err := NewHandler(NewVersionHandlerParams{
			Log:           common.GetTestLog(),
			DB:            db,
			ReleaseImages: configReleases,
		})
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := h.GetDefaultReleaseImageByCPUArchitecture(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(releaseImage.Default).Should(Equal(true))
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.12"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})
})

var _ = Describe("GetMustGatherImages", func() {
	var (
		h                *handler
		ctrl             *gomock.Controller
		mockRelease      *oc.MockRelease
		cpuArchitecture  = common.TestDefaultConfig.CPUArchitecture
		pullSecret       = "test_pull_secret"
		ocpVersion       = "4.8"
		mirror           = "release-mirror"
		imagesKey        = fmt.Sprintf("4.8-%s", cpuArchitecture)
		mustgatherImages = MustGatherVersions{
			imagesKey: MustGatherVersion{
				"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
				"odf": "registry.redhat.io/ocs4/odf-must-gather-rhel8",
				"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
			},
		}
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		var err error
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		db, dbName = common.PrepareTestDB()
		h, err = NewHandler(NewVersionHandlerParams{
			Log:                common.GetTestLog(),
			ReleaseHandler:     mockRelease,
			ReleaseImages:      defaultReleaseImages,
			MustGatherVersions: mustgatherImages,
			ReleaseImageMirror: mirror,
			DB:                 db,
		})
		Expect(err).ShouldNot(HaveOccurred())
		err = createSupportLevelTable(db)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	verifyOcpVersion := func(images MustGatherVersion, size int) {
		Expect(len(images)).To(Equal(size))
		Expect(images["ocp"]).To(Equal("blah"))
	}

	It("happy flow", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "quay.io/openshift-release-dev/ocp-release:4.8.0-x86_64", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())

		verifyOcpVersion(images, 4)
		Expect(images["lso"]).To(Equal(mustgatherImages[imagesKey]["lso"]))
	})

	It("unsupported_key", func() {
		images, err := h.GetMustGatherImages("unsupported", cpuArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})

	It("caching", func() {
		images, err := h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)

		images, err = h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)
	})

	It("properly handles separate images for multiple architectures of the same version", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "quay.io/openshift-release-dev/ocp-release:4.12.999-rc.4-multi", mirror, pullSecret).Return("must-gather-multi", nil).AnyTimes()
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "quay.io/openshift-release-dev/ocp-release:4.12.999-rc.4-x86_64", mirror, pullSecret).Return("must-gather-x86", nil).AnyTimes()

		images, err := h.GetMustGatherImages("4.12", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-multi"))

		images, err = h.GetMustGatherImages("4.12", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-x86"))
	})

	It("missing release image", func() {
		images, err := h.GetMustGatherImages("4.7", cpuArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("isn't specified in release images list"))
		Expect(images).Should(BeEmpty())
	})
})

var _ = Describe("GetReleaseImageByURL", func() {
	var (
		h                  *handler
		ctrl               *gomock.Controller
		mockRelease        *oc.MockRelease
		cpuArchitecture    = common.TestDefaultConfig.CPUArchitecture
		pullSecret         = "test_pull_secret"
		releaseImageUrl    = "quay.io/openshift-release-dev/ocp-release:4.7.0-x86_64"
		customOcpVersion   = "4.7.0"
		existingOcpVersion = "4.9.1"
		ctx                = context.Background()
		db                 *gorm.DB
		dbName             string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		db, dbName = common.PrepareTestDB()

		var err error
		h, err = NewHandler(NewVersionHandlerParams{
			Log:            common.GetTestLog(),
			ReleaseHandler: mockRelease,
			ReleaseImages:  defaultReleaseImages,
			DB:             db,
		})
		Expect(err).ShouldNot(HaveOccurred())
		err = createSupportLevelTable(db)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("for single-arch release image", func() {
		It("added successfully", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			releaseImage, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture}))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("when release image already exists", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(existingOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
				return *releaseImage.OpenshiftVersion == existingOcpVersion && *releaseImage.CPUArchitecture == cpuArchitecture
			})
			Expect(releaseImageFromCache).ShouldNot(BeNil())

			_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage, err := h.GetReleaseImage(ctx, existingOcpVersion, cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
		})

		It("succeeds for missing OS image", func() {
			ocpVersion := "4.7"
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			_, err := h.GetReleaseImageByURL(ctx, "invalidRelease", pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("for multi-arch release image", func() {
		It("added successfully", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

			releaseImage, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
			Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture, common.ARM64CPUArchitecture}))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("when release image already exists", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("4.11.1", nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

			releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
				return *releaseImage.OpenshiftVersion == "4.11.1-multi" && *releaseImage.CPUArchitecture == common.MultiCPUArchitecture
			})
			Expect(releaseImageFromCache).ShouldNot(BeNil())

			_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			// Query for multi-arch release image using generic multiarch
			releaseImage, err := h.GetReleaseImage(ctx, "4.11.1-multi", common.MultiCPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))

			// Query for non-existing architecture
			_, err = h.GetReleaseImage(ctx, "4.11.1", "architecture-chocobomb", pullSecret)
			Expect(err.Error()).Should(Equal("architecture-chocobomb is not a valid release image CPU architecture"))
		})
	})

	It("fails when the version extraction fails", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).Should(HaveOccurred())
	})

	It("fails when the arch extraction fails", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error when getting architecture")).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).Should(HaveOccurred())
	})

	It("keep support level from cache", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		_, err = h.GetReleaseImage(ctx, customOcpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("NewHandler", func() {
	validateNewHandler := func(releaseImages models.ReleaseImages) error {
		_, err := NewHandler(NewVersionHandlerParams{
			Log:           common.GetTestLog(),
			ReleaseImages: releaseImages,
		})
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
