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
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
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
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch

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

	cluster, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !clusterDeployment.Spec.Installed {
			return r.createNewCluster(ctx, req.NamespacedName, clusterDeployment)
		}
		if !r.isSNO(clusterDeployment) {
			return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment)
		}
		// cluster  is installed and SNO nothing to do
		return ctrl.Result{Requeue: false}, nil
	}
	if err != nil {
		return r.updateStatus(ctx, clusterDeployment, cluster, err)
	}

	// check for updates from user, compare spec and update if needed
	err = r.updateIfNeeded(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterDeployment, cluster, err)
	}

	// check for install config overrides and update if needed
	err = r.updateInstallConfigOverrides(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterDeployment, cluster, err)
	}

	// In case the Cluster is a Day 1 cluster and is installed, update the Metadata and create secrets for credentials
	if *cluster.Status == models.ClusterStatusInstalled && swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		if !clusterDeployment.Spec.Installed {
			err = r.updateClusterMetadata(ctx, clusterDeployment, cluster)
			if err != nil {
				return r.updateStatus(ctx, clusterDeployment, cluster, err)
			}
		}
		// Delete Day1 Cluster
		err = r.Installer.DeregisterClusterInternal(ctx, installer.DeregisterClusterParams{
			ClusterID: *cluster.ID,
		})
		if err != nil {
			return r.updateStatus(ctx, clusterDeployment, cluster, err)
		}
		if !r.isSNO(clusterDeployment) {
			//Create Day2 cluster
			return r.createNewDay2Cluster(ctx, req.NamespacedName, clusterDeployment)
		}
		return r.updateStatus(ctx, clusterDeployment, cluster, err)
	}

	if swag.StringValue(cluster.Kind) == models.ClusterKindCluster {
		// Day 1
		return r.installDay1(ctx, clusterDeployment, cluster)

	} else if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		// Day 2
		return r.installDay2Hosts(ctx, clusterDeployment, cluster)
	}

	return r.updateStatus(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) installDay1(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) (ctrl.Result, error) {
	ready, err := r.isReadyForInstallation(ctx, clusterDeployment, cluster)
	if err != nil {
		return r.updateStatus(ctx, clusterDeployment, cluster, err)
	}
	if ready {

		// create custom manifests if needed before installation
		err = r.addCustomManifests(ctx, clusterDeployment, cluster)
		if err != nil {
			_, _ = r.updateStatus(ctx, clusterDeployment, cluster, err)
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
			return r.updateStatus(ctx, clusterDeployment, cluster, err)
		}
		return r.updateStatus(ctx, clusterDeployment, ic, err)
	}
	return r.updateStatus(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) installDay2Hosts(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment, cluster *common.Cluster) (ctrl.Result, error) {

	for _, h := range cluster.Hosts {
		commonh, err := r.Installer.GetCommonHostInternal(ctx, cluster.ID.String(), h.ID.String())
		if err != nil {
			return r.updateStatus(ctx, clusterDeployment, cluster, err)
		}
		if r.HostApi.IsInstallable(h) && commonh.Approved {
			r.Log.Infof("Installing Day2 host %s in %s %s", *h.ID, clusterDeployment.Name, clusterDeployment.Namespace)
			err = r.Installer.InstallSingleDay2HostInternal(ctx, *cluster.ID, *h.ID)
			if err != nil {
				return r.updateStatus(ctx, clusterDeployment, cluster, err)
			}
		}
	}
	return r.updateStatus(ctx, clusterDeployment, cluster, nil)
}

func (r *ClusterDeploymentsReconciler) updateClusterMetadata(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) error {

	s, err := r.ensureAdminPasswordSecret(ctx, cluster, c)
	if err != nil {
		return err
	}
	k, err := r.ensureKubeConfigSecret(ctx, cluster, c)
	if err != nil {
		return err
	}
	cluster.Spec.Installed = true
	cluster.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		ClusterID: c.OpenshiftClusterID.String(),
		AdminPasswordSecretRef: corev1.LocalObjectReference{
			Name: s.Name,
		},
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: k.Name,
		},
	}
	return r.Update(ctx, cluster)
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

func (r *ClusterDeploymentsReconciler) isReadyForInstallation(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster) (bool, error) {
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

	expectedHosts := cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents +
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents
	return readyHosts == expectedHosts, nil
}

func isSupportedPlatform(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.Platform.AgentBareMetal == nil ||
		cluster.Spec.Provisioning.InstallStrategy == nil ||
		cluster.Spec.Provisioning.InstallStrategy.Agent == nil {
		return false
	}
	return true
}

func isUserManagedNetwork(cluster *hivev1.ClusterDeployment) bool {
	if cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		return true
	}
	return false
}

func (r *ClusterDeploymentsReconciler) updateIfNeeded(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment,
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

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR, cluster.ClusterNetworkCidr, &params.ClusterNetworkCidr)
		hostPrefix := int64(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
		if hostPrefix > 0 && hostPrefix != cluster.ClusterNetworkHostPrefix {
			params.ClusterNetworkHostPrefix = swag.Int64(hostPrefix)
			update = true
		}
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork[0], cluster.ServiceNetworkCidr, &params.ServiceNetworkCidr)
	}
	if len(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork) > 0 {
		updateString(spec.Provisioning.InstallStrategy.Agent.Networking.MachineNetwork[0].CIDR, cluster.MachineNetworkCidr, &params.MachineNetworkCidr)
	}

	updateString(spec.Platform.AgentBareMetal.APIVIP, cluster.APIVip, &params.APIVip)
	updateString(spec.Platform.AgentBareMetal.IngressVIP, cluster.IngressVip, &params.IngressVip)
	updateString(spec.Provisioning.InstallStrategy.Agent.SSHPublicKey, cluster.SSHPublicKey, &params.SSHPublicKey)

	if userManagedNetwork := isUserManagedNetwork(clusterDeployment); userManagedNetwork != swag.BoolValue(cluster.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(userManagedNetwork)
	}
	pullSecretData, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, clusterDeployment.Namespace)
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

func (r *ClusterDeploymentsReconciler) updateInstallConfigOverrides(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment,
	cluster *common.Cluster) error {
	// handle InstallConfigOverrides
	update := false
	annotations := clusterDeployment.ObjectMeta.GetAnnotations()
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
		r.Log.Infof("Updated clusterDeployment %s/%s", clusterDeployment.Namespace, clusterDeployment.Name)
		return nil
	}
	return nil
}

func (r *ClusterDeploymentsReconciler) syncManifests(ctx context.Context, cluster *common.Cluster,
	clusterDeployment *hivev1.ClusterDeployment, alreadyCreatedManifests models.ListManifests) error {

	r.Log.Debugf("Going to sync list of given with already created manifests")

	manifestsFromConfigMap, err := r.getClusterDeploymentManifest(ctx, clusterDeployment)
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

func (r *ClusterDeploymentsReconciler) getClusterDeploymentManifest(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment) (map[string]string, error) {
	configuredManifests := &corev1.ConfigMap{}
	configuredManifests.Data = map[string]string{}
	// get manifests from configmap if we have reference for it
	if clusterDeployment.Spec.Provisioning.ManifestsConfigMapRef != nil {
		err := r.Get(ctx, types.NamespacedName{Namespace: clusterDeployment.Namespace,
			Name: clusterDeployment.Spec.Provisioning.ManifestsConfigMapRef.Name}, configuredManifests)
		if err != nil {
			r.Log.WithError(err).Errorf("Failed to get configmap %s in %s",
				clusterDeployment.Spec.Provisioning.ManifestsConfigMapRef.Name, clusterDeployment.Namespace)
			return nil, err
		}
	}
	return configuredManifests.Data, nil
}

func (r *ClusterDeploymentsReconciler) addCustomManifests(ctx context.Context, clusterDeployment *hivev1.ClusterDeployment,
	cluster *common.Cluster) error {

	alreadyCreatedManifests, err := r.Manifests.ListClusterManifestsInternal(ctx, operations.ListClusterManifestsParams{
		ClusterID: *cluster.ID,
	})
	if err != nil {
		r.Log.WithError(err).Errorf("Failed to list manifests for %q cluster deployment", clusterDeployment.Name)
		return err
	}

	// if reference to manifests was deleted from cluster deployment
	// but we already added some in previous reconcile loop, we want to clean them.
	// if no reference were provided we will delete all manifests that were in the list
	if clusterDeployment.Spec.Provisioning.ManifestsConfigMapRef == nil && len(alreadyCreatedManifests) == 0 {
		r.Log.Debugf("Nothing to do, skipping manifest creation")
		return nil
	}

	return r.syncManifests(ctx, cluster, clusterDeployment, alreadyCreatedManifests)
}

func (r *ClusterDeploymentsReconciler) isSNO(cluster *hivev1.ClusterDeployment) bool {
	return cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		cluster.Spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0
}

func (r *ClusterDeploymentsReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	r.Log.Infof("Creating a new clusterDeployment %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, clusterDeployment, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, spec, pullSecret)
	if err != nil {
		r.Log.WithError(err).Error("failed to add OCP version")
		return r.updateStatus(ctx, clusterDeployment, nil, err)
	}

	clusterParams := &models.ClusterCreateParams{
		BaseDNSDomain:         spec.BaseDomain,
		Name:                  swag.String(spec.ClusterName),
		OpenshiftVersion:      swag.String(*openshiftVersion.ReleaseVersion),
		OlmOperators:          nil, // TODO: handle operators
		PullSecret:            swag.String(pullSecret),
		VipDhcpAllocation:     swag.Bool(false),
		IngressVip:            spec.Platform.AgentBareMetal.IngressVIP,
		SSHPublicKey:          spec.Provisioning.InstallStrategy.Agent.SSHPublicKey,
		UserManagedNetworking: swag.Bool(isUserManagedNetwork(clusterDeployment)),
	}

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork) > 0 {
		clusterParams.ClusterNetworkCidr = swag.String(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].CIDR)
		clusterParams.ClusterNetworkHostPrefix = int64(spec.Provisioning.InstallStrategy.Agent.Networking.ClusterNetwork[0].HostPrefix)
	}

	if len(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork) > 0 {
		clusterParams.ServiceNetworkCidr = swag.String(spec.Provisioning.InstallStrategy.Agent.Networking.ServiceNetwork[0])
	}

	if spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.ControlPlaneAgents == 1 &&
		spec.Provisioning.InstallStrategy.Agent.ProvisionRequirements.WorkerAgents == 0 {
		clusterParams.HighAvailabilityMode = swag.String(HighAvailabilityModeNone)
	}

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: clusterParams,
	})

	return r.updateStatus(ctx, clusterDeployment, c, err)
}

func (r *ClusterDeploymentsReconciler) createNewDay2Cluster(
	ctx context.Context,
	key types.NamespacedName,
	clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {

	r.Log.Infof("Creating a new Day2 Cluster %s %s", clusterDeployment.Name, clusterDeployment.Namespace)
	spec := clusterDeployment.Spec
	id := strfmt.UUID(uuid.New().String())
	apiVipDnsname := fmt.Sprintf("api.%s.%s", spec.ClusterName, spec.BaseDomain)

	pullSecret, err := getPullSecret(ctx, r.Client, spec.PullSecretRef.Name, key.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return r.updateStatus(ctx, clusterDeployment, nil, err)
	}

	openshiftVersion, err := r.addOpenshiftVersion(ctx, spec, pullSecret)
	if err != nil {
		r.Log.WithError(err).Error("failed to add OCP version")
		return r.updateStatus(ctx, clusterDeployment, nil, err)
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

	return r.updateStatus(ctx, clusterDeployment, c, err)
}

func (r *ClusterDeploymentsReconciler) getReleaseImage(ctx context.Context, spec hivev1.ClusterDeploymentSpec) (string, error) {
	var releaseImage string
	if spec.Provisioning.ReleaseImage != "" {
		releaseImage = spec.Provisioning.ReleaseImage
	} else {
		var err error
		releaseImage, err = getReleaseImage(ctx, r.Client, spec.Provisioning.ImageSetRef.Name)
		if err != nil {
			return "", err
		}
	}
	return releaseImage, nil
}

func (r *ClusterDeploymentsReconciler) addOpenshiftVersion(
	ctx context.Context,
	spec hivev1.ClusterDeploymentSpec,
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

	clusterDeploymentUpdates := r.CRDEventsHandler.GetClusterDeploymentUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Watches(&source.Kind{Type: &corev1.Secret{}}, handler.EnqueueRequestsFromMapFunc(mapSecretToClusterDeployment)).
		Watches(&source.Channel{Source: clusterDeploymentUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// updateStatus is updating all the ClusterDeployment Conditions.
// In case that an error has occured when trying to sync the Spec, the error (syncErr) is presented in SpecSyncedCondition.
// Internal bool differentiate between backend server error (internal HTTP 5XX) and user input error (HTTP 4XXX)
func (r *ClusterDeploymentsReconciler) updateStatus(ctx context.Context, cluster *hivev1.ClusterDeployment, c *common.Cluster, syncErr error) (ctrl.Result, error) {
	clusterSpecSynced(cluster, syncErr)
	if c != nil {
		cluster.Status.InstallStrategy = &hivev1.InstallStrategyStatus{Agent: &agent.InstallStrategyStatus{
			ConnectivityMajorityGroups: c.ConnectivityMajorityGroups,
		}}
		cluster.Status.InstallStrategy.Agent.ConnectivityMajorityGroups = c.ConnectivityMajorityGroups
		if c.Status != nil {
			status := *c.Status
			clusterReadyForInstallation(cluster, status)
			clusterValidated(cluster, status, c)
			clusterInstalled(cluster, status, swag.StringValue(c.StatusInfo))
		}
	} else {
		setClusterConditionsUnknown(cluster)
	}

	if cluster.Spec.Installed {
		cluster.Status.APIURL = fmt.Sprintf("https://api.%s.%s:6443", cluster.Spec.ClusterName, cluster.Spec.BaseDomain)
		cluster.Status.WebConsoleURL = common.GetConsoleUrl(cluster.Spec.ClusterName, cluster.Spec.BaseDomain)
	}

	if updated, result, _ := UpdateStatus(r.Log, r.Status().Update, ctx, cluster); !updated {
		return result, nil
	}
	if syncErr != nil && !IsHTTP4XXError(syncErr) {
		return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
	}
	return ctrl.Result{}, nil
}

// clusterSpecSynced is updating the Cluster SpecSynced Condition.
func clusterSpecSynced(cluster *hivev1.ClusterDeployment, syncErr error) {
	var condStatus corev1.ConditionStatus
	var reason string
	var msg string
	if syncErr == nil {
		condStatus = corev1.ConditionTrue
		reason = SyncedOkReason
		msg = SyncedOkMsg
	} else {
		condStatus = corev1.ConditionFalse
		if !IsHTTP4XXError(syncErr) {
			reason = BackendErrorReason
			msg = BackendErrorMsg + " " + syncErr.Error()
		} else {
			reason = InputErrorReason
			msg = InputErrorMsg + " " + syncErr.Error()
		}
	}
	setClusterCondition(&cluster.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterSpecSyncedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterReadyForInstallation(cluster *hivev1.ClusterDeployment, status string) {
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
		condStatus = corev1.ConditionFalse
		reason = ClusterAlreadyInstallingReason
		msg = ClusterAlreadyInstallingMsg
	default:
		condStatus = corev1.ConditionUnknown
		reason = UnknownStatusReason
		msg = fmt.Sprintf("%s %s", UnknownStatusMsg, status)
	}
	setClusterCondition(&cluster.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterReadyForInstallationCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterInstalled(cluster *hivev1.ClusterDeployment, status, statusInfo string) {
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
	setClusterCondition(&cluster.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterInstalledCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func clusterValidated(clusterDeployment *hivev1.ClusterDeployment, status string, c *common.Cluster) {
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
	setClusterCondition(&clusterDeployment.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterValidatedCondition,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

func setClusterConditionsUnknown(clusterDeployment *hivev1.ClusterDeployment) {
	setClusterCondition(&clusterDeployment.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterValidatedCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterDeployment.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterReadyForInstallationCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
	setClusterCondition(&clusterDeployment.Status.Conditions, hivev1.ClusterDeploymentCondition{
		Type:    ClusterInstalledCondition,
		Status:  corev1.ConditionUnknown,
		Reason:  NotAvailableReason,
		Message: NotAvailableMsg,
	})
}

// SetStatusCondition sets the corresponding condition in conditions to newCondition.
func setClusterCondition(conditions *[]hivev1.ClusterDeploymentCondition, newCondition hivev1.ClusterDeploymentCondition) {
	if conditions == nil {
		conditions = &[]hivev1.ClusterDeploymentCondition{}
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

func isConditionEqual(existingCond hivev1.ClusterDeploymentCondition, newCondition hivev1.ClusterDeploymentCondition) bool {
	if existingCond.Type == newCondition.Type {
		return existingCond.Status == newCondition.Status &&
			existingCond.Reason == newCondition.Reason &&
			existingCond.Message == newCondition.Message
	}
	return false
}

// FindStatusCondition finds the conditionType in conditions.
func FindStatusCondition(conditions []hivev1.ClusterDeploymentCondition, conditionType hivev1.ClusterDeploymentConditionType) *hivev1.ClusterDeploymentCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}
