package operators

import (
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// NewManager creates new instance of an Operator Manager
func NewManager(log logrus.FieldLogger) Manager {
	return NewManagerWithOperators(log, lso.NewLSOperator(), ocs.NewOcsOperator(log))
}

func NewManagerWithOperators(log logrus.FieldLogger, operators ...api.Operator) Manager {
	typeToOperator := make(map[models.OperatorType]api.Operator)
	for _, operator := range operators {
		typeToOperator[operator.GetType()] = operator
	}
	return Manager{
		log:       log,
		operators: typeToOperator,
	}
}
