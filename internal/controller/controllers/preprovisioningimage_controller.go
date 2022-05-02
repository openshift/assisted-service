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
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	"google.golang.org/appengine/log"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"

	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type imageConditionReason string

const (
	reasonImageSuccess imageConditionReason = "ImageSuccess"
	reasonImageWaiting imageConditionReason = "WaitingForImage"
)

// PreprovisioningImage reconciles a AgentClusterInstall object
type PreprovisioningImageReconciler struct {
	client.Client
	Log    logrus.FieldLogger
	Format models.ImageType
}

// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get;update;patch
func (r *PreprovisioningImageReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"preprovisioning_image":           req.Name,
			"preprovisioning_image_namespace": req.Namespace,
		})

	defer func() {
		log.Info("PreprovisioningImage Reconcile ended")
	}()

	log.Info("PreprovisioningImage Reconcile started")

	// Retrieve PreprovisioningImage
	image := &metal3_v1alpha1.PreprovisioningImage{}
	err := r.Get(ctx, req.NamespacedName, image)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("PreprovisioningImage not found")
			err = nil
		}
		return ctrl.Result{}, err
	}
	// Consider adding finalizer in case we need to cleanup resources
	// Retrieve InfraEnv
	infraEnv, err := r.findInfraEnvForPreprovisioningImage(ctx, log, image)
	if err != nil {
		return ctrl.Result{}, err
	}
	if infraEnv == nil {
		log.Info("failed to get corresponding infraEnv")
		return ctrl.Result{}, nil
	}
	if image.Status.ImageUrl == infraEnv.Status.ISODownloadURL {
		log.Info(ctx, "PreprovisioningImage and InfraEnv images are in sync. Nothing to update.")
		return ctrl.Result{}, nil
	}
	radyCondition : getCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition)
	err = r.setImage(image.GetGeneration(), &image.Status, infraEnv.Status.ISODownloadURL, infraEnv.Spec.CpuArchitecture)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("updating status")
	err = r.Status().Update(ctx, image)
	log.Info("PreprovisioningImage Reconcile successfully completed")
	return ctrl.Result{}, nil
}

func (r *PreprovisioningImageReconciler) setImage(generation int64, status *metal3_v1alpha1.PreprovisioningImageStatus, url string, arch string) error {
	r.Log.Info("Updating PreprovisioningImage ImageUrl to: %s", url)
	newStatus := status.DeepCopy()
	newStatus.ImageUrl = url
	newStatus.Architecture = arch
	switch r.Format {
	case models.ImageTypeFullIso:
		newStatus.Format = metal3_v1alpha1.ImageFormatISO
	case models.ImageTypeMinimalIso:
		newStatus.Format = metal3_v1alpha1.ImageFormatInitRD
	default:
		return fmt.Errorf("unsupported image format %s", r.Format)
	}
	time := metav1.Now()
	reason := reasonImageWaiting
	ready := metav1.ConditionFalse
	message := "waiting for image"
	if url != "" {
		reason = reasonImageSuccess
		ready = metav1.ConditionTrue
		message = "Generated image"
	}
	setImageCondition(generation, newStatus,
		metal3_v1alpha1.ConditionImageReady, ready,
		time, reason, message)
	setImageCondition(generation, newStatus,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionFalse,
		time, reason, "")

	*status = *newStatus
	return nil
}

func setImageCondition(generation int64, status *metal3_v1alpha1.PreprovisioningImageStatus,
	cond metal3_v1alpha1.ImageStatusConditionType, newStatus metav1.ConditionStatus,
	time metav1.Time, reason imageConditionReason, message string) {
	newCondition := metav1.Condition{
		Type:               string(cond),
		Status:             newStatus,
		LastTransitionTime: time,
		ObservedGeneration: generation,
		Reason:             string(reason),
		Message:            message,
	}
	meta.SetStatusCondition(&status.Conditions, newCondition)
}

// Find `PreprovisioningImage` resources that match an InfraEnv
//
// Only `PreprovisioningImage` resources that have a label with a
// reference to an InfraEnv
func (r *PreprovisioningImageReconciler) findPreprovisioningImageByInfraEnv(ctx context.Context, infraEnv *aiv1beta1.InfraEnv) ([]*metal3_v1alpha1.PreprovisioningImage, error) {
	imageList := metal3_v1alpha1.PreprovisioningImageList{}
	err := r.Client.List(ctx, &imageList, client.InNamespace(infraEnv.Namespace))
	if err != nil {
		return nil, err
	}

	images := []*metal3_v1alpha1.PreprovisioningImage{}

	for i, image := range imageList.Items {
		if val, ok := image.ObjectMeta.Labels[InfraEnvLabel]; ok {
			if strings.EqualFold(val, infraEnv.Name) {
				images = append(images, &imageList.Items[i])
			}
		}
	}
	return images, nil
}

func (r *PreprovisioningImageReconciler) findInfraEnvForPreprovisioningImage(ctx context.Context, log logrus.FieldLogger, image *metal3_v1alpha1.PreprovisioningImage) (*aiv1beta1.InfraEnv, error) {
	for key, value := range image.Labels {
		log.Debugf("PreprovisioningImage label %s value %s", key, value)

		// Find the `InfraEnvLabel` and get the infraEnv CR with a name matching the value
		if key == InfraEnvLabel {
			infraEnv := &aiv1beta1.InfraEnv{}

			log.Debugf("Loading InfraEnv %s", value)
			if err := r.Get(ctx, types.NamespacedName{Name: value, Namespace: image.Namespace}, infraEnv); err != nil {
				log.WithError(err).Errorf("failed to get infraEnv resource %s/%s", image.Namespace, value)
				return nil, client.IgnoreNotFound(err)
			}
			return infraEnv, nil
		}
	}
	return nil, nil
}
func (r *PreprovisioningImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapInfraEnvPPI := func(a client.Object) []reconcile.Request {
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

		images, err := r.findPreprovisioningImageByInfraEnv(ctx, infraEnv)
		if len(images) == 0 || err != nil {
			return []reconcile.Request{}
		}

		reconcileRequests := []reconcile.Request{}

		shouldReconcileImage := func(image *metal3_v1alpha1.PreprovisioningImage, infraEnv *aiv1beta1.InfraEnv) bool {
			if infraEnv.Status.ISODownloadURL == "" {
				log.Infof(ctx, "InfraEnv corresponding to the PreprovisioningImage has no image URL available.")
				return false
			}
			// The Image URL and InfraEnv's URL is the same.
			// nothing else to do.
			if image.Status.ImageUrl == infraEnv.Status.ISODownloadURL {
				log.Infof(ctx, "PreprovisioningImage and InfraEnv images are in sync. Nothing to update.")
				return false
			}
			return true
		}
		for i, _ := range images {
			// Don't queue if shouldReconcileImage explicitly tells us not to do so.
			if shouldReconcileImage(images[i], infraEnv) {
				reconcileRequests = append(reconcileRequests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: images[i].Namespace,
						Name:      images[i].Name,
					},
				})
			}
		}
		return reconcileRequests
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&metal3_v1alpha1.PreprovisioningImage{}).
		Watches(&source.Kind{Type: &metal3_v1alpha1.PreprovisioningImage{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &aiv1beta1.InfraEnv{}}, handler.EnqueueRequestsFromMapFunc(mapInfraEnvPPI)).
		Complete(r)
}
