package subsystem

import (
	"fmt"
	"io"
	"net/http"
	"path"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("test spec endpoint", func() {
	It("[minimal-set]get spec", func() {
		reply, err := http.Get(fmt.Sprintf("http://%s",
			path.Join(Options.InventoryHost, client.DefaultBasePath, "openapi")))
		Expect(err).To(BeNil())
		data, err := io.ReadAll(reply.Body)
		Expect(err).To(BeNil())
		reply.Body.Close()
		Expect(utils_test.IsJSON(data)).To(BeTrue(), fmt.Sprintf("got %s", string(data)))
	})
})
