package operators

import (
	"github.com/openshift/assisted-service/internal/operators/api"
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
	return NewManagerWithOperators(log, lso.NewLSOperator(), ocs.NewOcsOperator(log))
}

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
