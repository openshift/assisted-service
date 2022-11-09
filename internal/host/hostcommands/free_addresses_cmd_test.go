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
	"gorm.io/gorm"
)

var _ = Describe("free_addresses", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var fCmd CommandGetter
	var id, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		fCmd = newFreeAddressesCmd(common.GetTestLog(), "quay.io/example/free_addresses:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusInsufficient)
		host.Inventory = common.GenerateTestDefaultInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("happy flow", func() {
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeFreeNetworkAddresses))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Illegal inventory", func() {
		host.Inventory = "blah"
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("Missing networks", func() {
		host.Inventory = "{}"
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("Some large ipv4 networks", func() {
		var originalInventory models.Inventory
		Expect(json.Unmarshal([]byte(host.Inventory), &originalInventory)).To(Succeed())

		originalInventory.Interfaces = []*models.Interface{}

		newInterface1 := models.Interface{
			IPV4Addresses: []string{"192.18.128.0/18"},
			IPV6Addresses: []string{},
			Name:          "chocobomb",
		}
		originalInventory.Interfaces = append(originalInventory.Interfaces, &newInterface1)

		newInterface2 := models.Interface{
			IPV4Addresses: []string{"192.18.128.0/22"},
			IPV6Addresses: []string{},
			Name:          "chocobomb",
		}
		originalInventory.Interfaces = append(originalInventory.Interfaces, &newInterface2)

		newInventoryBytes, err := json.Marshal(&originalInventory)
		Expect(err).ToNot(HaveOccurred())
		host.Inventory = string(newInventoryBytes)
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeFreeNetworkAddresses))
		stepReplyDecoded := &models.FreeAddressesRequest{}
		Expect(json.Unmarshal([]byte(stepReply[0].Args[0]), stepReplyDecoded)).To(Succeed())
		Expect(stepReplyDecoded).To(Equal(&models.FreeAddressesRequest{"192.18.128.0/22"}))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Only large ipv4 networks - no error", func() {
		var originalInventory models.Inventory
		Expect(json.Unmarshal([]byte(host.Inventory), &originalInventory)).To(Succeed())
		originalInventory.Interfaces = []*models.Interface{{
			IPV4Addresses: []string{"192.18.128.0/18"},
			IPV6Addresses: []string{},
			Name:          "chocobomb",
		}}
		newInventoryBytes, err := json.Marshal(&originalInventory)
		Expect(err).ToNot(HaveOccurred())
		host.Inventory = string(newInventoryBytes)
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("IPv6 only", func() {
		host.Inventory = common.GenerateTestIPv6Inventory()
		stepReply, stepErr = fCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
