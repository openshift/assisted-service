package subsystem

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
)

var _ = Describe("test spec endpoint", func() {
	It("[minimal-set]get spec", func() {
		reply, err := http.Get(fmt.Sprintf("http://%s",
			path.Join(Options.InventoryHost, client.DefaultBasePath, "openapi")))
		Expect(err).To(BeNil())
		data, err := ioutil.ReadAll(reply.Body)
		Expect(err).To(BeNil())
		reply.Body.Close()
		Expect(isJSON(data)).To(BeTrue(), fmt.Sprintf("got %s", string(data)))
	})
})
