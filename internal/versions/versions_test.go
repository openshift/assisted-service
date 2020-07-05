package versions

import (
	"context"
	"os"
	"testing"

	"github.com/kelseyhightower/envconfig"

	operations "github.com/filanov/bm-inventory/restapi/operations/versions"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "versions")
}

var _ = Describe("list versions", func() {
	var (
		h        *handler
		versions Versions
	)
	It("default values", func() {
		Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
		h = NewHandler(versions)
		reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
		val, _ := reply.(*operations.ListComponentVersionsOK)
		Expect(val.Payload.Versions["assisted-installer-service"]).
			Should(Equal("quay.io/ocpmetal/installer-image-build:stable"))
		Expect(val.Payload.Versions["image-builder"]).Should(Equal("quay.io/ocpmetal/installer-image-build:stable"))
		Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("quay.io/ocpmetal/agent:stable"))
		Expect(val.Payload.Versions["ignition-manifests-and-kubeconfig-generate"]).
			Should(Equal("quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:stable"))
		Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("quay.io/ocpmetal/assisted-installer:stable"))
		Expect(val.Payload.ReleaseTag).Should(Equal(""))
	})

	It("mix default and non default", func() {
		os.Setenv("SELF_VERSION", "self-version")
		os.Setenv("IMAGE_BUILDER", "image-builder")
		os.Setenv("AGENT_DOCKER_IMAGE", "agent-image")
		os.Setenv("INSTALLER_IMAGE", "installer-image")
		Expect(envconfig.Process("test", &versions)).ShouldNot(HaveOccurred())
		h = NewHandler(versions)
		reply := h.ListComponentVersions(context.Background(), operations.ListComponentVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewListComponentVersionsOK()))
		val, _ := reply.(*operations.ListComponentVersionsOK)
		Expect(val.Payload.Versions["assisted-installer-service"]).Should(Equal("self-version"))
		Expect(val.Payload.Versions["image-builder"]).Should(Equal("image-builder"))
		Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("agent-image"))
		Expect(val.Payload.Versions["ignition-manifests-and-kubeconfig-generate"]).
			Should(Equal("quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:stable"))
		Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("installer-image"))
		Expect(val.Payload.ReleaseTag).Should(Equal(""))
	})
})
