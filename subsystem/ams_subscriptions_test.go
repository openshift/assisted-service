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
	"github.com/openshift/assisted-service/subsystem/utils_test"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = Describe("test AMS subscriptions", func() {
	ctx := context.Background()

	BeforeEach(func() {
		if Options.AuthType == auth.TypeNone {
			Skip("auth is disabled")
		}
	})

	AfterEach(func() {
		err := wiremock.CreateStubsForCreatingAMSSubscription(http.StatusOK)
		Expect(err).ToNot(HaveOccurred())
		err = wiremock.CreateStubsForDeletingAMSSubscription(http.StatusOK)
		Expect(err).ToNot(HaveOccurred())

		err = wiremock.CreateStubsForGettingAMSSubscription(http.StatusOK, ocm.SubscriptionStatusReserved)
		Expect(err).ToNot(HaveOccurred())

		err = wiremock.CreateStubsForUpdatingAMSSubscription(http.StatusOK, utils_test.SubscriptionUpdateDisplayName)
		Expect(err).ToNot(HaveOccurred())

		err = wiremock.CreateStubsForUpdatingAMSSubscription(http.StatusOK, utils_test.SubscriptionUpdateOpenshiftClusterID)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("AMS subscription on cluster creation", func() {

		It("happy flow", func() {

			clusterID, err := utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			log.Infof("Register cluster %s", clusterID)
			cc := utils_test.TestContext.GetCommonCluster(ctx, clusterID)
			Expect(cc.AmsSubscriptionID).To(Equal(utils_test.FakeSubscriptionID))
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("CreateSubscription failed", func() {

			By("override wiremock stub to fail AMS call on Unauthorized error", func() {
				err := wiremock.CreateStubsForCreatingAMSSubscription(http.StatusUnauthorized)
				Expect(err).ToNot(HaveOccurred())
			})

			By("register cluster", func() {
				_, err := utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).To(HaveOccurred())
			})

			By("override wiremock stub to fail AMS call on ServiceUnavailable error", func() {
				err := wiremock.CreateStubsForCreatingAMSSubscription(http.StatusServiceUnavailable)
				Expect(err).ToNot(HaveOccurred())
			})

			By("register cluster", func() {
				_, err := utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("AMS subscription on cluster deletion", func() {

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("happy flow - delete 'reserved' subscription on cluster deletion", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription (in 'reserved' status)", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("override wiremock stub to fail AMS call", func() {
				// we should delete the subscription, therefore, by making this AMS call fail and
				// expect inventory failure on deregistering a clsuter we can make sure the service has
				// try to delete the subscription
				err = wiremock.CreateStubsForDeletingAMSSubscription(http.StatusUnauthorized)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete 'reserved' subscription", func() {
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).To(HaveOccurred())
			})
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("happy flow - don't delete 'active' subscription on cluster deletion", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("update subscription with 'active' status", func() {
				err = wiremock.CreateStubsForGettingAMSSubscription(http.StatusOK, ocm.SubscriptionStatusActive)
				Expect(err).ToNot(HaveOccurred())
			})

			By("override wiremock stub to fail AMS call", func() {
				// we should not delete the subscription, therefore, by making this AMS call fail and
				// expect inventory success on deregistering a clsuter we can make sure the service has
				// not try to delete the subscription
				err = wiremock.CreateStubsForDeletingAMSSubscription(http.StatusUnauthorized)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete subscription", func() {
				// don't delete 'active' subscription
				// we can't really check that because it is done in an external dependency (AMS) so we just check there are no errors in the flow
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("GetSubscription failed", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForGettingAMSSubscription(http.StatusUnauthorized, ocm.SubscriptionStatusReserved)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete subscription", func() {
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).To(HaveOccurred())
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForGettingAMSSubscription(http.StatusServiceUnavailable, ocm.SubscriptionStatusReserved)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete subscription", func() {
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).To(HaveOccurred())
			})
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("DeleteSubscription failed", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForDeletingAMSSubscription(http.StatusUnauthorized)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete subscription", func() {
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).To(HaveOccurred())
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForDeletingAMSSubscription(http.StatusServiceUnavailable)
				Expect(err).ToNot(HaveOccurred())
			})

			By("delete subscription", func() {
				_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: clusterID})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("AMS subscription on cluster update with new cluster name", func() {

		It("happy flow", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("update subscription's display name", func() {
				newClusterName := "ams-cluster-new-name"
				_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Name: &newClusterName,
					},
				})
				Expect(err).ToNot(HaveOccurred())
			})
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("UpdateSubscription failed", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForUpdatingAMSSubscription(http.StatusUnauthorized, utils_test.SubscriptionUpdateDisplayName)
				Expect(err).ToNot(HaveOccurred())
			})

			By("update subscription's display name", func() {
				newClusterName := "ams-cluster-new-name"
				_, err = utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Name: &newClusterName,
					},
				})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("AMS subscription on cluster installation", func() {

		waitForConsoleUrlUpdateInAMS := func(clusterID strfmt.UUID) {

			waitFunc := func(ctx context.Context) (bool, error) {
				c := utils_test.TestContext.GetCommonCluster(ctx, clusterID)
				return c.IsAmsSubscriptionConsoleUrlSet, nil
			}
			err := wait.PollUntilContextTimeout(ctx, pollDefaultInterval, pollDefaultTimeout, false, waitFunc)
			Expect(err).NotTo(HaveOccurred())
		}

		It("happy flow", func() {

			var clusterID strfmt.UUID
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("update subscription with openshfit (external) cluster ID", func() {
				infraEnvID := registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
				registerHostsAndSetRoles(clusterID, *infraEnvID, utils_test.MinHosts, "test-cluster", "example.com")
				setClusterAsInstalling(ctx, clusterID)
			})

			By("update subscription with console url", func() {
				c := utils_test.TestContext.GetCluster(clusterID)
				for _, host := range c.Hosts {
					utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
				}
				waitForClusterState(ctx, clusterID, models.ClusterStatusFinalizing, utils_test.DefaultWaitForClusterStateTimeout, utils_test.ClusterFinalizingStateInfo)
				completeInstallation(utils_test.TestContext.AgentBMClient, clusterID)
				waitForConsoleUrlUpdateInAMS(clusterID)
			})

			By("update subscription with status 'Active'", func() {
				waitForClusterState(ctx, clusterID, models.ClusterStatusInstalled, utils_test.DefaultWaitForClusterStateTimeout, utils_test.ClusterInstallingStateInfo)
			})
		})

		// ATTENTION: this test override a wiremock stub - DO NOT RUN IN PARALLEL
		It("UpdateSubscription failed", func() {

			var clusterID strfmt.UUID
			var reply *installer.V2InstallClusterAccepted
			var err error

			By("create subscription", func() {
				clusterID, err = utils_test.TestContext.RegisterCluster(ctx, utils_test.TestContext.UserBMClient, "test-cluster", pullSecret)
				Expect(err).ToNot(HaveOccurred())
				log.Infof("Register cluster %s", clusterID)
			})

			By("override wiremock stub to fail AMS call", func() {
				err = wiremock.CreateStubsForUpdatingAMSSubscription(http.StatusUnauthorized, utils_test.SubscriptionUpdateOpenshiftClusterID)
				Expect(err).ToNot(HaveOccurred())
			})

			By("update subscription with openshfit (external) cluster ID", func() {
				infraEnvID := registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
				registerHostsAndSetRoles(clusterID, *infraEnvID, utils_test.MinHosts, "test-cluster", "example.com")
				reply, err = utils_test.TestContext.UserBMClient.Installer.V2InstallCluster(context.Background(), &installer.V2InstallClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				c := reply.GetPayload()
				Expect(*c.Status).Should(Equal(models.ClusterStatusPreparingForInstallation))
				utils_test.TestContext.GenerateEssentialPrepareForInstallationSteps(ctx, c.Hosts...)
				utils_test.TestContext.WaitForLastInstallationCompletionStatus(clusterID, models.LastInstallationPreparationStatusFailed)
			})
		})
	})
})
