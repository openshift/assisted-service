package handler

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
)

// V2ListSupportedOperators Retrieves the list of supported operators.
func (h *Handler) V2ListSupportedOperators(_ context.Context, _ restoperators.V2ListSupportedOperatorsParams) middleware.Responder {
	return restoperators.NewV2ListSupportedOperatorsOK().
		WithPayload(h.operatorsAPI.GetSupportedOperators())
}
