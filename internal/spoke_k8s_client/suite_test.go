package spoke_k8s_client

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestSpokeK8SClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spoke K8S client")
}
