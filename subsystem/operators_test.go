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
	operatorscommon "github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/operators/mce"
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
			Expect(reply.GetPayload()).To(ConsistOf(odf.Operator.Name, lso.Operator.Name, cnv.Operator.Name, lvm.Operator.Name, mce.Operator.Name))
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
				Expect(operatorscommon.HasOperator(c.MonitoredOperators, builtinOperator.Name)).Should(BeTrue())
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
			Expect(operatorscommon.HasOperator(c.MonitoredOperators, newOperator)).Should(BeTrue())
		})
	})

	Context("Update cluster", func() {
		var cluster *models.Cluster

		BeforeEach(func() {
			clusterCIDR := "10.128.0.0/14"
			serviceCIDR := "172.30.0.0/16"
			registerClusterReply, err := userBMClient.Installer.V2RegisterCluster(context.TODO(), &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					BaseDNSDomain:     "example.com",
					ClusterNetworks:   []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:   []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					Name:              swag.String("test-cluster"),
					OpenshiftVersion:  swag.String(VipAutoAllocOpenshiftVersion),
					PullSecret:        swag.String(pullSecret),
					SSHPublicKey:      sshPublicKey,
					VipDhcpAllocation: swag.Bool(true),
					NetworkType:       swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			cluster = registerClusterReply.GetPayload()
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

				Expect(operatorscommon.HasOperator(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operatorscommon.HasOperator(c.MonitoredOperators, odf.Operator.Name)).Should(BeTrue())
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

				Expect(operatorscommon.HasOperator(c.MonitoredOperators, lso.Operator.Name)).Should(BeTrue())
				Expect(operatorscommon.HasOperator(c.MonitoredOperators, odf.Operator.Name)).Should(BeFalse())
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
			infraEnvID := registerInfraEnvSpecificVersion(&clusterID, models.ImageTypeMinimalIso, cluster.OpenshiftVersion).ID
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

	Context("OLM operators", func() {
		ctx := context.Background()
		registerNewCluster := func(openshiftVersion string, highAvailabilityMode string, operators []*models.OperatorCreateParams, cpuArchitecture *string, vipDhcpAllocation *bool) *installer.V2RegisterClusterCreated {
			var err error
			var cluster *installer.V2RegisterClusterCreated
			clusterCIDR := "10.128.0.0/14"
			serviceCIDR := "172.30.0.0/16"

			if vipDhcpAllocation == nil {
				vipDhcpAllocation = swag.Bool(true)
				if highAvailabilityMode == models.ClusterHighAvailabilityModeNone {
					vipDhcpAllocation = swag.Bool(false)
				}
			}

			cluster, err = user2BMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("test-cluster"),
					OpenshiftVersion:     swag.String(openshiftVersion),
					HighAvailabilityMode: swag.String(highAvailabilityMode),
					PullSecret:           swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					CPUArchitecture:      swag.StringValue(cpuArchitecture),
					OlmOperators:         operators,
					BaseDNSDomain:        "example.com",
					ClusterNetworks:      []*models.ClusterNetwork{{Cidr: models.Subnet(clusterCIDR), HostPrefix: 23}},
					ServiceNetworks:      []*models.ServiceNetwork{{Cidr: models.Subnet(serviceCIDR)}},
					SSHPublicKey:         sshPublicKey,
					VipDhcpAllocation:    vipDhcpAllocation,
					NetworkType:          swag.String(models.ClusterNetworkTypeOVNKubernetes),
				},
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(cluster.GetPayload().Status)).Should(Equal("insufficient"))
			Expect(swag.StringValue(cluster.GetPayload().StatusInfo)).Should(Equal(clusterInsufficientStateInfo))
			Expect(cluster.GetPayload().StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))
			return cluster
		}

		It("LSO as ODF dependency on Z CPU architecture", func() {
			// Register cluster with ppc64le CPU architecture
			cluster := registerNewCluster(
				"4.13.0",
				models.ClusterHighAvailabilityModeFull,
				nil,
				swag.String(models.ClusterCPUArchitectureS390x),
				swag.Bool(false),
			)
			Expect(cluster.Payload.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureS390x))
			Expect(len(cluster.Payload.MonitoredOperators)).To(Equal(1))

			// Register infra-env with ppc64le CPU architecture
			infraEnvParams := installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("infra-env-1"),
					OpenshiftVersion: "4.13.0",
					ClusterID:        cluster.Payload.ID,
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					SSHAuthorizedKey: swag.String(sshPublicKey),
					CPUArchitecture:  models.ClusterCPUArchitectureS390x,
				},
			}
			infraEnv, err := user2BMClient.Installer.RegisterInfraEnv(ctx, &infraEnvParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv.Payload.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureS390x))

			ops, err := agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))
			Expect(err).ToNot(HaveOccurred())
			Expect(len(ops.GetPayload())).To(BeNumerically("==", 1))

			// Update cluster with ODF operator
			_, err = user2BMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					OlmOperators: []*models.OperatorCreateParams{
						{Name: odf.Operator.Name},
					},
				},
				ClusterID: *cluster.Payload.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())

			ops, err = agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))
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

			// Verify that the cluster is updatable
			_, err = user2BMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					Name: swag.String("new-cluster-name"),
				},
				ClusterID: *cluster.Payload.ID,
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("LSO as ODF dependency on ARM arch", func() {
			cluster := registerNewCluster(
				"4.13-multi",
				models.ClusterHighAvailabilityModeFull,
				nil,
				swag.String(models.ClusterCPUArchitectureArm64),
				nil,
			)
			Expect(cluster.Payload.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureArm64))
			Expect(len(cluster.Payload.MonitoredOperators)).To(Equal(1))

			infraEnvParams := installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("infra-env-1"),
					OpenshiftVersion: "4.13.0",
					ClusterID:        cluster.Payload.ID,
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					SSHAuthorizedKey: swag.String(sshPublicKey),
					CPUArchitecture:  models.ClusterCPUArchitectureArm64,
				},
			}
			infraEnv, err := user2BMClient.Installer.RegisterInfraEnv(ctx, &infraEnvParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(infraEnv.Payload.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureArm64))

			ops, err := agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))
			Expect(err).ToNot(HaveOccurred())
			Expect(len(ops.GetPayload())).To(BeNumerically("==", 1))

			// Update cluster with ODF operator
			_, err = user2BMClient.Installer.V2UpdateCluster(context.TODO(), &installer.V2UpdateClusterParams{
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					OlmOperators: []*models.OperatorCreateParams{
						{Name: lso.Operator.Name},
					},
				},
				ClusterID: *cluster.Payload.ID,
			})
			reason := err.(*installer.V2UpdateClusterBadRequest).Payload.Reason
			Expect(*reason).To(ContainSubstring("cannot use Local Storage Operator because it's not compatible with the arm64 architecture on version 4.13"))
		})

		It("should lvm installed as cnv dependency", func() {
			cluster := registerNewCluster(
				"4.12.0",
				models.ClusterHighAvailabilityModeNone,
				[]*models.OperatorCreateParams{{Name: cnv.Operator.Name}},
				nil,
				nil,
			)
			ops, err := agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

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
				cnv.Operator.Name,
				lvm.Operator.Name,
			))
		})

		It("should lvm have right subscription name on 4.12", func() {
			cluster := registerNewCluster(
				"4.12.0",
				models.ClusterHighAvailabilityModeNone,
				[]*models.OperatorCreateParams{{Name: cnv.Operator.Name}},
				nil,
				nil,
			)
			ops, err := agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

			Expect(err).ToNot(HaveOccurred())

			var operatorSubscriptionName string
			for _, op := range ops.GetPayload() {
				if op.Name == "lvm" {
					operatorSubscriptionName = op.SubscriptionName
					break
				}
			}

			Expect(operatorSubscriptionName).To(Equal(lvm.LvmsSubscriptionName))
		})

		It("should lvm have right subscription name on 4.11", func() {
			cluster := registerNewCluster(
				"4.11",
				models.ClusterHighAvailabilityModeNone,
				[]*models.OperatorCreateParams{{Name: lvm.Operator.Name}},
				nil,
				nil,
			)
			ops, err := agent2BMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().WithClusterID(*cluster.Payload.ID))

			Expect(err).ToNot(HaveOccurred())

			var operatorSubscriptionName string
			for _, op := range ops.GetPayload() {
				if op.Name == "lvm" {
					operatorSubscriptionName = op.SubscriptionName
					break
				}
			}

			Expect(operatorSubscriptionName).To(Equal(lvm.LvmoSubscriptionName))
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
			v2ReportMonitoredOperatorStatus(ctx, agentBMClient, *cluster.Payload.ID, odf.Operator.Name, models.OperatorStatusFailed, "4.12")

			ops, err := agentBMClient.Operators.V2ListOfClusterOperators(ctx, opclient.NewV2ListOfClusterOperatorsParams().
				WithClusterID(*cluster.Payload.ID).
				WithOperatorName(&odf.Operator.Name))

			Expect(err).ToNot(HaveOccurred())
			operators := ops.GetPayload()
			Expect(operators).To(HaveLen(1))
			Expect(operators[0].StatusInfo).To(BeEquivalentTo(string(models.OperatorStatusFailed)))
			Expect(operators[0].Status).To(BeEquivalentTo(models.OperatorStatusFailed))
			Expect(operators[0].Version).To(BeEquivalentTo("4.12"))
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
				v2ReportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusAvailable, "")
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
				v2ReportMonitoredOperatorStatus(context.TODO(), agentBMClient, clusterID, lso.Operator.Name, models.OperatorStatusFailed, "")
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
