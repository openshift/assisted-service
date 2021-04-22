package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/dbc"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=event.go -package=events -destination=mock_event.go

type Handler interface {
	// AddEvents and an event for and entityID.
	// Since events, might relate to multiple entities, for example:
	//     host added to cluster, we have the host-id as the main entityID and
	//     the cluster-id as another ID that this event should be related to
	// otherEntities arguments provides for specifying mor IDs that are relevant for this event
	AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})
	GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*dbc.Event, error)
}

var _ Handler = &Events{}

type Events struct {
	db  *gorm.DB
	log logrus.FieldLogger
}

func New(db *gorm.DB, log logrus.FieldLogger) *Events {
	return &Events{
		db:  db,
		log: log,
	}
}

func (e *Events) saveEvent(ctx context.Context, tx *gorm.DB, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, message string, t time.Time, requestID string, props ...interface{}) error {
	log := logutil.FromContext(ctx, e.log)
	tt := strfmt.DateTime(t)
	uid := clusterID
	rid := strfmt.UUID(requestID)

	additionalProps, err := toProps(props...)
	if err != nil {
		log.WithError(err).Error("failed to parse event's properties field")
	}

	event := dbc.Event{
		Event: models.Event{
			EventTime: &tt,
			ClusterID: &uid,
			Severity:  &severity,
			Message:   &message,
			RequestID: rid,
			Props:     additionalProps,
		},
	}
	if hostID != nil {
		event.HostID = *hostID
	}

	if err := tx.Create(&event).Error; err != nil {
		log.WithError(err).Error("Error adding event")
	}
	return nil
}

func (e *Events) AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	log := logutil.FromContext(ctx, e.log)
	requestID := requestid.FromContext(ctx)
	var isSuccess bool = false

	//each event is saved in its own embedded transaction
	tx := e.db.Begin()
	defer func() {
		if !isSuccess {
			log.Warnf("Rolling back event transaction on event=%s cluster_id=%s", msg, clusterID)
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	if err := e.saveEvent(ctx, tx, clusterID, hostID, severity, msg, eventTime, requestID, props...); err != nil {
		return
	}
	isSuccess = true
}

func (e Events) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*dbc.Event, error) {
	var evs []*dbc.Event
	var err error
	if hostID == nil {
		err = e.db.Order("event_time").Find(&evs, "cluster_id = ?", clusterID.String()).Error
	} else {
		err = e.db.Order("event_time").Find(&evs, "cluster_id = ? AND host_id = ?", clusterID.String(), (*hostID).String()).Error
	}
	if err != nil {
		return nil, err
	}

	return evs, nil
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
