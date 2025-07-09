package openshiftai

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
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
		operator                    *operator
		mockCtrl                    *gomock.Controller
		vendor1                     *MockGPUVendor
		vendor2                     *MockGPUVendor
		cluster, clusterWithoutHost *common.Cluster
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		vendor1 = NewMockGPUVendor(mockCtrl)
		vendor2 = NewMockGPUVendor(mockCtrl)
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				Hosts: []*models.Host{{}},
			}}
		clusterWithoutHost = &common.Cluster{}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should return no dependencies when no vendors are provided", func() {
		operator = NewOpenShiftAIOperator(common.GetTestLog())
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(BeEmpty())
	})

	It("should return no dependencies when vendors do not have GPUs", func() {
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(false, nil).Times(1)
		vendor1.EXPECT().GetName().Times(0)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(BeEmpty())
	})

	It("should return dependencies when vendors have GPUs", func() {
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor1.EXPECT().GetName().Return("vendor1").Times(1)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(ConsistOf("vendor1"))
	})

	It("should return dependencies for multiple vendors with GPUs", func() {
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor1.EXPECT().GetName().Return("vendor1").Times(1)
		vendor2.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor2.EXPECT().GetName().Return("vendor2").Times(1)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1, vendor2)
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(ConsistOf("vendor1", "vendor2"))
	})

	It("should skip vendors without GPUs", func() {
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(false, nil).Times(1)
		vendor2.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor2.EXPECT().GetName().Return("vendor2").Times(1)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1, vendor2)
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(ConsistOf("vendor2"))
	})

	It("should return an error if a vendor fails to check for GPUs", func() {
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(false, nil).Times(1)
		vendor2.EXPECT().ClusterHasGPU(cluster).Return(false, fmt.Errorf("some error")).Times(1)
		vendor2.EXPECT().GetName().Return("vendor2").Times(1)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1, vendor2)
		dependencies, err := operator.GetDependencies(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to check if cluster has GPU for vendor2"))
		Expect(dependencies).To(BeEmpty())
	})

	It("should return all vendor if there is no hosts in the cluster", func() {
		vendor1.EXPECT().GetName().Return("vendor1").Times(1)
		vendor2.EXPECT().GetName().Return("vendor2").Times(1)

		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1, vendor2)
		dependencies, err := operator.GetDependencies(clusterWithoutHost)
		Expect(err).ToNot(HaveOccurred())
		Expect(dependencies).To(ConsistOf("vendor1", "vendor2"))
	})
})
var _ = Describe("ValidateCluster", func() {
	var (
		ctx      context.Context
		operator *operator
		mockCtrl *gomock.Controller
		vendor1  *MockGPUVendor
		cluster  *common.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		vendor1 = NewMockGPUVendor(mockCtrl)
		cluster = &common.Cluster{}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("returns both validations as success when workers and GPU are valid", func() {
		// Setup config for enough workers
		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor1.EXPECT().GetName().AnyTimes()

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Success))
		Expect(results[1].Status).To(Equal(api.Success))
	})

	It("returns failure for workers if not enough worker nodes", func() {
		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		operator.config.MinWorkerNodes = 2
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(true, nil).Times(1)
		vendor1.EXPECT().GetName().AnyTimes()

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Failure))
		Expect(results[0].ValidationId).To(Equal(clusterValidationID))
		Expect(results[1].Status).To(Equal(api.Success))
	})

	It("returns failure for GPU if no vendor has GPU", func() {
		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(false, nil).Times(1)
		vendor1.EXPECT().GetName().AnyTimes()

		results, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results[0].Status).To(Equal(api.Success))
		Expect(results[1].Status).To(Equal(api.Failure))
		Expect(results[1].ValidationId).To(Equal(clusterGPUValidationID))
	})

	It("returns error if vendor returns error", func() {
		operator = NewOpenShiftAIOperator(common.GetTestLog(), vendor1)
		operator.config.MinWorkerNodes = 1
		cluster.Hosts = []*models.Host{
			{Role: models.HostRoleWorker},
		}
		vendor1.EXPECT().ClusterHasGPU(cluster).Return(false, fmt.Errorf("gpu error")).Times(1)
		vendor1.EXPECT().GetName().Return("vendor1").Times(1)

		_, err := operator.ValidateCluster(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to validate GPU"))
	})
})
