package subsystem

import (
	"context"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("test AMS subscriptions", func() {
	ctx := context.Background()

	BeforeEach(func() {
		if !Options.EnableAuth {
			Skip("auth is disabled")
		}
		if !Options.WithAMSSubscriptions {
			Skip("AMS is disabled")
		}
	})

	AfterEach(func() {
		clearDB()
	})

	Context("AMS subscription on cluster creation", func() {

		It("happy flow", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)
			cc := getCommonCluster(ctx, clusterID)
			Expect(cc.AmsSubscriptionID).To(Equal(FakeSubscriptionID))

			// update subscription with openshfit (external) cluster ID
			registerHostsAndSetRoles(clusterID, 3)
			c := installCluster(clusterID)
			for _, h := range c.Hosts {
				updateProgress(*h.ID, clusterID, models.HostStageDone)
			}
			waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
			err = completeInstallation(agentBMClient, clusterID)
			Expect(err).ToNot(HaveOccurred())
			waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, "installed")
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("CreateSubscription failed", func() {

			err := wiremock.createStubsForCreatingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			_, err = registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).To(HaveOccurred())

			err = wiremock.createStubsForCreatingAMSSubscription(http.StatusServiceUnavailable)
			Expect(err).ToNot(HaveOccurred())

			_, err = registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForCreatingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("UpdateSubscription failed", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// update subscription with openshfit (external) cluster ID
			registerHostsAndSetRoles(clusterID, 3)
			c := installCluster(clusterID)
			for _, h := range c.Hosts {
				updateProgress(*h.ID, clusterID, models.HostStageDone)
			}
			waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)

			err = wiremock.createStubsForUpdatingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			err = completeInstallation(agentBMClient, clusterID)
			Expect(err).To(HaveOccurred())

			err = wiremock.createStubsForUpdatingAMSSubscription(http.StatusServiceUnavailable)
			Expect(err).ToNot(HaveOccurred())

			err = completeInstallation(agentBMClient, clusterID)
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForUpdatingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("AMS subscription on cluster deletion", func() {

		It("happy flow - delete 'reserved' subscription on cluster deletion", func() {

			// create subscription (in 'reserved' status)
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// delete 'reserved' subscription
			// we can't really check that because it is done in an external dependency (AMS) so we just check there are no errors in the flow
			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).ToNot(HaveOccurred())
		})

		It("happy flow - don't delete 'active' subscription on cluster deletion", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// update subscription with 'active' status
			registerHostsAndSetRoles(clusterID, 3)
			c := installCluster(clusterID)
			for _, h := range c.Hosts {
				updateProgress(*h.ID, clusterID, models.HostStageDone)
			}
			waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, defaultWaitForClusterStateTimeout, clusterFinalizingStateInfo)
			err = completeInstallation(agentBMClient, clusterID)
			Expect(err).ToNot(HaveOccurred())
			waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, "installed")

			// don't delete 'active' subscription
			// we can't really check that because it is done in an external dependency (AMS) so we just check there are no errors in the flow
			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("GetSubscription failed", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			err = wiremock.createStubsForGettingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			err = wiremock.createStubsForGettingAMSSubscription(http.StatusServiceUnavailable)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForGettingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("DeleteSubscription failed", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusServiceUnavailable)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
