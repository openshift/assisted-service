package networkobservability

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Network Observability Operator", func() {
	var (
		log      = logrus.New()
		operator *operator
		cluster  *common.Cluster
		ctx      = context.TODO()
	)

	BeforeEach(func() {
		operator = NewNetworkObservabilityOperator(log)
		cluster = &common.Cluster{Cluster: models.Cluster{
			OpenshiftVersion: "4.12.0",
		}}
	})

	Context("GetName", func() {
		It("should return correct name", func() {
			Expect(operator.GetName()).To(Equal(Name))
		})
	})

	Context("GetFullName", func() {
		It("should return correct full name", func() {
			Expect(operator.GetFullName()).To(Equal(FullName))
		})
	})

	Context("GetDependencies", func() {
		It("should return no dependencies", func() {
			deps, err := operator.GetDependencies(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(deps).To(BeEmpty())
		})
	})

	Context("GetDependenciesFeatureSupportID", func() {
		It("should return nil for no dependencies", func() {
			deps := operator.GetDependenciesFeatureSupportID()
			Expect(deps).To(BeNil())
		})
	})

	Context("ValidateCluster", func() {
		It("should always succeed", func() {
			results, err := operator.ValidateCluster(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].Status).To(Equal(api.Success))
			Expect(results[0].ValidationId).To(Equal("network-observability-requirements-satisfied"))
		})
	})

	Context("GetPreflightRequirements", func() {
		It("should return zero requirements", func() {
			reqs, err := operator.GetPreflightRequirements(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(reqs.OperatorName).To(Equal(Name))
			Expect(reqs.Requirements.Master.Quantitative.CPUCores).To(Equal(int64(0)))
			Expect(reqs.Requirements.Master.Quantitative.RAMMib).To(Equal(int64(0)))
			Expect(reqs.Requirements.Worker.Quantitative.CPUCores).To(Equal(int64(0)))
			Expect(reqs.Requirements.Worker.Quantitative.RAMMib).To(Equal(int64(0)))
		})
	})

	Context("GenerateManifests", func() {
		It("should generate manifests successfully", func() {
			manifests, customManifest, err := operator.GenerateManifests(cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(manifests).To(HaveKey("50_openshift-network-observability_ns.yaml"))
			Expect(manifests).To(HaveKey("50_openshift-network-observability_operator_group.yaml"))
			Expect(manifests).To(HaveKey("50_openshift-network-observability_subscription.yaml"))
			// When FlowCollector is not created, customManifest may contain only YAML separators
			Expect(string(customManifest)).To(Or(BeEmpty(), MatchRegexp(`^---\s*$`)))
		})
	})

	Context("ValidateHost", func() {
		It("should always succeed", func() {
			host := &models.Host{
				Role: models.HostRoleWorker,
			}
			result, err := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Status).To(Equal(api.Success))
			Expect(result.ValidationId).To(Equal("network-observability-requirements-satisfied"))
		})
	})

	Context("GetProperties", func() {
		It("should return correct properties", func() {
			props := operator.GetProperties()
			Expect(props).To(HaveLen(2))
			Expect(props[0].Name).To(Equal("createFlowCollector"))
			Expect(props[0].DataType).To(Equal(models.OperatorPropertyDataTypeBoolean))
			Expect(props[1].Name).To(Equal("sampling"))
			Expect(props[1].DataType).To(Equal(models.OperatorPropertyDataTypeInteger))
		})
	})

	Context("GetMonitoredOperator", func() {
		It("should return monitored operator with correct values", func() {
			monOp := operator.GetMonitoredOperator()
			Expect(monOp).ToNot(BeNil())
			Expect(monOp.Name).To(Equal(Name))
			Expect(monOp.Namespace).To(Equal(Namespace))
			Expect(monOp.SubscriptionName).To(Equal(SubscriptionName))
			Expect(monOp.OperatorType).To(Equal(models.OperatorTypeOlm))
		})
	})

	Context("GetHostRequirements", func() {
		It("should return zero requirements for worker", func() {
			host := &models.Host{Role: models.HostRoleWorker}
			reqs, err := operator.GetHostRequirements(ctx, cluster, host)
			Expect(err).ToNot(HaveOccurred())
			Expect(reqs).ToNot(BeNil())
			Expect(reqs.CPUCores).To(Equal(int64(0)))
			Expect(reqs.RAMMib).To(Equal(int64(0)))
		})
	})

	Context("GetFeatureSupportID", func() {
		It("should return NETWORKOBSERVABILITY feature ID", func() {
			featureID := operator.GetFeatureSupportID()
			Expect(featureID).To(Equal(models.FeatureSupportLevelIDNETWORKOBSERVABILITY))
		})
	})

	Context("GetBundleLabels", func() {
		It("should return empty bundles", func() {
			bundles := operator.GetBundleLabels(nil)
			Expect(bundles).To(BeEmpty())
		})
	})

	Context("GetClusterValidationIDs", func() {
		It("should return correct validation ID", func() {
			ids := operator.GetClusterValidationIDs()
			Expect(ids).To(HaveLen(1))
			Expect(ids[0]).To(Equal("network-observability-requirements-satisfied"))
		})
	})

	Context("GetHostValidationID", func() {
		It("should return correct validation ID", func() {
			id := operator.GetHostValidationID()
			Expect(id).To(Equal("network-observability-requirements-satisfied"))
		})
	})
})
