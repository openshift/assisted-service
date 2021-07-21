package hostcommands

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	models "github.com/openshift/assisted-service/models/v1"
)

var _ = Describe("inventory", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var invCmd *inventoryCmd
	var id, clusterId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		invCmd = NewInventoryCmd(common.GetTestLog(), "quay.io/ocpmetal/inventory:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, clusterId, models.HostStatusDiscovering)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = invCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(HaveLen(1))
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeInventory))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("mounts viable linux paths for HW detection", func() {
		stepReply, stepErr = invCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(HaveLen(1))
		step := stepReply[0]

		By("running two commands via sh")
		Expect(step.Command).To(Equal("sh"))
		Expect(step.Args[0]).To(Equal("-c"))
		Expect(step.Args[1]).To(ContainSubstring("&&"))
		Expect(step.Args[1]).To(ContainSubstring("cp /etc/mtab /root/mtab"))

		By("verifying mounts to host's filesystem")
		Expect(step.Args[1]).To(ContainSubstring("/root/mtab:/host/etc/mtab:ro"))
		paths := []string{
			"/proc/meminfo",
			"/sys/kernel/mm/hugepages",
			"/proc/cpuinfo",
			"/sys/block",
			"/sys/devices",
			"/sys/bus",
			"/sys/class",
			"/run/udev",
		}
		for _, path := range paths {
			Expect(step.Args[1]).To(ContainSubstring(fmt.Sprintf("-v %[1]v:/host%[1]v:ro", path)))
		}
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
