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
	"time"

	"github.com/jinzhu/gorm"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

const (
	ImageCreated       = "ImageCreated"
	ImageBeingCreated  = "ImageBeingCreated"
	ImageCreationError = "ImageCreationError"
)

type ImageState struct {
	State     string
	StateInfo string
}

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
	var imageInfo *models.ImageInfo
	var updatedCluster *common.Cluster
	imageState := ImageState{
		State:     ImageCreationError,
		StateInfo: adiiov1alpha1.ImageStateFailedToCreate,
	}

	kubeKey := types.NamespacedName{
		Name:      installEnv.Spec.ClusterRef.Name,
		Namespace: installEnv.Spec.ClusterRef.Namespace,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}

	if err := r.Get(ctx, kubeKey, clusterDeployment); err != nil {
		clusterDeploymentRefErr := newKubeAPIError(
			errors.Wrapf(
				err,
				fmt.Sprintf("failed to find clusterDeployment with name %s in namespace %s",
					installEnv.Spec.ClusterRef.Name, installEnv.Spec.ClusterRef.Namespace)),
			k8serrors.IsNotFound(err),
		)
		return r.updateStatusAndReturnResult(ctx, installEnv, nil, imageState, clusterDeploymentRefErr)
	}

	c, err := r.Installer.GetClusterByKubeKey(types.NamespacedName{
		Name:      clusterDeployment.Name,
		Namespace: clusterDeployment.Namespace,
	})

	if gorm.IsRecordNotFoundError(err) {
		inventoryErr = common.NewApiError(http.StatusNotFound, err)
	} else if err != nil {
		inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
	} else {
		isoParams := installer.GenerateClusterISOParams{
			ClusterID:         *c.ID,
			ImageCreateParams: &models.ImageCreateParams{},
		}
		if len(installEnv.Spec.SSHAuthorizedKeys) > 0 {
			isoParams.ImageCreateParams.SSHPublicKey = installEnv.Spec.SSHAuthorizedKeys[0]
		}
		updatedCluster, inventoryErr = r.Installer.GenerateClusterISOInternal(ctx, isoParams)
	}

	if inventoryErr == nil {
		imageInfo = updatedCluster.ImageInfo
		imageState.State = ImageBeingCreated
		imageState.StateInfo = adiiov1alpha1.ImageStateCreated
	}

	return r.updateStatusAndReturnResult(ctx, installEnv, imageInfo, imageState, inventoryErr)
}

func (r *InstallEnvReconciler) updateStatusAndReturnResult(
	ctx context.Context,
	installEnv *adiiov1alpha1.InstallEnv,
	imageInfo *models.ImageInfo,
	imageState ImageState,
	err error) (ctrl.Result, error) {

	var res ctrl.Result

	if imageBeingCreated(err) {
		imageState.State = ImageCreated
		imageState.StateInfo = adiiov1alpha1.ImageStateCreated
		// Clear up the error stateInfo while installEnv is being created
		err = nil
		r.Log.Infof("Image %s being prepared for cluster %s stateInfo: %s",
			installEnv.Name, installEnv.ClusterName, imageState.StateInfo)
	} else if isClientError(err) {
		imageState.State = ImageCreationError
		imageState.StateInfo += ": " + err.Error()
	} else if err != nil {
		imageState.State = ImageCreationError
		imageState.StateInfo += ": internal error"
		res.Requeue = true
	}
	r.setStateAndStateInfo(adiiov1alpha1.ImageProgressCondition, imageState, installEnv)

	if err == nil && imageInfo != nil {
		installEnv.Status.ISODownloadURL = imageInfo.DownloadURL
	} else if err != nil {
		r.Log.WithError(err).Error("installEnv reconcile failed")
	}

	if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update installEnv status")
		res.Requeue = true
		return res, nil
	}

	return res, nil
}

func (r *InstallEnvReconciler) setStateAndStateInfo(conditionType adiiov1alpha1.InstallEnvConditionType,
	imageState ImageState, installEnv *adiiov1alpha1.InstallEnv) {
	r.setCondition(adiiov1alpha1.InstallEnvCondition{
		Type:               conditionType,
		Status:             corev1.ConditionUnknown,
		LastProbeTime:      metav1.Time{Time: time.Now()},
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             imageState.State,
		Message:            imageState.StateInfo,
	}, &installEnv.Status.Conditions)
}

func (r *InstallEnvReconciler) setCondition(condition adiiov1alpha1.InstallEnvCondition, conditions *[]adiiov1alpha1.InstallEnvCondition) {
	if index := findInstallEnvConditionIndexByType(condition.Type, conditions); index >= 0 {
		(*conditions)[index] = condition
	} else {
		*conditions = append(*conditions, condition)
	}
}

func findInstallEnvConditionIndexByType(conditionType adiiov1alpha1.InstallEnvConditionType, conditions *[]adiiov1alpha1.InstallEnvCondition) int {
	if conditions == nil {
		return -1
	}
	for cIndex, c := range *conditions {
		if c.Type == conditionType {
			return cIndex
		}
	}
	return -1
}

func (r *InstallEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.InstallEnv{}).
		Watches(&source.Channel{Source: r.PullSecretUpdatesChannel},
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}
