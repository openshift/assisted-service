package subsystem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Low level API behaviours", func() {
	It("low_level_api Rejects unknown JSON fields", func() {
		// Create a request and marshal it:
		requestObject := models.ClusterCreateParams{
			Name:             swag.String("test-cluster"),
			OpenshiftVersion: swag.String(openshiftVersion),
			PullSecret:       swag.String(pullSecret),
		}
		requestBytes, err := json.Marshal(requestObject)
		Expect(err).ToNot(HaveOccurred())
		var requestMap map[string]any
		err = json.Unmarshal(requestBytes, &requestMap)
		Expect(err).ToNot(HaveOccurred())

		// Add the unknown field (the result of a typo):
		requestMap["base_dns_doman"] = "example.com"

		// Marshal the modified request:
		requestBytes, err = json.Marshal(requestMap)
		Expect(err).ToNot(HaveOccurred())
		requestBody := bytes.NewBuffer(requestBytes)
		request, err := http.NewRequest(
			http.MethodPost,
			fmt.Sprintf(
				"http://%s/api/assisted-install/v2/clusters",
				Options.InventoryHost,
			),
			requestBody,
		)
		request.Header.Set("Authorization", "Bearer "+Options.TestToken)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		Expect(err).ToNot(HaveOccurred())
		response, err := http.DefaultClient.Do(request)
		Expect(err).ToNot(HaveOccurred())
		defer response.Body.Close()

		// Verify that the request has been rejected:
		Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		responseBytes, err := io.ReadAll(response.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(responseBytes)).To(MatchJSON(`{
			"code": 400,
			"message": "parsing newClusterParams body from \"\" failed, because json: unknown field \"base_dns_doman\""
		}`))
	})

	It("low_level_api Returns 400 for unmatched regex field", func() {
		requestObject := models.ClusterCreateParams{
			Name:             swag.String("test-cluster"),
			OpenshiftVersion: swag.String(openshiftVersion),
			PullSecret:       swag.String(pullSecret),
		}
		requestBytes, err := json.Marshal(requestObject)
		Expect(err).ToNot(HaveOccurred())
		var requestMap map[string]any
		err = json.Unmarshal(requestBytes, &requestMap)
		Expect(err).ToNot(HaveOccurred())

		// Add a field that is expected to contain a regex
		// with a regex that does not match it's pattern
		requestMap["cluster_network_cidr"] = "0.0.0.a/8"

		// Marshal the modified request:
		requestBytes, err = json.Marshal(requestMap)
		Expect(err).ToNot(HaveOccurred())
		requestBody := bytes.NewBuffer(requestBytes)
		request, err := http.NewRequest(
			http.MethodPost,
			fmt.Sprintf(
				"http://%s/api/assisted-install/v2/clusters",
				Options.InventoryHost,
			),
			requestBody,
		)
		request.Header.Set("Authorization", "Bearer "+Options.TestToken)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		Expect(err).ToNot(HaveOccurred())
		response, err := http.DefaultClient.Do(request)
		Expect(err).ToNot(HaveOccurred())
		defer response.Body.Close()

		// Verify that the request has been rejected:
		Expect(response.StatusCode).To(Equal(http.StatusBadRequest))
		responseBytes, err := io.ReadAll(response.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(responseBytes)).To(MatchJSON(`{
		  "code": 400,
		  "message": "cluster_network_cidr in body should match '^(?:(?:(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\/(?:(?:[0-9])|(?:[1-2][0-9])|(?:3[0-2])))|(?:(?:[0-9a-fA-F]*:[0-9a-fA-F]*){2,})/(?:(?:[0-9])|(?:[1-9][0-9])|(?:1[0-1][0-9])|(?:12[0-8])))$'"
		}`))
	})
})
