package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("Migrate hosts pkey", func() {
	var (
		db     *gorm.DB
		dbName string
		gm     *gormigrate.Gormigrate
		hosts  []*common.Host
	)

	hostIsInList := func(hostId *strfmt.UUID, hosts []*common.Host) bool {
		for _, host := range hosts {
			if host.ID.String() == hostId.String() {
				return true
			}
		}
		return false
	}

	validateHosts := func() {
		validationDB, err := common.OpenTestDBConn(dbName)
		Expect(err).ShouldNot(HaveOccurred())
		defer common.CloseDB(validationDB)
		var hostsAfterMigrate []*common.Host
		Expect(validationDB.Find(&hostsAfterMigrate).Error).ToNot(HaveOccurred())
		Expect(hostsAfterMigrate).To(HaveLen(2))
		Expect(hostIsInList(hosts[0].ID, hostsAfterMigrate)).To(BeTrue())
		Expect(hostIsInList(hosts[1].ID, hostsAfterMigrate)).To(BeTrue())
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		for i := 0; i != 2; i++ {
			hostID := strfmt.UUID(uuid.New().String())
			clusterID := strfmt.UUID(uuid.New().String())
			infraEnvID := strfmt.UUID(uuid.New().String())
			host := common.Host{
				Host: models.Host{
					ID:         &hostID,
					ClusterID:  &clusterID,
					InfraEnvID: infraEnvID,
				},
			}
			Expect(db.Create(&host).Error).ToNot(HaveOccurred())
		}

		Expect(db.Find(&hosts).Error).ToNot(HaveOccurred())
		Expect(hosts).To(HaveLen(2))
		Expect(hosts[0].ID).ToNot(Equal(hosts[1].ID))
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
		Expect(gm.MigrateTo(MIGRATE_HOSTS_PKEY_ID)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		columnNames, err := getHostsPkeyColumns(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(columnNames).To(HaveLen(2))
		Expect(columnNames).To(ConsistOf("id", "infra_env_id"))
		validateHosts()

		err = gm.RollbackMigration(migrateHostsPkey())
		Expect(err).ToNot(HaveOccurred())

		columnNames, err = getHostsPkeyColumns(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(columnNames).To(HaveLen(2))
		Expect(columnNames).To(ConsistOf("id", "cluster_id"))
		validateHosts()

		err = gm.MigrateTo(MIGRATE_HOSTS_PKEY_ID)
		Expect(err).ToNot(HaveOccurred())

		columnNames, err = getHostsPkeyColumns(db)
		Expect(err).ToNot(HaveOccurred())
		Expect(columnNames).To(HaveLen(2))
		Expect(columnNames).To(ConsistOf("id", "infra_env_id"))
		validateHosts()
	})
})
