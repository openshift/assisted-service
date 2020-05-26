package events

import (
	"context"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"time"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=event.go -package=events -destination=mock_event.go

type Handler interface {
	// AddEvents and an event for and entityID.
	// Since events, might relate to multiple entities, for example:
	//     host added to cluster, we have the host-id as the main entityID and
	//     the cluster-id as another ID that this event should be related to
	// otherEntities arguments provides for specifying mor IDs that are relevant for this event
	AddEvent(ctx context.Context, entityID string, msg string, eventTime time.Time, otherEntities ...string)
	GetEvents(entityID string) ([]*Event, error)
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

func addEventToDB(db *gorm.DB, id string, message string, t time.Time) error {
	tt := strfmt.DateTime(t)
	uid := strfmt.UUID(id)
	e := Event{
		Event: models.Event{
			EventTime: &tt,
			EntityID:  &uid,
			Message:   &message,
		},
	}

	if err := db.Create(&e).Error; err != nil {
		logrus.WithError(err).Error("Error adding event")
	}
	return nil
}

func (e *Events) AddEvent(ctx context.Context, entityID string, msg string, eventTime time.Time, otherEntities ...string) {
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

	err := addEventToDB(tx, entityID, msg, eventTime)
	if err != nil {
		return
	}

	// Since we don't keep different tables to support multiple IDs for a single event,
	// the workaround is to add to the DB a new event for every ID this event relates to
	for _, entity := range otherEntities {
		err := addEventToDB(tx, entity, msg, eventTime)
		if err != nil {
			return
		}
	}
	isSuccess = true
}

func (e Events) GetEvents(entityID string) ([]*Event, error) {
	var evs []*Event
	if err := e.db.Order("event_time").Find(&evs, "entity_id = ?", entityID).Error; err != nil {
		return nil, err
	}

	return evs, nil
}
