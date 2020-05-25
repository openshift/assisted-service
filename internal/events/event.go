package events

import (
	"time"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=event.go -package=events -destination=mock_event.go

type Handler interface {
	AddEvent(entityID string, msg string, eventTime time.Time, otherEntities ...string)
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

	d := db.Create(&e)
	er := d.GetErrors()
	if len(er) > 0 {
		logrus.WithError(er[0]).Error("Error adding event")
		return er[0]
	}
	return nil
}

func (e *Events) AddEvent(entityID string, msg string, eventTime time.Time, otherEntities ...string) {
	var isSuccess bool = false
	tx := e.db.Begin()
	defer func() {
		if !isSuccess {
			logrus.Warn("Rolling back transaction")
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	err := addEventToDB(tx, entityID, msg, eventTime)
	if err != nil {
		return
	}

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
	//if err := db.Where("event_time > ?", time.Now().Add(time.Duration(-40*time.Minute))).Order("event_time").Find(&evs, "entity_id = ?", "1").Error; err != nil {
	if err := e.db.Order("event_time").Find(&evs, "entity_id = ?", entityID).Error; err != nil {
		e.log.WithError(err).Errorf("failed to get list of events")
		return nil, err
	}

	return evs, nil
}
