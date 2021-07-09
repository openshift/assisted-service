package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang/mock/gomock"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var BASIC_KUBECONFIG = `test`

var _ = Describe("bmac reconcile", func() {
	var (
		c        client.Client
		bmhr     *BMACReconciler
		ctx      = context.Background()
		mockCtrl *gomock.Controller
	)

	BeforeEach(func() {
		schemes := GetKubeClientSchemes()
		c = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		bmhr = &BMACReconciler{
			Client:      c,
			Scheme:      scheme.Scheme,
			Log:         common.GetTestLog(),
			spokeClient: fakeclient.NewClientBuilder().WithScheme(schemes).Build(),
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("bmh reconcile: no labels", func() {
		host := newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())

		result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	Describe("queue bmh request for agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var infraEnv *v1beta1.InfraEnv

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1beta1.AgentSpec{})
			agent.Status.Inventory = v1beta1.HostInventory{
				ReportTime: &metav1.Time{Time: time.Now()},
				Memory: v1beta1.HostMemory{
					PhysicalBytes: 2,
				},
				Interfaces: []v1beta1.HostInterface{
					{
						Name: "eth0",
						IPV4Addresses: []string{
							"1.2.3.4",
						},
						IPV6Addresses: []string{
							"1001:db8::10/120",
						},
						MacAddress: macStr,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			infraEnv = newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{BootMACAddress: macStr})
			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = "testInfraEnv"
			host.ObjectMeta.Labels = labels
			host.Status.Provisioning.State = bmh_v1alpha1.StateReady
			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
		})

		Context("findBMHByAgent, when both agent and bmh exist,", func() {
			It("should return the agent if their MAC address matches", func() {
				result, err := bmhr.findBMHByAgent(context.Background(), agent)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(host))
			})

			It("should return nil if there is no match", func() {
				agent = newAgent("bmac-agent-no-MAC", testNamespace, v1beta1.AgentSpec{})
				Expect(c.Create(ctx, agent)).To(BeNil())

				result, err := bmhr.findBMHByAgent(context.Background(), agent)
				Expect(err).To(BeNil())
				Expect(result).To(BeNil())
			})
		})

		Context("findBMHByInfraEnv, when both InfraEnv and BMH exist", func() {
			It("should return the bmh if the lable matches the InfraEnv name", func() {
				result, err := bmhr.findBMHByInfraEnv(context.Background(), infraEnv)
				Expect(err).To(BeNil())
				Expect(result).NotTo(BeEmpty())
				Expect(result).To(ContainElement(host))
			})

			It("should return nil if there is no match", func() {
				noMatchEnv := newInfraEnvImage("no-reference-pointing-at-me", testNamespace, v1beta1.InfraEnvSpec{})
				Expect(c.Create(ctx, noMatchEnv)).To(BeNil())

				result, err := bmhr.findBMHByInfraEnv(context.Background(), noMatchEnv)
				Expect(err).To(BeNil())
				Expect(result).To(BeEmpty())
			})
		})
	})

	Describe("queue bmh request for cluster deployment", func() {
		var bmh *bmh_v1alpha1.BareMetalHost
		var agentCluster1 *v1beta1.Agent
		var agentCluster2 *v1beta1.Agent
		var clusterDeployment1 *hivev1.ClusterDeployment
		var clusterDeployment2 *hivev1.ClusterDeployment
		cluster1Name := "test-cluster"
		cluster2Name := "test-cluster2"
		macStr := "12-34-56-78-9A-BC"
		bmhName := "bmh-reconcile"

		BeforeEach(func() {
			creationTime1, err := time.Parse(time.RFC3339, "2021-05-04T00:00:00.000Z")
			Expect(err).To(Succeed())
			agentCluster1 = newAgentWithClusterReference("bmac-agent", testNamespace, "1.2.3.4", "1001:db8::10/120", macStr, cluster1Name, bmhName, creationTime1)
			Expect(c.Create(ctx, agentCluster1)).To(Succeed())

			agentCluster2 = newAgentWithClusterReference("bmac-agent2", testNamespace, "1.2.3.6", "1001:db8::11/120", "12-34-56-78-9A-BD", cluster2Name, bmhName, creationTime1)
			Expect(c.Create(ctx, agentCluster2)).To(Succeed())

			pullSecretName := "pull-secret"
			defaultClusterSpec1 := getDefaultClusterDeploymentSpec(cluster1Name, "test-cluster-aci", pullSecretName)
			clusterDeployment1 = newClusterDeployment(cluster1Name, testNamespace, defaultClusterSpec1)
			Expect(c.Create(ctx, clusterDeployment1)).To(Succeed())
			defaultClusterSpec2 := getDefaultClusterDeploymentSpec(cluster2Name, "test-cluster-aci", pullSecretName)
			clusterDeployment2 = newClusterDeployment(cluster2Name, testNamespace, defaultClusterSpec2)
			Expect(c.Create(ctx, clusterDeployment2)).To(Succeed())

			bmh = newBMH(bmhName, &bmh_v1alpha1.BareMetalHostSpec{BootMACAddress: macStr})
			Expect(c.Create(ctx, bmh)).To(Succeed())
		})

		Context("findAgentsByClusterDeployment, when both cluster deployment and agents exist", func() {
			It("should return the agent matching cluster deployment name", func() {
				agents := bmhr.findAgentsByClusterDeployment(context.Background(), clusterDeployment1)
				Expect(len(agents)).To(Equal(1))
				Expect(agents[0].ObjectMeta.Name).To(Equal(agentCluster1.ObjectMeta.Name))

				agents = bmhr.findAgentsByClusterDeployment(context.Background(), clusterDeployment2)
				Expect(len(agents)).To(Equal(1))
				Expect(agents[0].ObjectMeta.Name).To(Equal(agentCluster2.ObjectMeta.Name))
			})

			It("should return the a single agent if there are multiple agents with same BMH name", func() {
				// newerAgent is same as agentCluster1 but with newer CreationTimestamp
				creationTime, err := time.Parse(time.RFC3339, "2021-05-05T01:00:00.000Z")
				Expect(err).To(Succeed())
				newerAgent := newAgentWithClusterReference("bmac-agent-newer", testNamespace, "1.2.3.4", "1001:db8::10/120", macStr, cluster1Name, bmhName, creationTime)
				Expect(c.Create(ctx, newerAgent)).To(Succeed())

				agents := bmhr.findAgentsByClusterDeployment(context.Background(), clusterDeployment1)
				Expect(len(agents)).To(Equal(1))
				Expect(agents[0].ObjectMeta.Name).To(Equal(newerAgent.ObjectMeta.Name))
			})

			It("should return nothing if agent does not match cluster deployment name", func() {
				clusterName := "not-matching-agents-cluster-name"
				defaultClusterSpec := getDefaultClusterDeploymentSpec(clusterName, "test-cluster-aci", "test-pull")
				clusterDeploymentNotMatching := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
				agents := bmhr.findAgentsByClusterDeployment(context.Background(), clusterDeploymentNotMatching)
				Expect(len(agents)).To(Equal(0))
			})
		})

		Context("agentToBMHReconcileRequests", func() {
			It("should return the BMH reconcile request if agent mac addresses matches bmh", func() {
				BMHReconcileRequests := bmhr.agentToBMHReconcileRequests(context.Background(), agentCluster1)
				Expect(len(BMHReconcileRequests)).To(Equal(1))
				Expect(BMHReconcileRequests[0].Name).To(Equal(bmh.ObjectMeta.Name))
				Expect(BMHReconcileRequests[0].Namespace).To(Equal(bmh.ObjectMeta.Namespace))
			})

			It("should not return the BMH reconcile request if agent mac addresses does not match bmh", func() {
				creationTime, err := time.Parse(time.RFC3339, "2021-05-05T01:00:00.000Z")
				Expect(err).To(Succeed())
				agentNoMatch := newAgentWithClusterReference("bmac-agent-no-match", "no-match", "no-match", "no-match", "no-match", "no-match", "", creationTime)
				BMHReconcileRequests := bmhr.agentToBMHReconcileRequests(context.Background(), agentNoMatch)
				Expect(len(BMHReconcileRequests)).To(Equal(0))
			})
		})
	})

	Describe("Reconcile a BMH with an infraEnv label", func() {
		var host *bmh_v1alpha1.BareMetalHost
		BeforeEach(func() {
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = "testInfraEnv"
			host.ObjectMeta.Labels = labels
			host.Status.Provisioning.State = bmh_v1alpha1.StateReady
			Expect(c.Create(ctx, host)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
		})

		Context("with a non-existing infraEnv", func() {
			It("should return without failures", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("with an existing infraEnv without ISODownloadURL", func() {
			It("should requeue the reconcile", func() {
				infraEnv := newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
				Expect(c.Create(ctx, infraEnv)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("with an existing infraEnv with ISODownloadURL", func() {
			var infraEnv *v1beta1.InfraEnv
			var isoImageURL string
			var isoTimestamp metav1.Time

			BeforeEach(func() {
				isoImageURL = "http://buzz.lightyear.io/discovery-image.iso"
				isoTimestamp = metav1.Time{Time: time.Now().Add(-10 * time.Hour)}
				infraEnv = newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
				infraEnv.Status = v1beta1.InfraEnvStatus{
					ISODownloadURL: isoImageURL,
					CreatedTime:    &isoTimestamp,
				}
				Expect(c.Create(ctx, infraEnv)).To(BeNil())
			})

			AfterEach(func() {
				Expect(c.Delete(ctx, infraEnv)).ShouldNot(HaveOccurred())
			})

			It("should disable the BMH hardware inspection", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))

			})

			It("should disable the BMH hardware inspection regardless of the bmh state", func() {
				// Removing URL so that the Reconcile flow
				// stops early.
				infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: ""}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				// Test that only inspection missing will set both parameters
				host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
				host.Status.Provisioning.State = bmh_v1alpha1.StateRegistering

				result := bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(Equal(reconcileComplete{dirty: true, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))

				// Test that only cleaning != disabled will set both parameters
				host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeMetadata
				host.Status.Provisioning.State = bmh_v1alpha1.StateProvisioned

				result = bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(Equal(reconcileComplete{dirty: true, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))

				// This should not return a dirty result because label is already set
				result = bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(Equal(reconcileComplete{dirty: false, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))
			})

			It("should set the ISODownloadURL in the BMH", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.Spec.Image.URL).To(Equal(isoImageURL))
			})

			It("should disable cleaning and set online true in the BMH", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.Spec.Online).To(Equal(true))
				Expect(updatedHost.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))
			})
			It("should not reconcile BMH if the updated image has not been around longer than the grace period", func() {
				// Reconcile with the original ISO
				_ = bmhr.reconcileBMH(ctx, bmhr.Log, host)

				// Generate a new ISO with the current timestamp
				infraEnv.Status = v1beta1.InfraEnvStatus{
					ISODownloadURL: isoImageURL + ".new",
					CreatedTime:    &metav1.Time{Time: time.Now()},
				}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				// Should not reconcile because ISO is too recent.
				// We expect the old URL to be still attached to the BMH.
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(BeAssignableToTypeOf(reconcileRequeue{}))
				Expect(host.Spec.Image.URL).To(Equal(isoImageURL))
			})
			It("should not reconcile BMH if the initial image has not been around longer than the grace period", func() {
				// Generate a new ISO with the current timestamp
				infraEnv.Status = v1beta1.InfraEnvStatus{
					ISODownloadURL: isoImageURL,
					CreatedTime:    &metav1.Time{Time: time.Now()},
				}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				// There was no previous ISO attached, so the BMH should not have any URL.
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(BeAssignableToTypeOf(reconcileRequeue{}))
				Expect(host.Spec.Image).To(BeNil())
			})
			It("should reconcile BMH if the image timestamp is old", func() {
				infraEnv.Status = v1beta1.InfraEnvStatus{
					ISODownloadURL: isoImageURL + ".new",
					CreatedTime:    &metav1.Time{Time: time.Now().Add(-10 * time.Hour)},
				}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				// The ISO is old enough to pass through the filter, thus we expect the new
				// URL to be attached to the BMH.
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host)
				Expect(result).To(Equal(reconcileComplete{dirty: true, stop: true}))
				Expect(host.Spec.Image.URL).To(Equal(isoImageURL + ".new"))
			})
		})
	})

	Describe("Reconcile a BMH with a non-approved matching agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var staleAgent *v1beta1.Agent

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			staleAgent = newAgent("stale-bmac-agent", testNamespace, v1beta1.AgentSpec{})
			staleAgent.ObjectMeta.CreationTimestamp.Time = time.Now()
			staleAgent.Status.Inventory = v1beta1.HostInventory{
				Interfaces: []v1beta1.HostInterface{
					{
						Name:       "eth0",
						MacAddress: macStr,
					},
				},
			}
			Expect(c.Create(ctx, staleAgent)).To(BeNil())

			agent = newAgent("bmac-agent", testNamespace, v1beta1.AgentSpec{})
			agent.ObjectMeta.CreationTimestamp.Time = time.Now().Add(time.Minute)
			agent.Status.Inventory = v1beta1.HostInventory{
				Interfaces: []v1beta1.HostInterface{
					{
						Name:       "eth0",
						MacAddress: macStr,
					},
				},
				Disks: []v1beta1.HostDisk{
					{
						ID:                      "1",
						InstallationEligibility: v1beta1.HostInstallationEligibility{Eligible: true},
						Path:                    "/dev/sda",
						DriveType:               "SSD",
						Bootable:                true,
					},
					{
						ID:                      "2",
						InstallationEligibility: v1beta1.HostInstallationEligibility{Eligible: true},
						Path:                    "/dev/sdb",
						DriveType:               "SSD",
						Bootable:                true,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: "http://buzz.lightyear.io/discovery-image.iso"}
			hostSpec := bmh_v1alpha1.BareMetalHostSpec{
				Image: image,
				RootDeviceHints: &bmh_v1alpha1.RootDeviceHints{
					DeviceName: "/dev/sda",
					Rotational: new(bool),
				},
				BootMACAddress: macStr,
			}
			host = newBMH("bmh-reconcile", &hostSpec)
			annotations := make(map[string]string)
			annotations[BMH_AGENT_ROLE] = "master"
			annotations[BMH_AGENT_HOSTNAME] = "happy-meal"
			annotations[BMH_AGENT_MACHINE_CONFIG_POOL] = "number-8"
			annotations[BMH_AGENT_INSTALLER_ARGS] = `["--args", "aaaa"]`
			annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = "agent-ignition"
			host.ObjectMeta.SetAnnotations(annotations)
			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, staleAgent)).ShouldNot(HaveOccurred())
		})

		Context("when an agent matches", func() {
			It("should use the newest agent", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				// Check that staleAgent is *NOT* approved, since it's stale and old!
				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: staleAgent.Name, Namespace: staleAgent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.Approved).To(Equal(false))
			})

			It("should not fail on missing role", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.ObjectMeta.Annotations[BMH_AGENT_ROLE] = "without-purpose"
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(updatedHost))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(string(updatedAgent.Spec.Role)).To(Equal("without-purpose"))
			})

			It("should approve it", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.Approved).To(Equal(true))
			})

			It("should add a lable referring to the bmh", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.ObjectMeta.Labels[AGENT_BMH_LABEL]).To(Equal(host.Name))
			})

			It("should set the agent spec based on the BMH annotations", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.Role).To(Equal(models.HostRoleMaster))
				Expect(updatedAgent.Spec.Hostname).To(Equal("happy-meal"))
				Expect(updatedAgent.Spec.MachineConfigPool).To(Equal("number-8"))
				Expect(updatedAgent.Spec.InstallerArgs).To(Equal(`["--args", "aaaa"]`))
				Expect(updatedAgent.Spec.IgnitionConfigOverrides).To(Equal("agent-ignition"))
			})

			It("should keep InstallationDiskID as empty string if not RootDeviceHints match", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.DeviceName = "/dev/sdc"
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal(""))
			})

			It("should set the InstallationDiskID if the RootDeviceHints were provided and match", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("1"))
			})
		})
	})

	Describe("Reconcile a BMH with an approved matching agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1beta1.AgentSpec{Approved: true})
			agent.Status.Inventory = v1beta1.HostInventory{
				Memory: v1beta1.HostMemory{
					PhysicalBytes: 2,
				},
				Interfaces: []v1beta1.HostInterface{
					{
						Name: "eth0",
						IPV4Addresses: []string{
							"1.2.3.4",
						},
						IPV6Addresses: []string{
							"1001:db8::10/120",
						},
						MacAddress: macStr,
					},
				},
				Disks: []v1beta1.HostDisk{
					{Path: "/dev/sda", Bootable: true},
					{Path: "/dev/sdb", Bootable: false},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: "http://buzz.lightyear.io/discovery-image.iso"}
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr})
			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
		})

		Context("when an agent matches", func() {
			It("should set the metal3 hardwaredetails annotation if the Agent inventory is set", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))

				var hDetails bmh_v1alpha1.HardwareDetails
				err = json.Unmarshal([]byte(updatedHost.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]), &hDetails)
				Expect(err).To(BeNil())
				Expect(hDetails.NIC).To(HaveLen(2))
				Expect(hDetails.Storage).To(HaveLen(2))
				Expect(hDetails.RAMMebibytes).To(BeEquivalentTo(agent.Status.Inventory.Memory.PhysicalBytes / (1024 * 1024)))
				Expect(hDetails.CPU.Arch).To(Equal(agent.Status.Inventory.Cpu.Architecture))
				Expect(hDetails.CPU.Model).To(Equal(agent.Status.Inventory.Cpu.ModelName))
			})
		})
	})

	Describe("Reconcile a Spoke BMH", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var cluster *hivev1.ClusterDeployment
		var secret *corev1.Secret

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1beta1.AgentSpec{Approved: true})
			agent.Status.Inventory = v1beta1.HostInventory{
				Memory: v1beta1.HostMemory{
					PhysicalBytes: 2,
				},
				Interfaces: []v1beta1.HostInterface{
					{
						Name: "eth0",
						IPV4Addresses: []string{
							"1.2.3.4",
						},
						IPV6Addresses: []string{
							"1001:db8::10/120",
						},
						MacAddress: macStr,
					},
				},
				Disks: []v1beta1.HostDisk{
					{Path: "/dev/sda", Bootable: true},
					{Path: "/dev/sdb", Bootable: false},
				},
			}
			clusterName := "test-cluster"
			pullSecretName := "pull-secret"

			agent.Spec.Role = models.HostRoleWorker
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{Name: clusterName, Namespace: testNamespace}
			Expect(c.Create(ctx, agent)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: "http://buzz.lightyear.io/discovery-image.iso"}
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr, BMC: bmh_v1alpha1.BMCDetails{CredentialsName: fmt.Sprintf(adminKubeConfigStringTemplate, clusterName)}})
			Expect(c.Create(ctx, host)).To(BeNil())

			defaultClusterSpec := getDefaultClusterDeploymentSpec(clusterName, "test-cluster-aci", pullSecretName)
			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			cluster.Spec.Installed = true
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			secretName := fmt.Sprintf(adminKubeConfigStringTemplate, clusterName)
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: cluster.Namespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte(BASIC_KUBECONFIG),
				},
			}
			Expect(c.Create(ctx, secret)).ShouldNot(HaveOccurred())

			spokeMachine := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-bmh-reconcile",
					Namespace: "test-namespace",
					Labels: map[string]string{
						machinev1beta1.MachineClusterIDLabel: clusterName,
						MACHINE_ROLE:                         string(models.HostRoleWorker),
						MACHINE_TYPE:                         string(models.HostRoleWorker),
					},
				},
			}
			Expect(c.Create(ctx, spokeMachine)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		Context("when agent role worker and cluster deployment is set", func() {
			It("should set spoke BMH", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))

				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, spokeBMH)
				Expect(err).To(BeNil())
				Expect(spokeBMH.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]).To(Equal(updatedHost.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]))
				Expect(spokeBMH.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(spokeBMH.Spec.Image).To(Equal(updatedHost.Spec.Image))

				spokeMachine := &machinev1beta1.Machine{}
				machineName := fmt.Sprintf("%s-%s", cluster.Name, spokeBMH.Name)
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: testNamespace}, spokeMachine)
				Expect(err).To(BeNil())
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(machinev1beta1.MachineClusterIDLabel))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_ROLE))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_TYPE))

				spokeSecret := &corev1.Secret{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: testNamespace}, spokeSecret)
				Expect(err).To(BeNil())
				Expect(spokeSecret.Data).To(Equal(secret.Data))
			})
		})
	})

	Describe("Add detached annotation to a BMH if Agent installation is progressing or has completed", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var infraEnv *v1beta1.InfraEnv
		var isoImageURL string

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"

			agentSpec := v1beta1.AgentSpec{
				Approved: true,
			}
			agent = newAgent("bmac-agent", testNamespace, agentSpec)
			agent.Status.Inventory = v1beta1.HostInventory{
				Interfaces: []v1beta1.HostInterface{
					{
						MacAddress: macStr,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			isoImageURL = "http://buzz.lightyear.io/discovery-image.iso"
			infraEnv = newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: isoImageURL}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: isoImageURL}
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr})
			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = "testInfraEnv"
			host.ObjectMeta.Labels = labels
			Expect(c.Create(ctx, host)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
		})

		Context("when Agent installation has started", func() {
			It("should set the detached annotation if agent is installed", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionTrue,
						Reason: v1beta1.InstalledReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
			})

			It("should set the detached annotation if agent is installation is progressing", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionFalse,
						Reason: v1beta1.InstallationInProgressReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
			})

			It("should set the detached annotation if agent is installation has failed", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionFalse,
						Reason: v1beta1.InstallationFailedReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
			})
		})

		Context("when Agent is installation has not started", func() {
			It("should not set the detached annotation if InstalledCondition is not set", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
			})

			It("should not set the detached annotation if installation has not started", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionFalse,
						Reason: v1beta1.InstallationNotStartedReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
			})
		})

		Context("when BMH is detached", func() {
			It("should not change the URL when it changes in the InfraEnv", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionFalse,
						Reason: v1beta1.InstallationFailedReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))

				infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: "http://go.find.it"}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				result, err = bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost = &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
				Expect(updatedHost.Spec.Image.URL).To(Equal(isoImageURL))
				Expect(updatedHost.Spec.Image.URL).ToNot(Equal(infraEnv.Status.ISODownloadURL))
			})
		})
	})

})

func newAgentWithClusterReference(name string, namespace string, ipv4address string, ipv6address string, macaddress string, clusterName string, agentBMHLabel string, creationTime time.Time) *v1beta1.Agent {
	agent := newAgent(name, namespace, v1beta1.AgentSpec{})
	agent.Status.Inventory = v1beta1.HostInventory{
		ReportTime: &metav1.Time{Time: time.Now()},
		Memory: v1beta1.HostMemory{
			PhysicalBytes: 2,
		},
		Interfaces: []v1beta1.HostInterface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					ipv4address,
				},
				IPV6Addresses: []string{
					ipv6address,
				},
				MacAddress: macaddress,
			},
		},
	}
	agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{Name: clusterName, Namespace: namespace}
	if agentBMHLabel != "" {
		agent.ObjectMeta.Labels = make(map[string]string)
		agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = agentBMHLabel
	}
	agent.ObjectMeta.CreationTimestamp.Time = creationTime
	return agent
}
