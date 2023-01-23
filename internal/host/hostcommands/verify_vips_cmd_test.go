package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("verify_vips", func() {
	ctx := context.Background()
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var vCmd CommandGetter
	var id, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		vCmd = newVerifyVipsCmd(common.GetTestLog(), db)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusInsufficient)
		cluster = hostutil.GenerateTestClusterWithMachineNetworks(clusterId,
			[]*models.MachineNetwork{{Cidr: "1.2.3.0/24", ClusterID: clusterId}, {Cidr: "1001:db8::/120", ClusterID: clusterId}})
		host.Inventory = common.GenerateTestDefaultInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
	})

	setApiVips := func(apiVips ...string) {
		for _, v := range apiVips {
			Expect(db.Save(&models.APIVip{ClusterID: clusterId, IP: models.IP(v)}).Error).ToNot(HaveOccurred())
		}
	}

	setIngressVips := func(ingressVips ...string) {
		for _, v := range ingressVips {
			Expect(db.Save(&models.IngressVip{ClusterID: clusterId, IP: models.IP(v)}).Error).ToNot(HaveOccurred())
		}
	}

	getCluster := func() *common.Cluster {
		cls, err := common.GetClusterFromDB(db, clusterId, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		return cls
	}

	getRequest := func(data string) models.VerifyVipsRequest {
		var request models.VerifyVipsRequest
		Expect(json.Unmarshal([]byte(data), &request)).ToNot(HaveOccurred())
		return request
	}

	expectData := func(data string) {
		request := getRequest(data)
		cls := getCluster()
		Expect(request).To(HaveLen(len(cls.APIVips) + len(cls.IngressVips)))
		for _, a := range cls.APIVips {
			v := a
			Expect(funk.Find(request, func(ver *models.VerifyVip) bool { return v.IP == ver.Vip && ver.VipType == models.VipTypeAPI })).ToNot(BeNil())
		}
		for _, i := range cls.IngressVips {
			v := i
			Expect(funk.Find(request, func(ver *models.VerifyVip) bool { return v.IP == ver.Vip && ver.VipType == models.VipTypeIngress })).ToNot(BeNil())
		}
	}

	It("no vips", func() {
		stepReply, stepErr = vCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("with vips", func() {
		setApiVips("1.2.3.10", "1001:db8::100")
		setIngressVips("1.2.3.11", "1001:db8::101")
		stepReply, stepErr = vCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeVerifyVips))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply[0].Args).To(HaveLen(1))
		expectData(stepReply[0].Args[0])
	})

	It("only with api vips", func() {
		setApiVips("1.2.3.10", "1001:db8::100")
		stepReply, stepErr = vCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeVerifyVips))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply[0].Args).To(HaveLen(1))
		expectData(stepReply[0].Args[0])
	})

	It("with ipv4 vips", func() {
		setApiVips("1.2.3.10")
		setIngressVips("1.2.3.11")
		stepReply, stepErr = vCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeVerifyVips))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply[0].Args).To(HaveLen(1))
		expectData(stepReply[0].Args[0])
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
