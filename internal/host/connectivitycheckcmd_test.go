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

var _ = Describe("connectivitycheckcmd", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var connectivityCheckCmd *connectivityCheckCmd
	var id, clusterId strfmt.UUID
	var stepReply *models.Step
	var stepErr error
	dbName := "connectivitycheckcmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		connectivityCheckCmd = NewConnectivityCheckCmd(getTestLog(), db, nil, "quay.io/ocpmetal/connectivity_check:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, HostStatusDiscovering)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = connectivityCheckCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeConnectivityCheck))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step_unknow_cluster_id", func() {
		host.ClusterID = strfmt.UUID(uuid.New().String())
		stepReply, stepErr = connectivityCheckCmd.GetStep(ctx, &host)
		Expect(stepReply.StepType).To(Equal(models.StepTypeConnectivityCheck))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
