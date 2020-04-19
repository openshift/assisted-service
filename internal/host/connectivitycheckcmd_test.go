package host

import (
	"context"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
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

	BeforeEach(func() {
		db = prepareDB()
		connectivityCheckCmd = NewConnectivityCheckCmd(getTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
			Status:       swag.String(HostStatusDiscovering),
			HardwareInfo: defaultHwInfo,
		}
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

		// cleanup
		db.Close()
		stepReply = nil
		stepErr = nil
	})
})
