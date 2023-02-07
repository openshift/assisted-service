package metallb

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("MetalLB Operator", func() {
	ctx := context.TODO()
	operator := NewMetalLBOperator(common.GetTestLog(), nil)
	var cluster common.Cluster

	BeforeEach(func() {
		cluster = common.Cluster{Cluster: models.Cluster{}}
	})

	Context("ValidateCluster", func() {
		It("valid cluster", func() {
			properties := Properties{
				ApiIP:     "192.168.122.201",
				IngressIP: "192.168.122.202",
			}
			propertiesBytes, err := json.Marshal(&properties)
			Expect(err).NotTo(HaveOccurred())
			propertiesStr := string(propertiesBytes)

			metallbConfig := models.MonitoredOperator{
				Name:       Operator.Name,
				Properties: propertiesStr,
			}

			cluster.MonitoredOperators = append(cluster.MonitoredOperators, &metallbConfig)

			_, err = operator.ValidateCluster(ctx, &cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("invalid properties", func() {
			result, err := operator.ValidateCluster(ctx, &cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).To(Equal(api.Failure))
		})
	})
})
