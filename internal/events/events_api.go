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
	ret := make(models.EventList, len(evs))
	for i, ev := range evs {
		ret[i] = &models.Event{
			ClusterID: ev.ClusterID,
			HostID:    ev.HostID,
			Severity:  ev.Severity,
			EventTime: ev.EventTime,
			Message:   ev.Message,
			Props:     ev.Props,
		}
	}
	return events.NewListEventsOK().WithPayload(ret)
}

func (a *Api) V2ListEvents(ctx context.Context, params events.V2ListEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	evs, err := a.handler.V2GetEvents(params.ClusterID, params.HostID, params.InfraEnvID, params.Categories...)
	if err != nil {
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	ret := make(models.EventList, len(evs))
	for i, ev := range evs {
		ret[i] = &models.Event{
			ClusterID:  ev.ClusterID,
			HostID:     ev.HostID,
			InfraEnvID: ev.InfraEnvID,
			Severity:   ev.Severity,
			EventTime:  ev.EventTime,
			Message:    ev.Message,
			Props:      ev.Props,
		}
	}
	return events.NewListEventsOK().WithPayload(ret)
}
