package events

import (
	"context"
	"time"

	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=event.go -package=events -destination=mock_event.go

type Handler interface {
	// AddEvents and an event for and entityID.
	// Since events, might relate to multiple entities, for example:
	//     host added to cluster, we have the host-id as the main entityID and
	//     the cluster-id as another ID that this event should be related to
	// otherEntities arguments provides for specifying mor IDs that are relevant for this event
	AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time)
	GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*Event, error)
}

var _ Handler = &Events{}

type Event struct {
	gorm.Model
	models.Event
}

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

func addEventToDB(log logrus.FieldLogger, db *gorm.DB, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, message string, t time.Time, requestID string) error {
	tt := strfmt.DateTime(t)
	uid := clusterID
	rid := strfmt.UUID(requestID)

	e := Event{
		Event: models.Event{
			EventTime: &tt,
			ClusterID: &uid,
			Severity:  &severity,
			Message:   &message,
			RequestID: rid,
		},
	}
	if hostID != nil {
		e.HostID = *hostID
	}

	if err := db.Create(&e).Error; err != nil {
		log.WithError(err).Error("Error adding event")
	}
	return nil
}

func (e *Events) AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time) {
	log := logutil.FromContext(ctx, e.log)
	var isSuccess bool = false
	tx := e.db.Begin()
	defer func() {
		if !isSuccess {
			log.Warn("Rolling back transaction")
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	requestID := requestid.FromContext(ctx)
	err := addEventToDB(log, tx, clusterID, hostID, severity, msg, eventTime, requestID)
	if err != nil {
		return
	}
	isSuccess = true
}

func (e Events) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*Event, error) {
	var evs []*Event
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
