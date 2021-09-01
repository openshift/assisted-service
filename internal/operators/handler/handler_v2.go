package handler

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
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
