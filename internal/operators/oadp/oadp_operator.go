package oadp

import (
	"text/template"

	"github.com/lib/pq"
	"github.com/openshift/assisted-service/internal/common"
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/templating"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var Operator = models.MonitoredOperator{
	Namespace:        operatorNamespace,
	Name:             operatorName,
	OperatorType:     models.OperatorTypeOlm,
	SubscriptionName: operatorSubscriptionName,
	TimeoutSeconds:   30 * 60,
	Bundles: pq.StringArray{
		operatorscommon.BundleVirtualization.ID,
	},
}

type operator struct {
	log       logrus.FieldLogger
	templates *template.Template
}

// NewOadpOperator creates new OADP Operator.
func NewOadpOperator(log logrus.FieldLogger) *operator {
	templates, err := templating.LoadTemplates(templatesRoot)
	if err != nil {
		log.Fatal(err.Error())
	}
	return &operator{
		log:       log.WithField("operator", operatorName),
		templates: templates,
	}
}

// GetName reports the name of an operator this Operator manages
func (o *operator) GetName() string {
	return operatorName
}

// GetFullName reports the full name of the specified Operator
func (o *operator) GetFullName() string {
	return OperatorFullName
}

// GetDependencies provides a list of dependencies of the Operator
func (o *operator) GetDependencies(cluster *common.Cluster) ([]string, error) {
	return []string{}, nil
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
	return models.FeatureSupportLevelIDOADP
}

// GetBundleLabels returns the list of bundles names associated with the operator
func (o *operator) GetBundleLabels() []string {
	return []string(Operator.Bundles)
}
