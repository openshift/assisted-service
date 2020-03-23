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
		_, ok := getStepInList(reply.GetPayload(), models.StepTypeHardawareInfo)
		Expect(ok).Should(Equal(true))
	})

	It("debug", func() {
		node1, err := bmclient.Inventory.RegisterNode(ctx, &inventory.RegisterNodeParams{
			NewNodeParams: &models.NodeCreateParams{
				NodeID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		node2, err := bmclient.Inventory.RegisterNode(ctx, &inventory.RegisterNodeParams{
			NewNodeParams: &models.NodeCreateParams{
				NodeID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// set debug to node1
		_, err = bmclient.Inventory.SetDebugStep(ctx, &inventory.SetDebugStepParams{
			NodeID: *node1.GetPayload().NodeID,
			Step:   &models.DebugStep{Command: swag.String("echo hello")},
		})
		Expect(err).NotTo(HaveOccurred())

		var step *models.Step
		var ok bool
		// debug should be only for node1
		reply, err := bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{NodeID: *node2.GetPayload().ID})
		_, ok = getStepInList(reply.GetPayload(), models.StepTypeDebug)
		Expect(ok).Should(Equal(false))

		reply, err = bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{NodeID: *node1.GetPayload().ID})
		step, ok = getStepInList(reply.GetPayload(), models.StepTypeDebug)
		Expect(ok).Should(Equal(true))
		Expect(step.Data).Should(Equal("echo hello"))

		// debug executed only once
		reply, err = bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{NodeID: *node1.GetPayload().ID})
		_, ok = getStepInList(reply.GetPayload(), models.StepTypeDebug)
		Expect(ok).Should(Equal(false))
	})
})

func getStepInList(steps models.Steps, sType models.StepType) (*models.Step, bool) {
	for _, step := range steps {
		if step.StepType == sType {
			return step, true
		}
	}
	return nil, false
}
