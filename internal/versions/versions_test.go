package versions

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gopkg.in/square/go-jose.v2/json"
	"gorm.io/gorm"
)

func TestHandler_ListComponentVersions(t *testing.T) {
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
		URL:              swag.String("rhcos_4.10"),
		Version:          swag.String("version-410.123-0"),
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
	&models.ReleaseImage{
		// This image uses a syntax with missing "cpu_architectures". It is crafted
		// in order to make sure the change in MGMT-11494 is backwards-compatible.
		CPUArchitecture:  swag.String("fake-architecture-chocobomb"),
		CPUArchitectures: []string{},
		OpenshiftVersion: swag.String("4.11.2"),
		URL:              swag.String("release_4.11.2"),
		Version:          swag.String("4.11.2-fake-chocobomb"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
		OpenshiftVersion: swag.String("4.11.1"),
		URL:              swag.String("release_4.11.1"),
		Version:          swag.String("4.11.1-multi"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("release_4.10.1"),
		Version:          swag.String("4.10.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9"),
		Version:          swag.String("4.9-candidate"),
		Default:          true,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
		CPUArchitectures: []string{common.ARM64CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9_arm64"),
		Version:          swag.String("4.9-candidate_arm64"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("release_4.9.1"),
		Version:          swag.String("4.9.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("release_4.8"),
		Version:          swag.String("4.8-candidate"),
	},
}

var mustgatherImages = MustGatherVersions{
	"4.8": MustGatherVersion{
		"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
		"odf": "registry.redhat.io/ocs4/odf-must-gather-rhel8",
		"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
	},
}

var _ = Describe("list versions", func() {
	var (
		h               *handler
		err             error
		db              *gorm.DB
		dbName          string
		logger          logrus.FieldLogger
		mockRelease     *oc.MockRelease
		versions        Versions
		osImages        *models.OsImages
		releaseImages   *models.ReleaseImages
		cpuArchitecture string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		db, dbName = common.PrepareTestDB()

		logger = logrus.New()
		osImages = &models.OsImages{}
		releaseImages = &models.ReleaseImages{}
		cpuArchitecture = common.TestDefaultConfig.CPUArchitecture
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("ListComponentVersions", func() {
		It("default values", func() {
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListComponentVersions(context.Background(), operations.V2ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListComponentVersionsOK()))
			val, _ := reply.(*operations.V2ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).
				Should(Equal("Unknown"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("quay.io/edge-infrastructure/assisted-installer-agent:latest"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("quay.io/edge-infrastructure/assisted-installer:latest"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})

		It("mix default and non default", func() {
			os.Setenv("SELF_VERSION", "self-version")
			os.Setenv("AGENT_DOCKER_IMAGE", "agent-image")
			os.Setenv("INSTALLER_IMAGE", "installer-image")
			os.Setenv("CONTROLLER_IMAGE", "controller-image")
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListComponentVersions(context.Background(), operations.V2ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListComponentVersionsOK()))
			val, _ := reply.(*operations.V2ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).Should(Equal("self-version"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("agent-image"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("installer-image"))
			Expect(val.Payload.Versions["assisted-installer-controller"]).Should(Equal("controller-image"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})
	})

	Context("ListSupportedOpenshiftVersions", func() {
		readDefaultOsImages := func() {
			var bytes []byte
			bytes, err = ioutil.ReadFile("../../data/default_os_images.json")
			Expect(err).ShouldNot(HaveOccurred())
			err = json.Unmarshal(bytes, osImages)
			Expect(err).ShouldNot(HaveOccurred())
		}

		readDefaultReleaseImages := func() {
			var bytes []byte
			bytes, err = ioutil.ReadFile("../../data/default_release_images.json")
			Expect(err).ShouldNot(HaveOccurred())
			err = json.Unmarshal(bytes, releaseImages)
			Expect(err).ShouldNot(HaveOccurred())
		}

		It("get_defaults from data directory", func() {
			readDefaultOsImages()
			readDefaultReleaseImages()

			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			for _, releaseImage := range *releaseImages {
				key := *releaseImage.OpenshiftVersion
				version := val.Payload[key]
				architecture := *releaseImage.CPUArchitecture
				architectures := releaseImage.CPUArchitectures

				if len(architectures) == 0 && architecture != common.MultiCPUArchitecture {
					// For single-arch release we require in the test that there is a matching
					// OS image for the provided release image. Otherwise the whole release image
					// is not usable and indicates a mistake.
					if architecture == "" {
						architecture = common.CPUArchitecture
					}
					if architecture == common.CPUArchitecture {
						Expect(version.Default).Should(Equal(releaseImage.Default))
					}
					Expect(version.CPUArchitectures).Should(ContainElement(architecture))
					Expect(version.DisplayName).Should(Equal(releaseImage.Version))
					Expect(version.SupportLevel).Should(Equal(h.getSupportLevel(*releaseImage)))
				} else {
					// For multi-arch release we don't require a strict matching for every
					// architecture supported by this image. As long as we have at least one OS
					// image that matches, we are okay. This is to allow setups where release
					// image supports more architectures than we have available RHCOS images.
					Expect(len(version.CPUArchitectures)).ShouldNot(Equal(0))
					Expect(version.DisplayName).Should(Equal(releaseImage.Version))
					Expect(version.SupportLevel).Should(Equal(h.getSupportLevel(*releaseImage)))
				}
			}
		})

		It("getSupportLevel", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage := models.ReleaseImage{
				CPUArchitecture:  &cpuArchitecture,
				OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
				URL:              &common.TestDefaultConfig.ReleaseImageUrl,
				Version:          &common.TestDefaultConfig.ReleaseVersion,
			}

			// Production release version
			releaseImage.Version = swag.String("4.8.12")
			Expect(*h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))

			// Beta release version
			releaseImage.Version = swag.String("4.9.0-rc.4")
			Expect(*h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelBeta))

			// Support level specified in release image
			releaseImage.SupportLevel = models.OpenshiftVersionSupportLevelProduction
			Expect(*h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))
		})

		It("missing release images", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, models.ReleaseImages{}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).Should(BeEmpty())
		})

		It("release image without cpu_architectures field", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, models.ReleaseImages{
				// This image uses a syntax with missing "cpu_architectures". It is crafted
				// in order to make sure the change in MGMT-11494 is backwards-compatible.
				&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{},
					OpenshiftVersion: swag.String("4.11.1"),
					URL:              swag.String("release_4.11.1"),
					Default:          true,
					Version:          swag.String("4.11.1-chocobomb-for-test"),
				},
			}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			version := val.Payload["4.11.1"]
			Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
			Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
			Expect(version.Default).Should(Equal(true))
		})

		It("release image without matching OS image", func() {
			readDefaultOsImages()
			h, err = NewHandler(logger, mockRelease, versions, *osImages, models.ReleaseImages{
				&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.PowerCPUArchitecture, "chocobomb-fake-architecture"},
					OpenshiftVersion: swag.String("4.11.1"),
					URL:              swag.String("release_4.11.1"),
					Default:          true,
					Version:          swag.String("4.11.1-chocobomb-for-test"),
				},
			}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			version := val.Payload["4.11.1"]
			Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
			Expect(version.CPUArchitectures).Should(ContainElement(common.PowerCPUArchitecture))
			Expect(version.CPUArchitectures).ShouldNot(ContainElement("chocobomb-fake-architecture"))
			Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
			Expect(version.Default).Should(Equal(true))
		})

		It("single-arch and multi-arch for the same version", func() {
			readDefaultOsImages()
			h, err = NewHandler(logger, mockRelease, versions, *osImages, models.ReleaseImages{
				// Those images provide the same architecture using single-arch as well as multi-arch
				// release images. This is to test if in this scenario we don't return duplicated
				// entries in the supported architectures list.
				&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
					CPUArchitectures: []string{common.ARM64CPUArchitecture},
					OpenshiftVersion: swag.String("4.11.1"),
					URL:              swag.String("release_4.11.1"),
					Version:          swag.String("4.11.1-chocobomb-for-test"),
				},
				&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
					OpenshiftVersion: swag.String("4.11.1"),
					URL:              swag.String("release_4.11.1"),
					Default:          true,
					Version:          swag.String("4.11.1-chocobomb-for-test"),
				},
			}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			version := val.Payload["4.11.1"]
			Expect(version.CPUArchitectures).Should(ContainElement(common.ARM64CPUArchitecture))
			Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
			Expect(len(version.CPUArchitectures)).Should(Equal(2))
			Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
			Expect(version.Default).Should(Equal(true))
		})
	})

	Context("GetOsImage", func() {
		var (
			osImage              *models.OsImage
			architectures        []string
			patchVersionOsImages = models.OsImages{
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
		)

		BeforeEach(func() {
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("unsupported openshiftVersion", func() {
			osImage, err = h.GetOsImage("unsupported", common.TestDefaultConfig.CPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(osImage).Should(BeNil())
		})

		It("unsupported cpuArchitecture", func() {
			osImage, err = h.GetOsImage(common.TestDefaultConfig.OpenShiftVersion, "unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(osImage).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("isn't specified in OS images list"))
		})

		It("empty architecture fallback to default", func() {
			osImage, err = h.GetOsImage("4.9", "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.CPUArchitecture).Should(Equal(common.DefaultCPUArchitecture))
		})

		It("multiarch returns error", func() {
			osImage, err = h.GetOsImage("4.11", common.MultiCPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(osImage).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("isn't specified in OS images list"))
		})

		It("fetch OS image by major.minor", func() {
			osImage, err = h.GetOsImage("4.9", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.9"))
		})

		It("fetch missing major.minor.patch - find latest patch version by major.minor", func() {
			h, err = NewHandler(logger, mockRelease, versions, patchVersionOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetOsImage("4.10.1", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.10.10"))
		})

		It("missing major.minor - find latest patch version by major.minor", func() {
			h, err = NewHandler(logger, mockRelease, versions, patchVersionOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetOsImage("4.10", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.10.10"))
		})

		It("get from OsImages", func() {
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for _, key := range h.GetOpenshiftVersions() {
				architectures = h.GetCPUArchitectures(key)

				for _, architecture := range architectures {
					osImage, err = h.GetOsImage(key, architecture)
					Expect(err).ShouldNot(HaveOccurred())

					for _, rhcos := range *osImages {
						if *rhcos.OpenshiftVersion == key && *rhcos.CPUArchitecture == architecture {
							Expect(osImage).Should(Equal(rhcos))
						}
					}
				}
			}
		})
	})

	Context("GetReleaseImage", func() {
		var (
			releaseImage  *models.ReleaseImage
			architectures []string
		)

		BeforeEach(func() {
			releaseImages = &defaultReleaseImages
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("unsupported openshiftVersion", func() {
			releaseImage, err = h.GetReleaseImage("unsupported", common.TestDefaultConfig.CPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("unsupported cpuArchitecture", func() {
			releaseImage, err = h.GetReleaseImage(common.TestDefaultConfig.OpenShiftVersion, "unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("isn't specified in release images list"))
		})

		It("empty openshiftVersion", func() {
			releaseImage, err = h.GetReleaseImage("", common.TestDefaultConfig.CPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("empty cpuArchitecture", func() {
			releaseImage, err = h.GetReleaseImage(common.TestDefaultConfig.OpenShiftVersion, "")
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeNil())
		})

		It("fetch release image by major.minor", func() {
			releaseImage, err = h.GetReleaseImage("4.9", common.DefaultCPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
			Expect(*releaseImage.Version).Should(Equal("4.9-candidate"))
		})

		It("get from ReleaseImages", func() {
			for _, key := range h.GetOpenshiftVersions() {
				architectures = h.GetCPUArchitectures(key)

				for _, architecture := range architectures {
					releaseImage, err = h.GetReleaseImage(key, architecture)
					if err != nil {
						releaseImage, err = h.GetReleaseImage(key, common.MultiCPUArchitecture)
						Expect(err).ShouldNot(HaveOccurred())
					}

					for _, release := range *releaseImages {
						if *release.OpenshiftVersion == key && *release.CPUArchitecture == architecture {
							Expect(releaseImage).Should(Equal(release))
						}
					}
				}
			}
		})

		Context("for single-arch release image", func() {
			It("gets successfuly image with old syntax", func() {
				releaseImage, err = h.GetReleaseImage("4.11.2", "fake-architecture-chocobomb")
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.2"))
				Expect(*releaseImage.Version).Should(Equal("4.11.2-fake-chocobomb"))
			})

			It("gets successfuly image with new syntax", func() {
				releaseImage, err = h.GetReleaseImage("4.10.1", common.X86CPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.10.1"))
				Expect(*releaseImage.Version).Should(Equal("4.10.1-candidate"))
			})
		})

		Context("for multi-arch release image", func() {
			It("gets successfuly image using generic multiarch query", func() {
				releaseImage, err = h.GetReleaseImage("4.11.1", common.MultiCPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.1"))
				Expect(*releaseImage.Version).Should(Equal("4.11.1-multi"))
			})
			It("gets successfuly image using sub-architecture", func() {
				releaseImage, err = h.GetReleaseImage("4.11.1", common.PowerCPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.1"))
				Expect(*releaseImage.Version).Should(Equal("4.11.1-multi"))
			})
		})
	})

	Context("GetDefaultReleaseImage", func() {
		var (
			releaseImage *models.ReleaseImage
		)

		It("Default release image exists", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, defaultReleaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			releaseImage, err = h.GetDefaultReleaseImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Default).Should(Equal(true))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
			Expect(*releaseImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})

		It("Missing default release image", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, models.ReleaseImages{}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			releaseImage, err = h.GetDefaultReleaseImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal("Default release image is not available"))
		})
	})

	Context("GetMustGatherImages", func() {
		var (
			pullSecret = "test_pull_secret"
			ocpVersion = "4.8.0-fc.1"
			keyVersion = "4.8"
			mirror     = "release-mirror"
			images     MustGatherVersion
		)

		BeforeEach(func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, defaultReleaseImages, mustgatherImages, mirror)
			Expect(err).ShouldNot(HaveOccurred())
		})

		verifyOcpVersion := func(images MustGatherVersion, size int) {
			Expect(len(images)).To(Equal(size))
			Expect(images["ocp"]).To(Equal("blah"))
		}

		It("happy flow", func() {
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
			images, err = h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			verifyOcpVersion(images, 4)
			Expect(images["lso"]).To(Equal(mustgatherImages[keyVersion]["lso"]))
		})

		It("unsupported_key", func() {
			images, err = h.GetMustGatherImages("unsupported", cpuArchitecture, pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(images).Should(BeEmpty())
		})

		It("caching", func() {
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
			images, err = h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 4)

			images, err = h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 4)
		})

		It("missing release image", func() {
			images, err = h.GetMustGatherImages("4.7", cpuArchitecture, pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("isn't specified in release images list"))
			Expect(images).Should(BeEmpty())
		})
	})

	Context("AddReleaseImage", func() {
		var (
			pullSecret            = "test_pull_secret"
			releaseImageUrl       = "releaseImage"
			customOcpVersion      = "4.8.0"
			existingOcpVersion    = "4.9.1"
			releaseImageFromCache *models.ReleaseImage
			releaseImage          *models.ReleaseImage
		)

		BeforeEach(func() {
			osImages = &defaultOsImages
			releaseImages = &defaultReleaseImages
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("for single-arch release image", func() {
			It("added successfully", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

				releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
				Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture}))
				Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
				Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
				Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
			})

			It("added successfuly using specified ocpReleaseVersion and cpuArchitecture", func() {
				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, customOcpVersion, []string{cpuArchitecture})
				Expect(err).ShouldNot(HaveOccurred())
				releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))
				Expect(*releaseImageFromCache.Version).Should(Equal(customOcpVersion))
				Expect(*releaseImageFromCache.CPUArchitecture).Should(Equal(cpuArchitecture))
				Expect(releaseImageFromCache.CPUArchitectures).Should(Equal([]string{cpuArchitecture}))
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

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).ShouldNot(HaveOccurred())

				releaseImage, err = h.GetReleaseImage(existingOcpVersion, cpuArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
			})

			It("fails when missing OS image", func() {
				ocpVersion := "4.7"
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

				_, err = h.AddReleaseImage("invalidRelease", pullSecret, "", nil)
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(Equal(fmt.Sprintf("No OS images are available for version %s and architecture %s", ocpVersion, cpuArchitecture)))
			})
		})

		Context("for multi-arch release image", func() {
			It("added successfully", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

				releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(*releaseImage.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
				Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture, common.ARM64CPUArchitecture}))
				Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
				Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
				Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
			})

			It("added successfuly using specified ocpReleaseVersion and cpuArchitecture", func() {
				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, customOcpVersion, []string{cpuArchitecture, common.ARM64CPUArchitecture})
				Expect(err).ShouldNot(HaveOccurred())
				releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, common.MultiCPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))
				Expect(*releaseImageFromCache.Version).Should(Equal(customOcpVersion))
				Expect(*releaseImageFromCache.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
				Expect(releaseImageFromCache.CPUArchitectures).Should(Equal([]string{cpuArchitecture, common.ARM64CPUArchitecture}))
			})

			It("added successfuly and recalculated using specified ocpReleaseVersion and 'multiarch' cpuArchitecture", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, customOcpVersion, []string{common.MultiCPUArchitecture})
				Expect(err).ShouldNot(HaveOccurred())
				releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, common.MultiCPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())

				Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))
				Expect(*releaseImageFromCache.Version).Should(Equal(customOcpVersion))
				Expect(*releaseImageFromCache.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
				Expect(releaseImageFromCache.CPUArchitectures).Should(Equal([]string{cpuArchitecture, common.ARM64CPUArchitecture}))
			})

			It("when release image already exists", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("4.11.1", nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

				releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
					return *releaseImage.OpenshiftVersion == "4.11.1" && *releaseImage.CPUArchitecture == common.MultiCPUArchitecture
				})
				Expect(releaseImageFromCache).ShouldNot(BeNil())

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).ShouldNot(HaveOccurred())

				// Query for multi-arch release image using generic multiarch
				releaseImage, err = h.GetReleaseImage("4.11.1", common.MultiCPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))

				// Query for multi-arch release image using specific arch
				releaseImage, err = h.GetReleaseImage("4.11.1", common.X86CPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
				releaseImage, err = h.GetReleaseImage("4.11.1", common.ARM64CPUArchitecture)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))

				// Query for non-existing architecture
				releaseImage, err = h.GetReleaseImage("4.11.1", "architecture-chocobomb")
				Expect(err.Error()).Should(Equal("The requested CPU architecture (architecture-chocobomb) isn't specified in release images list"))
			})
		})

		Context("with failing OCP version extraction", func() {
			It("using default syntax", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).Should(HaveOccurred())
			})

			It("using specified cpuArchitectures", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", []string{cpuArchitecture})
				Expect(err).Should(HaveOccurred())
			})
		})

		Context("with failing architecture extraction", func() {
			It("using default syntax", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error when getting architecture")).AnyTimes()

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
				Expect(err).Should(HaveOccurred())
			})

			It("using specified ocpReleaseVersion", func() {
				mockRelease.EXPECT().GetOpenshiftVersion(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
				mockRelease.EXPECT().GetReleaseArchitecture(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error when getting architecture")).AnyTimes()

				_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, customOcpVersion, nil)
				Expect(err).Should(HaveOccurred())
			})
		})

		It("keep support level from cache", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", nil)
			Expect(err).ShouldNot(HaveOccurred())
			releaseImage, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("GetLatestOsImage", func() {
		var (
			osImage *models.OsImage
		)

		It("only one OS image", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages[0:1], *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.11.1"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})

		It("Multiple OS images", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.11.1"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})

		It("fails to get OS images for multiarch", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.MultiCPUArchitecture)
			Expect(err).Should(HaveOccurred())
			Expect(osImage).Should(BeNil())
			Expect(err.Error()).To(ContainSubstring("No OS images are available"))
		})
	})

	Context("validateVersions", func() {
		BeforeEach(func() {
			osImages = &models.OsImages{
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

			releaseImages = &models.ReleaseImages{
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
		})

		It("OS images specified", func() {
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("only OpenShift versions specified", func() {
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("missing URL in OS images", func() {
			(*osImages)[0].URL = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url"))
		})

		It("missing Version in OS images", func() {
			(*osImages)[0].Version = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version"))
		})

		It("missing CPUArchitecture in Release images", func() {
			(*releaseImages)[0].CPUArchitecture = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpu_architecture"))
		})

		It("missing URL in Release images", func() {
			(*releaseImages)[0].URL = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url"))
		})

		It("missing Version in Release images", func() {
			(*releaseImages)[0].Version = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version"))
		})

		It("empty osImages and openshiftVersions", func() {
			h, err = NewHandler(logger, mockRelease, versions, models.OsImages{}, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("No OS images are available"))
		})
	})

	Context("GetCPUArchitectures", func() {
		var (
			architectures []string
		)

		BeforeEach(func() {
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("unsupported version", func() {
			architectures = h.GetCPUArchitectures("unsupported")
		})

		It("multiple CPU architectures", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, defaultReleaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			architectures = h.GetCPUArchitectures("4.9")
			Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture, common.ARM64CPUArchitecture}))

			architectures = h.GetCPUArchitectures("4.9.1")
			Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture, common.ARM64CPUArchitecture}))
		})

		It("empty architecture fallback to default", func() {
			osImages = &models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String(""),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					Version:          swag.String("version-49.123-0"),
				},
				&models.OsImage{
					CPUArchitecture:  nil,
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					Version:          swag.String("version-49.123-0"),
				},
			}
			h, err = NewHandler(logger, mockRelease, versions, *osImages, models.ReleaseImages{}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for _, key := range h.GetOpenshiftVersions() {
				architectures = h.GetCPUArchitectures(key)
				Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture}))
			}
		})
	})
})

var _ = Describe("list versions", func() {
	var (
		h      *handler
		err    error
		db     *gorm.DB
		dbName string
	)
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease := oc.NewMockRelease(ctrl)

		var versions Versions
		Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())

		db, dbName = common.PrepareTestDB()
		logger := logrus.New()
		h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, models.ReleaseImages{}, nil, "")
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("positive", func() {
		res, err := h.getKey("4.6")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.getKey("4.6.9")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.getKey("4.6.9-beta")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("negative", func() {
		res, err := h.getKey("ere.654.45")
		Expect(err).Should(HaveOccurred())
		Expect(res).Should(Equal(""))
	})
})
