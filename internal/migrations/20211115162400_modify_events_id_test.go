package migrations

import (
	"strings"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("ModifyEventsID", func() {
	var (
		db          *gorm.DB
		dbName      string
		gm          *gormigrate.Gormigrate
		migrationID string
		events      []*common.Event
	)

	eventIsInList := func(eventId uint, events []*common.Event) bool {
		for _, event := range events {
			if event.ID == eventId {
				return true
			}
		}
		return false
	}

	validateEvents := func() {
		validationDB, err := common.OpenTestDBConn(dbName)
		Expect(err).ShouldNot(HaveOccurred())
		defer common.CloseDB(validationDB)
		var eventsAfterMigrate []*common.Event
		Expect(validationDB.Find(&eventsAfterMigrate).Error).ToNot(HaveOccurred())
		Expect(eventsAfterMigrate).To(HaveLen(2))
		Expect(eventIsInList(events[0].ID, eventsAfterMigrate)).To(BeTrue())
		Expect(eventIsInList(events[1].ID, eventsAfterMigrate)).To(BeTrue())
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		migrationID = "2021115162400"
		for i := 0; i != 2; i++ {
			hostID := strfmt.UUID(uuid.New().String())
			clusterID := strfmt.UUID(uuid.New().String())
			now := strfmt.DateTime(time.Now())
			event := common.Event{
				Event: models.Event{
					ClusterID: &clusterID,
					EventTime: &now,
					HostID:    &hostID,
					Message:   swag.String("Message"),
					Severity:  swag.String("info"),
				},
			}
			Expect(db.Create(&event).Error).ToNot(HaveOccurred())
		}

		Expect(db.Find(&events).Error).ToNot(HaveOccurred())
		Expect(events).To(HaveLen(2))
		Expect(events[0].ID).ToNot(Equal(events[1].ID))
		gm = gormigrate.New(db, gormigrate.DefaultOptions, pre())
		Expect(gm.MigrateTo(migrationID)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		t, err := getColumnType(dbName, &common.Event{}, "id")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("INT8"))
		validateEvents()

		err = gm.RollbackMigration(modifyEventsId())
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Event{}, "id")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("INT4"))
		validateEvents()

		err = gm.MigrateTo(migrationID)
		Expect(err).ToNot(HaveOccurred())

		t, err = getColumnType(dbName, &common.Event{}, "id")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.ToUpper(t)).To(Equal("INT8"))
		validateEvents()
	})
})
