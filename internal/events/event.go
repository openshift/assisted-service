package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
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
	"gorm.io/gorm/clause"
)

var DefaultEventCategories = []string{
	models.EventCategoryUser,
}

var defaultEventLimit int64 = 5000

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
	err = e.db.Transaction(func(tx *gorm.DB) error {
		// Check and if the event exceeds the limits:
		limitExceeded, limitReason, dberr := e.exceedsLimits(ctx, tx, &event)
		if dberr != nil {
			return dberr

		}
		if limitExceeded {
			e.reportDiscarded(ctx, &event, limitReason)
			return fmt.Errorf("v2SaveEvent limit exceeded")
		}

		// Create the new event:
		return tx.Create(&event).Error
	})
	if err != nil {
		log.WithError(err).Errorf("failed to add event. Rolling back transaction on event=%s resources: %s",
			message, strings.Join(errMsg, " "))
	}

	err = e.stream.Notify(ctx, &event)
	if err != nil {
		log.WithError(err).Warning("failed to notify event")
	}
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

// If specific hosts, deleted hosts or cluster level events were specified,
// we want to filter the rest of the events
func buildDisjunctiveQuery(tx *gorm.DB, hostIds []strfmt.UUID, deletedHosts, clusterLevel *bool) *gorm.DB {
	// if none of above selected, we don't filter
	if !swag.BoolValue(deletedHosts) && !swag.BoolValue(clusterLevel) && hostIds == nil {
		return tx.Where("TRUE")
	}

	// techical 'Where' before 'ORs'
	tx = tx.Where("FALSE")

	if swag.BoolValue(deletedHosts) {
		tx.Or("hosts.deleted_at IS NOT NULL")
	}

	if swag.BoolValue(clusterLevel) {
		tx = tx.Or("events.host_id IS NULL")
	}

	if hostIds != nil {
		tx = tx.Or("events.host_id IN (?)", hostsUUIDsToStrings(hostIds))
	}

	return tx
}

func filterEvents(tx *gorm.DB, clusterID *strfmt.UUID, hostIds []strfmt.UUID, infraEnvID *strfmt.UUID, severities []string, message *string, deletedHosts, clusterLevel *bool, cleanQuery *gorm.DB) *gorm.DB {
	if clusterID != nil {

		tx = tx.Where("events.cluster_id = ?", clusterID.String())

		// filter by event message
		if message != nil {
			tx = tx.Where("LOWER(events.message) LIKE ?", fmt.Sprintf("%%%s%%", strings.ToLower(escapePlaceHolders(*message))))
		}

		tx = tx.Where(buildDisjunctiveQuery(cleanQuery, hostIds, deletedHosts, clusterLevel))

		return tx
	}

	if hostIds != nil {
		return tx.Where("events.host_id IN (?)", hostsUUIDsToStrings(hostIds))
	}

	if infraEnvID != nil {
		return tx.Where("events.infra_env_id = ?", infraEnvID.String())
	}

	return tx
}

func hostsUUIDsToStrings(hostIDs []strfmt.UUID) []string {
	result := []string{}
	for _, hostID := range hostIDs {
		result = append(result, hostID.String())
	}
	return result
}

func countEventsBySeverity(db *gorm.DB, clusterID *strfmt.UUID) (*common.EventSeverityCount, error) {
	var (
		total    int
		severity string
		rows     *sql.Rows
		err      error
	)

	if clusterID == nil {
		return &common.EventSeverityCount{}, nil
	}

	rows, err = db.Where("events.cluster_id = ?", clusterID.String()).Select("COUNT(events.severity), events.severity").Group("events.severity").Rows()
	if err != nil {
		return nil, err
	}

	eventSeverityCount := common.EventSeverityCount{}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&total, &severity)
		if err != nil {
			return nil, err
		}
		eventSeverityCount[severity] = int64(total)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}
	return &eventSeverityCount, nil
}

func (e Events) prepareEventsTable(ctx context.Context, tx *gorm.DB, clusterID *strfmt.UUID, hostIds []strfmt.UUID, infraEnvID *strfmt.UUID, severity []string, message *string, deletedHosts *bool) *gorm.DB {
	allEvents := func() bool {
		return clusterID == nil && infraEnvID == nil && hostIds == nil
	}

	clusterBoundEvents := func() bool {
		return clusterID != nil
	}

	nonBoundEvents := func() bool {
		return clusterID == nil && infraEnvID != nil
	}

	hostOnlyEvents := func() bool {
		return clusterID == nil && infraEnvID == nil && hostIds != nil
	}

	//retrieveing all events can be done only by admins. This is done to restrict data
	//intensive queries by common users
	if allEvents() && e.authz.IsAdmin(ctx) {
		return tx
	}

	//for bound events that are searched with cluster id (whether on clusters, bound infra-env ,
	//host bound to a cluster or registered to a bound infra-env) check the access permission
	//relative to the cluster ownership
	if clusterBoundEvents() {
		tx = tx.Model(&common.Event{}).Select("events.*, clusters.user_name, clusters.org_id").
			Joins("INNER JOIN clusters ON clusters.id = events.cluster_id")

		// if deleted hosts flag is true, we need to add 'deleted_at' to know whether events are related to a deleted host
		if swag.BoolValue(deletedHosts) {
			tx = tx.Select("events.*, clusters.user_name, clusters.org_id, hosts.deleted_at").
				Joins("LEFT JOIN hosts ON hosts.id = events.host_id")
		}
		return tx
	}

	//for unbound events that are searched with infra-env id (whether events on hosts or the
	//infra-env level itself) check the access permission relative to the infra-env ownership
	if nonBoundEvents() {
		return tx.Model(&common.Event{}).Select("events.*, infra_envs.user_name, infra_envs.org_id").
			Joins("INNER JOIN infra_envs ON infra_envs.id = events.infra_env_id")
	}

	// Events must be linked to the infra_envs table and then to the hosts table
	// The hosts table does not hold an org_id, so permissions related fields must be supplied by the infra_env
	if hostOnlyEvents() {
		return tx.Model(&common.Event{}).Select("events.*, infra_envs.user_name, infra_envs.org_id").
			Joins("INNER JOIN infra_envs ON infra_envs.id = events.infra_env_id").
			Joins("INNER JOIN hosts ON hosts.id = events.host_id"). // This join is here to ensure that only events for a host that exists are fetched
			Where("hosts.deleted_at IS NULL")                       // Only interested in active hosts
	}

	//non supported option
	return nil
}

func preparePaginationParams(limit, offset *int64) (*int64, *int64) {
	if limit == nil {
		// If limit is not provided, we set it a default (currently 5000).
		limit = swag.Int64(defaultEventLimit)
	} else if *limit < -1 {
		// If limit not valid (smaller than -1), we set it -1 (no limit).
		limit = common.UnlimitedEvents
	}

	// if offset not specified or is negative, we return the first page.
	if offset == nil || *offset < 0 {
		offset = common.NoOffsetEvents
	}

	return limit, offset
}

func isDescending(order *string) (*bool, error) {
	if order == nil || *order == "ascending" {
		return swag.Bool(false), nil
	}
	if *order == "descending" {
		return swag.Bool(true), nil
	}
	return nil, errors.New("incompatible order parameter")
}

func escapePlaceHolders(message string) string {
	message = strings.ReplaceAll(message, "\\", "\\\\")
	message = strings.ReplaceAll(message, "_", "\\_")
	message = strings.ReplaceAll(message, "%", "\\%")
	return message
}

func (e Events) queryEvents(ctx context.Context, params *common.V2GetEventsParams) ([]*common.Event, *common.EventSeverityCount, *int64, error) {

	cleanQuery := e.db.Session(&gorm.Session{})
	tx := e.db.Where("category IN (?)", params.Categories)

	events := []*common.Event{}

	tx = e.prepareEventsTable(ctx, tx, params.ClusterID, params.HostIds, params.InfraEnvID, params.Severities, params.Message, params.DeletedHosts)
	if tx == nil {
		return make([]*common.Event, 0), &common.EventSeverityCount{}, swag.Int64(0), nil
	}

	tx = filterEvents(tx, params.ClusterID, params.HostIds, params.InfraEnvID, params.Severities, params.Message, params.DeletedHosts, params.ClusterLevel, cleanQuery)

	eventSeverityCount, err := countEventsBySeverity(tx.Session(&gorm.Session{}), params.ClusterID)
	if err != nil {
		return nil, nil, nil, err
	}

	/*
		we filter the severity after we count event severities as we want to count event severities
		with respect to to all filtering params but severities across all all possible pages
	*/
	if params.Severities != nil {
		tx = tx.Where("events.severity IN (?)", params.Severities)
	}

	var eventCount int64
	tx.Session(&gorm.Session{}).Count(&eventCount)

	isDescending, err := isDescending(params.Order)
	if err != nil {
		return nil, nil, nil, err
	}

	tx.Order(clause.OrderByColumn{
		Column: clause.Column{Name: "event_time"},
		Desc:   *isDescending,
	})

	params.Limit, params.Offset = preparePaginationParams(params.Limit, params.Offset)
	if *params.Limit == 0 {
		return make([]*common.Event, 0), eventSeverityCount, &eventCount, nil
	}

	// if we need to apply authorization check then repackage tx as a subquery
	// this is to ensure that user_name and org_id are unambiguous
	if e.authz != nil {
		tx = e.authz.OwnedBy(ctx, cleanQuery.Table("(?) as s", tx))
	}
	err = tx.Offset(int(*params.Offset)).Limit(int(*params.Limit)).Find(&events).Error
	if err != nil {
		return nil, nil, nil, err
	}

	return events, eventSeverityCount, &eventCount, nil
}

func (e Events) V2GetEvents(ctx context.Context, params *common.V2GetEventsParams) (*common.V2GetEventsResponse, error) {
	//initialize the selectedCategories either from the filter, if exists, or from the default values
	if len(params.Categories) == 0 {
		params.Categories = append(params.Categories, DefaultEventCategories...)
	}
	events, eventSeverityCount, eventCount, err := e.queryEvents(ctx, params)
	if err != nil {
		return nil, err
	}
	return &common.V2GetEventsResponse{
		Events:             events,
		EventSeverityCount: eventSeverityCount,
		EventCount:         eventCount,
	}, nil
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
