package openshiftlogging

import (
	"context"
	"fmt"
	"text/template"

	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = string(models.ClusterValidationIDOpenshiftLoggingRequirementsSatisfied)
	hostValidationID    = string(models.HostValidationIDOpenshiftLoggingRequirementsSatisfied)
)

var Operator = models.MonitoredOperator{
	Name:             Name,
	OperatorType:     models.OperatorTypeOlm,
	Namespace:        Namespace,
	SubscriptionName: SubscriptionName,
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleVirtualization.ID,
	},
}

// operator is an OpenShift Logging OLM operator plugin; it implements api.Operator
type operator struct {
	log       logrus.FieldLogger
	templates *template.Template
}

// NewOpenShiftLoggingOperator creates new OpenShift Logging Operator
func NewOpenShiftLoggingOperator(log logrus.FieldLogger) *operator {
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log.WithField("operator", Name),
		templates: templates,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return Name
}

// GetFullName reports the full name of the specified Operator
func (o *operator) GetFullName() string {
	return FullName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	// OpenShift Logging depends on Loki Operator
	return []string{"loki"}, nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLOKI}
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// ValidateCluster checks if the cluster satisfies the requirements to install the operator
func (o *operator) ValidateCluster(ctx context.Context, cluster *common.Cluster) ([]api.ValidationResult, error) {
	result := api.ValidationResult{
		Status:       api.Success,
		ValidationId: clusterValidationID,
	}

	// Check OpenShift version
	if ok, _ := common.BaseVersionLessThan(OpenShiftLoggingMinOpenshiftVersion, cluster.OpenshiftVersion); ok {
		result.Status = api.Failure
		result.Reasons = []string{
			fmt.Sprintf("OpenShift Logging Operator is only supported for OpenShift versions %s and above", OpenShiftLoggingMinOpenshiftVersion),
		}
	}

	return []api.ValidationResult{result}, nil
}

// ValidateHost returns validationResult based on node type requirements
func (o *operator) ValidateHost(
	ctx context.Context,
	cluster *common.Cluster,
	host *models.Host,
	hostRequirements *models.ClusterHostRequirementsDetails,
) (api.ValidationResult, error) {
	return api.ValidationResult{
		Status:       api.Success,
		ValidationId: o.GetHostValidationID(),
	}, nil
}

// GetProperties provides description of operator properties
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the OpenShift Logging Operator
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetHostRequirements provides the requirements that the host needs to satisfy
func (o *operator) GetHostRequirements(ctx context.Context, cluster *common.Cluster,
	host *models.Host) (*models.ClusterHostRequirementsDetails, error) {
	preflightRequirements, err := o.GetPreflightRequirements(ctx, cluster)
	if err != nil {
		o.log.WithError(err).Errorf("Cannot retrieve preflight requirements for cluster %s", cluster.ID)
		return nil, err
	}
	return preflightRequirements.Requirements.Worker.Quantitative, nil
}

// GetPreflightRequirements returns operator hardware requirements that can be determined with cluster data only
func (o *operator) GetPreflightRequirements(context context.Context,
	cluster *common.Cluster) (result *models.OperatorHardwareRequirements, err error) {
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return
	}
	result = &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
		Requirements: &models.HostTypeHardwareRequirementsWrapper{
			Master: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			},
			Worker: &models.HostTypeHardwareRequirements{
				Quantitative: &models.ClusterHostRequirementsDetails{
					CPUCores: 0,
					RAMMib:   0,
				},
			},
		},
	}
	return
}

// GetFeatureSupportID returns the operator unique feature-support ID
func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDOPENSHIFTLOGGING
}

// GetBundleLabels returns the list of bundles names associated with the operator
func (o *operator) GetBundleLabels(featureIDs []models.FeatureSupportLevelID) []string {
	return []string(Operator.Bundles)
}
