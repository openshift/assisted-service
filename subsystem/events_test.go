package subsystem

import (
	"context"
	"strings"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Events tests", func() {

	It("Match Event Name", func() {
		c, err := userBMClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				BaseDNSDomain:     "fake.domain",
				Name:              swag.String("test-v2events-cluster"),
				OpenshiftVersion:  swag.String(openshiftVersion),
				PullSecret:        swag.String(pullSecret),
				VipDhcpAllocation: swag.Bool(false),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterId := *c.GetPayload().ID

		evs, err := userBMClient.Events.V2ListEvents(context.TODO(), &events.V2ListEventsParams{
			ClusterID:  &clusterId,
			HostIds:    nil,
			InfraEnvID: nil,
			Categories: []string{models.EventCategoryUser},
		})

		Expect(err).Should(BeNil())
		Expect(len(evs.GetPayload())).ShouldNot(Equal(0))
		eventsCount := 0
		for _, ev := range evs.Payload {
			Expect(ev.ClusterID.String()).Should(Equal(clusterId.String()))
			if strings.Contains(ev.Name, eventgen.ClusterRegistrationSucceededEventName) {
				eventsCount++
			}
		}
		Expect(eventsCount).ShouldNot(Equal(0))
	})
})
