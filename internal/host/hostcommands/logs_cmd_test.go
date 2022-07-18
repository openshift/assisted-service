package hostcommands

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/copier"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("upload_logs", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var logsCmd *logsCmd
	var id, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		logsCmd = NewLogsCmd(common.GetTestLog(), db, DefaultInstructionConfig)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusError)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("get_step with logs", func() {
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeLogsGather))
		Expect(stepErr).ShouldNot(HaveOccurred())
		request := models.LogsGatherCmdRequest{}
		err := json.Unmarshal([]byte(stepReply[0].Args[0]), &request)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(stepReply[0].Args)).To(Equal(1))
		Expect(request.InfraEnvID.String()).To(Equal(infraEnvId.String()))
		Expect(request.ClusterID.String()).To(Equal(clusterId.String()))
		Expect(request.HostID.String()).To(Equal(id.String()))
	})

	It("get_step with logs", func() {
		cfgCopy := InstructionConfig{}
		Expect(copier.Copy(&cfgCopy, &DefaultInstructionConfig)).To(BeNil())
		cfgCopy.ServiceCACertPath = common.HostCACertPath
		logsCmd = NewLogsCmd(common.GetTestLog(), db, cfgCopy)
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeLogsGather))
		Expect(stepErr).ShouldNot(HaveOccurred())
		request := models.LogsGatherCmdRequest{}
		err := json.Unmarshal([]byte(stepReply[0].Args[0]), &request)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(stepReply[0].Args)).To(Equal(1))
		Expect(request.InfraEnvID.String()).To(Equal(infraEnvId.String()))
		Expect(request.ClusterID.String()).To(Equal(clusterId.String()))
		Expect(request.HostID.String()).To(Equal(id.String()))
	})

	It("get_step without logs", func() {
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply)).Should(Equal(0))
	})
	It("get_step logs Masters IPs", func() {
		host.Bootstrap = true
		db.Save(&host)
		id = strfmt.UUID(uuid.New().String())
		host2 := hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusError)
		host2.Inventory = common.GenerateTestDefaultInventory()
		host2.Role = models.HostRoleMaster
		Expect(db.Create(&host2).Error).ToNot(HaveOccurred())
		cluster := hostutil.GenerateTestCluster(clusterId)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		request := models.LogsGatherCmdRequest{}
		err := json.Unmarshal([]byte(stepReply[0].Args[0]), &request)
		Expect(err).NotTo(HaveOccurred())
		Expect(request.MasterIps).To(Equal([]string{"1.2.3.4"}))
	})
	It("get_step logs Masters IPs user-managed-networking ", func() {
		host.Bootstrap = true
		db.Save(&host)
		id = strfmt.UUID(uuid.New().String())
		host2 := hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusError)
		inventory := models.Inventory{
			Interfaces: []*models.Interface{
				{
					Name: "eth0",
					IPV4Addresses: []string{
						"1.2.3.4/24",
						"10.30.40.50/24",
					},
					IPV6Addresses: []string{
						"2001:db8::/32",
					},
				},
			},
			Disks: []*models.Disk{
				common.TestDefaultConfig.Disks,
			},
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host2.Inventory = string(b)
		host2.Role = models.HostRoleMaster
		Expect(db.Create(&host2).Error).ToNot(HaveOccurred())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:                    &clusterId,
				UserManagedNetworking: swag.Bool(true),
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = logsCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		request := models.LogsGatherCmdRequest{}
		err = json.Unmarshal([]byte(stepReply[0].Args[0]), &request)
		Expect(err).NotTo(HaveOccurred())
		Expect(request.MasterIps).To(Equal([]string{"1.2.3.4", "10.30.40.50", "2001:db8::"}))
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
