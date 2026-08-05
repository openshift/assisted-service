package events

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/events"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ restapi.EventsAPI = &Api{}

type Api struct {
	handler    eventsapi.Handler
	log        logrus.FieldLogger
	db         *gorm.DB
	metricsAPI metrics.API
}

func NewApi(handler eventsapi.Handler, log logrus.FieldLogger, db *gorm.DB, metricsAPI metrics.API) *Api {
	return &Api{
		handler:    handler,
		log:        log,
		db:         db,
		metricsAPI: metricsAPI,
	}
}

func parseProps(props string) ([]interface{}, error) {
	if props == "" {
		return nil, nil
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(props), &parsed); err != nil {
		return nil, err
	}
	switch v := parsed.(type) {
	case []interface{}:
		return v, nil
	default:
		return []interface{}{v}, nil
	}
}

func (a *Api) V2TriggerEvent(ctx context.Context, params events.V2TriggerEventParams) middleware.Responder {
	var (
		props []interface{}
		err   error
	)
	if props, err = parseProps(params.TriggerEventParams.Props); err != nil {
		wrapped := errors.Wrapf(err, "failed to unmarshal event properties: '%s'", params.TriggerEventParams.Props)
		a.log.WithError(wrapped).Error("V2AddEvent")
	}
	switch params.TriggerEventParams.Category {
	case models.EventCategoryUser, "":
		a.handler.V2AddEvent(ctx,
			params.TriggerEventParams.ClusterID,
			params.TriggerEventParams.HostID,
			params.TriggerEventParams.InfraEnvID,
			params.TriggerEventParams.Name,
			swag.StringValue(params.TriggerEventParams.Severity),
			swag.StringValue(params.TriggerEventParams.Message),
			time.Now(), props...)
	case models.EventCategoryMetrics:
		a.handler.V2AddMetricsEvent(ctx,
			params.TriggerEventParams.ClusterID,
			params.TriggerEventParams.HostID,
			params.TriggerEventParams.InfraEnvID,
			params.TriggerEventParams.Name,
			swag.StringValue(params.TriggerEventParams.Severity),
			swag.StringValue(params.TriggerEventParams.Message),
			time.Now(), props...)
	default:
		err := common.NewApiError(http.StatusBadRequest, errors.Errorf("unexpected category %s", params.TriggerEventParams.Category))
		a.log.WithError(err).Error("V2AddEvent")
		return err
	}
	return events.NewV2TriggerEventCreated()
}

func (a *Api) V2ListEvents(ctx context.Context, params events.V2ListEventsParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	// Merge deprecated HostID into HostIds before scope enforcement
	if params.HostID != nil {
		params.HostIds = append(params.HostIds, *params.HostID)
	}

	if err := a.enforceEventsScopeFromPayload(ctx, params); err != nil {
		return err
	}

	V2getEventsParams := common.V2GetEventsParams{
		ClusterID:    params.ClusterID,
		HostIds:      params.HostIds,
		InfraEnvID:   params.InfraEnvID,
		Limit:        params.Limit,
		Offset:       params.Offset,
		Order:        params.Order,
		Severities:   params.Severities,
		Message:      params.Message,
		DeletedHosts: params.DeletedHosts,
		ClusterLevel: params.ClusterLevel,
		Categories:   params.Categories,
	}

	response, err := a.handler.V2GetEvents(ctx, &V2getEventsParams)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get events")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	evs := response.GetEvents()
	eventSeverityCount := response.GetEventSeverityCount()
	eventCount := response.GetEventCount()

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

	return events.NewV2ListEventsOK().
		WithSeverityCountInfo((*eventSeverityCount)[models.EventSeverityInfo]).
		WithSeverityCountWarning((*eventSeverityCount)[models.EventSeverityWarning]).
		WithSeverityCountError((*eventSeverityCount)[models.EventSeverityError]).
		WithSeverityCountCritical((*eventSeverityCount)[models.EventSeverityCritical]).
		WithEventCount(*eventCount).
		WithPayload(ret)
}

// enforceEventsScopeFromPayload verifies that a scoped urlAuth token may only
// access events for the resource it was issued for.  Unscoped tokens (e.g.
// agentAuth or admin tokens with no ResourceType) are allowed through.
func (a *Api) enforceEventsScopeFromPayload(ctx context.Context, params events.V2ListEventsParams) middleware.Responder {
	payload := ocm.PayloadFromContext(ctx)
	if payload.ResourceType == "" {
		return nil
	}

	deny := func(format string, args ...interface{}) middleware.Responder {
		a.log.WithFields(logrus.Fields{
			"token_resource_type": payload.ResourceType,
			"token_resource_id":   payload.ResourceID,
		}).Warnf(format, args...)
		a.recordScopeCheck(payload.ResourceType, "denied")
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	allow := func() middleware.Responder {
		a.recordScopeCheck(payload.ResourceType, "allowed")
		return nil
	}

	switch payload.ResourceType {
	case gencrypto.InfraEnvKey:
		if params.InfraEnvID != nil {
			if payload.ResourceID != params.InfraEnvID.String() {
				return deny("Events scope mismatch: query infra_env_id=%s", params.InfraEnvID)
			}
			return allow()
		}
		if len(params.HostIds) > 0 {
			return a.verifyHostsBelongToInfraEnv(payload.ResourceID, params.HostIds)
		}
		return deny("Events scope rejected: infra_env_id token requires infra_env_id or host_ids filter")

	case gencrypto.ClusterKey:
		if params.ClusterID != nil {
			if payload.ResourceID != params.ClusterID.String() {
				return deny("Events scope mismatch: query cluster_id=%s", params.ClusterID)
			}
			return allow()
		}
		return deny("Events scope rejected: cluster_id token requires cluster_id filter")

	default:
		a.log.WithFields(logrus.Fields{
			"token_resource_type": payload.ResourceType,
			"token_resource_id":   payload.ResourceID,
		}).Error("Events scope check: unrecognized resource type in urlAuth token")
		a.recordScopeCheck(payload.ResourceType, "denied")
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}
}

func (a *Api) verifyHostsBelongToInfraEnv(infraEnvID string, hostIds []strfmt.UUID) middleware.Responder {
	resourceType := gencrypto.InfraEnvKey
	hostIdStrings := make([]string, len(hostIds))
	for i, id := range hostIds {
		hostIdStrings[i] = id.String()
	}
	var count int64
	err := a.db.Model(&common.Host{}).
		Where("id IN ? AND infra_env_id = ?", hostIdStrings, infraEnvID).
		Count(&count).Error
	if err != nil {
		a.log.WithFields(logrus.Fields{
			"host_ids":     hostIdStrings,
			"infra_env_id": infraEnvID,
		}).WithError(err).Error("Failed to verify hosts belong to infra_env for events scope check")
		a.recordScopeCheck(resourceType, "denied")
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("failed to verify host ownership"))
	}
	if count != int64(len(hostIds)) {
		a.log.WithFields(logrus.Fields{
			"host_ids":     hostIdStrings,
			"infra_env_id": infraEnvID,
			"matched":      count,
			"requested":    len(hostIds),
		}).Warn("Events scope rejected: not all hosts belong to token's infra_env")
		a.recordScopeCheck(resourceType, "denied")
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}
	a.recordScopeCheck(resourceType, "allowed")
	return nil
}

func (a *Api) recordScopeCheck(resourceType gencrypto.LocalJWTKeyType, result string) {
	if a.metricsAPI != nil {
		a.metricsAPI.URLAuthScopeCheck(string(resourceType), "query", result)
	}
}
