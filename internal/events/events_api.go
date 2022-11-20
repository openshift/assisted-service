package events

import (
	"context"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/events"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ restapi.EventsAPI = &Api{}

type Api struct {
	handler eventsapi.Handler
	log     logrus.FieldLogger
}

func (a *Api) V2EventsSubscribe(ctx context.Context, params events.V2EventsSubscribeParams) middleware.Responder {
	return a.handler.V2EventsSubscribe(ctx, params)
}

func (a *Api) V2EventsSubscriptionDelete(ctx context.Context, params events.V2EventsSubscriptionDeleteParams) middleware.Responder {
	return a.handler.V2EventsSubscriptionDelete(ctx, params)
}

func (a *Api) V2EventsSubscriptionGet(ctx context.Context, params events.V2EventsSubscriptionGetParams) middleware.Responder {
	return a.handler.V2EventsSubscriptionGet(ctx, params)
}

func (a *Api) V2EventsSubscriptionList(ctx context.Context, params events.V2EventsSubscriptionListParams) middleware.Responder {
	return a.handler.V2EventsSubscriptionList(ctx, params)
}

func NewApi(handler eventsapi.Handler, log logrus.FieldLogger) *Api {
	return &Api{
		handler: handler,
		log:     log,
	}
}

func (a *Api) V2ListEvents(ctx context.Context, params events.V2ListEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	evs, err := a.handler.V2GetEvents(ctx, params.ClusterID, params.HostID, params.InfraEnvID, params.Categories...)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	ret := make(models.EventList, len(evs))
	for i, ev := range evs {
		ret[i] = &models.Event{
			Name:       ev.Name,
			ClusterID:  ev.ClusterID,
			HostID:     ev.HostID,
			InfraEnvID: ev.InfraEnvID,
			Severity:   ev.Severity,
			EventTime:  ev.EventTime,
			Message:    ev.Message,
			Props:      ev.Props,
		}
	}
	return events.NewV2ListEventsOK().WithPayload(ret)
}
