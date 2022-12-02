package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/streaming"
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

func (e *Events) NotifyInternalEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, msg string) {
	log := logutil.FromContext(ctx, e.log)
	log.Debugf("Notifying internal event %s, nothing to do", msg)
}

func (e *Events) V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	requestID := requestid.FromContext(ctx)
	e.v2SaveEvent(ctx, clusterID, hostID, infraEnvID, name, models.EventCategoryMetrics, severity, msg, eventTime, requestID, props...)
}

func (e Events) queryEvents(ctx context.Context, selectedCategories []string,
	clusterID *strfmt.UUID, hostID *strfmt.UUID,
	infraEnvID *strfmt.UUID) (stream streaming.Stream[*common.Event], err error) {

	// Prepare the parts of the query:
	selectColumns := []string{
		"e.name",
		"e.cluster_id",
		"e.host_id",
		"e.infra_env_id",
		"e.severity",
		"e.event_time",
		"e.message",
		"e.props",
	}
	selectTables := []string{
		"events as e",
	}
	whereClauses := []string{
		fmt.Sprintf("e.category in (%s)", quote(selectedCategories...)),
	}
	orderClauses := []string{
		"e.event_time",
	}

	if clusterID != nil {
		whereClauses = append(
			whereClauses,
			fmt.Sprintf("e.cluster_id = %s", quote(clusterID.String())),
		)
	}
	if infraEnvID != nil {
		whereClauses = append(
			whereClauses,
			fmt.Sprintf("e.infra_env_id = %s", quote(infraEnvID.String())),
		)
	}
	if hostID != nil {
		whereClauses = append(
			whereClauses,
			fmt.Sprintf("e.host_id = %s", quote(hostID.String())),
		)
	}

	// TODO: Add authorization checks, don't merge without that!

	// Assemble the query:
	buffer := &bytes.Buffer{}
	fmt.Fprintf(buffer, "select\n")
	for i, selectColumn := range selectColumns {
		fmt.Fprintf(buffer, "\t%s", selectColumn)
		if i < len(selectColumns)-1 {
			fmt.Fprintf(buffer, ", ")
		}
		fmt.Fprintf(buffer, "\n")
	}
	fmt.Fprintf(buffer, "from\n")
	for i, selectTable := range selectTables {
		fmt.Fprintf(buffer, "\t%s", selectTable)
		if i < len(selectTables)-1 {
			fmt.Fprintf(buffer, ", ")
		}
		fmt.Fprintf(buffer, "\n")
	}
	if len(whereClauses) > 0 {
		fmt.Fprintf(buffer, "where\n")
		for i, whereClause := range whereClauses {
			fmt.Fprintf(buffer, "\t%s", whereClause)
			if i < len(whereClauses)-1 {
				fmt.Fprintf(buffer, " and")
			}
			fmt.Fprintf(buffer, "\n")
		}
	}
	if len(orderClauses) > 0 {
		fmt.Fprintf(buffer, "order by\n")
		for i, orderClause := range orderClauses {
			fmt.Fprintf(buffer, "\t%s", orderClause)
			if i < len(orderClauses)-1 {
				fmt.Fprintf(buffer, ", ")
			}
		}
		fmt.Fprintf(buffer, "\n")
	}
	query := buffer.String()

	// Create the row scanner:
	scanner := func(rows *sql.Rows) (event *common.Event, err error) {
		var (
			name       sql.NullString
			clusterID  sql.NullString
			hostID     sql.NullString
			infraEnvID sql.NullString
			severity   sql.NullString
			eventTime  sql.NullTime
			message    sql.NullString
			props      sql.NullString
		)
		err = rows.Scan(
			&name,
			&clusterID,
			&hostID,
			&infraEnvID,
			&severity,
			&eventTime,
			&message,
			&props,
		)
		if err != nil {
			return
		}
		event = &common.Event{}
		if name.Valid {
			event.Name = name.String
		}
		if clusterID.Valid {
			event.ClusterID = new(strfmt.UUID)
			*event.ClusterID = strfmt.UUID(clusterID.String)
		}
		if hostID.Valid {
			event.HostID = new(strfmt.UUID)
			*event.HostID = strfmt.UUID(hostID.String)
		}
		if infraEnvID.Valid {
			event.InfraEnvID = new(strfmt.UUID)
			*event.InfraEnvID = strfmt.UUID(infraEnvID.String)
		}
		if severity.Valid {
			event.Severity = new(string)
			*event.Severity = severity.String
		}
		if eventTime.Valid {
			event.EventTime = new(strfmt.DateTime)
			*event.EventTime = strfmt.DateTime(eventTime.Time)
		}
		if message.Valid {
			event.Message = new(string)
			*event.Message = message.String
		}
		if props.Valid {
			event.Props = props.String
		}
		return
	}

	// Start the transaction:
	db, err := e.db.DB()
	if err != nil {
		return nil, err
	}

	// Build the stream:
	stream, err = streaming.NewQuery[*common.Event]().
		DB(db).
		Text(query).
		Scanner(scanner).
		Fetch(100).
		Build()
	return
}

func quote(values ...string) string {
	buffer := &bytes.Buffer{}
	for _, value := range values {
		if buffer.Len() > 0 {
			buffer.WriteString(", ")
		}
		buffer.WriteString("'")
		buffer.WriteString(strings.ReplaceAll(value, "'", "''"))
		buffer.WriteString("'")
	}
	return buffer.String()
}

func (e Events) V2GetEvents(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID,
	infraEnvID *strfmt.UUID, categories ...string) (slice []*common.Event, err error) {
	stream, err := e.V2GetEventStream(ctx, clusterID, hostID, infraEnvID, categories...)
	if err != nil {
		return
	}
	slice, err = streaming.Collect(ctx, stream)
	if err != nil {
		return
	}
	return
}

func (e Events) V2GetEventStream(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID,
	infraEnvID *strfmt.UUID, categories ...string) (stream streaming.Stream[*common.Event], err error) {
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
