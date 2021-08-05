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
	"time"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const defaultRequeueAfterPerRecoverableError = 2 * bminventory.WindowBetweenRequestsInSeconds

type InfraEnvConfig struct {
	ImageType models.ImageType `envconfig:"ISO_IMAGE_TYPE" default:"minimal-iso"`
}

// InfraEnvReconciler reconciles a InfraEnv object
type InfraEnvReconciler struct {
	client.Client
	Config           InfraEnvConfig
	Log              logrus.FieldLogger
	Installer        bminventory.InstallerInternals
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=nmstateconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get;update;patch

func (r *InfraEnvReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"infra_env":           req.Name,
			"infra_env_namespace": req.Namespace,
		})

	defer func() {
		log.Info("InfraEnv Reconcile ended")
	}()

	log.Info("InfraEnv Reconcile started")

	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Get(ctx, req.NamespacedName, infraEnv); err != nil {
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.ensureISO(ctx, log, infraEnv)
}

func (r *InfraEnvReconciler) updateInfraEnvIfNeeded(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, infraEnvInternal *common.InfraEnv) (*common.InfraEnv, error) {
	var (
		params = &models.InfraEnvUpdateParams{}
		update bool
	)

	if infraEnv.Spec.Proxy != nil {
		if infraEnv.Spec.Proxy.NoProxy != "" && infraEnv.Spec.Proxy.NoProxy != *infraEnvInternal.Proxy.NoProxy {
			params.Proxy.NoProxy = swag.String(infraEnv.Spec.Proxy.NoProxy)
			log.Debugf("NoProxy changed from %s to %s", *infraEnvInternal.Proxy.NoProxy, infraEnv.Spec.Proxy.NoProxy)
			update = true
		}
		if infraEnv.Spec.Proxy.HTTPProxy != "" && infraEnv.Spec.Proxy.HTTPProxy != *infraEnvInternal.Proxy.HTTPProxy {
			params.Proxy.HTTPProxy = swag.String(infraEnv.Spec.Proxy.HTTPProxy)
			log.Debugf("HTTPProxy changed from %s to %s", *infraEnvInternal.Proxy.HTTPProxy, infraEnv.Spec.Proxy.HTTPProxy)
			update = true
		}
		if infraEnv.Spec.Proxy.HTTPSProxy != "" && infraEnv.Spec.Proxy.HTTPSProxy != *infraEnvInternal.Proxy.HTTPSProxy {
			params.Proxy.HTTPSProxy = swag.String(infraEnv.Spec.Proxy.HTTPSProxy)
			log.Debugf("HTTPProxy changed from %s to %s", *infraEnvInternal.Proxy.HTTPSProxy, infraEnv.Spec.Proxy.HTTPSProxy)
			update = true
		}
	}
	if len(infraEnv.Spec.AdditionalNTPSources) > 0 && infraEnvInternal.AdditionalNtpSources != infraEnv.Spec.AdditionalNTPSources[0] {
		params.AdditionalNtpSources = swag.String(strings.Join(infraEnv.Spec.AdditionalNTPSources[:], ","))
		log.Debugf("AdditionalNTPSources changed from %s to %s", infraEnvInternal.AdditionalNtpSources, infraEnv.Spec.AdditionalNTPSources[0])
		update = true
	}

	if update {
		updateString, err := json.Marshal(params)
		if err != nil {
			return infraEnvInternal, err
		}
		log.Infof("updating infraenv %s %s with %s", infraEnv.UID, string(updateString))

		updateInfraEnvInternal, err := r.Installer.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
			InfraEnvID:           infraEnvInternal.ID,
			InfraEnvUpdateParams: params,
		})
		return updateInfraEnvInternal, err
	}

	return infraEnvInternal, nil
}

func (r *InfraEnvReconciler) updateInfraEnvDiscoveryIgnitionIfNeeded(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, infraEnvInternal *common.InfraEnv) error {
	var (
		discoveryIgnitionParams = &models.DiscoveryIgnitionParams{}
		updateInfraEnvIgnition  bool
	)
	if infraEnv.Spec.IgnitionConfigOverride != "" && infraEnvInternal.IgnitionConfigOverride != infraEnv.Spec.IgnitionConfigOverride {
		discoveryIgnitionParams.Config = *swag.String(infraEnv.Spec.IgnitionConfigOverride)
		log.Debugf("IgnitionConfigOverride changed from %s to %s", infraEnvInternal.IgnitionConfigOverride, infraEnv.Spec.IgnitionConfigOverride)
		updateInfraEnvIgnition = true
	}
	if updateInfraEnvIgnition {
		updateString, err := json.Marshal(discoveryIgnitionParams)
		if err != nil {
			return err
		}
		log.Infof("updating infraenv %s %s with %s",
			infraEnv.Spec.ClusterRef.Name, infraEnv.Spec.ClusterRef.Namespace, string(updateString))
		err = r.Installer.UpdateDiscoveryIgnitionInternal(ctx, installer.UpdateDiscoveryIgnitionParams{
			DiscoveryIgnitionParams: discoveryIgnitionParams,
			ClusterID:               infraEnvInternal.ID,
		})
		return err
	}
	return nil
}

func (r *InfraEnvReconciler) buildMacInterfaceMap(log logrus.FieldLogger, nmStateConfig aiv1beta1.NMStateConfig) models.MacInterfaceMap {
	macInterfaceMap := make(models.MacInterfaceMap, 0, len(nmStateConfig.Spec.Interfaces))
	for _, cfg := range nmStateConfig.Spec.Interfaces {
		log.Debugf("adding MAC interface map to host static network config - Name: %s, MacAddress: %s ,",
			cfg.Name, cfg.MacAddress)
		macInterfaceMap = append(macInterfaceMap, &models.MacInterfaceMapItems0{
			MacAddress:     cfg.MacAddress,
			LogicalNicName: cfg.Name,
		})
	}
	return macInterfaceMap
}

func (r *InfraEnvReconciler) processNMStateConfig(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) ([]*models.HostStaticNetworkConfig, error) {
	var staticNetworkConfig []*models.HostStaticNetworkConfig

	if infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels == nil {
		return staticNetworkConfig, nil
	}
	for labelName, labelValue := range infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels {
		nmStateConfigs := &aiv1beta1.NMStateConfigList{}
		if err := r.List(ctx, nmStateConfigs, client.InNamespace(infraEnv.Namespace),
			client.MatchingLabels(map[string]string{labelName: labelValue})); err != nil {
			return staticNetworkConfig, err
		}
		for _, nmStateConfig := range nmStateConfigs.Items {
			log.Debugf("found nmStateConfig: %s for infraEnv: %s", nmStateConfig.Name, infraEnv.Name)
			staticNetworkConfig = append(staticNetworkConfig, &models.HostStaticNetworkConfig{
				MacInterfaceMap: r.buildMacInterfaceMap(log, nmStateConfig),
				NetworkYaml:     string(nmStateConfig.Spec.NetConfig.Raw),
			})
		}
	}
	return staticNetworkConfig, nil
}

// ensureISO generates ISO for the cluster if needed and will update the condition Reason and Message accordingly.
// It returns a result that includes ISODownloadURL.
func (r *InfraEnvReconciler) ensureISO(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) (ctrl.Result, error) {
	infraEnv.Status.AgentLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{aiv1beta1.InfraEnvNameLabel: infraEnv.Name}}
	var inventoryErr error
	var Requeue bool

	kubeKey := types.NamespacedName{
		Name:      infraEnv.Spec.ClusterRef.Name,
		Namespace: infraEnv.Spec.ClusterRef.Namespace,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}

	// Retrieve clusterDeployment
	if err := r.Get(ctx, kubeKey, clusterDeployment); err != nil {
		errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
			infraEnv.Spec.ClusterRef.Name, infraEnv.Spec.ClusterRef.Namespace)
		Requeue = false
		clientError := true
		if !k8serrors.IsNotFound(err) {
			Requeue = true
			clientError = false
		}
		clusterDeploymentRefErr := newKubeAPIError(errors.Wrapf(err, errMsg), clientError)

		// Update that we failed to retrieve the clusterDeployment
		conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + ": " + clusterDeploymentRefErr.Error(),
		})
		if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
			log.WithError(updateErr).Error("failed to update infraEnv status")
		}
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Retrieve infraEnv from the database
	infraEnvInternal, err := r.Installer.GetInfraEnvByKubeKey(types.NamespacedName{
		Name:      clusterDeployment.Name,
		Namespace: clusterDeployment.Namespace,
	})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Requeue = true
			msg := fmt.Sprintf("cluster does not exist: %s, ", clusterDeployment.Name)
			if clusterDeployment.Spec.ClusterInstallRef == nil {
				msg += "AgentClusterInstall is not defined in ClusterDeployment"
			} else {
				msg += fmt.Sprintf("check AgentClusterInstall conditions: name %s in namespace %s",
					clusterDeployment.Spec.ClusterInstallRef.Name, clusterDeployment.Namespace)
			}
			log.Errorf(msg)
			err = errors.Errorf(msg)

			inventoryErr = common.NewApiError(http.StatusNotFound, err)
		} else {
			Requeue = false
			inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
		}
		// Update that we failed to retrieve the cluster from the database
		conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionUnknown,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + ": " + inventoryErr.Error(),
		})
		if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
			log.WithError(updateErr).Error("failed to update infraEnv status")
		}
		return ctrl.Result{Requeue: Requeue}, nil
	}

	// Check for updates from user, compare spec and update the infraenv
	infraEnvInternal, err = r.updateInfraEnvIfNeeded(ctx, log, infraEnv, infraEnvInternal)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err)
	}

	// Check for discovery ignition specific updates from user, compare spec and update the ignition config
	if err = r.updateInfraEnvDiscoveryIgnitionIfNeeded(ctx, log, infraEnv, infraEnvInternal); err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err)
	}

	// Add openshift version to ensure it isn't missing in versions cache
	_, err = r.Installer.AddOpenshiftVersion(ctx, infraEnvInternal.OpenshiftVersion, infraEnvInternal.PullSecret)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err)
	}

	// Image successfully generated. Reflect that in infraEnv obj and conditions
	return r.updateEnsureISOSuccess(ctx, log, infraEnv, infraEnvInternal)
}

func (r *InfraEnvReconciler) updateEnsureISOSuccess(
	ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, infraEnvInternal *common.InfraEnv) (ctrl.Result, error) {
	conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ImageCreatedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ImageCreatedReason,
		Message: aiv1beta1.ImageStateCreated,
	})

	if infraEnv.Status.ISODownloadURL != infraEnvInternal.DownloadURL {
		log.Infof("ISODownloadURL changed from %s to %s", infraEnv.Status.ISODownloadURL, infraEnvInternal.DownloadURL)
		infraEnv.Status.ISODownloadURL = infraEnvInternal.DownloadURL
		imageCreatedAt := metav1.NewTime(time.Time(infraEnvInternal.CreatedAt))
		infraEnv.Status.CreatedTime = &imageCreatedAt
	}

	if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
		log.WithError(updateErr).Error("failed to update infraEnv status")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *InfraEnvReconciler) handleEnsureISOErrors(
	ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, err error) (ctrl.Result, error) {
	var (
		currentReason               = ""
		RequeueAfter  time.Duration = 0
		errMsg        string
		Requeue       bool
	)

	// TODO: Checking currentCondition as a workaround until MGMT-4695 get resolved.
	// If the current condition is in an error state, avoid clearing it up.
	if currentCondition := conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition); currentCondition != nil {
		currentReason = currentCondition.Reason
	}
	if imageBeingCreated(err) {
		Requeue = true
		RequeueAfter = defaultRequeueAfterPerRecoverableError
		err = nil                                                // clear up the error so it will requeue with RequeueAfter we set
		if currentReason != aiv1beta1.ImageCreationErrorReason { // Not an actual error, just an image generation in progress.
			log.Infof("Image %s being prepared for cluster %s", infraEnv.Name, infraEnv.Spec.ClusterRef.Name)
			conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  aiv1beta1.ImageCreatedReason,
				Message: aiv1beta1.ImageStateCreated,
			})
		}
	} else { // Actual errors
		log.WithError(err).Error("infraEnv reconcile failed")
		if isClientError(err) { // errors it can't recover from
			Requeue = false
			errMsg = ": " + err.Error()
			err = nil // clear the error, to avoid requeue.
		} else { // errors it may recover from
			Requeue = true
			RequeueAfter = defaultRequeueAfterPerRecoverableError
			errMsg = ": internal error"
		}
		conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + errMsg,
		})
		// In a case of an error, clear the download URL.
		log.Debugf("cleanup up ISODownloadURL due to %s", errMsg)
		infraEnv.Status.ISODownloadURL = ""
		infraEnv.Status.CreatedTime = nil
	}
	if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
		log.WithError(updateErr).Error("failed to update infraEnv status")
	}
	return ctrl.Result{Requeue: Requeue, RequeueAfter: RequeueAfter}, err
}

func imageBeingCreated(err error) bool {
	return IsHTTPError(err, http.StatusConflict)
}

func (r *InfraEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapNMStateConfigToInfraEnv := func(a client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"nmstate_config":           a.GetName(),
				"nmstate_config_namespace": a.GetNamespace(),
			})
		infraEnvs := &aiv1beta1.InfraEnvList{}
		if len(a.GetLabels()) == 0 {
			log.Debugf("NMState config: %s has no labels", a.GetName())
			return []reconcile.Request{}
		}
		if err := r.List(context.Background(), infraEnvs, client.InNamespace(a.GetNamespace())); err != nil {
			log.Debugf("failed to list InfraEnvs")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(infraEnvs.Items))
		for labelName, labelValue := range a.GetLabels() {
			log.Debugf("Detected NMState config with label name: %s with value %s, about to search for a matching InfraEnv",
				labelName, labelValue)
			for _, infraEnv := range infraEnvs.Items {
				if infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels[labelName] == labelValue {
					log.Debugf("Detected NMState config for InfraEnv: %s in namespace: %s", infraEnv.Name, infraEnv.Namespace)
					reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: infraEnv.Namespace,
						Name:      infraEnv.Name,
					}})
				}
			}
		}
		return reply
	}

	mapClusterDeploymentToInfraEnv := func(clusterDeployment client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"cluster_deployment":           clusterDeployment.GetName(),
				"cluster_deployment_namespace": clusterDeployment.GetNamespace(),
			})
		infraEnvs := &aiv1beta1.InfraEnvList{}
		if err := r.List(context.Background(), infraEnvs); err != nil {
			log.Debugf("failed to list InfraEnvs")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(infraEnvs.Items))
		for _, infraEnv := range infraEnvs.Items {
			if infraEnv.Spec.ClusterRef.Name == clusterDeployment.GetName() &&
				infraEnv.Spec.ClusterRef.Namespace == clusterDeployment.GetNamespace() {
				reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: infraEnv.Namespace,
					Name:      infraEnv.Name,
				}})
			}
		}
		return reply
	}

	infraEnvUpdates := r.CRDEventsHandler.GetInfraEnvUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.InfraEnv{}).
		Watches(&source.Kind{Type: &aiv1beta1.NMStateConfig{}}, handler.EnqueueRequestsFromMapFunc(mapNMStateConfigToInfraEnv)).
		Watches(&source.Kind{Type: &hivev1.ClusterDeployment{}}, handler.EnqueueRequestsFromMapFunc(mapClusterDeploymentToInfraEnv)).
		Watches(&source.Channel{Source: infraEnvUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
