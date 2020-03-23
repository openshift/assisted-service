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

var _ = Describe("Host tests", func() {
	ctx := context.Background()
	AfterEach(func() {
		clearDB()
	})

	It("host CRUD", func() {
		host, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		reply, err := bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		replyHost := reply.GetPayload()
		Expect(*replyHost.Status).Should(Equal("discovering"))

		list, err := bmclient.Inventory.ListHosts(ctx, &inventory.ListHostsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterHost(ctx, &inventory.DeregisterHostParams{HostID: host.GetPayload().ID.String()})
		list, err = bmclient.Inventory.ListHosts(ctx, &inventory.ListHostsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host.GetPayload().ID})
		Expect(err).Should(HaveOccurred())
	})

	It("next step", func() {
		host, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		reply, err := bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host.GetPayload().ID})
		_, ok := getStepInList(reply.GetPayload(), models.StepTypeHardawareInfo)
		Expect(ok).Should(Equal(true))
	})

	It("disable enable", func() {
		host, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Inventory.DisableHost(ctx, &inventory.DisableHostParams{HostID: *host.GetPayload().ID})
		reply, err := bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		replyHost := reply.GetPayload()
		Expect(*replyHost.Status).Should(Equal("disabled"))

		nsteps, err := bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host.GetPayload().ID})
		Expect(len(nsteps.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.EnableHost(ctx, &inventory.EnableHostParams{HostID: *host.GetPayload().ID})
		reply, err = bmclient.Inventory.GetHost(ctx, &inventory.GetHostParams{HostID: *host.GetPayload().ID})
		Expect(err).NotTo(HaveOccurred())
		replyHost = reply.GetPayload()
		Expect(*replyHost.Status).Should(Equal("discovering"))

		nsteps, err = bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host.GetPayload().ID})
		Expect(len(nsteps.GetPayload())).ShouldNot(Equal(0))
	})

	It("debug", func() {
		host1, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		host2, err := bmclient.Inventory.RegisterHost(ctx, &inventory.RegisterHostParams{
			NewHostParams: &models.HostCreateParams{
				HostID:    strToUUID(uuid.New().String()),
				Namespace: swag.String("my namespace"),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// set debug to host1
		_, err = bmclient.Inventory.SetDebugStep(ctx, &inventory.SetDebugStepParams{
			HostID: *host1.GetPayload().HostID,
			Step:   &models.DebugStep{Command: swag.String("echo hello")},
		})
		Expect(err).NotTo(HaveOccurred())

		var step *models.Step
		var ok bool
		// debug should be only for host1
		reply, err := bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host2.GetPayload().ID})
		_, ok = getStepInList(reply.GetPayload(), models.StepTypeDebug)
		Expect(ok).Should(Equal(false))

		reply, err = bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host1.GetPayload().ID})
		step, ok = getStepInList(reply.GetPayload(), models.StepTypeDebug)
		Expect(ok).Should(Equal(true))
		Expect(step.Data).Should(Equal("echo hello"))

		// debug executed only once
		reply, err = bmclient.Inventory.GetNextSteps(ctx, &inventory.GetNextStepsParams{HostID: *host1.GetPayload().ID})
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
