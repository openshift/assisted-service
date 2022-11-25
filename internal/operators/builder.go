package operators

import (
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

var OperatorCVO = models.MonitoredOperator{
	Name:           "cvo",
	OperatorType:   models.OperatorTypeBuiltin,
	TimeoutSeconds: 60 * 60,
}

var OperatorConsole = models.MonitoredOperator{
	Name:           "console",
	OperatorType:   models.OperatorTypeBuiltin,
	TimeoutSeconds: 60 * 60,
}

type Options struct {
	CheckClusterVersion bool
	CNVConfig           cnv.Config
}

// NewManager creates new instance of an Operator Manager
func NewManager(log logrus.FieldLogger, manifestAPI manifestsapi.ManifestsAPI, options Options, objectHandler s3wrapper.API, extracter oc.Extracter) *Manager {
	return NewManagerWithOperators(
		log,
		manifestAPI,
		options,
		objectHandler,
		lso.NewLSOperator(),
		odf.NewOcsOperator(log),
		odf.NewOdfOperator(log, extracter),
		cnv.NewCNVOperator(log, options.CNVConfig, extracter),
		lvm.NewLvmOperator(log, extracter),
		mce.NewMceOperator(log),
	)
}

// NewManagerWithOperators creates new instance of an Operator Manager and configures it with given operators
func NewManagerWithOperators(log logrus.FieldLogger, manifestAPI manifestsapi.ManifestsAPI, options Options, objectHandler s3wrapper.API, olmOperators ...api.Operator) *Manager {
	nameToOperator := make(map[string]api.Operator)

	// monitoredOperators includes all the supported operators to be monitored.
	monitoredOperators := map[string]*models.MonitoredOperator{
		// Builtins
		OperatorConsole.Name: &OperatorConsole,
	}

	if options.CheckClusterVersion {
		monitoredOperators[OperatorCVO.Name] = &OperatorCVO
	}

	for _, olmOperator := range olmOperators {
		nameToOperator[olmOperator.GetName()] = olmOperator
		// Add OLM operator to the monitoredOperators map
		monitoredOperators[olmOperator.GetName()] = olmOperator.GetMonitoredOperator()
	}

	return &Manager{
		log:                log,
		olmOperators:       nameToOperator,
		monitoredOperators: monitoredOperators,
		manifestsAPI:       manifestAPI,
		objectHandler:      objectHandler,
	}
}
