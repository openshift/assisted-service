package handler

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
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
func (h *Handler) V2ListBundles(_ context.Context, _ restoperators.V2ListBundlesParams) middleware.Responder {
	return restoperators.NewV2ListBundlesOK().WithPayload(h.operatorsAPI.ListBundles())
}

// V2GetBundle Retrieves the Bundle object for a specific bundleName.
func (h *Handler) V2GetBundle(ctx context.Context, params restoperators.V2GetBundleParams) middleware.Responder {
	log := logutil.FromContext(ctx, h.log)
	bundle, err := h.operatorsAPI.GetBundle(params.ID)
	if err != nil {
		log.Errorf("Failed to get operators for bundle %s: %v", params.ID, err)
		return common.GenerateErrorResponder(err)
	}
	return restoperators.NewV2GetBundleOK().WithPayload(bundle)
}
