package subsystem

import (
	"context"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node tests", func() {
	ctx := context.Background()
	AfterEach(func() {
		clearDB()
	})

	It("node CRUD", func() {
		node, err := bmclient.Inventory.RegisterNode(ctx, &inventory.RegisterNodeParams{
			NewNodeParams: &models.NodeCreateParams{
				HardwareInfo: swag.String("some HW info"),
				Namespace:    swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = bmclient.Inventory.GetNode(ctx, &inventory.GetNodeParams{NodeID: node.GetPayload().ID.String()})
		Expect(err).NotTo(HaveOccurred())

		list, err := bmclient.Inventory.ListNodes(ctx, &inventory.ListNodesParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterNode(ctx, &inventory.DeregisterNodeParams{NodeID: node.GetPayload().ID.String()})
		list, err = bmclient.Inventory.ListNodes(ctx, &inventory.ListNodesParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetNode(ctx, &inventory.GetNodeParams{NodeID: node.GetPayload().ID.String()})
		Expect(err).Should(HaveOccurred())
	})
})
