package networkobservability

import (
	"context"
	"text/template"

	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/api"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

const (
	clusterValidationID = "network-observability-requirements-satisfied"
	hostValidationID    = "network-observability-requirements-satisfied"
)

type operator struct {
	log       logrus.FieldLogger
	templates *template.Template
}

var Operator = models.MonitoredOperator{
	Name:             Name,
	Namespace:        Namespace,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: SubscriptionName,
	TimeoutSeconds:   60 * 60,
	Bundles:          pq.StringArray{},
}

// NewNetworkObservabilityOperator creates new instance of a Network Observability Operator installation plugin
func NewNetworkObservabilityOperator(log logrus.FieldLogger) *operator {
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

	return result, nil
}

// ValidateHost returns validationResult based on node type requirements such as memory and cpu
func (o *operator) ValidateHost(ctx context.Context, cluster *common.Cluster, host *models.Host, _ *models.ClusterHostRequirementsDetails) (api.ValidationResult, error) {
	return api.ValidationResult{Status: api.Success, ValidationId: o.GetHostValidationID()}, nil
}

// GenerateManifests generates manifests for the operator
func (o *operator) GenerateManifests(c *common.Cluster) (map[string][]byte, []byte, error) {
	// Find the operator in cluster's MonitoredOperators to get properties
	var config *Config
	var err error

	for _, clusterOp := range c.MonitoredOperators {
		if clusterOp.Name == o.GetName() {
			config, err = ParseProperties(clusterOp.Properties)
			if err != nil {
				o.log.WithError(err).Warnf("Failed to parse properties for %s, using defaults", o.GetName())
				config, _ = ParseProperties("")
			}
			break
		}
	}

	// If operator not found in cluster, use default config
	if config == nil {
		config, _ = ParseProperties("")
	}

	// Generate manifests using common.GenerateManifests
	// The FlowCollector template uses conditionals to only render when CreateFlowCollector is true
	return operatorscommon.GenerateManifests(
		templatesRoot, o.templates, config, &Operator,
	)
}

// GetProperties provides description of operator properties
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{
		{
			Name:         "createFlowCollector",
			DataType:     models.OperatorPropertyDataTypeBoolean,
			Description:  "Whether to create a FlowCollector resource automatically. If false, only the operator will be installed.",
			Mandatory:    false,
			DefaultValue: "false",
		},
		{
			Name:         "sampling",
			DataType:     models.OperatorPropertyDataTypeInteger,
			Description:  "Sampling rate for eBPF agent. A value of 50 means one packet every 50 is sampled. Lower values increase resource utilization.",
			Mandatory:    false,
			DefaultValue: "50",
		},
	}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the Network Observability Operator
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
	dependencies, err := o.GetDependencies(cluster)
	if err != nil {
		return &models.OperatorHardwareRequirements{}, err
	}

	return &models.OperatorHardwareRequirements{
		OperatorName: o.GetName(),
		Dependencies: dependencies,
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
	return models.FeatureSupportLevelIDNETWORKOBSERVABILITY
}

// GetBundleLabels returns the bundle labels for the operator
func (o *operator) GetBundleLabels(featureIDs []models.FeatureSupportLevelID) []string {
	return []string(Operator.Bundles)
}
