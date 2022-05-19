package migrations

import (
	"encoding/json"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("Migrate static config format", func() {
	var (
		db                         *gorm.DB
		dbName                     string
		gm                         *gormigrate.Gormigrate
		infraEnvID1, infraEnvID2   strfmt.UUID
		staticNetworkConfigStr     string
		staticNetworkConfigJSONStr string
	)

	validatePreMigrate := func() {
		var infraEnvs []*models.InfraEnv
		Expect(db.Find(&infraEnvs).Error).ToNot(HaveOccurred())
		Expect(infraEnvs).To(HaveLen(2))
		for _, infraEnv := range infraEnvs {
			switch *infraEnv.ID {
			case infraEnvID1:
				Expect(infraEnv.StaticNetworkConfig).To(Equal(staticNetworkConfigStr))
			case infraEnvID2:
				Expect(infraEnv.StaticNetworkConfig).To(BeEmpty())
			default:
				Fail("Unexpected id")
			}
		}
	}

	validatePostMigrate := func() {
		var infraEnvs []*models.InfraEnv
		Expect(db.Find(&infraEnvs).Error).ToNot(HaveOccurred())
		Expect(infraEnvs).To(HaveLen(2))
		for _, infraEnv := range infraEnvs {
			switch *infraEnv.ID {
			case infraEnvID1:
				Expect(infraEnv.StaticNetworkConfig).To(Equal(staticNetworkConfigJSONStr))
			case infraEnvID2:
				Expect(infraEnv.StaticNetworkConfig).To(BeEmpty())
			default:
				Fail("Unexpected id")
			}
		}
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		map1 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
		}
		map2 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
		}
		staticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1", map1),
			common.FormatStaticConfigHostYAML("nic20", "02000048ba48", "192.168.126.31", "192.168.141.31", "192.168.126.1", map2),
		}
		b, err := json.Marshal(&staticNetworkConfig)
		Expect(err).ToNot(HaveOccurred())
		staticNetworkConfigJSONStr = string(b)
		infraEnvID1 = strfmt.UUID(uuid.New().String())
		staticNetworkConfigStr = formatStaticNetworkConfigForDB(staticNetworkConfig)
		infraEnv := common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:                  &infraEnvID1,
				StaticNetworkConfig: staticNetworkConfigStr,
			},
		}
		Expect(db.Create(&infraEnv).Error).ToNot(HaveOccurred())
		infraEnvID2 = strfmt.UUID(uuid.New().String())
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &infraEnvID2,
			},
		}
		Expect(db.Create(&infraEnv).Error).ToNot(HaveOccurred())
		gm = gormigrate.New(db, gormigrate.DefaultOptions, post())
		Expect(gm.MigrateTo(CHANGE_STATIC_CONFIG_FORMAT_KEY)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Migrates down and up", func() {
		validatePostMigrate()

		Expect(gm.RollbackMigration(changeStaticConfigFormat())).ToNot(HaveOccurred())
		validatePreMigrate()
		Expect(gm.MigrateTo(CHANGE_STATIC_CONFIG_FORMAT_KEY)).ToNot(HaveOccurred())

		validatePostMigrate()
	})
})
