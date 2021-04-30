/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// BMACReconciler reconciles a Agent object
type BMACReconciler struct {
	client.Client
	Log         logrus.FieldLogger
	Scheme      *runtime.Scheme
	spokeClient client.Client
}

const (
	AGENT_BMH_LABEL                     = "agent-install.openshift.io/bmh"
	BMH_AGENT_ROLE                      = "bmac.agent-install.openshift.io/role"
	BMH_AGENT_HOSTNAME                  = "bmac.agent-install.openshift.io/hostname"
	BMH_AGENT_MACHINE_CONFIG_POOL       = "bmac.agent-install.openshift.io/machine-config-pool"
	BMH_INSTALL_ENV_LABEL               = "infraenvs.agent-install.openshift.io"
	BMH_AGENT_INSTALLER_ARGS            = "bmac.agent-install.openshift.io/installer-args"
	BMH_DETACHED_ANNOTATION             = "baremetalhost.metal3.io/detached"
	BMH_INSPECT_ANNOTATION              = "inspect.metal3.io"
	BMH_HARDWARE_DETAILS_ANNOTATION     = "inspect.metal3.io/hardwaredetails"
	BMH_AGENT_IGNITION_CONFIG_OVERRIDES = "bmac.agent-install.openshift.io/ignition-config-overrides"
	MACHINE_ROLE                        = "machine.openshift.io/cluster-api-machine-role"
	MACHINE_TYPE                        = "machine.openshift.io/cluster-api-machine-type"
)

// reconcileResult is an interface that encapsulates the result of a Reconcile
// call, as returned by the action corresponding to the current state.
type reconcileResult interface {
	Result() (reconcile.Result, error)
	Dirty() bool
}

// reconcileComplete is a result indicating that the current reconcile has completed,
// and there is nothing else to do.
type reconcileComplete struct {
	dirty bool
}

func (r reconcileComplete) Result() (result reconcile.Result, err error) {
	return
}

func (r reconcileComplete) Dirty() bool {
	return r.dirty
}

// reconcileRequeue is a result indicating that the reconcile should be
// requeued.
type reconcileRequeue struct {
	delay time.Duration
}

func (r reconcileRequeue) Result() (result reconcile.Result, err error) {
	result.RequeueAfter = r.delay
	// Set Requeue true as well as RequeueAfter in case the delay is 0.
	result.Requeue = true
	return
}

func (r reconcileRequeue) Dirty() bool {
	return false
}

// reconcileError is a result indicating that an error occurred while attempting
// to advance the current reconcile, and that reconciliation should be retried.
type reconcileError struct {
	err error
}

func (r reconcileError) Result() (result reconcile.Result, err error) {
	err = r.err
	return
}

func (r reconcileError) Dirty() bool {
	return false
}

// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

func (r *BMACReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	bmh := &bmh_v1alpha1.BareMetalHost{}

	if err := r.Get(ctx, req.NamespacedName, bmh); err != nil {
		if !errors.IsNotFound(err) {
			return reconcileError{err}.Result()
		}
		return reconcileComplete{}.Result()
	}

	// Let's reconcile the BMH
	result := r.reconcileBMH(ctx, bmh)
	if result.Dirty() {
		r.Log.Debugf("Updating dirty BMH %v", bmh)
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			r.Log.WithError(err).Errorf("Error updating after BMH reconcile")
			return reconcileError{err}.Result()
		}

		return result.Result()
	}

	// Should we check the status of the BMH resource?
	// Provisioning / Deprovisioning ?
	// What happens if we do the reconcile on a BMH that
	// is in a Deprovisioning state?

	agent := r.findAgent(ctx, bmh)

	// handle multiple agents matching the
	// same BMH's Mac Address
	if agent == nil {
		return result.Result()
	}

	// if we get to this point, it means we have found an Agent that is associated
	// with the BMH being reconciled. We will call both, reconcileAgentSpec and
	// reconcileAgentInventory, every time. The logic to decide whether there's
	// any action to take is implemented in each function respectively.
	result = r.reconcileAgentInventory(bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			r.Log.WithError(err).Errorf("Error updating hardwaredetails")
			return reconcileError{err}.Result()
		}
	}

	result = r.reconcileAgentSpec(bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, agent)
		if err != nil {
			r.Log.WithError(err).Errorf("Error updating agent")
			return reconcileError{err}.Result()
		}
	}

	// After the agent has started installation, Ironic should not manage the host.
	// Adding the detached annotation to the BMH stops Ironic from managing it.
	result = r.addBMHDetachedAnnotationIfAgentHasStartedInstallation(ctx, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			r.Log.WithError(err).Errorf("Error updating BMH detached annotation")
			return reconcileError{err}.Result()
		}
	}

	result = r.reconcileSpokeBMH(ctx, bmh, agent)

	return result.Result()
}

// This reconcile step takes care of copying data from the BMH into the Agent.
//
// The Agent's spec reconcile will only happen for non-Approved agents. These are
// considered to be `new` and, at this point, the agent has not started the deployment
// yet.
//
// We are only interested in the following data:
//
// - Agent role: bmac.agent-install.openshift.io/role
// - Hostname: bmac.agent-install.openshift.io/hostname
// - Machine Config Pool: bmac.agent-install.openshift.io/machine-config-pool
//
// Unless there are errors, the agent should be `Approved` at the end of this
// reconcile and a label should be set on it referencing the BMH. No changes to
// the BMH should happen in this reconcile step.
func (r *BMACReconciler) reconcileAgentSpec(bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {

	r.Log.Debugf("Started Agent Spec reconcile for agent %s/%s and bmh %s/%s", agent.Namespace, agent.Name, bmh.Namespace, bmh.Name)

	// Do all the copying from the BMH annotations to the agent.

	annotations := bmh.ObjectMeta.GetAnnotations()
	if val, ok := annotations[BMH_AGENT_ROLE]; ok {
		agent.Spec.Role = models.HostRole(val)
	}

	if val, ok := annotations[BMH_AGENT_HOSTNAME]; ok {
		agent.Spec.Hostname = val
	}

	if val, ok := annotations[BMH_AGENT_MACHINE_CONFIG_POOL]; ok {
		agent.Spec.MachineConfigPool = val
	}

	if val, ok := annotations[BMH_AGENT_INSTALLER_ARGS]; ok {
		agent.Spec.InstallerArgs = val
	}

	if val, ok := annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]; ok {
		agent.Spec.IgnitionConfigOverrides = val
	}

	agent.Spec.Approved = true
	if agent.ObjectMeta.Labels == nil {
		agent.ObjectMeta.Labels = make(map[string]string)
	}

	// Label the agent with the reference to this BMH
	agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = bmh.Name

	// findInstallationDiskID will return an empty string
	// if no disk is found from the list. Should be find
	// to "overwrite" this value everytime as the default
	// is ""
	agent.Spec.InstallationDiskID = r.findInstallationDiskID(agent.Status.Inventory.Disks, bmh.Spec.RootDeviceHints)

	return reconcileComplete{true}
}

// The detached annotation is added if the installation of the agent associated with
// the host has started.
func (r *BMACReconciler) addBMHDetachedAnnotationIfAgentHasStartedInstallation(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {

	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	// Annotation already exists
	if _, ok := bmhAnnotations[BMH_DETACHED_ANNOTATION]; ok {
		return reconcileComplete{}
	}

	if agent.Status.Conditions == nil {
		return reconcileComplete{}
	}
	installConditionReason := conditionsv1.FindStatusCondition(agent.Status.Conditions, InstalledCondition).Reason

	// Do nothing if InstalledCondition is not in Installed, InProgress, or Failed
	if installConditionReason != InstallationInProgressReason &&
		installConditionReason != InstalledReason &&
		installConditionReason != InstallationFailedReason {
		return reconcileComplete{}
	}

	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}

	bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "true"

	return reconcileComplete{true}
}

// Reconcile BMH's HardwareDetails using the agent's inventory
//
// Here we will copy as much data from the Agent's Inventory as possible
// into the `inspect.metal3.io/hardwaredetails` annotation in the BMH.
//
// This will trigger a reconcile on the BMH side, resulting in this data
// being copied from the annotation into the BMH's HardwareDetails status.
//
//
// Care must be taken to only update the data when really needed. Doing an update
// on every BMAC reconcile will trigger an infinite loop of reconciles between
// BMAC and the BMH reconcile as the former will update the hardwaredetails annotation
// while the latter will continue to update the status.
func (r *BMACReconciler) reconcileAgentInventory(bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	// This check should be updated. We should check the
	// agent status instead.
	if len(agent.Status.Inventory.Interfaces) == 0 {
		return reconcileComplete{}
	}

	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	if _, ok := bmhAnnotations[BMH_HARDWARE_DETAILS_ANNOTATION]; ok {
		return reconcileComplete{}
	}

	// Check whether HardwareDetails has been set already. This annotation
	// status should only be set through this Reconcile in this scenario.
	if bmh.Status.HardwareDetails != nil {
		return reconcileComplete{}
	}

	inventory := agent.Status.Inventory
	hardwareDetails := bmh_v1alpha1.HardwareDetails{}

	// Add the interfaces
	for _, iface := range inventory.Interfaces {
		// BMH handles dual stack in a different way, it requires adding
		// multiple NICs, same mac and name, with different IPs.
		// reference: https://github.com/metal3-io/baremetal-operator/pull/758
		for _, ip := range append(iface.IPV6Addresses, iface.IPV4Addresses...) {
			hardwareDetails.NIC = append(hardwareDetails.NIC, bmh_v1alpha1.NIC{
				IP:        ip,
				Name:      iface.Name,
				Model:     iface.Vendor,
				MAC:       iface.MacAddress,
				SpeedGbps: int(iface.SpeedMbps / 1024),
			})
		}
	}

	// Add storage
	for _, d := range inventory.Disks {
		// missing WWNVendorExtension, WWNWithExtension
		disk := bmh_v1alpha1.Storage{
			Name:         d.Path,
			HCTL:         d.Hctl,
			Model:        d.Model,
			SizeBytes:    bmh_v1alpha1.Capacity(d.SizeBytes),
			SerialNumber: d.Serial,
			WWN:          d.Wwn,
			Vendor:       d.Vendor,
			Rotational:   strings.EqualFold(d.DriveType, "hdd"),
		}

		hardwareDetails.Storage = append(hardwareDetails.Storage, disk)
	}

	// Add the memory information in MebiByte
	if agent.Status.Inventory.Memory.PhysicalBytes > 0 {
		hardwareDetails.RAMMebibytes = int(conversions.BytesToGiB(inventory.Memory.PhysicalBytes))
	}

	// Add CPU information
	hardwareDetails.CPU = bmh_v1alpha1.CPU{
		Flags:          inventory.Cpu.Flags,
		Count:          int(inventory.Cpu.Count),
		Model:          inventory.Cpu.ModelName,
		Arch:           inventory.Cpu.Architecture,
		ClockMegahertz: bmh_v1alpha1.ClockSpeed(inventory.Cpu.ClockMegahertz),
	}

	// BMH has a `virtual` field that the Agent does not have
	hardwareDetails.SystemVendor = bmh_v1alpha1.HardwareSystemVendor{
		Manufacturer: inventory.SystemVendor.Manufacturer,
		ProductName:  inventory.SystemVendor.ProductName,
		SerialNumber: inventory.SystemVendor.SerialNumber,
	}

	// Add hostname
	hardwareDetails.Hostname = agent.Status.Inventory.Hostname

	bytes, err := json.Marshal(hardwareDetails)
	if err != nil {
		return reconcileError{err}
	}

	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}

	bmh.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION] = string(bytes)
	return reconcileComplete{true}

}

// Reconcile the `BareMetalHost` resource
//
// This reconcile step sets the Image.URL value in the `BareMetalHost`
// spec by copying it from the `InfraEnv` referenced in the resource's
// labels. If the previous action succeeds, this step will also set the
// BMH_INSPECT_ANNOTATION to disabled on the BareMetalHost.
//
// The above changes will be done only if the ISODownloadURL value has already
// been set in the `InfraEnv` resource and the Image.URL value has not been
// set in the `BareMetalHost`
func (r *BMACReconciler) reconcileBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) reconcileResult {
	// No need to reconcile if the image URL has been set in
	// the BMH already.
	if bmh.Spec.Image != nil && bmh.Spec.Image.URL != "" {
		return reconcileComplete{}
	}

	r.Log.Debugf("Started BMH reconcile for %s/%s", bmh.Namespace, bmh.Name)
	r.Log.Debugf("BMH value %v", bmh)

	for ann, value := range bmh.Labels {
		r.Log.Debugf("BMH label %s value %s", ann, value)

		// Find the `BMH_INSTALL_ENV_LABEL`, get the infraEnv configured in it
		// and copy the ISO Url from the InfraEnv to the BMH resource.
		if ann == BMH_INSTALL_ENV_LABEL {
			infraEnv := &aiv1beta1.InfraEnv{}
			// TODO: Watch for the InfraEnv resource and do the reconcile
			// when the required data is there.
			// https://github.com/openshift/assisted-service/pull/1279/files#r604213425

			r.Log.Debugf("Loading InfraEnv %s", value)
			if err := r.Get(ctx, types.NamespacedName{Name: value, Namespace: bmh.Namespace}, infraEnv); err != nil {
				r.Log.WithError(err).Errorf("failed to get infraEnv resource %s/%s", bmh.Namespace, value)
				return reconcileError{client.IgnoreNotFound(err)}
			}

			if infraEnv.Status.ISODownloadURL == "" {
				// the image has not been created yet, try later.
				r.Log.Infof("Image URL for InfraEnv (%s/%s) not available yet. Retrying reconcile for BareMetalHost  %s/%s",
					infraEnv.Namespace, infraEnv.Name, bmh.Namespace, bmh.Name)
				return reconcileRequeue{time.Minute}
			}

			r.Log.Debugf("Setting attributes in BMH")
			// We'll just overwrite this at this point
			// since the nullness and emptyness checks
			// are done at the beginning of this function.
			bmh.Spec.Image = &bmh_v1alpha1.Image{}
			liveIso := "live-iso"
			bmh.Spec.Image.URL = infraEnv.Status.ISODownloadURL
			bmh.Spec.Image.DiskFormat = &liveIso

			bmh.Spec.AutomatedCleaningMode = "disabled"
			bmh.Spec.Online = true

			// Let's make sure inspection is disabled for BMH resources
			// that are associated with an agent-based deployment.
			//
			// Ideally, the user would do this while creating the BMH
			// we are just taking extra care that this is the case.
			if bmh.ObjectMeta.Annotations == nil {
				bmh.ObjectMeta.Annotations = make(map[string]string)
			}

			bmh.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION] = "disabled"

			// This state is reached if the BMH was part of a
			// previous cluster installation. At that point
			// the detached annotation was added and set to true.
			// Now the user would like to reinstall or use the BMH
			// in another installation. The user has removed bmh.Spec.Image
			// thus triggering this code path again. The annotation
			// now needs to be removed so that Ironic can manage the host.
			delete(bmh.ObjectMeta.Annotations, BMH_DETACHED_ANNOTATION)

			r.Log.Infof("Image URL has been set in the BareMetalHost  %s/%s", bmh.Namespace, bmh.Name)
			return reconcileComplete{true}
		}
	}

	return reconcileComplete{}
}

// Reconcile the `BareMetalHost` resource on the spoke cluster
//
// Baremetal-operator in the hub cluster creates a host using the live-iso feature. To add this host as a worker node
// to an existing spoke cluster, the following things need to be done:
// - Check agent to see if day2 flow needs to be triggered.
// - Get the kubeconfig client from secret data
// - Creates a new Machine
// - Create BMH with externallyProvisioned set to true and set the newly created machine as ConsumerRef
// BMH_HARDWARE_DETAILS_ANNOTATION is needed for auto approval of the CSR.
func (r *BMACReconciler) reconcileSpokeBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	// Only worker role is supported for day2 operation
	if agent.Spec.Role != models.HostRoleWorker || agent.Spec.ClusterDeploymentName == nil {
		r.Log.Debugf("Skipping spoke BareMetalHost reconcile for  agent %s/%s, role %s and clusterDeployment %s.", agent.Namespace, agent.Name, agent.Spec.Role, agent.Spec.ClusterDeploymentName)
		return reconcileComplete{}
	}

	cdKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, cdKey, clusterDeployment); err != nil {
		r.Log.WithError(err).Errorf("failed to get clusterDeployment resource %s/%s", cdKey.Namespace, cdKey.Name)
		return reconcileError{err}
	}

	// If the cluster is not installed yet, we can't get kubeconfig for the cluster yet.
	if !clusterDeployment.Spec.Installed {
		r.Log.Infof("ClusterDeployment %s/%s for agent %s/%s is not installed yet", clusterDeployment.Namespace, clusterDeployment.Name, agent.Namespace, agent.Name)
		// TODO: If cluster is not Installed, wait until a reconcile is trigged by a watch event instead
		return reconcileComplete{}
	}

	// Secret contains kubeconfig for the spoke cluster
	secret := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, clusterDeployment.Name)
	err := r.Get(ctx, types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: name}, secret)
	if err != nil && errors.IsNotFound(err) {
		r.Log.WithError(err).Errorf("failed to get secret resource %s/%s", clusterDeployment.Namespace, name)
		// TODO: If secret is not found, wait until a reconcile is trigged by a watch event instead
		return reconcileComplete{}
	} else if err != nil {
		return reconcileError{err}
	}

	spokeClient, err := r.getSpokeClient(secret)
	if err != nil {
		r.Log.WithError(err).Errorf("failed to create spoke kubeclient")
		return reconcileError{err}
	}

	machine, err := r.ensureSpokeMachine(ctx, spokeClient, bmh, clusterDeployment)
	if err != nil {
		r.Log.WithError(err).Errorf("failed to create or update spoke Machine")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeBMHSecret(ctx, spokeClient, bmh)
	if err != nil {
		r.Log.WithError(err).Errorf("failed to create or update spoke BareMetalHost Secret")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeBMH(ctx, spokeClient, bmh, machine, agent)
	if err != nil {
		r.Log.WithError(err).Errorf("failed to create or update spoke BareMetalHost")
		return reconcileError{err}
	}

	return reconcileComplete{}
}

// Finds the installation disk based on the RootDeviceHints
//
// This function implements the logic to find the installation disk following what's currently
// supported by OpenShift, instead of *all* the supported cases in Ironic. The following link
// points to the RootDeviceDisk translation done by the BareMetal Operator that is then sent to
// Ironic:
// https://github.com/metal3-io/baremetal-operator/blob/dbe8780ad14f53132ba606d1baec808997febe49/pkg/provisioner/ironic/devicehints/devicehints.go#L11-L54
//
// The logic is quite straightforward and the checks done match what is in the aforementioned link.
// Some string checks require equality, others partial equality, whereas the int checks require numeric comparison.
//
// Ironic's internal filter process requires that all the disks have to fully match the RootDeviceHints (and operation),
// which is what this function does.
//
// This function also filters out disks that are not elegible for installation, as we already know those cannot be used.
func (r *BMACReconciler) findInstallationDiskID(devices []aiv1beta1.HostDisk, hints *bmh_v1alpha1.RootDeviceHints) string {
	if hints == nil {
		return ""
	}

	for _, disk := range devices {

		if !disk.InstallationEligibility.Eligible {
			continue
		}

		if hints.DeviceName != "" && hints.DeviceName != disk.Path {
			continue
		}

		if hints.HCTL != "" && hints.HCTL != disk.Hctl {
			continue
		}

		if hints.Model != "" && !strings.Contains(disk.Model, hints.Model) {
			continue
		}

		if hints.Vendor != "" && !strings.Contains(disk.Vendor, hints.Model) {
			continue
		}

		if hints.SerialNumber != "" && hints.SerialNumber != disk.Serial {
			continue
		}

		if hints.MinSizeGigabytes != 0 {
			sizeGB := int(disk.SizeBytes / (1024 * 3))
			if hints.MinSizeGigabytes < sizeGB {
				continue
			}
		}

		if hints.WWN != "" && hints.WWN != disk.Wwn {
			continue
		}

		// No WWNWithExtension
		// if hints.WWWithExtension != "" && hints.WWWithExtension != disk.Wwwithextension {
		// 	return ""
		// }

		// No WWNNVendorExtension
		// if hints.WWNVendorExtension != "" && hints.WWNVendorExtension != disk.WwnVendorextension {
		// 	return ""
		// }

		switch {
		case hints.Rotational == nil:
		case *hints.Rotational:
			if !strings.EqualFold(disk.DriveType, "hdd") {
				continue
			}
		case !*hints.Rotational:
			if strings.EqualFold(disk.DriveType, "hdd") {
				continue
			}
		}

		return disk.ID
	}

	return ""
}

// Finds the agents related to this BMH
//
// The BMH <-> agent relation should be one-to-one. This
// function returns the agent that matches the following
// criteria.
//
// An agent is only related to the BMH if it has an iface
// whose MacAddress matches the BootMACAddress set in the BMH.
//
// In the scenario where more than one Agent match the BootMACAdress
// from the BMH, this function will return the newest Agent. Using the
// CreationTimestamp is authoritative for this scenario because the
// newest agent will be the latest agent registered, and therefore the
// active one.
//
// `nil` will be returned if no agent matches
func (r *BMACReconciler) findAgent(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) *aiv1beta1.Agent {
	agentList := aiv1beta1.AgentList{}
	err := r.Client.List(ctx, &agentList, client.InNamespace(bmh.Namespace))
	if err != nil {
		return nil
	}

	agents := []*aiv1beta1.Agent{}
	for i, agent := range agentList.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" && strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				agents = append(agents, &agentList.Items[i])
			}
		}
	}

	if len(agents) == 0 {
		return nil
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ObjectMeta.CreationTimestamp.After(agents[j].ObjectMeta.CreationTimestamp.Time)
	})

	return agents[0]
}

// Find `BareMetalHost` resources that match an agent
//
// Only `BareMetalHost` resources that match one of the Agent's
// MAC addresses will be returned.
func (r *BMACReconciler) findBMH(ctx context.Context, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhList := bmh_v1alpha1.BareMetalHostList{}
	err := r.Client.List(ctx, &bmhList, client.InNamespace(agent.Namespace))
	if err != nil {
		return nil, err
	}

	for _, bmh := range bmhList.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if agentInterface.MacAddress != "" && strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				return &bmh, nil
			}
		}
	}
	return nil, nil
}

func (r *BMACReconciler) ensureSpokeBMH(ctx context.Context, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, machine *machinev1beta1.Machine, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhSpoke, mutateFn := r.newSpokeBMH(bmh, machine, agent)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, bmhSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Spoke BareMetalHost created")
	}
	return bmhSpoke, nil
}

func (r *BMACReconciler) ensureSpokeMachine(ctx context.Context, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, clusterDeployment *hivev1.ClusterDeployment) (*machinev1beta1.Machine, error) {
	machineSpoke, mutateFn := r.newSpokeMachine(bmh, clusterDeployment)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, machineSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Spoke Machine created")
	}
	return machineSpoke, nil
}

func (r *BMACReconciler) ensureSpokeBMHSecret(ctx context.Context, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost) (*corev1.Secret, error) {
	secretName := bmh.Spec.BMC.CredentialsName
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: bmh.Namespace, Name: secretName}, secret)
	if err != nil {
		return secret, err
	}
	secretSpoke, mutateFn := r.newSpokeBMHSecret(secret, bmh)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, secretSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		r.Log.Info("Spoke BareMetalHost Secret created")
	}
	return secretSpoke, nil
}

func (r *BMACReconciler) newSpokeBMH(bmh *bmh_v1alpha1.BareMetalHost, machine *machinev1beta1.Machine, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, controllerutil.MutateFn) {
	bmhSpoke := &bmh_v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		},
	}
	mutateFn := func() error {
		bmhSpoke.Spec = bmh.Spec
		// The host is created by the baremetal operator on hub cluster. So BMH on the spoke cluster needs
		// to be set to externally provisioned
		bmhSpoke.Spec.ExternallyProvisioned = true
		// If the Image field is filled in, ExternallyProvisioned is ignored. So remove the Image field from spec
		bmhSpoke.Spec.Image = &bmh_v1alpha1.Image{}
		bmhSpoke.Spec.ConsumerRef = &corev1.ObjectReference{
			Name:      machine.Name,
			Namespace: machine.Namespace,
		}
		// copy annotations. hardwaredetails annotations is needed for automatic csr approval
		bmhSpoke.ObjectMeta.Annotations = bmh.ObjectMeta.Annotations
		// HardwareDetails annotation needs a special case. The annotation gets removed once it's consumed by the baremetal operator
		// and data is copied to the bmh status. So we are reconciling the annotation from the agent status inventory.
		// If HardwareDetails annotation is already copied from hub bmh.annotation above, this won't overwrite it.
		if _, err := r.reconcileAgentInventory(bmhSpoke, agent).Result(); err != nil {
			return err
		}
		return nil
	}

	return bmhSpoke, mutateFn
}

func (r *BMACReconciler) newSpokeBMHSecret(secret *corev1.Secret, bmh *bmh_v1alpha1.BareMetalHost) (*corev1.Secret, controllerutil.MutateFn) {
	secretSpoke := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: bmh.Namespace,
		},
	}
	mutateFn := func() error {
		secretSpoke.Data = secret.Data
		return nil
	}

	return secretSpoke, mutateFn
}

func (r *BMACReconciler) newSpokeMachine(bmh *bmh_v1alpha1.BareMetalHost, clusterDeployment *hivev1.ClusterDeployment) (*machinev1beta1.Machine, controllerutil.MutateFn) {
	machineName := fmt.Sprintf("%s-%s", clusterDeployment.Name, bmh.Name)
	machine := &machinev1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: bmh.Namespace,
		},
	}
	mutateFn := func() error {
		// Setting the same labels as the rest of the machines in the spoke cluster
		machine.Labels = AddLabel(machine.Labels, machinev1beta1.MachineClusterIDLabel, clusterDeployment.Name)
		machine.Labels = AddLabel(machine.Labels, MACHINE_ROLE, string(models.HostRoleWorker))
		machine.Labels = AddLabel(machine.Labels, MACHINE_TYPE, string(models.HostRoleWorker))
		return nil
	}

	return machine, mutateFn
}

func (r *BMACReconciler) getSpokeClient(secret *corev1.Secret) (client.Client, error) {
	var err error
	if r.spokeClient != nil {
		return r.spokeClient, err
	}
	r.spokeClient, err = getSpokeClient(secret)
	return r.spokeClient, err
}

func (r *BMACReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapAgentToClusterDeployment := func(a client.Object) []reconcile.Request {
		ctx := context.Background()
		agent := &aiv1beta1.Agent{}

		if err := r.Get(ctx, types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}, agent); err != nil {
			return []reconcile.Request{}
		}

		// No need to list all the `BareMetalHost` resources if the agent
		// already has the reference to one of them.
		if val, ok := agent.ObjectMeta.Labels[AGENT_BMH_LABEL]; ok {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{
				Namespace: agent.Namespace,
				Name:      val,
			}}}
		}

		bmh, err := r.findBMH(ctx, agent)
		if bmh == nil || err != nil {
			return []reconcile.Request{}
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}}}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&bmh_v1alpha1.BareMetalHost{}).
		Watches(&source.Kind{Type: &aiv1beta1.Agent{}}, handler.EnqueueRequestsFromMapFunc(mapAgentToClusterDeployment)).
		Complete(r)
}
