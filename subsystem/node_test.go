package subsystem

import (
	"context"

	"github.com/google/uuid"

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
				NodeID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		reply, err := bmclient.Inventory.GetNode(ctx, &inventory.GetNodeParams{NodeID: *node.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		replyNode := reply.GetPayload()
		Expect(*replyNode.Status).Should(Equal("discovering"))

		list, err := bmclient.Inventory.ListNodes(ctx, &inventory.ListNodesParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterNode(ctx, &inventory.DeregisterNodeParams{NodeID: node.GetPayload().ID.String()})
		list, err = bmclient.Inventory.ListNodes(ctx, &inventory.ListNodesParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetNode(ctx, &inventory.GetNodeParams{NodeID: *node.GetPayload().ID})
		Expect(err).Should(HaveOccurred())
	})

	It("next step", func() {
		node, err := bmclient.Inventory.RegisterNode(ctx, &inventory.RegisterNodeParams{
			NewNodeParams: &models.NodeCreateParams{
				NodeID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		reply, err := bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{NodeID: *node.GetPayload().ID})

		var found bool
		for _, step := range reply.GetPayload() {
			if step.StepType == models.StepTypeHardawareInfo {
				found = true
				break
			}
		}
		Expect(found).Should(Equal(true))
	})
})
