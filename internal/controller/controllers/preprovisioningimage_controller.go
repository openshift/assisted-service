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

const archMismatchReason = "InfraEnvArchMismatch"

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

	if infraEnv.Spec.CpuArchitecture != image.Spec.Architecture {
		log.Infof("Image arch %s does not match infraEnv arch %s", image.Spec.Architecture, infraEnv.Spec.CpuArchitecture)
		setMismatchedArchCondition(image, infraEnv.Spec.CpuArchitecture)
		err = r.Status().Update(ctx, image)
		if err != nil {
			log.WithError(err).Error("failed to update status")
		}
		return ctrl.Result{}, err
	}

	infraEnvUpdated, err := r.AddIronicAgentToInfraEnv(ctx, log, infraEnv)
	if infraEnvUpdated {
		return ctrl.Result{}, nil
	}
	if err != nil {
		setIronicAgentIgnitionFailureCondition(image, err)
		if updErr := r.Status().Update(ctx, image); updErr != nil {
			log.WithError(err).Error("failed to update status")
		}
		return ctrl.Result{}, err
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

func initrdExtraKernelParams(infraEnv aiv1beta1.InfraEnv) string {
	params := []string{fmt.Sprintf("coreos.live.rootfs_url=%s rd.bootif=0", infraEnv.Status.BootArtifacts.RootfsURL)}
	for _, arg := range infraEnv.Spec.KernelArguments {
		if arg.Operation == models.KernelArgumentOperationAppend {
			params = append(params, arg.Value)
		}
	}
	return strings.Join(params, " ")
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

func setMismatchedArchCondition(image *metal3_v1alpha1.PreprovisioningImage, infraArch string) {
	message := fmt.Sprintf("PreprovisioningImage CPU architecture (%s) does not match InfraEnv CPU architecture (%s)", image.Spec.Architecture, infraArch)
	reason := imageConditionReason(archMismatchReason)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageReady, metav1.ConditionFalse,
		reason, message)
	setImageCondition(image.GetGeneration(), &image.Status,
		metal3_v1alpha1.ConditionImageError, metav1.ConditionTrue,
		reason, message)
}

func setIronicAgentIgnitionFailureCondition(image *metal3_v1alpha1.PreprovisioningImage, err error) {
	message := fmt.Sprintf("Could not add ironic agent to image: %s", err.Error())
	reason := imageConditionReason("IronicAgentIgnitionUpdateFailure")
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
			reconcileRequests = append(reconcileRequests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: images[i].Namespace,
					Name:      images[i].Name,
				},
			})
		}
		return reconcileRequests
	}
	return mapInfraEnvPPI
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
	ironicAgentImage, err := r.getIronicAgentImageByRelease(ctx, log, infraEnvInternal)
	if err != nil {
		log.WithError(err).Warningf("Failed to get ironic agent image by release for infraEnv: %s", infraEnv.Name)
	}

	// if ironicAgentImage can't be found by version use the default
	if ironicAgentImage == "" {
		if infraEnvInternal.CPUArchitecture == common.ARM64CPUArchitecture {
			ironicAgentImage = r.Config.BaremetalIronicAgentImageForArm
		} else {
			ironicAgentImage = r.Config.BaremetalIronicAgentImage
		}
		log.Infof("Setting default ironic agent image (%s) for infraEnv %s", ironicAgentImage, infraEnv.Name)
	}

	ironicServiceURL, inspectorURL, err := r.BMOUtils.GetIronicServiceURLS()
	if err != nil {
		log.WithError(err).Error("failed to get IronicServiceURLs")
		return false, err
	}

	conf, err := ignition.GenerateIronicConfig(ironicServiceURL, inspectorURL, *infraEnvInternal, ironicAgentImage)
	if err != nil {
		log.WithError(err).Error("failed to generate Ironic ignition config")
		return false, err
	}

	updated := false
	if string(conf) != infraEnvInternal.InternalIgnitionConfigOverride {
		_, err = r.Installer.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{InfraEnvID: *infraEnvInternal.ID, InfraEnvUpdateParams: &models.InfraEnvUpdateParams{}}, swag.String(string(conf)))
		if err != nil {
			return false, err
		}
		updated = true
		r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	}
	if _, haveAnnotation := infraEnv.ObjectMeta.Annotations[EnableIronicAgentAnnotation]; !haveAnnotation {
		if infraEnv.ObjectMeta.Annotations == nil {
			infraEnv.ObjectMeta.Annotations = make(map[string]string)
		}

		infraEnv.Annotations[EnableIronicAgentAnnotation] = "true"
		if err := r.Client.Update(ctx, infraEnv); err != nil {
			// Just warn here if the update fails as this annotation is just informational
			log.WithError(err).Warnf("failed to set %s annotation on infraEnv %s", EnableIronicAgentAnnotation, infraEnv.Name)
		}
	}

	return updated, nil
}

func (r *PreprovisioningImageReconciler) getIronicAgentImageByRelease(ctx context.Context, log logrus.FieldLogger, infraEnv *common.InfraEnv) (string, error) {
	image := r.hubIronicAgentImage
	architectures := r.hubReleaseArchitectures
	if image == "" {
		cv := &configv1.ClusterVersion{}
		if err := r.Get(ctx, types.NamespacedName{Name: "version"}, cv); err != nil {
			return "", err
		}
		var err error
		architectures, err = r.OcRelease.GetReleaseArchitecture(log, cv.Status.Desired.Image, r.ReleaseImageMirror, infraEnv.PullSecret)
		if err != nil {
			return "", err
		}
		image, err = r.OcRelease.GetIronicAgentImage(log, cv.Status.Desired.Image, r.ReleaseImageMirror, infraEnv.PullSecret)
		if err != nil {
			return "", err
		}
		r.hubIronicAgentImage = image
		r.hubReleaseArchitectures = architectures
		log.Infof("Caching hub ironic agent image %s for architectures %v", image, architectures)
	}

	if !funk.Contains(architectures, infraEnv.CPUArchitecture) {
		return "", fmt.Errorf("release image architectures %v do not match infraEnv architecture %s", architectures, infraEnv.CPUArchitecture)
	}

	return image, nil
}
