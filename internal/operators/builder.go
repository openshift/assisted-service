package operators

import (
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var OperatorCVO models.MonitoredOperator = models.MonitoredOperator{
	Name:           "cvo",
	OperatorType:   models.OperatorTypeBuiltin,
	TimeoutSeconds: 60 * 60,
}

var OperatorConsole models.MonitoredOperator = models.MonitoredOperator{
	Name:           "console",
	OperatorType:   models.OperatorTypeBuiltin,
	TimeoutSeconds: 60 * 60,
}

// monitoredOperators includes all the supported operators to be monitored.
// In order to add a new operator - it must be added to this map.
var monitoredOperators = map[string]*models.MonitoredOperator{
	// Builtins
	OperatorCVO.Name:     &OperatorCVO,
	OperatorConsole.Name: &OperatorConsole,

	// OLMs
	lso.Operator.Name: &lso.Operator,
	ocs.Operator.Name: &ocs.Operator,
}

// NewManager creates new instance of an Operator Manager
func NewManager(log logrus.FieldLogger) *Manager {
	return NewManagerWithOperators(log, lso.NewLSOperator(), ocs.NewOcsOperator(log), cnv.NewCNVOperator(log))
}

// NewManagerWithOperators creates new instance of an Operator Manager and configures it with given operators
func NewManagerWithOperators(log logrus.FieldLogger, olmOperators ...api.Operator) *Manager {
	nameToOperator := make(map[string]api.Operator)
	for _, olmOperator := range olmOperators {
		nameToOperator[olmOperator.GetName()] = olmOperator
	}

	return &Manager{
		log:                log,
		olmOperators:       nameToOperator,
		monitoredOperators: monitoredOperators,
	}
}
