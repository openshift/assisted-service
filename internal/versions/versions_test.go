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
	"gopkg.in/square/go-jose.v2/json"
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "versions")
}

var defaultOpenShiftVersions = models.OpenshiftVersions{
	"4.5": models.OpenshiftVersion{
		DisplayName:  "4.5.1",
		ReleaseImage: "release_4.5", ReleaseVersion: "4.5.1",
		RhcosImage: "rhcos_4.5", RhcosRootfs: "rhcos_rootfs_4.5",
		RhcosVersion: "version-45.123-0",
		SupportLevel: "oldie",
	},
	"4.6": models.OpenshiftVersion{
		DisplayName:  "4.6-candidate",
		ReleaseImage: "release_4.6", ReleaseVersion: "4.6-candidate",
		RhcosImage: "rhcos_4.6", RhcosRootfs: "rhcos_rootfs_4.6",
		RhcosVersion: "version-46.123-0",
		SupportLevel: "newbie",
	},
}

var newOpenShiftVersions = models.OpenshiftVersions{
	"4.9": models.OpenshiftVersion{
		DisplayName:  "4.9-candidate",
		ReleaseImage: "release_4.9", ReleaseVersion: "4.9-candidate",
		SupportLevel: "newbie",
	},
}

var defaultOsImages = models.OsImages{
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
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("rhcos_4.8"),
		RootfsURL:        swag.String("rhcos_rootfs_4.8"),
		Version:          swag.String("version-48.123-0"),
	},
}

var defaultReleaseImages = models.ReleaseImages{
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

var supportedCustomOpenShiftVersions = models.OpenshiftVersions{
	"4.8": models.OpenshiftVersion{
		DisplayName:  "4.8.0",
		ReleaseImage: "release_4.8", ReleaseVersion: "4.8.0-fc.1",
		RhcosImage: "rhcos_4.8", RhcosRootfs: "rhcos_rootfs_4.8",
		RhcosVersion: "version-48.123-0",
		SupportLevel: models.OpenshiftVersionSupportLevelBeta,
	},
}

var mustgatherImages = MustGatherVersions{
	"4.8": MustGatherVersion{
		"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
		"ocs": "registry.redhat.io/ocs4/ocs-must-gather-rhel8",
		"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
	},
}

var _ = Describe("list versions", func() {
	var (
		h                 *handler
		err               error
		logger            logrus.FieldLogger
		mockRelease       *oc.MockRelease
		versions          Versions
		openshiftVersions *models.OpenshiftVersions
		osImages          *models.OsImages
		releaseImages     *models.ReleaseImages
		cpuArchitecture   string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		logger = logrus.New()
		openshiftVersions = &models.OpenshiftVersions{}
		osImages = &models.OsImages{}
		releaseImages = &models.ReleaseImages{}
		cpuArchitecture = common.TestDefaultConfig.CPUArchitecture
	})

	Context("ListComponentVersions", func() {
		It("default values", func() {
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListComponentVersionsOK()))
			val, _ := reply.(*operations.V2ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).
				Should(Equal("quay.io/ocpmetal/assisted-service:latest"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("quay.io/ocpmetal/agent:latest"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("quay.io/ocpmetal/assisted-installer:latest"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})

		It("mix default and non default", func() {
			os.Setenv("SELF_VERSION", "self-version")
			os.Setenv("AGENT_DOCKER_IMAGE", "agent-image")
			os.Setenv("INSTALLER_IMAGE", "installer-image")
			os.Setenv("CONTROLLER_IMAGE", "controller-image")
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
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
		It("empty", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(BeEmpty())
		})

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

			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

			for key, version := range val.Payload {
				releaseImage, err1 := h.GetReleaseImage(key, common.DefaultCPUArchitecture)
				Expect(err1).ShouldNot(HaveOccurred())
				architectures, err1 := h.GetCPUArchitectures(key)
				Expect(err1).ShouldNot(HaveOccurred())

				Expect(version.CPUArchitectures).Should(Equal(architectures))
				Expect(version.Default).Should(Equal(releaseImage.Default))
				Expect(version.DisplayName).Should(Equal(*releaseImage.Version))
				Expect(version.SupportLevel).Should(Equal(h.getSupportLevel(*releaseImage)))
			}
		})

		It("getSupportLevel", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage := models.ReleaseImage{
				CPUArchitecture:  &cpuArchitecture,
				OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
				URL:              &common.TestDefaultConfig.ReleaseImageUrl,
				Version:          &common.TestDefaultConfig.ReleaseVersion,
			}

			// Production release version
			releaseImage.Version = swag.String("4.8.12")
			Expect(h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))

			// Beta release version
			releaseImage.Version = swag.String("4.9.0-rc.4")
			Expect(h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelBeta))

			// Support level specified in release image
			releaseImage.SupportLevel = models.OpenshiftVersionSupportLevelProduction
			Expect(h.getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))
		})
	})

	Context("GetOsImage", func() {
		var (
			osImage       *models.OsImage
			architectures []string
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("default", func() {
			for key := range *openshiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())

				for _, architecture := range architectures {
					currKey := key
					osImage, err = h.GetOsImage(currKey, architecture)
					Expect(err).ShouldNot(HaveOccurred())

					ocpVersion := (*openshiftVersions)[currKey]
					Expect(*osImage).Should(Equal(models.OsImage{
						OpenshiftVersion: &currKey,
						URL:              &ocpVersion.RhcosImage,
						RootfsURL:        &ocpVersion.RhcosRootfs,
						Version:          &ocpVersion.RhcosVersion,
					}))
				}
			}
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

		It("get from OsImages", func() {
			openshiftVersions = &newOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for key := range *openshiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())

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
			openshiftVersions = &defaultOpenShiftVersions
			releaseImages = &defaultReleaseImages
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("default", func() {
			for key := range *openshiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())

				for _, architecture := range architectures {
					currKey := key
					currArch := architecture
					releaseImage, err = h.GetReleaseImage(currKey, currArch)
					Expect(err).ShouldNot(HaveOccurred())

					ocpVersion := (*openshiftVersions)[currKey]
					Expect(*releaseImage).Should(Equal(models.ReleaseImage{
						CPUArchitecture:  &currArch,
						OpenshiftVersion: &currKey,
						URL:              &ocpVersion.ReleaseImage,
						Version:          &ocpVersion.ReleaseVersion,
					}))
				}
			}
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
			Expect(err.Error()).To(ContainSubstring("isn't specified in Release images list"))
		})

		It("get from ReleaseImages", func() {
			openshiftVersions = &newOpenShiftVersions
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for key := range *openshiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())

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

	Context("IsOpenshiftVersionSupported", func() {
		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("positive", func() {
			for key := range *openshiftVersions {
				Expect(h.isOpenshiftVersionSupported(key)).Should(BeTrue())
			}
		})

		It("negative", func() {
			Expect(h.isOpenshiftVersionSupported("unknown")).Should(BeFalse())
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
			openshiftVersions = &supportedCustomOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, mustgatherImages, mirror)
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
			openshiftVersions = &defaultOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, mustgatherImages, mirror)
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.5", mirror, pullSecret).Return("blah", nil).Times(1)
			images, err = h.GetMustGatherImages("4.5.1", cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 1)

			images, err = h.GetMustGatherImages("4.5.1", cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 1)
		})
	})

	Context("AddReleaseImage", func() {
		var (
			pullSecret            = "test_pull_secret"
			releaseImageUrl       = "releaseImage"
			customOcpVersion      = "4.8.0-fc.1"
			customKeyVersion      = "4.8"
			versionFromCache      models.OpenshiftVersion
			releaseImageFromCache *models.ReleaseImage
			releaseImage          *models.ReleaseImage
		)

		It("added release image successfully", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			versionFromCache = h.openshiftVersions[customKeyVersion]
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customKeyVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
			Expect(h.GetOsImage(customKeyVersion, common.TestDefaultConfig.CPUArchitecture)).Should(Equal(&models.OsImage{
				OpenshiftVersion: &customKeyVersion,
				URL:              &versionFromCache.RhcosImage,
				RootfsURL:        &versionFromCache.RhcosRootfs,
				Version:          &versionFromCache.RhcosVersion,
			}))
		})

		It("added release image successfully - empty openshiftVersions", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customKeyVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("override version successfully", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))

			// Override version with a new release image
			releaseImageUrl = "newReleaseImage"
			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			releaseImageFromCache, err = h.GetReleaseImage(customOcpVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*releaseImageFromCache.URL).Should(Equal(releaseImageUrl))
		})

		It("keep support level from cache", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			versionFromCache = h.openshiftVersions[customKeyVersion]
			releaseImage, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			releaseImage, err = h.GetReleaseImage(customKeyVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(versionFromCache.SupportLevel).Should(Equal(h.openshiftVersions[customKeyVersion].SupportLevel))
		})

		It("failed getting version from release", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).Should(HaveOccurred())
		})

		It("missing from OS images", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			_, err = h.AddReleaseImage("invalidRelease", pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal(fmt.Sprintf("No OS images are available for version: %s", customKeyVersion)))
		})

		It("release image already exists", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any()).Return(cpuArchitecture, nil).AnyTimes()

			var releaseImageFromCache *models.ReleaseImage
			releaseImageFromCache, err = h.GetReleaseImage(customKeyVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())

			_, err = h.AddReleaseImage(releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage, err = h.GetReleaseImage(customKeyVersion, cpuArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.Version))
		})
	})

	Context("GetLatestOsImage", func() {
		var (
			osImage *models.OsImage
		)

		It("No OS images", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			_, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err.Error()).To(ContainSubstring("No OS images are available"))
		})

		It("only one OS image", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, defaultOsImages[:1], *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.9"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})

		It("Multiple OS images", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, defaultOsImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImage, err = h.GetLatestOsImage(common.TestDefaultConfig.CPUArchitecture)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*osImage.OpenshiftVersion).Should(Equal("4.9"))
			Expect(*osImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
		})
	})

	Context("validateVersions", func() {
		var (
			openshiftVersions *models.OpenshiftVersions
		)

		BeforeEach(func() {
			openshiftVersions = &models.OpenshiftVersions{
				"4.8": models.OpenshiftVersion{
					DisplayName:  "4.8-candidate",
					ReleaseImage: "release_4.8", ReleaseVersion: "4.8-candidate",
					RhcosImage: "rhcos_4.8", RhcosRootfs: "rhcos_rootfs_4.8",
					RhcosVersion: "version-48.123-0",
					SupportLevel: "newbie",
				},
			}

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
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("only OpenShift versions specified", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("missing URL in OS images", func() {
			(*osImages)[0].URL = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url"))
		})

		It("missing Rootfs in OS images", func() {
			(*osImages)[0].RootfsURL = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rootfs_url"))
		})

		It("missing Version in OS images", func() {
			(*osImages)[0].Version = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version"))
		})

		It("missing CPUArchitecture in Release images", func() {
			(*releaseImages)[0].CPUArchitecture = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpu_architecture"))
		})

		It("missing URL in Release images", func() {
			(*releaseImages)[0].URL = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url"))
		})

		It("missing Version in Release images", func() {
			(*releaseImages)[0].Version = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version"))
		})
	})

	Context("GetCPUArchitectures", func() {
		var (
			architectures []string
		)

		BeforeEach(func() {
			h, err = NewHandler(logger, mockRelease, versions, defaultOpenShiftVersions, *osImages, *releaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			osImages = &defaultOsImages
		})

		It("releaseImages not defined", func() {
			for key := range defaultOpenShiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture}))
			}
		})

		It("unsupported version", func() {
			architectures, err = h.GetCPUArchitectures("unsupported")
			Expect(err).Should(HaveOccurred())
		})

		It("multiple CPU architectures", func() {
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, defaultOsImages, defaultReleaseImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			for key := range newOpenShiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(architectures).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture, "arm64"}))
			}
		})

		It("empty architecture fallback to default", func() {
			empty := ""
			osImages = &defaultOsImages
			(*osImages)[0].CPUArchitecture = &empty
			(*osImages)[1].CPUArchitecture = nil
			h, err = NewHandler(logger, mockRelease, versions, models.OpenshiftVersions{}, *osImages, models.ReleaseImages{}, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for key := range newOpenShiftVersions {
				architectures, err = h.GetCPUArchitectures(key)
				Expect(err).ShouldNot(HaveOccurred())
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
		h, err = NewHandler(logger, mockRelease, versions, models.OpenshiftVersions{}, models.OsImages{}, models.ReleaseImages{}, nil, "")
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
