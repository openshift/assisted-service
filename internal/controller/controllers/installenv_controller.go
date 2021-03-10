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
	"net/http"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// InstallEnvReconciler reconciles a InstallEnv object
type InstallEnvReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	Installer        bminventory.InstallerInternals
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups=adi.io.my.domain,resources=installenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=installenvs/status,verbs=get;update;patch

func (r *InstallEnvReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	installEnv := &adiiov1alpha1.InstallEnv{}
	if err := r.Get(ctx, req.NamespacedName, installEnv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.ensureISO(ctx, installEnv)
}

func (r *InstallEnvReconciler) updateClusterIfNeeded(ctx context.Context, installEnv *adiiov1alpha1.InstallEnv, cluster *common.Cluster) error {
	var (
		params = &models.ClusterUpdateParams{}
		update bool
		log    = logutil.FromContext(ctx, r.Log)
	)

	if installEnv.Spec.Proxy != nil {
		if installEnv.Spec.Proxy.NoProxy != "" && installEnv.Spec.Proxy.NoProxy != cluster.NoProxy {
			params.NoProxy = swag.String(installEnv.Spec.Proxy.NoProxy)
			update = true
		}
		if installEnv.Spec.Proxy.HTTPProxy != "" && installEnv.Spec.Proxy.HTTPProxy != cluster.HTTPProxy {
			params.HTTPProxy = swag.String(installEnv.Spec.Proxy.HTTPProxy)
			update = true
		}
		if installEnv.Spec.Proxy.HTTPSProxy != "" && installEnv.Spec.Proxy.HTTPSProxy != cluster.HTTPSProxy {
			params.HTTPSProxy = swag.String(installEnv.Spec.Proxy.HTTPSProxy)
			update = true
		}
	}
	if len(installEnv.Spec.AdditionalNTPSources) > 0 && cluster.AdditionalNtpSource != installEnv.Spec.AdditionalNTPSources[0] {
		params.AdditionalNtpSource = swag.String(strings.Join(installEnv.Spec.AdditionalNTPSources[:], ","))
		update = true
	}

	if update {
		updateString, err := json.Marshal(params)
		if err != nil {
			return err
		}
		log.Infof("updating cluster %s %s with %s",
			installEnv.Spec.ClusterRef.Name, installEnv.Spec.ClusterRef.Namespace, string(updateString))
		_, err = r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
			ClusterUpdateParams: params,
			ClusterID:           *cluster.ID,
		})
		return err
	}

	return nil
}

func (r *InstallEnvReconciler) updateClusterDiscoveryIgnitionIfNeeded(ctx context.Context, installEnv *adiiov1alpha1.InstallEnv, cluster *common.Cluster) error {
	var (
		discoveryIgnitionParams = &models.DiscoveryIgnitionParams{}
		updateClusterIgnition   bool
		log                     = logutil.FromContext(ctx, r.Log)
	)
	if installEnv.Spec.IgnitionConfigOverride != "" && cluster.IgnitionConfigOverrides != installEnv.Spec.IgnitionConfigOverride {
		discoveryIgnitionParams.Config = *swag.String(installEnv.Spec.IgnitionConfigOverride)
		updateClusterIgnition = true
	}
	if updateClusterIgnition {
		updateString, err := json.Marshal(discoveryIgnitionParams)
		if err != nil {
			return err
		}
		log.Infof("updating cluster %s %s with %s",
			installEnv.Spec.ClusterRef.Name, installEnv.Spec.ClusterRef.Namespace, string(updateString))
		err = r.Installer.UpdateDiscoveryIgnitionInternal(ctx, installer.UpdateDiscoveryIgnitionParams{
			DiscoveryIgnitionParams: discoveryIgnitionParams,
			ClusterID:               *cluster.ID,
		})
		return err
	}
	return nil
}

// ensureISO generates ISO for the cluster if needed and will update the condition Reason and Message accordingly.
// It returns a result that includes ISODownloadURL.
func (r *InstallEnvReconciler) ensureISO(ctx context.Context, installEnv *adiiov1alpha1.InstallEnv) (ctrl.Result, error) {
	var inventoryErr error
	var Requeue bool
	var updatedCluster *common.Cluster

	kubeKey := types.NamespacedName{
		Name:      installEnv.Spec.ClusterRef.Name,
		Namespace: installEnv.Spec.ClusterRef.Namespace,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}

	// Retrieve clusterDeployment
	if err := r.Get(ctx, kubeKey, clusterDeployment); err != nil {
		errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
			installEnv.Spec.ClusterRef.Name, installEnv.Spec.ClusterRef.Namespace)
		Requeue = false
		clientError := true
		if !k8serrors.IsNotFound(err) {
			Requeue = true
			clientError = false
		}
		clusterDeploymentRefErr := newKubeAPIError(errors.Wrapf(err, errMsg), clientError)

		// Update that we failed to retrieve the clusterDeployment
		conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  adiiov1alpha1.ImageCreationErrorReason,
			Message: adiiov1alpha1.ImageStateFailedToCreate + ": " + clusterDeploymentRefErr.Error(),
		})
		if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update installEnv status")
		}
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Retrieve cluster from the database
	cluster, err := r.Installer.GetClusterByKubeKey(types.NamespacedName{
		Name:      clusterDeployment.Name,
		Namespace: clusterDeployment.Namespace,
	})
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			Requeue = true
			inventoryErr = common.NewApiError(http.StatusNotFound, err)
		} else {
			Requeue = false
			inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
		}
		// Update that we failed to retrieve the cluster from the database
		conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  adiiov1alpha1.ImageCreationErrorReason,
			Message: adiiov1alpha1.ImageStateFailedToCreate + ": " + inventoryErr.Error(),
		})
		if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update installEnv status")
		}
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Check for updates from user, compare spec and update the cluster
	if err := r.updateClusterIfNeeded(ctx, installEnv, cluster); err != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, err)
	}

	// Check for discovery ignition specific updates from user, compare spec and update the ignition config
	if err := r.updateClusterDiscoveryIgnitionIfNeeded(ctx, installEnv, cluster); err != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, err)
	}

	// Generate ISO
	isoParams := installer.GenerateClusterISOParams{
		ClusterID:         *cluster.ID,
		ImageCreateParams: &models.ImageCreateParams{},
	}
	if len(installEnv.Spec.SSHAuthorizedKeys) > 0 {
		isoParams.ImageCreateParams.SSHPublicKey = installEnv.Spec.SSHAuthorizedKeys[0]
	}
	// GenerateClusterISOInternal will generate an ISO only if there it was not generated before,
	// or something has changed in isoParams.
	updatedCluster, inventoryErr = r.Installer.GenerateClusterISOInternal(ctx, isoParams)
	if inventoryErr != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, inventoryErr)
	}
	// Image successfully generated. Reflect that in installEnv obj and conditions
	return r.updateEnsureISOSuccess(ctx, installEnv, updatedCluster.ImageInfo)
}

func (r *InstallEnvReconciler) updateEnsureISOSuccess(
	ctx context.Context, installEnv *adiiov1alpha1.InstallEnv, imageInfo *models.ImageInfo) (ctrl.Result, error) {
	conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
		Type:    adiiov1alpha1.ImageCreatedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  adiiov1alpha1.ImageCreatedReason,
		Message: adiiov1alpha1.ImageStateCreated,
	})
	installEnv.Status.ISODownloadURL = imageInfo.DownloadURL
	if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update installEnv status")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *InstallEnvReconciler) handleEnsureISOErrors(
	ctx context.Context, installEnv *adiiov1alpha1.InstallEnv, err error) (ctrl.Result, error) {
	var errMsg string
	var Requeue bool

	if imageBeingCreated(err) { // Not an actual error, just an image generation in progress.
		err = nil
		Requeue = false
		r.Log.Infof("Image %s being prepared for cluster %s", installEnv.Name, installEnv.ClusterName)
		conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ImageCreatedCondition,
			Status:  corev1.ConditionTrue,
			Reason:  adiiov1alpha1.ImageCreatedReason,
			Message: adiiov1alpha1.ImageStateCreated,
		})
	} else { // Actual errors
		r.Log.WithError(err).Error("installEnv reconcile failed")
		if isClientError(err) {
			Requeue = false
			errMsg = ": " + err.Error()
		} else {
			Requeue = true
			errMsg = ": internal error"
		}
		conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
			Type:    adiiov1alpha1.ImageCreatedCondition,
			Status:  corev1.ConditionFalse,
			Reason:  adiiov1alpha1.ImageCreationErrorReason,
			Message: adiiov1alpha1.ImageStateFailedToCreate + errMsg,
		})
		// In a case of an error, clear the download URL.
		installEnv.Status.ISODownloadURL = ""
	}
	if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update installEnv status")
	}
	return ctrl.Result{Requeue: Requeue}, err
}

func imageBeingCreated(err error) bool {
	return IsHTTPError(err, http.StatusConflict)
}

func (r *InstallEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	installEnvUpdates := r.CRDEventsHandler.GetInstallEnvUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.InstallEnv{}).
		Watches(&source.Channel{Source: installEnvUpdates},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
