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
		RhcosImage: swag.String("rhcos_4.5"), RhcosVersion: swag.String("version-45.123-0"),
		SupportLevel: swag.String("oldie"),
	},
	"4.6": models.OpenshiftVersion{
		DisplayName:  swag.String("4.6-candidate"),
		ReleaseImage: swag.String("release_4.6"), ReleaseVersion: swag.String("4.6-candidate"),
		RhcosImage: swag.String("rhcos_4.6"), RhcosVersion: swag.String("version-46.123-0"),
		SupportLevel: swag.String("newbie"),
	},
}

var supportedCustomOpenShiftVersions = models.OpenshiftVersions{
	"4.8": models.OpenshiftVersion{
		DisplayName:  swag.String("4.8.0"),
		ReleaseImage: swag.String("release_4.8"), ReleaseVersion: swag.String("4.8.0-fc.1"),
		RhcosImage: swag.String("rhcos_4.8"), RhcosVersion: swag.String("version-48.123-0"),
		SupportLevel: swag.String(models.OpenshiftVersionSupportLevelCustom),
	},
}

var _ = Describe("list versions", func() {
	var (
		h                 *handler
		logger            logrus.FieldLogger
		mockRelease       *oc.MockRelease
		versions          Versions
		openshiftVersions *models.OpenshiftVersions
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		logger = logrus.New()
		openshiftVersions = &models.OpenshiftVersions{}
	})

	Context("ListComponentVersions", func() {
		It("default values", func() {
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
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
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
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
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")

			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(BeEmpty())
		})

		readDefaultOpenshiftVersions := func() {
			bytes, err := ioutil.ReadFile("../../data/default_ocp_versions.json")
			Expect(err).ShouldNot(HaveOccurred())
			err = json.Unmarshal(bytes, openshiftVersions)
			Expect(err).ShouldNot(HaveOccurred())
		}

		It("get_defaults", func() {
			readDefaultOpenshiftVersions()
			CURRENT_DEFAULT_VERSION := "4.7" //keep align with default_ocp_versions.json

			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
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

	Context("GetReleaseImage", func() {
		var (
			releaseImage string
			err          error
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
		})

		It("default", func() {
			for key := range *openshiftVersions {
				releaseImage, err = h.GetReleaseImage(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage).Should(Equal(*(*openshiftVersions)[key].ReleaseImage))
			}
		})

		It("unsupported_key", func() {
			releaseImage, err = h.GetReleaseImage("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeEmpty())
		})
	})

	Context("GetRHCOSImage", func() {
		var (
			rhcosImage string
			err        error
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
		})

		It("default", func() {
			for key := range *openshiftVersions {
				rhcosImage, err = h.GetRHCOSImage(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(rhcosImage).Should(Equal(*(*openshiftVersions)[key].RhcosImage))
			}
		})

		It("unsupported_key", func() {
			rhcosImage, err = h.GetRHCOSImage("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(rhcosImage).Should(BeEmpty())
		})
	})

	Context("GetRHCOSVersion", func() {
		var (
			rhcosVersion string
			err          error
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
		})

		It("default", func() {
			for key := range *openshiftVersions {
				rhcosVersion, err = h.GetRHCOSVersion(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(rhcosVersion).Should(Equal(*(*openshiftVersions)[key].RhcosVersion))
			}
		})

		It("unsupported_key", func() {
			rhcosVersion, err = h.GetRHCOSVersion("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(rhcosVersion).Should(BeEmpty())
		})
	})

	Context("GetReleaseVersion", func() {
		var (
			releaseVersion string
			err            error
		)

		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
		})

		It("default", func() {
			for key := range *openshiftVersions {
				releaseVersion, err = h.GetReleaseVersion(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseVersion).Should(Equal(*(*openshiftVersions)[key].ReleaseVersion))
			}
		})

		It("unsupported_key", func() {
			releaseVersion, err = h.GetReleaseVersion("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(releaseVersion).Should(BeEmpty())
		})
	})

	Context("GetVersion", func() {
		var (
			version *models.OpenshiftVersion
			err     error
		)

		BeforeEach(func() {
			openshiftVersions = &supportedCustomOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
		})

		It("default", func() {
			for key := range *openshiftVersions {
				version, err = h.GetVersion(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(*version).Should(Equal((*openshiftVersions)[key]))
			}
		})

		It("unsupported_key", func() {
			version, err = h.GetVersion("unsupported")
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("IsOpenshiftVersionSupported", func() {
		BeforeEach(func() {
			openshiftVersions = &defaultOpenShiftVersions
			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
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

	Context("AddOpenshiftVersion", func() {
		var (
			pullSecret              = "test_pull_secret"
			releaseImage            = "releaseImage"
			ocpVersion              = "4.7.0-fc.1"
			keyVersion              = "4.7"
			customOcpVersion        = "4.8.0-fc.1"
			customKeyVersion        = "4.8"
			customOpenShiftVersions models.OpenshiftVersions
		)

		BeforeEach(func() {
			customOpenShiftVersions = models.OpenshiftVersions{
				"4.7": models.OpenshiftVersion{
					RhcosImage:     swag.String("rhcos_4.7.0"),
					RhcosVersion:   swag.String("version-47.123-0"),
					ReleaseVersion: nil, DisplayName: nil, ReleaseImage: nil, SupportLevel: nil,
				},
			}
		})

		It("added version successfully", func() {
			h := NewHandler(logger, mockRelease, versions, customOpenShiftVersions, "")
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			version, err := h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			versionKey, err := h.GetKey(ocpVersion)
			Expect(err).ShouldNot(HaveOccurred())

			versionFromCache := h.openshiftVersions[versionKey]
			Expect(*version.DisplayName).Should(Equal(ocpVersion))
			Expect(h.GetReleaseVersion(keyVersion)).Should(Equal(ocpVersion))
			Expect(h.GetReleaseImage(keyVersion)).Should(Equal(releaseImage))
			Expect(h.GetRHCOSImage(keyVersion)).Should(Equal(*versionFromCache.RhcosImage))
			Expect(h.GetRHCOSVersion(keyVersion)).Should(Equal(*versionFromCache.RhcosVersion))
			Expect(*version.SupportLevel).Should(Equal(models.OpenshiftVersionSupportLevelCustom))
		})

		It("override version successfully", func() {
			h := NewHandler(logger, mockRelease, versions, customOpenShiftVersions, "")
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			_, err := h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(h.GetReleaseImage(keyVersion)).Should(Equal(releaseImage))

			// Override version with a new release image
			releaseImage = "newReleaseImage"
			_, err = h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(h.GetReleaseImage(keyVersion)).Should(Equal(releaseImage))
		})

		It("keep support level from cache", func() {
			h := NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, "")
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()

			version, err := h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			versionKey, err := h.GetKey(customOcpVersion)
			Expect(err).ShouldNot(HaveOccurred())

			versionFromCache := h.openshiftVersions[versionKey]
			Expect(*version.SupportLevel).Should(Equal(*versionFromCache.SupportLevel))
		})

		It("failed getting version from release", func() {
			h := NewHandler(logger, mockRelease, versions, customOpenShiftVersions, "")
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()

			_, err := h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).Should(HaveOccurred())
		})

		It("missing from OPENSHIFT_VERSIONS", func() {
			h := NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, "")
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()

			_, err := h.AddOpenshiftVersion("invalidRelease", pullSecret)
			Expect(err).Should(HaveOccurred())

			versionKey, _ := h.GetKey(ocpVersion)
			Expect(err.Error()).Should(Equal(fmt.Sprintf("RHCOS image is not configured for version: %s, "+
				"supported versions: [4.8]", versionKey)))
		})

		It("release image already exists", func() {
			h := NewHandler(logger, mockRelease, versions, supportedCustomOpenShiftVersions, "")

			versionFromCache, err := h.GetVersion(customKeyVersion)
			Expect(err).ShouldNot(HaveOccurred())

			version, err := h.AddOpenshiftVersion(releaseImage, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(version).Should(Equal(versionFromCache))
		})
	})
})

var _ = Describe("list versions", func() {
	var (
		h *handler
	)
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mockRelease := oc.NewMockRelease(ctrl)

		var versions Versions
		Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())

		logger := logrus.New()
		h = NewHandler(logger, mockRelease, versions, models.OpenshiftVersions{}, "")
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
