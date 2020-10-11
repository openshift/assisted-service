package host

import (
	"context"
	"time"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
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
		stopCmd = NewStopInstallationCmd(getTestLog(), DefaultInstructionConfig)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, models.HostStatusError)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step with logs", func() {
		stepReply, stepErr = stopCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeExecute))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply.Command).Should(Equal("bash"))
	})
	It("get_step without logs", func() {
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		stepReply, stepErr = stopCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeExecute))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply.Command).Should(Equal("/usr/bin/podman"))
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
