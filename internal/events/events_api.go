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

	evs, err := a.handler.GetEvents(params.ClusterID, params.HostID)
	if err != nil {
		if params.HostID != nil {
			log.Errorf("failed to get events for cluster %s host %s", params.ClusterID.String(), params.HostID.String())
		} else {
			log.Errorf("failed to get events for cluster %s", params.ClusterID.String())
		}
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
		}
	}
	return events.NewListEventsOK().WithPayload(ret)

}
