package events

import (
	"context"

	"github.com/filanov/bm-inventory/models"

	"github.com/filanov/bm-inventory/internal/common"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/bm-inventory/restapi"
	"github.com/filanov/bm-inventory/restapi/operations/events"
	"github.com/go-openapi/runtime/middleware"
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
	evs, err := a.handler.GetEvents(params.EntityID.String())
	if err != nil {
		log.Errorf("failed to get events for id %s ", params.EntityID.String())
		return events.NewListEventsInternalServerError().
			WithPayload(common.GenerateInternalFromError(err))
	}
	ret := make(models.EventList, len(evs))
	for i, ev := range evs {
		ret[i] = &models.Event{
			EntityID:  ev.EntityID,
			EventTime: ev.EventTime,
			Message:   ev.Message,
		}
	}
	return events.NewListEventsOK().WithPayload(ret)

}
