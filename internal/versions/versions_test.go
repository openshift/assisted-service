package versions

import (
	"context"
	"os"
	"testing"

	"github.com/kelseyhightower/envconfig"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "versions")
}

var _ = Describe("list versions", func() {
	var (
		h                 *handler
		versions          Versions
		openshiftVersions models.OpenshiftVersions
	)

	BeforeEach(func() {
		openshiftVersions = make(models.OpenshiftVersions)
	})

	Context("ListComponentVersions", func() {
		It("default values", func() {
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h = NewHandler(versions, openshiftVersions, "")
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
			val, _ := reply.(*operations.ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).
				Should(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
			Expect(val.Payload.Versions["image-builder"]).Should(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("quay.io/ocpmetal/agent:latest"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("quay.io/ocpmetal/assisted-installer:latest"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})

		It("mix default and non default", func() {
			os.Setenv("SELF_VERSION", "self-version")
			os.Setenv("IMAGE_BUILDER", "image-builder")
			os.Setenv("AGENT_DOCKER_IMAGE", "agent-image")
			os.Setenv("INSTALLER_IMAGE", "installer-image")
			os.Setenv("CONTROLLER_IMAGE", "controller-image")
			Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
			h = NewHandler(versions, openshiftVersions, "")
			reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
			val, _ := reply.(*operations.ListComponentVersionsOK)
			Expect(val.Payload.Versions["assisted-installer-service"]).Should(Equal("self-version"))
			Expect(val.Payload.Versions["image-builder"]).Should(Equal("image-builder"))
			Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("agent-image"))
			Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("installer-image"))
			Expect(val.Payload.Versions["assisted-installer-controller"]).Should(Equal("controller-image"))
			Expect(val.Payload.ReleaseTag).Should(Equal(""))
		})
	})

	Context("ListSupportedOpenshiftVersions", func() {
		It("empty", func() {
			h = NewHandler(versions, openshiftVersions, "")

			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(BeEmpty())
		})

		It("get_defaults", func() {
			openshiftVersions["4.5"] = models.OpenshiftVersion{ReleaseImage: "release_4.5"}
			openshiftVersions["4.6"] = models.OpenshiftVersion{ReleaseImage: "release_4.6"}

			h = NewHandler(versions, openshiftVersions, "")

			reply := h.ListSupportedOpenshiftVersions(context.Background(), operations.ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.ListSupportedOpenshiftVersionsOK)

			Expect(val.Payload).Should(HaveLen(len(openshiftVersions)))

			for key, version := range val.Payload {
				Expect(version).Should(Equal(openshiftVersions[key]))
			}
		})
	})

	Context("GetReleaseImage", func() {
		BeforeEach(func() {
			openshiftVersions["4.5"] = models.OpenshiftVersion{ReleaseImage: "release_4.5"}
			openshiftVersions["4.6"] = models.OpenshiftVersion{ReleaseImage: "release_4.6"}

			h = NewHandler(versions, openshiftVersions, "")
		})

		It("default", func() {
			for key := range openshiftVersions {
				releaseImage, err := h.GetReleaseImage(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage).Should(Equal(openshiftVersions[key].ReleaseImage))
			}
		})

		It("override_default", func() {
			overrideRelaseImage := "override-release-image"
			h = NewHandler(versions, openshiftVersions, overrideRelaseImage)

			for key := range openshiftVersions {
				releaseImage, err := h.GetReleaseImage(key)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(releaseImage).Should(Equal(overrideRelaseImage))
			}
		})

		It("unsupported_key", func() {
			releaseImage, err := h.GetReleaseImage("unsupported")
			Expect(err).Should(HaveOccurred())
			Expect(releaseImage).Should(BeEmpty())
		})
	})

	Context("IsOpenshiftVersionSupported", func() {
		It("positive", func() {
			h = NewHandler(versions, openshiftVersions, "")

			for key := range openshiftVersions {
				Expect(h.IsOpenshiftVersionSupported(key)).Should(BeTrue())
			}
		})

		It("negative", func() {
			h = NewHandler(versions, openshiftVersions, "")
			Expect(h.IsOpenshiftVersionSupported("unknown")).Should(BeFalse())
		})
	})
})
