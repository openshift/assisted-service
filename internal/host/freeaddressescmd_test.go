package host

import (
	"context"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("inventory", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var fCmd *freeAddressesCmd
	var id, clusterId strfmt.UUID
	var stepReply *models.Step
	var stepErr error
	dbName := "freeaddresses_cmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		fCmd = NewFreeAddressesCmd(getTestLog(), "quay.io/ocpmetal/free_addresses:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, HostStatusInsufficient)
		host.Inventory = defaultInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("happy flow", func() {
		stepReply, stepErr = fCmd.GetStep(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply.StepType).To(Equal(models.StepTypeFreeNetworkAddresses))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Illegal inventory", func() {
		host.Inventory = "blah"
		stepReply, stepErr = fCmd.GetStep(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("Missing networks", func() {
		host.Inventory = "{}"
		stepReply, stepErr = fCmd.GetStep(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
