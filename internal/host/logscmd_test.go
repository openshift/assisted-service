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

var _ = Describe("upload_logs", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var logsCmd *logsCmd
	var id, clusterId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	dbName := "logs_cmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		logsCmd = NewLogsCmd(getTestLog(), db, DefaultInstructionConfig)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, models.HostStatusError)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step with logs", func() {
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeExecute))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply[0].Command).Should(Equal("podman"))
	})
	It("get_step without logs", func() {
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply)).Should(Equal(0))
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
