package openshiftai

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var _ = Describe("Operator", func() {
	var (
		ctx      context.Context
		operator *operator
	)

	BeforeEach(func() {
		ctx = context.Background()
		operator = NewOpenShiftAIOperator(common.GetTestLog())
	})

	DescribeTable(
		"Validate hosts",
		func(host *models.Host, expected api.ValidationResult) {
			cluster := &common.Cluster{
				Cluster: models.Cluster{
					Hosts: []*models.Host{
						host,
					},
				},
			}
			actual, _ := operator.ValidateHost(ctx, cluster, host, nil)
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Host with no inventory",
			&models.Host{},
			api.ValidationResult{
				Status:       api.Pending,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Missing inventory in the host",
				},
			},
		),
		Entry(
			"Worker host with insufficient memory",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 8,
					Ram:  8 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Insufficient memory to deploy OpenShift AI, requires 32 GiB but found 8 GiB",
				},
			},
		),
		Entry(
			"Worker host with insufficient CPU",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 4,
					Ram:  32 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Failure,
				ValidationId: operator.GetHostValidationID(),
				Reasons: []string{
					"Insufficient CPU to deploy OpenShift AI, requires 8 CPU cores but found 4",
				},
			},
		),
		Entry(
			"Worker host with sufficient resources",
			&models.Host{
				Role: models.HostRoleWorker,
				Inventory: Inventory(&InventoryResources{
					Cpus: 8,
					Ram:  32 * conversions.GiB,
				}),
			},
			api.ValidationResult{
				Status:       api.Success,
				ValidationId: operator.GetHostValidationID(),
			},
		),
	)
})

var _ = Describe("GetDependencies", func() {
	var (
		operator *operator
	)

	BeforeEach(func() {
		operator = NewOpenShiftAIOperator(common.GetTestLog())
	})

	It("should return no dependencies for a non-SNO cluster", func() {
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{{}},
			},
		}
		dependencies := operator.GetDependencies(cluster)
		Expect(dependencies).To(BeEmpty())
	})

	It("should return no dependencies for a cluster without hosts", func() {
		cluster := &common.Cluster{}
		dependencies := operator.GetDependencies(cluster)
		Expect(dependencies).To(BeEmpty())
	})

	It("should return LVM dependency for SNO cluster", func() {
		cluster := &common.Cluster{
			Cluster: models.Cluster{
				ControlPlaneCount: 1,
			},
		}
		dependencies := operator.GetDependencies(cluster)
		Expect(dependencies).To(ConsistOf(lvm.Operator.Name))
	})
})

var _ = Describe("ValidateCluster", func() {
	var (
		ctx      context.Context
		operator *operator
		cluster  *common.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		operator = NewOpenShiftAIOperator(common.GetTestLog())
		cluster = &common.Cluster{}
	})

	It("returns success with informational message when no GPU operators selected", func() {
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Success))
		Expect(results[1].Status).To(Equal(api.Success))
		Expect(results[1].ValidationId).To(Equal(clusterGPUValidationID))
		Expect(results[1].Reasons).To(ConsistOf("No GPU vendor selected - OpenShift AI will install without GPU support"))
	})

	It("returns success without warning when nvidia-gpu is selected", func() {
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{Name: "nvidia-gpu"},
		}

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Success))
		Expect(results[1].Status).To(Equal(api.Success))
		Expect(results[1].Reasons).To(BeEmpty())
	})

	It("returns success without warning when amd-gpu is selected", func() {
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{Name: "amd-gpu"},
		}

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[1].Status).To(Equal(api.Success))
		Expect(results[1].Reasons).To(BeEmpty())
	})

	It("returns failure for workers if not enough worker nodes", func() {
		operator.config.MinWorkerNodes = 2
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{Name: "nvidia-gpu"},
		}

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Failure))
		Expect(results[0].ValidationId).To(Equal(clusterValidationID))
		Expect(results[1].Status).To(Equal(api.Success))
	})
})
