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

var _ = Describe("stop-podman", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var stopCmd *stopInstallationCmd
	var id, clusterId strfmt.UUID
	var stepReply *models.Step
	var stepErr error
	dbName := "stop_podman"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		stopCmd = NewStopInstallationCmd(getTestLog())

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, HostStatusError)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = stopCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeExecute))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
