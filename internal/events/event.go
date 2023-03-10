package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	commonevents "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/stream"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var DefaultEventCategories = []string{
	models.EventCategoryUser,
}

type Events struct {
	db     *gorm.DB
	log    logrus.FieldLogger
	authz  auth.Authorizer
	stream stream.Notifier
}

func New(db *gorm.DB, authz auth.Authorizer, stream stream.Notifier, log logrus.FieldLogger) eventsapi.Handler {
	return &Events{
		db:     db,
		log:    log,
		authz:  authz,
		stream: stream,
	}
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
			err = e.stream.Notify(ctx, &event)
			if err != nil {
				log.WithError(err).Warning("failed to notify event")
			}
		}
	}()

	// Check and if the event exceeds the limits:
	limitExceeded, limitReason, dberr := e.exceedsLimits(ctx, tx, &event)
	if dberr != nil {
		return
	}
	if limitExceeded {
		e.reportDiscarded(ctx, &event, limitReason)
		return
	}

	// Create the new event:
	dberr = tx.Create(&event).Error
}

// exceedsLimit checks if there are already events that are too close to the given one. It returns
// a boolean flag with the result and a set of log fields that explain why the limit was exceeded.
func (e *Events) exceedsLimits(ctx context.Context, tx *gorm.DB, event *common.Event) (result bool,
	reason logrus.Fields, err error) {
	// Do nothing if there is no configured limit:
	limit, ok := eventLimits[event.Name]
	if !ok {
		return
	}

	// Prepare the query to find the events whose distance to this one is less than the limit:
	query := tx.Table("events").
		Select("count(*)").
		Where("name = ?", event.Name).
		Where("event_time > ?", time.Now().Add(-limit))
	if event.ClusterID != nil {
		query = query.Where("cluster_id = ?", event.ClusterID.String())
	}
	if event.HostID != nil {
		query = query.Where("host_id = ?", event.HostID.String())
	}
	if event.InfraEnvID != nil {
		query = query.Where("infra_env_id = ?", event.InfraEnvID.String())
	}

	// Run the query:
	var count int
	err = query.Scan(&count).Error
	if err != nil {
		return
	}
	if count > 0 {
		result = true
		reason = logrus.Fields{
			"limit": limit,
			"count": count,
		}
	}
	return
}

// reportDiscarded writes to the log a message indicating that the given event has been discarded.
// The log message will include the details of the event and the reason.
func (e *Events) reportDiscarded(ctx context.Context, event *common.Event,
	reason logrus.Fields) {
	log := logutil.FromContext(ctx, e.log)
	fields := logrus.Fields{
		"name":       event.Name,
		"category":   event.Category,
		"request_id": event.RequestID.String(),
		"props":      event.Props,
	}
	if event.EventTime != nil {
		fields["time"] = event.EventTime.String()
	}
	if event.ClusterID != nil {
		fields["cluster_id"] = event.ClusterID.String()
	}
	if event.HostID != nil {
		fields["host_id"] = event.HostID.String()
	}
	if event.InfraEnvID != nil {
		fields["infra_env_id"] = event.InfraEnvID.String()
	}
	if event.Severity != nil {
		fields["severity"] = *event.Severity
	}
	if event.Message != nil {
		fields["message"] = *event.Message
	}
	for name, value := range reason {
		fields[name] = value
	}
	log.WithFields(fields).Warn("Event will be discarded")
}

// eventLimits contains the minimum distance in time between events. The key of the map is the
// event name and the value is the distance.
var eventLimits = map[string]time.Duration{
	commonevents.UpgradeAgentFailedEventName:   time.Hour,
	commonevents.UpgradeAgentFinishedEventName: time.Hour,
	commonevents.UpgradeAgentStartedEventName:  time.Hour,
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
