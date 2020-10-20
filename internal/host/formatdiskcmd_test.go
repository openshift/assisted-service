package host

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("format disk", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var fCmd *formatDiskCmd
	var id, clusterId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	dbName := "formatdisks_cmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		fCmd = NewFormatDiskCmd(getTestLog())

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, models.HostStatusKnown)
		host.Inventory = defaultInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("No Bootable Disks", func() {
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Illegal inventory", func() {
		host.Inventory = "blah"
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("No Disks", func() {
		host.Inventory = "{}"
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("One bootable disk", func() {
		disk := "vdb"
		inventory := models.Inventory{
			Disks: []*models.Disk{
				{
					SizeBytes: 130,
					DriveType: "HDD",
					Bootable:  true,
					Name:      disk,
				},
			},
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).NotTo(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply)).To(Equal(1))
		Expect(stepReply[0].Command).Should(Equal("dd"))
		of := fmt.Sprintf("of=/dev/%s", disk)
		expectedArgs := []string{"if=/dev/zero", of, "bs=10M", "count=1"}
		Expect(stepReply[0].Args).Should(Equal(expectedArgs))
	})

	It("Multiple bootable disks", func() {
		disk1 := "vdb"
		disk2 := "vdc"
		inventory := models.Inventory{
			Disks: []*models.Disk{
				{
					SizeBytes: 130,
					DriveType: "HDD",
					Bootable:  true,
					Name:      disk1,
				},
				{
					SizeBytes: 200,
					DriveType: "HDD",
					Bootable:  true,
					Name:      disk2,
				},
			},
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).NotTo(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply)).To(Equal(2))
		Expect(stepReply[0].Command).Should(Equal("dd"))
		Expect(stepReply[1].Command).Should(Equal("dd"))
		of := fmt.Sprintf("of=/dev/%s", disk1)
		expectedArgs := []string{"if=/dev/zero", of, "bs=10M", "count=1"}
		Expect(stepReply[0].Args).Should(Equal(expectedArgs))
		of = fmt.Sprintf("of=/dev/%s", disk2)
		expectedArgs = []string{"if=/dev/zero", of, "bs=10M", "count=1"}
		Expect(stepReply[1].Args).Should(Equal(expectedArgs))
	})

	It("Mixed disks", func() {
		inventory := models.Inventory{
			Disks: []*models.Disk{
				{
					SizeBytes: 130,
					DriveType: "HDD",
					Bootable:  true,
					Name:      "vdb",
				},
				{
					SizeBytes: 200,
					DriveType: "HDD",
					Bootable:  false,
					Name:      "vda",
				},
				{
					SizeBytes: 200,
					DriveType: "HDD",
					Bootable:  true,
					Name:      "vdc",
				},
			},
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).NotTo(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply)).To(Equal(2))
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
