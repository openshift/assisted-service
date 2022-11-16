package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var DefaultEventCategories = []string{
	models.EventCategoryUser,
}

type Events struct {
	db    *gorm.DB
	log   logrus.FieldLogger
	authz auth.Authorizer
}

func New(db *gorm.DB, authz auth.Authorizer, log logrus.FieldLogger) eventsapi.Handler {
	return &Events{
		db:    db,
		log:   log,
		authz: authz,
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
	e.handleEventSubscription(&clusterID, event)
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
	e.handleEventSubscription(clusterID, event)
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

func (e *Events) NotifyInternalEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, msg string) {
	log := logutil.FromContext(ctx, e.log)
	log.Debugf("Notifying internal event %s, nothing to do", msg)
}

func (e *Events) V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	e.v2SaveEvent(ctx, clusterID, hostID, infraEnvID, name, models.EventCategoryMetrics, severity, msg, eventTime, requestID, props...)
}

func (e Events) queryEvents(ctx context.Context, selectedCategories []string, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID) ([]*common.Event, error) {

	WithIDs := func(db *gorm.DB) *gorm.DB {
		if clusterID != nil {
			db = db.Where("cluster_id = ?", clusterID.String())
		}
		if infraEnvID != nil {
			db = db.Where("infra_env_id = ?", infraEnvID.String())
		}
		if hostID != nil {
			db = db.Where("host_id = ?", hostID.String())
		}
		return db
	}

	allEvents := func() bool {
		return clusterID == nil && infraEnvID == nil && hostID == nil
	}

	clusterBoundEvents := func() bool {
		return clusterID != nil
	}

	nonBoundEvents := func() bool {
		return clusterID == nil && infraEnvID != nil
	}

	hostOnlyEvents := func() bool {
		return clusterID == nil && infraEnvID == nil && hostID != nil
	}

	//prepare the common parts of the query
	db := e.db.Order("event_time").Where("category IN (?)", selectedCategories)
	if e.authz != nil {
		db = e.authz.OwnedBy(ctx, db)
	}

	var result *gorm.DB

	//retrieveing all events can be done only by admins. This is done to restrict data
	//intensive queries by common users
	if allEvents() && e.authz.IsAdmin(ctx) {
		result = db
	}

	//for bound events that are searched with cluster id (whether on clusters, bound infra-env ,
	//host bound to a cluster or registered to a bound infra-env) check the access permission
	//relative to the cluster ownership
	if clusterBoundEvents() {
		result = db.Model(&common.Cluster{}).Select("events.*, clusters.user_name, clusters.org_id").
			Joins("INNER JOIN \"events\" ON events.cluster_id = clusters.id")
	}

	//for unbound events that are searched with infra-env id (whether events on hosts or the
	//infra-env level itself) check the access permission relative to the infra-env ownership
	if nonBoundEvents() {
		result = db.Model(&common.InfraEnv{}).Select("events.*, infra_envs.user_name, infra_envs.org_id").
			Joins("INNER JOIN events ON events.infra_env_id = infra_envs.id")
	}

	//for query made on the host only check the permission relative to it's infra-env. since
	//host table does not contain an org_id we can not perform a join on that table and has to go
	//through the infra-env table which is good because authorization is done on the infra-level
	if hostOnlyEvents() {
		result = db.Model(&common.Host{}).Select("events.*, infra_envs.user_name, infra_envs.org_id").
			Joins("INNER JOIN infra_envs ON hosts.infra_env_id = infra_envs.id").Joins("INNER JOIN events ON events.host_id = hosts.id")
	}

	if result == nil { //non supported option
		return make([]*common.Event, 0), nil
	}

	var events []*common.Event
	return events, WithIDs(result).Find(&events).Error
}

func (e Events) V2GetEvents(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	//initialize the selectedCategories either from the filter, if exists, or from the default values
	selectedCategories := make([]string, 0)
	if len(categories) > 0 {
		selectedCategories = categories[:]
	} else {
		selectedCategories = append(selectedCategories, DefaultEventCategories...)
	}

	return e.queryEvents(ctx, selectedCategories, clusterID, hostID, infraEnvID)
}

func (e Events) handleEventSubscription(clusterID *strfmt.UUID, event common.Event) {
	var eventSubscriptionList models.EventSubscriptionList
	if clusterID == nil {
		return
	}
	e.log.Infof("Searching event subscriptions with cluster_id = %s, event_name: %s", clusterID.String(), event.Name)
	err := e.db.Find(&eventSubscriptionList, "cluster_id = ? and event_name = ?", clusterID.String(), event.Name).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		e.log.WithError(err).Errorf("Failed to get event subscription for cluster_id: %s, event_name: %s", clusterID, event.Name)
	}
	for i, _ := range eventSubscriptionList {
		e.callEventSubscriptionURL(*eventSubscriptionList[i], event)
	}

}

func (e Events) callEventSubscriptionURL(eventSubscription models.EventSubscription, event common.Event) {
	e.log.Infof("Calling event callback for cluster_id: %s, event_name: %s, url: %s",
		*eventSubscription.ClusterID, *eventSubscription.EventName, *eventSubscription.URL)

	status := "Failed"
	defer func() {
		e.db.Model(&models.EventSubscription{}).Where("id = ?", eventSubscription.ID).Update("status", &status)
	}()

	json_data, err := json.Marshal(event)
	if err != nil {
		e.log.WithError(err).Error("Failed to marshal event")
	}

	resp, err := http.Post(*eventSubscription.URL, "application/json",
		bytes.NewBuffer(json_data))

	if err != nil {
		e.log.WithError(err).Errorf("event subscription callback failed")
		status = err.Error()
	} else {
		status = resp.Status
		e.log.Infof("event subscription callback status: %s", status)
	}
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
