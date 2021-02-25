package operators_test

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var (
	cluster     *common.Cluster
	clusterHost *models.Host
	log         = logrus.New()
	manager     operators.Manager
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
	It("should generate OCS and LSO manifests when OCS operator is enabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		manifests, err := manager.GenerateManifests(cluster)

		Expect(err).NotTo(HaveOccurred())

		Expect(manifests).NotTo(BeNil())
		Expect(len(manifests)).To(Equal(10))
		Expect(manifests).To(HaveKey(ContainSubstring("openshift-ocs")))
		Expect(manifests).To(HaveKey(ContainSubstring("openshift-lso")))
	})

	It("should generate LSO manifests when LSO operator is enabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		manifests, err := manager.GenerateManifests(cluster)

		Expect(err).NotTo(HaveOccurred())

		Expect(manifests).NotTo(BeNil())
		Expect(len(manifests)).To(Equal(5))
		Expect(manifests).To(HaveKey(ContainSubstring("openshift-lso")))
	})

	It("should generate no manifests when no operator is present", func() {
		cluster.Operators = ""
		manifests, err := manager.GenerateManifests(cluster)
		Expect(err).NotTo(HaveOccurred())

		Expect(manifests).NotTo(BeNil())
		Expect(manifests).To(BeEmpty())
	})

	table.DescribeTable("should report any operator enabled", func(operators []*models.ClusterOperator, expected bool) {
		cluster.Operators = convertFromClusterOperators(operators)

		results := manager.AnyOperatorEnabled(cluster)
		Expect(results).To(Equal(expected))
	},
		table.Entry("false for no operators", []*models.ClusterOperator{}, false),
		table.Entry("false for lso operator disabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
		}, false),
		table.Entry("false for ocs operator disabled", []*models.ClusterOperator{{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)}}, false),
		table.Entry("false for lso and ocs operators disabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
		}, false),

		table.Entry("true for lso operator enabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
		}, true),
		table.Entry("true for ocs operator enabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
		}, true),
		table.Entry("true for lso and ocs operators enabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
		}, true),
		table.Entry("true for lso enabled and ocs disabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
		}, true),
		table.Entry("true for lso disabled and ocs enabled", []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
		}, true),
	)

	It("should report operators disabled for unmarshalling error", func() {
		cluster.Operators = "{{"

		results := manager.AnyOperatorEnabled(cluster)
		Expect(results).To(BeFalse())
	})

	It("should deem operators cluster-valid when none is present", func() {
		cluster.Operators = ""

		results, err := manager.ValidateCluster(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
			api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
		))
	})

	It("should deem operators cluster-valid when OCS is disabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		results, err := manager.ValidateCluster(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))

		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
			api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
		))
	})

	It("should deem OCS operator cluster-invalid when it's enabled and invalid", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		results, err := manager.ValidateCluster(context.TODO(), cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
			api.ValidationResult{Status: api.Failure, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied),
				Reasons: []string{"Insufficient hosts to deploy OCS. A minimum of 3 hosts is required to deploy OCS. "}},
		))
	})

	It("should deem operators host-valid when none is present", func() {
		cluster.Operators = ""

		results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))
		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
		))
	})

	It("should deem operators host-valid when OCS is disabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))

		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied), Reasons: []string{"ocs is disabled"}},
		))
	})

	It("should deem operators host-valid when OCS is enabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(2))

		Expect(results).To(ContainElements(
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
			api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOcsRequirementsSatisfied), Reasons: []string{}},
		))
	})

	table.DescribeTable("should resolve dependencies", func(input models.Operators, expected models.Operators) {
		cluster.Operators = convertFromClusterOperators(input)
		err := manager.UpdateDependencies(cluster)
		Expect(err).ToNot(HaveOccurred())
		var operators []*models.ClusterOperator
		err = json.Unmarshal([]byte(cluster.Operators), &operators)
		Expect(err).ToNot(HaveOccurred())
		Expect(operators).To(HaveLen(len(expected)))
		Expect(operators).To(ContainElements(expected))
	},
		table.Entry("when only LSO is specified",
			models.Operators{&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)}},
			models.Operators{&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)}},
		),
		table.Entry("when only OCS is specified",
			models.Operators{&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)}},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
		),
		table.Entry("when both OCS and LSO are specified",
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
		),
		table.Entry("when OCS is disabled and LSO enabled",
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
		),
		table.Entry("when both OCS and LSO are disabled",
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
			},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
			},
		),
		table.Entry("when OCS is specified and disabled",
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
			},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)},
			},
		),
		table.Entry("when OCS is enabled and LSO  disabled",
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(false)},
			},
			models.Operators{
				&models.ClusterOperator{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)},
				&models.ClusterOperator{OperatorType: models.OperatorTypeLso, Enabled: swag.Bool(true)},
			},
		),
	)
})

func convertFromClusterOperators(operators []*models.ClusterOperator) string {
	if operators == nil {
		return ""
	}
	reply, err := json.Marshal(operators)
	if err != nil {
		return ""
	}
	return string(reply)
}
