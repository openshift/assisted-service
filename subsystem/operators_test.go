package subsystem

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	opclient "github.com/openshift/assisted-service/client/operators"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/cnv"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Operators endpoint tests", func() {

	var (
		clusterID strfmt.UUID
	)

	AfterEach(func() {
		clearDB()
	})

	Context("supported-operators", func() {
		It("should return all supported operators", func() {
			reply, err := userBMClient.Operators.ListSupportedOperators(context.TODO(), opclient.NewListSupportedOperatorsParams())

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.GetPayload()).To(ConsistOf(ocs.Operator.Name, lso.Operator.Name, cnv.Operator.Name))
		})

		It("should provide operator properties", func() {
			params := opclient.NewListOperatorPropertiesParams().WithOperatorName(ocs.Operator.Name)
			reply, err := userBMClient.Operators.ListOperatorProperties(context.TODO(), params)

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.Payload).To(BeEquivalentTo(models.OperatorProperties{}))
		})
	})

	Context("Create cluster", func() {
		It("Have builtins", func() {
			reply, err := userBMClient.Installer.RegisterCluster(context.TODO(), &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			cluster := reply.GetPayload()
			c := &common.Cluster{Cluster: *cluster}

			for _, builtinOperator := range operators.NewManager(log, nil, operators.Options{}, nil, nil).GetSupportedOperatorsByType(models.OperatorTypeBuiltin) {
				Expect(operators.IsEnabled(c.MonitoredOperators, builtinOperator.Name)).Should(BeTrue())
			}
		})

		It("New OLM", func() {
			newOperator := ocs.Operator.Name

			reply, err := userBMClient.Installer.RegisterCluster(context.TODO(), &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
					OlmOperators: []*models.OperatorCreateParams{
						{
							Name: newOperator,
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster := reply.GetPayload()

			getClusterReply, err := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(*cluster.ID))
			Expect(err).NotTo(HaveOccurred())

			cluster = getClusterReply.GetPayload()
			c := &common.Cluster{Cluster: *cluster}
			Expect(operators.IsEnabled(c.MonitoredOperators, newOperator)).Should(BeTrue())
		})
	})

	Context("Update cluster", func() {
		BeforeEach(func() {
			registerClusterReply, err := userBMClient.Installer.RegisterCluster(context.TODO(), &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster := registerClusterReply.GetPayload()
			clusterID = *cluster.ID
			log.Infof("Register cluster %s", cluster.ID.String())
		})

		It("Update OLMs", func() {
			By("First time - operators is empty", func() {
				_, err := userBMClient.Installer.UpdateCluster(context.TODO(), &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
							{Name: ocs.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ToNot(HaveOccurred())
				getReply, err2 := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(clusterID))
				Expect(err2).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, ocs.Operator.Name)).Should(BeTrue())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)}, models.Usage{Name: strings.ToUpper(ocs.Operator.Name)})
			})

			By("Second time - operators is not empty", func() {
				_, err := userBMClient.Installer.UpdateCluster(context.TODO(), &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ToNot(HaveOccurred())
				getReply, err := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(clusterID))
				Expect(err).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, ocs.Operator.Name)).Should(BeFalse())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)})
			})
		})

		It("Update OLMs", func() {
			By("First time - operators is empty", func() {
				_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
							{Name: ocs.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ToNot(HaveOccurred())
				getReply, err2 := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(clusterID))
				Expect(err2).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, ocs.Operator.Name)).Should(BeTrue())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)}, models.Usage{Name: strings.ToUpper(ocs.Operator.Name)})
			})

			By("Second time - operators is not empty", func() {
				_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ToNot(HaveOccurred())
				getReply, err := userBMClient.Installer.GetCluster(context.TODO(), installer.NewGetClusterParams().WithClusterID(clusterID))
				Expect(err).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, ocs.Operator.Name)).Should(BeFalse())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)})
			})
		})
	})

	Context("Monitored operators", func() {
		var cluster *installer.RegisterClusterCreated
		ctx := context.Background()
		BeforeEach(func() {
			var err error
			cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
					OlmOperators: []*models.OperatorCreateParams{
						{Name: ocs.Operator.Name},
						{Name: lso.Operator.Name},
					},
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
			Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
			Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
		})

		It("should be all returned", func() {
			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

			Expect(err).ToNot(HaveOccurred())
			Expect(len(ops.GetPayload())).To(BeNumerically(">=", 3))

			var operatorNames []string
			for _, op := range ops.GetPayload() {
				operatorNames = append(operatorNames, op.Name)
			}

			// Builtin
			for _, builtinOperator := range operators.NewManager(log, nil, operators.Options{}, nil, nil).GetSupportedOperatorsByType(models.OperatorTypeBuiltin) {
				Expect(operatorNames).To(ContainElements(builtinOperator.Name))
			}

			// OLM
			Expect(operatorNames).To(ContainElements(
				ocs.Operator.Name,
				lso.Operator.Name,
			))
		})

		It("should selected be returned", func() {
			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&ocs.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			Expect(ops.GetPayload()).To(HaveLen(1))
			Expect(ops.GetPayload()[0].Name).To(BeEquivalentTo(ocs.Operator.Name))
		})

		It("should be updated", func() {
			reportMonitoredOperatorStatus(ctx, agentBMClient, *cluster.Payload.ID, ocs.Operator.Name, models.OperatorStatusFailed)

			ops, err := agentBMClient.Operators.ListOfClusterOperators(ctx, opclient.NewListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&ocs.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			operators := ops.GetPayload()
			Expect(operators).To(HaveLen(1))
			Expect(operators[0].StatusInfo).To(BeEquivalentTo(string(models.OperatorStatusFailed)))
			Expect(operators[0].Status).To(BeEquivalentTo(models.OperatorStatusFailed))

		})
	})

	Context("Installation", func() {
		BeforeEach(func() {
			cID, err := registerCluster(context.TODO(), userBMClient, "test-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			clusterID = cID
			// in order to simulate infra env generation
			generateClusterISO(clusterID, models.ImageTypeMinimalIso)
			registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
		})

		It("All OLM operators available", func() {
			By("Update OLM", func() {
				_, err := userBMClient.Installer.UpdateCluster(context.TODO(), &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())
			})

			By("Report operator available", func() {
				reportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusAvailable)
			})

			By("Wait for cluster to be installed", func() {
				setClusterAsFinalizing(context.TODO(), clusterID)
				completeInstallationAndVerify(context.TODO(), agentBMClient, clusterID, true)
			})
		})

		It("Failed OLM Operator", func() {
			By("Update OLM", func() {
				_, err := userBMClient.Installer.UpdateCluster(context.TODO(), &installer.UpdateClusterParams{
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())
			})

			By("Report operator failed", func() {
				reportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusFailed)
			})

			By("Wait for cluster to be degraded", func() {
				setClusterAsFinalizing(context.TODO(), clusterID)
				completeInstallation(agentBMClient, clusterID)
				expectedStatusInfo := fmt.Sprintf("%s. Failed OLM operators: %s", cluster.StatusInfoDegraded, lso.Operator.Name)
				waitForClusterState(context.TODO(), clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, expectedStatusInfo)
			})
		})
	})

	Context("[V2ClusterUpdate] Installation", func() {
		BeforeEach(func() {
			cID, err := registerCluster(context.TODO(), userBMClient, "test-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			clusterID = cID
			// in order to simulate infra env generation
			generateClusterISO(clusterID, models.ImageTypeMinimalIso)
			registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
		})

		It("All OLM operators available", func() {
			By("Update OLM", func() {
				_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())
			})

			By("Report operator available", func() {
				reportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusAvailable)
			})

			By("Wait for cluster to be installed", func() {
				setClusterAsFinalizing(context.TODO(), clusterID)
				completeInstallationAndVerify(context.TODO(), agentBMClient, clusterID, true)
			})
		})

		It("Failed OLM Operator", func() {
			By("Update OLM", func() {
				_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ShouldNot(HaveOccurred())
			})

			By("Report operator failed", func() {
				reportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusFailed)
			})

			By("Wait for cluster to be degraded", func() {
				setClusterAsFinalizing(context.TODO(), clusterID)
				completeInstallation(agentBMClient, clusterID)
				expectedStatusInfo := fmt.Sprintf("%s. Failed OLM operators: %s", cluster.StatusInfoDegraded, lso.Operator.Name)
				waitForClusterState(context.TODO(), clusterID, models.ClusterStatusInstalled, defaultWaitForClusterStateTimeout, expectedStatusInfo)
			})
		})
	})
})
