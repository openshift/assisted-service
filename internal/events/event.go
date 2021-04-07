package events

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

type Sender interface {
	// AddEvents and an event for and entityID.
	// Since events, might relate to multiple entities, for example:
	//     host added to cluster, we have the host-id as the main entityID and
	//     the cluster-id as another ID that this event should be related to
	// otherEntities arguments provides for specifying mor IDs that are relevant for this event
	AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time)

	SendClusterEvent(ctx context.Context, event ClusterEvent, eventTime time.Time)
	SendHostEvent(ctx context.Context, event HostEvent, eventTime time.Time)
}

//go:generate mockgen -source=event.go -package=events -destination=mock_event.go
type Handler interface {
	Sender
	GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*common.Event, error)
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

func addEventToDB(log logrus.FieldLogger, db *gorm.DB, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, message string, t time.Time, requestID string) error {
	tt := strfmt.DateTime(t)
	uid := clusterID
	rid := strfmt.UUID(requestID)

	e := common.Event{
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

func (e *Events) SendClusterEvent(ctx context.Context, event ClusterEvent, eventTime time.Time) {
	e.AddEvent(ctx, *event.GetClusterId(), nil, event.GetSeverity(), event.FormatMessage(), eventTime)
}

func (e *Events) SendHostEvent(ctx context.Context, event HostEvent, eventTime time.Time) {
	e.AddEvent(ctx, *event.GetClusterId(), event.GetHostId(), event.GetSeverity(), event.FormatMessage(), eventTime)
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

func (e Events) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID) ([]*common.Event, error) {
	var evs []*common.Event
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

type BaseEvent interface {
	GetId() string
	GetSeverity() string
	FormatMessage() string
	FormatLogMessage() string
}

type ClusterEvent interface {
	BaseEvent
	GetClusterId() *strfmt.UUID
}

type HostEvent interface {
	BaseEvent
	GetClusterId() *strfmt.UUID
	GetHostId() *strfmt.UUID
}
