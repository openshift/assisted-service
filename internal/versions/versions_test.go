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
		DisplayName:  swag.String("4.5.1"),
		ReleaseImage: swag.String("release_4.5"), ReleaseVersion: swag.String("4.5.1"),
		RhcosImage: swag.String("rhcos_4.5"), RhcosRootfs: swag.String("rhcos_rootfs_4.5"),
		RhcosVersion: swag.String("version-45.123-0"),
		SupportLevel: swag.String("oldie"),
	},
	"4.6": models.OpenshiftVersion{
		DisplayName:  swag.String("4.6-candidate"),
		ReleaseImage: swag.String("release_4.6"), ReleaseVersion: swag.String("4.6-candidate"),
		RhcosImage: swag.String("rhcos_4.6"), RhcosRootfs: swag.String("rhcos_rootfs_4.6"),
		RhcosVersion: swag.String("version-46.123-0"),
		SupportLevel: swag.String("newbie"),
	},
}

var newOpenShiftVersions = models.OpenshiftVersions{
	"4.9": models.OpenshiftVersion{
		DisplayName:  swag.String("4.9-candidate"),
		ReleaseImage: swag.String("release_4.9"), ReleaseVersion: swag.String("4.9-candidate"),
		SupportLevel: swag.String("newbie"),
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
}

var supportedCustomOpenShiftVersions = models.OpenshiftVersions{
	"4.8": models.OpenshiftVersion{
		DisplayName:  swag.String("4.8.0"),
		ReleaseImage: swag.String("release_4.8"), ReleaseVersion: swag.String("4.8.0-fc.1"),
		RhcosImage: swag.String("rhcos_4.8"), RhcosRootfs: swag.String("rhcos_rootfs_4.8"),
		RhcosVersion: swag.String("version-48.123-0"),
		SupportLevel: swag.String(models.OpenshiftVersionSupportLevelCustom),
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
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		logger = logrus.New()
		openshiftVersions = &models.OpenshiftVersions{}
		osImages = &models.OsImages{}
	})

	Context("ListComponentVersions", func() {
		It("default values", func() {
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
			val, _ := reply.(*operations.ListComponentVersionsOK)
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
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
			val, _ := reply.(*operations.ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).Should(Equal("self-version"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("agent-image"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("installer-image"))
			Expect(val.Payload.Versions["assisted-installer-controller"]).Should(Equal("controller-image"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})
	})

	Context("ListSupportedOpenshiftVersions", func() {
		It("empty", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(BeEmpty())
		})

		readDefaultOpenshiftVersions := func() {
			var bytes []byte
			bytes, err = ioutil.ReadFile("../../data/default_ocp_versions.json")
			Expect(err).ShouldNot(HaveOccurred())
			err = json.Unmarshal(bytes, openshiftVersions)
			Expect(err).ShouldNot(HaveOccurred())
		}

		It("get_defaults", func() {
			readDefaultOpenshiftVersions()
			CURRENT_DEFAULT_VERSION := "4.8" //keep align with default_ocp_versions.json

			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(HaveLen(len(*openshiftVersions)))

			for key, version := range val.Payload {
				Expect(version).Should(Equal((*openshiftVersions)[key]))
			}
			Expect((*openshiftVersions)[CURRENT_DEFAULT_VERSION].Default).To(BeTrue())
		})
	})

	Context("GetOsImage", func() {
		var (
			osImage *models.OsImage
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("default", func() {
			for key := range *openshiftVersions {
				currKey := key
				osImage, err = h.GetOsImage(currKey)
				Expect(err).ShouldNot(HaveOccurred())

				ocpVersion := (*openshiftVersions)[currKey]
				Expect(*osImage).Should(Equal(models.OsImage{
					OpenshiftVersion: &currKey,
					URL:              ocpVersion.RhcosImage,
					RootfsURL:        ocpVersion.RhcosRootfs,
					Version:          ocpVersion.RhcosVersion,
				}))
			}
		})

		It("unsupported_key", func() {
			osImage, err = h.GetOsImage("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(osImage).Should(BeNil())
		})

		It("get from OsImages", func() {
			openshiftVersions = &newOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			for key := range *openshiftVersions {
				osImage, err = h.GetOsImage(key)
				Expect(err).ShouldNot(HaveOccurred())

				for _, rhcos := range *osImages {
					if *rhcos.OpenshiftVersion == key {
						Expect(osImage).Should(Equal(rhcos))
					}
				}
			}
		})
	})

	Context("GetOpenshiftVersion", func() {
		var (
			version      *models.OpenshiftVersion
			testVersions models.OpenshiftVersions
			testVersion  models.OpenshiftVersion
			versionKey   = "4.8"
		)

		BeforeEach(func() {
			openshiftVersions = &supportedCustomOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			testVersions = models.OpenshiftVersions{}
			testVersion = models.OpenshiftVersion{
				DisplayName:    swag.String("4.8-candidate"),
				ReleaseImage:   swag.String("release_4.8"),
				ReleaseVersion: swag.String("4.8-candidate"),
				SupportLevel:   swag.String("newbie"),
				RhcosImage:     swag.String("rhcos_4.8"),
				RhcosRootfs:    swag.String("rhcos_rootfs_4.8"),
				RhcosVersion:   swag.String("version-48.123-0"),
			}
		})

		It("default", func() {
			for key := range *openshiftVersions {
				version, err = h.GetOpenshiftVersion(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*version).Should(Equal((*openshiftVersions)[key]))
			}
		})

		It("unsupported_key", func() {
			version, err = h.GetOpenshiftVersion("unsupported")
			Expect(err).Should(HaveOccurred())
		})

		It("missing DisplayName in OpenshiftVersion", func() {
			testVersion.DisplayName = nil
			testVersions[versionKey] = testVersion

			h, err = NewHandler(logger, mockRelease, versions, testVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = h.GetOpenshiftVersion(versionKey)
			Expect(err.Error()).To(ContainSubstring("DisplayName"))
		})

		It("missing ReleaseImage in OpenshiftVersion", func() {
			testVersion.ReleaseImage = nil
			testVersions[versionKey] = testVersion

			h, err = NewHandler(logger, mockRelease, versions, testVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = h.GetOpenshiftVersion(versionKey)
			Expect(err.Error()).To(ContainSubstring("ReleaseImage"))
		})

		It("missing ReleaseVersion in OpenshiftVersion", func() {
			testVersion.ReleaseVersion = nil
			testVersions[versionKey] = testVersion

			h, err = NewHandler(logger, mockRelease, versions, testVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = h.GetOpenshiftVersion(versionKey)
			Expect(err.Error()).To(ContainSubstring("ReleaseVersion"))
		})

		It("missing SupportLevel in OpenshiftVersion", func() {
			testVersion.SupportLevel = nil
			testVersions[versionKey] = testVersion

			h, err = NewHandler(logger, mockRelease, versions, testVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = h.GetOpenshiftVersion(versionKey)
			Expect(err.Error()).To(ContainSubstring("SupportLevel"))
		})
	})

	Context("IsOpenshiftVersionSupported", func() {
		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("positive", func() {
			for key := range *openshiftVersions {
				Expect(h.IsOpenshiftVersionSupported(key)).Should(BeTrue())
			}
		})

		It("negative", func() {
			Expect(h.IsOpenshiftVersionSupported("unknown")).Should(BeFalse())
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
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, mustgatherImages, mirror)
			Expect(err).ShouldNot(HaveOccurred())
		})

		verifyOcpVersion := func(images MustGatherVersion, size int) {
			Expect(len(images)).To(Equal(size))
			Expect(images["ocp"]).To(Equal("blah"))
		}

		It("happy flow", func() {
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
			images, err = h.GetMustGatherImages(ocpVersion, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			verifyOcpVersion(images, 4)
			Expect(images["lso"]).To(Equal(mustgatherImages[keyVersion]["lso"]))
		})

		It("unsupported_key", func() {
			images, err = h.GetMustGatherImages("unsupported", pullSecret)
			Expect(err).Should(HaveOccurred())
			Expect(images).Should(BeEmpty())
		})

		It("caching", func() {
			openshiftVersions = &defaultOpenShiftVersions
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, mustgatherImages, mirror)
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.5", mirror, pullSecret).Return("blah", nil).Times(1)
			images, err = h.GetMustGatherImages("4.5.1", pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 1)

			images, err = h.GetMustGatherImages("4.5.1", pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			verifyOcpVersion(images, 1)
		})
	})

	Context("AddOpenshiftVersion", func() {
		var (
			pullSecret              = "test_pull_secret"
			releaseImage            = "releaseImage"
			ocpVersion              = "4.7.0-fc.1"
			keyVersion              = "4.7"
			customOcpVersion        = "4.8.0-fc.1"
			customKeyVersion        = "4.8"
			customOpenShiftVersions models.OpenshiftVersions
			version                 *models.OpenshiftVersion
			versionFromCache        *models.OpenshiftVersion
		)

		BeforeEach(func() {
			customOpenShiftVersions = models.OpenshiftVersions{
				"4.7": models.OpenshiftVersion{
					RhcosImage:     swag.String("rhcos_4.7.0"),
					RhcosRootfs:    swag.String("rhcos_rootfs_4.7"),
					RhcosVersion:   swag.String("version-47.123-0"),
					ReleaseVersion: nil, DisplayName: nil, ReleaseImage: nil, SupportLevel: nil,
				},
			}
		})

		It("added version successfully", func() {
			h, err = NewHandler(logger, mockRelease, versions, customOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			version, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			var versionKey string
			versionKey, err = h.GetKey(ocpVersion)
			Expect(err).ShouldNot(HaveOccurred())
			versionFromCache, err = h.GetOpenshiftVersion(versionKey)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*version.DisplayName).Should(Equal(ocpVersion))
			Expect(*version.SupportLevel).Should(Equal(models.OpenshiftVersionSupportLevelCustom))
			Expect(*version.ReleaseVersion).Should(Equal(ocpVersion))
			Expect(*version.ReleaseImage).Should(Equal(releaseImage))
			Expect(h.GetOsImage(keyVersion)).Should(Equal(&models.OsImage{
				OpenshiftVersion: &versionKey,
				URL:              versionFromCache.RhcosImage,
				RootfsURL:        versionFromCache.RhcosRootfs,
				Version:          versionFromCache.RhcosVersion,
			}))
		})

		It("override version successfully", func() {
			h, err = NewHandler(logger, mockRelease, versions, customOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			_, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			versionFromCache, err = h.GetOpenshiftVersion(ocpVersion)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*versionFromCache.ReleaseImage).Should(Equal(releaseImage))

			// Override version with a new release image
			releaseImage = "newReleaseImage"
			_, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			versionFromCache, err = h.GetOpenshiftVersion(ocpVersion)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*versionFromCache.ReleaseImage).Should(Equal(releaseImage))
		})

		It("keep support level from cache", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()

			version, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			var versionKey string
			versionKey, err = h.GetKey(customOcpVersion)
			Expect(err).ShouldNot(HaveOccurred())
			versionFromCache, err = h.GetOpenshiftVersion(versionKey)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(*version.SupportLevel).Should(Equal(*versionFromCache.SupportLevel))
		})

		It("failed getting version from release", func() {
			h, err = NewHandler(logger, mockRelease, versions, customOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()

			_, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).Should(HaveOccurred())
		})

		It("missing from OPENSHIFT_VERSIONS", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			_, err = h.AddOpenshiftVersion("invalidRelease", pullSecret)
			Expect(err).Should(HaveOccurred())

			versionKey, _ := h.GetKey(ocpVersion)
			Expect(err.Error()).Should(Equal(fmt.Sprintf("RHCOS image is not configured for version: %s, "+
				"supported versions: [4.8]", versionKey)))
		})

		It("release image already exists", func() {
			h, err = NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())

			var versionFromCache *models.OpenshiftVersion
			versionFromCache, err = h.GetOpenshiftVersion(customKeyVersion)
			Expect(err).ShouldNot(HaveOccurred())

			version, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(version).Should(Equal(versionFromCache))
		})
	})

	Context("validateVersions", func() {
		var (
			versionKey        = "4.8"
			openshiftVersions *models.OpenshiftVersions
			openshiftVersion  models.OpenshiftVersion
			osImages          *models.OsImages
		)

		BeforeEach(func() {
			openshiftVersions = &models.OpenshiftVersions{
				"4.8": models.OpenshiftVersion{
					DisplayName:  swag.String("4.8-candidate"),
					ReleaseImage: swag.String("release_4.8"), ReleaseVersion: swag.String("4.8-candidate"),
					RhcosImage: swag.String("rhcos_4.8"), RhcosRootfs: swag.String("rhcos_rootfs_4.8"),
					RhcosVersion: swag.String("version-48.123-0"),
					SupportLevel: swag.String("newbie"),
				},
			}
			openshiftVersion = (*openshiftVersions)[versionKey]

			osImages = &models.OsImages{
				&models.OsImage{
					CPUArchitecture:  swag.String("x86_64"),
					OpenshiftVersion: swag.String("4.9"),
					URL:              swag.String("rhcos_4.9"),
					RootfsURL:        swag.String("rhcos_rootfs_4.9"),
					Version:          swag.String("version-49.123-0"),
				},
			}
		})

		It("OS images specified", func() {
			osImages = &defaultOsImages
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("only OpenShift versions specified", func() {
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("missing RhcosImage in OpenShift versions", func() {
			openshiftVersion.RhcosImage = nil
			(*openshiftVersions)[versionKey] = openshiftVersion
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("URL"))
		})

		It("missing RhcosRootfs in OpenShift versions", func() {
			openshiftVersion.RhcosRootfs = nil
			(*openshiftVersions)[versionKey] = openshiftVersion
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("RootfsURL"))
		})

		It("missing RhcosVersion in OpenShift versions", func() {
			openshiftVersion.RhcosVersion = nil
			(*openshiftVersions)[versionKey] = openshiftVersion
			h, err = NewHandler(logger, mockRelease, versions, *openshiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Version"))
		})

		It("missing URL in OS images", func() {
			(*osImages)[0].URL = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("URL"))
		})

		It("missing Rootfs in OS images", func() {
			(*osImages)[0].RootfsURL = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Rootfs"))
		})

		It("missing Version in OS images", func() {
			(*osImages)[0].Version = nil
			h, err = NewHandler(logger, mockRelease, versions, newOpenShiftVersions, *osImages, nil, "")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Version"))
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
		h, err = NewHandler(logger, mockRelease, versions, models.OpenshiftVersions{}, models.OsImages{}, nil, "")
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("positive", func() {
		res, err := h.GetKey("4.6")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.GetKey("4.6.9")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.GetKey("4.6.9-beta")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("negative", func() {
		res, err := h.GetKey("ere.654.45")
		Expect(err).Should(HaveOccurred())
		Expect(res).Should(Equal(""))
	})
})
