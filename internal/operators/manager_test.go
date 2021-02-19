package operators_test

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var (
	cluster     *common.Cluster
	ctrl        *gomock.Controller
	log         = logrus.New()
	mockHostAPI *host.MockAPI
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

	ctrl = gomock.NewController(GinkgoT())
	mockHostAPI = host.NewMockAPI(ctrl)
	manager = operators.NewManager(log)
})

var _ = AfterEach(func() {
	ctrl.Finish()
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

	It("should deem OCS operator valid when it's absent", func() {
		cluster.Operators = ""

		valid := manager.ValidateOCSRequirements(cluster)

		Expect(valid).To(Equal("success"))
	})

	It("should deem OCS operator valid when it's disabled", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(false)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		valid := manager.ValidateOCSRequirements(cluster)

		Expect(valid).To(Equal("success"))
	})

	It("should deem OCS operator invalid when it's enabled and invalid", func() {
		clusterOperators := []*models.ClusterOperator{
			{OperatorType: models.OperatorTypeOcs, Enabled: swag.Bool(true)}}
		cluster.Operators = convertFromClusterOperators(clusterOperators)

		valid := manager.ValidateOCSRequirements(cluster)

		Expect(valid).To(Equal("failure"))
	})
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
