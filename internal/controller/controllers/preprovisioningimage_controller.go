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
	"strconv"
	"time"

	"github.com/iancoleman/strcase"
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type imageConditionReason string

// PreprovisioningImage reconciles a AgentClusterInstall object
type PreprovisioningImageReconciler struct {
	client.Client
	Log                    logrus.FieldLogger
	Installer              bminventory.InstallerInternals
	CRDEventsHandler       CRDEventsHandler
	IronicIgniotionBuilder ignition.IronicIgniotionBuilder
	VersionsHandler        versions.Handler
	OcRelease              oc.Release
	ReleaseImageMirror     string
	IronicServiceURL       string
}

// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get

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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !funk.Some(image.Spec.AcceptFormats, metal3_v1alpha1.ImageFormatISO, metal3_v1alpha1.ImageFormatInitRD) {
		// Currently, the PreprovisioningImageController only support ISO and InitRD image
		log.Infof("Unsupported image format: %s", image.Spec.AcceptFormats)
		setUnsupportedFormatCondition(image)
		err = r.Status().Update(ctx, image)
		if err != nil {
			log.WithError(err).Error("failed to update status")
		}
		return ctrl.Result{}, err
	}
	// Consider adding finalizer in case we need to clean up resources
	// Retrieve InfraEnv
	infraEnv, err := r.findInfraEnvForPreprovisioningImage(ctx, log, image)
	if err != nil {
		log.WithError(err).Error("failed to get corresponding infraEnv")
		return ctrl.Result{}, err
	}

	if infraEnv == nil {
		log.Info("failed to find infraEnv for image")
		return ctrl.Result{}, nil
	}
	if !IronicAgentEnabled(log, infraEnv) {
		return r.AddIronicAgentToInfraEnv(ctx, log, infraEnv)
	}

	if infraEnv.Status.CreatedTime == nil {
		log.Info("InfraEnv image has not been created yet")
		setNotCreatedCondition(image)
		err = r.Status().Update(ctx, image)
		if err != nil {
			log.WithError(err).Error("failed to update status")
			return ctrl.Result{}, err
		}
		// no need to requeue, the change in the infraenv should trigger a reconcile
		return ctrl.Result{}, nil
	}

	// The image has been created sooner than the specified cooldown period
	imageTimePlusCooldown := infraEnv.Status.CreatedTime.Time.Add(InfraEnvImageCooldownPeriod)
	if imageTimePlusCooldown.After(time.Now()) {
		log.Info("InfraEnv image is too recent. Requeuing and retrying again soon")
		setCoolDownCondition(image)
		err = r.Status().Update(ctx, image)
		if err != nil {
			log.WithError(err).Error("failed to update status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Until(imageTimePlusCooldown)}, nil
	}

	if image.Status.ImageUrl == infraEnv.Status.ISODownloadURL {
		log.Info("PreprovisioningImage and InfraEnv images are in sync. Nothing to update.")
		return ctrl.Result{}, nil
	}
	err = r.setImage(image, *infraEnv)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("updating status")
	err = r.Status().Update(ctx, image)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("PreprovisioningImage updated successfully")
	return ctrl.Result{}, nil
}

// getConvergedDiscoveryTemplate merge the ironic ignition with the discovery ignition
func (r *PreprovisioningImageReconciler) getIronicIgnitionConfig(log logrus.FieldLogger, infraEnvInternal common.InfraEnv, ironicAgentImage string) (string, error) {
	config, err := r.IronicIgniotionBuilder.GenerateIronicConfig(r.IronicServiceURL, infraEnvInternal, ironicAgentImage)
	if err != nil {
		log.WithError(err).Error("failed to generate Ironic ignition config")
		return "", err
	}
	return string(config), err
}

func (r *PreprovisioningImageReconciler) setImage(image *metal3_v1alpha1.PreprovisioningImage, infraEnv aiv1beta1.InfraEnv) error {
	r.Log.Infof("Updating PreprovisioningImage ImageUrl to: %s", infraEnv.Status.ISODownloadURL)
	image.Status.Architecture = infraEnv.Spec.CpuArchitecture
	if funk.Contains(image.Spec.AcceptFormats, metal3_v1alpha1.ImageFormatISO) {
		r.Log.Infof("Updating PreprovisioningImage ImageUrl with ISO artifacts")
		image.Status.Format = metal3_v1alpha1.ImageFormatISO
		image.Status.ImageUrl = infraEnv.Status.ISODownloadURL
		image.Status.KernelUrl = ""
		image.Status.ExtraKernelParams = ""
	} else if funk.Contains(image.Spec.AcceptFormats, metal3_v1alpha1.ImageFormatInitRD) {
		r.Log.Infof("Updating PreprovisioningImage ImageUrl with InitRD artifacts")
		image.Status.Format = metal3_v1alpha1.ImageFormatInitRD
		image.Status.ImageUrl = infraEnv.Status.BootArtifacts.InitrdURL
		image.Status.KernelUrl = infraEnv.Status.BootArtifacts.KernelURL
		image.Status.ExtraKernelParams = fmt.Sprintf("coreos.live.rootfs_url=%s", infraEnv.Status.BootArtifacts.RootfsURL)
	}
	imageCreatedCondition := conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition)
	reason := imageConditionReason(imageCreatedCondition.Reason)
	ready := metav1.ConditionStatus(imageCreatedCondition.Status)
	message := imageCreatedCondition.Message
	generation := image.GetGeneration()
	setImageCondition(generation, &image.Status,
		metal3_v1alpha1.ConditionImageReady, ready,
		reason, message)

	// infraEnv only have ImageCreatedCondition we will set the PreprovisioningImage ConditionImageError to true
	// if the ImageCreatedCondition reason is ImageCreationErrorReason
	imageErrorStatus := metav1.ConditionFalse
	if imageCreatedCondition.Reason == aiv1beta1.ImageCreationErrorReason {
		imageErrorStatus = metav1.ConditionTrue
	}
	setImageCondition(generation, &image.Status,
		metal3_v1alpha1.ConditionImageError, imageErrorStatus,
		reason, message)
	return nil
}

func setNotCreatedCondition(image *metal3_v1alpha1.PreprovisioningImage) {
	message := "Waiting for InfraEnv image to be created"
	reason := imageConditionReason(strcase.ToCamel(message))
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionFalse,
		reason, message)
}

func setCoolDownCondition(image *metal3_v1alpha1.PreprovisioningImage) {
	message := "Waiting for InfraEnv image to cool down"
	reason := imageConditionReason(strcase.ToCamel(message))
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionFalse,
		reason, message)

}

func setUnsupportedFormatCondition(image *metal3_v1alpha1.PreprovisioningImage) {
	message := "Unsupported image format"
	reason := imageConditionReason(strcase.ToCamel(message))
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionTrue,
		reason, message)
}

func setImageCondition(generation int64, status *metal3_v1alpha1.PreprovisioningImageStatus,
	cond metal3_v1alpha1.ImageStatusConditionType, newStatus metav1.ConditionStatus,
	reason imageConditionReason, message string) {
	newCondition := metav1.Condition{
		Type:               string(cond),
		Status:             newStatus,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: generation,
		Reason:             string(reason),
		Message:            message,
	}
	meta.SetStatusCondition(&status.Conditions, newCondition)
}

// Find `PreprovisioningImage` resources that match an InfraEnv
// Only `PreprovisioningImage` resources that have a label with a reference to an InfraEnv
func (r *PreprovisioningImageReconciler) findPreprovisioningImagesByInfraEnv(ctx context.Context, infraEnv *aiv1beta1.InfraEnv) ([]metal3_v1alpha1.PreprovisioningImage, error) {
	infraenvLabel, err := labels.NewRequirement(InfraEnvLabel, selection.Equals, []string{infraEnv.Name})
	if err != nil {
		return []metal3_v1alpha1.PreprovisioningImage{}, errors.Wrapf(err, "invalid label selector for InfraEnv: %v", infraEnv.Name)
	}
	selector := labels.NewSelector().Add(*infraenvLabel)
	imageList := metal3_v1alpha1.PreprovisioningImageList{}
	err = r.Client.List(ctx, &imageList, client.InNamespace(infraEnv.Namespace), &client.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	return imageList.Items, nil
}

func (r *PreprovisioningImageReconciler) findInfraEnvForPreprovisioningImage(ctx context.Context, log logrus.FieldLogger, image *metal3_v1alpha1.PreprovisioningImage) (*aiv1beta1.InfraEnv, error) {
	// Find the `InfraEnvLabel` and get the infraEnv CR with a name matching the value
	if value, ok := image.ObjectMeta.Labels[InfraEnvLabel]; ok {
		infraEnv := &aiv1beta1.InfraEnv{}
		log.Debugf("Loading InfraEnv %s", value)
		if err := r.Get(ctx, types.NamespacedName{Name: value, Namespace: image.Namespace}, infraEnv); err != nil {
			log.WithError(err).Errorf("failed to get infraEnv resource %s/%s", image.Namespace, value)
			return nil, client.IgnoreNotFound(err)
		}
		return infraEnv, nil
	}
	return nil, nil
}

func (r *PreprovisioningImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapInfraEnvPPI := r.mapInfraEnvPPI()
	return ctrl.NewControllerManagedBy(mgr).
		For(&metal3_v1alpha1.PreprovisioningImage{}).
		Watches(&source.Kind{Type: &metal3_v1alpha1.PreprovisioningImage{}}, &handler.EnqueueRequestForObject{}).
		Watches(&source.Kind{Type: &aiv1beta1.InfraEnv{}}, handler.EnqueueRequestsFromMapFunc(mapInfraEnvPPI)).
		Complete(r)
}

func (r *PreprovisioningImageReconciler) mapInfraEnvPPI() func(a client.Object) []reconcile.Request {
	mapInfraEnvPPI := func(a client.Object) []reconcile.Request {
		ctx := context.Background()
		log := logutil.FromContext(ctx, r.Log).WithFields(
			logrus.Fields{
				"infra_env":           a.GetName(),
				"infra_env_namespace": a.GetNamespace(),
			})
		infraEnv := &aiv1beta1.InfraEnv{}

		if err := r.Get(ctx, types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}, infraEnv); err != nil {
			return []reconcile.Request{}
		}

		images, err := r.findPreprovisioningImagesByInfraEnv(ctx, infraEnv)
		if err != nil {
			log.WithError(err).Infof("failed getting InfraEnv related preprovisioningImages %s/%s ", a.GetNamespace(), a.GetName())
			return []reconcile.Request{}
		}
		if len(images) == 0 {
			return []reconcile.Request{}
		}

		reconcileRequests := []reconcile.Request{}

		for i := range images {
			// Don't queue if the Image URL and InfraEnv's URL is the same.
			if images[i].Status.ImageUrl != infraEnv.Status.ISODownloadURL {
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
	return mapInfraEnvPPI
}

func (r *PreprovisioningImageReconciler) AddIronicAgentToInfraEnv(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) (ctrl.Result, error) {
	// Retrieve infraenv from the database
	key := types.NamespacedName{
		Name:      infraEnv.Name,
		Namespace: infraEnv.Namespace,
	}
	infraEnvInternal, err := r.Installer.GetInfraEnvByKubeKey(key)
	if err != nil {
		log.WithError(err).Error("failed to get corresponding infraEnv")
		return ctrl.Result{}, err
	}
	ironicAgentImage := ""
	if infraEnvInternal.OpenshiftVersion != "" {
		ironicAgentImage, err = r.getIronicAgentImage(log, *infraEnvInternal)
		if err != nil {
			log.WithError(err).Warningf("Failed to get ironicAgentImage for infraEnv: %s", infraEnv.Name)
		}
	}

	// if the infraEnv doesn't have the enableIronicAgent annotation add the ironicIgnition to the invfraEnv
	// set the annotation and notify the infraEnv changed
	conf, err := r.getIronicIgnitionConfig(log, *infraEnvInternal, ironicAgentImage)
	if err != nil {
		return ctrl.Result{}, err
	}

	_, err = r.Installer.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{InfraEnvID: *infraEnvInternal.ID, InfraEnvUpdateParams: &models.InfraEnvUpdateParams{}}, &conf)
	if err != nil {
		return ctrl.Result{}, err
	}
	if infraEnv.ObjectMeta.Annotations == nil {
		infraEnv.ObjectMeta.Annotations = make(map[string]string)
	}

	infraEnv.Annotations[EnableIronicAgentAnnotation] = "true"
	err = r.Client.Update(ctx, infraEnv)
	if err != nil {
		return ctrl.Result{}, err
	}
	// TODO: if the annotation is enough to trigger the infraEnv reconciliation remove the notification
	r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	return ctrl.Result{}, err
}

func (r *PreprovisioningImageReconciler) getIronicAgentImage(log logrus.FieldLogger, infraEnv common.InfraEnv) (string, error) {
	SupportConvergedFlow, _ := common.VersionGreaterOrEqual(infraEnv.OpenshiftVersion, MinimalVersionForConvergedFlow)
	// Get the ironic agent image from the release only if the openshift version is higher then the MinimalVersionForConvergedFlow
	if !SupportConvergedFlow {
		r.Log.Infof("Openshift version (%s) is lower than the minimal version for the converged flow (%s)."+
			" this means that the service will use the default ironic agent image and not the ironic agent image from the release",
			infraEnv.OpenshiftVersion, MinimalVersionForConvergedFlow)
		return "", nil
	}
	releaseImage, err := r.VersionsHandler.GetReleaseImage(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return "", err
	}
	ironicAgentImage, err := r.OcRelease.GetIronicAgentImage(log, *releaseImage.URL, r.ReleaseImageMirror, infraEnv.PullSecret)
	if err != nil {
		return "", err
	}
	return ironicAgentImage, nil
}

func IronicAgentEnabled(log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) bool {
	value, ok := infraEnv.GetAnnotations()[EnableIronicAgentAnnotation]
	if !ok {
		return false
	}
	log.Debugf("InfraEnv annotation %s value %s", EnableIronicAgentAnnotation, value)
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		log.WithError(err).Errorf("failed to parse %s to bool value", value)
	}
	return enabled
}
