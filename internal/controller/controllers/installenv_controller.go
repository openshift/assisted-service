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
	"fmt"
	"net/http"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// InstallEnvReconciler reconciles a InstallEnv object
type InstallEnvReconciler struct {
	client.Client
	Log                      logrus.FieldLogger
	Installer                bminventory.InstallerInternals
	PullSecretUpdatesChannel chan event.GenericEvent
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
	c, err := r.Installer.GetClusterByKubeKey(types.NamespacedName{
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

	// Generate ISO
	isoParams := installer.GenerateClusterISOParams{
		ClusterID:         *c.ID,
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
	return ctrl.Result{Requeue: Requeue}, nil
}

func imageBeingCreated(err error) bool {
	return IsHTTPError(err, http.StatusConflict)
}

func (r *InstallEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.InstallEnv{}).
		Watches(&source.Channel{Source: r.PullSecretUpdatesChannel},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
