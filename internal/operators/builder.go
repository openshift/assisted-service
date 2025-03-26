package operators

import (
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/operators/amdgpu"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/operators/authorino"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/fenceagentsremediation"
	"github.com/openshift/assisted-service/internal/operators/kmm"
	"github.com/openshift/assisted-service/internal/operators/kubedescheduler"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/mtv"
	"github.com/openshift/assisted-service/internal/operators/nmstate"
	"github.com/openshift/assisted-service/internal/operators/nodefeaturediscovery"
	"github.com/openshift/assisted-service/internal/operators/nodehealthcheck"
	"github.com/openshift/assisted-service/internal/operators/nodemaintenance"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/internal/operators/openshiftai"
	"github.com/openshift/assisted-service/internal/operators/osc"
	"github.com/openshift/assisted-service/internal/operators/pipelines"
	"github.com/openshift/assisted-service/internal/operators/selfnoderemediation"
	"github.com/openshift/assisted-service/internal/operators/serverless"
	"github.com/openshift/assisted-service/internal/operators/servicemesh"
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
func NewManager(log logrus.FieldLogger, manifestAPI manifestsapi.ManifestsAPI, options Options, objectHandler s3wrapper.API) *Manager {
	return NewManagerWithOperators(
		log, manifestAPI, options, objectHandler,
		lso.NewLSOperator(),
		odf.NewOcsOperator(log),
		odf.NewOdfOperator(log),
		cnv.NewCNVOperator(log, options.CNVConfig),
		lvm.NewLvmOperator(log),
		mce.NewMceOperator(log),
		mtv.NewMTVOperator(log),
		nodefeaturediscovery.NewNodeFeatureDiscoveryOperator(log),
		nvidiagpu.NewNvidiaGPUOperator(log),
		pipelines.NewPipelinesOperator(log),
		servicemesh.NewServiceMeshOperator(log),
		serverless.NewServerLessOperator(log),
		openshiftai.NewOpenShiftAIOperator(log),
		authorino.NewAuthorinoOperator(log),
		osc.NewOscOperator(log),
		nmstate.NewNmstateOperator(log),
		amdgpu.NewAMDGPUOperator(log),
		kmm.NewKMMOperator(log),
		nodehealthcheck.NewNodeHealthcheckOperator(log),
		selfnoderemediation.NewSelfNodeRemediationOperator(log),
		fenceagentsremediation.NewFenceAgentsRemediationOperator(log),
		nodemaintenance.NewNodeMaintenanceOperator(log),
		kubedescheduler.NewKubeDeschedulerOperator(log),
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
