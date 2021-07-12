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
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	restclient "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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
	Log               logrus.FieldLogger
	Scheme            *runtime.Scheme
	Installer         bminventory.InstallerInternals
	ClusterApi        cluster.API
	HostApi           host.API
	CRDEventsHandler  CRDEventsHandler
	Manifests         manifests.ClusterManifestsInternals
	ServiceBaseURL    string
	AuthType          auth.AuthType
	EnableDay2Cluster bool
}

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
		if !r.isSNO(clusterInstall) && r.EnableDay2Cluster {
			return r.createNewDay2Cluster(ctx, log, req.NamespacedName, clusterDeployment, clusterInstall)
		}
		// cluster is installed and SNO nothing to do.
		return ctrl.Result{Requeue: false}, nil
	}
	if err != nil {
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	// check for updates from user, compare spec and update if needed
	err = r.updateIfNeeded(ctx, log, clusterDeployment, clusterInstall, cluster)
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
		if !isInstalled(clusterDeployment, clusterInstall) {
			// create secrets and update status
			err = r.updateClusterMetadata(ctx, log, clusterDeployment, cluster, clusterInstall)
			if err != nil {
				log.WithError(err).Error("failed to update cluster metadata")
			}
			return r.updateStatus(ctx, log, clusterInstall, cluster, err)
		} else if r.EnableDay2Cluster && !r.isSNO(clusterInstall) {
			// Delete Day1 Cluster
			_, err = r.deregisterClusterIfNeeded(ctx, log, req.NamespacedName)
			if err != nil {
				log.WithError(err).Error("failed to deregister cluster")
				return r.updateStatus(ctx, log, clusterInstall, cluster, err)
			}
			//Create Day2 cluster
			return r.createNewDay2Cluster(ctx, log, req.NamespacedName, clusterDeployment, clusterInstall)
		}
		return r.updateStatus(ctx, log, clusterInstall, cluster, err)
	}

	// Create Kubeconfig no-ingress if needed
	if *cluster.Status == models.ClusterStatusInstalling || *cluster.Status == models.ClusterStatusFinalizing {
		if err := r.createNoIngressKubeConfig(ctx, log, clusterDeployment, cluster, clusterInstall); err != nil {
			log.WithError(err).Error("failed to create kubeconfig no-ingress secret")
			return r.updateStatus(ctx, log, clusterInstall, cluster, err)
		}
	}

	if swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		// Day 1
		return r.installDay1(ctx, log, clusterDeployment, clusterInstall, cluster)

	} else if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		// Day 2
		return r.installDay2Hosts(ctx, log, clusterDeployment, clusterInstall, cluster)
	}

	return r.updateStatus(ctx, log, clusterInstall, cluster, nil)
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
				if swag.StringValue(cluster.Status) == models.ClusterStatusInstalling {
					log.Infof("ClusterInstall is being deleted, cancel installation for cluster %s", *cluster.ID)
					if _, err = r.Installer.CancelInstallationInternal(ctx, installer.CancelInstallationParams{
						ClusterID: *cluster.ID,
					}); err != nil {
						return &ctrl.Result{Requeue: true}, err
					}
				}
			}
			// deletion finalizer found, deregister the backend cluster and delete agents
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
	clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) (ctrl.Result, error) {
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
		ic, err = r.Installer.InstallClusterInternal(ctx, installer.InstallClusterParams{
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

func (r *ClusterDeploymentsReconciler) installDay2Hosts(ctx context.Context, log logrus.FieldLogger, clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) (ctrl.Result, error) {

	for _, h := range cluster.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, cluster.ID.String(), h.ID.String())
		if err != nil {
			log.WithError(err).Errorf("Failed to get common host %s from cluster %s", h.ID.String(), cluster.ID.String())
			return r.updateStatus(ctx, log, clusterInstall, cluster, err)
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			log.Infof("Installing Day2 host %s in %s %s", *h.ID, clusterDeployment.Name, clusterDeployment.Namespace)
			err = r.Installer.InstallSingleDay2HostInternal(ctx, *cluster.ID, *h.ID)
			if err != nil {
				return r.updateStatus(ctx, log, clusterInstall, cluster, err)
			}
		}
	}
	return r.updateStatus(ctx, log, clusterInstall, cluster, nil)
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
		AdminPasswordSecretRef: corev1.LocalObjectReference{
			Name: s.Name,
		},
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: k.Name,
		},
	}
	return r.Update(ctx, clusterInstall)
}

func (r *ClusterDeploymentsReconciler) ensureAdminPasswordSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminPasswordSecretStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	cred, err := r.Installer.GetCredentialsInternal(ctx, installer.GetCredentialsParams{
		ClusterID: *c.ID,
	})
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"username": []byte(cred.Username),
		"password": []byte(cred.Password),
	}
	return r.createClusterCredentialSecret(ctx, log, cluster, c, name, data, "kubeadmincreds")
}

func (r *ClusterDeploymentsReconciler) updateKubeConfigSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr != nil && !k8serrors.IsNotFound(getErr) {
		return nil, getErr
	}

	respBody, _, err := r.Installer.DownloadClusterFilesInternal(ctx, installer.DownloadClusterFilesParams{
		ClusterID: *c.ID,
		FileName:  constants.Kubeconfig,
	})
	if err != nil {
		return nil, err
	}
	respBytes, err := ioutil.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"kubeconfig": respBytes,
	}
	if k8serrors.IsNotFound(getErr) {
		return r.createClusterCredentialSecret(ctx, log, cluster, c, name, data, "kubeconfig")
	}
	s.Data = data
	return s, r.Update(ctx, s)
}

func (r *ClusterDeploymentsReconciler) ensureKubeConfigNoIngressSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	respBody, _, err := r.Installer.DownloadClusterFilesInternal(ctx, installer.DownloadClusterFilesParams{
		ClusterID: *c.ID,
		FileName:  constants.KubeconfigNoIngress,
	})
	if err != nil {
		return nil, err
	}
	respBytes, err := ioutil.ReadAll(respBody)
	if err != nil {
		return nil, err
	}
	data := map[string][]byte{
		"kubeconfig": respBytes,
	}

	return r.createClusterCredentialSecret(ctx, log, cluster, c, name, data, "kubeconfig")
}

func (r *ClusterDeploymentsReconciler) createClusterCredentialSecret(ctx context.Context, log logrus.FieldLogger, cluster *hivev1.ClusterDeployment, c *common.Cluster, name string, data map[string][]byte, secretType string) (*corev1.Secret, error) {
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

	registered, approvedHosts, err := r.getNumOfClusterAgents(ctx, clusterInstall, c)
	if err != nil {
		log.WithError(err).Error("failed to fetch agents")
		return false, err
	}

	expectedHosts := clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents +
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents
	return approvedHosts == expectedHosts && registered == approvedHosts, nil
}

func isSupportedPlatform(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.ClusterInstallRef == nil ||
		cluster.Spec.ClusterInstallRef.Group != hiveext.Group ||
		cluster.Spec.ClusterInstallRef.Kind != "AgentClusterInstall" {
		return false
	}
	return true
}

func isUserManagedNetwork(clusterInstall *hiveext.AgentClusterInstall) bool {
	if clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0 {
		return true
	}
	return false
}

//see https://docs.openshift.com/container-platform/4.7/installing/installing_platform_agnostic/installing-platform-agnostic.html#installation-bare-metal-config-yaml_installing-platform-agnostic
func hyperthreadingInSpec(clusterInstall *hiveext.AgentClusterInstall) bool {
	//check if either master or worker pool hyperthreading settings are explicitly specified
	return clusterInstall.Spec.ControlPlane != nil ||
		funk.Contains(clusterInstall.Spec.Compute, func(pool hiveext.AgentMachinePool) bool {
			return pool.Name == hiveext.WorkerAgentMachinePool
		})
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

func (r *ClusterDeploymentsReconciler) updateIfNeeded(ctx context.Context,
	log logrus.FieldLogger,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {

	update := false
	notifyInfraEnv := false
	var infraEnv *aiv1beta1.InfraEnv

	params := &models.ClusterUpdateParams{}

	spec := clusterDeployment.Spec
	updateString := func(new, old string, target **string) {
		if new != old {
			*target = swag.String(new)
			update = true
		}
	}

	updateString(spec.ClusterName, cluster.Name, &params.Name)
	updateString(spec.BaseDomain, cluster.BaseDNSDomain, &params.BaseDNSDomain)

	if len(clusterInstall.Spec.Networking.ClusterNetwork) > 0 {
		updateString(clusterInstall.Spec.Networking.ClusterNetwork[0].CIDR, cluster.ClusterNetworkCidr, &params.ClusterNetworkCidr)
		hostPrefix := int64(clusterInstall.Spec.Networking.ClusterNetwork[0].HostPrefix)
		if hostPrefix > 0 && hostPrefix != cluster.ClusterNetworkHostPrefix {
			params.ClusterNetworkHostPrefix = swag.Int64(hostPrefix)
			update = true
		}
	}
	if len(clusterInstall.Spec.Networking.ServiceNetwork) > 0 {
		updateString(clusterInstall.Spec.Networking.ServiceNetwork[0], cluster.ServiceNetworkCidr, &params.ServiceNetworkCidr)
	}
	if len(clusterInstall.Spec.Networking.MachineNetwork) > 0 {
		updateString(clusterInstall.Spec.Networking.MachineNetwork[0].CIDR, cluster.MachineNetworkCidr, &params.MachineNetworkCidr)
	}

	// Update APIVIP and IngressVIP only if cluster is not SNO or VipDhcpAllocation is not enabled
	// In absence of this check, the reconcile loop in the controller fails all the time
	isDHCPEnabled := swag.BoolValue(cluster.VipDhcpAllocation)
	isSNO := common.IsSingleNodeCluster(cluster)

	if !isSNO && !isDHCPEnabled {
		updateString(clusterInstall.Spec.APIVIP, cluster.APIVip, &params.APIVip)
		updateString(clusterInstall.Spec.IngressVIP, cluster.IngressVip, &params.IngressVip)
	}

	// Trim key before comapring as done in RegisterClusterInternal
	sshPublicKey := strings.TrimSpace(clusterInstall.Spec.SSHPublicKey)
	updateString(sshPublicKey, cluster.SSHPublicKey, &params.SSHPublicKey)

	if userManagedNetwork := isUserManagedNetwork(clusterInstall); userManagedNetwork != swag.BoolValue(cluster.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(userManagedNetwork)
	}
	pullSecretData, err := getPullSecret(ctx, r.Client, spec.PullSecretRef, clusterDeployment.Namespace)
	if err != nil {
		return errors.Wrap(err, "failed to get pull secret for update")
	}
	// TODO: change isInfraEnvUpdate to false, once clusterDeployment pull-secret can differ from infraEnv
	if pullSecretData != cluster.PullSecret {
		params.PullSecret = swag.String(pullSecretData)
		update = true
		notifyInfraEnv = true
	}

	// update hyperthreading settings
	hyperthreading := getHyperthreading(clusterInstall)
	if cluster.Hyperthreading != *hyperthreading {
		params.Hyperthreading = hyperthreading
		update = true
	}

	if !update {
		return nil
	}
	_, err = r.Installer.UpdateClusterNonInteractive(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *cluster.ID,
	})
	if err != nil {
		return err
	}

	infraEnv, err = getInfraEnvByClusterDeployment(ctx, log, r.Client, clusterDeployment.Name, clusterDeployment.Namespace)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to search for infraEnv for clusterDeployment %s", clusterDeployment.Name))
	}

	log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
	if notifyInfraEnv && infraEnv != nil {
		log.Infof("Notify that infraEnv %s should re-generate the image for clusterDeployment %s", infraEnv.Name, clusterDeployment.ClusterName)
		r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	}
	return nil
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
		_, err := r.Installer.UpdateClusterInstallConfigInternal(ctx, installer.UpdateClusterInstallConfigParams{
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
			_ = r.Manifests.DeleteClusterManifestInternal(ctx, operations.DeleteClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  manifest.FileName,
				Folder:    swag.String(models.ManifestFolderOpenshift),
			})
		}
	}

	// create/update all manifests provided by configmap data
	for filename, manifest := range manifestsFromConfigMap {
		log.Infof("Creating cluster deployment %s manifest %s", cluster.KubeKeyName, filename)
		_, err := r.Manifests.CreateClusterManifestInternal(ctx, operations.CreateClusterManifestParams{
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
	configuredManifests := &corev1.ConfigMap{}
	configuredManifests.Data = map[string]string{}
	// get manifests from configmap if we have reference for it
	if clusterInstall.Spec.ManifestsConfigMapRef != nil {
		err := r.Get(ctx, types.NamespacedName{Namespace: clusterInstall.Namespace,
			Name: clusterInstall.Spec.ManifestsConfigMapRef.Name}, configuredManifests)
		if err != nil {
			log.WithError(err).Errorf("Failed to get configmap %s in %s",
				clusterInstall.Spec.ManifestsConfigMapRef.Name, clusterInstall.Namespace)
			return nil, err
		}
	}
	return configuredManifests.Data, nil
}

func (r *ClusterDeploymentsReconciler) addCustomManifests(ctx context.Context, log logrus.FieldLogger,
	clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) error {

	alreadyCreatedManifests, err := r.Manifests.ListClusterManifestsInternal(ctx, operations.ListClusterManifestsParams{
		ClusterID: *cluster.ID,
	})
	if err != nil {
		log.WithError(err).Errorf("Failed to list manifests for %q cluster install", clusterInstall.Name)
		return err
	}

	// if reference to manifests was deleted from cluster deployment
	// but we already added some in previous reconcile loop, we want to clean them.
	// if no reference were provided we will delete all manifests that were in the list
	if clusterInstall.Spec.ManifestsConfigMapRef == nil && len(alreadyCreatedManifests) == 0 {
		log.Debugf("Nothing to do, skipping manifest creation")
		return nil
	}

	return r.syncManifests(ctx, log, cluster, clusterInstall, alreadyCreatedManifests)
}

func (r *ClusterDeploymentsReconciler) isSNO(clusterInstall *hiveext.AgentClusterInstall) bool {
	return clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	log logrus.FieldLogger,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {

	var infraEnv *aiv1beta1.InfraEnv

	log.Infof("Creating a new cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef, key.Namespace)
	if err != nil {
		log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, log, clusterInstall, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, clusterInstall.Spec, pullSecret)
	if err != nil {
		log.WithError(err).Error("failed to add OCP version")
		_, _ = r.updateStatus(ctx, log, clusterInstall, nil, err)
		// The controller will requeue after one minute, giving the user a chance to fix releaseImage
		return ctrl.Result{Requeue: true, RequeueAfter: longerRequeueAfterOnError}, nil
	}

	clusterParams := &models.ClusterCreateParams{
		BaseDNSDomain:         spec.BaseDomain,
		Name:                  swag.String(spec.ClusterName),
		OpenshiftVersion:      swag.String(*openshiftVersion.ReleaseVersion),
		OlmOperators:          nil, // TODO: handle operators
		PullSecret:            swag.String(pullSecret),
		VipDhcpAllocation:     swag.Bool(false),
		IngressVip:            clusterInstall.Spec.IngressVIP,
		SSHPublicKey:          clusterInstall.Spec.SSHPublicKey,
		UserManagedNetworking: swag.Bool(isUserManagedNetwork(clusterInstall)),
	}

	if len(clusterInstall.Spec.Networking.ClusterNetwork) > 0 {
		clusterParams.ClusterNetworkCidr = swag.String(clusterInstall.Spec.Networking.ClusterNetwork[0].CIDR)
		clusterParams.ClusterNetworkHostPrefix = int64(clusterInstall.Spec.Networking.ClusterNetwork[0].HostPrefix)
	}

	if len(clusterInstall.Spec.Networking.ServiceNetwork) > 0 {
		clusterParams.ServiceNetworkCidr = swag.String(clusterInstall.Spec.Networking.ServiceNetwork[0])
	}

	if clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0 {
		clusterParams.HighAvailabilityMode = swag.String(HighAvailabilityModeNone)
	}

	if hyperthreadingInSpec(clusterInstall) {
		clusterParams.Hyperthreading = getHyperthreading(clusterInstall)
	}

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: clusterParams,
	})
	if err == nil { // Cluster registration succeeded
		infraEnv, err = getInfraEnvByClusterDeployment(ctx, log, r.Client, clusterDeployment.Name, clusterDeployment.Namespace)
		if err != nil {
			log.Errorf("failed to search for infraEnv to notify, for clusterDeployment %s", clusterDeployment.Name)
		} else if infraEnv != nil { // infraEnv exists for that clusterDeployment
			log.Infof("Notify that infraEnv %s should re-generate the image for clusterDeployment %s",
				infraEnv.Name, clusterDeployment.Name)
			r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
		}
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

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef, key.Namespace)
	if err != nil {
		log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, log, clusterInstall, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, clusterInstall.Spec, pullSecret)
	if err != nil {
		log.WithError(err).Error("failed to add OCP version")
		_, _ = r.updateStatus(ctx, log, clusterInstall, nil, err)
		// The controller will requeue after one minute, giving the user a chance to fix releaseImage
		return ctrl.Result{Requeue: true, RequeueAfter: longerRequeueAfterOnError}, nil
	}

	clusterParams := &models.AddHostsClusterCreateParams{
		APIVipDnsname:    swag.String(apiVipDnsname),
		Name:             swag.String(spec.ClusterName),
		OpenshiftVersion: swag.String(*openshiftVersion.ReleaseVersion),
		ID:               &id,
	}

	c, err := r.Installer.RegisterAddHostsClusterInternal(ctx, &key, installer.RegisterAddHostsClusterParams{
		NewAddHostsClusterParams: clusterParams,
	})
	if err != nil {
		log.WithError(err).Error("failed to create day2 cluster")
	}
	return r.updateStatus(ctx, log, clusterInstall, c, err)
}

func (r *ClusterDeploymentsReconciler) getReleaseImage(ctx context.Context, spec hiveext.AgentClusterInstallSpec) (string, error) {
	releaseImage, err := getReleaseImage(ctx, r.Client, spec.ImageSetRef.Name)
	if err != nil {
		return "", err
	}
	return releaseImage, nil
}

func (r *ClusterDeploymentsReconciler) addOpenshiftVersion(
	ctx context.Context,
	spec hiveext.AgentClusterInstallSpec,
	pullSecret string) (*models.OpenshiftVersion, error) {

	releaseImage, err := r.getReleaseImage(ctx, spec)
	if err != nil {
		return nil, err
	}

	openshiftVersion, err := r.Installer.AddOpenshiftVersion(ctx, releaseImage, pullSecret)
	if err != nil {
		return nil, err
	}

	return openshiftVersion, nil
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

	if err = r.Installer.DeregisterClusterInternal(ctx, installer.DeregisterClusterParams{
		ClusterID: *c.ID,
	}); err != nil {
		return buildReply(err)
	}
	// Delete agents because their backend cluster got deregistered.
	if err = r.DeleteClusterDeploymentAgents(ctx, log, key); err != nil {
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

func (r *ClusterDeploymentsReconciler) DeleteClusterDeploymentAgents(ctx context.Context, log logrus.FieldLogger, clusterDeployment types.NamespacedName) error {
	agents := &aiv1beta1.AgentList{}
	log = log.WithFields(logrus.Fields{"clusterDeployment": clusterDeployment.Name, "namespace": clusterDeployment.Namespace})
	if err := r.List(ctx, agents); err != nil {
		return err
	}
	for i, clusterAgent := range agents.Items {
		if clusterAgent.Spec.ClusterDeploymentName.Name == clusterDeployment.Name &&
			clusterAgent.Spec.ClusterDeploymentName.Namespace == clusterDeployment.Namespace {
			log.Infof("delete agent %s namespace %s", clusterAgent.Name, clusterAgent.Namespace)
			if err := r.Client.Delete(ctx, &agents.Items[i]); err != nil {
				log.WithError(err).Errorf("Failed to delete resource %s %s", clusterAgent.Name, clusterAgent.Namespace)
				return err
			}
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapSecretToClusterDeployment := func(a client.Object) []reconcile.Request {
		clusterDeployments := &hivev1.ClusterDeploymentList{}
		if err := r.List(context.Background(), clusterDeployments); err != nil {
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

	clusterDeploymentUpdates := r.CRDEventsHandler.GetClusterDeploymentUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(mapSecretToClusterDeployment)).
		Watches(&source.Kind{Type: &hiveext.AgentClusterInstall{}}, handler.EnqueueRequestsFromMapFunc(mapClusterInstallToClusterDeployment)).
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
		clusterInstall.Status.MachineNetwork = []hiveext.MachineNetworkEntry{{CIDR: c.MachineNetworkCidr}}

		if c.Status != nil {
			clusterInstall.Status.DebugInfo.State = swag.StringValue(c.Status)
			clusterInstall.Status.DebugInfo.StateInfo = swag.StringValue(c.StatusInfo)
			status := *c.Status
			var err error
			err = r.populateEventsURL(log, clusterInstall, c)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
			err = r.populateLogsURL(log, clusterInstall, c)
			if err != nil {
				return ctrl.Result{Requeue: true}, nil
			}
			var registeredHosts, approvedHosts int
			if status == models.ClusterStatusReady {
				registeredHosts, approvedHosts, err = r.getNumOfClusterAgents(ctx, clusterInstall, c)
				if err != nil {
					log.WithError(err).Error("failed to fetch cluster's agents")
					return ctrl.Result{Requeue: true}, nil
				}
			}
			clusterRequirementsMet(clusterInstall, status, c, registeredHosts, approvedHosts)
			clusterValidated(clusterInstall, status, c)
			clusterCompleted(clusterInstall, status, swag.StringValue(c.StatusInfo), c.MonitoredOperators)
			clusterFailed(clusterInstall, status, swag.StringValue(c.StatusInfo))
			clusterStopped(clusterInstall, status)
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
			eventUrl, err := r.eventsURL(log, string(*c.ID))
			if err != nil {
				log.WithError(err).Error("failed to generate Events URL")
				return err
			}
			clusterInstall.Status.DebugInfo.EventsURL = eventUrl
		}
	} else if r.EnableDay2Cluster && !r.isSNO(clusterInstall) {
		clusterInstall.Status.DebugInfo.EventsURL = ""
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) populateLogsURL(log logrus.FieldLogger, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) error {
	if swag.StringValue(c.Status) != models.ClusterStatusInstalled {
		if err := r.setControllerLogsDownloadURL(clusterInstall, c); err != nil {
			log.WithError(err).Error("failed to generate controller logs URL")
			return err
		}
	} else if r.EnableDay2Cluster && !r.isSNO(clusterInstall) {
		clusterInstall.Status.DebugInfo.LogsURL = ""
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) eventsURL(log logrus.FieldLogger, clusterId string) (string, error) {
	eventsURL := fmt.Sprintf("%s%s/clusters/%s/events", r.ServiceBaseURL, restclient.DefaultBasePath, clusterId)
	if r.AuthType != auth.TypeLocal {
		return eventsURL, nil
	}
	eventsURL, err := gencrypto.SignURL(eventsURL, clusterId)
	if err != nil {
		log.WithError(err).Error("failed to get Events URL")
		return "", err
	}
	return eventsURL, nil
}

func (r *ClusterDeploymentsReconciler) getNumOfClusterAgents(ctx context.Context, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) (int, int, error) {
	registeredHosts := 0
	approvedHosts := 0
	for _, h := range c.Hosts {
		if r.HostApi.IsInstallable(h) {
			registeredHosts += 1
			commonh, err := r.Installer.GetCommonHostInternal(ctx, c.ID.String(), h.ID.String())
			if err != nil {
				return 0, 0, err
			}
			if commonh.Approved {
				approvedHosts += 1
			}
		}
	}

	return registeredHosts, approvedHosts, nil
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

func clusterRequirementsMet(clusterInstall *hiveext.AgentClusterInstall, status string, c *common.Cluster, registeredHosts, approvedHosts int) {
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
	case models.ClusterStatusInsufficient, models.ClusterStatusPendingForInput, models.ClusterStatusReady:
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
	case models.ClusterStatusInstalled:
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

func (r *ClusterDeploymentsReconciler) setControllerLogsDownloadURL(
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {
	if clusterInstall.Status.DebugInfo.LogsURL != "" {
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
	downloadURL := fmt.Sprintf("%s%s/clusters/%s/logs",
		r.ServiceBaseURL, restclient.DefaultBasePath, cluster.ID.String())

	if r.AuthType != auth.TypeLocal {
		return downloadURL, nil
	}

	var err error
	downloadURL, err = gencrypto.SignURL(downloadURL, cluster.ID.String())
	if err != nil {
		return "", errors.Wrap(err, "failed to sign cluster controller logs URL")
	}

	return downloadURL, nil
}
