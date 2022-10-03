package subsystem

import (
	"context"
	"encoding/json"
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
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Operators endpoint tests", func() {

	var (
		clusterID strfmt.UUID
	)

	Context("supported-operators", func() {
		It("should return all supported operators", func() {
			reply, err := userBMClient.Operators.V2ListSupportedOperators(context.TODO(), opclient.NewV2ListSupportedOperatorsParams())

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.GetPayload()).To(ConsistOf(odf.Operator.Name, lso.Operator.Name, cnv.Operator.Name, lvm.Operator.Name))
		})

		It("should provide operator properties", func() {
			params := opclient.NewV2ListOperatorPropertiesParams().WithOperatorName(odf.Operator.Name)
			reply, err := userBMClient.Operators.V2ListOperatorProperties(context.TODO(), params)

			Expect(err).ToNot(HaveOccurred())
			Expect(reply.Payload).To(BeEquivalentTo(models.OperatorProperties{}))
		})
	})

	Context("Create cluster", func() {
		It("Have builtins", func() {
			reply, err := userBMClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
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
			newOperator := odf.Operator.Name

			reply, err := userBMClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
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

			getClusterReply, err := userBMClient.Installer.V2GetCluster(context.TODO(), installer.NewV2GetClusterParams().WithClusterID(*cluster.ID))
			Expect(err).NotTo(HaveOccurred())

			cluster = getClusterReply.GetPayload()
			c := &common.Cluster{Cluster: *cluster}
			Expect(operators.IsEnabled(c.MonitoredOperators, newOperator)).Should(BeTrue())
		})
	})

	Context("Update cluster", func() {
		BeforeEach(func() {
			clusterCIDR := "10.128.0.0/14"
			serviceCIDR := "172.30.0.0/16"
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:     "example.com",
					ClusterNetworks:   []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:   []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:              swag.String("test-cluster"),
					OpenshiftVersion:  swag.String(openshiftVersion),
					PullSecret:        swag.String(pullSecret),
					SSHPublicKey:      sshPublicKey,
					VipDhcpAllocation: swag.Bool(true),
					NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster := registerClusterReply.GetPayload()
			clusterID = *cluster.ID
			log.Infof("Register cluster %s", cluster.ID.String())
		})

		It("Update OLMs", func() {
			By("First time - operators is empty", func() {
				_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: lso.Operator.Name},
							{Name: odf.Operator.Name},
						},
					},
					ClusterID: clusterID,
				})
				Expect(err).ToNot(HaveOccurred())
				getReply, err2 := userBMClient.Installer.V2GetCluster(context.TODO(), installer.NewV2GetClusterParams().WithClusterID(clusterID))
				Expect(err2).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, odf.Operator.Name)).Should(BeTrue())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)}, models.Usage{Name: strings.ToUpper(odf.Operator.Name)})
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
				getReply, err := userBMClient.Installer.V2GetCluster(context.TODO(), installer.NewV2GetClusterParams().WithClusterID(clusterID))
				Expect(err).ToNot(HaveOccurred())
				c := &common.Cluster{Cluster: *getReply.Payload}

				Expect(operators.IsEnabled(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operators.IsEnabled(c.MonitoredOperators, odf.Operator.Name)).Should(BeFalse())
				verifyUsageSet(c.FeatureUsage, models.Usage{Name: strings.ToUpper(lso.Operator.Name)})
			})
		})

		It("Updated OLM validation failure reflected in the cluster", func() {
			updateCpuCores := func(h *models.Host, cpucores int64) {
				hInventory := models.Inventory{}
				_ = json.Unmarshal([]byte(h.Inventory), &hInventory)
				hInventory.CPU = &models.CPU{Count: cpucores}
				generateEssentialHostStepsWithInventory(context.TODO(), h, h.RequestedHostname, &hInventory)
			}
			By("add hosts with a minimal worker (cnv operator is not enabled)")
			infraEnvID := registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
			hosts := registerHostsAndSetRolesDHCP(clusterID, *infraEnvID, 6, "test-cluster", "example.com")

			worker := getHostV2(*infraEnvID, *hosts[5].ID)
			updateCpuCores(worker, 2)
			for _, h := range hosts {
				By(fmt.Sprintf("waiting for host %s to be ready", h.RequestedHostname))
				waitForHostState(context.TODO(), models.HostStatusKnown, defaultWaitForHostStateTimeout, h)
			}
			By("waiting for the cluster to be ready")
			waitForClusterState(context.TODO(), clusterID, models.ClusterStatusReady, defaultWaitForClusterStateTimeout,
				IgnoreStateInfo)

			By("enable CNV operator")
			_, err := userBMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					OlmOperators: []*models.OperatorCreateParams{
						{Name: cnv.Operator.Name},
					},
				},
				ClusterID: clusterID,
			})
			Expect(err).ToNot(HaveOccurred())

			By("check that the cluster move to insufficient immediately")
			c := getCluster(clusterID)
			Expect(*c.Status).To(Equal(models.ClusterStatusInsufficient))
		})
	})

	Context("Monitored operators", func() {
		var cluster *installer.V2RegisterClusterCreated
		ctx := context.Background()
		BeforeEach(func() {
			var err error
			cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("test-cluster"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(pullSecret),
					OlmOperators: []*models.OperatorCreateParams{
						{Name: odf.Operator.Name},
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
			ops, err := agentBMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

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
				odf.Operator.Name,
				lso.Operator.Name,
			))
		})

		It("should selected be returned", func() {
			ops, err := agentBMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&odf.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			Expect(ops.GetPayload()).To(HaveLen(1))
			Expect(ops.GetPayload()[0].Name).To(BeEquivalentTo(odf.Operator.Name))
		})

		It("should be updated", func() {
			v2ReportMonitoredOperatorStatus(ctx, agentBMClient, *cluster.Payload.ID, odf.Operator.Name, models.OperatorStatusFailed)

			ops, err := agentBMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&odf.Operator.Name))

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
			infraEnvID := registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
			registerHostsAndSetRoles(clusterID, *infraEnvID, minHosts, "test-cluster", "example.com")
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
				v2ReportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusAvailable)
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
				v2ReportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusFailed)
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
