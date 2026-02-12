package stream_test

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/stream"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Get notifiable cluster", func() {
	var (
		cluster   *common.Cluster
		clusterId strfmt.UUID
		hostId    strfmt.UUID
	)

	BeforeEach(func() {
		clusterId = strfmt.UUID(uuid.New().String())
		hostId = strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
				Hosts: []*models.Host{
					{ID: &hostId},
				},
			},
		}
	})

	It("should remove hosts field", func() {
		notifiableCluster := stream.GetNotifiableCluster(cluster)

		Expect(notifiableCluster.Hosts).Should(BeEmpty())
		Expect(notifiableCluster.ID).Should(Equal(&clusterId))
	})

	It("should include primary_ip_stack in payload when set", func() {
		stack := common.PrimaryIPStackV4
		cluster.PrimaryIPStack = &stack

		notifiableCluster := stream.GetNotifiableCluster(cluster)
		payload := notifiableCluster.Payload()

		jsonData, err := json.Marshal(payload)
		Expect(err).ToNot(HaveOccurred())

		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		Expect(err).ToNot(HaveOccurred())

		Expect(result["primary_ip_stack"]).To(Equal(float64(4)))
	})

	It("should omit primary_ip_stack when nil", func() {
		notifiableCluster := stream.GetNotifiableCluster(cluster)
		payload := notifiableCluster.Payload()

		jsonData, err := json.Marshal(payload)
		Expect(err).ToNot(HaveOccurred())

		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		Expect(err).ToNot(HaveOccurred())

		_, exists := result["primary_ip_stack"]
		Expect(exists).To(BeFalse())
	})
})
