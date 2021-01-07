/*
Copyright 2020.

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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/bminventory"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Log       logrus.FieldLogger
	Scheme    *runtime.Scheme
	Installer bminventory.InstallerInternals
}

// +kubebuilder:rbac:groups=adi.io.my.domain,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=images/status,verbs=get;update;patch

func (r *ImageReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	image := &adiiov1alpha1.Image{}
	if err := r.Get(ctx, req.NamespacedName, image); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.create(ctx, image)
}

func (r *ImageReconciler) create(ctx context.Context, image *adiiov1alpha1.Image) (ctrl.Result, error) {
	state := adiiov1alpha1.ImageStateFailedToCreate

	cluster, clusterRefErr := r.getClusterByRef(ctx, image.Spec.ClusterRef)
	if clusterRefErr != nil {
		return r.updateStatusAndReturnResult(ctx, image, nil, state, clusterRefErr)
	}

	var imageInfo *models.ImageInfo
	updatedCluster, inventoryErr := r.Installer.GenerateClusterISOInternal(
		ctx,
		installer.GenerateClusterISOParams{
			ClusterID: strfmt.UUID(cluster.Status.ID),
			ImageCreateParams: &models.ImageCreateParams{
				SSHPublicKey:    image.Spec.SSHPublicKey,
				StaticIpsConfig: image.Spec.StaticIpConfiguration,
			},
		})
	if inventoryErr == nil {
		imageInfo = updatedCluster.ImageInfo
		state = adiiov1alpha1.ImageStateCreated
	}

	return r.updateStatusAndReturnResult(ctx, image, imageInfo, state, inventoryErr)
}

func (r *ImageReconciler) getClusterByRef(
	ctx context.Context,
	ref *corev1.ObjectReference) (*adiiov1alpha1.Cluster, error) {
	key := types.NamespacedName{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}
	cluster := &adiiov1alpha1.Cluster{}
	if err := r.Get(ctx, key, cluster); err != nil {
		return nil, newKubeAPIError(
			errors.Wrapf(
				err,
				fmt.Sprintf(
					"failed to find cluster with name %s in namespace %s",
					ref.Name, ref.Namespace)),
			k8serrors.IsNotFound(err))
	}
	return cluster, nil
}

func (r *ImageReconciler) updateStatusAndReturnResult(
	ctx context.Context,
	image *adiiov1alpha1.Image,
	imageInfo *models.ImageInfo,
	state string,
	err error) (ctrl.Result, error) {

	var res ctrl.Result

	if isClientError(err) {
		state += ": " + err.Error()
	} else if err != nil {
		state += ": internal error"
		res.Requeue = true
	}
	image.Status.State = state

	if err == nil && imageInfo != nil {
		image.Status.SizeBytes = int(*imageInfo.SizeBytes)
		image.Status.DownloadUrl = imageInfo.DownloadURL
		image.Status.ExpirationTime = &v1.Time{Time: time.Time(imageInfo.ExpiresAt)}
	} else if err != nil {
		r.Log.WithError(err).Error("image reconcile failed")
	}

	if updateErr := r.Status().Update(ctx, image); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update image status")
		res.Requeue = true
		return res, nil
	}

	return res, nil
}

func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.Image{}).
		Complete(r)
}
