package hostcommands

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("Upgrade agent command", func() {
	var ctx context.Context
	var host models.Host
	var db *gorm.DB
	var cmd CommandGetter
	var id, clusterId, infraEnvId strfmt.UUID
	var dbName string

	BeforeEach(func() {
		ctx = context.Background()
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusInsufficient)
		host.Inventory = common.GenerateTestDefaultInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		cmd = NewUpgradeAgentCmd("quay.io/my/image:v1.2.3")
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Creates a single upgrade agent step", func() {
		reply, err := cmd.GetSteps(ctx, &host)
		Expect(err).ToNot(HaveOccurred())
		Expect(reply).ToNot(BeNil())
		Expect(reply).To(HaveLen(1))
		Expect(reply[0].StepType).To(Equal(models.StepTypeUpgradeAgent))
	})
})
