package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/identity"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var DefaultEventCategories = []string{
	models.EventCategoryUser,
}

type Events struct {
	db  *gorm.DB
	log logrus.FieldLogger
}

func New(db *gorm.DB, log logrus.FieldLogger) eventsapi.Handler {
	return &Events{
		db:  db,
		log: log,
	}
}

func (e *Events) saveEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, category string, severity string, message string, t time.Time, requestID string, props ...interface{}) error {
	log := logutil.FromContext(ctx, e.log)
	tt := strfmt.DateTime(t)
	uid := clusterID
	rid := strfmt.UUID(requestID)

	additionalProps, err := toProps(props...)
	if err != nil {
		log.WithError(err).Error("failed to parse event's properties field")
	}
	event := common.Event{
		Event: models.Event{
			EventTime: &tt,
			ClusterID: &uid,
			Severity:  &severity,
			Category:  category,
			Message:   &message,
			RequestID: rid,
			Props:     additionalProps,
		},
	}
	if hostID != nil {
		event.HostID = hostID
	}

	//each event is saved in its own embedded transaction
	var dberr error
	tx := e.db.Begin()
	defer func() {
		if dberr != nil {
			log.Warnf("Rolling back transaction on event=%s", message)
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()
	if dberr = tx.Create(&event).Error; err != nil {
		log.WithError(err).Error("Error adding event")
	}
	return dberr
}

func (e *Events) v2SaveEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, category string, severity string, message string, t time.Time, requestID string, props ...interface{}) {
	log := logutil.FromContext(ctx, e.log)
	tt := strfmt.DateTime(t)
	rid := strfmt.UUID(requestID)
	errMsg := make([]string, 0)
	additionalProps, err := toProps(props...)
	if err != nil {
		log.WithError(err).Error("failed to parse event's properties field")
	}
	event := common.Event{
		Event: models.Event{
			EventTime: &tt,
			Name:      name,
			Severity:  &severity,
			Category:  category,
			Message:   &message,
			RequestID: rid,
			Props:     additionalProps,
		},
	}
	if clusterID != nil {
		event.ClusterID = clusterID
		errMsg = append(errMsg, fmt.Sprintf("cluster_id = %s", clusterID.String()))
	}

	if hostID != nil {
		event.HostID = hostID
		errMsg = append(errMsg, fmt.Sprintf("host_id = %s", hostID.String()))
	}

	if infraEnvID != nil {
		event.InfraEnvID = infraEnvID
		errMsg = append(errMsg, fmt.Sprintf("infra_env_id = %s", infraEnvID.String()))
	}

	//each event is saved in its own embedded transaction
	var dberr error
	tx := e.db.Begin()
	defer func() {
		if dberr != nil {
			log.WithError(err).Errorf("failed to add event. Rolling back transaction on event=%s resources: %s",
				message, strings.Join(errMsg, " "))
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()
	dberr = tx.Create(&event).Error
}

func (e *Events) SendClusterEvent(ctx context.Context, event eventsapi.ClusterEvent) {
	e.SendClusterEventAtTime(ctx, event, time.Now())
}

func (e *Events) SendClusterEventAtTime(ctx context.Context, event eventsapi.ClusterEvent, eventTime time.Time) {
	cID := event.GetClusterId()
	e.V2AddEvent(ctx, &cID, nil, nil, event.GetName(), event.GetSeverity(), event.FormatMessage(), eventTime)
}

func (e *Events) SendHostEvent(ctx context.Context, event eventsapi.HostEvent) {
	e.SendHostEventAtTime(ctx, event, time.Now())
}

func (e *Events) SendHostEventAtTime(ctx context.Context, event eventsapi.HostEvent, eventTime time.Time) {
	hostID := event.GetHostId()
	infraEnvID := event.GetInfraEnvId()
	e.V2AddEvent(ctx, event.GetClusterId(), &hostID, &infraEnvID, event.GetName(), event.GetSeverity(), event.FormatMessage(), eventTime)
}

func (e *Events) SendInfraEnvEvent(ctx context.Context, event eventsapi.InfraEnvEvent) {
	e.SendInfraEnvEventAtTime(ctx, event, time.Now())
}

func (e *Events) SendInfraEnvEventAtTime(ctx context.Context, event eventsapi.InfraEnvEvent, eventTime time.Time) {
	infraEnvID := event.GetInfraEnvId()
	e.V2AddEvent(ctx, event.GetClusterId(), nil, &infraEnvID, event.GetName(), event.GetSeverity(), event.FormatMessage(), eventTime)
}

func (e *Events) AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	_ = e.saveEvent(ctx, clusterID, hostID, models.EventCategoryUser, severity, msg, eventTime, requestID, props...)
}

func (e *Events) AddMetricsEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	_ = e.saveEvent(ctx, clusterID, hostID, models.EventCategoryMetrics, severity, msg, eventTime, requestID, props...)
}

func (e *Events) V2AddEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	e.v2SaveEvent(ctx, clusterID, hostID, infraEnvID, name, models.EventCategoryUser, severity, msg, eventTime, requestID, props...)
}

func (e *Events) V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	e.v2SaveEvent(ctx, clusterID, hostID, infraEnvID, name, models.EventCategoryMetrics, severity, msg, eventTime, requestID, props...)
}

func (e Events) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	var err error
	var events []*common.Event

	//initialize the selectedCategories either from the filter, if exists, or from the default values
	selectedCategories := make([]string, 0)
	if len(categories) > 0 {
		selectedCategories = categories[:]
	} else {
		selectedCategories = append(selectedCategories, DefaultEventCategories...)
	}

	if hostID == nil {
		err = e.clusterEventsQuery(&events, selectedCategories, clusterID).Error
	} else {
		err = e.hostEventsQuery(&events, selectedCategories, clusterID, hostID).Error
	}
	return events, err
}

func (e Events) clusterEventsQuery(events *[]*common.Event, selectedCategories []string, clusterID strfmt.UUID) *gorm.DB {
	return e.db.Where("category IN (?)", selectedCategories).Order("event_time").
		Find(events, "cluster_id = ?", clusterID.String())
}

func (e Events) hostEventsQuery(events *[]*common.Event, selectedCategories []string, clusterID strfmt.UUID, hostID *strfmt.UUID) *gorm.DB {
	return e.db.Where("category IN (?)", selectedCategories).Order("event_time").
		Find(events, "cluster_id = ? AND host_id = ?", clusterID.String(), (*hostID).String())
}

func (e Events) eventsQuery(ctx context.Context, events *[]*common.Event, selectedCategories []string, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID) *gorm.DB {
	whereCondition := make([]string, 0)
	user := ocm.UserNameFromContext(ctx)

	if clusterID != nil {
		cluster, err := common.GetClusterFromDB(e.db, *clusterID, common.UseEagerLoading)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "cluster %s not found.", clusterID.String())}
			}
			return &gorm.DB{Error: err}
		}
		if user != "" && !identity.IsAdmin(ctx) { // A case where there is a non-admin user in context, the service should only present resources matching to it.
			if cluster.UserName == user {
				whereCondition = append(whereCondition, fmt.Sprintf("cluster_id = '%s'", clusterID.String()))
			} else {
				e.log.Errorf(
					"user %s is not authorized to query events for cluster %s: cluster owned by another user.",
					user, clusterID.String())
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "cluster %s not found.", clusterID.String())}
			}
		} else { // A case where there is no user in context, simply filter by resource id
			whereCondition = append(whereCondition, fmt.Sprintf("cluster_id = '%s'", clusterID.String()))
		}
	}
	if hostID != nil {
		host, err := common.GetHostFromDBbyHostId(e.db, *hostID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "host %s not found.", hostID.String())}
			}
			return &gorm.DB{Error: err}
		}
		if user != "" && !identity.IsAdmin(ctx) { // A case where there is a non-admin user in context, the service should only present resources matching to it.
			if host != nil && host.UserName == user {
				whereCondition = append(whereCondition, fmt.Sprintf("host_id = '%s'", hostID.String()))
			} else {
				e.log.Errorf(
					"user %s is not authorized to query events for host %s: host owned by another user.",
					user, hostID.String())
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "host %s not found.", hostID.String())}
			}
		} else { // A case where there is no user in context, simply filter by resource id
			whereCondition = append(whereCondition, fmt.Sprintf("host_id = '%s'", hostID.String()))
		}
	}
	if infraEnvID != nil {
		infraEnv, err := common.GetInfraEnvFromDB(e.db, *infraEnvID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "infra_env %s not found.", infraEnvID.String())}
			}
			return &gorm.DB{Error: err}
		}
		if user != "" && !identity.IsAdmin(ctx) { // A case where there is a non-admin user in context, the service should only present resources matching to it.
			if infraEnv.UserName == user {
				whereCondition = append(whereCondition, fmt.Sprintf("infra_env_id = '%s'", infraEnvID.String()))
			} else {
				e.log.Errorf(
					"user %s is not authorized to query events for infra_env %s: infra_env owned by another user.",
					user, infraEnvID.String())
				return &gorm.DB{Error: errors.Wrapf(gorm.ErrRecordNotFound, "infra_env %s not found.", infraEnvID.String())}
			}
		} else { // A case where there is no user in context, simply filter by resource id
			whereCondition = append(whereCondition, fmt.Sprintf("infra_env_id = '%s'", infraEnvID.String()))
		}
	}
	if len(whereCondition) == 0 {
		queryErr := errors.Wrap(gorm.ErrInvalidTransaction, "events query with no filters is not allowed.")
		e.log.Error(queryErr)
		return &gorm.DB{Error: queryErr}
	}
	return e.db.Where("category IN (?)", selectedCategories).Order("event_time").Find(&events, strings.Join(whereCondition, " AND "))
}

func (e Events) V2GetEvents(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	var err error
	var events []*common.Event

	//initialize the selectedCategories either from the filter, if exists, or from the default values
	selectedCategories := make([]string, 0)
	if len(categories) > 0 {
		selectedCategories = categories[:]
	} else {
		selectedCategories = append(selectedCategories, DefaultEventCategories...)
	}

	err = e.eventsQuery(ctx, &events, selectedCategories, clusterID, hostID, infraEnvID).Error
	return events, err
}

func toProps(attrs ...interface{}) (result string, err error) {
	props := make(map[string]interface{})
	length := len(attrs)

	if length == 1 {
		if attr, ok := attrs[0].(map[string]interface{}); ok {
			props = attr
		}
	}

	if length > 1 && length%2 == 0 {
		for i := 0; i < length; i += 2 {
			props[attrs[i].(string)] = attrs[i+1]
		}
	}

	if len(props) > 0 {
		var b []byte
		if b, err = json.Marshal(props); err == nil {
			return string(b), nil
		}
	}

	return "", err
}
