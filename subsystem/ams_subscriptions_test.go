package subsystem

import (
	"context"
	"net/http"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = Describe("test AMS subscriptions", func() {
	ctx := context.Background()

	BeforeEach(func() {
		if Options.AuthType == auth.TypeNone {
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

		waitForConsoleUrlUpdateInAMS := func(clusterID strfmt.UUID) {

			waitFunc := func() (bool, error) {
				c := getCommonCluster(ctx, clusterID)
				return c.IsAmsSubscriptionConsoleUrlSet, nil
			}
			err := wait.Poll(pollDefaultInterval, pollDefaultTimeout, waitFunc)
			Expect(err).NotTo(HaveOccurred())
		}

		It("happy flow", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)
			cc := getCommonCluster(ctx, clusterID)
			Expect(cc.AmsSubscriptionID).To(Equal(FakeSubscriptionID))

			// update subscription with console url
			registerHostsAndSetRoles(clusterID, minHosts)
			setClusterAsFinalizing(ctx, clusterID)
			completeInstallation(agentBMClient, clusterID)
			waitForConsoleUrlUpdateInAMS(clusterID)

			// update subscription with openshfit (external) cluster ID
			waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, clusterInstallingStateInfo)
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
	})

	Context("AMS subscription on cluster deletion", func() {

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("happy flow - delete 'reserved' subscription on cluster deletion", func() {

			// create subscription (in 'reserved' status)
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// we should delete the subscription, therefore, by making this AMS call fail and
			// expect inventory failure on deregistering a clsuter we can make sure the service has
			// try to delete the subscription
			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			// delete 'reserved' subscription
			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("happy flow - don't delete 'active' subscription on cluster deletion", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// update subscription with 'active' status
			err = wiremock.createStubsForGettingAMSSubscription(http.StatusOK, ocm.SubscriptionStatusActive)
			Expect(err).ToNot(HaveOccurred())

			// we should not delete the subscription, therefore, by making this AMS call fail and
			// expect inventory success on deregistering a clsuter we can make sure the service has
			// not try to delete the subscription
			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusUnauthorized)
			Expect(err).ToNot(HaveOccurred())

			// don't delete 'active' subscription
			// we can't really check that because it is done in an external dependency (AMS) so we just check there are no errors in the flow
			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).ToNot(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForDeletingAMSSubscription(http.StatusOK)
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("GetSubscription failed", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			err = wiremock.createStubsForGettingAMSSubscription(http.StatusUnauthorized, ocm.SubscriptionStatusReserved)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			err = wiremock.createStubsForGettingAMSSubscription(http.StatusServiceUnavailable, ocm.SubscriptionStatusReserved)
			Expect(err).ToNot(HaveOccurred())

			_, err = userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForGettingAMSSubscription(http.StatusOK, ocm.SubscriptionStatusReserved)
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

	Context("AMS subscription on cluster update with new cluster name", func() {

		It("happy flow", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			// update subscription's display name
			newClusterName := "ams-cluster-new-name"
			_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name: &newClusterName,
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("UpdateSubscription failed", func() {

			// create subscription
			clusterID, err := registerCluster(ctx, userBMClient, "test-ams-subscriptions-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)

			err = wiremock.createStubsForUpdatingAMSSubscription(http.StatusUnauthorized, subscriptionUpdateDisplayName)
			Expect(err).ToNot(HaveOccurred())

			// update subscription's display name
			newClusterName := "ams-cluster-new-name"
			_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name: &newClusterName,
				},
			})
			Expect(err).To(HaveOccurred())

			// override back to keep other tests consistent tests
			err = wiremock.createStubsForUpdatingAMSSubscription(http.StatusOK, subscriptionUpdatePostInstallation)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
