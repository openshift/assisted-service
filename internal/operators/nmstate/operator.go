package nmstate

import (
	"context"
	"fmt"

	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = string(models.ClusterValidationIDNmstateRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDNmstateRequirementsSatisfied)
)

type operator struct {
	log logrus.FieldLogger
}

var Operator = models.MonitoredOperator{
	Name:             Name,
	Namespace:        Namespace,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: SubscriptionName,
	TimeoutSeconds:   60 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleVirtualization.ID,
	},
}

// New NMSTATEperator creates new instance of a Local Storage Operator installation plugin
func NewNmstateOperator(log logrus.FieldLogger) *operator {
	return &operator{
		log: log,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Operator.Name
}

func (o *operator) GetFullName() string {
	return FullName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return make([]string, 0), nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return nil
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster verifies whether this operator is valid for given cluster
func (o *operator) ValidateCluster(_ context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	result := []api.ValidationResult{{
		Status:       api.Success,
		ValidationId: clusterValidationID,
	}}

	if !featuresupport.IsFeatureCompatibleWithArchitecture(models.FeatureSupportLevelIDNMSTATE, cluster.OpenshiftVersion, cluster.CPUArchitecture) {
		result[0].Status = api.Failure
		result[0].Reasons = []string{fmt.Sprintf("%s is not supported for %s CPU architecture.", o.GetFullName(), cluster.CPUArchitecture)}

		return result, nil
	}

	ok, err := common.BaseVersionLessThan(NmstateMinOpenshiftVersion, cluster.OpenshiftVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to compare openshift versions: %w", err)
	}

	if ok {
		result[0].Status = api.Failure
		result[0].Reasons = []string{fmt.Sprintf("%s is only supported for openshift versions %s and above", o.GetFullName(), NmstateMinOpenshiftVersion)}

		return result, nil
	}

	return result, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, _ *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(c *common.Cluster) (map[string][]byte, []byte, error) {
	return Manifests()
}

// GetProperties provides description of operator properties: none required
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the NMSTATE
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides operator's requirements towards the host
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster, host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	log := logutil.FromContext(ctx, o.log)
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Cannot retrieve preflight requirements for host %s", host.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Worker.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context, cluster *common.Cluster) (*models.OperatorHardwareRequirements, error) {
	dependecies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}

	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependecies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{},
			},
		},
	}, nil
}

func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNMSTATE
}

// GetBundleLabels returns the bundle labels for the operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
