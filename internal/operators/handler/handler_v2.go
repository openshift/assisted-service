package handler

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
)

// V2ListOfClusterOperators Lists operators to be monitored for a cluster.
func (h *Handler) V2ListOfClusterOperators(ctx context.Context, params restoperators.V2ListOfClusterOperatorsParams) middleware.Responder {
	operatorsList, err := h.GetMonitoredOperators(ctx, params.ClusterID, params.OperatorName, h.db)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return restoperators.NewV2ListOfClusterOperatorsOK().WithPayload(operatorsList)
}

// V2ListOperatorProperties Lists properties for an operator name.
func (h *Handler) V2ListOperatorProperties(ctx context.Context, params restoperators.V2ListOperatorPropertiesParams) middleware.Responder {
	log := logutil.FromContext(ctx, h.log)
	properties, err := h.operatorsAPI.GetOperatorProperties(params.OperatorName)
	if err != nil {
		log.Errorf("%s operator has not been found", params.OperatorName)
		return restoperators.NewV2ListOperatorPropertiesNotFound()
	}

	return restoperators.NewV2ListOperatorPropertiesOK().
		WithPayload(properties)
}

// V2ListSupportedOperators Retrieves the list of supported operators.
func (h *Handler) V2ListSupportedOperators(_ context.Context, _ restoperators.V2ListSupportedOperatorsParams) middleware.Responder {
	return restoperators.NewV2ListSupportedOperatorsOK().
		WithPayload(h.operatorsAPI.GetSupportedOperators())
}

// V2GetBundles Retrieves the list of supported bundles.
func (h *Handler) V2GetBundles(_ context.Context, _ restoperators.V2GetBundlesParams) middleware.Responder {
	return restoperators.NewV2GetBundlesOK().WithPayload(h.operatorsAPI.GetBundles())
}

// V2GetBundleOperators Retrieves the list of operators for a specific bundle.
func (h *Handler) V2GetBundleOperators(ctx context.Context, params restoperators.V2GetBundleOperatorsParams) middleware.Responder {
	log := logutil.FromContext(ctx, h.log)
	operators, err := h.operatorsAPI.GetOperatorsByBundle(models.Bundle(params.BundleName))
	if err != nil {
		log.Errorf("Failed to get operators for bundle %s: %v", params.BundleName, err)
		return common.GenerateErrorResponder(err)
	}
	return restoperators.NewV2GetBundleOperatorsOK().WithPayload(operators)
}
