package handler

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
)

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
