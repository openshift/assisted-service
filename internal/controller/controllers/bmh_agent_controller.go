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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	logutil "github.com/openshift/assisted-service/pkg/log"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	APIReader             client.Reader
	Log                   logrus.FieldLogger
	Scheme                *runtime.Scheme
	SpokeK8sClientFactory spoke_k8s_client.SpokeK8sClientFactory
	spokeClient           client.Client
	ConvergedFlowEnabled  bool
}

const (
	AGENT_BMH_LABEL                     = "agent-install.openshift.io/bmh"
	BMH_AGENT_ROLE                      = "bmac.agent-install.openshift.io/role"
	BMH_AGENT_HOSTNAME                  = "bmac.agent-install.openshift.io/hostname"
	BMH_AGENT_MACHINE_CONFIG_POOL       = "bmac.agent-install.openshift.io/machine-config-pool"
	BMH_INFRA_ENV_LABEL                 = "infraenvs.agent-install.openshift.io"
	BMH_AGENT_INSTALLER_ARGS            = "bmac.agent-install.openshift.io/installer-args"
	BMH_ANNOTATION                      = "metal3.io/BareMetalHost"
	BMH_API_VERSION                     = "baremetal.cluster.k8s.io/v1alpha1"
	BMH_DETACHED_ANNOTATION             = "baremetalhost.metal3.io/detached"
	BMH_INSPECT_ANNOTATION              = "inspect.metal3.io"
	BMH_HARDWARE_DETAILS_ANNOTATION     = "inspect.metal3.io/hardwaredetails"
	BMH_AGENT_IGNITION_CONFIG_OVERRIDES = "bmac.agent-install.openshift.io/ignition-config-overrides"
	MACHINE_ROLE                        = "machine.openshift.io/cluster-api-machine-role"
	MACHINE_TYPE                        = "machine.openshift.io/cluster-api-machine-type"
	MCS_CERT_NAME                       = "ca.crt"
	OPENSHIFT_MACHINE_API_NAMESPACE     = "openshift-machine-api"
	ASSISTED_DEPLOY_METHOD              = "start_assisted_install"
)

var (
	InfraEnvImageCooldownPeriod = 60 * time.Second
)

const certificateAuthoritiesIgnitionOverride = `{
	"ignition": {
	  "version": "3.1.0",
	  "security": {
		"tls": {
		  "certificateAuthorities": [{
			"source": "{{.SOURCE}}"
		  }]
		}
	  }
	}
  }`

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
		if !k8serrors.IsNotFound(err) {
			return reconcileError{err}.Result()
		}
		return reconcileComplete{}.Result()
	}

	// Should we check the status of the BMH resource?
	// Provisioning / Deprovisioning ?
	// What happens if we do the reconcile on a BMH that
	// is in a Deprovisioning state?

	agent := r.findAgent(ctx, bmh)

	if agent != nil {
		result := r.reconcileUnboundAgent(log, bmh, agent)
		if result.Dirty() {
			err := r.Client.Update(ctx, bmh)
			if err != nil {
				log.WithError(err).Errorf("Error adding reset annotation on BMH for unbound agent")
				return reconcileError{err}.Result()
			}
		}

		if result.Stop(ctx) {
			return result.Result()
		}
	}

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
		log.Debugf("Stopping BMAC reconcile after reconcileBMH")
		return result.Result()
	}

	// handle multiple agents matching the
	// same BMH's Mac Address
	if agent == nil {
		log.Debugf("Stopping BMAC reconcile after reconcileBMH")
		return result.Result()
	}

	// if we get to this point, it means we have found an Agent that is associated
	// with the BMH being reconciled. We will call both, reconcileAgentSpec and
	// reconcileAgentInventory, every time. The logic to decide whether there's
	// any action to take is implemented in each function respectively.
	result = r.reconcileAgentSpec(log, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, agent)
		if err != nil {
			log.WithError(err).Errorf("Error updating agent")
			return reconcileError{err}.Result()
		}
	} else {
		log.Debugf("Agent %s/%s: update skipped", agent.Namespace, agent.Name)
	}

	if result.Stop(ctx) {
		log.Debugf("Stopping BMAC reconcile after reconcileAgentSpec")
		return result.Result()
	}

	// In the converged flow ironic will reconcile the BMH_HARDWARE_DETAILS_ANNOTATION
	if !r.ConvergedFlowEnabled {
		result = r.reconcileAgentInventory(log, bmh, agent)
		if result.Dirty() {
			err := r.Client.Update(ctx, bmh)
			if err != nil {
				log.WithError(err).Errorf("Error updating hardwaredetails")
				return reconcileError{err}.Result()
			}
		}

		if result.Stop(ctx) {
			log.Debugf("Stopping BMAC reconcile after reconcileAgentInventory")
			return result.Result()
		}
	}

	result = r.ensureMCSCert(ctx, log, bmh, agent)
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			log.WithError(err).Errorf("Error adding MCS cert of spoke cluster into BMH")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
		log.Debugf("Stopping BMAC reconcile after ensureMCSCert")
		return result.Result()
	}

	if r.ConvergedFlowEnabled {
		result = r.addBMHDetachedAnnotationIfBmhIsProvisioned(log, bmh, agent)
	} else {
		// After the agent has started installation, Ironic should not manage the host.
		// Adding the detached annotation to the BMH stops Ironic from managing it.
		result = r.addBMHDetachedAnnotationIfAgentHasStartedInstallation(log, bmh, agent)
	}
	if result.Dirty() {
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			log.WithError(err).Errorf("Error updating BMH detached annotation")
			return reconcileError{err}.Result()
		}
	}

	if result.Stop(ctx) {
		log.Debugf("Stopping BMAC reconcile after add detached annotation")
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

	var dirty bool
	annotations := bmh.ObjectMeta.GetAnnotations()
	if val, ok := annotations[BMH_AGENT_ROLE]; ok {
		if agent.Spec.Role != models.HostRole(val) {
			agent.Spec.Role = models.HostRole(val)
			dirty = true
		}
	}

	if val, ok := annotations[BMH_AGENT_HOSTNAME]; ok {
		if agent.Spec.Hostname != val {
			agent.Spec.Hostname = val
			dirty = true
		}
	}

	if val, ok := annotations[BMH_AGENT_MACHINE_CONFIG_POOL]; ok {
		if agent.Spec.MachineConfigPool != val {
			agent.Spec.MachineConfigPool = val
			dirty = true
		}
	}

	if val, ok := annotations[BMH_AGENT_INSTALLER_ARGS]; ok {
		if agent.Spec.InstallerArgs != val {
			agent.Spec.InstallerArgs = val
			dirty = true
		}
	}

	if val, ok := annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]; ok {
		if agent.Spec.IgnitionConfigOverrides != val {
			agent.Spec.IgnitionConfigOverrides = val
			dirty = true
		}
	}

	if !agent.Spec.Approved {
		agent.Spec.Approved = true
		dirty = true
	}
	if agent.ObjectMeta.Labels == nil {
		agent.ObjectMeta.Labels = make(map[string]string)
		dirty = true
	}

	// Label the agent with the reference to this BMH
	name, ok := agent.ObjectMeta.Labels[AGENT_BMH_LABEL]
	if !ok || name != bmh.Name {
		agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = bmh.Name
		dirty = true
	}

	// findInstallationDiskID will return an empty string
	// if no disk is found from the list. Should be find
	// to "overwrite" this value everytime as the default
	// is ""
	installationDiskID := r.findInstallationDiskID(agent.Status.Inventory.Disks, bmh.Spec.RootDeviceHints)
	if agent.Spec.InstallationDiskID != installationDiskID {
		agent.Spec.InstallationDiskID = installationDiskID
		dirty = true
	}
	log.Debugf("Agent spec reconcile finished:  %v", agent)

	return reconcileComplete{dirty: dirty}
}

// The detached annotation is added if the BMH provisioning state is provisioned
func (r *BMACReconciler) addBMHDetachedAnnotationIfBmhIsProvisioned(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {

	shouldSkip := shouldSkipDetach(log, bmh, agent)
	if shouldSkip {
		return reconcileComplete{}
	}

	if r.ConvergedFlowEnabled && bmh.Status.Provisioning.State != bmh_v1alpha1.StateProvisioned {
		log.Debugf("Skipping adding detached annotation. BMH provisioning state is: %s should be: %s", bmh.Status.Provisioning.State, bmh_v1alpha1.StateProvisioned)
		return reconcileComplete{}

	}
	return detachBMH(log, bmh, agent)
}

func detachBMH(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}
	bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION] = "assisted-service-controller"
	log.Infof("Added detached annotation to agent \n %v", agent)

	return reconcileComplete{dirty: true, stop: true}
}

func shouldSkipDetach(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) bool {
	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	// Annotation already exists
	if _, ok := bmhAnnotations[BMH_DETACHED_ANNOTATION]; ok {
		log.Debugf("Skipping adding detached annotation. annotation already exists \n %v", agent)
		return true
	}

	//check if we are in unbinding-pending-user-action status. If yes, we should not
	//re-apply the detached label because ironic need to restart the host
	boundCondition := conditionsv1.FindStatusCondition(agent.Status.Conditions, aiv1beta1.BoundCondition)
	if boundCondition != nil && boundCondition.Reason == aiv1beta1.UnbindingPendingUserActionReason {
		log.Debugf("Skipping adding detached annotation. pending unbound condition \n %v", agent)
		return true
	}
	return false
}

// The detached annotation is added if the installation of the agent associated with
// the host has started.
func (r *BMACReconciler) addBMHDetachedAnnotationIfAgentHasStartedInstallation(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {

	shouldSkip := shouldSkipDetach(log, bmh, agent)
	if shouldSkip {
		return reconcileComplete{}
	}

	c := conditionsv1.FindStatusCondition(agent.Status.Conditions, aiv1beta1.InstalledCondition)
	if c == nil {
		log.Debugf("Skipping adding detached annotation. missing install condition \n %v", agent)
		return reconcileComplete{}
	}
	installConditionReason := c.Reason

	// Do nothing if InstalledCondition is not in Installed, InProgress, or Failed
	if installConditionReason != aiv1beta1.InstallationInProgressReason &&
		installConditionReason != aiv1beta1.InstalledReason &&
		installConditionReason != aiv1beta1.InstallationFailedReason {
		log.Debugf("Skipping adding detached annotation. host not in proper installation condition \n %v", agent)
		return reconcileComplete{}
	}

	return detachBMH(log, bmh, agent)
}

// Reconcile BMH's HardwareDetails using the agent's inventory
//
// Here we will copy as much data from the Agent's Inventory as possible
// into the `inspect.metal3.io/hardwaredetails` annotation in the BMH.
//
// This will trigger a reconcile on the BMH side, resulting in this data
// being copied from the annotation into the BMH's HardwareDetails status.
//
// Care must be taken to only update the data when really needed. Doing an update
// on every BMAC reconcile will trigger an infinite loop of reconciles between
// BMAC and the BMH reconcile as the former will update the hardwaredetails annotation
// while the latter will continue to update the status.
func (r *BMACReconciler) reconcileAgentInventory(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	log.Debugf("Started Agent Inventory reconcile for agent %s/%s and bmh %s/%s", agent.Namespace, agent.Name, bmh.Namespace, bmh.Name)

	// This check should be updated. We should check the agent's conditions instead
	if len(agent.Status.Inventory.Interfaces) == 0 {
		log.Debugf("Skipping Agent Inventory (hardwaredetails) reconcile \n %v", agent)
		return reconcileComplete{}
	}

	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	if _, ok := bmhAnnotations[BMH_HARDWARE_DETAILS_ANNOTATION]; ok {
		log.Debugf("%s annotation exists", BMH_HARDWARE_DETAILS_ANNOTATION)
		return reconcileComplete{}
	}

	// Check whether HardwareDetails has been set already. This annotation
	// status should only be set through this Reconcile in this scenario.
	if bmh.Status.HardwareDetails != nil && bmh.Status.HardwareDetails.Hostname != "" {
		log.Debugf("HardwareDetails or Hostname already present in the BMH")
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
				IP:        strings.SplitN(ip, "/", 2)[0],
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
			Rotational:   strings.EqualFold(d.DriveType, string(models.DriveTypeHDD)),
		}

		hardwareDetails.Storage = append(hardwareDetails.Storage, disk)
	}

	// Add the memory information in MebiByte
	if agent.Status.Inventory.Memory.PhysicalBytes > 0 {
		hardwareDetails.RAMMebibytes = int(conversions.BytesToMib(inventory.Memory.PhysicalBytes))
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
	if agent.Spec.Hostname != "" {
		hardwareDetails.Hostname = agent.Spec.Hostname
	} else {
		hardwareDetails.Hostname = inventory.Hostname
	}

	bytes, err := json.Marshal(hardwareDetails)
	if err != nil {
		return reconcileError{err}
	}

	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}

	bmh.ObjectMeta.Annotations[BMH_HARDWARE_DETAILS_ANNOTATION] = string(bytes)
	log.Debugf("Agent Inventory reconciled to BMH \n %v \n %v", agent, bmh)
	return reconcileComplete{dirty: true, stop: true}

}

// Ask BMH to reboot the host if the agent is unbound after installation
//
// By re-attaching the BMH and clearing the Image field on it, BMAC will clear
// the Image data to force the boot from ISO
func (r *BMACReconciler) reconcileUnboundAgent(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	log.Debugf("Started Unbound Agent reconcile for agent %s/%s and bmh %s/%s", agent.Namespace, agent.Name, bmh.Namespace, bmh.Name)

	// proceed with the reconcile only when the agent ask for user action following unbinding
	// and the detached annotation is in place (which means that we have not dealt with this case before)
	boundCondition := conditionsv1.FindStatusCondition(agent.Status.Conditions, aiv1beta1.BoundCondition)
	_, isDetached := bmh.ObjectMeta.Annotations[BMH_DETACHED_ANNOTATION]
	if boundCondition == nil || boundCondition.Reason != aiv1beta1.UnbindingPendingUserActionReason || !isDetached {
		log.Debugf("Skipping Unbound Agent reconcile \n %v", agent)
		return reconcileComplete{}
	}

	// re-attach the bmh and clear the ISO url to force ironic image refresh
	// and re-conciliation of BMH and agent. Also, clear the hw details just
	// in case. They will be regenerated in the reconciles following the agent's
	// reboot
	delete(bmh.ObjectMeta.Annotations, BMH_DETACHED_ANNOTATION)
	delete(bmh.ObjectMeta.Annotations, BMH_HARDWARE_DETAILS_ANNOTATION)
	bmh.Spec.Image = nil

	log.Infof("Unbound Agent reconciled to BMH \n %v \n %v", agent, bmh)
	return reconcileComplete{dirty: true, stop: true}
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
//
//	to contain multiple informations instead of a bunch of variables that are later on
//	separately interpreted.
func shouldReconcileBMH(bmh *bmh_v1alpha1.BareMetalHost, infraEnv *aiv1beta1.InfraEnv) (bool, bool, time.Duration, string) {
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

	// Stop `Reconcile` if BMH does not have an InfraEnv.
	if infraEnv == nil {
		return reconcileComplete{stop: true}
	}

	dirty := false
	// in case of converged flow set the custom deploy and cleaning mode instead of the annotations
	if r.ConvergedFlowEnabled {
		if bmh.Spec.CustomDeploy == nil || bmh.Spec.CustomDeploy.Method != ASSISTED_DEPLOY_METHOD {
			log.Infof("Updating BMH CustomDeploy to %s", ASSISTED_DEPLOY_METHOD)
			bmh.Spec.CustomDeploy = &bmh_v1alpha1.CustomDeploy{Method: ASSISTED_DEPLOY_METHOD}
			dirty = true
		}
		if bmh.Spec.AutomatedCleaningMode != bmh_v1alpha1.CleaningModeDisabled {
			log.Infof("Updating BMH AutomatedCleaningMode to %s", bmh_v1alpha1.CleaningModeDisabled)
			bmh.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
			dirty = true
		}
		return reconcileComplete{dirty: dirty, stop: false}
	}

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

	if !r.validateWorkerForDay2(log, agent) {
		return reconcileComplete{}
	}
	cd, installed, err := r.getClusterDeploymentAndCheckIfInstalled(ctx, log, agent)
	if err != nil {
		return reconcileError{err}
	}
	if !installed {
		return reconcileComplete{}
	}

	key := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      getClusterDeploymentAdminKubeConfigSecretName(cd),
	}
	isNonePlatform, propagateError, err := isNonePlatformCluster(ctx, r.Client, cd)
	if err != nil {
		log.WithError(err).Errorf("Failed to determine if platform is none for cluster deployment %s/%s", cd.Namespace, cd.Name)
		// In case ACI reference doesn't exist or ACI not found do not reconcile again since such a change in these will trigger BMH reconciliation
		// In all other cases propagate the error
		if propagateError {
			return reconcileError{err}
		}
		return reconcileComplete{}
	}
	if isNonePlatform {
		// If none platform do not attempt to create spoke BMH
		return reconcileComplete{}
	}

	secret, err := getSecret(ctx, r.Client, r.APIReader, key)
	if err != nil {
		return reconcileError{err}
	}
	if err = ensureSecretIsLabelled(ctx, r.Client, secret, key); err != nil {
		return reconcileError{err}
	}

	spokeClient, err := r.getSpokeClient(secret)
	if err != nil {
		log.WithError(err).Errorf("failed to create spoke kubeclient")
		return reconcileError{err}
	}

	checksum, url, err, stopReconcileLoop := r.getChecksumAndURL(ctx, log, spokeClient)
	if err != nil {
		log.WithError(err).Errorf("failed to get checksum and url value from master spoke machine")
		if stopReconcileLoop {
			log.Info("Stopping reconcileSpokeBMH")
			return reconcileComplete{dirty: false, stop: stopReconcileLoop}
		}
		return reconcileError{err}
	}

	machineNSName := types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", cd.Name, bmh.Name),
		Namespace: OPENSHIFT_MACHINE_API_NAMESPACE,
	}

	key = types.NamespacedName{
		Namespace: bmh.Namespace,
		Name:      bmh.Spec.BMC.CredentialsName,
	}
	_, err = r.ensureSpokeBMHSecret(ctx, log, spokeClient, key)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke BareMetalHost Secret")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeBMH(ctx, log, spokeClient, bmh, machineNSName, agent)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke BareMetalHost")
		return reconcileError{err}
	}

	_, err = r.ensureSpokeMachine(ctx, log, spokeClient, bmh, cd, machineNSName, checksum, url)
	if err != nil {
		log.WithError(err).Errorf("failed to create or update spoke Machine")
		return reconcileError{err}
	}

	// Add detached annotation to hub BMH so that Ironic stops managing the host from
	// the hub cluster. After spoke BMH is created, the host will be managed by the spoke
	// cluster.
	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	if _, ok := bmhAnnotations[BMH_DETACHED_ANNOTATION]; !ok {
		detachBMH(log, bmh, agent)
		return reconcileComplete{dirty: true, stop: true}
	}
	return reconcileComplete{}
}

// Finds the installation disk based on the RootDeviceHints
func (r *BMACReconciler) findInstallationDiskID(devices []aiv1beta1.HostDisk, hints *bmh_v1alpha1.RootDeviceHints) string {
	if hints == nil {
		return ""
	}

	disks := make([]*models.Disk, len(devices))

	for i, dev := range devices {
		disks[i] = &models.Disk{
			Bootable:                dev.Bootable,
			ByID:                    dev.ByID,
			ByPath:                  dev.ByPath,
			DriveType:               models.DriveType(strings.ToUpper(dev.DriveType)),
			Hctl:                    dev.Hctl,
			ID:                      dev.ID,
			InstallationEligibility: models.DiskInstallationEligibility{Eligible: dev.InstallationEligibility.Eligible},
			IoPerf:                  &models.IoPerf{SyncDuration: dev.IoPerf.SyncDurationMilliseconds},
			Model:                   dev.Model,
			Name:                    dev.Name,
			Path:                    dev.Path,
			Serial:                  dev.Serial,
			SizeBytes:               dev.SizeBytes,
			Smart:                   dev.Smart,
			Vendor:                  dev.Vendor,
			Wwn:                     dev.Wwn,
		}
	}

	acceptable := hostutil.GetAcceptableDisksWithHints(disks, hints)
	if len(acceptable) > 0 {
		return acceptable[0].ID
	}

	// If hints are provided but we did not find an eligible disk, we need to raise an error.
	// By no means we should pass empty disk ID, as that would mean that the o/installer can use
	// any of the available disks. This is counterintuitive for users providing rootDeviceHints
	// and may cause unexpected behaviour, e.g. wiping drive that must have its data preserved.
	// Because BMAC should not direcly modify Conditions on the Agent CR (it would be racing with
	// the Agent Reconciler), we let the assisted installer handle error caused by the incorrect
	// disk further in the process.
	return "/dev/not-found-by-hints"
}

// Finds the agents related to this ClusterDeployment
//
// The ClusterDeployment <-> agent relation is one-to-many.
// This function returns all Agents whose ClusterDeploymentName name
// matches this ClusterDeployment's name.
func (r *BMACReconciler) findAgentsByClusterDeployment(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) []*aiv1beta1.Agent {
	agentList := aiv1beta1.AgentList{}
	err := r.Client.List(ctx, &agentList, client.MatchingLabels{AgentLabelClusterDeploymentNamespace: clusterDeployment.Namespace})
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

func (r *BMACReconciler) ensureSpokeBMH(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, machineName types.NamespacedName, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhSpoke, mutateFn := r.newSpokeBMH(log, bmh, machineName, agent)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, bmhSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Spoke BareMetalHost created")
	}
	return bmhSpoke, nil
}

// get spokeMachineMaster and retrieve checksum , url to set into spokeMachineWorker
func (r *BMACReconciler) getChecksumAndURL(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client) (string, string, error, bool) {
	var checksum, url string
	machineList := &machinev1beta1.MachineList{}
	err := spokeClient.List(ctx, machineList, client.MatchingLabels{MACHINE_TYPE: string(models.HostRoleMaster)})
	if err != nil {
		return checksum, url, err, false
	}
	//MGMT-10570 check that the master list is not empty before referencing it
	//Stop the reconciliation in this case because it is a fatal error
	if len(machineList.Items) == 0 {
		return checksum, url, errors.New("There are no machines with master label"), true
	}
	providerSpecValue := string(machineList.Items[0].Spec.ProviderSpec.Value.Raw)

	var providerSpecValueObj map[string]interface{}
	err = json.Unmarshal([]byte(providerSpecValue), &providerSpecValueObj)
	if err != nil {
		return checksum, url, err, false
	}
	image := providerSpecValueObj["image"].(map[string]interface{})
	checksum = fmt.Sprint(image["checksum"])
	url = fmt.Sprint(image["url"])
	return checksum, url, err, false
}

func (r *BMACReconciler) ensureSpokeMachine(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, bmh *bmh_v1alpha1.BareMetalHost, clusterDeployment *hivev1.ClusterDeployment, machineName types.NamespacedName, checksum string, URL string) (*machinev1beta1.Machine, error) {
	machineSpoke, mutateFn := r.newSpokeMachine(bmh, clusterDeployment, machineName, checksum, URL)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, machineSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Spoke Machine created")
	}
	return machineSpoke, nil
}

// Get MCS cert from the spoke cluster, encode it and add it to BMH
// To increase the capacity of the spoke cluster on day2, while adding remote worker nodes to the spoke cluster,
// we need to pull the MCS certificate from the spoke cluster and inject it as part of the ignition config
// before the node boots so that there is no failure due to Certificate signed by Unknown Authority
// Ref: https://access.redhat.com/solutions/4799921
func (r *BMACReconciler) ensureMCSCert(ctx context.Context, log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, agent *aiv1beta1.Agent) reconcileResult {
	if !r.validateWorkerForDay2(log, agent) {
		return reconcileComplete{}
	}
	cd, installed, err := r.getClusterDeploymentAndCheckIfInstalled(ctx, log, agent)
	if err != nil {
		return reconcileError{err}
	}
	if !installed {
		return reconcileComplete{}
	}

	key := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      getClusterDeploymentAdminKubeConfigSecretName(cd),
	}

	secret, err := getSecret(ctx, r.Client, r.APIReader, key)
	if err != nil {
		log.WithError(err).Errorf("failed to get secret %s", key)
		return reconcileError{err}
	}
	if err = ensureSecretIsLabelled(ctx, r.Client, secret, key); err != nil {
		return reconcileError{err}
	}

	spokeClient, err := r.getSpokeClient(secret)
	if err != nil {
		log.WithError(err).Errorf("failed to create spoke kubeclient")
		return reconcileError{err}
	}

	MCSCert, ignitionWithMCSCert, err := r.createIgnitionWithMCSCert(ctx, log, spokeClient)
	if err != nil {
		log.WithError(err).Errorf("failed to create ignition with mcs cert")
		return reconcileError{err}
	}
	if bmh.ObjectMeta.Annotations == nil {
		bmh.ObjectMeta.Annotations = make(map[string]string)
	}
	bmhAnnotations := bmh.ObjectMeta.GetAnnotations()
	userIgnition, ok := bmhAnnotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES]

	// check if bmh already has MCS cert in it
	if strings.Contains(userIgnition, MCSCert) {
		log.Debug("BMH already has MCS cert set")
		return reconcileComplete{}
	}

	// User has set ignition via annotation
	if ok {
		log.Debug("User has set ignition via annotation")
		res, err := ignition.MergeIgnitionConfig([]byte(ignitionWithMCSCert), []byte(userIgnition))
		if err != nil {
			log.WithError(err).Errorf("Error while merging the ignitions")
			return reconcileError{err}
		}
		bmh.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = res

	} else {
		log.Debug("User has not set ignition via annotation so we set the annotation with ignition containing MCS cert from spoke cluster")
		bmh.ObjectMeta.Annotations[BMH_AGENT_IGNITION_CONFIG_OVERRIDES] = ignitionWithMCSCert
	}
	log.Info("MCS certificate injected")
	return reconcileComplete{dirty: true, stop: true}
}

func (r *BMACReconciler) createIgnitionWithMCSCert(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client) (string, string, error) {
	configMap := &corev1.ConfigMap{}
	var encodedMCSCrt, ignitionWithMCSCert string
	key := types.NamespacedName{
		Namespace: "kube-system",
		Name:      "root-ca",
	}
	if err := spokeClient.Get(ctx, key, configMap); err != nil {
		if k8serrors.IsNotFound(err) {
			// Hypershift stores root ca in a different configmap and different namespace
			key = types.NamespacedName{
				Namespace: "openshift-config",
				Name:      "kube-root-ca",
			}
			if err := spokeClient.Get(ctx, key, configMap); err != nil {
				if k8serrors.IsNotFound(err) {
					return encodedMCSCrt, ignitionWithMCSCert, err
				}
			}
		}
	}
	if configMap.Data == nil {
		return encodedMCSCrt, ignitionWithMCSCert, errors.Errorf("Configmap %s/%s  does not contain any data", configMap.Namespace, configMap.Name)
	}
	certData, ok := configMap.Data[MCS_CERT_NAME]
	if !ok || len(certData) == 0 {
		return encodedMCSCrt, ignitionWithMCSCert, errors.Errorf("Configmap data for %s/%s  does not contain %s", configMap.Namespace, configMap.Name, MCS_CERT_NAME)
	}
	mcsCrt := configMap.Data[MCS_CERT_NAME]
	encodedMCSCrt = base64.StdEncoding.EncodeToString([]byte(mcsCrt))
	ignitionWithMCSCert, err := r.formatMCSCertificateIgnition(encodedMCSCrt)
	if err != nil {
		return encodedMCSCrt, ignitionWithMCSCert, errors.Errorf("Failed to create ignition string with MCS cert")
	}
	return encodedMCSCrt, ignitionWithMCSCert, nil

}

func (r *BMACReconciler) ensureSpokeBMHSecret(ctx context.Context, log logrus.FieldLogger, spokeClient client.Client, key types.NamespacedName) (*corev1.Secret, error) {
	secret, err := getSecret(ctx, r.Client, r.APIReader, key)
	if err != nil {
		log.WithError(err).Errorf("failed to get secret resource %s/%s", key.Namespace, key.Name)
		return secret, err
	}
	if err := ensureSecretIsLabelled(ctx, r.Client, secret, key); err != nil {
		log.WithError(err).Errorf("failed to label secret resource %s/%s", key.Namespace, key.Name)
		return secret, err
	}
	secretSpoke, mutateFn := r.newSpokeBMHSecret(secret)
	if result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, secretSpoke, mutateFn); err != nil {
		return nil, err
	} else if result != controllerutil.OperationResultNone {
		log.Info("Spoke BareMetalHost Secret created")
	}
	return secretSpoke, nil
}

func (r *BMACReconciler) newSpokeBMH(log logrus.FieldLogger, bmh *bmh_v1alpha1.BareMetalHost, machineName types.NamespacedName, agent *aiv1beta1.Agent) (*bmh_v1alpha1.BareMetalHost, controllerutil.MutateFn) {
	bmhSpoke := &bmh_v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmh.Name,
			Namespace: OPENSHIFT_MACHINE_API_NAMESPACE,
		},
	}
	mutateFn := func() error {
		bmhSpoke.Spec = bmh.Spec
		// The host is created by the baremetal operator on hub cluster. So BMH on the spoke cluster needs
		// to be set to externally provisioned
		bmhSpoke.Spec.ExternallyProvisioned = true
		bmhSpoke.Spec.Image = bmh.Spec.Image
		bmhSpoke.Spec.ConsumerRef = &corev1.ObjectReference{
			APIVersion: machinev1beta1.SchemeGroupVersion.String(),
			Kind:       "Machine",
			Name:       machineName.Name,
			Namespace:  machineName.Namespace,
		}
		// copy annotations. hardwaredetails annotations is needed for automatic csr approval
		// We don't copy all annotations because there are some annotations that should not be
		// carried over from the hub BMH. The detached annotation is an example.
		if bmhSpoke.ObjectMeta.Annotations == nil {
			bmhSpoke.ObjectMeta.Annotations = make(map[string]string)
		}

		// HardwareDetails annotation needs a special case. The annotation gets removed once it's consumed by the baremetal operator
		// and data is copied to the bmh status. So we are reconciling the annotation from the agent status inventory.
		// If HardwareDetails annotation is already copied from hub bmh.annotation above, this won't overwrite it.
		if _, err := r.reconcileAgentInventory(log, bmhSpoke, agent).Result(); err != nil {
			return err
		}
		return nil
	}

	return bmhSpoke, mutateFn
}

func (r *BMACReconciler) newSpokeBMHSecret(secret *corev1.Secret) (*corev1.Secret, controllerutil.MutateFn) {
	secretSpoke := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: OPENSHIFT_MACHINE_API_NAMESPACE,
			Labels: map[string]string{
				BackupLabel: BackupLabelValue,
			},
		},
	}
	mutateFn := func() error {
		secretSpoke.Data = secret.Data
		return nil
	}

	return secretSpoke, mutateFn
}

func (r *BMACReconciler) newSpokeMachine(bmh *bmh_v1alpha1.BareMetalHost, clusterDeployment *hivev1.ClusterDeployment, machineName types.NamespacedName, checksum string, URL string) (*machinev1beta1.Machine, controllerutil.MutateFn) {
	machine := &machinev1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName.Name,
			Namespace: machineName.Namespace,
		},
	}
	mutateFn := func() error {
		if machine.ObjectMeta.Annotations == nil {
			machine.ObjectMeta.Annotations = make(map[string]string)
		}
		machine.ObjectMeta.Annotations[BMH_ANNOTATION] = fmt.Sprintf("%s/%s", OPENSHIFT_MACHINE_API_NAMESPACE, bmh.Name)
		providerSpecValueFormat := `{
						"apiVersion": "{{.BMH_API_VERSION}}",
						"kind": "BareMetalMachineProviderSpec",
						"image": {
						"checksum": "{{.CHECKSUM}}",
						"url": "{{.URL}}"
						}}`

		tmpl, err := template.New("valueString").Parse(providerSpecValueFormat)
		if err != nil {
			return err
		}
		buf := &bytes.Buffer{}
		var providerSpecValue = map[string]interface{}{
			"BMH_API_VERSION": BMH_API_VERSION,
			"CHECKSUM":        checksum,
			"URL":             URL,
		}
		if err = tmpl.Execute(buf, providerSpecValue); err != nil {
			return err
		}

		machine.Spec = machinev1beta1.MachineSpec{
			ProviderSpec: machinev1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: buf.Bytes(),
				},
			},
		}

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
	// We only set `spokeClient` during tests. Do not
	// set it as it would cache the client, which would
	// make the reconcile useless to manage multiple spoke
	// clusters.
	if r.spokeClient != nil {
		return r.spokeClient, err
	}
	return r.SpokeK8sClientFactory.CreateFromSecret(secret)
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

func (r *BMACReconciler) formatMCSCertificateIgnition(mcsCert string) (string, error) {
	var ignitionParams = map[string]string{
		"SOURCE": fmt.Sprintf("data:text/plain;charset=utf-8;base64,%s", mcsCert),
	}
	tmpl, err := template.New("nodeIgnition").Parse(certificateAuthoritiesIgnitionOverride)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, ignitionParams); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *BMACReconciler) validateWorkerForDay2(log logrus.FieldLogger, agent *aiv1beta1.Agent) bool {
	// Only worker role is supported for day2 operation
	if agent.Status.Role != models.HostRoleWorker || agent.Spec.ClusterDeploymentName == nil {
		log.Debugf("Skipping spoke BareMetalHost reconcile for  agent %s/%s, role %s and clusterDeployment %s.", agent.Namespace, agent.Name, agent.Status.Role, agent.Spec.ClusterDeploymentName)
		return false
	}
	return true
}

func (r *BMACReconciler) getClusterDeploymentAndCheckIfInstalled(ctx context.Context, log logrus.FieldLogger, agent *aiv1beta1.Agent) (*hivev1.ClusterDeployment, bool, error) {
	if agent.Spec.ClusterDeploymentName == nil {
		log.Debugf("Agent %s/%s is not bind yet", agent.Namespace, agent.Name)
		return nil, false, nil
	}

	clusterDeployment := &hivev1.ClusterDeployment{}

	cdKey := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	var err error
	if err = r.Get(ctx, cdKey, clusterDeployment); err != nil {
		log.WithError(err).Errorf("failed to get clusterDeployment resource %s/%s", cdKey.Namespace, cdKey.Name)
		return clusterDeployment, false, err
	}

	// If the cluster is not installed yet, we can't get kubeconfig for the cluster yet.
	if !clusterDeployment.Spec.Installed {
		log.Debugf("ClusterDeployment %s/%s for agent %s/%s is not installed yet", clusterDeployment.Namespace, clusterDeployment.Name, agent.Namespace, agent.Name)
		// If cluster is not Installed, wait until a reconcile is trigged by a ClusterDeployment watch event instead
		return clusterDeployment, false, err
	}
	return clusterDeployment, true, err
}
