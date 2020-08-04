package host

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("reset", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var rstCmd *resetInstallationCmd
	var id, clusterId strfmt.UUID
	var stepReply *models.Step
	var stepErr error
	dbName := "reset_cmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		rstCmd = NewResetInstallationCmd(getTestLog())

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = rstCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeResetInstallation))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
