package events

import (
	"context"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/streaming"
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

func NewApi(handler eventsapi.Handler, log logrus.FieldLogger) *Api {
	return &Api{
		handler: handler,
		log:     log,
	}
}

func (a *Api) V2ListEvents(ctx context.Context, params events.V2ListEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	eventStream, err := a.handler.V2GetEventStream(ctx, params.ClusterID, params.HostID, params.InfraEnvID, params.Categories...)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	eventMapper := func(ctx context.Context, event *common.Event) (model *models.Event, err error) {
		model = &models.Event{
			Name:       event.Name,
			ClusterID:  event.ClusterID,
			HostID:     event.HostID,
			InfraEnvID: event.InfraEnvID,
			Severity:   event.Severity,
			EventTime:  event.EventTime,
			Message:    event.Message,
			Props:      event.Props,
		}
		return
	}
	modelStream := streaming.Map(eventStream, eventMapper)
	responder, err := streaming.NewResponder[*models.Event]().
		Source(modelStream).
		Flush(100).
		Context(ctx).
		Build()
	if err != nil {
		log.WithError(err).Errorf("failed to create responder")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return responder
}
