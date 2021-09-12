package events

import (
	"context"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/events"
	"github.com/sirupsen/logrus"
)

var _ restapi.EventsAPI = &Api{}

type Api struct {
	handler Handler
	log     logrus.FieldLogger
}

func NewApi(handler Handler, log logrus.FieldLogger) *Api {
	return &Api{
		handler: handler,
		log:     log,
	}
}

func (a *Api) ListEvents(ctx context.Context, params events.ListEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	evs, err := a.handler.GetEvents(params.ClusterID, params.HostID, params.Categories...)
	if err != nil {
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	ret := toModelEvents(evs)
	return events.NewListEventsOK().WithPayload(ret)
}

func (a *Api) ListInfraEnvEvents(ctx context.Context, params events.ListInfraEnvEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	evs, err := a.handler.GetInfraEnvEvents(params.InfraEnvID, params.HostID, params.Categories...)
	if err != nil {
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	ret := toModelEvents(evs)
	return events.NewListEventsOK().WithPayload(ret)
}

func toModelEvents(evs []*common.Event) models.EventList {
	ret := make(models.EventList, len(evs))
	for i, ev := range evs {
		ret[i] = &models.Event{
			ClusterID:  ev.ClusterID,
			InfraEnvID: ev.InfraEnvID,
			HostID:     ev.HostID,
			Severity:   ev.Severity,
			EventTime:  ev.EventTime,
			Message:    ev.Message,
			Props:      ev.Props,
		}
	}
	return ret
}
