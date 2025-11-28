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
	"strings"
	"time"

	"github.com/go-openapi/swag"
	"github.com/iancoleman/strcase"
	metal3_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/controllers/mirrorregistry"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/network"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type imageConditionReason string

const (
	archMismatchReason                = "InfraEnvArchMismatch"
	PreprovisioningImageFinalizerName = "preprovisioningimage." + aiv1beta1.Group + "/ai-deprovision"
)

type PreprovisioningImageControllerConfig struct {
	// The default ironic agent image was obtained by running "oc adm release info --image-for=ironic-agent  quay.io/openshift-release-dev/ocp-release:4.11.0-fc.0-x86_64"
	BaremetalIronicAgentImage string `envconfig:"IRONIC_AGENT_IMAGE" default:"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:d3f1d4d3cd5fbcf1b9249dd71d01be4b901d337fdc5f8f66569eb71df4d9d446"`
	// The default ironic agent image for arm architecture was obtained by running "oc adm release info --image-for=ironic-agent quay.io/openshift-release-dev/ocp-release@sha256:1b8e71b9bccc69c732812ebf2bfba62af6de77378f8329c8fec10b63a0dbc33c"
	// The release image digest for arm architecture was obtained from this link https://mirror.openshift.com/pub/openshift-v4/aarch64/clients/ocp-dev-preview/4.11.0-fc.0/release.txt
	BaremetalIronicAgentImageForArm string `envconfig:"IRONIC_AGENT_IMAGE_ARM" default:"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:cb0edf19fffc17f542a7efae76939b1e9757dc75782d4727fb0aa77ed5809b43"`
}

// PreprovisioningImage reconciles a AgentClusterInstall object
type PreprovisioningImageReconciler struct {
	client.Client
	Log                     logrus.FieldLogger
	Installer               bminventory.InstallerInternals
	CRDEventsHandler        CRDEventsHandler
	VersionsHandler         versions.Handler
	OcRelease               oc.Release
	ReleaseImageMirror      string
	Config                  PreprovisioningImageControllerConfig
	hubIronicAgentImage     string
	hubReleaseArchitectures []string
	BMOUtils                BMOUtils
}

// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=metal3.io,resources=preprovisioningimages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch

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

	if !image.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handlePreprovisioningImageDeletion(ctx, log, image)
	}

	if !funk.ContainsString(image.GetFinalizers(), PreprovisioningImageFinalizerName) {
		return r.ensurePreprovisioningImageFinalizer(ctx, log, image)
	}

	if !funk.Some(image.Spec.AcceptFormats, metal3_v1alpha1.ImageFormatISO, metal3_v1alpha1.ImageFormatInitRD) {
		// Currently, the PreprovisioningImageController only support ISO and InitRD image
		log.Infof("Unsupported image format: %s", image.Spec.AcceptFormats)
		return ctrl.Result{}, r.patchImageStatus(ctx, log, image, setUnsupportedFormatCondition)
	}

	// Retrieve InfraEnv
	infraEnv, err := r.findInfraEnvForPreprovisioningImage(ctx, log, image)
	if err != nil {
		log.WithError(err).Error("failed to get corresponding infraEnv")
		return ctrl.Result{}, err
	}
	if infraEnv == nil || !infraEnv.DeletionTimestamp.IsZero() {
		log.Warn("infraEnv is not found or is being deleted")
		return ctrl.Result{}, r.patchImageStatus(ctx, log, image, setInfraEnvNotAvailableCondition)
	}

	log = log.WithFields(logrus.Fields{
		"infra_env":           infraEnv.Name,
		"infra_env_namespace": infraEnv.Namespace,
	})

	imageArch := common.NormalizeCPUArchitecture(image.Spec.Architecture)
	infraArch := common.NormalizeCPUArchitecture(infraEnv.Spec.CpuArchitecture)
	if infraArch != imageArch {
		log.Infof("Image arch %s does not match infraEnv arch %s", imageArch, infraArch)
		return ctrl.Result{}, r.patchImageStatus(ctx, log, image, func(img *metal3_v1alpha1.PreprovisioningImage) {
			setMismatchedArchCondition(img, imageArch, infraArch)
		})
	}

	infraEnvUpdated, err := r.AddIronicAgentToInfraEnv(ctx, log, infraEnv)
	if infraEnvUpdated {
		return ctrl.Result{}, nil
	}
	if err != nil {
		patchErr := r.patchImageStatus(ctx, log, image, func(img *metal3_v1alpha1.PreprovisioningImage) {
			setIronicAgentIgnitionFailureCondition(img, err)
		})
		if patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, err
	}

	if infraEnv.Status.CreatedTime == nil {
		log.Info("InfraEnv image has not been created yet")
		// If the status updated successfully, no need to requeue, the change in the infraenv should trigger a reconcile
		return ctrl.Result{}, r.patchImageStatus(ctx, log, image, setNotCreatedCondition)
	}

	// The image has been created sooner than the specified cooldown period
	imageTimePlusCooldown := infraEnv.Status.CreatedTime.Time.Add(InfraEnvImageCooldownPeriod)
	if imageTimePlusCooldown.After(time.Now()) {
		log.Info("InfraEnv image is too recent. Requeuing and retrying again soon")
		err = r.patchImageStatus(ctx, log, image, setCoolDownCondition)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Until(imageTimePlusCooldown)}, nil
	}

	return ctrl.Result{}, r.handleImageUpdate(ctx, log, image, infraEnv)
}

func clearImageStatus(image *metal3_v1alpha1.PreprovisioningImage) {
	image.Status.ImageUrl = ""
	image.Status.KernelUrl = ""
	image.Status.ExtraKernelParams = ""
	image.Status.Format = ""
	image.Status.Architecture = ""
}

func (r *PreprovisioningImageReconciler) handleImageUpdate(ctx context.Context, log logrus.FieldLogger, image *metal3_v1alpha1.PreprovisioningImage, infraEnv *aiv1beta1.InfraEnv) error {
	bmh, err := r.getBMH(ctx, image)
	if err != nil {
		return err
	}

	imageUpdated := image.Status.ImageUrl != "" && image.Status.ImageUrl != infraEnv.Status.ISODownloadURL
	log.Info("Setting images in PreprovisioningImage status")
	err = r.patchImageStatus(ctx, log, image, func(img *metal3_v1alpha1.PreprovisioningImage) {
		r.setImage(img, *infraEnv)
	})
	if err != nil {
		return err
	}
	log.Info("Images successfully set in PreprovisioningImage status")

	if imageUpdated && bmh.Status.Provisioning.State != metal3_v1alpha1.StateProvisioned {
		log.Info("Setting reboot annotation on BMH")
		if err = r.setBMHRebootAnnotation(ctx, bmh); err != nil {
			log.WithError(err).Error("failed to set BMH reboot annotation")
			return err
		}
	}

	return nil
}

func initrdExtraKernelParams(infraEnv aiv1beta1.InfraEnv) string {
	params := []string{fmt.Sprintf("coreos.live.rootfs_url=%s rd.bootif=0", infraEnv.Status.BootArtifacts.RootfsURL)}
	for _, arg := range infraEnv.Spec.KernelArguments {
		if arg.Operation == models.KernelArgumentOperationAppend {
			params = append(params, arg.Value)
		}
	}
	return strings.Join(params, " ")
}

func (r *PreprovisioningImageReconciler) setImage(image *metal3_v1alpha1.PreprovisioningImage, infraEnv aiv1beta1.InfraEnv) {
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
		image.Status.ExtraKernelParams = initrdExtraKernelParams(infraEnv)
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
}

func setNotCreatedCondition(image *metal3_v1alpha1.PreprovisioningImage) {
	clearImageStatus(image)
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
	clearImageStatus(image)
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
	clearImageStatus(image)
	message := "Unsupported image format"
	reason := imageConditionReason(strcase.ToCamel(message))
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionTrue,
		reason, message)
}

func setMismatchedArchCondition(image *metal3_v1alpha1.PreprovisioningImage, imageArch, infraArch string) {
	clearImageStatus(image)
	message := fmt.Sprintf("PreprovisioningImage CPU architecture (%s) does not match InfraEnv CPU architecture (%s)", imageArch, infraArch)
	reason := imageConditionReason(archMismatchReason)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionTrue,
		reason, message)
}

func setIronicAgentIgnitionFailureCondition(image *metal3_v1alpha1.PreprovisioningImage, err error) {
	clearImageStatus(image)
	message := fmt.Sprintf("Could not add ironic agent to image: %s", err.Error())
	reason := imageConditionReason("IronicAgentIgnitionUpdateFailure")
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionTrue,
		reason, message)
}

func setInfraEnvNotAvailableCondition(image *metal3_v1alpha1.PreprovisioningImage) {
	clearImageStatus(image)
	message := fmt.Sprintf("InfraEnv %s/%s is not found or is being deleted", image.Labels[InfraEnvLabel], image.Namespace)
	reason := imageConditionReason("InfraEnvNotAvailable")
	setImageCondition(image.GetGeneration(), &image.Status, metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse, reason, message)
	setImageCondition(image.GetGeneration(), &image.Status, metal3_v1alpha1.ConditionImageError, metav1.ConditionFalse, reason, message)
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&metal3_v1alpha1.PreprovisioningImage{}).
		Watches(&aiv1beta1.InfraEnv{}, handler.EnqueueRequestsFromMapFunc(r.mapInfraEnvPPI)).
		Watches(&metal3_v1alpha1.BareMetalHost{}, handler.EnqueueRequestsFromMapFunc(mapBMHtoPPI)).
		Complete(r)
}

func mapBMHtoPPI(ctx context.Context, a client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: a.GetNamespace(),
			Name:      a.GetName(),
		},
	}}
}

func (r *PreprovisioningImageReconciler) mapInfraEnvPPI(ctx context.Context, a client.Object) []reconcile.Request {
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
		reconcileRequests = append(reconcileRequests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: images[i].Namespace,
				Name:      images[i].Name,
			},
		})
	}
	return reconcileRequests
}

func (r *PreprovisioningImageReconciler) getIronicAgentImageFromUserOverride(log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) string {
	ironicAgentImage, ok := infraEnv.GetAnnotations()[ironicAgentImageOverrideAnnotation]
	if ok && ironicAgentImage != "" {
		log.Infof("Using override ironic agent image (%s) from infraEnv", ironicAgentImage)
		return ironicAgentImage
	}
	return ""
}

func (r *PreprovisioningImageReconciler) imageMatchesInfraenvArch(log logrus.FieldLogger, infraEnvInternal *common.InfraEnv, ironicImage string) bool {
	if ironicImage == "" {
		return false
	}

	architectures, err := r.OcRelease.GetImageArchitecture(log, ironicImage, infraEnvInternal.PullSecret)
	if err != nil {
		log.WithError(err).Info("Failed to get image architecture for ironic agent image")
		return false
	}

	matches := funk.Contains(architectures, infraEnvInternal.CPUArchitecture)
	if !matches {
		log.Infof("CPU architecture (%v) of Ironic agent image (%s) does not match infraEnv arch (%s)",
			architectures,
			ironicImage,
			infraEnvInternal.CPUArchitecture)
	}
	return matches
}

func (r *PreprovisioningImageReconciler) getIronicAgentImageFromHUB(ctx context.Context, log logrus.FieldLogger, infraEnvInternal *common.InfraEnv) string {
	image := r.hubIronicAgentImage
	architectures := r.hubReleaseArchitectures
	if image == "" {
		cv := &configv1.ClusterVersion{}
		err := r.Get(ctx, types.NamespacedName{Name: "version"}, cv)
		if err != nil {
			log.WithError(err).Warningf("Failed to get ClusterVersion")
			return ""
		}
		architectures, err = r.OcRelease.GetReleaseArchitecture(log, cv.Status.Desired.Image, r.ReleaseImageMirror, infraEnvInternal.PullSecret)
		if err != nil {
			log.WithError(err).Warningf("Failed to get release architecture for (%s)", cv.Status.Desired.Image)
			return ""
		}
		image, err = r.OcRelease.GetIronicAgentImage(log, cv.Status.Desired.Image, r.ReleaseImageMirror, infraEnvInternal.PullSecret)
		if err != nil {
			log.WithError(err).Warningf("Failed to get ironic agent image from release (%s)", cv.Status.Desired.Image)
			return ""
		}
		r.hubIronicAgentImage = image
		r.hubReleaseArchitectures = architectures
		log.Infof("Caching hub ironic agent image %s for architectures %v", image, architectures)
	}

	if !funk.Contains(architectures, infraEnvInternal.CPUArchitecture) {
		log.Warningf("Release image architectures %v do not match infraEnv architecture %s", architectures, infraEnvInternal.CPUArchitecture)
		return ""
	}

	log.Infof("Setting ironic agent image (%s) from HUB cluster", image)
	return image
}

func (r *PreprovisioningImageReconciler) getIronicAgentImageFromClusterImageSet(ctx context.Context, log logrus.FieldLogger, infraEnvInternal *common.InfraEnv) string {
	cv := &configv1.ClusterVersion{}
	err := r.Get(ctx, types.NamespacedName{Name: "version"}, cv)
	if err != nil {
		log.WithError(err).Warnf("Failed to get ClusterVersion for ClusterImageSet query")
		return ""
	}

	hubVersion := cv.Status.Desired.Version
	log.Infof("Querying ClusterImageSet for hub version %s and spoke architecture %s", hubVersion, infraEnvInternal.CPUArchitecture)

	releaseImage, err := r.VersionsHandler.GetReleaseImage(ctx, hubVersion, infraEnvInternal.CPUArchitecture, infraEnvInternal.PullSecret)
	if err != nil {
		log.WithError(err).Warnf("Failed to get release image from ClusterImageSet for version %s and arch %s", hubVersion, infraEnvInternal.CPUArchitecture)
		return ""
	}

	if releaseImage == nil || releaseImage.URL == nil || *releaseImage.URL == "" {
		log.Warnf("No release image found in ClusterImageSet for version %s and arch %s", hubVersion, infraEnvInternal.CPUArchitecture)
		return ""
	}

	ironicAgentImage, err := r.OcRelease.GetIronicAgentImage(log, *releaseImage.URL, r.ReleaseImageMirror, infraEnvInternal.PullSecret)
	if err != nil {
		log.WithError(err).Warnf("Failed to extract ironic agent image from release %s", *releaseImage.URL)
		return ""
	}

	log.Infof("Found ironic agent image (%s) from ClusterImageSet for spoke arch %s", ironicAgentImage, infraEnvInternal.CPUArchitecture)
	return ironicAgentImage
}

func (r *PreprovisioningImageReconciler) getIronicAgentDefaultImage(log logrus.FieldLogger, infraEnvInternal *common.InfraEnv) string {
	var ironicAgentImage string
	if infraEnvInternal.CPUArchitecture == common.ARM64CPUArchitecture {
		ironicAgentImage = r.Config.BaremetalIronicAgentImageForArm
	} else {
		ironicAgentImage = r.Config.BaremetalIronicAgentImage
	}

	log.Infof("Setting default ironic agent image (%s)", ironicAgentImage)
	return ironicAgentImage
}

// getIronicAgentImageByPriority returns the ironic agent image based on priority order
// Priority 1: User override annotation
// Priority 2: ICC config (if architecture matches)
// Priority 3: Hub release image (if architectures matches OR using a multi release image)
// Priority 4: ClusterImageSet query
// Priority 5: Default image (lowest priority)
func (r *PreprovisioningImageReconciler) getIronicAgentImageByPriority(
	ctx context.Context,
	log logrus.FieldLogger,
	infraEnv *aiv1beta1.InfraEnv,
	infraEnvInternal *common.InfraEnv,
	iccIronicAgentImage string,
) string {
	if image := r.getIronicAgentImageFromUserOverride(log, infraEnv); image != "" {
		return image
	}

	if iccIronicAgentImage != "" && r.imageMatchesInfraenvArch(log, infraEnvInternal, iccIronicAgentImage) {
		log.Infof("Setting ironic agent image (%s) from ICC config", iccIronicAgentImage)
		return iccIronicAgentImage
	}

	if image := r.getIronicAgentImageFromHUB(ctx, log, infraEnvInternal); image != "" {
		return image
	}

	if image := r.getIronicAgentImageFromClusterImageSet(ctx, log, infraEnvInternal); image != "" {
		return image
	}

	return r.getIronicAgentDefaultImage(log, infraEnvInternal)
}

func (r *PreprovisioningImageReconciler) getIronicConfig(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, infraEnvInternal *common.InfraEnv) (*ICCConfig, error) {
	iccConfig, err := r.BMOUtils.getICCConfig(ctx)
	if err != nil {
		log.WithError(err).Info("ICC configuration is not available")
	}

	if iccConfig != nil {
		log.Infof("Using ironic URLs from ICC config (Base: %s, Inspector: %s)", iccConfig.IronicBaseURL, iccConfig.IronicInspectorBaseUrl)
	} else {
		iccConfig = &ICCConfig{}
		if err := r.fillIronicServiceURLs(ctx, infraEnv, infraEnvInternal, iccConfig); err != nil {
			return nil, err
		}
	}

	iccConfig.IronicAgentImage = r.getIronicAgentImageByPriority(ctx, log, infraEnv, infraEnvInternal, iccConfig.IronicAgentImage)

	if iccConfig.IronicAgentImage == "" {
		return nil, fmt.Errorf("Failed to determine ironic config")
	}

	log.Infof("Ironic Agent Image is (%s) Ironic URL is (%s) Inspector URL is (%s)",
		iccConfig.IronicAgentImage,
		iccConfig.IronicBaseURL,
		iccConfig.IronicInspectorBaseUrl)
	return iccConfig, nil
}

// AddIronicAgentToInfraEnv updates the infra-env in the database with the ironic agent ignition config if required
// returns true when the infra-env was updated, false otherwise
func (r *PreprovisioningImageReconciler) AddIronicAgentToInfraEnv(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) (bool, error) {
	// Retrieve infraenv from the database
	key := types.NamespacedName{
		Name:      infraEnv.Name,
		Namespace: infraEnv.Namespace,
	}
	infraEnvInternal, err := r.Installer.GetInfraEnvByKubeKey(key)
	if err != nil {
		log.WithError(err).Error("failed to get corresponding infraEnv")
		return false, err
	}

	iccConfig, err := r.getIronicConfig(ctx, log, infraEnv, infraEnvInternal)
	if err != nil {
		log.WithError(err).Errorf("Failed to get Ironic configuration")
		return false, err
	}

	conf, err := ignition.GenerateIronicConfig(iccConfig.IronicBaseURL, iccConfig.IronicInspectorBaseUrl, *infraEnvInternal, iccConfig.IronicAgentImage)
	if err != nil {
		log.WithError(err).Error("failed to generate Ironic ignition config")
		return false, err
	}

	updated := false
	if string(conf) != infraEnvInternal.InternalIgnitionConfigOverride {
		var mirrorRegistryConfiguration *common.MirrorRegistryConfiguration
		if infraEnvInternal.MirrorRegistryConfiguration != "" {
			mirrorRegistryConfiguration, err = r.processMirrorRegistryConfig(ctx, log, infraEnv)
			if err != nil {
				log.WithError(err).Error("failed to process mirror registry config")
				return false, err
			}
		}

		_, err = r.Installer.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{InfraEnvID: *infraEnvInternal.ID, InfraEnvUpdateParams: &models.InfraEnvUpdateParams{}}, swag.String(string(conf)), mirrorRegistryConfiguration)
		if err != nil {
			return false, err
		}
		updated = true
		r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	}
	if _, haveAnnotation := infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]; !haveAnnotation {
		setAnnotation(&infraEnv.ObjectMeta, EnableIronicAgentAnnotation, "true")
		if err := r.Client.Update(ctx, infraEnv); err != nil {
			// Just warn here if the update fails as this annotation is just informational
			log.WithError(err).Warnf("failed to set %s annotation on infraEnv", EnableIronicAgentAnnotation)
		}
	}

	return updated, nil
}

func (r *PreprovisioningImageReconciler) getIPFamilyForInfraEnv(ctx context.Context, infraEnv *aiv1beta1.InfraEnv, internalInfraEnv *common.InfraEnv) (v4 bool, v6 bool, err error) {
	ipFamilyAnnotation := infraEnv.GetAnnotations()[infraEnvIPFamilyAnnotation]
	if ipFamilyAnnotation != "" {
		families := strings.Split(ipFamilyAnnotation, ",")
		return funk.Contains(families, ipv4Family), funk.Contains(families, ipv6Family), nil
	}
	if internalInfraEnv.ClusterID == "" {
		return false, false, fmt.Errorf("cannot find address family for non-bound infraEnv")
	}
	cluster, err := r.Installer.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: internalInfraEnv.ClusterID})
	if err != nil {
		return false, false, err
	}
	return network.GetConfiguredAddressFamilies(cluster)
}

func (r *PreprovisioningImageReconciler) fillIronicServiceURLs(ctx context.Context, infraEnv *aiv1beta1.InfraEnv, internalInfraEnv *common.InfraEnv, iccConfig *ICCConfig) error {
	ironicIPs, inspectorIPs, err := r.BMOUtils.GetIronicIPs()
	if err != nil {
		return err
	}

	// default to the first IP returned
	// v4 for dualstack hub or whatever family the single stack is
	ironicURL := getUrlFromIP(ironicIPs[0])
	inspectorURL := getUrlFromIP(inspectorIPs[0])

	if len(ironicIPs) > 1 {
		v4, v6, err := r.getIPFamilyForInfraEnv(ctx, infraEnv, internalInfraEnv)
		r.Log.Debugf("infraEnv IP families: v4: %t, v6: %t", v4, v6)
		if err != nil {
			r.Log.WithError(err).Warnf("failed to determine IP family for infraEnv %s", internalInfraEnv.ID)
		} else if !v4 && v6 {
			// spoke is single stack v6 so take v6 hub address
			ironicURL = getUrlFromIP(ironicIPs[1])
			inspectorURL = getUrlFromIP(inspectorIPs[1])
		}
	}

	iccConfig.IronicBaseURL = ironicURL
	iccConfig.IronicInspectorBaseUrl = inspectorURL
	return nil
}

func (r *PreprovisioningImageReconciler) getBMH(ctx context.Context, image *metal3_v1alpha1.PreprovisioningImage) (*metal3_v1alpha1.BareMetalHost, error) {
	bmhKey := types.NamespacedName{Namespace: image.Namespace}
	for _, owner := range image.GetOwnerReferences() {
		if owner.Kind == "BareMetalHost" {
			bmhKey.Name = owner.Name
		}
	}

	if bmhKey.Name == "" {
		return nil, fmt.Errorf("failed to find BMH owner for preprovisioningimage")
	}

	bmh := &metal3_v1alpha1.BareMetalHost{}
	if err := r.Get(ctx, bmhKey, bmh); err != nil {
		return nil, errors.Wrapf(err, "failed to get owning bmh %s", bmhKey)
	}
	return bmh, nil
}

func (r *PreprovisioningImageReconciler) setBMHRebootAnnotation(ctx context.Context, bmh *metal3_v1alpha1.BareMetalHost) error {
	patch := client.MergeFrom(bmh.DeepCopy())
	setAnnotation(&bmh.ObjectMeta, "reboot.metal3.io", "{\"force\": true}")
	r.Log.Infof("Setting reboot annotation on BMH %s", bmh.Name)
	if err := r.Patch(ctx, bmh, patch); err != nil {
		return errors.Wrapf(err, "failed to add reboot annotation to BMH %s/%s", bmh.Namespace, bmh.Name)
	}

	return nil
}

// ensurePreprovisioningImageFinalizer adds a finalizer to the PreprovisioningImage
func (r *PreprovisioningImageReconciler) ensurePreprovisioningImageFinalizer(ctx context.Context, log logrus.FieldLogger, image *metal3_v1alpha1.PreprovisioningImage) (ctrl.Result, error) {
	controllerutil.AddFinalizer(image, PreprovisioningImageFinalizerName)
	if err := r.Update(ctx, image); err != nil {
		log.WithError(err).Errorf("failed to add finalizer %s to PreprovisioningImage %s/%s", PreprovisioningImageFinalizerName, image.Namespace, image.Name)
		return ctrl.Result{Requeue: true}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// handlePreprovisioningImageDeletion handles the deletion of a PreprovisioningImage by checking if any BMHs
// with automated cleaning enabled still reference it.
func (r *PreprovisioningImageReconciler) handlePreprovisioningImageDeletion(ctx context.Context, log logrus.FieldLogger, image *metal3_v1alpha1.PreprovisioningImage) (ctrl.Result, error) {
	if !funk.ContainsString(image.GetFinalizers(), PreprovisioningImageFinalizerName) {
		// Allow deletion of the PreprovisioningImage if the finalizer is not present
		return ctrl.Result{}, nil
	}

	// Get the BMH that owns this PreprovisioningImage
	bmh, err := r.getBMH(ctx, image)
	if err != nil {
		if client.IgnoreNotFound(err) == nil || strings.Contains(err.Error(), "failed to find BMH owner") {
			// BMH not found or this preprovisioningimage is not owned by a BMH, allow deletion of the PreprovisioningImage
			log.Info("BMH not found, removing PreprovisioningImage finalizer")
			controllerutil.RemoveFinalizer(image, PreprovisioningImageFinalizerName)
			if err = r.Update(ctx, image); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from PreprovisioningImage %s/%s", PreprovisioningImageFinalizerName, image.Namespace, image.Name)
				return ctrl.Result{Requeue: true}, err
			}
			return ctrl.Result{}, nil
		}
		log.WithError(err).Error("failed to get BMH for PreprovisioningImage")
		return ctrl.Result{RequeueAfter: longerRequeueAfterOnError}, err
	}

	// PreprovisioningImage should wait for a BMH with automated cleaning enabled to be deleted
	if bmh.Spec.AutomatedCleaningMode != metal3_v1alpha1.CleaningModeDisabled {
		log.Infof("Cannot delete PreprovisioningImage yet: BMH %s/%s with automatedCleaningMode=%s exists and requires the image for deprovisioning",
			bmh.Namespace, bmh.Name, bmh.Spec.AutomatedCleaningMode)
		return ctrl.Result{Requeue: true}, nil
	}

	// Safe to delete, remove finalizer
	log.Info("Removing finalizer from PreprovisioningImage")
	controllerutil.RemoveFinalizer(image, PreprovisioningImageFinalizerName)
	if err := r.Update(ctx, image); err != nil {
		log.WithError(err).Errorf("failed to remove finalizer %s from PreprovisioningImage %s/%s", PreprovisioningImageFinalizerName, image.Namespace, image.Name)
		return ctrl.Result{Requeue: true}, err
	}
	return ctrl.Result{}, nil
}

// processMirrorRegistryConfig retrieves the mirror registry configuration from the referenced ConfigMap
func (r *PreprovisioningImageReconciler) processMirrorRegistryConfig(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) (*common.MirrorRegistryConfiguration, error) {
	mirrorRegistryConfiguration, userTomlConfigMap, err := mirrorregistry.ProcessMirrorRegistryConfig(ctx, log, r.Client, infraEnv.Spec.MirrorRegistryRef)
	if err != nil {
		return nil, err
	}
	if mirrorRegistryConfiguration != nil {
		namespacedName := types.NamespacedName{Name: infraEnv.Spec.MirrorRegistryRef.Name, Namespace: infraEnv.Spec.MirrorRegistryRef.Namespace}
		if err = ensureConfigMapIsLabelled(ctx, r.Client, userTomlConfigMap, namespacedName); err != nil {
			return nil, errors.Wrapf(err, "Unable to mark infraenv mirror configmap for backup")
		}
	}

	return mirrorRegistryConfiguration, nil
}

// patchImageStatus updates the PreprovisioningImage status using the given condition setter function
func (r *PreprovisioningImageReconciler) patchImageStatus(
	ctx context.Context,
	log logrus.FieldLogger,
	image *metal3_v1alpha1.PreprovisioningImage,
	conditionSetter func(*metal3_v1alpha1.PreprovisioningImage),
) error {
	patch := client.MergeFrom(image.DeepCopy())
	conditionSetter(image)
	return r.Status().Patch(ctx, image, patch)
}
