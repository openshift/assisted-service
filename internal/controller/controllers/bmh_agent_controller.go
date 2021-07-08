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
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	logutil "github.com/openshift/assisted-service/pkg/log"
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
	BMH_INFRA_ENV_LABEL                 = "infraenvs.agent-install.openshift.io"
	BMH_AGENT_INSTALLER_ARGS            = "bmac.agent-install.openshift.io/installer-args"
	BMH_DETACHED_ANNOTATION             = "baremetalhost.metal3.io/detached"
	BMH_INSPECT_ANNOTATION              = "inspect.metal3.io"
	BMH_HARDWARE_DETAILS_ANNOTATION     = "inspect.metal3.io/hardwaredetails"
	BMH_AGENT_IGNITION_CONFIG_OVERRIDES = "bmac.agent-install.openshift.io/ignition-config-overrides"
	MACHINE_ROLE                        = "machine.openshift.io/cluster-api-machine-role"
	MACHINE_TYPE                        = "machine.openshift.io/cluster-api-machine-type"
)

var (
	InfraEnvImageCooldownPeriod = 60 * time.Second
)

// reconcileResult is an interface that encapsulates the result of a Reconcile
// call, as returned by the action corresponding to the current state.
//
// Set the `dirty` flag when the BMH CR (or any CR) has been modified and an `Update`
// is required.
//
// Set the `stop` flag when the `Reconcile` flow should be stopped. For example, the
// required data for the current step is not ready yet. This will prevent the Reconcile
// from going to the next step.
type reconcileResult interface {
	Result() (reconcile.Result, error)
	Dirty() bool
	Stop(ctx context.Context) bool
}

// reconcileComplete is a result indicating that the current reconcile has completed,
// and there is nothing else to do. It allows for setting the implementation or the
// stop flag.
type reconcileComplete struct {
	dirty bool
	stop  bool
}

func (r reconcileComplete) Result() (result reconcile.Result, err error) {
	return
}

func (r reconcileComplete) Dirty() bool {
	return r.dirty
}

func (r reconcileComplete) Stop(ctx context.Context) bool {
	return r.stop || ctx.Err() != nil
}

type reconcileRequeue struct {
	requeueAfter time.Duration
}

func (r reconcileRequeue) Result() (result reconcile.Result, err error) {
	result = reconcile.Result{
		Requeue:      true,
		RequeueAfter: r.requeueAfter,
	}
	return result, err
}

func (r reconcileRequeue) Dirty() bool {
	return false
}

func (r reconcileRequeue) Stop(ctx context.Context) bool {
	return true
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

func (r reconcileError) Stop(ctx context.Context) bool {
	return true
}

// +kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

func (r *BMACReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"bare_metal_host":           req.Name,
			"bare_metal_host_namespace": req.Namespace,
		})

	defer func() {
		log.Info("BareMetalHost Reconcile ended")
	}()

	log.Info("BareMetalHost Reconcile started")
	bmh := &bmh_v1alpha1.BareMetalHost{}

	if err := r.Get(ctx, req.NamespacedName, bmh); err != nil {
		if !errors.IsNotFound(err) {
			return reconcileError{err}.Result()
		}
		return reconcileComplete{}.Result()
	}

	// Let's reconcile the BMH
	result := r.reconcileBMH(ctx, log, bmh)
	if result.Dirty() {
		log.Debugf("Updating dirty BMH %v", bmh)
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			log.WithError(err).Errorf("Error updating after BMH reconcile")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
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
			log.WithError(err).Errorf("Error updating hardwaredetails")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
		return result.Result()
	}

	result = r.reconcileAgentSpec(log, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, agent)
		if err != nil {
			log.WithError(err).Errorf("Error updating agent")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
		return result.Result()
	}

	// After the agent has started installation, Ironic should not manage the host.
	// Adding the detached annotation to the BMH stops Ironic from managing it.
	result = r.addBMHDetachedAnnotationIfAgentHasStartedInstallation(ctx, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			log.WithError(err).Errorf("Error updating BMH detached annotation")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
		return result.Result()
	}

	result = r.reconcileSpokeBMH(ctx, log, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			log.WithError(err).Errorf("Error adding BMH detached annotation after creating spoke BMH")
			return reconcileError{err}.Result()
		}
	}

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
func (r *BMACReconciler) reconcileAgentSpec(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {

	log.Debugf("Started Agent Spec reconcile for agent %s/%s and bmh %s/%s", agent.Namespace, agent.Name, bmh.Namespace, bmh.Name)

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

	return reconcileComplete{dirty: true}
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
	installConditionReason := conditionsv1.FindStatusCondition(agent.Status.Conditions, aiv1beta1.InstalledCondition).Reason

	// Do nothing if InstalledCondition is not in Installed, InProgress, or Failed
	if installConditionReason != aiv1beta1.InstallationInProgressReason &&
		installConditionReason != aiv1beta1.InstalledReason &&
		installConditionReason != aiv1beta1.InstallationFailedReason {
		return reconcileComplete{}
	}

	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}

	bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "assisted-service-controller"

	return reconcileComplete{dirty: true}
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
	return reconcileComplete{dirty: true}

}

// Utility to verify whether a BMH should be reconciled based on the InfraEnv
//
// This function verifies the following things:
//
// 1. InfraEnv exists
// 2. InfraEnv's ISODownloadURL exists
// 3. The BMH.Spec.URL is the same to the InfraEnv's ISODownload URL
// 4. The InfraEnv's ISO is at least 60 seconds old
//
// If all the checks above are true, then the reconcile won't happen
// and the reconcileBMH function will return.
//
// Note that this function is also used to decide whether a BMH reconcile should be queued
// or not. This will help queuing fewer reconciles when we know the BMH is not ready.
//
// The function returns 2 booleans, time.Duration and a string:
//
// 1. Should we proceed with the BMH's reconcile
// 2. Should the full `Reconcile` stop as well
// 3. If requeueing is needed, for how long should we back off
// 4. If reconciling should not continue, a reason that will be printed in the log
//
// TODO: This function should return `reconcileResult` or some other interface suitable
//       to contain multiple informations instead of a bunch of variables that are later on
//       separately interpreted.
func shouldReconcileBMH(bmh *bmh_v1alpha1.BareMetalHost, infraEnv *aiv1beta1.InfraEnv) (bool, bool, time.Duration, string) {
	// Stop `Reconcile` if BMH does not have an InfraEnv.
	if infraEnv == nil {
		return false, false, 0, "BMH does not have an InfraEnv"
	}

	// This is a separate check because an existing
	// InfraEnv with an empty ISODownloadURL means the
	// global `Reconcile` function should also return
	// as there is nothing left to do for it. Note that we
	// do not have to explicitly requeue here. As soon as
	// the InfraEnv gets its ISO, reconciliation will be
	// triggered because of the watcher on the InfraEnv CR.
	if infraEnv.Status.ISODownloadURL == "" {
		return false, true, 0, "InfraEnv corresponding to the BMH has no image URL available."
	}

	// The Image URL exists and InfraEnv's URL has not changed
	// nothing else to do.
	if bmh.Spec.Image != nil && bmh.Spec.Image.URL == infraEnv.Status.ISODownloadURL {
		return false, false, 0, "BMH and InfraEnv images are in sync. Nothing to update."
	}

	// The image has been created sooner than the specified cooldown period
	imageTime := infraEnv.Status.CreatedTime.Time.Add(InfraEnvImageCooldownPeriod)
	if imageTime.After(time.Now()) {
		return false, false, time.Until(imageTime), "InfraEnv image is too recent. Requeuing and retrying again soon."
	}

	return true, false, 0, ""
}

func (r *BMACReconciler) findInfraEnvForBMH(ctx context.Context, log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost) (*aiv1beta1.InfraEnv, error) {
	for ann, value := range bmh.Labels {
		log.Debugf("BMH label %s value %s", ann, value)

		// Find the `BMH_INFRA_ENV_LABEL`, get the infraEnv configured in it
		// and copy the ISO Url from the InfraEnv to the BMH resource.
		if ann == BMH_INFRA_ENV_LABEL {
			infraEnv := &aiv1beta1.InfraEnv{}

			log.Debugf("Loading InfraEnv %s", value)
			if err := r.Get(ctx, types.NamespacedName{Name: value, Namespace: bmh.Namespace}, infraEnv); err != nil {
				log.WithError(err).Errorf("failed to get infraEnv resource %s/%s", bmh.Namespace, value)
				return nil, client.IgnoreNotFound(err)
			}

			return infraEnv, nil
		}
	}

	return nil, nil
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
func (r *BMACReconciler) reconcileBMH(ctx context.Context, log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost) reconcileResult {
	log.Debugf("Started BMH reconcile for %s/%s", bmh.Namespace, bmh.Name)
	log.Debugf("BMH value %v", bmh)

	// A detached BMH is considered to be unmanaged by the hub
	// cluster and, therefore, BMAC reconciles on this BMH should
	// not happen.
	//
	// User is expected to remove the `detached` annotation manually
	// to bring this BMH back into the pool of reconciled BMH resources.
	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	if _, ok := bmhAnnotations[BMH_DETACHED_ANNOTATION]; ok {
		log.Debugf("Stopped BMH reconcile for %s/%s because it has been detached", bmh.Namespace, bmh.Name)
		return reconcileComplete{stop: true}
	}

	infraEnv, err := r.findInfraEnvForBMH(ctx, log, bmh)

	if err != nil {
		return reconcileError{err}
	}

	dirty := false
	annotations := bmh.ObjectMeta.GetAnnotations()

	// Set the following parameters regardless of the state
	// of the InfraEnv and the BMH. There is no need for
	// inspection and cleaning to happen out of assisted
	// service's loop.
	if _, ok := annotations[BMH_INSPECT_ANNOTATION]; !ok || bmh.Spec.AutomatedCleaningMode != bmh_v1alpha1.CleaningModeDisabled {
		bmh.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled

		// Let's make sure inspection is disabled for BMH resources
		// that are associated with an agent-based deployment.
		//
		// Ideally, the user would do this while creating the BMH
		// we are just taking extra care that this is the case.
		if bmh.ObjectMeta.Annotations == nil {
			bmh.ObjectMeta.Annotations = make(map[string]string)
		}

		bmh.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION] = "disabled"
		dirty = true
	}

	proceed, stopReconcileLoop, requeuePeriod, reason := shouldReconcileBMH(bmh, infraEnv)

	if !proceed {
		if requeuePeriod != 0 {
			log.Infof("Requeuing reconcileBMH: %s", reason)
			return reconcileRequeue{requeueAfter: requeuePeriod}
		}
		log.Infof("Stopping reconcileBMH: %s", reason)
		return reconcileComplete{dirty: dirty, stop: stopReconcileLoop}
	}

	// Set the bmh.Spec.Image field to nil if the BMH is in a non-ready state.
	// This will make sure that any existing image will be de-provisioned first
	// and, therefore, removed from Ironic's cache. The deprovision step is critical
	// to guarantee that Ironic will fetch the latest version of the image.
	if bmh.Status.Provisioning.State != bmh_v1alpha1.StateReady && bmh.Status.Provisioning.State != bmh_v1alpha1.StateAvailable {
		log.Infof("Removing BMH image field to trigger Ironic image refresh")
		bmh.Spec.Image = nil
		return reconcileComplete{stop: true, dirty: true}
	}

	r.Log.Debugf("Setting attributes in BMH")
	// We'll just overwrite this at this point
	// since the nullness and emptyness checks
	// are done at the beginning of this function.
	bmh.Spec.Image = &bmh_v1alpha1.Image{}
	liveIso := "live-iso"
	bmh.Spec.Online = true
	bmh.Spec.Image.URL = infraEnv.Status.ISODownloadURL
	bmh.Spec.Image.DiskFormat = &liveIso

	r.Log.Infof("Image URL has been set in the BareMetalHost  %s/%s", bmh.Namespace, bmh.Name)
	return reconcileComplete{dirty: true, stop: true}
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
func (r *BMACReconciler) reconcileSpokeBMH(ctx context.Context, log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	// Only worker role is supported for day2 operation
	if agent.Spec.Role != models.HostRoleWorker || agent.Spec.ClusterDeploymentName == nil {
		log.Debugf("Skipping spoke BareMetalHost reconcile for  agent %s/%s, role %s and clusterDeployment %s.", agent.Namespace, agent.Name, agent.Spec.Role, agent.Spec.ClusterDeploymentName)
		return reconcileComplete{}
	}

	cdKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, cdKey, clusterDeployment); err != nil {
		log.WithError(err).Errorf("failed to get clusterDeployment resource %s/%s", cdKey.Namespace, cdKey.Name)
		return reconcileError{err}
	}

	// If the cluster is not installed yet, we can't get kubeconfig for the cluster yet.
	if !clusterDeployment.Spec.Installed {
		log.Infof("ClusterDeployment %s/%s for agent %s/%s is not installed yet", clusterDeployment.Namespace, clusterDeployment.Name, agent.Namespace, agent.Name)
		// If cluster is not Installed, wait until a reconcile is trigged by a ClusterDeployment watch event instead
		return reconcileComplete{}
	}

	// Secret contains kubeconfig for the spoke cluster
	secret := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, clusterDeployment.Name)
	err := r.Get(ctx, types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: name}, secret)
	if err != nil && errors.IsNotFound(err) {
		log.WithError(err).Errorf("failed to get secret resource %s/%s", clusterDeployment.Namespace, name)
		// TODO: If secret is not found, wait until a reconcile is trigged by a watch event instead
		return reconcileComplete{}
	} else if err != nil {
		return reconcileError{err}
	}

	spokeClient, err := r.getSpokeClient(secret)
	if err != nil {
		log.WithError(err).Errorf("failed to create spoke kubeclient")
		return reconcileError{err}
	}

	machine, err := r.ensureSpokeMachine(ctx, log, spokeClient, bmh, clusterDeployment)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke Machine")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeBMHSecret(ctx, log, spokeClient, bmh)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke BareMetalHost Secret")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeBMH(ctx, log, spokeClient, bmh, machine, agent)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke BareMetalHost")
		return reconcileError{err}
	}

	// Add detached annotation to hub BMH so that Ironic stops managing the host from
	// the hub cluster. After spoke BMH is created, the host will be managed by the spoke
	// cluster.
	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	if _, ok := bmhAnnotations[BMH_DETACHED_ANNOTATION]; !ok {
		if bmh.ObjectMeta.Annotations == nil {
			bmh.ObjectMeta.Annotations = make(map[string]string)
		}
		bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "assisted-service-controller"
		return reconcileComplete{dirty: true}
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

// Finds the agents related to this ClusterDeployment
//
// The ClusterDeployment <-> agent relation is one-to-many.
// This function returns all Agents whose ClusterDeploymentName name
// matches this ClusterDeployment's name.
func (r *BMACReconciler) findAgentsByClusterDeployment(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) []*aiv1beta1.Agent {
	agentList := aiv1beta1.AgentList{}
	err := r.Client.List(ctx, &agentList)
	if err != nil {
		return []*aiv1beta1.Agent{}
	}

	// There may be more than one Agent that maps to the same BMH
	// if the cluster deployment had previous failed installations.
	// Only the newest agent for each BMH is returned.
	bmhToAgentMap := map[string]*aiv1beta1.Agent{}
	for i, agent := range agentList.Items {
		if agent.Spec.ClusterDeploymentName == nil {
			continue
		}
		if agent.Spec.ClusterDeploymentName.Name != clusterDeployment.Name {
			continue
		}
		if bmhName, ok := agent.ObjectMeta.Labels[AGENT_BMH_LABEL]; ok {
			if existingAgent, ok := bmhToAgentMap[bmhName]; ok {
				// if there are two Agent with the same bmhName, return the newest
				if agent.ObjectMeta.CreationTimestamp.After(existingAgent.ObjectMeta.CreationTimestamp.Time) {
					bmhToAgentMap[bmhName] = &agentList.Items[i]
				}
			} else {
				bmhToAgentMap[bmhName] = &agentList.Items[i]
			}
		}
	}

	agents := []*aiv1beta1.Agent{}
	for _, agent := range bmhToAgentMap {
		agents = append(agents, agent)
	}

	return agents
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
func (r *BMACReconciler) findBMHByAgent(ctx context.Context, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
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

// Find `BareMetalHost` resources that match an InfraEnv
//
// Only `BareMetalHost` resources that have a label with a
// reference to an InfraEnv
func (r *BMACReconciler) findBMHByInfraEnv(ctx context.Context, infraEnv *aiv1beta1.InfraEnv) ([]*bmh_v1alpha1.BareMetalHost, error) {
	bmhList := bmh_v1alpha1.BareMetalHostList{}
	err := r.Client.List(ctx, &bmhList, client.InNamespace(infraEnv.Namespace))
	if err != nil {
		return nil, err
	}

	bmhs := []*bmh_v1alpha1.BareMetalHost{}

	for i, bmh := range bmhList.Items {
		if val, ok := bmh.ObjectMeta.Labels[BMH_INFRA_ENV_LABEL]; ok {
			if strings.EqualFold(val, infraEnv.Name) {
				bmhs = append(bmhs, &bmhList.Items[i])
			}
		}
	}
	return bmhs, nil
}

func (r *BMACReconciler) ensureSpokeBMH(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, machine *machinev1beta1.Machine, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhSpoke, mutateFn := r.newSpokeBMH(bmh, machine, agent)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, bmhSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Spoke BareMetalHost created")
	}
	return bmhSpoke, nil
}

func (r *BMACReconciler) ensureSpokeMachine(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, clusterDeployment *hivev1.ClusterDeployment) (*machinev1beta1.Machine, error) {
	machineSpoke, mutateFn := r.newSpokeMachine(bmh, clusterDeployment)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, machineSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Spoke Machine created")
	}
	return machineSpoke, nil
}

func (r *BMACReconciler) ensureSpokeBMHSecret(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost) (*corev1.Secret, error) {
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
		log.Info("Spoke BareMetalHost Secret created")
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
		bmhSpoke.Spec.Image = bmh.Spec.Image
		bmhSpoke.Spec.ConsumerRef = &corev1.ObjectReference{
			Name:      machine.Name,
			Namespace: machine.Namespace,
		}
		// copy annotations. hardwaredetails annotations is needed for automatic csr approval
		// We don't copy all annotations because there are some annotations that should not be
		// carried over from the hub BMH. The detached annotation is an example.
		if bmhSpoke.ObjectMeta.Annotations == nil {
			bmhSpoke.ObjectMeta.Annotations = make(map[string]string)
		}
		bmhSpoke.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION] = bmh.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION]
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

// Returns a list of BMH ReconcileRequests for a given Agent
func (r *BMACReconciler) agentToBMHReconcileRequests(ctx context.Context, agent *aiv1beta1.Agent) []reconcile.Request {
	// No need to list all the `BareMetalHost` resources if the agent
	// already has the reference to one of them.
	if val, ok := agent.ObjectMeta.Labels[AGENT_BMH_LABEL]; ok {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Namespace: agent.Namespace,
			Name:      val,
		}}}
	}

	bmh, err := r.findBMHByAgent(ctx, agent)
	if bmh == nil || err != nil {
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{
		Namespace: bmh.Namespace,
		Name:      bmh.Name,
	}}}
}

func (r *BMACReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapAgentToBMH := func(a client.Object) []reconcile.Request {
		ctx := context.Background()
		agent := &aiv1beta1.Agent{}

		if err := r.Get(ctx, types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}, agent); err != nil {
			return []reconcile.Request{}
		}

		return r.agentToBMHReconcileRequests(ctx, agent)
	}

	mapClusterDeploymentToBMH := func(a client.Object) []reconcile.Request {
		ctx := context.Background()
		clusterDeployment := &hivev1.ClusterDeployment{}

		if err := r.Get(ctx, types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}, clusterDeployment); err != nil {
			return []reconcile.Request{}
		}

		// Don't queue any reconcile if the ClusterDeployment
		// has not been installed.
		if !clusterDeployment.Spec.Installed {
			return []reconcile.Request{}
		}

		reconcileRequests := []reconcile.Request{}
		agents := r.findAgentsByClusterDeployment(ctx, clusterDeployment)
		for _, agent := range agents {
			reconcileRequests = append(reconcileRequests, r.agentToBMHReconcileRequests(ctx, agent)...)
		}

		return reconcileRequests
	}

	mapInfraEnvToBMH := func(a client.Object) []reconcile.Request {
		ctx := context.Background()
		infraEnv := &aiv1beta1.InfraEnv{}

		if err := r.Get(ctx, types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}, infraEnv); err != nil {
			return []reconcile.Request{}
		}

		// Don't queue any reconcile if the InfraEnv
		// doesn't have the ISODownloadURL set yet.
		if infraEnv.Status.ISODownloadURL == "" {
			return []reconcile.Request{}
		}

		bmhs, err := r.findBMHByInfraEnv(ctx, infraEnv)
		if len(bmhs) == 0 || err != nil {
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, len(bmhs))

		for i, bmh := range bmhs {

			// Don't queue if shouldReconcileBMH explicitly tells us not to do so. If the function
			// returns a cooldown period, do not stop reconcile immediately here but let the
			// requeue happen during the reconciliation.
			queue, _, requeuePeriod, _ := shouldReconcileBMH(bmh, infraEnv)
			if !queue && requeuePeriod == 0 {
				continue
			}

			requests[i] = reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: bmh.Namespace,
					Name:      bmh.Name,
				}}
		}

		return requests
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("baremetal-agent-controller").
		For(&bmh_v1alpha1.BareMetalHost{}).
		Watches(&source.Kind{Type: &aiv1beta1.Agent{}}, handler.EnqueueRequestsFromMapFunc(mapAgentToBMH)).
		Watches(&source.Kind{Type: &aiv1beta1.InfraEnv{}}, handler.EnqueueRequestsFromMapFunc(mapInfraEnvToBMH)).
		Watches(&source.Kind{Type: &hivev1.ClusterDeployment{}}, handler.EnqueueRequestsFromMapFunc(mapClusterDeploymentToBMH)).
		Complete(r)
}
