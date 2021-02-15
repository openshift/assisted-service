package operators

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

// NewManager creates new instance of an Operator Manager
func NewManager(log logrus.FieldLogger) Manager {
	cfg := ocs.Config{}
	err := envconfig.Process("myapp", &cfg)
	if err != nil {
		log.Fatal(err.Error())
	}
	return NewManagerWithConfig(log, &cfg)
}

// NewManagerWithConfig creates new instance of an Operator Manager
func NewManagerWithConfig(log logrus.FieldLogger, cfg *ocs.Config) Manager {
	ocsValidator := ocs.NewOCSValidator(log.WithField("pkg", "ocs-operator-state"), cfg)

	//TODO: for better testability refactor into a proper builder pattern when hardcoded OCS dependency is gone
	typeToOperator := map[models.OperatorType]api.Operator{
		models.OperatorTypeLso: lso.NewLSOperator(),
	}

	return Manager{
		log:                log,
		ocsValidatorConfig: cfg,
		ocsValidator:       ocsValidator,
		operators:          typeToOperator,
	}
}
