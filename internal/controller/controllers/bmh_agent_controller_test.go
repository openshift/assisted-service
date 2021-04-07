package controllers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/golang/mock/gomock"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("bmac reconcile", func() {
	var (
		c        client.Client
		bmhr     *BMACReconciler
		ctx      = context.Background()
		mockCtrl *gomock.Controller
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		bmhr = &BMACReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    common.GetTestLog(),
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("bmh reconcile: no labels", func() {
		host := newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())

		result, err := bmhr.Reconcile(newBMHRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	Describe("queue bmh request for agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1alpha1.Agent

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1alpha1.AgentSpec{})
			agent.Status.Inventory = v1alpha1.HostInventory{
				ReportTime: &metav1.Time{Time: time.Now()},
				Memory: v1alpha1.HostMemory{
					PhysicalBytes: 2,
				},
				Interfaces: []v1alpha1.HostInterface{
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

			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{BootMACAddress: macStr})
			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
		})

		Context("findBMH, when both agent and bmh exist,", func() {
			It("should return the agent if their MAC address matches", func() {
				result, err := bmhr.findBMH(context.Background(), agent)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(host))
			})

			It("should return nil if there is no match", func() {
				agent = newAgent("bmac-agent-no-MAC", testNamespace, v1alpha1.AgentSpec{})
				Expect(c.Create(ctx, agent)).To(BeNil())

				result, err := bmhr.findBMH(context.Background(), agent)
				Expect(err).To(BeNil())
				Expect(result).To(BeNil())
			})
		})
	})

	Describe("Reconcile a BMH with an installEnv label", func() {
		var host *bmh_v1alpha1.BareMetalHost
		BeforeEach(func() {
			host = newBMH("bmh-reconcile", &bmh_v1alpha1.BareMetalHostSpec{})
			labels := make(map[string]string)
			labels[BMH_INSTALL_ENV_LABEL] = "testInstallEnv"
			host.ObjectMeta.Labels = labels
			Expect(c.Create(ctx, host)).To(BeNil())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
		})

		Context("with a non-existing installenv", func() {
			It("should return without failures", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("with an existing installEnv without ISODownloadURL", func() {
			It("should requeue the reconcile", func() {
				installEnv := newInstallEnvImage("testInstallEnv", testNamespace, v1alpha1.InstallEnvSpec{})
				Expect(c.Create(ctx, installEnv)).To(BeNil())

				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result.RequeueAfter).To(Equal(time.Minute))
			})
		})

		Context("with an existing installEnv with ISODownloadURL", func() {
			var installEnv *v1alpha1.InstallEnv
			var isoImageURL string

			BeforeEach(func() {
				isoImageURL = "http://buzz.lightyear.io/discovery-image.iso"
				installEnv = newInstallEnvImage("testInstallEnv", testNamespace, v1alpha1.InstallEnvSpec{})
				installEnv.Status = v1alpha1.InstallEnvStatus{ISODownloadURL: isoImageURL}
				Expect(c.Create(ctx, installEnv)).To(BeNil())
			})

			AfterEach(func() {
				Expect(c.Delete(ctx, installEnv)).ShouldNot(HaveOccurred())
			})

			It("should disable the BMH hardware inspection", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.ObjectMeta.Annotations).To(HaveKey(BMH_INSPECT_ANNOTATION))
				Expect(updatedHost.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION]).To(Equal("disabled"))

			})

			It("should set the ISODownloadURL in the BMH", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err = c.Get(ctx, types.NamespacedName{Name: "bmh-reconcile", Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				Expect(updatedHost.Spec.Image.URL).To(Equal(isoImageURL))
			})
		})
	})

	Describe("Reconcile a BMH with a non-approved matching agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1alpha1.Agent

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1alpha1.AgentSpec{})
			agent.Status.Inventory = v1alpha1.HostInventory{
				Interfaces: []v1alpha1.HostInterface{
					{
						Name:       "eth0",
						MacAddress: macStr,
					},
				},
				Disks: []v1alpha1.HostDisk{
					{
						ID:                      "1",
						InstallationEligibility: v1alpha1.HostInstallationEligibility{Eligible: true},
						Path:                    "/dev/sda",
						DriveType:               "SSD",
						Bootable:                true,
					},
					{
						ID:                      "2",
						InstallationEligibility: v1alpha1.HostInstallationEligibility{Eligible: true},
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
			host.ObjectMeta.SetAnnotations(annotations)
			Expect(c.Create(ctx, host)).To(BeNil())

		})

		AfterEach(func() {
			Expect(c.Delete(ctx, host)).ShouldNot(HaveOccurred())
			Expect(c.Delete(ctx, agent)).ShouldNot(HaveOccurred())
		})

		Context("when an agent matches", func() {
			It("should not fail on missing role", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.ObjectMeta.Annotations[BMH_AGENT_ROLE] = "without-purpose"
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(newBMHRequest(updatedHost))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(string(updatedAgent.Spec.Role)).To(Equal("without-purpose"))
			})

			It("should approve it", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.Approved).To(Equal(true))
			})

			It("should add a lable referring to the bmh", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.ObjectMeta.Labels[AGENT_BMH_LABEL]).To(Equal(host.Name))
			})

			It("should set the agent spec based on the BMH annotations", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.Role).To(Equal(models.HostRoleMaster))
				Expect(updatedAgent.Spec.Hostname).To(Equal("happy-meal"))
				Expect(updatedAgent.Spec.MachineConfigPool).To(Equal("number-8"))
			})

			It("should keep InstallationDiskID as empty string if not RootDeviceHints match", func() {
				updatedHost := &bmh_v1alpha1.BareMetalHost{}
				err := c.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: testNamespace}, updatedHost)
				Expect(err).To(BeNil())
				updatedHost.Spec.RootDeviceHints.DeviceName = "/dev/sdc"
				Expect(c.Update(ctx, updatedHost)).To(BeNil())

				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal(""))
			})

			It("should set the InstallationDiskID if the RootDeviceHints were provided and match", func() {
				result, err := bmhr.Reconcile(newBMHRequest(host))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				updatedAgent := &v1alpha1.Agent{}
				err = c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, updatedAgent)
				Expect(err).To(BeNil())
				Expect(updatedAgent.Spec.InstallationDiskID).To(Equal("1"))
			})
		})
	})

	Describe("Reconcile a BMH with an approved matching agent", func() {
		var host *bmh_v1alpha1.BareMetalHost
		var agent *v1alpha1.Agent

		BeforeEach(func() {
			macStr := "12-34-56-78-9A-BC"
			agent = newAgent("bmac-agent", testNamespace, v1alpha1.AgentSpec{Approved: true})
			agent.Status.Inventory = v1alpha1.HostInventory{
				Memory: v1alpha1.HostMemory{
					PhysicalBytes: 2,
				},
				Interfaces: []v1alpha1.HostInterface{
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
				Disks: []v1alpha1.HostDisk{
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
				result, err := bmhr.Reconcile(newBMHRequest(host))
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
})
