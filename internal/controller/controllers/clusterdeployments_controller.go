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
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	restclient "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	hiveext "github.com/openshift/assisted-service/internal/controller/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
	InstallConfigOverrides            = v1beta1.Group + "/install-config-overrides"
)

const HighAvailabilityModeNone = "None"
const defaultRequeueAfterOnError = 10 * time.Second

// ClusterDeploymentsReconciler reconciles a Cluster object
type ClusterDeploymentsReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	Scheme           *runtime.Scheme
	Installer        bminventory.InstallerInternals
	ClusterApi       cluster.API
	HostApi          host.API
	CRDEventsHandler CRDEventsHandler
	Manifests        manifests.ClusterManifestsInternals
	ServiceBaseURL   string
	AuthType         auth.AuthType
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get;update;patch

func (r *ClusterDeploymentsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Infof("Reconcile has been called for ClusterDeployment name=%s namespace=%s", req.Name, req.Namespace)

	clusterDeployment := &hivev1.ClusterDeployment{}
	err := r.Get(ctx, req.NamespacedName, clusterDeployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return r.deregisterClusterIfNeeded(ctx, req.NamespacedName)
		}
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{Requeue: true}, nil
	}

	// ignore unsupported platforms
	if !isSupportedPlatform(clusterDeployment) {
		return ctrl.Result{}, nil
	}

	clusterInstall := &hiveext.AgentClusterInstall{}
	if clusterDeployment.Spec.ClusterInstallRef == nil {
		r.Log.Infof("AgentClusterInstall not set for ClusterDeployment %s", clusterDeployment.Name)
		return ctrl.Result{}, nil
	}

	aciName := clusterDeployment.Spec.ClusterInstallRef.Name
	err = r.Get(ctx,
		types.NamespacedName{
			Namespace: clusterDeployment.Namespace,
			Name:      aciName,
		},
		clusterInstall)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.Log.WithField("AgentClusterInstall", aciName).Infof("AgentClusterInstall does not exist for ClusterDeployment %s", clusterDeployment.Name)
			return ctrl.Result{}, nil
		}
		r.Log.WithError(err).Errorf("Failed to get AgentClusterInstall %s", aciName)
		return ctrl.Result{Requeue: true}, err
	}

	err = r.ensureOwnerRef(ctx, clusterDeployment, clusterInstall)
	if err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	cluster, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !isInstalled(clusterDeployment, clusterInstall) {
			return r.createNewCluster(ctx, req.NamespacedName, clusterDeployment, clusterInstall)
		}
		if !r.isSNO(clusterInstall) {
			return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment, clusterInstall)
		}
		// cluster  is installed and SNO. Clear EventsURL
		clusterInstall.Status.DebugInfo.EventsURL = ""
		if updateErr := r.Status().Update(ctx, clusterInstall); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update ClusterDeployment Status")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{Requeue: false}, nil
	}
	if err != nil {
		return r.updateStatus(ctx, clusterInstall, cluster, err)
	}

	// check for updates from user, compare spec and update if needed
	err = r.updateIfNeeded(ctx, clusterDeployment, clusterInstall, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterInstall, cluster, err)
	}

	// check for install config overrides and update if needed
	err = r.updateInstallConfigOverrides(ctx, clusterInstall, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterInstall, cluster, err)
	}

	// In case the Cluster is a Day 1 cluster and is installed, update the Metadata and create secrets for credentials
	if *cluster.Status == models.ClusterStatusInstalled && swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		if !isInstalled(clusterDeployment, clusterInstall) {
			// create secrets and update status
			err = r.updateClusterMetadata(ctx, clusterDeployment, cluster, clusterInstall)
			return r.updateStatus(ctx, clusterInstall, cluster, err)
		} else {
			// Delete Day1 Cluster
			err = r.Installer.DeregisterClusterInternal(ctx, installer.DeregisterClusterParams{
				ClusterID: *cluster.ID,
			})
			if err != nil {
				return r.updateStatus(ctx, clusterInstall, cluster, err)
			}
			if !r.isSNO(clusterInstall) {
				//Create Day2 cluster
				return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment, clusterInstall)
			}
			return r.updateStatus(ctx, clusterInstall, cluster, err)
		}
	}

	if swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		// Day 1
		return r.installDay1(ctx, clusterDeployment, clusterInstall, cluster)

	} else if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		// Day 2
		return r.installDay2Hosts(ctx, clusterDeployment, clusterInstall, cluster)
	}

	return r.updateStatus(ctx, clusterInstall, cluster, nil)
}

func isInstalled(clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall) bool {
	if clusterDeployment.Spec.Installed {
		return true
	}
	cond := FindStatusCondition(clusterInstall.Status.Conditions, ClusterCompletedCondition)
	return cond != nil && cond.Reason == InstalledReason
}

func (r *ClusterDeploymentsReconciler) installDay1(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) (ctrl.Result, error) {
	ready, err := r.isReadyForInstallation(ctx, clusterDeployment, clusterInstall, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterInstall, cluster, err)
	}
	if ready {

		// create custom manifests if needed before installation
		err = r.addCustomManifests(ctx, clusterInstall, cluster)
		if err != nil {
			_, _ = r.updateStatus(ctx, clusterInstall, cluster, err)
			// We decided to requeue with one minute timeout in order to give user a chance to fix manifest
			// this timeout allows us not to run reconcile too much time and
			// still have a nice feedback when user will fix the error
			return ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Minute}, nil
		}

		r.Log.Infof("Installing clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
		var ic *common.Cluster
		ic, err = r.Installer.InstallClusterInternal(ctx, installer.InstallClusterParams{
			ClusterID: *cluster.ID,
		})
		if err != nil {
			return r.updateStatus(ctx, clusterInstall, cluster, err)
		}
		return r.updateStatus(ctx, clusterInstall, ic, err)
	}
	return r.updateStatus(ctx, clusterInstall, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) installDay2Hosts(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall, cluster *common.Cluster) (ctrl.Result, error) {

	for _, h := range cluster.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, cluster.ID.String(), h.ID.String())
		if err != nil {
			return r.updateStatus(ctx, clusterInstall, cluster, err)
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			r.Log.Infof("Installing Day2 host %s in %s %s", *h.ID, clusterDeployment.Name, clusterDeployment.Namespace)
			err = r.Installer.InstallSingleDay2HostInternal(ctx, *cluster.ID, *h.ID)
			if err != nil {
				return r.updateStatus(ctx, clusterInstall, cluster, err)
			}
		}
	}
	return r.updateStatus(ctx, clusterInstall, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) updateClusterMetadata(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster, clusterInstall *hiveext.AgentClusterInstall) error {

	s, err := r.ensureAdminPasswordSecret(ctx, cluster, c)
	if err != nil {
		return err
	}
	k, err := r.ensureKubeConfigSecret(ctx, cluster, c)
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

func (r *ClusterDeploymentsReconciler) ensureAdminPasswordSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
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
	return r.createClusterCredentialSecret(ctx, cluster, c, name, data, "kubeadmincreds")
}

func (r *ClusterDeploymentsReconciler) ensureKubeConfigSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (*corev1.Secret, error) {
	s := &corev1.Secret{}
	name := fmt.Sprintf(adminKubeConfigStringTemplate, cluster.Name)
	getErr := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, s)
	if getErr == nil || !k8serrors.IsNotFound(getErr) {
		return s, getErr
	}
	respBody, _, err := r.Installer.DownloadClusterKubeconfigInternal(ctx, installer.DownloadClusterKubeconfigParams{
		ClusterID: *c.ID,
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

	return r.createClusterCredentialSecret(ctx, cluster, c, name, data, "kubeconfig")
}

func (r *ClusterDeploymentsReconciler) createClusterCredentialSecret(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster, name string, data map[string][]byte, secretType string) (*corev1.Secret, error) {
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
		r.Log.WithError(err).Errorf("error getting GVK for clusterdeployment")
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

func (r *ClusterDeploymentsReconciler) isReadyForInstallation(ctx context.Context, cluster *hivev1.ClusterDeployment, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster) (bool, error) {
	if ready, _ := r.ClusterApi.IsReadyForInstallation(c); !ready {
		return false, nil
	}

	readyHosts := 0
	for _, h := range c.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, c.ID.String(), h.ID.String())
		if err != nil {
			return false, err
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			readyHosts += 1
		}
	}

	expectedHosts := clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents +
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents
	return readyHosts == expectedHosts, nil
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

func (r *ClusterDeploymentsReconciler) updateIfNeeded(ctx context.Context,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {

	update := false
	notifyInfraEnv := false
	var infraEnv *v1beta1.InfraEnv

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

	updateString(clusterInstall.Spec.APIVIP, cluster.APIVip, &params.APIVip)
	updateString(clusterInstall.Spec.IngressVIP, cluster.IngressVip, &params.IngressVip)
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
	if !update {
		return nil
	}
	_, err = r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *cluster.ID,
	})
	if err != nil {
		return err
	}

	infraEnv, err = getInfraEnvByClusterDeployment(ctx, r.Client, clusterDeployment)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to search for infraEnv for clusterDeployment %s", clusterDeployment.Name))
	}

	r.Log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
	if notifyInfraEnv && infraEnv != nil {
		r.Log.Infof("Notify that infraEnv %s should re-generate the image for clusterDeployment %s", infraEnv.Name, clusterDeployment.ClusterName)
		r.CRDEventsHandler.NotifyInfraEnvUpdates(infraEnv.Name, infraEnv.Namespace)
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) updateInstallConfigOverrides(ctx context.Context, clusterInstall *hiveext.AgentClusterInstall,
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
			return err
		}
		r.Log.Infof("Updated InstallConfig overrides on clusterInstall %s/%s", clusterInstall.Namespace, clusterInstall.Name)
		return nil
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) syncManifests(ctx context.Context, cluster *common.Cluster,
	clusterInstall *hiveext.AgentClusterInstall, alreadyCreatedManifests models.ListManifests) error {

	r.Log.Debugf("Going to sync list of given with already created manifests")

	manifestsFromConfigMap, err := r.getClusterDeploymentManifest(ctx, clusterInstall)
	if err != nil {
		return err
	}

	// delete all manifests that are not part of configmap
	// skip errors
	for _, manifest := range alreadyCreatedManifests {
		if _, ok := manifestsFromConfigMap[manifest.FileName]; !ok {
			r.Log.Infof("Deleting cluster deployment %s manifest %s", cluster.KubeKeyName, manifest.FileName)
			_ = r.Manifests.DeleteClusterManifestInternal(ctx, operations.DeleteClusterManifestParams{
				ClusterID: *cluster.ID,
				FileName:  manifest.FileName,
				Folder:    swag.String(models.ManifestFolderOpenshift),
			})
		}
	}

	// create/update all manifests provided by configmap data
	for filename, manifest := range manifestsFromConfigMap {
		r.Log.Infof("Creating cluster deployment %s manifest %s", cluster.KubeKeyName, filename)
		_, err := r.Manifests.CreateClusterManifestInternal(ctx, operations.CreateClusterManifestParams{
			ClusterID: *cluster.ID,
			CreateManifestParams: &models.CreateManifestParams{
				Content:  swag.String(base64.StdEncoding.EncodeToString([]byte(manifest))),
				FileName: swag.String(filename),
				Folder:   swag.String(models.ManifestFolderOpenshift),
			}})
		if err != nil {
			r.Log.WithError(err).Errorf("Failed to create cluster deployment %s manifest %s", cluster.KubeKeyName, filename)
			return err
		}
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) getClusterDeploymentManifest(ctx context.Context, clusterInstall *hiveext.AgentClusterInstall) (map[string]string, error) {
	configuredManifests := &corev1.ConfigMap{}
	configuredManifests.Data = map[string]string{}
	// get manifests from configmap if we have reference for it
	if clusterInstall.Spec.ManifestsConfigMapRef != nil {
		err := r.Get(ctx, types.NamespacedName{Namespace: clusterInstall.Namespace,
			Name: clusterInstall.Spec.ManifestsConfigMapRef.Name}, configuredManifests)
		if err != nil {
			r.Log.WithError(err).Errorf("Failed to get configmap %s in %s",
				clusterInstall.Spec.ManifestsConfigMapRef.Name, clusterInstall.Namespace)
			return nil, err
		}
	}
	return configuredManifests.Data, nil
}

func (r *ClusterDeploymentsReconciler) addCustomManifests(ctx context.Context, clusterInstall *hiveext.AgentClusterInstall,
	cluster *common.Cluster) error {

	alreadyCreatedManifests, err := r.Manifests.ListClusterManifestsInternal(ctx, operations.ListClusterManifestsParams{
		ClusterID: *cluster.ID,
	})
	if err != nil {
		r.Log.WithError(err).Errorf("Failed to list manifests for %q cluster install", clusterInstall.Name)
		return err
	}

	// if reference to manifests was deleted from cluster deployment
	// but we already added some in previous reconcile loop, we want to clean them.
	// if no reference were provided we will delete all manifests that were in the list
	if clusterInstall.Spec.ManifestsConfigMapRef == nil && len(alreadyCreatedManifests) == 0 {
		r.Log.Debugf("Nothing to do, skipping manifest creation")
		return nil
	}

	return r.syncManifests(ctx, cluster, clusterInstall, alreadyCreatedManifests)
}

func (r *ClusterDeploymentsReconciler) isSNO(clusterInstall *hiveext.AgentClusterInstall) bool {
	return clusterInstall.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		clusterInstall.Spec.ProvisionRequirements.WorkerAgents == 0
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {

	r.Log.Infof("Creating a new clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, clusterInstall, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, clusterInstall.Spec, pullSecret)
	if err != nil {
		r.Log.WithError(err).Error("failed to add OCP version")
		return r.updateStatus(ctx, clusterInstall, nil, err)
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

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: clusterParams,
	})

	return r.updateStatus(ctx, clusterInstall, c, err)
}

func (r *ClusterDeploymentsReconciler) createNewDay2Cluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment,
	clusterInstall *hiveext.AgentClusterInstall) (ctrl.Result, error) {

	r.Log.Infof("Creating a new Day2 Cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec
	id := strfmt.UUID(uuid.New().String())
	apiVipDnsname := fmt.Sprintf("api.%s.%s", spec.ClusterName, spec.BaseDomain)

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, clusterInstall, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, clusterInstall.Spec, pullSecret)
	if err != nil {
		r.Log.WithError(err).Error("failed to add OCP version")
		return r.updateStatus(ctx, clusterInstall, nil, err)
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

	return r.updateStatus(ctx, clusterInstall, c, err)
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

func (r *ClusterDeploymentsReconciler) deregisterClusterIfNeeded(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "failed to deregister cluster: %s", key.Name)
		r.Log.Error(err)
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

	r.Log.Infof("Cluster resource deleted, Unregistered cluster: %s", c.ID.String())

	return buildReply(nil)
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
		aci, ok := a.(*hiveext.AgentClusterInstall)
		if !ok {
			r.Log.Errorf("%v was not an AgentClusterInstall", a) // shouldn't be possible
			return []reconcile.Request{}
		}
		r.Log.Debugf("Map ACI : %s %s CD ref name %s", aci.Namespace, aci.Name, aci.Spec.ClusterDeploymentRef.Name)
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
// In case that an error has occured when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *ClusterDeploymentsReconciler) updateStatus(ctx context.Context, clusterInstall *hiveext.AgentClusterInstall, c *common.Cluster, syncErr error) (ctrl.Result, error) {
	clusterSpecSynced(clusterInstall, syncErr)
	if c != nil {
		clusterInstall.Status.ConnectivityMajorityGroups = c.ConnectivityMajorityGroups
		eventUrl, err := r.eventsURL(string(*c.ID))
		if err != nil {
			return ctrl.Result{Requeue: true}, nil
		}
		clusterInstall.Status.DebugInfo = hiveext.DebugInfo{
			EventsURL: eventUrl,
		}
		if c.Status != nil {
			status := *c.Status
			clusterRequirementsMet(clusterInstall, status)
			clusterValidated(clusterInstall, status, c)
			clusterCompleted(clusterInstall, status, swag.StringValue(c.StatusInfo))
			clusterFailed(clusterInstall, status, swag.StringValue(c.StatusInfo))
			clusterStopped(clusterInstall, status)
		}
	} else {
		setClusterConditionsUnknown(clusterInstall)
	}

	if updateErr := r.Status().Update(ctx, clusterInstall); updateErr != nil {
		r.Log.WithError(updateErr).Error("failed to update ClusterDeployment Status")
		return ctrl.Result{Requeue: true}, nil
	}
	if syncErr != nil && !IsUserError(syncErr) {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ClusterDeploymentsReconciler) eventsURL(clusterId string) (string, error) {
	eventsURL := fmt.Sprintf("%s%s/clusters/%s/events", r.ServiceBaseURL, restclient.DefaultBasePath, clusterId)
	if r.AuthType != auth.TypeLocal {
		return eventsURL, nil
	}
	eventsURL, err := gencrypto.SignURL(eventsURL, clusterId)
	if err != nil {
		r.Log.WithError(err).Error("failed to get Events URL")
		return "", err
	}
	return eventsURL, nil
}

// clusterSpecSynced is updating the Cluster SpecSynced Condition.
func clusterSpecSynced(cluster *hiveext.AgentClusterInstall, syncErr error) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	if syncErr == nil {
		condStatus = corev1.ConditionTrue
		reason = SyncedOkReason
		msg = SyncedOkMsg
	} else {
		condStatus = corev1.ConditionFalse
		if !IsUserError(syncErr) {
			reason = BackendErrorReason
			msg = BackendErrorMsg + " " + syncErr.Error()
		} else {
			reason = InputErrorReason
			msg = InputErrorMsg + " " + syncErr.Error()
		}
	}
	setClusterCondition(&cluster.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterSpecSyncedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterRequirementsMet(clusterInstall *hiveext.AgentClusterInstall, status string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.ClusterStatusReady:
		condStatus = corev1.ConditionTrue
		reason = ClusterReadyReason
		msg = ClusterReadyMsg
	case models.ClusterStatusInsufficient, models.ClusterStatusPendingForInput:
		condStatus = corev1.ConditionFalse
		reason = ClusterNotReadyReason
		msg = ClusterNotReadyMsg
	case models.ClusterStatusPreparingForInstallation, models.ClusterStatusInstalled,
		models.ClusterStatusInstalling, models.ClusterStatusInstallingPendingUserAction,
		models.ClusterStatusError, models.ClusterStatusAddingHosts, models.ClusterStatusFinalizing:
		condStatus = corev1.ConditionTrue
		reason = ClusterAlreadyInstallingReason
		msg = ClusterAlreadyInstallingMsg
	default:
		condStatus = corev1.ConditionUnknown
		reason = UnknownStatusReason
		msg = fmt.Sprintf("%s %s", UnknownStatusMsg, status)
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterRequirementsMetCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterCompleted(clusterInstall *hiveext.AgentClusterInstall, status, statusInfo string) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	switch status {
	case models.ClusterStatusInstalled, models.ClusterStatusAddingHosts:
		condStatus = corev1.ConditionTrue
		reason = InstalledReason
		msg = fmt.Sprintf("%s %s", InstalledMsg, statusInfo)
	case models.ClusterStatusError:
		condStatus = corev1.ConditionFalse
		reason = InstallationFailedReason
		msg = fmt.Sprintf("%s %s", InstallationFailedMsg, statusInfo)
	case models.ClusterStatusInsufficient, models.ClusterStatusPendingForInput, models.ClusterStatusReady:
		condStatus = corev1.ConditionFalse
		reason = InstallationNotStartedReason
		msg = InstallationNotStartedMsg
	case models.ClusterStatusPreparingForInstallation, models.ClusterStatusInstalling, models.ClusterStatusFinalizing,
		models.ClusterStatusInstallingPendingUserAction:
		condStatus = corev1.ConditionFalse
		reason = InstallationInProgressReason
		msg = fmt.Sprintf("%s %s", InstallationInProgressMsg, statusInfo)
	default:
		condStatus = corev1.ConditionUnknown
		reason = UnknownStatusReason
		msg = fmt.Sprintf("%s %s", UnknownStatusMsg, status)
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterCompletedCondition,
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
		reason = ClusterFailedReason
		msg = fmt.Sprintf("%s %s", ClusterFailedMsg, statusInfo)
	default:
		condStatus = corev1.ConditionFalse
		reason = ClusterNotFailedReason
		msg = ClusterNotFailedMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterFailedCondition,
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
		reason = ClusterStoppedFailedReason
		msg = ClusterStoppedFailedMsg
	case models.ClusterStatusCancelled:
		condStatus = corev1.ConditionTrue
		reason = ClusterStoppedCanceledReason
		msg = ClusterStoppedCanceledMsg
	case models.ClusterStatusInstalled:
		condStatus = corev1.ConditionTrue
		reason = ClusterStoppedCompletedReason
		msg = ClusterStoppedCompletedMsg
	default:
		condStatus = corev1.ConditionFalse
		reason = ClusterNotStoppedReason
		msg = ClusterNotStoppedMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterStoppedCondition,
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
				if v.Status == cluster.ValidationFailure {
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
		reason = ValidationsFailingReason
		msg = fmt.Sprintf("%s %s", ClusterValidationsFailingMsg, failedValidationInfo)
	case models.ClusterStatusPendingForInput == status:
		condStatus = corev1.ConditionFalse
		reason = ValidationsFailingReason
		msg = fmt.Sprintf("%s %s", ClusterValidationsFailingMsg, failedValidationInfo)
	case models.ClusterStatusAddingHosts == status:
		condStatus = corev1.ConditionTrue
		reason = ValidationsPassingReason
		msg = ClusterValidationsOKMsg
	case c.ValidationsInfo == "":
		condStatus = corev1.ConditionUnknown
		reason = ValidationsUnknownReason
		msg = ClusterValidationsUnknownMsg
	default:
		condStatus = corev1.ConditionTrue
		reason = ValidationsPassingReason
		msg = ClusterValidationsOKMsg
	}
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterValidatedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func setClusterConditionsUnknown(clusterInstall *hiveext.AgentClusterInstall) {
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterValidatedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterRequirementsMetCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterCompletedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterFailedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterInstall.Status.Conditions, hivev1.ClusterInstallCondition{
		Type:    ClusterStoppedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
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
func (r *ClusterDeploymentsReconciler) ensureOwnerRef(ctx context.Context, cd *hivev1.ClusterDeployment, ci *hiveext.AgentClusterInstall) error {

	if err := controllerutil.SetOwnerReference(cd, ci, r.Scheme); err != nil {
		r.Log.WithError(err).Error("error setting owner reference Agent Cluster Install")
		return err
	}
	return r.Update(ctx, ci)
}
