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

var _ = Describe("inventory", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var invCmd *inventoryCmd
	var hostId, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		invCmd = NewInventoryCmd(common.GetTestLog(), "quay.io/example/inventory:latest")

		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = invCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(HaveLen(1))
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeInventory))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
