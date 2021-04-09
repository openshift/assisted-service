package versions

import (
	"context"
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
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "versions")
}

var defaultOpenShiftVersions = models.OpenshiftVersions{
	"4.5": models.OpenshiftVersion{
		DisplayName: swag.String("4.5.1"), ReleaseImage: swag.String("release_4.5"),
		RhcosImage: swag.String("rhcos_4.5"), RhcosVersion: swag.String("version-45.123-0"),
		SupportLevel: swag.String("oldie"),
	},
	"4.6": models.OpenshiftVersion{
		DisplayName: swag.String("4.6-candidate"), ReleaseImage: swag.String("release_4.6"),
		RhcosImage: swag.String("rhcos_4.6"), RhcosVersion: swag.String("version-46.123-0"),
		SupportLevel: swag.String("newbie"),
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
			Expect(val.Payload.Versions["assisted-service"]).
				Should(Equal("quay.io/ocpmetal/assisted-service:latest"))
			Expect(val.Payload.Versions["agent"]).Should(Equal("quay.io/ocpmetal/agent:latest"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("quay.io/ocpmetal/assisted-installer:latest"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})

		It("mix default and non default", func() {
			os.Setenv("SELF_VERSION", "registry/user/self-version:latest")
			os.Setenv("AGENT_DOCKER_IMAGE", "local-registry/org/agent-image:321")
			os.Setenv("INSTALLER_IMAGE", "installer-image")
			os.Setenv("CONTROLLER_IMAGE", "controller-image:latest")
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())

			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
			val, _ := reply.(*operations.ListComponentVersionsOK)
			Expect(val.Payload.Versions["self-version"]).Should(Equal(os.Getenv("SELF_VERSION")))
			Expect(val.Payload.Versions["agent-image"]).Should(Equal(os.Getenv("AGENT_DOCKER_IMAGE")))
			Expect(val.Payload.Versions["installer-image"]).Should(Equal(os.Getenv("INSTALLER_IMAGE")))
			Expect(val.Payload.Versions["controller-image"]).Should(Equal(os.Getenv("CONTROLLER_IMAGE")))
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

		It("get_defaults", func() {
			openshiftVersions = &defaultOpenShiftVersions

			h = NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(HaveLen(len(*openshiftVersions)))

			for key, version := range val.Payload {
				Expect(version).Should(Equal((*openshiftVersions)[key]))
			}
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

	Context("IsOpenshiftVersionSupported", func() {
		It("positive", func() {
			h := NewHandler(logger, mockRelease, versions, *openshiftVersions, "")

			for key := range *openshiftVersions {
				Expect(h.IsOpenshiftVersionSupported(key)).Should(BeTrue())
			}
		})

		It("negative", func() {
			h := NewHandler(logger, mockRelease, versions, *openshiftVersions, "")
			Expect(h.IsOpenshiftVersionSupported("unknown")).Should(BeFalse())
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
		res, err := h.GetSupportedVersionFormat("4.6")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.GetSupportedVersionFormat("4.6.9")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))

		res, err = h.GetSupportedVersionFormat("4.6.9-beta")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("negative", func() {
		res, err := h.GetSupportedVersionFormat("ere.654.45")
		Expect(err).Should(HaveOccurred())
		Expect(res).Should(Equal(""))
	})
})
