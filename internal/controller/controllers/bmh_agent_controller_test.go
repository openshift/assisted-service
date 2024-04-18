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
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/openshift/assisted-service/models"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubectl/pkg/drain"
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

	It("adds a finalizer to the BMH when it has the management annotation", func() {
		host := newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
		host.ObjectMeta.SetAnnotations(map[string]string{BMH_DELETE_ANNOTATION: "true"})
		Expect(c.Create(ctx, host)).To(BeNil())

		bmhr.ConvergedFlowEnabled = true
		result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{Name: host.Name, Namespace: host.Namespace}
		Expect(c.Get(ctx, key, host)).To(Succeed())
		Expect(host.GetFinalizers()).To(ContainElement(BMH_FINALIZER_NAME))
	})

	It("doesn't add a finalizer to the BMH when it does not have the management annotation", func() {
		host := newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())

		result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{Name: host.Name, Namespace: host.Namespace}
		Expect(c.Get(ctx, key, host)).To(Succeed())
		Expect(host.GetFinalizers()).ToNot(ContainElement(BMH_FINALIZER_NAME))
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
				Expect(c.Get(ctx, types.NamespacedName{Name: infraEnv.Name, Namespace: infraEnv.Namespace}, infraEnv)).ToNot(HaveOccurred())
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

				result := bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
				Expect(result).To(Equal(reconcileComplete{dirty: true, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))

				// Test that cleaning mode stays the same
				host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeMetadata
				host.Status.Provisioning.State = bmh_v1alpha1.StateProvisioned

				result = bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
				Expect(result).To(Equal(reconcileComplete{dirty: false, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeMetadata))

				// This should not return a dirty result because label is already set
				result = bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
				Expect(result).To(Equal(reconcileComplete{dirty: false, stop: true}))
				Expect(host.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(host.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(host.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeMetadata))
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

			It("should not disable cleaning and set online true in the BMH", func() {
				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.Spec.Online).To(Equal(true))
				Expect(updatedHost.Spec.AutomatedCleaningMode).NotTo(Equal(bmh_v1alpha1.CleaningModeDisabled))
			})
			It("should not reconcile BMH if the updated image has not been around longer than the grace period", func() {
				// Reconcile with the original ISO
				_ = bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)

				// Generate a new ISO with the current timestamp
				infraEnv.Status = v1beta1.InfraEnvStatus{
					ISODownloadURL: isoImageURL + ".new",
					CreatedTime:    &metav1.Time{Time: time.Now()},
				}
				Expect(c.Update(ctx, infraEnv)).To(BeNil())

				// Should not reconcile because ISO is too recent.
				// We expect the old URL to be still attached to the BMH.
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
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
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
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
				result := bmhr.reconcileBMH(ctx, bmhr.Log, host, nil, infraEnv)
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
						ByPath:                  "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:2:0:0",
						DriveType:               string(models.DriveTypeSSD),
						Bootable:                true,
						SizeBytes:               int64(120) * 1000 * 1000 * 1000,
					},
					{
						ID:                      "2",
						InstallationEligibility: v1beta1.HostInstallationEligibility{Eligible: true},
						Path:                    "/dev/sdb",
						ByPath:                  "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:2:1:0",
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

			// Add node labels annotations
			annotations[NODE_LABEL_PREFIX+"first-label"] = ""
			annotations[NODE_LABEL_PREFIX+"second-label"] = "second-label"
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

			Context("should set the agent spec node labels based on the corresponding BMH annotations", func() {
				It("set initial node labels", func() {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.NodeLabels).To(Equal(map[string]string{
						"first-label":  "",
						"second-label": "second-label",
					}))
				})
				It("modify node labels", func() {
					h2 := bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, &h2)).ToNot(HaveOccurred())
					annotations := h2.ObjectMeta.GetAnnotations()
					annotations[NODE_LABEL_PREFIX+"third-label"] = "blah"
					annotations[NODE_LABEL_PREFIX+"forth-label"] = "forth"
					delete(annotations, NODE_LABEL_PREFIX+"second-label")
					h2.ObjectMeta.SetAnnotations(annotations)
					Expect(c.Update(ctx, &h2)).ToNot(HaveOccurred())
					updatedAgent := &v1beta1.Agent{}
					Expect(c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)).ToNot(HaveOccurred())
					updatedAgent.Spec.NodeLabels = map[string]string{
						"first-label":  "",
						"second-label": "second-label",
					}
					Expect(c.Update(ctx, updatedAgent)).ToNot(HaveOccurred())
					result, err := bmhr.Reconcile(ctx, newBMHRequest(&h2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.NodeLabels).To(Equal(map[string]string{
						"first-label": "",
						"third-label": "blah",
						"forth-label": "forth",
					}))
				})
				It("clear node labels", func() {
					updatedAgent := &v1beta1.Agent{}
					Expect(c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)).ToNot(HaveOccurred())
					updatedAgent.Spec.NodeLabels = map[string]string{
						"first-label":  "",
						"second-label": "second-label",
					}
					Expect(c.Update(ctx, updatedAgent)).ToNot(HaveOccurred())
					h2 := bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, &h2)).ToNot(HaveOccurred())
					annotations := h2.ObjectMeta.GetAnnotations()
					delete(annotations, NODE_LABEL_PREFIX+"first-label")
					delete(annotations, NODE_LABEL_PREFIX+"second-label")
					h2.ObjectMeta.SetAnnotations(annotations)
					Expect(c.Update(ctx, &h2)).ToNot(HaveOccurred())
					result, err := bmhr.Reconcile(ctx, newBMHRequest(&h2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.NodeLabels).To(BeEmpty())
				})
			})
			Context("should set the agent labels based on the corresponding BMH annotations", func() {
				addAnnotation := func(bmh *bmh_v1alpha1.BareMetalHost, key, value string) {
					if bmh.Annotations == nil {
						bmh.Annotations = make(map[string]string)
					}
					bmh.Annotations[key] = value
					Expect(c.Update(ctx, bmh)).ToNot(HaveOccurred())
				}
				addLabel := func(bmh *bmh_v1alpha1.BareMetalHost, key, value string) {
					addAnnotation(bmh, AGENT_LABEL_PREFIX+key, value)
				}
				expectToContainKeyValue := func(agent *v1beta1.Agent, key, value string) {
					v, exists := getLabel(agent.Labels, key)
					Expect(exists).To(BeTrue())
					Expect(v).To(Equal(value))
				}
				expectToNotContainKey := func(agent *v1beta1.Agent, key string) {
					_, exists := getLabel(agent.Labels, key)
					Expect(exists).To(BeFalse())
				}
				BeforeEach(func() {
					addLabel(host, "initial-key", "initial-value")
				})
				It("set initial agent labels", func() {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					expectToContainKeyValue(updatedAgent, "initial-key", "initial-value")
				})
				It("modify agent labels", func() {
					h2 := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, h2)).ToNot(HaveOccurred())
					addLabel(h2, "third-label", "blah")
					addLabel(h2, "forth-label", "forth")
					Expect(c.Update(ctx, h2)).ToNot(HaveOccurred())
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					Expect(c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)).ToNot(HaveOccurred())
					expectToContainKeyValue(updatedAgent, "third-label", "blah")
					expectToContainKeyValue(updatedAgent, "forth-label", "forth")
					expectToContainKeyValue(updatedAgent, "initial-key", "initial-value")
					expectToNotContainKey(updatedAgent, "initial-key1")

					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, h2)).ToNot(HaveOccurred())
					addLabel(h2, "forth-label", "forth-label-value")
					Expect(c.Update(ctx, updatedAgent)).ToNot(HaveOccurred())
					result, err = bmhr.Reconcile(ctx, newBMHRequest(h2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					expectToContainKeyValue(updatedAgent, "forth-label", "forth-label-value")
				})
			})
			Context("reconcile cluster reference", func() {
				const (
					clusterName              = "cluster-name"
					clusterNamespace         = "cluster-namespace"
					infraenvClusterName      = "infraenv-name"
					infraenvClusterNamespace = "infraenv-namespace"
				)
				setClusterReferenceInAgent := func(name, namespace string) {
					toUpdate := &v1beta1.Agent{}
					Expect(c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, toUpdate)).ToNot(HaveOccurred())
					clusterRef := &v1beta1.ClusterReference{
						Name:      name,
						Namespace: namespace,
					}
					toUpdate.Spec.ClusterDeploymentName = clusterRef
					Expect(c.Update(ctx, toUpdate)).ToNot(HaveOccurred())
					updatedAgent := &v1beta1.Agent{}
					Expect(c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)).ToNot(HaveOccurred())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(Equal(clusterRef))
				}
				setClusterReferenceInBMH := func(name, namespace string) {
					toUpdate := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, toUpdate)).ToNot(HaveOccurred())
					clusterRef := fmt.Sprintf("%s/%s", namespace, name)
					toUpdate.ObjectMeta.Annotations[BMH_CLUSTER_REFERENCE] = clusterRef
					Expect(c.Update(ctx, toUpdate)).ToNot(HaveOccurred())
					updatedBMH := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, updatedBMH)).ToNot(HaveOccurred())
					Expect(updatedBMH.ObjectMeta.Annotations[BMH_CLUSTER_REFERENCE]).To(Equal(clusterRef))
				}
				setEmptyClusterReferenceInBMH := func() {
					toUpdate := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, toUpdate)).ToNot(HaveOccurred())
					toUpdate.ObjectMeta.Annotations[BMH_CLUSTER_REFERENCE] = ""
					Expect(c.Update(ctx, toUpdate)).ToNot(HaveOccurred())
					updatedBMH := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, updatedBMH)).ToNot(HaveOccurred())
					Expect(updatedBMH.ObjectMeta.Annotations[BMH_CLUSTER_REFERENCE]).To(Equal(""))
				}
				setClusterReferenceInInfraenv := func(name, namespace string) {
					toUpdate := &v1beta1.InfraEnv{}
					Expect(c.Get(ctx, types.NamespacedName{Name: infraEnv.Name, Namespace: infraEnv.Namespace}, toUpdate)).ToNot(HaveOccurred())
					clusterRef := &v1beta1.ClusterReference{
						Name:      name,
						Namespace: namespace,
					}
					toUpdate.Spec.ClusterRef = clusterRef
					Expect(c.Update(ctx, toUpdate)).ToNot(HaveOccurred())
					updatedInfraenv := &v1beta1.InfraEnv{}
					Expect(c.Get(ctx, types.NamespacedName{Name: infraEnv.Name, Namespace: infraEnv.Namespace}, updatedInfraenv)).ToNot(HaveOccurred())
					Expect(updatedInfraenv.Spec.ClusterRef).To(Equal(clusterRef))
				}
				It("no cluster reference in BMH and agent - should not set cluster reference in agent", func() {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(BeNil())
				})
				It("no cluster reference in BMH  - should not clear cluster reference in agent", func() {
					setClusterReferenceInAgent(clusterName, clusterNamespace)
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(Equal(&v1beta1.ClusterReference{
						Name:      clusterName,
						Namespace: clusterNamespace,
					}))
				})
				It("empty cluster-reference in BMH - should clear cluster reference in agent", func() {
					setEmptyClusterReferenceInBMH()
					setClusterReferenceInAgent(clusterName, clusterNamespace)
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(BeNil())
				})
				It("BMH has cluster reference - should copy to agent", func() {
					setClusterReferenceInBMH(clusterName, clusterNamespace)
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(Equal(&v1beta1.ClusterReference{
						Name:      clusterName,
						Namespace: clusterNamespace,
					}))
				})
				It("Infraenv has cluster reference - should not copy to agent and stop", func() {
					setClusterReferenceInBMH(clusterName, clusterNamespace)
					setClusterReferenceInInfraenv(infraenvClusterName, infraenvClusterNamespace)
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(HaveOccurred())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(BeNil())
				})
				It("Infraenv has cluster reference - should not clear cluster reference in agent", func() {
					setClusterReferenceInAgent(clusterName, clusterNamespace)
					setClusterReferenceInInfraenv(infraenvClusterName, infraenvClusterNamespace)
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedAgent := &v1beta1.Agent{}
					err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
					Expect(err).To(BeNil())
					Expect(updatedAgent.Spec.ClusterDeploymentName).To(Equal(&v1beta1.ClusterReference{
						Name:      clusterName,
						Namespace: clusterNamespace,
					}))
				})
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

			It("should set the InstallationDiskID if the by-path RootDeviceHints were provided and match", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.DeviceName = "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:2:0:0"
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
			It("should not touch InstallationDiskID if the RootDeviceHints were not provided", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints = nil
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				updatedAgent.Spec.InstallationDiskID = "/dev/chocobomb"
				Expect(c.Update(ctx, updatedAgent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("/dev/chocobomb"))
			})

			It("should set the InstallationDiskID if both diskID and the RootDeviceHints were provided", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.DeviceName = "/dev/sda"
				updatedHost.Spec.RootDeviceHints.MinSizeGigabytes = 110
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				updatedAgent := &v1beta1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				updatedAgent.Spec.InstallationDiskID = "/dev/chocobomb"
				Expect(c.Update(ctx, updatedAgent)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("1"))
			})

			It("Should update agent only once", func() {
				mockClient := NewMockK8sClient(mockCtrl)
				bmhr.Client = mockClient
				mockClient.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(types.NamespacedName{}), gomock.AssignableToTypeOf(&v1beta1.InfraEnv{})).DoAndReturn(
					func(ctx context.Context, name types.NamespacedName, infraEnv *v1beta1.InfraEnv, opts ...client.GetOption) error {
						return c.Get(ctx, name, infraEnv, opts...)
					},
				).Times(2)

				mockClient.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(types.NamespacedName{}), gomock.AssignableToTypeOf(&bmh_v1alpha1.BareMetalHost{})).DoAndReturn(
					func(ctx context.Context, name types.NamespacedName, bmh *bmh_v1alpha1.BareMetalHost, opts ...client.GetOption) error {
						return c.Get(ctx, name, bmh, opts...)
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

		Context("Day 2 host", func() {
			var host_day2 *bmh_v1alpha1.BareMetalHost
			var agent_day2 *v1beta1.Agent
			var infraEnv_day2 *v1beta1.InfraEnv
			BeforeEach(func() {
				macStr := "12-34-56-78-9A-AA"
				isoImageURL := "http://buzz.lightyear.io/discovery-image.iso"
				image := &bmh_v1alpha1.Image{URL: isoImageURL}
				infraEnv_day2 = newInfraEnvImage("testInfraEnv-day2", testNamespace, v1beta1.InfraEnvSpec{})
				infraEnv_day2.Status = v1beta1.InfraEnvStatus{ISODownloadURL: isoImageURL}
				Expect(c.Create(ctx, infraEnv_day2)).To(BeNil())
				bmhName = "bmh-reconcile-day2"
				host_day2 = newBMH(bmhName, &bmh_v1alpha1.BareMetalHostSpec{Image: image, BootMACAddress: macStr, BMC: bmh_v1alpha1.BMCDetails{CredentialsName: fmt.Sprintf(adminKubeConfigStringTemplate, cluster.Name)}})
				annotations := make(map[string]string)
				annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = `{"ignition":{"version":"3.1.0", "security": {"tls":{"certificateAuthorities":[{"source":"data:text/plain;charset=utf-8;base64,c29tZSBjZXJ0aWZpY2F0ZQ=="}]}}}}`

				host_day2.ObjectMeta.SetAnnotations(annotations)
				labels := make(map[string]string)
				labels[BMH_INFRA_ENV_LABEL] = infraEnv.Name
				host_day2.ObjectMeta.Labels = labels

				Expect(c.Create(ctx, host_day2)).To(BeNil())

				agent_day2 = newAgent("bmac-agent-day2", testNamespace, v1beta1.AgentSpec{Approved: true})
				agent_day2.Status.Inventory = v1beta1.HostInventory{
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
				agent_day2.Status.Role = models.HostRoleWorker
				agent_day2.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{Name: cluster.Name, Namespace: testNamespace}
				Expect(c.Create(ctx, agent_day2)).To(BeNil())

			})
			It("should create spoke BMH for day 2 host with worker role when it's installing - happy flow", func() {
				agent_day2.Status.DebugInfo.State = models.HostStatusInstalling
				Expect(c.Update(ctx, agent_day2)).To(BeNil())

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
				Expect(c.Update(context.Background(), agent_day2)).ShouldNot(HaveOccurred())
				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host_day2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				By("Checking if the BMH has the correct annotations")
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_AGENT_IGNITION_CONFIG_OVERRIDES))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).NotTo(Equal(""))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).To(ContainSubstring("dGVzdA=="))

				By("Checking the spoke BMH exists and is correct")
				machineName := fmt.Sprintf("%s-%s", cluster.Name, host_day2.Name)
				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).To(BeNil())
				Expect(spokeBMH.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]).To(Equal(updatedHost.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]))
				Expect(spokeBMH.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(spokeBMH.Spec.Image).To(Equal(updatedHost.Spec.Image))
				Expect(spokeBMH.Spec.ConsumerRef.Kind).To(Equal("Machine"))
				Expect(spokeBMH.Spec.ConsumerRef.Name).To(Equal(machineName))
				Expect(spokeBMH.Spec.ConsumerRef.Namespace).To(Equal(OPENSHIFT_MACHINE_API_NAMESPACE))
				Expect(spokeBMH.Spec.ConsumerRef.APIVersion).To(Equal(machinev1beta1.SchemeGroupVersion.String()))

				By("Checking the spoke Machine exists and is correct")
				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).To(BeNil())
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(machinev1beta1.MachineClusterIDLabel))
				Expect(spokeMachine.ObjectMeta.Annotations).To(HaveKey("metal3.io/BareMetalHost"))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_ROLE))
				Expect(spokeMachine.ObjectMeta.Labels[MACHINE_ROLE]).To(Equal(string(models.HostRoleWorker)))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_TYPE))
				Expect(spokeMachine.ObjectMeta.Labels[MACHINE_TYPE]).To(Equal(string(models.HostRoleWorker)))
				Expect(string(spokeMachine.Spec.ProviderSpec.Value.Raw)).To(ContainSubstring(imageURL))
			})
			It("should create spoke BMH & Machine for day 2 host with master role when it's installing - happy flow", func() {
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

				By("Setting the role to master")
				annotations := make(map[string]string)
				annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = `{"ignition":{"version":"3.1.0", "security": {"tls":{"certificateAuthorities":[{"source":"data:text/plain;charset=utf-8;base64,c29tZSBjZXJ0aWZpY2F0ZQ=="}]}}}}`
				annotations[BMH_AGENT_ROLE] = "master"
				host_day2.ObjectMeta.SetAnnotations(annotations)
				Expect(c.Update(ctx, host_day2)).To(BeNil())
				agent_day2.Status.Role = "master"

				By("Updating the agent to installing")
				agent_day2.Status.DebugInfo.State = models.HostStatusInstalling
				Expect(c.Update(context.Background(), agent_day2)).ShouldNot(HaveOccurred())
				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host_day2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}

				By("Checking if the BMH has the correct annotations")
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_AGENT_IGNITION_CONFIG_OVERRIDES))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).NotTo(Equal(""))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).To(ContainSubstring("dGVzdA=="))

				By("Checking the spoke BMH exists and is correct")
				machineName := fmt.Sprintf("%s-%s", cluster.Name, host_day2.Name)
				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).To(BeNil())
				Expect(spokeBMH.ObjectMeta.Annotations).To(HaveKey(BMH_HARDWARE_DETAILS_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]).To(Equal(updatedHost.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]))
				Expect(spokeBMH.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
				Expect(spokeBMH.Spec.Image).To(Equal(updatedHost.Spec.Image))
				Expect(spokeBMH.Spec.ConsumerRef.Kind).To(Equal("Machine"))
				Expect(spokeBMH.Spec.ConsumerRef.Name).To(Equal(machineName))
				Expect(spokeBMH.Spec.ConsumerRef.Namespace).To(Equal(OPENSHIFT_MACHINE_API_NAMESPACE))
				Expect(spokeBMH.Spec.ConsumerRef.APIVersion).To(Equal(machinev1beta1.SchemeGroupVersion.String()))

				By("Checking the spoke Machine exists and is correct")
				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).To(BeNil())
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(machinev1beta1.MachineClusterIDLabel))
				Expect(spokeMachine.ObjectMeta.Annotations).To(HaveKey("metal3.io/BareMetalHost"))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_ROLE))
				Expect(spokeMachine.ObjectMeta.Labels[MACHINE_ROLE]).To(Equal(string(models.HostRoleMaster)))
				Expect(spokeMachine.ObjectMeta.Labels).To(HaveKey(MACHINE_TYPE))
				Expect(spokeMachine.ObjectMeta.Labels[MACHINE_TYPE]).To(Equal(string(models.HostRoleMaster)))
				Expect(string(spokeMachine.Spec.ProviderSpec.Value.Raw)).To(ContainSubstring(imageURL))

			})
			It("should not create spoke BMH for day 2 host if cluster does not exist", func() {
				agent_day2.Spec.ClusterDeploymentName = nil
				Expect(c.Update(ctx, agent_day2)).To(BeNil())

				for range [3]int{} {
					result, err := bmhr.Reconcile(ctx, newBMHRequest(host_day2))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))
				}
				By("Checking if the BMH does not have the detached annotation")
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))

				By("Checking the spoke BMH does not exist")
				machineName := fmt.Sprintf("%s-%s", cluster.Name, host_day2.Name)
				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).To(HaveOccurred())

				By("Checking the spoke Machine does not exist")
				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).To(HaveOccurred())
			})
			It("should not create spoke BMH for day 2 if cluster is not day 2", func() {
				pullSecretName := "pull-secret"
				clusterName := "fake-day2-test-cluster"
				defaultClusterSpec := getDefaultClusterDeploymentSpec(clusterName, "test-cluster-aci-day2", pullSecretName)
				fake_day2_cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
				fake_day2_cluster.Spec.Installed = false
				Expect(c.Create(ctx, fake_day2_cluster)).ShouldNot(HaveOccurred())
				clusterInstall = newAgentClusterInstall(fake_day2_cluster.Spec.ClusterInstallRef.Name, testNamespace, hiveext.AgentClusterInstallSpec{}, fake_day2_cluster)
				Expect(c.Create(ctx, clusterInstall)).ShouldNot(HaveOccurred())
				agent_day2.Spec.ClusterDeploymentName = &v1beta1.ClusterReference{Name: clusterName, Namespace: testNamespace}
				Expect(c.Update(ctx, agent_day2)).To(BeNil())

				By("Checking if the BMH does not have the detached annotation")
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))

				By("Checking the spoke BMH does not exist")
				machineName := fmt.Sprintf("%s-%s", cluster.Name, host_day2.Name)
				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host_day2.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).To(HaveOccurred())

				By("Checking the spoke Machine does not exist")
				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when agent role worker and cluster deployment is set", func() {
			It("should not create spoke BMH when agent is not installing", func() {
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
				Expect(updatedHost.ObjectMeta.Annotations).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_AGENT_IGNITION_CONFIG_OVERRIDES))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).NotTo(Equal(""))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]).To(ContainSubstring("dGVzdA=="))

				machineName := fmt.Sprintf("%s-%s", cluster.Name, host.Name)

				spokeBMH := &bmh_v1alpha1.BareMetalHost{}
				spokeClient := bmhr.spokeClient
				err = spokeClient.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeBMH)
				Expect(err).NotTo(BeNil())

				spokeMachine := &machinev1beta1.Machine{}
				err = spokeClient.Get(ctx, types.NamespacedName{Name: machineName, Namespace: OPENSHIFT_MACHINE_API_NAMESPACE}, spokeMachine)
				Expect(err).NotTo(BeNil())
			})
			It("should create spoke BMH when agent is installing", func() {
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
				agent.Status.DebugInfo.State = models.HostStatusInstallingInProgress
				Expect(c.Update(context.Background(), agent)).ShouldNot(HaveOccurred())
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
				Expect(spokeBMH.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))
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

			It("should fall back to Hypershift root CA storage - .crt name", func() {
				_, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).NotTo(HaveOccurred())
				_, err = bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(HaveOccurred())
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kube-root-ca.crt",
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
			It("should not set the detached annotation value for BMHs not labeled with an infraenv", func() {
				host.SetAnnotations(map[string]string{BMH_DETACHED_ANNOTATION: "some-other-value"})
				delete(host.Labels, BMH_INFRA_ENV_LABEL)
				Expect(c.Update(ctx, host)).To(BeNil())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("some-other-value"))
			})
			It("should set the detached annotation if agent started rebooting", func() {
				agent.Status.Progress.CurrentStage = models.HostStageRebooting
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

			It("should set the detached annotation if agent has joined cluster", func() {
				agent.Status.Progress.CurrentStage = models.HostStageJoined

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
				agent.Status.Progress.CurrentStage = models.HostStageFailed
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
				agent.Status.Progress.CurrentStage = models.HostStageFailed
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

		It("should not disable cleaning in the BMH", func() {

			host.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeMetadata
			Expect(c.Update(ctx, host)).To(BeNil())

			result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			updatedHost := &bmh_v1alpha1.BareMetalHost{}
			err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
			Expect(err).To(BeNil())
			Expect(updatedHost.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeMetadata))
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
		Context("with a provisioned agent", func() {
			var agent *v1beta1.Agent

			BeforeEach(func() {
				agentSpec := v1beta1.AgentSpec{
					Approved: true,
				}
				agent = newAgent("bmac-agent", testNamespace, agentSpec)
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
			})
			It("should set detached annotation on the BMH", func() {
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
			It("sets the detached annotation metadata with the BMH delete annotation", func() {
				host.Annotations = map[string]string{BMH_DELETE_ANNOTATION: "true"}
				Expect(c.Update(ctx, host)).To(Succeed())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(Succeed())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))

				args := &bmh_v1alpha1.DetachedAnnotationArguments{}
				Expect(json.Unmarshal([]byte(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]), &args)).To(Succeed())
				Expect(string(args.DeleteAction)).To(Equal(bmh_v1alpha1.DetachedDeleteActionDelay))
			})
			It("removes the metadata when the delete annotation is removed", func() {
				args := &bmh_v1alpha1.DetachedAnnotationArguments{
					DeleteAction: bmh_v1alpha1.DetachedDeleteActionDelay,
				}
				data, err := json.Marshal(args)
				Expect(err).To(BeNil())
				host.Annotations = map[string]string{BMH_DETACHED_ANNOTATION: string(data)}
				Expect(c.Update(ctx, host)).To(Succeed())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(Succeed())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))

				Expect(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]).To(Equal("assisted-service-controller"))
			})
			It("adds the metadata when detached annotation is already present and the delete annotation is also present", func() {
				host.Annotations = map[string]string{
					BMH_DELETE_ANNOTATION:   "true",
					BMH_DETACHED_ANNOTATION: "assisted-service-controller",
				}
				Expect(c.Update(ctx, host)).To(Succeed())

				result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(Succeed())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_DETACHED_ANNOTATION))

				args := &bmh_v1alpha1.DetachedAnnotationArguments{}
				Expect(json.Unmarshal([]byte(updatedHost.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]), &args)).To(Succeed())
				Expect(string(args.DeleteAction)).To(Equal(bmh_v1alpha1.DetachedDeleteActionDelay))
			})
			Context("when the agent is unbinding-pending-user-action", func() {
				BeforeEach(func() {
					agent.Status.Conditions = []conditionsv1.Condition{
						{
							Type:   v1beta1.BoundCondition,
							Status: corev1.ConditionTrue,
							Reason: v1beta1.UnbindingPendingUserActionReason,
						},
					}
					Expect(c.Update(ctx, agent)).To(BeNil())
				})
				It("clears customDeploy and unsets detached", func() {
					host.Annotations = map[string]string{
						BMH_DETACHED_ANNOTATION: "assisted-service-controller",
					}
					Expect(c.Update(ctx, host)).To(Succeed())

					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedHost := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(BeNil())
					Expect(updatedHost.ObjectMeta.Annotations).ToNot(HaveKey(BMH_DETACHED_ANNOTATION))
					Expect(updatedHost.Spec.CustomDeploy).To(BeNil())
				})
				It("resets customDeploy when the BMH is available", func() {
					host.Spec.CustomDeploy = nil
					host.Status.Provisioning.State = bmh_v1alpha1.StateAvailable
					Expect(c.Update(ctx, host)).To(Succeed())

					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedHost := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(BeNil())
					Expect(updatedHost.Spec.CustomDeploy).To(HaveValue(Equal(bmh_v1alpha1.CustomDeploy{Method: ASSISTED_DEPLOY_METHOD})))
				})
				It("doesn't reset customDeploy when the BMH is deprovisioning", func() {
					host.Spec.CustomDeploy = nil
					host.Status.Provisioning.State = bmh_v1alpha1.StateDeprovisioning
					Expect(c.Update(ctx, host)).To(Succeed())

					result, err := bmhr.Reconcile(ctx, newBMHRequest(host))
					Expect(err).To(BeNil())
					Expect(result).To(Equal(ctrl.Result{}))

					updatedHost := &bmh_v1alpha1.BareMetalHost{}
					Expect(c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)).To(BeNil())
					Expect(updatedHost.Spec.CustomDeploy).To(BeNil())
				})
			})
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

var _ = Describe("handleBMHFinalizer", func() {
	var (
		bmhr              *BMACReconciler
		ctx               = context.Background()
		mockCtrl          *gomock.Controller
		mockClient        *MockK8sClient
		mockDrainer       *MockDrainer
		mockClientFactory *spoke_k8s_client.MockSpokeK8sClientFactory
		bmh               *bmh_v1alpha1.BareMetalHost
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = NewMockK8sClient(mockCtrl)
		mockDrainer = NewMockDrainer(mockCtrl)
		mockClientFactory = spoke_k8s_client.NewMockSpokeK8sClientFactory(mockCtrl)
		bmhr = &BMACReconciler{
			Client:                mockClient,
			APIReader:             mockClient,
			Scheme:                scheme.Scheme,
			Log:                   common.GetTestLog(),
			ConvergedFlowEnabled:  true,
			Drainer:               mockDrainer,
			SpokeK8sClientFactory: mockClientFactory,
		}
		bmh = newBMH("testBMH", &bmh_v1alpha1.BareMetalHostSpec{})
		bmh.Annotations = map[string]string{BMH_DELETE_ANNOTATION: "true"}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("doesn't run when converged flow is not enabled", func() {
		bmhr.ConvergedFlowEnabled = false
		mockClient.EXPECT().Update(gomock.Any(), gomock.Any()).Times(0)
		result := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
		Expect(result.Stop(ctx)).To(BeFalse())
		_, err := result.Result()
		Expect(err).NotTo(HaveOccurred())
	})

	It("doesn't run when the BMH is not annotated", func() {
		bmh.Annotations = nil
		mockClient.EXPECT().Update(gomock.Any(), gomock.Any()).Times(0)
		result := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
		Expect(result.Stop(ctx)).To(BeFalse())
		_, err := result.Result()
		Expect(err).NotTo(HaveOccurred())
	})

	It("adds the finalizer to the BMH when it doesn't exist", func() {
		res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
		Expect(res.Dirty()).To(BeTrue())
		Expect(bmh.GetFinalizers()).To(ContainElement(BMH_FINALIZER_NAME))
		_, err := res.Result()
		Expect(err).NotTo(HaveOccurred())
	})

	It("doesn't update the BMH when the finalizer already exists", func() {
		bmh.ObjectMeta.Finalizers = []string{BMH_FINALIZER_NAME}
		res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
		Expect(res.Dirty()).To(BeFalse())
		_, err := res.Result()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("BMH is being deleted", func() {
		BeforeEach(func() {
			now := metav1.Now()
			bmh.ObjectMeta.Finalizers = []string{BMH_FINALIZER_NAME}
			bmh.DeletionTimestamp = &now
		})

		It("removes the finalizer and detached", func() {
			args := &bmh_v1alpha1.DetachedAnnotationArguments{
				DeleteAction: bmh_v1alpha1.DetachedDeleteActionDelay,
			}
			data, err := json.Marshal(args)
			Expect(err).To(BeNil())
			setAnnotation(&bmh.ObjectMeta, BMH_DETACHED_ANNOTATION, string(data))

			res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
			Expect(res.Dirty()).To(BeTrue())
			Expect(bmh.GetFinalizers()).NotTo(ContainElement(BMH_FINALIZER_NAME))
			Expect(bmh.GetAnnotations()).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
			_, err = res.Result()
			Expect(err).NotTo(HaveOccurred())
		})

		It("removes detached if the finalizer is already gone", func() {
			args := &bmh_v1alpha1.DetachedAnnotationArguments{
				DeleteAction: bmh_v1alpha1.DetachedDeleteActionDelay,
			}
			data, err := json.Marshal(args)
			Expect(err).To(BeNil())
			setAnnotation(&bmh.ObjectMeta, BMH_DETACHED_ANNOTATION, string(data))
			bmh.ObjectMeta.Finalizers = nil

			res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, nil)
			Expect(res.Dirty()).To(BeTrue())
			Expect(bmh.GetAnnotations()).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
			_, err = res.Result()
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a matching agent", func() {
			var (
				agent           *v1beta1.Agent
				mockSpokeClient *spoke_k8s_client.MockSpokeK8sClient
			)

			BeforeEach(func() {
				agentSpec := v1beta1.AgentSpec{
					ClusterDeploymentName: &v1beta1.ClusterReference{Name: "test-cluster", Namespace: testNamespace},
					Hostname:              "agent.example.com",
				}
				agent = newAgent("test-agent", testNamespace, agentSpec)
				mockSpokeClient = spoke_k8s_client.NewMockSpokeK8sClient(mockCtrl)
			})

			setupSpokeClient := func() {
				// mock clusterdeployment
				cdKey := types.NamespacedName{Name: agent.Spec.ClusterDeploymentName.Name, Namespace: testNamespace}
				mockClient.EXPECT().Get(ctx, cdKey, gomock.AssignableToTypeOf(&hivev1.ClusterDeployment{})).DoAndReturn(
					func(_ context.Context, key client.ObjectKey, cd *hivev1.ClusterDeployment, _ ...client.GetOption) error {
						cd.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
							AdminKubeconfigSecretRef: corev1.LocalObjectReference{Name: "clusterKubeConfig"},
						}
						return nil
					},
				)

				// mock secret
				secretKey := types.NamespacedName{Name: "clusterKubeConfig", Namespace: testNamespace}
				mockClient.EXPECT().Get(ctx, secretKey, gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(
					func(_ context.Context, key client.ObjectKey, secret *corev1.Secret, _ ...client.GetOption) error {
						secret.ObjectMeta.Labels = map[string]string{
							BackupLabel:        "true",
							WatchResourceLabel: "true",
						}
						secret.Data = map[string][]byte{"kubeconfig": []byte("definitely_a_kubeconfig")}
						return nil
					},
				)

				// mock client and clientset
				clientset := &kubernetes.Clientset{}
				mockClientFactory.EXPECT().ClientAndSetFromSecret(gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(
					func(secret *corev1.Secret) (spoke_k8s_client.SpokeK8sClient, *kubernetes.Clientset, error) {
						Expect(secret.Data["kubeconfig"]).To(Equal([]byte("definitely_a_kubeconfig")))
						return mockSpokeClient, clientset, nil
					},
				)
			}

			validateStartAnnotation := func(annotations map[string]string) {
				startString, ok := annotations[BMH_NODE_DRAIN_START_ANNOTATION]
				Expect(ok).To(BeTrue())
				_, err := time.Parse(time.RFC3339, startString)
				Expect(err).NotTo(HaveOccurred())
			}

			It("deletes an unbound agent and removes the finalizer", func() {
				agent.Spec.ClusterDeploymentName = nil
				mockClient.EXPECT().Delete(ctx, gomock.AssignableToTypeOf(&v1beta1.Agent{})).Return(nil)

				res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
				Expect(bmh.GetFinalizers()).NotTo(ContainElement(BMH_FINALIZER_NAME))
				_, err := res.Result()
				Expect(err).NotTo(HaveOccurred())
			})

			It("drains the node", func() {
				setupSpokeClient()
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: agent.Spec.Hostname},
				}
				mockSpokeClient.EXPECT().GetNode(gomock.Any(), agent.Spec.Hostname).Return(node, nil)

				mockDrainer.EXPECT().RunCordonOrUncordon(gomock.AssignableToTypeOf(&drain.Helper{}), node, true).Return(nil)
				mockDrainer.EXPECT().RunNodeDrain(gomock.AssignableToTypeOf(&drain.Helper{}), agent.Spec.Hostname).Return(nil)

				res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
				Expect(res.Dirty()).To(BeTrue())
				innerResult, err := res.Result()
				Expect(innerResult.Requeue).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				Expect(bmh.GetAnnotations()).To(HaveKeyWithValue(BMH_NODE_DRAIN_STATUS_ANNOTATION, drainStatusSuccess))
				validateStartAnnotation(bmh.GetAnnotations())
			})

			It("requeues if the drain fails", func() {
				setupSpokeClient()
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: agent.Spec.Hostname},
				}
				mockSpokeClient.EXPECT().GetNode(gomock.Any(), agent.Spec.Hostname).Return(node, nil)

				mockDrainer.EXPECT().RunCordonOrUncordon(gomock.AssignableToTypeOf(&drain.Helper{}), node, true).Return(nil)
				mockDrainer.EXPECT().RunNodeDrain(gomock.AssignableToTypeOf(&drain.Helper{}), agent.Spec.Hostname).Return(fmt.Errorf("drain failed"))

				res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
				Expect(res.Dirty()).To(BeTrue())
				innerResult, err := res.Result()
				Expect(innerResult.Requeue).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())

				Expect(bmh.GetAnnotations()).To(HaveKeyWithValue(BMH_NODE_DRAIN_STATUS_ANNOTATION, drainStatusInProgress))
				validateStartAnnotation(bmh.GetAnnotations())
			})

			It("sets an error if the node can't be found", func() {
				setupSpokeClient()
				mockSpokeClient.EXPECT().GetNode(gomock.Any(), agent.Spec.Hostname).Return(nil, fmt.Errorf("failed to find node"))

				res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
				Expect(res.Dirty()).To(BeTrue())
				innerResult, err := res.Result()
				Expect(innerResult.Requeue).To(BeFalse())
				Expect(err).To(HaveOccurred())

				annotations := bmh.GetAnnotations()
				Expect(annotations).To(HaveKeyWithValue(BMH_NODE_DRAIN_STATUS_ANNOTATION, HavePrefix("failed to drain node")))
				validateStartAnnotation(bmh.GetAnnotations())
			})

			It("doesn't run drain if the operation times out", func() {
				setAnnotation(&bmh.ObjectMeta, BMH_NODE_DRAIN_TIMEOUT_ANNOTATION, "1m")
				startTimestamp := time.Now().UTC().Add(-time.Minute * 2).Truncate(time.Second).Format(time.RFC3339)
				setAnnotation(&bmh.ObjectMeta, BMH_NODE_DRAIN_START_ANNOTATION, startTimestamp)

				res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
				Expect(res.Dirty()).To(BeTrue())
				innerResult, err := res.Result()
				Expect(innerResult.Requeue).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				Expect(bmh.GetAnnotations()).To(HaveKeyWithValue(BMH_NODE_DRAIN_STATUS_ANNOTATION, drainStatusTimeout))
			})

			Context("draining has succeeded", func() {
				BeforeEach(func() {
					setAnnotation(&bmh.ObjectMeta, BMH_NODE_DRAIN_STATUS_ANNOTATION, drainStatusSuccess)
				})

				It("sets the BMH to clean and deprovisions", func() {
					setAnnotation(&bmh.ObjectMeta, BMH_DETACHED_ANNOTATION, "assisted-service-controller")
					bmh.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
					bmh.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{Method: ASSISTED_DEPLOY_METHOD}

					res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
					Expect(res.Dirty()).To(BeTrue())
					Expect(bmh.GetAnnotations()).NotTo(HaveKey(BMH_DETACHED_ANNOTATION))
					Expect(bmh.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeMetadata))
					_, err := res.Result()
					Expect(err).NotTo(HaveOccurred())
				})

				Context("after the BMH is set for cleaning", func() {
					BeforeEach(func() {
						bmh.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeMetadata
					})

					It("annotates and deletes the agent", func() {
						mockClient.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&v1beta1.Agent{})).DoAndReturn(
							func(_ context.Context, updatedAgent *v1beta1.Agent, _ ...client.UpdateOption) error {
								Expect(updatedAgent.GetAnnotations()).To(HaveKey(BMH_FINALIZER_NAME))
								return nil
							},
						)
						mockClient.EXPECT().Delete(ctx, gomock.AssignableToTypeOf(&v1beta1.Agent{})).Return(nil)

						res := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent)
						Expect(bmh.GetFinalizers()).NotTo(ContainElement(BMH_FINALIZER_NAME))
						_, err := res.Result()
						Expect(err).NotTo(HaveOccurred())
					})

					It("fails when annotating the agent fails", func() {
						mockClient.EXPECT().Update(ctx, gomock.AssignableToTypeOf(&v1beta1.Agent{})).Return(fmt.Errorf("failed to update agent"))
						_, err := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent).Result()
						Expect(err).To(HaveOccurred())
					})

					It("fails when deleting the agent fails", func() {
						mockClient.EXPECT().Delete(ctx, gomock.AssignableToTypeOf(&v1beta1.Agent{})).Return(fmt.Errorf("agent delete failed"))
						setAnnotation(&agent.ObjectMeta, BMH_FINALIZER_NAME, "true")
						_, err := bmhr.handleBMHFinalizer(ctx, bmhr.Log, bmh, agent).Result()
						Expect(err).To(HaveOccurred())
					})
				})
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
	agent.ObjectMeta.Labels = make(map[string]string)
	agent.ObjectMeta.Labels[AgentLabelClusterDeploymentNamespace] = namespace
	if agentBMHLabel != "" {
		agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = agentBMHLabel
	}
	agent.ObjectMeta.CreationTimestamp.Time = creationTime
	return agent
}
