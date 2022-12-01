package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("delete agent upgrade events", func() {
	var (
		db     *gorm.DB
		dbName string
		gm     *gormigrate.Gormigrate
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	newEvent := func(name string) *common.Event {
		clusterID := strfmt.UUID(uuid.New().String())
		now := strfmt.DateTime(time.Now())
		hostID := strfmt.UUID(uuid.New().String())
		return &common.Event{
			Event: models.Event{
				ClusterID: &clusterID,
				EventTime: &now,
				HostID:    &hostID,
				Name:      name,
			},
		}
	}

	It("delete only relevant events", func() {
		Expect(db.Create(newEvent("upgrade_agent_started")).Error).To(BeNil())
		Expect(db.Create(newEvent("upgrade_agent_started")).Error).To(BeNil())
		Expect(db.Create(newEvent("upgrade_agent_finished")).Error).To(BeNil())
		Expect(db.Create(newEvent("host_registration_succeeded")).Error).To(BeNil())
		Expect(db.Create(newEvent("cluster_logs_uploaded")).Error).To(BeNil())

		Expect(gm.MigrateTo(deleteAgentUpgradeEventsID)).ToNot(HaveOccurred())

		var events []*common.Event
		Expect(db.Find(&events).Error).To(BeNil())
		Expect(events).To(HaveLen(2))
		Expect(db.Find(&events).Where("name = host_registration_succeeded").Error).To(BeNil())
		Expect(db.Find(&events).Where("name = cluster_logs_uploaded").Error).To(BeNil())
	})

})
