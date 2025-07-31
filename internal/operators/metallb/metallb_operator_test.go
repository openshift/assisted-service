package metallb_test

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/metallb"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("MetalLB operator", func() {
	var (
		log       = logrus.New()
		metalLBOp = metallb.NewMetalLBOperator(log)
	)

	It("GetName", func() {
		Expect(metalLBOp.GetName()).To(Equal("metallb"))
	})

	It("GetFullName", func() {
		Expect(metalLBOp.GetFullName()).To(Equal("MetalLB"))
	})

	It("GetDependencies", func() {
		cluster := common.Cluster{}
		dependencies, err := metalLBOp.GetDependencies(&cluster)
		Expect(err).To(Not(HaveOccurred()))
		Expect(dependencies).To(BeEmpty())
	})

	Context("GetClusterValidationID", func() {
		It("should return expected cluster validation ID", func() {
			validationIDs := metalLBOp.GetClusterValidationIDs()
			Expect(validationIDs).To(ContainElement(string(models.ClusterValidationIDMetallbRequirementsSatisfied)))
		})
	})

	Context("GetHostValidationID", func() {
		It("should return expected host validation ID", func() {
			validationID := metalLBOp.GetHostValidationID()
			Expect(validationID).To(Equal(string(models.HostValidationIDMetallbRequirementsSatisfied)))
		})
	})

	Context("ValidateCluster", func() {
		var cluster *common.Cluster

		BeforeEach(func() {
			cluster = &common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.11.0",
			}}
		})

		It("should be valid for supported OpenShift version", func() {
			cluster.OpenshiftVersion = "4.11.0"
			results, err := metalLBOp.ValidateCluster(context.TODO(), cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Status).To(Equal(api.Success))
		})

	})

	Context("ValidateHost", func() {
		var (
			cluster *common.Cluster
			host    *models.Host
		)

		BeforeEach(func() {
			cluster = &common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.11.0",
			}}

			inventoryStr := metallb.Inventory(&metallb.InventoryResources{
				Cpus: 2,
				Ram:  8 * metallb.GiB,
			})
			hostID := strfmt.UUID(uuid.New().String())
			host = &models.Host{
				ID:        &hostID,
				Inventory: inventoryStr,
			}
		})

		It("should be valid with proper inventory", func() {
			result, err := metalLBOp.ValidateHost(context.TODO(), cluster, host, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Status).To(Equal(api.Success))
		})
	})

	Context("GenerateManifests", func() {
		It("should generate manifests without error", func() {
			cluster := &common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.11.0",
				Name:             "test-cluster",
			}}
			manifests, tgzManifests, err := metalLBOp.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).ToNot(BeEmpty())
			Expect(tgzManifests).ToNot(BeNil())
		})
	})

	Context("GetMonitoredOperator", func() {
		It("should return monitored operator", func() {
			monitoredOperator := metalLBOp.GetMonitoredOperator()
			Expect(monitoredOperator).ToNot(BeNil())
			Expect(monitoredOperator.Name).To(Equal("metallb"))
			Expect(monitoredOperator.Namespace).To(Equal("metallb-system"))
		})
	})

	Context("GetHostRequirements", func() {
		It("should return host requirements", func() {
			cluster := &common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.11.0",
			}}
			hostID := strfmt.UUID(uuid.New().String())
			host := &models.Host{ID: &hostID}

			requirements, err := metalLBOp.GetHostRequirements(context.TODO(), cluster, host)
			Expect(err).ToNot(HaveOccurred())
			Expect(requirements).ToNot(BeNil())
			// MetalLB has minimal resource requirements and doesn't need specific hardware
		})
	})

	Context("GetFeatureSupportID", func() {
		It("should return feature support ID", func() {
			supportID := metalLBOp.GetFeatureSupportID()
			Expect(supportID).To(Equal(models.FeatureSupportLevelIDMETALLB))
		})
	})
})
