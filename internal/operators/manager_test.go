package operators_test

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

var (
	ctx          = context.Background()
	cluster      *common.Cluster
	clusterHost  *models.Host
	log          = logrus.New()
	manager      *operators.Manager
	ctrl         *gomock.Controller
	manifestsAPI *manifestsapi.MockManifestsAPI
	mockS3Api    *s3wrapper.MockAPI
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
	b, err := common.MarshalInventory(&models.Inventory{
		CPU:    &models.CPU{Count: 8},
		Memory: &models.Memory{UsableBytes: 64 * conversions.GiB},
		Disks: []*models.Disk{
			{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: common.TestDiskId},
			{SizeBytes: 40 * conversions.GB, DriveType: "SSD"}}})
	Expect(err).To(Not(HaveOccurred()))
	clusterHost = &models.Host{Inventory: b, Role: models.HostRoleMaster, InstallationDiskID: common.TestDiskId}

	ctrl = gomock.NewController(GinkgoT())
	manifestsAPI = manifestsapi.NewMockManifestsAPI(ctrl)
	mockS3Api = s3wrapper.NewMockAPI(ctrl)
	manager = operators.NewManager(log, manifestsAPI, operators.Options{}, mockS3Api, nil)
})

var _ = AfterEach(func() {
	ctrl.Finish()
})
var _ = Describe("Operators manager", func() {

	Context("GenerateManifests", func() {
		It("Check YAMLs of all supported OLM operators", func() {
			cluster.MonitoredOperators = manager.GetSupportedOperatorsByType(models.OperatorTypeOlm)

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, params operations.CreateClusterManifestParams) middleware.Responder {
					manifestContent, err := base64.StdEncoding.DecodeString(*params.CreateManifestParams.Content)
					if err != nil {
						return common.GenerateErrorResponder(err)
					}
					if _, err := yaml.YAMLToJSON(manifestContent); err != nil {
						return common.GenerateErrorResponder(err)
					}
					return operations.NewV2CreateClusterManifestCreated()
				}).AnyTimes()
			err := manager.GenerateManifests(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create 8 manifests (OCS + LSO) using the manifest API", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&ocs.Operator,
				&lso.Operator,
			}

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(operations.NewV2CreateClusterManifestCreated()).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should create 4 manifests (LSO) using the manifest API", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&lso.Operator,
			}

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(operations.NewV2CreateClusterManifestCreated()).Times(3)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should create 8 manifests (CNV + LSO) using the manifest API", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&cnv.Operator,
				&lso.Operator,
			}

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(operations.NewV2CreateClusterManifestCreated()).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
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
				&lso.Operator,
			}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(3))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Failure, ValidationId: string(models.ClusterValidationIDOcsRequirementsSatisfied),
					Reasons: []string{"A minimum of 3 hosts is required to deploy OCS."}},
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
				&lso.Operator,
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

	Context("ResolveDependencies", func() {
		table.DescribeTable("should resolve dependencies", func(input []*models.MonitoredOperator, expected []*models.MonitoredOperator) {
			cluster.MonitoredOperators = input
			resolvedDependencies, err := manager.ResolveDependencies(cluster.MonitoredOperators)
			Expect(err).ToNot(HaveOccurred())

			Expect(resolvedDependencies).To(HaveLen(len(expected)))
			Expect(resolvedDependencies).To(ContainElements(expected))
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

	Context("Host requirements", func() {
		const (
			operatorName1 = "operator-1"
			operatorName2 = "operator-2"
		)
		var (
			manager              *operators.Manager
			operator1, operator2 *api.MockOperator
			host                 *models.Host
		)

		BeforeEach(func() {
			operator1 = mockOperatorBase(operatorName1)
			operator2 = mockOperatorBase(operatorName2)
			cluster.MonitoredOperators = models.MonitoredOperatorsList{{Name: operatorName1}, {Name: operatorName2}}
			manager = operators.NewManagerWithOperators(log, manifestsAPI, operators.Options{}, nil, operator1, operator2)
			host = &models.Host{}
		})
		It("should be provided for configured operators", func() {
			host.Role = models.HostRoleMaster

			requirements1 := models.ClusterHostRequirementsDetails{CPUCores: 1}
			operator1.EXPECT().GetHostRequirements(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(&requirements1, nil)
			requirements2 := models.ClusterHostRequirementsDetails{CPUCores: 2}
			operator2.EXPECT().GetHostRequirements(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(&requirements2, nil)

			reqBreakdown, err := manager.GetRequirementsBreakdownForHostInCluster(context.TODO(), cluster, host)

			Expect(err).ToNot(HaveOccurred())
			Expect(reqBreakdown).To(HaveLen(2))
			Expect(reqBreakdown).To(ContainElements(
				&models.OperatorHostRequirements{OperatorName: operatorName1, Requirements: &requirements1},
				&models.OperatorHostRequirements{OperatorName: operatorName2, Requirements: &requirements2},
			))
		})

		It("should return error", func() {
			host.Role = models.HostRoleMaster
			requirements1 := models.ClusterHostRequirementsDetails{CPUCores: 1}
			operator1.EXPECT().GetHostRequirements(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(&requirements1, nil)

			theError := errors.New("boom")
			operator2.EXPECT().GetHostRequirements(gomock.Any(), gomock.Eq(cluster), gomock.Eq(host)).Return(nil, theError)

			_, err := manager.GetRequirementsBreakdownForHostInCluster(context.TODO(), cluster, host)

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeEquivalentTo(theError))
		})
	})

	Context("Preflight requirements", func() {
		const (
			operatorName1 = "operator-1"
			operatorName2 = "operator-2"
		)
		var (
			manager              *operators.Manager
			operator1, operator2 *api.MockOperator
		)

		BeforeEach(func() {
			operator1 = mockOperatorBase(operatorName1)
			operator2 = mockOperatorBase(operatorName2)
			manager = operators.NewManagerWithOperators(log, manifestsAPI, operators.Options{}, nil, operator1, operator2)
		})
		It("should be provided for configured operators", func() {
			requirements1 := models.OperatorHardwareRequirements{OperatorName: operatorName1}
			operator1.EXPECT().GetPreflightRequirements(gomock.Any(), gomock.Eq(cluster)).Return(&requirements1, nil)
			requirements2 := models.OperatorHardwareRequirements{OperatorName: operatorName2}
			operator2.EXPECT().GetPreflightRequirements(gomock.Any(), gomock.Eq(cluster)).Return(&requirements2, nil)

			reqBreakdown, err := manager.GetPreflightRequirementsBreakdownForCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(reqBreakdown).To(HaveLen(2))
			Expect(reqBreakdown).To(ContainElements(
				&models.OperatorHardwareRequirements{OperatorName: operatorName1},
				&models.OperatorHardwareRequirements{OperatorName: operatorName2},
			))
		})

		It("should return error", func() {
			theError := errors.New("boom")
			operator1.EXPECT().GetPreflightRequirements(gomock.Any(), gomock.Eq(cluster)).Return(nil, theError).AnyTimes()
			operator2.EXPECT().GetPreflightRequirements(gomock.Any(), gomock.Eq(cluster)).Return(nil, theError).AnyTimes()

			_, err := manager.GetPreflightRequirementsBreakdownForCluster(context.TODO(), cluster)

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeEquivalentTo(theError))
		})
	})
})

func mockOperatorBase(operatorName string) *api.MockOperator {
	operator1 := api.NewMockOperator(ctrl)
	operator1.EXPECT().GetName().AnyTimes().Return(operatorName)
	monitoredOperator1 := &models.MonitoredOperator{}
	operator1.EXPECT().GetMonitoredOperator().Return(monitoredOperator1)

	return operator1
}
