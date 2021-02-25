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
	"strings"
	"time"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// BMACReconciler reconciles a Agent object
type BMACReconciler struct {
	client.Client
	Log    logrus.FieldLogger
	Scheme *runtime.Scheme
}

const (
	AGENT_BMH_LABEL                 = "adi.openshift.io/bmh"
	BMH_AGENT_ROLE                  = "bmac.adi.openshift.io/role"
	BMH_AGENT_HOSTNAME              = "bmac.adi.openshift.io/hostname"
	BMH_AGENT_MACHINE_CONFIG_POOL   = "bmac.adi.openshift.io/machine-config-pool"
	BMH_INSTALL_ENV_LABEL           = "installenvs.adi.openshift.io"
	BMH_INSPECT_ANNOTATION          = "inspect.metal3.io"
	BMH_HARDWARE_DETAILS_ANNOTATION = "inspect.metal3.io/hardwaredetails"
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

func (r *BMACReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
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
		err := r.Client.Update(ctx, bmh)
		if err != nil {
			r.Log.WithError(err).Errorf("Error updating hardwaredetails")
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

		return result.Result()
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
// - Agent role: bmac.adi.openshift.io/role
// - Hostname: bmac.adi.openshift.io/hostname
// - Machine Config Pool: bmac.adi.openshift.io/machine-config-pool
//
// Unless there are errors, the agent should be `Approved` at the end of this
// reconcile and a label should be set on it referencing the BMH. No changes to
// the BMH should happen in this reconcile step.
func (r *BMACReconciler) reconcileAgentSpec(bmh *bmh_v1alpha1.BareMetalHost, agent *adiiov1alpha1.Agent) reconcileResult {

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

	agent.Spec.Approved = true
	if agent.ObjectMeta.Labels == nil {
		agent.ObjectMeta.Labels = make(map[string]string)
	}

	// Label the agent with the reference to this BMH
	agent.ObjectMeta.Labels[AGENT_BMH_LABEL] = bmh.Name

	// TODO: Analyze the root device hints
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
func (r *BMACReconciler) reconcileAgentInventory(bmh *bmh_v1alpha1.BareMetalHost, agent *adiiov1alpha1.Agent) reconcileResult {
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
		// missing vendor, rotational, WWNVendorExtension, WWNWithExtension
		disk := bmh_v1alpha1.Storage{
			Name:         d.Path,
			HCTL:         d.Hctl,
			Model:        d.Model,
			SizeBytes:    bmh_v1alpha1.Capacity(d.SizeBytes),
			SerialNumber: d.Serial,
			WWN:          d.Wwn,
		}

		hardwareDetails.Storage = append(hardwareDetails.Storage, disk)
	}

	// Add the memory information in MebiByte
	if agent.Status.Inventory.Memory.PhysicalBytes > 0 {
		hardwareDetails.RAMMebibytes = int(inventory.Memory.PhysicalBytes / (1024 * 1024))
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
// spec by copying it from the `InstallEnv` referenced in the resource's
// labels. If the previous action succeeds, this step will also set the
// BMH_INSPECT_ANNOTATION to disabled on the BareMetalHost.
//
// The above changes will be done only if the ISODownloadURL value has already
// been set in the `InstallEnv` resource and the Image.URL value has not been
// set in the `BareMetalHost`
func (r *BMACReconciler) reconcileBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) reconcileResult {
	// No need to reconcile if the image URL has been set in
	// the BMH already.
	if bmh.Spec.Image != nil && bmh.Spec.Image.URL != "" {
		return reconcileComplete{}
	}

	r.Log.Debugf("Started BMH reconcile for %s/%s", bmh.Namespace, bmh.Name)

	for ann, value := range bmh.Labels {

		// Find the `BMH_INSTALL_ENV_LABEL`, get the installEnv configured in it
		// and copy the ISO Url from the InstallEnv to the BMH resource.
		if ann == BMH_INSTALL_ENV_LABEL {
			installEnv := &adiiov1alpha1.InstallEnv{}
			// TODO: Watch for the InstallEnv resource and do the reconcile
			// when the required data is there.
			// https://github.com/openshift/assisted-service/pull/1279/files#r604213425
			if err := r.Get(ctx, types.NamespacedName{Name: value, Namespace: bmh.Namespace}, installEnv); err != nil {
				return reconcileError{client.IgnoreNotFound(err)}
			}

			if installEnv.Status.ISODownloadURL == "" {
				// the image has not been created yet, try later.
				r.Log.Infof("Image URL for InstallEnv (%s/%s) not available yet. Retrying reconcile for BareMetalHost  %s/%s",
					installEnv.Namespace, installEnv.Name, bmh.Namespace, bmh.Name)
				return reconcileRequeue{time.Minute}
			}

			// We'll just overwrite this at this point
			// since the nullness and emptyness checks
			// are done at the beginning of this function.
			bmh.Spec.Image = &bmh_v1alpha1.Image{}
			liveIso := "live-iso"
			bmh.Spec.Image.URL = installEnv.Status.ISODownloadURL
			bmh.Spec.Image.DiskFormat = &liveIso

			// Let's make sure inspection is disabled for BMH resources
			// that are associated with an agent-based deployment.
			//
			// Ideally, the user would do this while creating the BMH
			// we are just taking extra care that this is the case.
			if bmh.ObjectMeta.Annotations == nil {
				bmh.ObjectMeta.Annotations = make(map[string]string)
			}

			bmh.ObjectMeta.Annotations[BMH_INSPECT_ANNOTATION] = "disabled"

			r.Log.Infof("Image URL has been set in the BareMetalHost  %s/%s", bmh.Namespace, bmh.Name)
			return reconcileComplete{true}
		}
	}

	return reconcileComplete{}
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
// from the BMH, the following logic applies:
//
// 1. Return the agent that has been approved, if any.
// 2. If the above fails, return the first agent found.
//
// Otherwise, this function will return nil.
//
// Eventually, this will be changed to inspect the agents status
// instead of `Approved`. We want to reconcile only agents that
// are in specific states (like discovering). Waiting on MGMT-4266
//
// Another option is to return the newest one, as the newest agent
// should be authoritative
// https://github.com/openshift/assisted-service/pull/1279/files#r604215906
func (r *BMACReconciler) findAgent(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) *adiiov1alpha1.Agent {
	agentList := adiiov1alpha1.AgentList{}
	err := r.Client.List(ctx, &agentList, client.InNamespace(bmh.Namespace))
	if err != nil {
		return nil
	}

	agents := []*adiiov1alpha1.Agent{}

	for i, agent := range agentList.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {

				// Short circuit if we find an agent
				// that is Approved
				if agent.Spec.Approved {
					return &agent
				}

				agents = append(agents, &agentList.Items[i])
			}
		}
	}

	if len(agents) > 0 {
		return agents[0]
	}

	return nil
}

// Find `BareMetalHost` resources that match an agent
//
// Only `BareMetalHost` resources that match one of the Agent's
// MAC addresses will be returned.
func (r *BMACReconciler) findBMH(ctx context.Context, agent *adiiov1alpha1.Agent) (*bmh_v1alpha1.BareMetalHost, error) {
	bmhList := bmh_v1alpha1.BareMetalHostList{}
	err := r.Client.List(ctx, &bmhList, client.InNamespace(agent.Namespace))
	if err != nil {
		return nil, err
	}

	for _, bmh := range bmhList.Items {
		for _, agentInterface := range agent.Status.Inventory.Interfaces {
			if strings.EqualFold(bmh.Spec.BootMACAddress, agentInterface.MacAddress) {
				return &bmh, nil
			}
		}
	}
	return nil, nil
}

func (r *BMACReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapAgentToClusterDeployment := handler.ToRequestsFunc(
		func(a handler.MapObject) []reconcile.Request {
			ctx := context.Background()
			agent := &adiiov1alpha1.Agent{}

			if err := r.Get(ctx, types.NamespacedName{Name: a.Meta.GetName(), Namespace: a.Meta.GetNamespace()}, agent); err != nil {
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
		})

	return ctrl.NewControllerManagedBy(mgr).
		For(&bmh_v1alpha1.BareMetalHost{}).
		Watches(&source.Kind{Type: &adiiov1alpha1.Agent{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapAgentToClusterDeployment}).
		Complete(r)
}
