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
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type InstallEnvConfig struct {
	ImageType models.ImageType `envconfig:"ISO_IMAGE_TYPE" default:"minimal-iso"`
}

// InstallEnvReconciler reconciles a InstallEnv object
type InstallEnvReconciler struct {
	client.Client
	Config           InstallEnvConfig
	Log              logrus.FieldLogger
	Installer        bminventory.InstallerInternals
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=nmstateconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=installenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=installenvs/status,verbs=get;update;patch

func (r *InstallEnvReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	r.Log.Debugf("InstallEnv Reconcile start for InstallEnv: %s, Namespace %s",
		req.NamespacedName.Name, req.NamespacedName.Name)
	ctx := context.Background()

	installEnv := &aiv1beta1.InstallEnv{}
	if err := r.Get(ctx, req.NamespacedName, installEnv); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.ensureISO(ctx, installEnv)
}

func (r *InstallEnvReconciler) updateClusterIfNeeded(ctx context.Context, installEnv *aiv1beta1.InstallEnv, cluster *common.Cluster) error {
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

func (r *InstallEnvReconciler) updateClusterDiscoveryIgnitionIfNeeded(ctx context.Context, installEnv *aiv1beta1.InstallEnv, cluster *common.Cluster) error {
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

func (r *InstallEnvReconciler) buildMacInterfaceMap(nmStateConfig aiv1beta1.NMStateConfig) models.MacInterfaceMap {
	macInterfaceMap := make(models.MacInterfaceMap, 0, len(nmStateConfig.Spec.Interfaces))
	for _, cfg := range nmStateConfig.Spec.Interfaces {
		r.Log.Debugf("adding MAC interface map to host static network config - Name: %s, MacAddress: %s ,",
			cfg.Name, cfg.MacAddress)
		macInterfaceMap = append(macInterfaceMap, &models.MacInterfaceMapItems0{
			MacAddress:     cfg.MacAddress,
			LogicalNicName: cfg.Name,
		})
	}
	return macInterfaceMap
}

func (r *InstallEnvReconciler) processNMStateConfig(ctx context.Context, installEnv *aiv1beta1.InstallEnv) ([]*models.HostStaticNetworkConfig, error) {
	var staticNetworkConfig []*models.HostStaticNetworkConfig

	if installEnv.Spec.NMStateConfigLabelSelector.MatchLabels == nil {
		return staticNetworkConfig, nil
	}
	for labelName, labelValue := range installEnv.Spec.NMStateConfigLabelSelector.MatchLabels {
		nmStateConfigs := &aiv1beta1.NMStateConfigList{}
		if err := r.List(ctx, nmStateConfigs, client.InNamespace(installEnv.Namespace),
			client.MatchingLabels(map[string]string{labelName: labelValue})); err != nil {
			return staticNetworkConfig, err
		}
		for _, nmStateConfig := range nmStateConfigs.Items {
			r.Log.Debugf("found nmStateConfig: %s for installEnv: %s", nmStateConfig.Name, installEnv.Name)
			staticNetworkConfig = append(staticNetworkConfig, &models.HostStaticNetworkConfig{
				MacInterfaceMap: r.buildMacInterfaceMap(nmStateConfig),
				NetworkYaml:     string(nmStateConfig.Spec.NetConfig.Raw),
			})
		}
	}
	return staticNetworkConfig, nil
}

// ensureISO generates ISO for the cluster if needed and will update the condition Reason and Message accordingly.
// It returns a result that includes ISODownloadURL.
func (r *InstallEnvReconciler) ensureISO(ctx context.Context, installEnv *aiv1beta1.InstallEnv) (ctrl.Result, error) {
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
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + ": " + clusterDeploymentRefErr.Error(),
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
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + ": " + inventoryErr.Error(),
		})
		if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update installEnv status")
		}
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Check for updates from user, compare spec and update the cluster
	if err = r.updateClusterIfNeeded(ctx, installEnv, cluster); err != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, err)
	}

	// Check for discovery ignition specific updates from user, compare spec and update the ignition config
	if err = r.updateClusterDiscoveryIgnitionIfNeeded(ctx, installEnv, cluster); err != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, err)
	}

	isoParams := installer.GenerateClusterISOParams{
		ClusterID: *cluster.ID,
		ImageCreateParams: &models.ImageCreateParams{
			ImageType:    r.Config.ImageType,
			SSHPublicKey: installEnv.Spec.SSHAuthorizedKey,
		},
	}

	staticNetworkConfig, err := r.processNMStateConfig(ctx, installEnv)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, installEnv, err)
	}
	if len(staticNetworkConfig) > 0 {
		isoParams.ImageCreateParams.StaticNetworkConfig = staticNetworkConfig
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
	ctx context.Context, installEnv *aiv1beta1.InstallEnv, imageInfo *models.ImageInfo) (ctrl.Result, error) {
	conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ImageCreatedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ImageCreatedReason,
		Message: aiv1beta1.ImageStateCreated,
	})
	installEnv.Status.ISODownloadURL = imageInfo.DownloadURL
	if updateErr := r.Status().Update(ctx, installEnv); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update installEnv status")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *InstallEnvReconciler) handleEnsureISOErrors(
	ctx context.Context, installEnv *aiv1beta1.InstallEnv, err error) (ctrl.Result, error) {
	var (
		currentReason = ""
		errMsg        string
		Requeue       bool
	)
	// TODO: Checking currentCondition as a workaround until MGMT-4695 and MGMT-4696 get resolved.
	// If the current condition is in an error state, avoid clearing it up.
	if currentCondition := conditionsv1.FindStatusCondition(installEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition); currentCondition != nil {
		currentReason = currentCondition.Reason
	}
	if imageBeingCreated(err) && currentReason != aiv1beta1.ImageCreationErrorReason { // Not an actual error, just an image generation in progress.
		err = nil
		Requeue = false
		r.Log.Infof("Image %s being prepared for cluster %s", installEnv.Name, installEnv.ClusterName)
		conditionsv1.SetStatusCondition(&installEnv.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionTrue,
			Reason:  aiv1beta1.ImageCreatedReason,
			Message: aiv1beta1.ImageStateCreated,
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
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + errMsg,
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
	mapNMStateConfigToInstallEnv := handler.ToRequestsFunc(
		func(a handler.MapObject) []reconcile.Request {
			installEnvs := &aiv1beta1.InstallEnvList{}
			if len(a.Meta.GetLabels()) == 0 {
				r.Log.Debugf("NMState config: %s has no labels", a.Meta.GetName())
				return []reconcile.Request{}
			}
			if err := r.List(context.Background(), installEnvs, client.InNamespace(a.Meta.GetNamespace())); err != nil {
				r.Log.Debugf("failed to list InstallEnvs")
				return []reconcile.Request{}
			}

			reply := make([]reconcile.Request, 0, len(installEnvs.Items))
			for labelName, labelValue := range a.Meta.GetLabels() {
				r.Log.Debugf("Detected NMState config with label name: %s with value %s, about to search for a matching InstallEnv",
					labelName, labelValue)
				for _, installEnv := range installEnvs.Items {
					if installEnv.Spec.NMStateConfigLabelSelector.MatchLabels[labelName] == labelValue {
						r.Log.Debugf("Detected NMState config for InstallEnv: %s in namespace: %s", installEnv.Name, installEnv.Namespace)
						reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
							Namespace: installEnv.Namespace,
							Name:      installEnv.Name,
						}})
					}
				}
			}
			return reply
		})

	installEnvUpdates := r.CRDEventsHandler.GetInstallEnvUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.InstallEnv{}).
		Watches(&source.Kind{Type: &aiv1beta1.NMStateConfig{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapNMStateConfigToInstallEnv}).
		Watches(&source.Channel{Source: installEnvUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
