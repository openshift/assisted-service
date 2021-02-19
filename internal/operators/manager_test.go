package operators_test

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var (
	cluster     *common.Cluster
	clusterHost *models.Host
	log         = logrus.New()
	manager     *operators.Manager
)

var _ = BeforeEach(func() {
	// create simple cluster
	clusterID := strfmt.UUID(uuid.New().String())
	cluster = &common.Cluster{
		Cluster: models.Cluster{
			ID: &clusterID,
		},
	}
	cluster.ImageInfo = &models.ImageInfo{}

	clusterHost = &models.Host{}
	manager = operators.NewManager(log)
})

var _ = Describe("Operators manager", func() {
	Context("GenerateManifests", func() {
		It("should generate OCS and LSO manifests when OCS operator is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&ocs.Operator,
			}

			manifests, err := manager.GenerateManifests(cluster)

			Expect(err).NotTo(HaveOccurred())

			Expect(manifests).NotTo(BeNil())
			Expect(len(manifests)).To(Equal(10))
			Expect(manifests).To(HaveKey(ContainSubstring("openshift-ocs")))
			Expect(manifests).To(HaveKey(ContainSubstring("openshift-lso")))
		})

		It("should generate LSO manifests when LSO operator is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&lso.Operator,
			}

			manifests, err := manager.GenerateManifests(cluster)

			Expect(err).NotTo(HaveOccurred())
			Expect(manifests).NotTo(BeNil())
			Expect(len(manifests)).To(Equal(5))
			Expect(manifests).To(HaveKey(ContainSubstring("openshift-lso")))
		})

		It("should generate CNV and LSO manifests when CNV operator is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&cnv.Operator,
			}

			manifests, err := manager.GenerateManifests(cluster)

			Expect(err).NotTo(HaveOccurred())
			Expect(manifests).NotTo(BeNil())
			Expect(len(manifests)).To(Equal(10))
			Expect(manifests).To(HaveKey(ContainSubstring("openshift-cnv")))
			Expect(manifests).To(HaveKey(ContainSubstring("openshift-lso")))
		})
	})

	Context("AnyOLMOperatorEnabled", func() {
		table.DescribeTable("should report any operator enabled", func(operators []*models.MonitoredOperator, expected bool) {
			cluster.MonitoredOperators = operators
			results := manager.AnyOLMOperatorEnabled(cluster)
			Expect(results).To(Equal(expected))
		},
			table.Entry("false for no operators", []*models.MonitoredOperator{}, false),
			table.Entry("true for lso operator", []*models.MonitoredOperator{
				&lso.Operator,
			}, true),
			table.Entry("true for ocs operator", []*models.MonitoredOperator{
				&ocs.Operator,
			}, true),
			table.Entry("true for lso and ocs operators", []*models.MonitoredOperator{
				&lso.Operator,
				&ocs.Operator,
			}, true),
			table.Entry("true for lso, ocs and cnv operators", []*models.MonitoredOperator{
				&lso.Operator,
				&ocs.Operator,
				&cnv.Operator,
			}, true),
		)
	})

	Context("ValidateCluster", func() {
		It("should deem operators cluster-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
			))
		})

		It("should deem OCS operator cluster-invalid when it's enabled and invalid", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&ocs.Operator,
			}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Failure, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied),
					Reasons: []string{"Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS. "}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
			))
		})
	})

	Context("ValidateHost", func() {
		It("should deem operators host-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
			))
		})

		It("should deem operators host-valid when OCS is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&ocs.Operator,
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))

			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
			))
		})
	})

	Context("UpdateDependencies", func() {
		table.DescribeTable("should resolve dependencies", func(input []*models.MonitoredOperator, expected []*models.MonitoredOperator) {
			cluster.MonitoredOperators = input
			err := manager.UpdateDependencies(cluster)
			Expect(err).ToNot(HaveOccurred())

			Expect(cluster.MonitoredOperators).To(HaveLen(len(expected)))
			Expect(cluster.MonitoredOperators).To(ContainElements(expected))
		},
			table.Entry("when only LSO is specified",
				[]*models.MonitoredOperator{&lso.Operator},
				[]*models.MonitoredOperator{&lso.Operator},
			),
			table.Entry("when only OCS is specified",
				[]*models.MonitoredOperator{&ocs.Operator},
				[]*models.MonitoredOperator{&ocs.Operator, &lso.Operator},
			),
			table.Entry("when both OCS and LSO are specified",
				[]*models.MonitoredOperator{&ocs.Operator, &lso.Operator},
				[]*models.MonitoredOperator{&ocs.Operator, &lso.Operator},
			),
			table.Entry("when only CNV is specified",
				[]*models.MonitoredOperator{&cnv.Operator},
				[]*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
			),
			table.Entry("when CNV, OCS and LSO are specified",
				[]*models.MonitoredOperator{&cnv.Operator, &ocs.Operator, &lso.Operator},
				[]*models.MonitoredOperator{&cnv.Operator, &ocs.Operator, &lso.Operator},
			),
		)
	})

	Context("Supported Operators", func() {
		It("should provide list of supported operators", func() {
			supportedOperators := manager.GetSupportedOperators()

			Expect(supportedOperators).To(ConsistOf("ocs", "lso", "cnv"))
		})

		It("should provide properties of an operator", func() {
			properties, err := manager.GetOperatorProperties("ocs")

			Expect(err).ToNot(HaveOccurred())
			Expect(properties).To(BeEquivalentTo(models.OperatorProperties{}))
		})
	})
})
