package operators_test

import (
	"context"
	"encoding/base64"
	"errors"

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
	"github.com/openshift/assisted-service/internal/operators/odf"
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
			ID:               &clusterID,
			OpenshiftVersion: "4.8.1",
		},
	}
	cluster.ImageInfo = &models.ImageInfo{}
	b, err := common.MarshalInventory(&models.Inventory{
		CPU:    &models.CPU{Count: 8},
		Memory: &models.Memory{UsableBytes: 64 * conversions.GiB},
		Disks: []*models.Disk{
			{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: common.TestDiskId},
			{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeSSD}}})
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
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).DoAndReturn(
				func(ctx context.Context, params operations.V2CreateClusterManifestParams, isCustomManifest bool) (*models.Manifest, error) {
					manifestContent, err := base64.StdEncoding.DecodeString(*params.CreateManifestParams.Content)
					if err != nil {
						return nil, err
					}
					if _, err := yaml.YAMLToJSON(manifestContent); err != nil {
						return nil, err
					}
					return &models.Manifest{}, nil
				}).AnyTimes()
			err := manager.GenerateManifests(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create 8 manifests (ODF + LSO) using the manifest API and openshift version is 4.8.X", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			m := models.Manifest{}

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should create 8 manifests (ODF + LSO) using the manifest API and openshift version is 4.9.X or above", func() {
			cluster.OpenshiftVersion = "4.9.0"
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			m := models.Manifest{}

			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should create 4 manifests (LSO) using the manifest API", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&lso.Operator,
			}
			m := models.Manifest{}
			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(3)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should create 8 manifests (CNV + LSO) using the manifest API", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&cnv.Operator,
				&lso.Operator,
			}
			m := models.Manifest{}
			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(6)
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
			table.Entry("true for odf operator", []*models.MonitoredOperator{
				&odf.Operator,
			}, true),
			table.Entry("true for lso and odf operators", []*models.MonitoredOperator{
				&lso.Operator,
				&odf.Operator,
			}, true),
			table.Entry("true for lso, odf and cnv operators", []*models.MonitoredOperator{
				&lso.Operator,
				&odf.Operator,
				&cnv.Operator,
			}, true),
		)
	})

	Context("EnsureLVMAndCNVDoNotClash", func() {
		It("should return error when both cnv and lvm operator enabled before 4.12", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, "4.11.0", monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("no error when both cnv and lvm operator enabled after 4.12", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, "4.12.0", monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("lvm operator enabled without cnv before 4.12", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, "4.11.0", monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("cnv operator enabled without lvm before 4.12", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, "4.11.0", monitoredOperators)
			Expect(err).To(BeNil())
		})
	})

	Context("EnsureOperatorArchCapability", func() {
		It("no error on LVM with ARM architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "lvm"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("no error on all operators with x86 architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
				{Name: "mce"},
			}
			cluster.CPUArchitecture = common.X86CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("error on LVM with LSO architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "lso"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("error on LVM with ODF architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "odf"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("error on LVM with CNV architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "cnv"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("no on operators supports both ARM and x86 while cluster supports multi cpu architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}
			cluster.CPUArchitecture = common.MultiCPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)

			Expect(err).To(BeNil())
		})
		It("error on operators supports both ARM and x86 while ARM architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)

			Expect(err).To(Not(BeNil()))
			ExpectWithOffset(1, err.Error()).To(ContainSubstring("Local Storage Operator"))
			ExpectWithOffset(1, err.Error()).To(ContainSubstring("OpenShift Data Foundation"))
			ExpectWithOffset(1, err.Error()).To(ContainSubstring("OpenShift Virtualization"))
			ExpectWithOffset(1, err.Error()).To(Not(ContainSubstring("Logical Volume Management")))
		})

	})

	Context("ValidateCluster", func() {
		It("should deem operators cluster-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(5))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied), Reasons: []string{"odf is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
			))
		})

		It("should deem ODF operator cluster-invalid when it's enabled and invalid", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(5))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Failure, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied),
					Reasons: []string{"A minimum of 3 hosts is required to deploy ODF."}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
			))
		})
	})

	Context("ValidateHost", func() {
		It("should deem operators host-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(5))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{"odf is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
			))
		})

		It("should deem operators host-valid when ODF is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(5))

			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
			))
		})
	})

	Context("ResolveDependencies", func() {
		table.DescribeTable("should resolve dependencies", func(input []*models.MonitoredOperator, expected []*models.MonitoredOperator) {
			cluster.MonitoredOperators = input
			resolvedDependencies, err := manager.ResolveDependencies(cluster, cluster.MonitoredOperators)
			Expect(err).ToNot(HaveOccurred())

			Expect(resolvedDependencies).To(HaveLen(len(expected)))
			Expect(resolvedDependencies).To(ContainElements(expected))
		},
			table.Entry("when only LSO is specified",
				[]*models.MonitoredOperator{&lso.Operator},
				[]*models.MonitoredOperator{&lso.Operator},
			),
			table.Entry("when only ODF is specified",
				[]*models.MonitoredOperator{&odf.Operator},
				[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
			),
			table.Entry("when both ODF and LSO are specified",
				[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
				[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
			),
			table.Entry("when only CNV is specified",
				[]*models.MonitoredOperator{&cnv.Operator},
				[]*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
			),
			table.Entry("when CNV, ODF and LSO are specified",
				[]*models.MonitoredOperator{&cnv.Operator, &odf.Operator, &lso.Operator},
				[]*models.MonitoredOperator{&cnv.Operator, &odf.Operator, &lso.Operator},
			),
		)
	})

	Context("Supported Operators", func() {
		It("should provide list of supported operators", func() {
			supportedOperators := manager.GetSupportedOperators()

			Expect(supportedOperators).To(ConsistOf("odf", "lso", "cnv", "lvm", "mce"))
		})

		It("should provide properties of an operator", func() {
			properties, err := manager.GetOperatorProperties("odf")

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
