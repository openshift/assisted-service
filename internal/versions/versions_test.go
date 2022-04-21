package versions

import (
	"context"
	"errors"
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
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gopkg.in/square/go-jose.v2/json"
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "versions")
}

var defaultOsImages = models.OsImages{
	&models.OsImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("rhcos_4.10"),
		RootfsURL:        swag.String("rhcos_rootfs_4.10"),
		Version:          swag.String("version-410.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("rhcos_4.9"),
		RootfsURL:        swag.String("rhcos_rootfs_4.9"),
		Version:          swag.String("version-49.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String("arm64"),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("rhcos_4.9_arm64"),
		RootfsURL:        swag.String("rhcos_rootfs_4.9_arm64"),
		Version:          swag.String("version-49.123-0_arm64"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("rhcos_4.91"),
		RootfsURL:        swag.String("rhcos_rootfs_4.91"),
		Version:          swag.String("version-491.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("rhcos_4.8"),
		RootfsURL:        swag.String("rhcos_rootfs_4.8"),
		Version:          swag.String("version-48.123-0"),
	},
}

var defaultReleaseImages = models.ReleaseImages{
	&models.ReleaseImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("release_4.10.1"),
		Version:          swag.String("4.10.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9"),
		Version:          swag.String("4.9-candidate"),
		Default:          true,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String("arm64"),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9_arm64"),
		Version:          swag.String("4.9-candidate_arm64"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String("x86_64"),
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("release_4.9.1"),
		Version:          swag.String("4.9.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String("x86_64"),
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

		logger = logrus.New()
		osImages = &models.OsImages{}
		releaseImages = &models.ReleaseImages{}
		cpuArchitecture = common.TestDefaultConfig.CPUArchitecture
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

		It("get_defaults", func() {
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
				if architecture == "" {
					architecture = common.CPUArchitecture
				}
				if architecture == common.CPUArchitecture {
					Expect(version.Default).Should(Equal(releaseImage.Default))
				}
				Expect(version.CPUArchitectures).Should(ContainElement(architecture))
				Expect(version.DisplayName).Should(Equal(releaseImage.Version))
				Expect(version.SupportLevel).Should(Equal(h.getSupportLevel(*releaseImage)))
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
	})

	Context("GetOsImage", func() {
		var (
			osImage              *models.OsImage
			architectures        []string
			patchVersionOsImages = models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String("x86_64"),
					OpenshiftVersion: swag.String("4.10.10"),
					URL:              swag.String("rhcos_4.10.2"),
					RootfsURL:        swag.String("rhcos_rootfs_4.10.2"),
					Version:          swag.String("version-4102.123-0"),
				},
				&models.OsImage{
					CPUArchitecture:  swag.String("x86_64"),
					OpenshiftVersion: swag.String("4.10.9"),
					URL:              swag.String("rhcos_4.10.1"),
					RootfsURL:        swag.String("rhcos_rootfs_4.10.1"),
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
			Expect(err.Error()).To(ContainSubstring("isn't specified"))
		})

		It("empty architecture fallback to default", func() {
			osImage, err = h.GetOsImage("4.9", "")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.CPUArchitecture).Should(Equal(common.DefaultCPUArchitecture))
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
					Expect(err).ShouldNot(HaveOccurred())

					for _, release := range *releaseImages {
						if *release.OpenshiftVersion == key && *release.CPUArchitecture == architecture {
							Expect(releaseImage).Should(Equal(release))
						}
					}
				}
			}
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

		It("added release image successfully", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", "")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("added release image successfully - empty openshiftVersions", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", "")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("keep support level from cache", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", "")
			Expect(err).ShouldNot(HaveOccurred())
			releaseImage, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("failed getting version from release", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", "")
			Expect(err).Should(HaveOccurred())
		})

		It("missing from OS images", func() {
			ocpVersion := "4.7"
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			_, err = h.AddReleaseImage("invalidRelease", pullSecret, "", "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal(fmt.Sprintf("No OS images are available for version: %s", ocpVersion)))
		})

		It("release image already exists", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(existingOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
				return *releaseImage.OpenshiftVersion == existingOcpVersion && *releaseImage.CPUArchitecture == cpuArchitecture
			})
			Expect(releaseImageFromCache).ShouldNot(BeNil())

			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, "", "")
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage, err = h.GetReleaseImage(existingOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
		})

		It("use specified ocpReleaseVersion and cpuArchitecture", func() {
			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret, customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImageFromCache.Version).Should(Equal(customOcpVersion))
			Expect(*releaseImageFromCache.CPUArchitecture).Should(Equal(cpuArchitecture))
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
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.10.1"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})

		It("Multiple OS images", func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.10.1"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})
	})

	Context("validateVersions", func() {
		BeforeEach(func() {
			osImages = &models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String("x86_64"),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					RootfsURL:        swag.String("rhcos_rootfs_4.9"),
					Version:          swag.String("version-49.123-0"),
				},
				&models.OsImage{
					CPUArchitecture:  swag.String("arm64"),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9_arm64"),
					RootfsURL:        swag.String("rhcos_rootfs_4.9_arm64"),
					Version:          swag.String("version-49.123-0_arm64"),
				},
			}

			releaseImages = &models.ReleaseImages{
				&models.ReleaseImage{
					CPUArchitecture:  swag.String("x86_64"),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("release_4.9"),
					Version:          swag.String("4.9-candidate"),
				},
				&models.ReleaseImage{
					CPUArchitecture:  swag.String("arm64"),
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

		It("missing Rootfs in OS images", func() {
			(*osImages)[0].RootfsURL = nil
			h, err = NewHandler(logger, mockRelease, versions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rootfs_url"))
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
			Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture, "arm64"}))

			architectures = h.GetCPUArchitectures("4.9.1")
			Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture, "arm64"}))
		})

		It("empty architecture fallback to default", func() {
			osImages = &models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String(""),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					RootfsURL:        swag.String("rhcos_rootfs_4.9"),
					Version:          swag.String("version-49.123-0"),
				},
				&models.OsImage{
					CPUArchitecture:  nil,
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					RootfsURL:        swag.String("rhcos_rootfs_4.9"),
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
		h   *handler
		err error
	)
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease := oc.NewMockRelease(ctrl)

		var versions Versions
		Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())

		logger := logrus.New()
		h, err = NewHandler(logger, mockRelease, versions, defaultOsImages, models.ReleaseImages{}, nil, "")
		Expect(err).ShouldNot(HaveOccurred())
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
