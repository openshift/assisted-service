package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var BASIC_KUBECONFIG = `test`
var BASIC_CERT = `test`

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
			Client:               c,
			APIReader:            c,
			Scheme:               scheme.Scheme,
			Log:                  common.GetTestLog(),
			spokeClient:          fakeclient.NewClientBuilder().WithScheme(schemes).Build(),
			ConvergedFlowEnabled: false,
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

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).NotTo(HaveKey(BMH_INSPECT_ANNOTATION))
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
		var infraEnv *v1beta1.InfraEnv

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
						DriveType:               string(models.DriveTypeSSD),
						Bootable:                true,
						SizeBytes:               int64(120) * 1000 * 1000 * 1000,
					},
					{
						ID:                      "2",
						InstallationEligibility: v1beta1.HostInstallationEligibility{Eligible: true},
						Path:                    "/dev/sdb",
						DriveType:               string(models.DriveTypeSSD),
						Bootable:                true,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			isoImageURL := "http://buzz.lightyear.io/discovery-image.iso"
			infraEnv = newInfraEnvImage("myInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: isoImageURL}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: isoImageURL}
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

			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = infraEnv.Name
			host.ObjectMeta.Labels = labels

			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, staleAgent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, infraEnv)).ShouldNot(HaveOccurred())
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

			It("should set invalid InstallationDiskID if RootDeviceHints device name doesn't match", func() {
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
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("/dev/not-found-by-hints"))
			})

			It("should set invalid InstallationDiskID if RootDeviceHints min size doesn't match", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.MinSizeGigabytes = 121
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("/dev/not-found-by-hints"))
			})

			It("should set the InstallationDiskID if the RootDeviceHints were provided and match", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.DeviceName = "/dev/sda"
				updatedHost.Spec.RootDeviceHints.MinSizeGigabytes = 110
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("1"))
			})

			It("Should update agent only once", func() {
				mockClient := NewMockK8sClient(mockCtrl)
				bmhr.Client = mockClient
				mockClient.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(types.NamespacedName{}), gomock.AssignableToTypeOf(&v1beta1.InfraEnv{})).DoAndReturn(
					func(ctx context.Context, name types.NamespacedName, infraEnv *v1beta1.InfraEnv) error {
						return c.Get(ctx, name, infraEnv)
					},
				).Times(2)

				mockClient.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(types.NamespacedName{}), gomock.AssignableToTypeOf(&bmh_v1alpha1.BareMetalHost{})).DoAndReturn(
					func(ctx context.Context, name types.NamespacedName, bmh *bmh_v1alpha1.BareMetalHost) error {
						return c.Get(ctx, name, bmh)
					},
				).Times(2)

				mockClient.EXPECT().List(gomock.Any(), gomock.AssignableToTypeOf(&v1beta1.AgentList{}), gomock.Any()).DoAndReturn(
					func(ctx context.Context, agentList *v1beta1.AgentList, namespace client.InNamespace) error {
						return c.List(ctx, agentList, namespace)
					},
				).Times(2)

				mockClient.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&bmh_v1alpha1.BareMetalHost{})).DoAndReturn(
					func(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost, opts ...client.UpdateOption) error {
						return c.Update(ctx, bmh)
					},
				).Times(2)
				mockClient.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&v1beta1.Agent{})).DoAndReturn(
					func(ctx context.Context, agent *v1beta1.Agent, opts ...client.UpdateOption) error {
						return c.Update(ctx, agent)
					},
				).Times(1)
				for i := 0; i != 2; i++ {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())

					Expect(updatedAgent.Spec.Approved).To(Equal(true))

					Expect(updatedAgent.ObjectMeta.Labels[AGENT_BMH_LABEL]).To(Equal(host.Name))

					Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("1"))
					Expect(updatedAgent.Spec.Role).To(Equal(models.HostRoleMaster))
					Expect(updatedAgent.Spec.Hostname).To(Equal("happy-meal"))
					Expect(updatedAgent.Spec.MachineConfigPool).To(Equal("number-8"))
					Expect(updatedAgent.Spec.InstallerArgs).To(Equal(`["--args", "aaaa"]`))
					Expect(updatedAgent.Spec.IgnitionConfigOverrides).To(Equal("agent-ignition"))
				}
			})
		})
	})

	Describe("Reconcile a BMH with an approved matching agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var infraEnv *v1beta1.InfraEnv

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1beta1.AgentSpec{Approved: true})
			agent.Status.Inventory = v1beta1.HostInventory{
				Memory: v1beta1.HostMemory{
					PhysicalBytes: 2,
				},
				Hostname: "discovered-hostname",
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

			isoImageURL := "http://buzz.lightyear.io/discovery-image.iso"
			infraEnv = newInfraEnvImage("myInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: isoImageURL}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: isoImageURL}
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr})

			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = infraEnv.Name
			host.ObjectMeta.Labels = labels

			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, infraEnv)).ShouldNot(HaveOccurred())
		})

		Context("when an agent matches", func() {
			It("should set the metal3 hardwaredetails hostname to the inventory if agent.Spec.Hostname is provided", func() {
				agent.Spec.Hostname = "desired-hostname"
				Expect(c.Update(ctx, agent)).To(BeNil())

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
				Expect(hDetails.Hostname).To(Equal(agent.Spec.Hostname))
			})

			It("should set the metal3 hardwaredetails hostname to the inventory if agent.Spec.Hostname is not provided", func() {
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
				Expect(hDetails.Hostname).To(Equal(agent.Status.Inventory.Hostname))
			})

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
				Expect(hDetails.Hostname).To(Equal(agent.Status.Inventory.Hostname))
			})
		})
	})

	Describe("Reconcile a Spoke BMH", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1beta1.Agent
		var infraEnv *v1beta1.InfraEnv
		var cluster *hivev1.ClusterDeployment
		var clusterInstall *hiveext.AgentClusterInstall
		var adminKubeconfigSecret *corev1.Secret
		var secretName string
		var bmhName string
		imageURL := "http://192.168.111.35:6181/images/rhcos-48.84.202106091622-0-openstack.x86_64.qcow2/cached-rhcos-48.84.202106091622-0-openstack.x86_64.qcow2"

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

			agent.Status.Role = models.HostRoleWorker
			agent.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{Name: clusterName, Namespace: testNamespace}
			Expect(c.Create(ctx, agent)).To(BeNil())

			isoImageURL := "http://buzz.lightyear.io/discovery-image.iso"
			infraEnv = newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: isoImageURL}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())

			image := &bmh_v1alpha1.Image{URL: isoImageURL}
			bmhName = "bmh-reconcile"
			host = newBMH(bmhName, &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr, BMC: bmh_v1alpha1.BMCDetails{CredentialsName: fmt.Sprintf(adminKubeConfigStringTemplate, clusterName)}})
			annotations := make(map[string]string)
			annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = `{"ignition":{"version":"3.1.0", "security": {"tls":{"certificateAuthorities":[{"source":"data:text/plain;charset=utf-8;base64,c29tZSBjZXJ0aWZpY2F0ZQ=="}]}}}}`
			host.ObjectMeta.SetAnnotations(annotations)

			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = infraEnv.Name
			host.ObjectMeta.Labels = labels

			Expect(c.Create(ctx, host)).To(BeNil())

			defaultClusterSpec := getDefaultClusterDeploymentSpec(clusterName, "test-cluster-aci", pullSecretName)
			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			cluster.Spec.Installed = true
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
			clusterInstall = newAgentClusterInstall(cluster.Spec.ClusterInstallRef.Name, testNamespace, hiveext.AgentClusterInstallSpec{}, cluster)
			Expect(c.Create(ctx, clusterInstall)).ShouldNot(HaveOccurred())
			secretName = fmt.Sprintf(adminKubeConfigStringTemplate, clusterName)
			adminKubeconfigSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: cluster.Namespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte(BASIC_KUBECONFIG),
				},
			}
			Expect(c.Create(ctx, adminKubeconfigSecret)).ShouldNot(HaveOccurred())

			spokeMachine := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-bmh-reconcile",
					Namespace: testNamespace,
					Labels: map[string]string{
						machinev1beta1.MachineClusterIDLabel: clusterName,
						MACHINE_ROLE:                         string(models.HostRoleWorker),
						MACHINE_TYPE:                         string(models.HostRoleWorker),
					},
				},
			}
			Expect(c.Create(ctx, spokeMachine)).To(BeNil())

			spokeMachineMaster := &machinev1beta1.Machine{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "machine.openshift.io/v1beta1",
					Kind:       "Machine",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spoke-machine-master-0",
					Namespace: testNamespace,
					Labels: map[string]string{
						machinev1beta1.MachineClusterIDLabel: clusterName,
						MACHINE_ROLE:                         string(models.HostRoleMaster),
						MACHINE_TYPE:                         string(models.HostRoleMaster),
					},
				},
				Spec: machinev1beta1.MachineSpec{
					ProviderSpec: machinev1beta1.ProviderSpec{
						Value: &runtime.RawExtension{
							Raw: []byte(fmt.Sprintf(`{
												"image": {
												"checksum": "%s.md5sum",
												"url": "%s"
												}}`, imageURL, imageURL)),
						},
					},
				},
			}
			Expect(bmhr.spokeClient.Create(ctx, spokeMachineMaster)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, infraEnv)).ShouldNot(HaveOccurred())
		})

		Context("when agent role worker and cluster deployment is set", func() {
			It("should set spoke BMH", func() {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "root-ca",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ca.crt": BASIC_CERT,
					},
				}
				Expect(bmhr.spokeClient.Create(ctx, configMap)).ShouldNot(HaveOccurred())
				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_AGENT_IGNITION_CONFIG_OVERRIDES))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).NotTo(Equal(""))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).To(ContainSubstring("dGVzdA=="))

				machineName := fmt.Sprintf("%s-%s", cluster.Name, host.Name)

				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).To(BeNil())
				Expect(spokeBMH.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]).To(Equal(updatedHost.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]))
				Expect(spokeBMH.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(spokeBMH.Spec.Image).To(Equal(updatedHost.Spec.Image))
				Expect(spokeBMH.Spec.ConsumerRef.Kind).To(Equal("Machine"))
				Expect(spokeBMH.Spec.ConsumerRef.Name).To(Equal(machineName))
				Expect(spokeBMH.Spec.ConsumerRef.Namespace).To(Equal(OPENSHIFT_MACHINE_API_NAMESPACE))
				Expect(spokeBMH.Spec.ConsumerRef.APIVersion).To(Equal(machinev1beta1.SchemeGroupVersion.String()))

				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).To(BeNil())
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(machinev1beta1.MachineClusterIDLabel))
				Expect(spokeMachine.ObjectMeta.Annotations).To(HaveKey("metal3.io/BareMetalHost"))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_ROLE))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_TYPE))
				Expect(string(spokeMachine.Spec.ProviderSpec.Value.Raw)).To(ContainSubstring(imageURL))

				spokeSecret := &corev1.Secret{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: adminKubeconfigSecret.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeSecret)
				Expect(err).To(BeNil())
				Expect(spokeSecret.Data).To(Equal(adminKubeconfigSecret.Data))
				Expect(spokeSecret.Labels).To(HaveKeyWithValue(BackupLabel, BackupLabelValue))

			})
			It("should not set spoke BMH - None platform", func() {
				clusterInstall.Spec.Networking.UserManagedNetworking = swag.Bool(true)
				Expect(c.Update(ctx, clusterInstall)).ToNot(HaveOccurred())
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())

				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).ToNot(BeNil())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
				spokeSecret := &corev1.Secret{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: adminKubeconfigSecret.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeSecret)
				Expect(err).ToNot(BeNil())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})

			It("validate label on Secrets", func() {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "root-ca",
						Namespace: "kube-system",
					},
					Data: map[string]string{
						"ca.crt": BASIC_CERT,
					},
				}
				Expect(bmhr.spokeClient.Create(ctx, configMap)).ShouldNot(HaveOccurred())
				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				secret := &corev1.Secret{}
				By("Checking if the secret has the custom label")
				secretKey := types.NamespacedName{
					Name:      secretName,
					Namespace: cluster.Namespace,
				}
				Expect(c.Get(ctx, secretKey, secret)).To(BeNil())
				Expect(secret.Labels).To(HaveKeyWithValue(WatchResourceLabel, WatchResourceValue))
				Expect(secret.Labels).To(HaveKeyWithValue(BackupLabel, BackupLabelValue))
			})

			It("ClusterDeployment not set in Agent", func() {
				agent.Spec.ClusterDeploymentName = nil
				Expect(c.Update(ctx, agent)).To(BeNil())
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))
			})

			It("should fall back to Hypershift root CA storage", func() {
				_, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).NotTo(HaveOccurred())
				_, err = bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(HaveOccurred())
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-root-ca",
						Namespace: "openshift-config",
					},
					Data: map[string]string{
						"ca.crt": BASIC_CERT,
					},
				}
				Expect(bmhr.spokeClient.Create(ctx, configMap)).ShouldNot(HaveOccurred())
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))
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
			It("conditions list doesn't contain installed condition", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).NotTo(Equal("assisted-service-controller"))
			})
			It("should set the detached annotation if agent is installed", func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.InstalledCondition,
						Status: corev1.ConditionTrue,
						Reason: v1beta1.InstalledReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
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

				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
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

				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
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

				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))

				infraEnv.Status = v1beta1.InfraEnvStatus{ISODownloadURL: "http://go.find.it"}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
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

		Context("when Agent is unbound-pending-on-user-action", func() {
			BeforeEach(func() {
				agent.Status.Conditions = []conditionsv1.Condition{
					{
						Type:   v1beta1.BoundCondition,
						Status: corev1.ConditionTrue,
						Reason: v1beta1.UnbindingPendingUserActionReason,
					},
				}
				Expect(c.Update(ctx, agent)).To(BeNil())

				bmh := &bmh_v1alpha1.BareMetalHost{}
				_ = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, bmh)
				Expect(bmh.Spec.Image).NotTo(BeNil())
				bmh.ObjectMeta.Annotations = make(map[string]string)
				bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "assisted-service-controller"
				Expect(c.Update(ctx, bmh)).To(BeNil())
			})

			It("should remove the detached annotation and clear the ISO", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).NotTo(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(updatedHost.Spec.Image).To(BeNil())
			})
		})
	})
})

var _ = Describe("bmac reconcile - converged flow enabled", func() {
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
			Client:               c,
			APIReader:            c,
			Scheme:               scheme.Scheme,
			Log:                  common.GetTestLog(),
			spokeClient:          fakeclient.NewClientBuilder().WithScheme(schemes).Build(),
			ConvergedFlowEnabled: true,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("with an existing infraEnv with ISODownloadURL", func() {
		var infraEnv *v1beta1.InfraEnv
		var isoImageURL string
		var isoTimestamp metav1.Time
		var host *bmh_v1alpha1.BareMetalHost

		BeforeEach(func() {
			isoImageURL = "http://buzz.lightyear.io/discovery-image.iso"
			isoTimestamp = metav1.Time{Time: time.Now().Add(-10 * time.Hour)}
			infraEnv = newInfraEnvImage("testInfraEnv", testNamespace, v1beta1.InfraEnvSpec{})
			infraEnv.Status = v1beta1.InfraEnvStatus{
				ISODownloadURL: isoImageURL,
				CreatedTime:    &isoTimestamp,
			}
			Expect(c.Create(ctx, infraEnv)).To(BeNil())
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
			labels := make(map[string]string)
			labels[BMH_INFRA_ENV_LABEL] = "testInfraEnv"
			host.ObjectMeta.Labels = labels
			Expect(c.Create(ctx, host)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, infraEnv)).ShouldNot(HaveOccurred())
		})

		It("should not disable the BMH hardware inspection", func() {
			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.ObjectMeta.Annotations).To(BeNil())
		})

		It("should disable cleaning in the BMH", func() {

			host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeMetadata
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))
			// check that the host isn't detached
			Expect(updatedHost.ObjectMeta.Annotations).To(BeNil())
		})

		It("should set custom deploy method in the BMH", func() {

			host.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{}
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.CustomDeploy.Method).To(Equal(ASSISTED_DEPLOY_METHOD))
			// check that the host isn't detached
			Expect(updatedHost.ObjectMeta.Annotations).To(BeNil())
		})
		It("should set detached annotation on the BMH", func() {
			agentSpec := v1beta1.AgentSpec{
				Approved: true,
			}
			agent := newAgent("bmac-agent", testNamespace, agentSpec)
			macStr := "12-34-56-78-9A-BC"
			agent.Status.Inventory = v1beta1.HostInventory{
				Interfaces: []v1beta1.HostInterface{
					{
						MacAddress: macStr,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			host.Spec.BootMACAddress = macStr
			host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
			host.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{Method: ASSISTED_DEPLOY_METHOD}
			host.Status.Provisioning.State = bmh_v1alpha1.StateProvisioned
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
			Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
		})
		It("should not set detached annotation on the BMH, BMH is still preparing", func() {
			agentSpec := v1beta1.AgentSpec{
				Approved: true,
			}
			agent := newAgent("bmac-agent", testNamespace, agentSpec)
			macStr := "12-34-56-78-9A-BC"
			agent.Status.Inventory = v1beta1.HostInventory{
				Interfaces: []v1beta1.HostInterface{
					{
						MacAddress: macStr,
					},
				},
			}
			Expect(c.Create(ctx, agent)).To(BeNil())

			host.Spec.BootMACAddress = macStr
			host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
			host.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{Method: ASSISTED_DEPLOY_METHOD}
			host.Status.Provisioning.State = bmh_v1alpha1.StatePreparing
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.ObjectMeta.Annotations).To(Not(HaveKey(BMH_DETACHED_ANNOTATION)))
		})
		It("should not set the ISODownloadURL in the BMH", func() {
			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.Image).To(BeNil())
		})

		It("should not set custom deploy method in case the BMH is detached", func() {
			host.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{}
			delete(host.ObjectMeta.Labels, BMH_INFRA_ENV_LABEL)
			host.ObjectMeta.Annotations = make(map[string]string)
			host.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "assisted-service-controller"
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.CustomDeploy.Method).ToNot(Equal(ASSISTED_DEPLOY_METHOD))
			// check that the host is still detached
			Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
		})

		It("should not set custom deploy method in case the BMH doesn't have InfraEnv label", func() {
			host.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{}
			delete(host.ObjectMeta.Labels, BMH_INFRA_ENV_LABEL)
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.CustomDeploy.Method).ToNot(Equal(ASSISTED_DEPLOY_METHOD))
			// check that the host isn't detached
			Expect(updatedHost.ObjectMeta.Annotations).To(BeNil())
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
	agent.ObjectMeta.Labels = make(map[string]string)
	agent.ObjectMeta.Labels[AgentLabelClusterDeploymentNamespace] = namespace
	if agentBMHLabel != "" {
		agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = agentBMHLabel
	}
	agent.ObjectMeta.CreationTimestamp.Time = creationTime
	return agent
}
