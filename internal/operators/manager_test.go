package operators_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/fenceagentsremediation"
	"github.com/openshift/assisted-service/internal/operators/kubedescheduler"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/mtv"
	"github.com/openshift/assisted-service/internal/operators/nmstate"
	"github.com/openshift/assisted-service/internal/operators/nodehealthcheck"
	"github.com/openshift/assisted-service/internal/operators/nodemaintenance"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/operators/openshiftai"
	"github.com/openshift/assisted-service/internal/operators/selfnoderemediation"
	"github.com/openshift/assisted-service/internal/operators/serverless"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

func MatchControllerManifest(expectedName, expectedDecodedBodyRegexp string) gomock.Matcher {
	return controllerManifestMatcher{
		expectedName,
		expectedDecodedBodyRegexp,
	}
}

func decodeInterface(x interface{}) ([]map[string]string, error) {
	data := []map[string]string{}
	jsonContentBytes, ok := x.([]byte)
	if !ok {
		return data, errors.New("interface is not of expected type []byte")
	}
	err := json.Unmarshal(jsonContentBytes, &data)
	return data, err

}

type controllerManifestMatcher struct {
	expectedName              string
	expectedDecodedBodyRegexp string
}

func (m controllerManifestMatcher) Matches(x interface{}) bool {
	manifestList, err := decodeInterface(x)
	if err != nil {
		return false
	}
	for _, manifest := range manifestList {
		if manifest["Name"] == m.expectedName {
			decodedManifest, err := base64.StdEncoding.DecodeString(manifest["Content"])
			if err != nil {
				return false
			}
			r := regexp.MustCompile(m.expectedDecodedBodyRegexp)
			match := r.FindString(string(decodedManifest))
			if len(match) > 1 {
				return true
			}
		}
	}
	return false
}

func (m controllerManifestMatcher) String() string {
	return fmt.Sprintf("content for objects with name %s should match regexp %s", m.expectedName, m.expectedDecodedBodyRegexp)
}

func (m controllerManifestMatcher) Got(got interface{}) string {
	data, err := decodeInterface(got)
	return fmt.Sprintf("%v (%v)", data, err)
}

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
			OpenshiftVersion: "4.14.0",
		},
	}
	cluster.ImageInfo = &models.ImageInfo{}
	clusterHost = getMockHostWithDisks(int64(20), int64(40))

	ctrl = gomock.NewController(GinkgoT())
	manifestsAPI = manifestsapi.NewMockManifestsAPI(ctrl)
	mockS3Api = s3wrapper.NewMockAPI(ctrl)
	manager = operators.NewManager(log, manifestsAPI, operators.Options{}, mockS3Api)
})

var validYamlOrError = func(ctx context.Context, params operations.V2CreateClusterManifestParams, isCustomManifest bool) (*models.Manifest, error) {
	manifestContent, err := base64.StdEncoding.DecodeString(*params.CreateManifestParams.Content)
	if err != nil {
		return nil, err
	}
	if _, err := yaml.YAMLToJSON(manifestContent); err != nil {
		return nil, err
	}
	return &models.Manifest{}, nil
}

var _ = AfterEach(func() {
	ctrl.Finish()
})
var _ = Describe("Operators manager", func() {

	Context("GenerateManifests", func() {
		It("Check YAMLs of all supported OLM operators", func() {
			cluster.MonitoredOperators = manager.GetSupportedOperatorsByType(models.OperatorTypeOlm)
			mockS3Api.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).DoAndReturn(validYamlOrError).AnyTimes()
			err := manager.GenerateManifests(ctx, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should upload AgentServiceConfig as controller manifest when MCE + ODF is deployed", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&mce.Operator,
			}

			m := models.Manifest{}

			mockS3Api.EXPECT().Upload(gomock.Any(), MatchControllerManifest(odf.Operator.Name, "(?s).*AgentServiceConfig.*storageClassName: ocs-storagecluster-cephfs.*"), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("should upload AgentServiceConfig as controller manifest when MCE + LVM is deployed", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&lvm.Operator,
				&mce.Operator,
			}

			m := models.Manifest{}

			mockS3Api.EXPECT().Upload(gomock.Any(), MatchControllerManifest(lvm.Operator.Name, "(?s).*AgentServiceConfig.*storageClassName: lvms-vg1.*"), gomock.Any()).Return(nil).Times(1)
			manifestsAPI.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&m, nil).Times(6)
			Expect(manager.GenerateManifests(ctx, cluster)).ShouldNot(HaveOccurred())
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

	DescribeTable("AnyOLMOperatorEnabled, should report any operator enabled", func(operators []*models.MonitoredOperator, expected bool) {
		cluster.MonitoredOperators = operators
		results := manager.AnyOLMOperatorEnabled(cluster)
		Expect(results).To(Equal(expected))
	},
		Entry("false for no operators", []*models.MonitoredOperator{}, false),
		Entry("true for lso operator", []*models.MonitoredOperator{
			&lso.Operator,
		}, true),
		Entry("true for odf operator", []*models.MonitoredOperator{
			&odf.Operator,
		}, true),
		Entry("true for lso and odf operators", []*models.MonitoredOperator{
			&lso.Operator,
			&odf.Operator,
		}, true),
		Entry("true for lso, odf and cnv operators", []*models.MonitoredOperator{
			&lso.Operator,
			&odf.Operator,
			&cnv.Operator,
		}, true),
	)

	Context("EnsureLVMAndCNVDoNotClash", func() {
		It("should return error when both cnv and lvm operator enabled before 4.12", func() {
			testVersion := "4.11.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("no error when both cnv and lvm operator enabled after 4.12", func() {
			testVersion := "4.12.0"
			cluster.ControlPlaneCount = 1
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())

		})
		It("lvm operator enabled without cnv before 4.12", func() {
			testVersion := "4.11.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("lvm operator enabled without cnv after 4.12", func() {
			testVersion := "4.13.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("cnv operator enabled without lvm before 4.12", func() {
			testVersion := "4.11.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("cnv operator enabled without lvm after 4.12", func() {
			testVersion := "4.13.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("cnv operator enabled with lvm after 4.15", func() {
			testVersion := "4.15.0"
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "lvm"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("cnv operator enabled with lvm after 4.15 multinode", func() {
			testVersion := "4.15.0"
			cluster.ControlPlaneCount = common.MinMasterHostsNeededForInstallationInHaMode
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "lvm"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).To(BeNil())
		})
		It("fail cnv operator enabled with lvm before 4.15 multinode", func() {
			testVersion := "4.14.0"
			cluster.ControlPlaneCount = common.MinMasterHostsNeededForInstallationInHaMode
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "cnv"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "lvm"},
			}
			err := operators.EnsureLVMAndCNVDoNotClash(cluster, testVersion, monitoredOperators)
			Expect(err).ToNot(BeNil())
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
		It("error on LSO with ARM architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "lso"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("error on ODF with ARM architecture", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "odf"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()))
		})
		It("error on CNV with ARM architecture below version 4.14", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "cnv"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			for _, version := range []string{"4.8", "4.11", "4.12", "4.13"} {
				v := version
				cluster.OpenshiftVersion = v
				err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
				Expect(err).To(Not(BeNil()),
					fmt.Sprintf("ocpVersion: %v, with error: %v", cluster.OpenshiftVersion, err))
			}
		})
		It("no error on CNV with ARM architecture above version 4.14", func() {
			monitoredOperators := []*models.MonitoredOperator{{Name: "cnv"}}
			cluster.CPUArchitecture = common.ARM64CPUArchitecture
			for _, version := range []string{"4.14", "4.15", "4.16", "4.17", "4.31"} {
				v := version
				cluster.OpenshiftVersion = v
				err := manager.EnsureOperatorArchCapability(cluster, cluster.CPUArchitecture, monitoredOperators)
				Expect(err).To(BeNil(),
					fmt.Sprintf("ocpVersion: %v, with error: %v", cluster.OpenshiftVersion, err))
			}
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

		DescribeTable("Validate CNV with ARM on architecture", func(ocpVersion string, shouldSupport string) {
			cluster.OpenshiftVersion = ocpVersion
			monitoredOperators := []*models.MonitoredOperator{
				{Name: "lvm"},
				{Name: "lso"},
				{Name: "odf"},
				{Name: "cnv"},
			}

			err := manager.EnsureOperatorArchCapability(cluster, common.ARM64CPUArchitecture, monitoredOperators)
			Expect(err).To(Not(BeNil()),
				fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))

			ExpectWithOffset(1, err.Error()).To(ContainSubstring("Local Storage Operator"),
				fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))
			ExpectWithOffset(1, err.Error()).To(ContainSubstring("OpenShift Data Foundation"),
				fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))
			ExpectWithOffset(1, err.Error()).To(Not(ContainSubstring("Logical Volume Management")),
				fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))
			if shouldSupport == "Supported" {
				ExpectWithOffset(1, err.Error()).To(ContainSubstring("OpenShift Virtualization"),
					fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))
			} else {
				ExpectWithOffset(1, err.Error()).To(Not(ContainSubstring("OpenShift Virtualization")),
					fmt.Sprintf("OCPversion: %s, Got error: %s", common.ARM64CPUArchitecture, err))
			}

		},
			Entry("ArchCapability version 4.11", "4.11", "Supported"),
			Entry("ArchCapability version 4.13", "4.13", "Supported"),
			Entry("ArchCapability version 4.14", "4.14", "Not Supported"),
			Entry("ArchCapability version 4.21", "4.21", "Not Supported"),
		)
	})

	Context("ValidateCluster", func() {
		It("should deem operators cluster-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(22))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied), Reasons: []string{"odf is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeFeatureDiscoveryRequirementsSatisfied), Reasons: []string{"node-feature-discovery is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNvidiaGpuRequirementsSatisfied), Reasons: []string{"nvidia-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDPipelinesRequirementsSatisfied), Reasons: []string{"pipelines is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDServicemeshRequirementsSatisfied), Reasons: []string{"servicemesh is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDServerlessRequirementsSatisfied), Reasons: []string{"serverless is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOpenshiftAiRequirementsSatisfied), Reasons: []string{"openshift-ai is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDAuthorinoRequirementsSatisfied), Reasons: []string{"authorino is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNmstateRequirementsSatisfied), Reasons: []string{"nmstate is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDAmdGpuRequirementsSatisfied), Reasons: []string{"amd-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKmmRequirementsSatisfied), Reasons: []string{"kmm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeHealthcheckRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodehealthcheck.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDSelfNodeRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", selfnoderemediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", fenceagentsremediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodemaintenance.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", kubedescheduler.Operator.Name)}},
			))
		})

		It("should deem ODF operator cluster-invalid when it's enabled and invalid", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			results, err := manager.ValidateCluster(context.TODO(), cluster)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(22))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Failure, ValidationId: string(models.ClusterValidationIDOdfRequirementsSatisfied),
					Reasons: []string{"The cluster must either have no dedicated worker nodes or at least three. Add or remove hosts, or change their roles configurations to meet the requirement."}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeFeatureDiscoveryRequirementsSatisfied), Reasons: []string{"node-feature-discovery is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNvidiaGpuRequirementsSatisfied), Reasons: []string{"nvidia-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDPipelinesRequirementsSatisfied), Reasons: []string{"pipelines is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDServicemeshRequirementsSatisfied), Reasons: []string{"servicemesh is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDServerlessRequirementsSatisfied), Reasons: []string{"serverless is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOpenshiftAiRequirementsSatisfied), Reasons: []string{"openshift-ai is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDAuthorinoRequirementsSatisfied), Reasons: []string{"authorino is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNmstateRequirementsSatisfied), Reasons: []string{"nmstate is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDAmdGpuRequirementsSatisfied), Reasons: []string{"amd-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKmmRequirementsSatisfied), Reasons: []string{"kmm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeHealthcheckRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodehealthcheck.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDSelfNodeRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", selfnoderemediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", fenceagentsremediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodemaintenance.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", kubedescheduler.Operator.Name)}},
			))
		})
	})

	Context("ValidateHost", func() {
		It("should deem operators host-valid when none is present", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(22))
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{"odf is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeFeatureDiscoveryRequirementsSatisfied), Reasons: []string{"node-feature-discovery is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNvidiaGpuRequirementsSatisfied), Reasons: []string{"nvidia-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDPipelinesRequirementsSatisfied), Reasons: []string{"pipelines is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDServicemeshRequirementsSatisfied), Reasons: []string{"servicemesh is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDServerlessRequirementsSatisfied), Reasons: []string{"serverless is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOpenshiftAiRequirementsSatisfied), Reasons: []string{"openshift-ai is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDAuthorinoRequirementsSatisfied), Reasons: []string{"authorino is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNmstateRequirementsSatisfied), Reasons: []string{"nmstate is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDAmdGpuRequirementsSatisfied), Reasons: []string{"amd-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKmmRequirementsSatisfied), Reasons: []string{"kmm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeHealthcheckRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodehealthcheck.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDSelfNodeRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", selfnoderemediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", fenceagentsremediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodemaintenance.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", kubedescheduler.Operator.Name)}},
			))
		})

		It("should deem operators host-valid when ODF is enabled", func() {
			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&lso.Operator,
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)
			Expect(err).ToNot(HaveOccurred())
			Expect(results).To(HaveLen(22))

			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeFeatureDiscoveryRequirementsSatisfied), Reasons: []string{"node-feature-discovery is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNvidiaGpuRequirementsSatisfied), Reasons: []string{"nvidia-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDPipelinesRequirementsSatisfied), Reasons: []string{"pipelines is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDServicemeshRequirementsSatisfied), Reasons: []string{"servicemesh is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDServerlessRequirementsSatisfied), Reasons: []string{"serverless is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOpenshiftAiRequirementsSatisfied), Reasons: []string{"openshift-ai is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDAuthorinoRequirementsSatisfied), Reasons: []string{"authorino is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNmstateRequirementsSatisfied), Reasons: []string{"nmstate is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDAmdGpuRequirementsSatisfied), Reasons: []string{"amd-gpu is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKmmRequirementsSatisfied), Reasons: []string{"kmm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDNodeHealthcheckRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodehealthcheck.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDSelfNodeRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", selfnoderemediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDFenceAgentsRemediationRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", fenceagentsremediation.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDNodeMaintenanceRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", nodemaintenance.Operator.Name)}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDKubeDeschedulerRequirementsSatisfied), Reasons: []string{fmt.Sprintf("%s is disabled", kubedescheduler.Operator.Name)}},
			))
		})

		It("should be not valid if not enough disk space available for mce and odf", func() {
			clusterHost = getMockHostWithDisks(int64(50), int64(50))

			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&mce.Operator,
			}

			// For compact mode
			cluster.Hosts = []*models.Host{
				{Role: models.HostRoleMaster},
				{Role: models.HostRoleMaster},
				{Role: models.HostRoleMaster},
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Failure, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{"Insufficient resources to deploy ODF in compact mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 75 GB minimum and an installation disk."}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: nil},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
			))
		})

		It("should be valid if enough disk space available for mce and odf", func() {
			clusterHost = getMockHostWithDisks(int64(20), int64(80))

			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&mce.Operator,
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: nil},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
			))
		})

		It("should be valid if enough disk space available for mce and odf and then only odf", func() {
			clusterHost = getMockHostWithDisks(int64(20), int64(80))

			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
				&mce.Operator,
			}

			results, err := manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: nil},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
			))

			clusterHost = getMockHostWithDisks(int64(20), int64(30))

			cluster.MonitoredOperators = []*models.MonitoredOperator{
				&odf.Operator,
			}

			results, err = manager.ValidateHost(context.TODO(), cluster, clusterHost)

			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(ContainElements(
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied), Reasons: []string{"lso is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied), Reasons: []string{}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMceRequirementsSatisfied), Reasons: []string{"mce is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied), Reasons: []string{"cnv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied), Reasons: []string{"lvm is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDMtvRequirementsSatisfied), Reasons: []string{"mtv is disabled"}},
				api.ValidationResult{Status: api.Success, ValidationId: string(models.ClusterValidationIDOscRequirementsSatisfied), Reasons: []string{"osc is disabled"}},
			))
		})
	})

	DescribeTable("ResolveDependencies, should resolve dependencies", func(input []*models.MonitoredOperator, expected []*models.MonitoredOperator) {
		cluster.MonitoredOperators = input
		resolvedDependencies, err := manager.ResolveDependencies(cluster, cluster.MonitoredOperators)
		Expect(err).ToNot(HaveOccurred())

		Expect(resolvedDependencies).To(HaveLen(len(expected)))
		Expect(resolvedDependencies).To(ContainElements(expected))
	},
		Entry("when only LSO is specified",
			[]*models.MonitoredOperator{&lso.Operator},
			[]*models.MonitoredOperator{&lso.Operator},
		),
		Entry("when only ODF is specified",
			[]*models.MonitoredOperator{&odf.Operator},
			[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
		),
		Entry("when both ODF and LSO are specified",
			[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
			[]*models.MonitoredOperator{&odf.Operator, &lso.Operator},
		),
		Entry("when only CNV is specified",
			[]*models.MonitoredOperator{&cnv.Operator},
			[]*models.MonitoredOperator{&cnv.Operator, &lso.Operator},
		),
		Entry("when CNV, ODF and LSO are specified",
			[]*models.MonitoredOperator{&cnv.Operator, &odf.Operator, &lso.Operator},
			[]*models.MonitoredOperator{&cnv.Operator, &odf.Operator, &lso.Operator},
		),
	)

	Context("Supported Operators", func() {
		It("should provide list of supported operators", func() {
			supportedOperators := manager.GetSupportedOperators()

			Expect(supportedOperators).To(ConsistOf(
				"cnv",
				"lso",
				"lvm",
				"mce",
				"mtv",
				"osc",
				"node-feature-discovery",
				"nvidia-gpu",
				"odf",
				"openshift-ai",
				"pipelines",
				"serverless",
				"servicemesh",
				"authorino",
				"nmstate",
				"amd-gpu",
				"kmm",
				nodehealthcheck.Operator.Name,
				selfnoderemediation.Operator.Name,
				fenceagentsremediation.Operator.Name,
				nodemaintenance.Operator.Name,
				kubedescheduler.Operator.Name,
			))
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

	Context("Bundles", func() {
		// we use the real operators here, as we want to test the manager's ability to group them into bundles
		var (
			manager                                                                                              *operators.Manager
			cnvOperator, odfOperator, oaiOperator, serverlessOperator, lsoOperator, nmstateOperator, mtvOperator api.Operator
		)
		BeforeEach(func() {
			cfg := cnv.Config{}
			cnvOperator = cnv.NewCNVOperator(log, cfg)
			// note that odf belongs to both Virtualization and Openshift AI bundles
			odfOperator = odf.NewOdfOperator(log)
			oaiOperator = openshiftai.NewOpenShiftAIOperator(log)
			nmstateOperator = nmstate.NewNmstateOperator(log)
			serverlessOperator = serverless.NewServerLessOperator(log)
			// note that lso doesn't belongs to any bundle
			lsoOperator = lso.NewLSOperator()
			mtvOperator = mtv.NewMTVOperator(log)

			manager = operators.NewManagerWithOperators(log, manifestsAPI, operators.Options{}, nil, cnvOperator, odfOperator, oaiOperator, serverlessOperator, lsoOperator, nmstateOperator, mtvOperator)
		})

		It("ListBundles should return the list of available bundles", func() {
			bundles := manager.ListBundles()
			bundleIDs := make([]string, len(bundles))
			for i, bundle := range bundles {
				bundleIDs[i] = bundle.ID
			}
			Expect(bundleIDs).To(ConsistOf(
				operatorscommon.BundleVirtualization.ID,
				operatorscommon.BundleOpenShiftAINVIDIA.ID,
				operatorscommon.BundleOpenShiftAIAMD.ID,
			))
		})

		It("Virtualization bundle contains the MTV and CNV operators", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleVirtualization.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle).ToNot(BeNil())
			Expect(bundle.Operators).To(ContainElements(
				mtvOperator.GetName(),
				cnvOperator.GetName(),
			))
		})

		It("OpenShift AI NVIDIA bundle contains the OpenShift AI, Serverless and ODF operators", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleOpenShiftAINVIDIA.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle).ToNot(BeNil())
			Expect(bundle.Operators).To(ContainElements(
				oaiOperator.GetName(),
				serverlessOperator.GetName(),
				odfOperator.GetName(),
			))
		})

		It("LSO isn't part of any bundle", func() {
			bundles := manager.ListBundles()
			for _, bundle := range bundles {
				Expect(bundle.Operators).NotTo(ContainElement(lso.Operator.Name))
			}
		})

		It("Fails with incorrect bundle name", func() {
			bundle, err := manager.GetBundle("invalid bundle")
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("bundle 'invalid bundle' is not supported"))
			Expect(bundle).To(BeNil())
		})

		It("OpenShift AI NVIDIA bundle should have a description", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleOpenShiftAINVIDIA.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Description).ToNot(BeEmpty())
		})

		It("OpenShift AI NVIDIA bundle should have a title", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleOpenShiftAINVIDIA.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Title).ToNot(BeEmpty())
		})

		It("OpenShift AI AMD bundle should have a description", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleOpenShiftAIAMD.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Description).ToNot(BeEmpty())
		})

		It("OpenShift AI AMD bundle should have a title", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleOpenShiftAIAMD.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Title).ToNot(BeEmpty())
		})

		It("Virtualization bundle should have a description", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleVirtualization.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Description).ToNot(BeEmpty())
		})

		It("Virtualization bundle should have a title", func() {
			bundle, err := manager.GetBundle(operatorscommon.BundleVirtualization.ID)
			Expect(err).ToNot(HaveOccurred())
			Expect(bundle.Title).ToNot(BeEmpty())
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

func getMockHostWithDisks(sizeDiskA, sizeDiskB int64) *models.Host {
	hostID := strfmt.UUID(uuid.New().String())
	b, err := common.MarshalInventory(&models.Inventory{
		CPU:    &models.CPU{Count: 8},
		Memory: &models.Memory{UsableBytes: 64 * conversions.GiB},
		Disks: []*models.Disk{
			{SizeBytes: sizeDiskA * conversions.GB, DriveType: models.DriveTypeHDD, ID: common.TestDiskId},
			{SizeBytes: sizeDiskB * conversions.GB, DriveType: models.DriveTypeSSD}}})
	Expect(err).To(Not(HaveOccurred()))
	return &models.Host{ID: &hostID, Inventory: b, Role: models.HostRoleMaster, InstallationDiskID: common.TestDiskId}
}
