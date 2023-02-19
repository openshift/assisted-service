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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/openshift/assisted-service/api/common"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	restclient "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	adminPasswordSecretStringTemplate = "%s-admin-password"
	adminKubeConfigStringTemplate     = "%s-admin-kubeconfig"
	InstallConfigOverrides            = aiv1beta1.Group + "/install-config-overrides"
	ClusterDeploymentFinalizerName    = "clusterdeployments." + aiv1beta1.Group + "/ai-deprovision"
	AgentClusterInstallFinalizerName  = "agentclusterinstall." + aiv1beta1.Group + "/ai-deprovision"
)

const HighAvailabilityModeNone = "None"
const defaultRequeueAfterOnError = 10 * time.Second
const longerRequeueAfterOnError = 1 * time.Minute

// ClusterDeploymentsReconciler reconciles a Cluster object
type ClusterDeploymentsReconciler struct {
	client.Client
	APIReader        client.Reader
	Log              logrus.FieldLogger
	Scheme           *runtime.Scheme
	Installer        bminventory.InstallerInternals
	ClusterApi       cluster.API
	HostApi          host.API
	CRDEventsHandler CRDEventsHandler
	Manifests        manifestsapi.ClusterManifestsInternals
	ServiceBaseURL   string
	PullSecretHandler
	AuthType              auth.AuthType
	VersionsHandler       versions.Handler
	SpokeK8sClientFactory spoke_k8s_client.SpokeK8sClientFactory
}

const minimalOpenShiftVersionForDefaultNetworkTypeOVNKubernetes = "4.12.0-0.0"

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/finalizers,verbs=update

func (r *ClusterDeploymentsReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	logFields := logrus.Fields{
		"cluster_deployment":           req.Name,
		"cluster_deployment_namespace": req.Namespace,
	}
	log := logutil.FromContext(ctx, r.Log).WithFields(logFields)

	defer func() {
		log.Info("ClusterDeployment Reconcile ended")
	}()

	log.Info("ClusterDeployment Reconcile started")

	clusterDeployment := &hivev1.ClusterDeployment{}
	clusterInstallDeleted := false

	if err := r.Get(ctx, req.NamespacedName, clusterDeployment); err != nil {
		log.WithError(err).Errorf("failed to get ClusterDeployment name=%s namespace=%s", req.Name, req.Namespace)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// ignore unsupported platforms
	if !isSupportedPlatform(clusterDeployment) {
		return ctrl.Result{}, nil
	}

	clusterInstall := &hiveext.AgentClusterInstall{}
	if clusterDeployment.Spec.ClusterInstallRef == nil {
		log.Infof("AgentClusterInstall not set for ClusterDeployment %s", clusterDeployment.Name)
		return ctrl.Result{}, nil
	}

	aciName := clusterDeployment.Spec.ClusterInstallRef.Name
	err := r.Get(ctx,
		types.NamespacedName{
			Namespace: clusterDeployment.Namespace,
			Name:      aciName,
		},
		clusterInstall)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// mark that clusterInstall was already deleted so we skip it if needed.
			clusterInstallDeleted = true
			log.WithField("AgentClusterInstall", aciName).Infof("AgentClusterInstall does not exist for ClusterDeployment %s", clusterDeployment.Name)
			if clusterDeployment.ObjectMeta.DeletionTimestamp.IsZero() {
				// we have no agentClusterInstall and clusterDeployment is not being deleted. stop reconciliation.
				return ctrl.Result{}, nil
			}
		} else {
			log.WithError(err).Errorf("failed to get AgentClusterInstall name=%s namespace=%s", aciName, clusterDeployment.Namespace)
			return ctrl.Result{Requeue: true}, err
		}
	}

	if !clusterInstallDeleted {
		logFields["agent_cluster_install"] = clusterInstall.Name
		logFields["agent_cluster_install_namespace"] = clusterInstall.Namespace
		log = logutil.FromContext(ctx, log).WithFields(logFields)
		aciReply, aciErr := r.agentClusterInstallFinalizer(ctx, log, req, clusterInstall)
		if aciReply != nil {
			return *aciReply, aciErr
		}
	}

	cdReply, cdErr := r.clusterDeploymentFinalizer(ctx, log, clusterDeployment)
	if cdReply != nil {
		return *cdReply, cdErr
	}

	err = r.ensureOwnerRef(ctx, log, clusterDeployment, clusterInstall)
	if err != nil {
		log.WithError(err).Error("error setting owner reference")
		return ctrl.Result{Requeue: true}, err
	}

	cluster, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !isInstalled(clusterDeployment, clusterInstall) {
			return r.createNewCluster(ctx, log, req.NamespacedName, clusterDeployment, clusterInstall)
		}

		return r.createNewDay2Cluster(ctx, log, req.NamespacedName, clusterDeployment, clusterInstall)
	}
	if err != nil {
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	err = r.validateClusterDeployment(ctx, log, clusterDeployment, clusterInstall)
	if err != nil {
		log.Error(err)
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	// check for updates from user, compare spec and update if needed
	cluster, err = r.updateIfNeeded(ctx, log, clusterDeployment, clusterInstall, cluster)
	if err != nil {
		log.WithError(err).Error("failed to update cluster")
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	// check for install config overrides and update if needed
	err = r.updateInstallConfigOverrides(ctx, log, clusterInstall, cluster)
	if err != nil {
		log.WithError(err).Error("failed to update install config overrides")
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	// In case the Cluster is a Day 1 cluster and is installed, update the Metadata and create secrets for credentials
	if *cluster.Status == models.ClusterStatusInstalled && swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		return r.handleClusterInstalled(ctx, log, clusterDeployment, cluster, clusterInstall, req.NamespacedName)
	}

	// Create Kubeconfig no-ingress if needed
	if *cluster.Status == models.ClusterStatusInstalling || *cluster.Status == models.ClusterStatusFinalizing {
		if err1 := r.createNoIngressKubeConfig(ctx, log, clusterDeployment, cluster, clusterInstall); err1 != nil {
			log.WithError(err1).Error("failed to create kubeconfig no-ingress secret")
			return r.updateStatus(ctx, log, clusterInstall, cluster, err1)
		}
	}

	if swag.StringValue(cluster.Kind) == models.ClusterKindCluster && !clusterInstall.Spec.HoldInstallation {
		// Day 1
		pullSecret, err := r.PullSecretHandler.GetValidPullSecret(ctx, getPullSecretKey(req.Namespace, clusterDeployment.Spec.PullSecretRef))
		if err != nil {
			log.WithError(err).Error("failed to get pull secret")
			return r.updateStatus(ctx, log, clusterInstall, nil, err)
		}
		return r.installDay1(ctx, log, clusterDeployment, clusterInstall, cluster, pullSecret)
	} else if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		// Day 2
		return r.installDay2Hosts(ctx, log, clusterDeployment, clusterInstall, cluster)
	}

	return r.updateStatus(ctx, log, clusterInstall, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) validateClusterDeployment(ctx context.Context, log logrus.FieldLogger,
	clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall) error {

	// Make sure that the ImageSetRef is set for clusters not already installed
	if clusterInstall.Spec.ImageSetRef == nil && !clusterDeployment.Spec.Installed {
		return newInputError("missing ImageSetRef for cluster that is not installed")
	}

	return nil
}

func (r *ClusterDeploymentsReconciler) agentClusterInstallFinalizer(ctx context.Context, log logrus.FieldLogger, req ctrl.Request,
	clusterInstall *hiveext.AgentClusterInstall) (*ctrl.Result, error) {
	if clusterInstall.ObjectMeta.DeletionTimestamp.IsZero() { // clusterInstall not being deleted
		// Register a finalizer if it is absent.
		if !funk.ContainsString(clusterInstall.GetFinalizers(), AgentClusterInstallFinalizerName) {
			controllerutil.AddFinalizer(clusterInstall, AgentClusterInstallFinalizerName)
			if err := r.Update(ctx, clusterInstall); err != nil {
				log.WithError(err).Errorf("failed to add finalizer %s to resource %s %s",
					AgentClusterInstallFinalizerName, clusterInstall.Name, clusterInstall.Namespace)
				return &ctrl.Result{Requeue: true}, err
			}
		}
	} else { // clusterInstall is being deleted
		if funk.ContainsString(clusterInstall.GetFinalizers(), AgentClusterInstallFinalizerName) {
			cluster, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return &ctrl.Result{Requeue: true}, err
			}
			if err == nil {
				if swag.StringValue(cluster.Status) == models.ClusterStatusInstalling || swag.StringValue(cluster.Status) == models.ClusterStatusPreparingForInstallation {
					log.Infof("ClusterInstall is being deleted, cancel installation for cluster %s", *cluster.ID)
					if _, err = r.Installer.CancelInstallationInternal(ctx, installer.V2CancelInstallationParams{
						ClusterID: *cluster.ID,
					}); err != nil {
						return &ctrl.Result{Requeue: true}, err
					}
				}
			}
			//Unbind agents
			if err = r.unbindAgents(ctx, log, req.NamespacedName); err != nil {
				return &ctrl.Result{Requeue: true}, err
			}

			// deletion finalizer found, deregister the backend cluster
			reply, cleanUpErr := r.deregisterClusterIfNeeded(ctx, log, req.NamespacedName)
			if cleanUpErr != nil {
				log.WithError(cleanUpErr).Errorf("failed to run pre-deletion cleanup for finalizer %s on resource %s %s",
					AgentClusterInstallFinalizerName, clusterInstall.Name, clusterInstall.Namespace)
				return &reply, cleanUpErr
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(clusterInstall, AgentClusterInstallFinalizerName)
			if err := r.Update(ctx, clusterInstall); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from resource %s %s",
					AgentClusterInstallFinalizerName, clusterInstall.Name, clusterInstall.Namespace)
				return &ctrl.Result{Requeue: true}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return &ctrl.Result{}, nil
	}
	return nil, nil
}

func (r *ClusterDeploymentsReconciler) clusterDeploymentFinalizer(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment) (*ctrl.Result, error) {
	if clusterDeployment.ObjectMeta.DeletionTimestamp.IsZero() { // clusterDeployment not being deleted
		// Register a finalizer if it is absent.
		if !funk.ContainsString(clusterDeployment.GetFinalizers(), ClusterDeploymentFinalizerName) {
			controllerutil.AddFinalizer(clusterDeployment, ClusterDeploymentFinalizerName)
			if err := r.Update(ctx, clusterDeployment); err != nil {
				log.WithError(err).Errorf("failed to add finalizer %s to resource %s %s",
					ClusterDeploymentFinalizerName, clusterDeployment.Name, clusterDeployment.Namespace)
				return &ctrl.Result{Requeue: true}, err
			}
		}
	} else { // clusterDeployment is being deleted
		if funk.ContainsString(clusterDeployment.GetFinalizers(), ClusterDeploymentFinalizerName) {
			reply, cleanUpErr := r.deleteClusterInstall(ctx, log, clusterDeployment)
			if cleanUpErr != nil {
				log.WithError(cleanUpErr).Errorf(
					"clusterDeployment %s %s is still waiting for clusterInstall %s to be deleted",
					clusterDeployment.Name, clusterDeployment.Namespace, clusterDeployment.Spec.ClusterInstallRef.Name,
				)
				return &reply, cleanUpErr
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(clusterDeployment, ClusterDeploymentFinalizerName)
			if err := r.Update(ctx, clusterDeployment); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from resource %s %s",
					ClusterDeploymentFinalizerName, clusterDeployment.Name, clusterDeployment.Namespace)
				return &ctrl.Result{Requeue: true}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return &ctrl.Result{}, nil
	}
	return nil, nil
}

func isInstalled(clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall) bool {
	if clusterDeployment.Spec.Installed {
		return true
	}
	cond := FindStatusCondition(clusterInstall.Status.Conditions, hiveext.ClusterCompletedCondition)
	return cond != nil && cond.Reason == hiveext.ClusterInstalledReason
}

func (r *ClusterDeploymentsReconciler) installDay1(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster, pullSecret string) (ctrl.Result, error) {
	ready, err := r.isReadyForInstallation(ctx, log, clusterInstall, cluster)
	if err != nil {
		log.WithError(err).Error("failed to check if cluster ready for installation")
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}
	if ready {
		// create custom manifests if needed before installation
		err = r.addCustomManifests(ctx, log, clusterInstall, cluster)
		if err != nil {
			log.WithError(err).Error("failed to add custom manifests")
			_, _ = r.updateStatus(ctx, log, clusterInstall, cluster, err)
			// We decided to requeue with one minute timeout in order to give user a chance to fix manifest
			// this timeout allows us not to run reconcile too much time and
			// still have a nice feedback when user will fix the error
			return ctrl.Result{Requeue: true, RequeueAfter: longerRequeueAfterOnError}, nil
		}

		log.Infof("Installing clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
		var ic *common.Cluster
		ic, err = r.Installer.InstallClusterInternal(ctx, installer.V2InstallClusterParams{
			ClusterID: *cluster.ID,
		})
		if err != nil {
			log.WithError(err).Error("failed to start cluster install")
			return r.updateStatus(ctx, log, clusterInstall, cluster, err)
		}
		return r.updateStatus(ctx, log, clusterInstall, ic, err)
	}
	return r.updateStatus(ctx, log, clusterInstall, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) spokeKubeClient(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) (spoke_k8s_client.SpokeK8sClient, error) {
	adminKubeConfigSecretName := getClusterDeploymentAdminKubeConfigSecretName(clusterDeployment)

	namespacedName := types.NamespacedName{
		Namespace: clusterDeployment.Namespace,
		Name:      adminKubeConfigSecretName,
	}

	secret, err := getSecret(ctx, r.Client, r.APIReader, namespacedName)
	if err != nil {
		r.Log.WithError(err).Errorf("failed to get kubeconfig secret %s", namespacedName)
		return nil, err
	}
	if err = ensureSecretIsLabelled(ctx, r.Client, secret, namespacedName); err != nil {
		r.Log.WithError(err).Errorf("failed to label kubeconfig secret %s", namespacedName)
		return nil, err
	}
	return r.SpokeK8sClientFactory.CreateFromSecret(secret)
}

func (r *ClusterDeploymentsReconciler) updateWorkerMcpPaused(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, clusterDeployment *hivev1.ClusterDeployment) error {
	agents, err := findAgentsByAgentClusterInstall(r.Client, ctx, log, clusterInstall)
	if err != nil {
		log.WithError(err).Errorf("failed to find agents for cluster install %s/%s", clusterInstall.Namespace, clusterInstall.Name)
		return err
	}
	var paused, shouldUpdate bool
	funk.ForEach(agents, func(agent *aiv1beta1.Agent) {
		// State is checked to be not equal to "installed" to verify that day1 installed nodes are not taken into account
		if !funk.IsEmpty(agent.Spec.NodeLabels) && agent.Spec.MachineConfigPool != "" && agent.Status.DebugInfo.State != models.HostStatusInstalled {
			shouldUpdate = true

			// MCP should be paused when there is at least 1 installing day2 node.  Otherwise, it should be unpaused
			if agent.Status.Progress.CurrentStage != models.HostStageDone &&
				funk.ContainsString([]string{models.HostStatusInstalling, models.HostStatusInstallingInProgress}, agent.Status.DebugInfo.State) {
				paused = true
			}
		}
	})
	if shouldUpdate {
		spokeClient, err := r.spokeKubeClient(ctx, clusterDeployment)
		if err != nil {
			log.WithError(err).Errorf("failed to create spoke client for cluster deployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
			return err
		}
		err = spokeClient.PatchMachineConfigPoolPaused(paused, "worker")
		if err != nil {
			log.WithError(err).Errorf("failed to patch worker machine config pool for cluster deployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
			return err
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) installDay2Hosts(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) (ctrl.Result, error) {
	hosts, err := r.Installer.GetKnownApprovedHosts(*cluster.ID)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ready and approved hosts for cluster %s", cluster.ID.String())
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}
	for _, h := range hosts {
		log.Infof("Installing Day2 host %s in %s %s", *h.ID, clusterDeployment.Name, clusterDeployment.Namespace)
		err = r.Installer.InstallSingleDay2HostInternal(ctx, *cluster.ID, h.InfraEnvID, *h.ID)
		if err != nil {
			return r.updateStatus(ctx, log, clusterInstall, cluster, err)
		}
	}
	err = r.updateWorkerMcpPaused(ctx, log, clusterInstall, clusterDeployment)
	return r.updateStatus(ctx, log, clusterInstall, cluster, err)
}

func (r *ClusterDeploymentsReconciler) createNoIngressKubeConfig(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster, clusterInstall *hiveext.AgentClusterInstall) error {
	if clusterInstall.Spec.ClusterMetadata != nil {
		return nil
	}
	k, err := r.ensureKubeConfigNoIngressSecret(ctx, log, cluster, c)
	if err != nil {
		return err
	}
	clusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		ClusterID: c.OpenshiftClusterID.String(),
		InfraID:   string(*c.ID),
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: k.Name,
		},
	}
	return r.Update(ctx, clusterInstall)
}

func (r *ClusterDeploymentsReconciler) updateClusterMetadata(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster, clusterInstall *hiveext.AgentClusterInstall) error {

	s, err := r.ensureAdminPasswordSecret(ctx, log, cluster, c)
	if err != nil {
		return err
	}
	k, err := r.updateKubeConfigSecret(ctx, log, cluster, c)
	if err != nil {
		return err
	}
	clusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		ClusterID: c.OpenshiftClusterID.String(),
		InfraID:   string(*c.ID),
		AdminPasswordSecretRef: &corev1.LocalObjectReference{
			Name: s.Name,
		},
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: k.Name,
		},
	}
	return r.Update(ctx, clusterInstall)
}

func (r *ClusterDeploymentsReconciler) ensureAdminPasswordSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	name := fmt.Sprintf(adminPasswordSecretStringTemplate, cluster.Name)
	secretRef := types.NamespacedName{Namespace: cluster.Namespace, Name: name}
	s, getErr := getSecret(ctx, r.Client, r.APIReader, secretRef)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	if err := ensureSecretIsLabelled(ctx, r.Client, s, secretRef); err != nil {
		return s, errors.Wrap(err, "Failed to label user-data secret")
	}
	cred, err := r.Installer.GetCredentialsInternal(ctx, installer.V2GetCredentialsParams{
		ClusterID: *c.ID,
	})
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"username": []byte(cred.Username),
		"password": []byte(cred.Password),
	}
	return r.createClusterCredentialSecret(ctx, log, cluster, name, data, "kubeadmincreds")
}

func (r *ClusterDeploymentsReconciler) updateKubeConfigSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	name := getClusterDeploymentAdminKubeConfigSecretName(cluster)
	secretRef := types.NamespacedName{Namespace: cluster.Namespace, Name: name}
	s, getErr := getSecret(ctx, r.Client, r.APIReader, secretRef)
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return nil, getErr
	}
	if err := ensureSecretIsLabelled(ctx, r.Client, s, secretRef); err != nil {
		return s, errors.Wrap(err, "Failed to label user-data secret")
	}

	respBody, _, err := r.Installer.V2DownloadClusterCredentialsInternal(ctx, installer.V2DownloadClusterCredentialsParams{
		ClusterID: *c.ID,
		FileName:  constants.Kubeconfig,
	})
	if err != nil {
		return nil, err
	}
	respBytes, err := io.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"kubeconfig": respBytes,
	}
	if k8serrors.IsNotFound(getErr) {
		return r.createClusterCredentialSecret(ctx, log, cluster, name, data, "kubeconfig")
	}
	s.Data = data
	return s, r.Update(ctx, s)
}

func (r *ClusterDeploymentsReconciler) ensureKubeConfigNoIngressSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := getClusterDeploymentAdminKubeConfigSecretName(cluster)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	respBody, _, err := r.Installer.V2DownloadClusterCredentialsInternal(ctx, installer.V2DownloadClusterCredentialsParams{
		ClusterID: *c.ID,
		FileName:  constants.KubeconfigNoIngress,
	})
	if err != nil {
		return nil, err
	}
	respBytes, err := io.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"kubeconfig": respBytes,
	}

	return r.createClusterCredentialSecret(ctx, log, cluster, name, data, "kubeconfig")
}

func (r *ClusterDeploymentsReconciler) createClusterCredentialSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, name string, data map[string][]byte, secretType string) (*corev1.Secret, error) {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
		},
		Data: data,
	}

	s.Labels = AddLabel(s.Labels, "hive.openshift.io/cluster-deployment-name", cluster.Name)
	s.Labels = AddLabel(s.Labels, "hive.openshift.io/secret-type", secretType)

	deploymentGVK, err := apiutil.GVKForObject(cluster, r.Scheme)
	if err != nil {
		log.WithError(err).Errorf("error getting GVK for clusterdeployment")
		return nil, err
	}

	s.OwnerReferences = []metav1.OwnerReference{{
		APIVersion:         deploymentGVK.GroupVersion().String(),
		Kind:               deploymentGVK.Kind,
		Name:               cluster.Name,
		UID:                cluster.UID,
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}}
	return s, r.Create(ctx, s)
}

func (r *ClusterDeploymentsReconciler) isReadyForInstallation(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) (bool, error) {
	if ready, _ := r.ClusterApi.IsReadyForInstallation(c); !ready {
		return false, nil
	}

	registered, approvedHosts, err := r.getNumOfClusterAgents(c)
	if err != nil {
		log.WithError(err).Error("failed to fetch agents")
		return false, err
	}

	agents, err := findAgentsByAgentClusterInstall(r.Client, ctx, log, clusterInstall)
	if err != nil {
		log.WithError(err).Error("failed to fetch ACI's agents")
		return false, err
	}

	unsyncedHosts := getNumOfUnsyncedAgents(agents)
	log.Debugf("Calculating installation readiness, found %d unsynced agents out of total of %d agents", unsyncedHosts, len(agents))
	expectedHosts := clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents +
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents
	return approvedHosts == expectedHosts && registered == approvedHosts && unsyncedHosts == 0, nil
}

func isSupportedPlatform(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.ClusterInstallRef == nil ||
		cluster.Spec.ClusterInstallRef.Group != hiveext.Group ||
		cluster.Spec.ClusterInstallRef.Kind != "AgentClusterInstall" {
		return false
	}
	return true
}

// UserManagedNetworking is considered false if
// 1. The cluster is an SNO (with no workers)
// 2. The User specifically set this parameter to false
// 3. Or, if the user did not set the value at all
func isUserManagedNetwork(clusterInstall *hiveext.AgentClusterInstall) bool {
	return swag.BoolValue(clusterInstall.Spec.Networking.UserManagedNetworking) ||
		clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 && clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0
}

func isDiskEncryptionEnabled(clusterInstall *hiveext.AgentClusterInstall) bool {
	if clusterInstall.Spec.DiskEncryption == nil {
		return false
	}
	switch swag.StringValue(clusterInstall.Spec.DiskEncryption.EnableOn) {
	case models.DiskEncryptionEnableOnAll, models.DiskEncryptionEnableOnMasters, models.DiskEncryptionEnableOnWorkers:
		return true
	case models.DiskEncryptionEnableOnNone:
		return false
	default:
		return false
	}
}

// see https://docs.openshift.com/container-platform/4.7/installing/installing_platform_agnostic/installing-platform-agnostic.html#installation-bare-metal-config-yaml_installing-platform-agnostic
func hyperthreadingInSpec(clusterInstall *hiveext.AgentClusterInstall) bool {
	//check if either master or worker pool hyperthreading settings are explicitly specified
	return clusterInstall.Spec.ControlPlane != nil ||
		funk.Contains(clusterInstall.Spec.Compute, func(pool hiveext.AgentMachinePool) bool {
			return pool.Name == hiveext.WorkerAgentMachinePool
		})
}

func getPlatform(openshiftPlatformType hiveext.PlatformType) *models.Platform {
	if openshiftPlatformType == "" {
		//empty platform type means N/A. The service assigns a platform
		//according to the cluster specifications (HA, UserManagedNetworking etc.)
		return nil
	}

	//convert between openshift API and the service platform name convension
	switch openshiftPlatformType {
	case hiveext.VSpherePlatformType:
		return &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
		}
	case hiveext.NonePlatformType:
		return &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeNone),
		}
	case hiveext.BareMetalPlatformType:
		return &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
		}
	case hiveext.NutanixPlatformType:
		return &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
		}
	default:
		return nil
	}
}

func getPlatformType(platform *models.Platform) hiveext.PlatformType {
	if platform == nil || platform.Type == nil {
		return ""
	}

	switch *platform.Type {
	case models.PlatformTypeBaremetal:
		return hiveext.BareMetalPlatformType
	case models.PlatformTypeNone:
		return hiveext.NonePlatformType
	case models.PlatformTypeVsphere:
		return hiveext.VSpherePlatformType
	case models.PlatformTypeNutanix:
		return hiveext.NutanixPlatformType
	default:
		return ""
	}
}

func getHyperthreading(clusterInstall *hiveext.AgentClusterInstall) *string {
	const (
		None    = 0
		Workers = 1
		Masters = 2
		All     = 3
	)
	var config uint = 0

	//if there is no configuration of hyperthreading in the Spec then
	//we are opting of the default behavior which is all enabled
	if !hyperthreadingInSpec(clusterInstall) {
		config = All
	}

	//check if the Spec enables hyperthreading for workers
	for _, machinePool := range clusterInstall.Spec.Compute {
		if machinePool.Name == hiveext.WorkerAgentMachinePool && machinePool.Hyperthreading == hiveext.HyperthreadingEnabled {
			config = config | Workers
		}
	}

	//check if the Spec enables hyperthreading for masters
	if clusterInstall.Spec.ControlPlane != nil {
		if clusterInstall.Spec.ControlPlane.Hyperthreading == hiveext.HyperthreadingEnabled {
			config = config | Masters
		}
	}

	//map between CRD Spec and cluster API
	switch config {
	case None:
		return swag.String(models.ClusterHyperthreadingNone)
	case Workers:
		return swag.String(models.ClusterHyperthreadingWorkers)
	case Masters:
		return swag.String(models.ClusterHyperthreadingMasters)
	default:
		return swag.String(models.ClusterHyperthreadingAll)
	}
}

func (r *ClusterDeploymentsReconciler) getEncodedCACert(ctx context.Context,
	log logrus.FieldLogger,
	caCertificateRef *hiveext.CaCertificateReference) (*string, error) {
	secretRef := types.NamespacedName{Namespace: caCertificateRef.Namespace, Name: caCertificateRef.Name}
	caSecret, err := getSecret(ctx, r.Client, r.APIReader, secretRef)
	if err != nil {
		return nil, errors.Wrap(err, "error getting ca certificate secret")
	}
	if err := ensureSecretIsLabelled(ctx, r.Client, caSecret, secretRef); err != nil {
		return nil, errors.Wrap(err, "Failed to label user-data secret")
	}
	caCertBytes, hasCACert := caSecret.Data[corev1.TLSCertKey]
	if !hasCACert {
		return nil, fmt.Errorf("CA Secret is missing tls.crt key")
	}
	encodedCACert := base64.StdEncoding.EncodeToString(caCertBytes)
	caCertificate := swag.String(encodedCACert)
	return caCertificate, nil
}

func (r *ClusterDeploymentsReconciler) parseIgnitionEndpoint(ctx context.Context,
	log logrus.FieldLogger,
	kubeapiIgnitionEndpoint *hiveext.IgnitionEndpoint) (*models.IgnitionEndpoint, error) {

	ignitionEndpoint := &models.IgnitionEndpoint{}
	ignitionEndpoint.URL = swag.String(kubeapiIgnitionEndpoint.Url)

	caCertificateReference := kubeapiIgnitionEndpoint.CaCertificateReference
	if caCertificateReference != nil {
		caCertificate, err := r.getEncodedCACert(ctx, log, caCertificateReference)
		if err == nil {
			ignitionEndpoint.CaCertificate = caCertificate
		} else {
			return nil, err
		}
	} else {
		ignitionEndpoint.CaCertificate = swag.String("")
	}
	return ignitionEndpoint, nil
}

func (r *ClusterDeploymentsReconciler) updateIgnitionInUpdateParams(ctx context.Context,
	log logrus.FieldLogger,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster,
	params *models.V2ClusterUpdateParams) (bool, error) {
	update := false
	if clusterInstall.Spec.IgnitionEndpoint != nil {
		ignitionEndpoint, err := r.parseIgnitionEndpoint(ctx, log, clusterInstall.Spec.IgnitionEndpoint)
		if err == nil {
			params.IgnitionEndpoint = ignitionEndpoint
			if cluster.IgnitionEndpoint == nil || !reflect.DeepEqual(cluster.IgnitionEndpoint, ignitionEndpoint) {
				update = true
			}
		} else {
			log.WithError(err).Errorf("Failed to get and parse ignition ca certificate %s/%s", clusterInstall.Namespace, clusterInstall.Name)
			return false, errors.Wrap(err, fmt.Sprintf("Failed to get and parse ignition ca certificate %s/%s", clusterInstall.Namespace, clusterInstall.Name))
		}
	} else {
		if cluster.IgnitionEndpoint != nil {
			if cluster.IgnitionEndpoint.URL != nil {
				update = true
				params.IgnitionEndpoint.URL = swag.String("")
			}
			if cluster.IgnitionEndpoint.CaCertificate != nil {
				update = true
				params.IgnitionEndpoint.CaCertificate = swag.String("")
			}
		}
	}
	return update, nil
}

func (r *ClusterDeploymentsReconciler) updateNetworkParams(clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster, params *models.V2ClusterUpdateParams) (*bool, error) {
	update := false
	updateString := func(new, old string, target **string) {
		if new != old {
			*target = swag.String(new)
			update = true
		}
	}

	if len(clusterInstall.Spec.Networking.ClusterNetwork) > 0 {
		newClusterNetworks := clusterNetworksEntriesToArray(clusterInstall.Spec.Networking.ClusterNetwork)
		if len(newClusterNetworks) != len(cluster.ClusterNetworks) {
			params.ClusterNetworks = newClusterNetworks
			update = true
		} else {
			for index := range newClusterNetworks {
				if newClusterNetworks[index].Cidr != cluster.ClusterNetworks[index].Cidr ||
					newClusterNetworks[index].HostPrefix != cluster.ClusterNetworks[index].HostPrefix {
					params.ClusterNetworks = newClusterNetworks
					update = true
					break
				}
			}
		}
	}
	if len(clusterInstall.Spec.Networking.ServiceNetwork) > 0 {
		newServiceNetworks := serviceNetworksEntriesToArray(clusterInstall.Spec.Networking.ServiceNetwork)
		if len(newServiceNetworks) != len(cluster.ServiceNetworks) {
			params.ServiceNetworks = newServiceNetworks
			update = true
		} else {
			for index := range newServiceNetworks {
				if newServiceNetworks[index].Cidr != cluster.ServiceNetworks[index].Cidr {
					params.ServiceNetworks = newServiceNetworks
					update = true
					break
				}
			}
		}
	}
	if len(clusterInstall.Spec.Networking.MachineNetwork) > 0 {
		newMachineNetworks := machineNetworksEntriesToArray(clusterInstall.Spec.Networking.MachineNetwork)
		if len(newMachineNetworks) != len(cluster.MachineNetworks) {
			params.MachineNetworks = newMachineNetworks
			update = true
		} else {
			for index := range newMachineNetworks {
				if newMachineNetworks[index].Cidr != cluster.MachineNetworks[index].Cidr {
					params.MachineNetworks = newMachineNetworks
					update = true
					break
				}
			}
		}
	}

	if clusterInstall.Spec.Networking.NetworkType != "" {
		updateString(clusterInstall.Spec.Networking.NetworkType, swag.StringValue(cluster.NetworkType), &params.NetworkType)
	} else if !clusterDeployment.Spec.Installed {
		desiredNetworkType, err := selectClusterNetworkType(params, cluster)
		if err != nil {
			return nil, err
		}
		updateString(swag.StringValue(desiredNetworkType), swag.StringValue(cluster.NetworkType), &params.NetworkType)
	}

	// Update APIVIP and IngressVIP only if cluster spec has VIPs defined and no DHCP enabled.
	// In absence of this check, the reconcile loop in the controller fails all the time.
	//
	// We must not run this reconciler in case VIPs are missing from the cluster spec as this can
	// indicate a scenario when backend calculates them automatically, e.g. SNO cluster.
	isDHCPEnabled := swag.BoolValue(cluster.VipDhcpAllocation)
	if !isDHCPEnabled && (clusterInstall.Spec.APIVIP != "" || clusterInstall.Spec.IngressVIP != "") {
		desiredApiVips, _ := validations.HandleApiVipBackwardsCompatibility(
			*cluster.ID,
			clusterInstall.Spec.APIVIP,
			apiVipsEntriesToArray(clusterInstall.Spec.APIVIPs))

		if clusterInstall.Spec.APIVIP != cluster.APIVip ||
			!network.AreApiVipsIdentical(desiredApiVips, cluster.APIVips) {

			params.APIVip = swag.String(clusterInstall.Spec.APIVIP)
			params.APIVips = desiredApiVips
			update = true
		}

		desiredIngressVips, _ := validations.HandleIngressVipBackwardsCompatibility(*cluster.ID,
			clusterInstall.Spec.IngressVIP,
			ingressVipsEntriesToArray(clusterInstall.Spec.IngressVIPs))

		if clusterInstall.Spec.IngressVIP != cluster.IngressVip ||
			!network.AreIngressVipsIdentical(desiredIngressVips, cluster.IngressVips) {

			params.IngressVip = swag.String(clusterInstall.Spec.IngressVIP)
			params.IngressVips = desiredIngressVips
			update = true
		}
	}

	if userManagedNetwork := isUserManagedNetwork(clusterInstall); userManagedNetwork != swag.BoolValue(cluster.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(userManagedNetwork)
	}
	return swag.Bool(update), nil
}

func (r *ClusterDeploymentsReconciler) updateIfNeeded(ctx context.Context,
	log logrus.FieldLogger,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) (*common.Cluster, error) {

	update := false
	params := &models.V2ClusterUpdateParams{}

	spec := clusterDeployment.Spec
	updateString := func(new, old string, target **string) {
		if new != old {
			*target = swag.String(new)
			update = true
		}
	}

	updateString(spec.ClusterName, cluster.Name, &params.Name)
	updateString(spec.BaseDomain, cluster.BaseDNSDomain, &params.BaseDNSDomain)

	shouldUpdateNetworkParams, err := r.updateNetworkParams(clusterDeployment, clusterInstall, cluster, params)
	if err != nil {
		return cluster, errors.Wrap(err, "failed to update network params")
	}
	update = swag.BoolValue(shouldUpdateNetworkParams) || update
	// Trim key before comapring as done in RegisterClusterInternal
	sshPublicKey := strings.TrimSpace(clusterInstall.Spec.SSHPublicKey)
	updateString(sshPublicKey, cluster.SSHPublicKey, &params.SSHPublicKey)

	// Update ignition endpoint if needed
	shouldUpdate, err := r.updateIgnitionInUpdateParams(ctx, log, clusterInstall, cluster, params)
	if err != nil {
		return cluster, errors.Wrap(err, "Couldn't resolve clusterdeployment ignition fields")
	}
	update = shouldUpdate || update

	pullSecretData, err := r.PullSecretHandler.GetValidPullSecret(ctx, getPullSecretKey(clusterDeployment.Namespace, spec.PullSecretRef))
	if err != nil {
		return cluster, errors.Wrap(err, "failed to get pull secret for update")
	}

	if pullSecretData != cluster.PullSecret {
		params.PullSecret = swag.String(pullSecretData)
		update = true
	}

	// update hyperthreading settings
	hyperthreading := getHyperthreading(clusterInstall)
	if cluster.Hyperthreading != *hyperthreading {
		params.Hyperthreading = hyperthreading
		update = true
	}

	//update platform
	platform := getPlatform(clusterInstall.Spec.PlatformType)
	if cluster.Platform != nil && platform != nil && *cluster.Platform.Type != *platform.Type {
		params.Platform = platform
		update = true
	}

	if clusterInstall.Spec.DiskEncryption != nil {
		params.DiskEncryption = &models.DiskEncryption{}
		if cluster.DiskEncryption == nil { // true when current cluster configuration does not include disk encryption
			cluster.DiskEncryption = &models.DiskEncryption{}
		}
		updateString(swag.StringValue(clusterInstall.Spec.DiskEncryption.EnableOn), swag.StringValue(cluster.DiskEncryption.EnableOn), &params.DiskEncryption.EnableOn)
		updateString(swag.StringValue(clusterInstall.Spec.DiskEncryption.Mode), swag.StringValue(cluster.DiskEncryption.Mode), &params.DiskEncryption.Mode)
		if clusterInstall.Spec.DiskEncryption.TangServers != cluster.DiskEncryption.TangServers {
			params.DiskEncryption.TangServers = clusterInstall.Spec.DiskEncryption.TangServers
			update = true
		}
	}

	if clusterInstall.Spec.Proxy != nil {
		updateString(swag.StringValue(&clusterInstall.Spec.Proxy.HTTPProxy), cluster.HTTPProxy, &params.HTTPProxy)
		updateString(swag.StringValue(&clusterInstall.Spec.Proxy.HTTPSProxy), cluster.HTTPSProxy, &params.HTTPSProxy)
		updateString(swag.StringValue(&clusterInstall.Spec.Proxy.NoProxy), cluster.NoProxy, &params.NoProxy)
	} else {
		params.HTTPSProxy = swag.String("")
		params.HTTPProxy = swag.String("")
		params.NoProxy = swag.String("")
	}

	if !update {
		return cluster, nil
	}

	var clusterAfterUpdate *common.Cluster

	clusterAfterUpdate, err = r.Installer.UpdateClusterNonInteractive(ctx, installer.V2UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *cluster.ID,
	})
	if err != nil {
		return cluster, err
	}

	log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)

	return clusterAfterUpdate, nil
}

func selectClusterNetworkType(params *models.V2ClusterUpdateParams, cluster *common.Cluster) (*string, error) {
	clusterWithNewNetworks := &common.Cluster{
		Cluster: models.Cluster{
			ClusterNetworks: cluster.ClusterNetworks,
			ServiceNetworks: cluster.ServiceNetworks,
			MachineNetworks: cluster.MachineNetworks,
		},
	}

	if common.IsSliceNonEmpty(params.ClusterNetworks) {
		clusterWithNewNetworks.ClusterNetworks = params.ClusterNetworks
	}
	if common.IsSliceNonEmpty(params.ServiceNetworks) {
		clusterWithNewNetworks.ServiceNetworks = params.ServiceNetworks
	}
	if common.IsSliceNonEmpty(params.MachineNetworks) {
		clusterWithNewNetworks.MachineNetworks = params.MachineNetworks
	}

	isOpenShiftVersionRecentEnough, err := common.VersionGreaterOrEqual(cluster.OpenshiftVersion, minimalOpenShiftVersionForDefaultNetworkTypeOVNKubernetes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse cluster OpenShift version")
	}

	if funk.Any(funk.Filter(common.GetNetworksCidrs(clusterWithNewNetworks), func(ip *string) bool {
		if ip == nil {
			return false
		}
		return network.IsIPv6CIDR(*ip)
	})) || common.IsSingleNodeCluster(cluster) || isOpenShiftVersionRecentEnough {
		return swag.String(models.ClusterNetworkTypeOVNKubernetes), nil
	} else {
		return swag.String(models.ClusterNetworkTypeOpenShiftSDN), nil
	}
}

func (r *ClusterDeploymentsReconciler) updateInstallConfigOverrides(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {
	// handle InstallConfigOverrides
	update := false
	annotations := clusterInstall.ObjectMeta.GetAnnotations()
	installConfigOverrides := annotations[InstallConfigOverrides]
	if cluster.InstallConfigOverrides != installConfigOverrides {
		cluster.InstallConfigOverrides = installConfigOverrides
		update = true
	}
	if update {
		_, err := r.Installer.UpdateClusterInstallConfigInternal(ctx, installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           *cluster.ID,
			InstallConfigParams: cluster.InstallConfigOverrides,
		})
		if err != nil {
			if IsUserError(err) {
				apiErr := errors.Wrapf(err, "Failed to parse '%s' annotation", InstallConfigOverrides)
				return common.NewApiError(http.StatusBadRequest, apiErr)
			}
			return err
		}
		log.Infof("Updated InstallConfig overrides on clusterInstall %s/%s", clusterInstall.Namespace, clusterInstall.Name)
		return nil
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) syncManifests(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster,
	clusterInstall *hiveext.AgentClusterInstall, alreadyCreatedManifests models.ListManifests) error {

	log.Debugf("Going to sync list of given with already created manifests")

	manifestsFromConfigMap, err := r.getClusterDeploymentManifest(ctx, log, clusterInstall)
	if err != nil {
		return err
	}

	// delete all manifests that are not part of configmap
	// skip errors
	for _, manifest := range alreadyCreatedManifests {
		if _, ok := manifestsFromConfigMap[manifest.FileName]; !ok {
			log.Infof("Deleting cluster deployment %s manifest %s", cluster.KubeKeyName, manifest.FileName)
			_ = r.Manifests.DeleteClusterManifestInternal(ctx, operations.V2DeleteClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  manifest.FileName,
				Folder:    swag.String(models.ManifestFolderOpenshift),
			})
		}
	}

	// create/update all manifests provided by configmap data
	for filename, manifest := range manifestsFromConfigMap {
		log.Infof("Creating cluster deployment %s manifest %s", cluster.KubeKeyName, filename)
		_, err := r.Manifests.CreateClusterManifestInternal(ctx, operations.V2CreateClusterManifestParams{
			ClusterID: *cluster.ID,
			CreateManifestParams: &models.CreateManifestParams{
				Content:  swag.String(base64.StdEncoding.EncodeToString([]byte(manifest))),
				FileName: swag.String(filename),
				Folder:   swag.String(models.ManifestFolderOpenshift),
			}})
		if err != nil {
			log.WithError(err).Errorf("Failed to create cluster deployment %s manifest %s", cluster.KubeKeyName, filename)
			return err
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) getClusterDeploymentManifest(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall) (map[string]string, error) {
	configuredManifests := map[string]string{}

	// Get manifests from referenced ConfigMaps (fallback to single ManifestsConfigMapRef)
	if clusterInstall.Spec.ManifestsConfigMapRefs != nil {
		for _, ref := range clusterInstall.Spec.ManifestsConfigMapRefs {
			configMap, err := r.getManifestConfigMap(ctx, log, clusterInstall, ref.Name)
			if err != nil {
				return nil, err
			}
			// Add data to manifests map
			for k, v := range configMap.Data {
				if _, exists := configuredManifests[k]; exists {
					return nil, errors.Errorf("Conflict in manifest names ('%s' is not unique)", k)
				}
				configuredManifests[k] = v
			}
		}
	} else if clusterInstall.Spec.ManifestsConfigMapRef != nil {
		configMap, err := r.getManifestConfigMap(ctx, log, clusterInstall, clusterInstall.Spec.ManifestsConfigMapRef.Name)
		if err != nil {
			return nil, err
		}
		configuredManifests = configMap.Data
	}

	return configuredManifests, nil
}

func (r *ClusterDeploymentsReconciler) getManifestConfigMap(ctx context.Context, log logrus.FieldLogger,
	clusterInstall *hiveext.AgentClusterInstall, configMapName string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	configMap.Data = map[string]string{}
	err := r.Get(ctx,
		types.NamespacedName{
			Namespace: clusterInstall.Namespace,
			Name:      configMapName,
		},
		configMap,
	)
	if err != nil {
		log.WithError(err).Errorf("Failed to get configmap %s in %s", configMapName, clusterInstall.Namespace)
		return nil, err
	}
	return configMap, nil
}

func (r *ClusterDeploymentsReconciler) addCustomManifests(ctx context.Context, log logrus.FieldLogger,
	clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) error {

	alreadyCreatedManifests, err := r.Manifests.ListClusterManifestsInternal(ctx, operations.V2ListClusterManifestsParams{
		ClusterID: *cluster.ID,
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to list manifests for %q cluster install", clusterInstall.Name)
		return err
	}

	// if reference to manifests was deleted from cluster deployment
	// but we already added some in previous reconcile loop, we want to clean them.
	// if no reference were provided we will delete all manifests that were in the list
	if len(alreadyCreatedManifests) == 0 &&
		clusterInstall.Spec.ManifestsConfigMapRef == nil &&
		clusterInstall.Spec.ManifestsConfigMapRefs == nil {
		log.Debugf("Nothing to do, skipping manifest creation")
		return nil
	}

	return r.syncManifests(ctx, log, cluster, clusterInstall, alreadyCreatedManifests)
}

func CreateClusterParams(clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall,
	pullSecret string, releaseImageVersion string, releaseImageCPUArch string,
	ignitionEndpoint *models.IgnitionEndpoint) *models.ClusterCreateParams {
	spec := clusterDeployment.Spec

	clusterParams := &models.ClusterCreateParams{
		BaseDNSDomain:         spec.BaseDomain,
		Name:                  swag.String(spec.ClusterName),
		OpenshiftVersion:      &releaseImageVersion,
		OlmOperators:          nil, // TODO: handle operators
		PullSecret:            swag.String(pullSecret),
		VipDhcpAllocation:     swag.Bool(false),
		APIVip:                clusterInstall.Spec.APIVIP,
		IngressVip:            clusterInstall.Spec.IngressVIP,
		SSHPublicKey:          clusterInstall.Spec.SSHPublicKey,
		CPUArchitecture:       releaseImageCPUArch,
		UserManagedNetworking: swag.Bool(isUserManagedNetwork(clusterInstall)),
		Platform:              getPlatform(clusterInstall.Spec.PlatformType),
	}

	if len(clusterInstall.Spec.Networking.ClusterNetwork) > 0 {
		for _, net := range clusterInstall.Spec.Networking.ClusterNetwork {
			clusterParams.ClusterNetworks = append(clusterParams.ClusterNetworks, &models.ClusterNetwork{
				Cidr:       models.Subnet(net.CIDR),
				HostPrefix: int64(net.HostPrefix)})
		}
	}

	if len(clusterInstall.Spec.Networking.ServiceNetwork) > 0 {
		for _, cidr := range clusterInstall.Spec.Networking.ServiceNetwork {
			clusterParams.ServiceNetworks = append(clusterParams.ServiceNetworks, &models.ServiceNetwork{
				Cidr: models.Subnet(cidr),
			})
		}
	}

	if len(clusterInstall.Spec.Networking.MachineNetwork) > 0 {
		for _, net := range clusterInstall.Spec.Networking.MachineNetwork {
			clusterParams.MachineNetworks = append(clusterParams.MachineNetworks, &models.MachineNetwork{
				Cidr: models.Subnet(net.CIDR),
			})
		}
	}

	if clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0 {
		clusterParams.HighAvailabilityMode = swag.String(HighAvailabilityModeNone)
	}

	if hyperthreadingInSpec(clusterInstall) {
		clusterParams.Hyperthreading = getHyperthreading(clusterInstall)
	}

	if isDiskEncryptionEnabled(clusterInstall) {
		clusterParams.DiskEncryption = &models.DiskEncryption{
			EnableOn:    clusterInstall.Spec.DiskEncryption.EnableOn,
			Mode:        clusterInstall.Spec.DiskEncryption.Mode,
			TangServers: clusterInstall.Spec.DiskEncryption.TangServers,
		}
	}

	if clusterInstall.Spec.Proxy != nil {
		if clusterInstall.Spec.Proxy.NoProxy != "" {
			clusterParams.NoProxy = swag.String(clusterInstall.Spec.Proxy.NoProxy)
		}
		if clusterInstall.Spec.Proxy.HTTPProxy != "" {
			clusterParams.HTTPProxy = swag.String(clusterInstall.Spec.Proxy.HTTPProxy)
		}
		if clusterInstall.Spec.Proxy.HTTPSProxy != "" {
			clusterParams.HTTPSProxy = swag.String(clusterInstall.Spec.Proxy.HTTPSProxy)
		}
	}

	if ignitionEndpoint != nil {
		clusterParams.IgnitionEndpoint = ignitionEndpoint
	}

	return clusterParams
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	log logrus.FieldLogger,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {

	log.Infof("Creating a new cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec

	pullSecret, err := r.PullSecretHandler.GetValidPullSecret(ctx, getPullSecretKey(key.Namespace, spec.PullSecretRef))
	if err != nil {
		log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, log, clusterInstall, nil, err)
	}

	releaseImage, err := r.getReleaseImage(ctx, log, clusterInstall.Spec, pullSecret)
	if err != nil {
		log.WithError(err)
		_, _ = r.updateStatus(ctx, log, clusterInstall, nil, err)
		// The controller will requeue after one minute, giving the user a chance to fix releaseImage
		return ctrl.Result{Requeue: true, RequeueAfter: longerRequeueAfterOnError}, nil
	}

	var ignitionEndpoint *models.IgnitionEndpoint
	if clusterInstall.Spec.IgnitionEndpoint != nil {
		ignitionEndpoint, err = r.parseIgnitionEndpoint(ctx, log, clusterInstall.Spec.IgnitionEndpoint)
		if err != nil {
			log.WithError(err).Errorf("Failed to get and parse ignition ca certificate %s/%s", clusterInstall.Namespace, clusterInstall.Name)
			return r.updateStatus(ctx, log, clusterInstall, nil, err)
		}
	}

	clusterParams := CreateClusterParams(clusterDeployment, clusterInstall, pullSecret, *releaseImage.Version,
		*releaseImage.CPUArchitecture, ignitionEndpoint)

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.V2RegisterClusterParams{
		NewClusterParams: clusterParams,
	})

	return r.updateStatus(ctx, log, clusterInstall, c, err)
}

func (r *ClusterDeploymentsReconciler) TransformClusterToDay2(
	ctx context.Context,
	log logrus.FieldLogger,
	cluster *common.Cluster,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {
	log.Infof("transforming day1 cluster %s into day2 cluster", cluster.ID.String())
	c, err := r.Installer.TransformClusterToDay2Internal(ctx, *cluster.ID)
	if err != nil {
		log.WithError(err).Errorf("failed to transform cluster %s into day2 cluster", cluster.ID.String())
	}
	return r.updateStatus(ctx, log, clusterInstall, c, err)
}

func (r *ClusterDeploymentsReconciler) createNewDay2Cluster(
	ctx context.Context,
	log logrus.FieldLogger,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {

	log.Infof("Creating a new Day2 Cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec
	id := strfmt.UUID(uuid.New().String())
	apiVipDnsname := fmt.Sprintf("api.%s.%s", spec.ClusterName, spec.BaseDomain)

	clusterParams := &models.ImportClusterParams{
		APIVipDnsname: swag.String(apiVipDnsname),
		Name:          swag.String(spec.ClusterName),
	}

	// add optional parameter
	if clusterInstall.Spec.ClusterMetadata != nil {
		cid := strfmt.UUID(clusterInstall.Spec.ClusterMetadata.ClusterID)
		clusterParams.OpenshiftClusterID = &cid
	}

	c, err := r.Installer.V2ImportClusterInternal(ctx, &key, &id, installer.V2ImportClusterParams{
		NewImportClusterParams: clusterParams,
	})
	if err != nil {
		log.WithError(err).Error("failed to create day2 cluster")
	}
	return r.updateStatus(ctx, log, clusterInstall, c, err)
}

func (r *ClusterDeploymentsReconciler) getReleaseImage(
	ctx context.Context,
	log logrus.FieldLogger,
	spec hiveext.AgentClusterInstallSpec,
	pullSecret string) (*models.ReleaseImage, error) {

	clusterImageSet := &hivev1.ClusterImageSet{}
	key := types.NamespacedName{
		Namespace: "",
		Name:      spec.ImageSetRef.Name,
	}
	if err := r.Client.Get(ctx, key, clusterImageSet); err != nil {
		return nil, errors.Wrapf(err, "failed to get cluster image set %s", key.Name)
	}

	releaseImage, err := r.VersionsHandler.GetReleaseImageByURL(ctx, clusterImageSet.Spec.ReleaseImage, pullSecret)
	if err != nil {
		log.Error(err)
		errMsg := "failed to get release image '%s'. Please ensure the releaseImage field in ClusterImageSet '%s' is valid (error: %s)."
		return nil, errors.New(fmt.Sprintf(errMsg, clusterImageSet.Spec.ReleaseImage, spec.ImageSetRef.Name, err.Error()))
	}

	return releaseImage, nil
}

func (r *ClusterDeploymentsReconciler) deregisterClusterIfNeeded(ctx context.Context, log logrus.FieldLogger, key types.NamespacedName) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "failed to deregister cluster: %s", key.Name)
		log.Error(err)
		return reply, err
	}

	c, err := r.Installer.GetClusterByKubeKey(key)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return if from any reason cluster is already deleted from db (or never existed)
		return buildReply(nil)
	}

	if err != nil {
		return buildReply(err)
	}

	if err = r.Installer.DeregisterClusterInternal(ctx, c); err != nil {
		return buildReply(err)
	}

	log.Infof("Cluster resource deleted, Unregistered cluster: %s", c.ID.String())

	return buildReply(nil)
}

func (r *ClusterDeploymentsReconciler) deleteClusterInstall(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "clusterInstall: %s not deleted", clusterDeployment.Spec.ClusterInstallRef.Name)
		log.Error(err)
		return reply, err
	}

	clusterInstall := &hiveext.AgentClusterInstall{}
	err := r.Get(ctx,
		types.NamespacedName{
			Name:      clusterDeployment.Spec.ClusterInstallRef.Name,
			Namespace: clusterDeployment.Namespace,
		},
		clusterInstall)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return buildReply(nil)
		}
		return buildReply(err)
	}

	if err = r.Delete(ctx, clusterInstall); err != nil {
		return buildReply(err)
	}
	// place this err so we requeue and verify deletion
	err = errors.Errorf("could not confirm clusterInstall %s deletion was successfuly completed", clusterDeployment.Spec.ClusterInstallRef.Name)
	return buildReply(err)
}

func (r *ClusterDeploymentsReconciler) shouldDeleteAgentOnUnbind(ctx context.Context, agent aiv1beta1.Agent, clusterDeployment types.NamespacedName) bool {
	log := logutil.FromContext(ctx, r.Log).WithFields(logrus.Fields{
		"cluster_deployment":           clusterDeployment.Name,
		"cluster_deployment_namespace": clusterDeployment.Namespace,
	})
	infraEnvName, ok := agent.Labels[aiv1beta1.InfraEnvNameLabel]
	if !ok {
		log.Errorf("Failed to find infraEnv name for agent %s in namespace %s", agent.Name, agent.Namespace)
		return false
	}

	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Get(ctx, types.NamespacedName{Name: infraEnvName, Namespace: agent.Namespace}, infraEnv); err != nil {
		log.Errorf("Failed to get infraEnv %s in namespace %s", infraEnvName, agent.Namespace)
		return false
	}

	if infraEnv.Spec.ClusterRef != nil &&
		infraEnv.Spec.ClusterRef.Name == clusterDeployment.Name &&
		infraEnv.Spec.ClusterRef.Namespace == clusterDeployment.Namespace {

		return true
	}

	return false
}

func (r *ClusterDeploymentsReconciler) unbindAgents(ctx context.Context, log logrus.FieldLogger, clusterDeployment types.NamespacedName) error {
	agents := &aiv1beta1.AgentList{}
	log = log.WithFields(logrus.Fields{"clusterDeployment": clusterDeployment.Name, "namespace": clusterDeployment.Namespace})
	if err := r.List(ctx, agents); err != nil {
		return err
	}
	for i, clusterAgent := range agents.Items {
		if clusterAgent.Spec.ClusterDeploymentName != nil &&
			clusterAgent.Spec.ClusterDeploymentName.Name == clusterDeployment.Name &&
			clusterAgent.Spec.ClusterDeploymentName.Namespace == clusterDeployment.Namespace {
			if r.shouldDeleteAgentOnUnbind(ctx, clusterAgent, clusterDeployment) {
				log.Infof("deleting agent %s in namespace %s", clusterAgent.Name, clusterAgent.Namespace)
				if err := r.Delete(ctx, &agents.Items[i]); err != nil {
					log.WithError(err).Errorf("failed to delete agent %s in namespace %s", clusterAgent.Name, clusterAgent.Namespace)
					return err
				}
			} else {
				log.Infof("unbind agent %s namespace %s", clusterAgent.Name, clusterAgent.Namespace)
				agents.Items[i].Spec.ClusterDeploymentName = nil
				if err := r.Update(ctx, &agents.Items[i]); err != nil {
					log.WithError(err).Errorf("failed to add unbind resource %s %s", clusterAgent.Name, clusterAgent.Namespace)
					return err
				}
			}
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapSecretToClusterDeployment := func(a client.Object) []reconcile.Request {
		clusterDeployments := &hivev1.ClusterDeploymentList{}

		// PullSecretRef is a LocalObjectReference, which means it must exist in the
		// same namespace. Let's get only the ClusterDeployments from the Secret's
		// namespace.
		if err := r.List(context.Background(), clusterDeployments, &client.ListOptions{Namespace: a.GetNamespace()}); err != nil {
			// TODO: silently ignoring error here
			return []reconcile.Request{}
		}
		reply := make([]reconcile.Request, 0, len(clusterDeployments.Items))
		for _, clusterDeployment := range clusterDeployments.Items {
			if clusterDeployment.Spec.PullSecretRef != nil && clusterDeployment.Spec.PullSecretRef.Name == a.GetName() {
				reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: clusterDeployment.Namespace,
					Name:      clusterDeployment.Name,
				}})
			}
		}
		return reply
	}

	// Reconcile the ClusterDeployment referenced by this AgentClusterInstall.
	mapClusterInstallToClusterDeployment := func(a client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"agent_cluster_install":           a.GetName(),
				"agent_cluster_install_namespace": a.GetNamespace(),
			})
		aci, ok := a.(*hiveext.AgentClusterInstall)
		if !ok {
			log.Errorf("%v was not an AgentClusterInstall", a) // shouldn't be possible
			return []reconcile.Request{}
		}
		log.Debugf("Map ACI : %s %s CD ref name %s", aci.Namespace, aci.Name, aci.Spec.ClusterDeploymentRef.Name)
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: aci.Namespace,
					Name:      aci.Spec.ClusterDeploymentRef.Name,
				},
			},
		}
	}

	mapAgentToClusterDeployment := func(a client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"agent":           a.GetName(),
				"agent_namespace": a.GetNamespace(),
			})
		agent, ok := a.(*aiv1beta1.Agent)
		if !ok {
			log.Errorf("%v was not an Agent", a) // shouldn't be possible
			return []reconcile.Request{}
		}
		if agent.Spec.ClusterDeploymentName != nil {
			log.Debugf("Map Agent : %s %s CD ref name %s ns %s", agent.Namespace, agent.Name, agent.Spec.ClusterDeploymentName.Name, agent.Spec.ClusterDeploymentName.Namespace)
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: agent.Spec.ClusterDeploymentName.Namespace,
						Name:      agent.Spec.ClusterDeploymentName.Name,
					},
				},
			}
		} else {
			return []reconcile.Request{}
		}
	}

	agentSpecStatusChangedPredicate := builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			oldAgent, ok := updateEvent.ObjectOld.(*aiv1beta1.Agent)
			if !ok {
				return false
			}

			newAgent, ok := updateEvent.ObjectNew.(*aiv1beta1.Agent)
			if !ok {
				return false
			}

			// For updates of Agent we will compare the SyncStatus before and after update and
			// proceed to reconciliation only if the status changed
			var oldSyncStatus corev1.ConditionStatus
			var newSyncStatus corev1.ConditionStatus

			for _, condition := range oldAgent.Status.Conditions {
				if condition.Reason == string(aiv1beta1.SpecSyncedCondition) {
					oldSyncStatus = condition.Status
				}
			}
			for _, condition := range newAgent.Status.Conditions {
				if condition.Reason == string(aiv1beta1.SpecSyncedCondition) {
					newSyncStatus = condition.Status
				}
			}

			return oldSyncStatus != newSyncStatus
		},
	})

	clusterDeploymentUpdates := r.CRDEventsHandler.GetClusterDeploymentUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(mapSecretToClusterDeployment)).
		Watches(&source.Kind{Type: &hiveext.AgentClusterInstall{}}, handler.EnqueueRequestsFromMapFunc(mapClusterInstallToClusterDeployment)).
		Watches(&source.Kind{Type: &aiv1beta1.Agent{}},
			handler.EnqueueRequestsFromMapFunc(mapAgentToClusterDeployment),
			agentSpecStatusChangedPredicate).
		Watches(&source.Channel{Source: clusterDeploymentUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// updateStatus is updating all the AgentClusterInstall Conditions.
// In case that an error has occurred when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *ClusterDeploymentsReconciler) updateStatus(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster, syncErr error) (ctrl.Result, error) {
	clusterSpecSynced(clusterInstall, syncErr)
	if c != nil {
		clusterInstall.Status.ConnectivityMajorityGroups = c.ConnectivityMajorityGroups
		clusterInstall.Status.MachineNetwork = machineNetworksArrayToEntries(c.MachineNetworks)

		if c.Status != nil {
			clusterInstall.Status.DebugInfo.State = swag.StringValue(c.Status)
			clusterInstall.Status.DebugInfo.StateInfo = swag.StringValue(c.StatusInfo)
			if c.Progress == nil {
				clusterInstall.Status.Progress = hiveext.ClusterProgressInfo{}
			} else {
				clusterInstall.Status.Progress.TotalPercentage = c.Progress.TotalPercentage
			}
			clusterInstall.Status.APIVIP = c.APIVip
			clusterInstall.Status.IngressVIP = c.IngressVip
			clusterInstall.Status.APIVIPs = apiVipsArrayToStrings(c.APIVips)
			clusterInstall.Status.IngressVIPs = ingressVipsArrayToStrings(c.IngressVips)
			clusterInstall.Status.UserManagedNetworking = c.UserManagedNetworking
			clusterInstall.Status.PlatformType = getPlatformType(c.Platform)
			status := *c.Status
			var err error
			err = r.populateEventsURL(log, clusterInstall, c)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
			err = r.populateLogsURL(ctx, log, clusterInstall, c)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
			var registeredHosts, approvedHosts, unsyncedHosts int
			if status == models.ClusterStatusReady {
				registeredHosts, approvedHosts, err = r.getNumOfClusterAgents(c)
				if err != nil {
					log.WithError(err).Error("failed to fetch cluster's agents")
					return ctrl.Result{Requeue: true}, nil
				}
				agents, err := findAgentsByAgentClusterInstall(r.Client, ctx, log, clusterInstall)
				if err != nil {
					log.WithError(err).Error("failed to fetch ACI's agents")
					return ctrl.Result{Requeue: true}, nil
				}
				unsyncedHosts = getNumOfUnsyncedAgents(agents)
				log.Debugf("Updating ACI conditions, found %d unsynced agents out of total of %d agents", unsyncedHosts, len(agents))
			}
			clusterRequirementsMet(clusterInstall, status, registeredHosts, approvedHosts, unsyncedHosts)
			clusterValidated(clusterInstall, status, c)
			clusterCompleted(clusterInstall, status, swag.StringValue(c.StatusInfo), c.MonitoredOperators)
			clusterFailed(clusterInstall, status, swag.StringValue(c.StatusInfo))
			clusterStopped(clusterInstall, status)
		}

		if c.ValidationsInfo != "" {
			newValidationsInfo := ValidationsStatus{}
			err := json.Unmarshal([]byte(c.ValidationsInfo), &newValidationsInfo)
			if err != nil {
				log.WithError(err).Error("failed to umarshed ValidationsInfo")
				return ctrl.Result{}, err
			}
			clusterInstall.Status.ValidationsInfo = newValidationsInfo
		}
	} else {
		setClusterConditionsUnknown(clusterInstall)
	}

	if updateErr := r.Status().Update(ctx, clusterInstall); updateErr != nil {
		log.WithError(updateErr).Error("failed to update ClusterDeployment Status")
		return ctrl.Result{Requeue: true}, nil
	}
	if syncErr != nil && !IsUserError(syncErr) {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ClusterDeploymentsReconciler) populateEventsURL(log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) error {
	if *c.Status != models.ClusterStatusInstalled {
		if clusterInstall.Status.DebugInfo.EventsURL == "" {
			tokenGen := gencrypto.CryptoPair{JWTKeyType: gencrypto.ClusterKey, JWTKeyValue: c.ID.String()}
			eventUrl, err := generateEventsURL(r.ServiceBaseURL, r.AuthType, tokenGen, "cluster_id", c.ID.String())
			if err != nil {
				log.WithError(err).Error("failed to generate Events URL")
				return err
			}
			clusterInstall.Status.DebugInfo.EventsURL = eventUrl
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) populateLogsURL(ctx context.Context, log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) error {
	if swag.StringValue(c.Status) != models.ClusterStatusInstalled {
		if err := r.setControllerLogsDownloadURL(ctx, log, clusterInstall, c); err != nil {
			log.WithError(err).Error("failed to generate controller logs URL")
			return err
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) getNumOfClusterAgents(c *common.Cluster) (int, int, error) {
	return r.Installer.GetKnownHostApprovedCounts(*c.ID)
}

// Finds the agents related to provided AgentClusterInstall
//
// The AgentClusterInstall <-> Agent relation is one-to-many.
// Thsi function returns all Agents whose ClusterDeploymentName name
// matches the ClusterDeploymentRef name in the provided ACI.
func findAgentsByAgentClusterInstall(k8sclient client.Client, ctx context.Context, log logrus.FieldLogger, aci *hiveext.AgentClusterInstall) ([]*aiv1beta1.Agent, error) {
	agentList := aiv1beta1.AgentList{}
	agents := []*aiv1beta1.Agent{}
	err := k8sclient.List(ctx, &agentList, client.MatchingLabels{AgentLabelClusterDeploymentNamespace: aci.Namespace})

	if err != nil {
		return nil, err
	}

	for i := range agentList.Items {
		agent := &agentList.Items[i]
		if agent.Spec.ClusterDeploymentName == nil {
			continue
		}
		if agent.Spec.ClusterDeploymentName.Name != aci.Spec.ClusterDeploymentRef.Name {
			continue
		} else {
			agents = append(agents, agent)
		}
	}
	log.Debugf("Found %d agents matching ClusterDeployment %s", len(agents), aci.Spec.ClusterDeploymentRef.Name)

	for _, x := range agents {
		log.Debugf("Agent '%s', dumping below:", x.Name)
		log.Debugf("Ignition: '%s'", x.Spec.IgnitionConfigOverrides)
		for _, y := range x.Status.Conditions {
			log.Debugf("Type: '%s', Status: '%s', Reason: '%s', Message: '%s'", y.Type, y.Status, y.Reason, y.Message)
		}
	}

	return agents, nil
}

func getNumOfUnsyncedAgents(agents []*aiv1beta1.Agent) int {
	num := 0
	for _, agent := range agents {
		for _, cond := range agent.Status.Conditions {
			if cond.Type == aiv1beta1.SpecSyncedCondition && cond.Status == corev1.ConditionFalse {
				num = num + 1
				continue
			}
		}
	}

	return num
}

// clusterSpecSynced is updating the Cluster SpecSynced Condition.
func clusterSpecSynced(cluster *hiveext.AgentClusterInstall, syncErr error) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	if syncErr == nil {
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterSyncedOkReason
		msg = hiveext.ClusterSyncedOkReason
	} else {
		condStatus = corev1.ConditionFalse
		if !IsUserError(syncErr) {
			reason = hiveext.ClusterBackendErrorReason
			msg = hiveext.ClusterBackendErrorMsg + " " + syncErr.Error()
		} else {
			reason = hiveext.ClusterInputErrorReason
			msg = hiveext.ClusterInputErrorMsg + " " + syncErr.Error()
		}
	}
	setClusterCondition(&cluster.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterSpecSyncedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterRequirementsMet(clusterInstall *hiveext.AgentClusterInstall, status string, registeredHosts, approvedHosts, unsyncedHosts int) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string

	switch status {
	case models.ClusterStatusReady:
		expectedHosts := clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents +
			clusterInstall.Spec.ProvisionRequirements.WorkerAgents
		if registeredHosts < expectedHosts {
			condStatus = corev1.ConditionFalse
			reason = hiveext.ClusterInsufficientAgentsReason
			msg = fmt.Sprintf(hiveext.ClusterInsufficientAgentsMsg, expectedHosts, approvedHosts)
		} else if unsyncedHosts != 0 {
			condStatus = corev1.ConditionFalse
			reason = hiveext.ClusterUnsyncedAgentsReason
			msg = fmt.Sprintf(hiveext.ClusterUnsyncedAgentsMsg, unsyncedHosts)
		} else if approvedHosts < expectedHosts {
			condStatus = corev1.ConditionFalse
			reason = hiveext.ClusterUnapprovedAgentsReason
			msg = fmt.Sprintf(hiveext.ClusterUnapprovedAgentsMsg, expectedHosts-approvedHosts)
		} else if registeredHosts > expectedHosts {
			condStatus = corev1.ConditionFalse
			reason = hiveext.ClusterAdditionalAgentsReason
			msg = fmt.Sprintf(hiveext.ClusterAdditionalAgentsMsg, expectedHosts, registeredHosts)
		} else {
			condStatus = corev1.ConditionTrue
			reason = hiveext.ClusterReadyReason
			msg = hiveext.ClusterReadyMsg
		}
	case models.ClusterStatusInsufficient, models.ClusterStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterNotReadyReason
		msg = hiveext.ClusterNotReadyMsg
	case models.ClusterStatusPreparingForInstallation,
		models.ClusterStatusInstalling, models.ClusterStatusInstallingPendingUserAction,
		models.ClusterStatusAddingHosts, models.ClusterStatusFinalizing:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterAlreadyInstallingReason
		msg = hiveext.ClusterAlreadyInstallingMsg
	case models.ClusterStatusInstalled, models.ClusterStatusError:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterInstallationStoppedReason
		msg = hiveext.ClusterInstallationStoppedMsg
	default:
		condStatus = corev1.ConditionUnknown
		reason = hiveext.ClusterUnknownStatusReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterUnknownStatusMsg, status)
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterRequirementsMetCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterCompleted(clusterInstall *hiveext.AgentClusterInstall, status, statusInfo string, opers []*models.MonitoredOperator) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	var cvoMsg string
	for _, op := range opers {
		if op.Name == operators.OperatorCVO.Name {
			if op.Status != "" {
				cvoMsg = fmt.Sprintf(". Cluster version status: %s, message: %s", op.Status, op.StatusInfo)
			}
		}
	}
	switch status {
	case models.ClusterStatusInstalled, models.ClusterStatusAddingHosts:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterInstalledReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterInstalledMsg, statusInfo)
	case models.ClusterStatusError:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterInstallationFailedReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterInstallationFailedMsg, statusInfo)
	case models.ClusterStatusReady:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterInstallationNotStartedReason
		msg = hiveext.ClusterInstallationNotStartedMsg
		if clusterInstall.Spec.HoldInstallation {
			reason = hiveext.ClusterInstallationOnHoldReason
			msg = hiveext.ClusterInstallationOnHoldMsg
		}
	case models.ClusterStatusInsufficient, models.ClusterStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterInstallationNotStartedReason
		msg = hiveext.ClusterInstallationNotStartedMsg
	case models.ClusterStatusPreparingForInstallation, models.ClusterStatusInstalling, models.ClusterStatusFinalizing,
		models.ClusterStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterInstallationInProgressReason
		msg = fmt.Sprintf("%s %s%s", hiveext.ClusterInstallationInProgressMsg, statusInfo, cvoMsg)
	default:
		condStatus = corev1.ConditionUnknown
		reason = hiveext.ClusterUnknownStatusReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterUnknownStatusMsg, status)
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterCompletedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterFailed(clusterInstall *hiveext.AgentClusterInstall, status, statusInfo string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.ClusterStatusError:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterFailedReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterFailedMsg, statusInfo)
	default:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterNotFailedReason
		msg = hiveext.ClusterNotFailedMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterFailedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterStopped(clusterInstall *hiveext.AgentClusterInstall, status string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.ClusterStatusError:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterStoppedFailedReason
		msg = hiveext.ClusterStoppedFailedMsg
	case models.ClusterStatusCancelled:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterStoppedCanceledReason
		msg = hiveext.ClusterStoppedCanceledMsg
	case models.ClusterStatusInstalled, models.ClusterStatusAddingHosts:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterStoppedCompletedReason
		msg = hiveext.ClusterStoppedCompletedMsg
	default:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterNotStoppedReason
		msg = hiveext.ClusterNotStoppedMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterStoppedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterValidated(clusterInstall *hiveext.AgentClusterInstall, status string, c *common.Cluster) {
	failedValidationInfo := ""
	validationRes, err := cluster.GetValidations(c)
	var failures []string
	if err == nil {
		for _, vRes := range validationRes {
			for _, v := range vRes {
				if v.Status != cluster.ValidationSuccess {
					failures = append(failures, v.Message)
				}
			}
		}
		failedValidationInfo = strings.Join(failures[:], ",")
	}
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch {
	case models.ClusterStatusInsufficient == status:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterValidationsFailingReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterValidationsFailingMsg, failedValidationInfo)
	case models.ClusterStatusPendingForInput == status:
		condStatus = corev1.ConditionFalse
		reason = hiveext.ClusterValidationsUserPendingReason
		msg = fmt.Sprintf("%s %s", hiveext.ClusterValidationsUserPendingMsg, failedValidationInfo)
	case models.ClusterStatusAddingHosts == status:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterValidationsPassingReason
		msg = hiveext.ClusterValidationsOKMsg
	case c.ValidationsInfo == "":
		condStatus = corev1.ConditionUnknown
		reason = hiveext.ClusterValidationsUnknownReason
		msg = hiveext.ClusterValidationsUnknownMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = hiveext.ClusterValidationsPassingReason
		msg = hiveext.ClusterValidationsOKMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterValidatedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func setClusterConditionsUnknown(clusterInstall *hiveext.AgentClusterInstall) {
	clusterInstall.Status.DebugInfo.State = ""
	clusterInstall.Status.DebugInfo.StateInfo = ""
	clusterInstall.Status.DebugInfo.LogsURL = ""
	clusterInstall.Status.DebugInfo.EventsURL = ""
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterValidatedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  hiveext.ClusterNotAvailableReason,
		Message: hiveext.ClusterNotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterRequirementsMetCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  hiveext.ClusterNotAvailableReason,
		Message: hiveext.ClusterNotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterCompletedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  hiveext.ClusterNotAvailableReason,
		Message: hiveext.ClusterNotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterFailedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  hiveext.ClusterNotAvailableReason,
		Message: hiveext.ClusterNotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    hiveext.ClusterStoppedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  hiveext.ClusterNotAvailableReason,
		Message: hiveext.ClusterNotAvailableMsg,
	})
}

// SetStatusCondition sets the corresponding condition in conditions to newCondition.
func setClusterCondition(conditions *[]hivev1.ClusterInstallCondition, newCondition hivev1.ClusterInstallCondition) {
	if conditions == nil {
		conditions = &[]hivev1.ClusterInstallCondition{}
	}
	existingCondition := FindStatusCondition(*conditions, newCondition.Type)
	if existingCondition == nil {
		newCondition.LastTransitionTime = metav1.NewTime(time.Now())
		newCondition.LastProbeTime = metav1.NewTime(time.Now())
		*conditions = append(*conditions, newCondition)
		return
	}

	if !isConditionEqual(*existingCondition, newCondition) {
		existingCondition.Status = newCondition.Status
		existingCondition.Reason = newCondition.Reason
		existingCondition.Message = newCondition.Message
		existingCondition.LastTransitionTime = metav1.NewTime(time.Now())
		existingCondition.LastProbeTime = metav1.NewTime(time.Now())
	}
}

func isConditionEqual(existingCond hivev1.ClusterInstallCondition, newCondition hivev1.ClusterInstallCondition) bool {
	if existingCond.Type == newCondition.Type {
		return existingCond.Status == newCondition.Status &&
			existingCond.Reason == newCondition.Reason &&
			existingCond.Message == newCondition.Message
	}
	return false
}

// FindStatusCondition finds the conditionType in conditions.
func FindStatusCondition(conditions []hivev1.ClusterInstallCondition, conditionType string) *hivev1.ClusterInstallCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}

// ensureOwnerRef sets the owner reference of ClusterDeployment on AgentClusterInstall
func (r *ClusterDeploymentsReconciler) ensureOwnerRef(ctx context.Context, log logrus.FieldLogger, cd *hivev1.ClusterDeployment, ci *hiveext.AgentClusterInstall) error {

	if err := controllerutil.SetOwnerReference(cd, ci, r.Scheme); err != nil {
		log.WithError(err).Error("error setting owner reference Agent Cluster Install")
		return err
	}
	return r.Update(ctx, ci)
}

func (r *ClusterDeploymentsReconciler) areLogsCollected(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) (bool, error) {
	if !time.Time(cluster.ControllerLogsCollectedAt).Equal(time.Time{}) { // timestamp update, meaning logs were collected from a controller
		return true, nil
	}
	return r.Installer.HostWithCollectedLogsExists(*cluster.ID)
}

func (r *ClusterDeploymentsReconciler) setControllerLogsDownloadURL(
	ctx context.Context,
	log logrus.FieldLogger,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {
	if clusterInstall.Status.DebugInfo.LogsURL != "" {
		return nil
	}
	logsCollected, err := r.areLogsCollected(ctx, log, cluster)
	if err != nil {
		return err
	}
	if !logsCollected {
		return nil
	}
	logsUrl, err := r.generateControllerLogsDownloadURL(cluster)
	if err != nil {
		return err
	}
	clusterInstall.Status.DebugInfo.LogsURL = logsUrl

	return nil
}

func (r *ClusterDeploymentsReconciler) generateControllerLogsDownloadURL(cluster *common.Cluster) (string, error) {
	downloadURL := fmt.Sprintf("%s%s/v2/clusters/%s/logs",
		r.ServiceBaseURL, restclient.DefaultBasePath, cluster.ID.String())

	if r.AuthType != auth.TypeLocal {
		return downloadURL, nil
	}

	var err error
	downloadURL, err = gencrypto.SignURL(downloadURL, cluster.ID.String(), gencrypto.ClusterKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to sign cluster controller logs URL")
	}

	return downloadURL, nil
}

func (r *ClusterDeploymentsReconciler) handleClusterInstalled(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster, clusterInstall *hiveext.AgentClusterInstall, key types.NamespacedName) (ctrl.Result, error) {
	var err error
	if !isInstalled(clusterDeployment, clusterInstall) {
		// create secrets and update status
		err = r.updateClusterMetadata(ctx, log, clusterDeployment, cluster, clusterInstall)
		if err != nil {
			log.WithError(err).Error("failed to update cluster metadata")
		}
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}
	return r.TransformClusterToDay2(ctx, log, cluster, clusterInstall)
}

func getClusterDeploymentAdminKubeConfigSecretName(cd *hivev1.ClusterDeployment) string {
	if cd.Spec.ClusterMetadata != nil && cd.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name != "" {
		return cd.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name
	}
	return fmt.Sprintf(adminKubeConfigStringTemplate, cd.Name)
}
