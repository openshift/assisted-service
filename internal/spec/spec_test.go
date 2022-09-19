package spec

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestHost(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "spec test")
}

func isJSON(s []byte) bool {
	var js map[string]interface{}
	return json.Unmarshal(s, &js) == nil

}

var _ = Describe("spec", func() {
	var (
		ts           *httptest.Server
		defaultReply = "Hello"
	)

	BeforeEach(func() {
		ts = httptest.NewServer(
			WithSpecMiddleware(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, defaultReply)
				}),
			),
		)
	})

	AfterEach(func() {
		ts.Close()
	})

	It("get spec", func() {
		for _, path := range openapiPaths {
			res, err := http.Get(fmt.Sprintf("%s%s", ts.URL, path))
			if err != nil {
				log.Fatal(err)
			}
			reply, err := io.ReadAll(res.Body)
			Expect(err).To(BeNil())
			res.Body.Close()
			Expect(isJSON(reply)).To(BeTrue(), fmt.Sprintf("got %s", string(reply)))
		}
	})

	It("not a spec", func() {
		res, err := http.Get(ts.URL)
		if err != nil {
			log.Fatal(err)
		}
		reply, err := io.ReadAll(res.Body)
		Expect(err).To(BeNil())
		res.Body.Close()
		Expect(string(reply)).To(Equal(defaultReply))
	})
})
