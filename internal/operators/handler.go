package operators

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/restapi"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
	"github.com/sirupsen/logrus"
)

// handler implements REST API interface and deals with HTTP objects and transport data model.
type handler struct {
	// TODO: remove when other methods from this interface are implemented
	restapi.OperatorsAPI
	// operatorsAPI is responsible for executing the actual logic related to the operators
	operatorsAPI API
	log          logrus.FieldLogger
}

// NewHandler creates new handler
func NewHandler(operatorsAPI API, log logrus.FieldLogger) *handler {
	return &handler{operatorsAPI: operatorsAPI, log: log}
}

// ListOperatorProperties Lists properties for an operator name.
func (h *handler) ListOperatorProperties(_ context.Context, params restoperators.ListOperatorPropertiesParams) middleware.Responder {
	properties, err := h.operatorsAPI.GetOperatorProperties(params.OperatorName)
	if err != nil {
		h.log.Errorf("%s operator has not been found", params.OperatorName)
		return restoperators.NewListOperatorPropertiesNotFound()
	}

	return restoperators.NewListOperatorPropertiesOK().
		WithPayload(properties)
}

// ListSupportedOperators Retrieves the list of supported operators.
func (h *handler) ListSupportedOperators(_ context.Context, _ restoperators.ListSupportedOperatorsParams) middleware.Responder {
	return restoperators.NewListSupportedOperatorsOK().
		WithPayload(h.operatorsAPI.GetSupportedOperators())
}
