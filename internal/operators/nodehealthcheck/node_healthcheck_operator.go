package nodehealthcheck

import (
	"text/template"

	"github.com/kelseyhightower/envconfig"
	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var Operator = models.MonitoredOperator{
	Namespace:        operatorNamespace,
	Name:             operatorscommon.NodeHealthcheckOperatorName,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: operatorSubscriptionName,
	TimeoutSeconds:   30 * 60,
	Bundles:          pq.StringArray{},
}

type operator struct {
	log       logrus.FieldLogger
	config    *Config
	templates *template.Template
}

// NewNodeHealthcheckOperator creates new Node Healthcheck Operator.
func NewNodeHealthcheckOperator(log logrus.FieldLogger) *operator {
	config := &Config{}
	err := envconfig.Process(common.EnvConfigPrefix, config)
	if err != nil {
		log.Fatal(err.Error())
	}
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log.WithField("operator", operatorscommon.NodeHealthcheckOperatorName),
		config:    config,
		templates: templates,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return operatorscommon.NodeHealthcheckOperatorName
}

// GetFullName reports the full name of the specified Operator
func (o *operator) GetFullName() string {
	return OperatorFullName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return []string{operatorscommon.SelfNodeRemediationOperatorName}, nil
}

func (o *operator) GetDependenciesFeatureSupportID() []models.FeatureSupportLevelID {
	return []models.FeatureSupportLevelID{models.FeatureSupportLevelIDSELFNODEREMEDIATION}
}

// GetClusterValidationIDs returns cluster validation IDs for the Operator
func (o *operator) GetClusterValidationIDs() []string {
	return []string{clusterValidationID}
}

// GetHostValidationID returns host validation ID for the Operator
func (o *operator) GetHostValidationID() string {
	return hostValidationID
}

// GetProperties provides description of operator properties
func (o *operator) GetProperties() models.OperatorProperties {
	return models.OperatorProperties{}
}

// GetMonitoredOperator returns MonitoredOperator corresponding to the Operator implementation
func (o *operator) GetMonitoredOperator() *models.MonitoredOperator {
	return &Operator
}

// GetFeatureSupportID returns the operator unique feature-support ID
func (o *operator) GetFeatureSupportID() models.FeatureSupportLevelID {
	return models.FeatureSupportLevelIDNODEHEALTHCHECK
}

// GetBundleLabels returns the list of bundles names associated with the operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
