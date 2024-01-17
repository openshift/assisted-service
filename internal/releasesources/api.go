package releasesources

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/configuration"
	"github.com/sirupsen/logrus"
)

type releaseSourcesAPIHandler struct {
	log logrus.FieldLogger
	releaseSourcesHandler
}

func NewAPIHandler(log logrus.FieldLogger, releaseSourcesHandler releaseSourcesHandler) restapi.ConfigurationAPI {
	return &releaseSourcesAPIHandler{log: log, releaseSourcesHandler: releaseSourcesHandler}
}

var _ restapi.ConfigurationAPI = (*releaseSourcesAPIHandler)(nil)

func (r *releaseSourcesAPIHandler) V2ListReleaseSources(ctx context.Context, params operations.V2ListReleaseSourcesParams) middleware.Responder {
	return operations.NewV2ListReleaseSourcesOK().WithPayload(r.releaseSources)
}
