package host

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/swag"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("dhcpallocate", func() {
	ctx := context.Background()
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var dCmd *dhcpAllocateCmd
	var id, clusterId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	dbName := "dhcpallocate_cmd"

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		dCmd = NewDhcpAllocateCmd(getTestLog(), "quay.io/ocpmetal/dhcp_lease_allocator:latest", db)

		id = strfmt.UUID("32b4463e-5f94-4245-87cf-a6948014045c")
		clusterId = strfmt.UUID("bd9d3b83-80a3-4b94-8b61-c12b2f1a2373")
		host = getTestHost(id, clusterId, models.HostStatusInsufficient)
		host.Inventory = masterInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("happy flow", func() {
		cluster = getTestCluster(clusterId, "1.2.3.0/24")
		cluster.VipDhcpAllocation = swag.Bool(true)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeDhcpLeaseAllocate))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply[0].Args)).To(BeNumerically(">", 0))
		var req models.DhcpAllocationRequest
		Expect(json.Unmarshal([]byte(stepReply[0].Args[len(stepReply[0].Args)-1]), &req)).ToNot(HaveOccurred())
		Expect(req.Interface).To(Equal(swag.String("eth0")))
		Expect(req.APIVipMac).To(Equal(asMAC("00:1a:4a:b5:4d:cc")))
		Expect(req.IngressVipMac).To(Equal(asMAC("00:1a:4a:83:b1:f7")))
		Expect(req.APIVipLease).To(BeEmpty())
		Expect(req.IngressVipLease).To(BeEmpty())
	})

	It("happy flow with leases", func() {
		cluster = getTestCluster(clusterId, "1.2.3.0/24")
		cluster.VipDhcpAllocation = swag.Bool(true)
		cluster.ApiVipLease = "apiLease"
		cluster.IngressVipLease = "ingressLease"
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeDhcpLeaseAllocate))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(len(stepReply[0].Args)).To(BeNumerically(">", 0))
		var req models.DhcpAllocationRequest
		Expect(json.Unmarshal([]byte(stepReply[0].Args[len(stepReply[0].Args)-1]), &req)).ToNot(HaveOccurred())
		Expect(req.Interface).To(Equal(swag.String("eth0")))
		Expect(req.APIVipMac).To(Equal(asMAC("00:1a:4a:b5:4d:cc")))
		Expect(req.IngressVipMac).To(Equal(asMAC("00:1a:4a:83:b1:f7")))
		Expect(req.APIVipLease).To(Equal("apiLease"))
		Expect(req.IngressVipLease).To(Equal("ingressLease"))
	})

	It("Dhcp disabled", func() {
		cluster = getTestCluster(clusterId, "1.2.3.0/24")
		cluster.VipDhcpAllocation = swag.Bool(false)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("CIDR missing", func() {
		cluster = getTestCluster(clusterId, "")
		cluster.VipDhcpAllocation = swag.Bool(true)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Bad CIDR", func() {
		cluster = getTestCluster(clusterId, "blah")
		cluster.VipDhcpAllocation = swag.Bool(true)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("CIDR Mismatch", func() {
		cluster = getTestCluster(clusterId, "4.5.6.0/24")
		cluster.VipDhcpAllocation = swag.Bool(true)
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
