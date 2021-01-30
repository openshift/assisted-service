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
	"time"

	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const defaultRequeueAfterOnError = 10 * time.Second

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log                      logrus.FieldLogger
	Scheme                   *runtime.Scheme
	Installer                bminventory.InstallerInternals
	ClusterApi               cluster.API
	HostApi                  host.API
	PullSecretUpdatesChannel chan event.GenericEvent
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=clusters/status,verbs=get;update;patch

func (r *ClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	cluster := &adiiov1alpha1.Cluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return r.deregisterClusterIfNeeded(ctx, req.NamespacedName)
		}
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{Requeue: true}, nil
	}

	c, err := r.Installer.GetClusterByKubeKey(req.NamespacedName)

	if gorm.IsRecordNotFoundError(err) {
		return r.createNewCluster(ctx, req.NamespacedName, cluster)
	}

	// todo check error type, retry for 5xx fail on 4xx
	if err != nil {
		return r.updateState(ctx, cluster, nil, err)
	}

	var updated bool
	var result ctrl.Result
	// check for updates from user, compare spec and update if needed
	updated, result, err = r.updateIfNeeded(ctx, cluster, c)
	if err != nil {
		return r.updateState(ctx, cluster, c, err)
	}

	if updated {
		return result, err
	}

	if r.isReadyForInstallation(cluster, c) {
		var ic *common.Cluster
		ic, err = r.Installer.InstallClusterInternal(ctx, installer.InstallClusterParams{
			ClusterID: *c.ID,
		})
		if err != nil {
			return r.updateState(ctx, cluster, c, err)
		}
		return r.updateState(ctx, cluster, ic, nil)
	}

	return r.updateState(ctx, cluster, c, nil)
}

func (r *ClusterReconciler) isReadyForInstallation(cluster *adiiov1alpha1.Cluster, c *common.Cluster) bool {
	if ready, _ := r.ClusterApi.IsReadyForInstallation(c); !ready {
		return false
	}

	readyHosts := 0
	for _, h := range c.Hosts {
		if r.HostApi.IsInstallable(h) {
			readyHosts += 1
		}
	}

	expectedHosts := cluster.Spec.ProvisionRequirements.ControlPlaneAgents + cluster.Spec.ProvisionRequirements.WorkerAgents
	return readyHosts == expectedHosts
}

func (r *ClusterReconciler) getPullSecret(ctx context.Context, name, namespace string) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	if err := r.Get(ctx, key, secret); err != nil {
		return "", errors.Wrapf(err, "failed to get pull secret %s", key)
	}

	data, ok := secret.Data["pullSecret"]
	if !ok {
		return "", errors.Errorf("pullSecret is not configured")
	}

	return string(data), nil
}

func (r *ClusterReconciler) createNewCluster(
	ctx context.Context,
	key types.NamespacedName,
	cluster *adiiov1alpha1.Cluster) (ctrl.Result, error) {

	r.Log.Infof("Creating a new cluster %s %s", cluster.Name, cluster.Namespace)
	spec := cluster.Spec

	pullSecret, err := r.getPullSecret(ctx, spec.PullSecretRef.Name, spec.PullSecretRef.Namespace)
	if err != nil {
		r.Log.WithError(err).Error("failed to get pull secret")
		return ctrl.Result{}, nil
	}

	c, err := r.Installer.RegisterClusterInternal(ctx, &key, installer.RegisterClusterParams{
		NewClusterParams: &models.ClusterCreateParams{
			AdditionalNtpSource:      swag.String(spec.AdditionalNtpSource),
			BaseDNSDomain:            spec.BaseDNSDomain,
			ClusterNetworkCidr:       swag.String(spec.ClusterNetworkCidr),
			ClusterNetworkHostPrefix: spec.ClusterNetworkHostPrefix,
			HTTPProxy:                swag.String(spec.HTTPProxy),
			HTTPSProxy:               swag.String(spec.HTTPSProxy),
			IngressVip:               spec.IngressVip,
			Name:                     swag.String(spec.Name),
			NoProxy:                  swag.String(spec.NoProxy),
			OpenshiftVersion:         swag.String(spec.OpenshiftVersion),
			Operators:                nil, // TODO: handle operators
			PullSecret:               swag.String(pullSecret),
			ServiceNetworkCidr:       swag.String(spec.ServiceNetworkCidr),
			SSHPublicKey:             spec.SSHPublicKey,
			UserManagedNetworking:    swag.Bool(spec.UserManagedNetworking),
			VipDhcpAllocation:        swag.Bool(spec.VIPDhcpAllocation),
		},
	})
	// TODO: handle specific errors, 5XX retry, 4XX update status with the error
	return r.updateState(ctx, cluster, c, err)
}

func (r *ClusterReconciler) syncClusterState(cluster *adiiov1alpha1.Cluster, c *common.Cluster) {
	SetTimeIfNotNill := func(dst **metav1.Time, src strfmt.DateTime) {
		var defaultTime strfmt.DateTime
		if src != defaultTime {
			t := metav1.NewTime(time.Time(src))
			*dst = &t
		}
	}

	strfmtUUIDToStrings := func(uuidArr []strfmt.UUID) []string {
		out := make([]string, len(uuidArr))
		for i := range uuidArr {
			out[i] = uuidArr[i].String()
		}
		return out
	}

	cluster.Status.State = swag.StringValue(c.Status)
	cluster.Status.StateInfo = swag.StringValue(c.StatusInfo)
	if len(c.HostNetworks) > 0 {
		cluster.Status.HostNetworks = make([]adiiov1alpha1.HostNetwork, len(c.HostNetworks))
		for i, hn := range c.HostNetworks {
			cluster.Status.HostNetworks[i] = adiiov1alpha1.HostNetwork{
				Cidr:    hn.Cidr,
				HostIds: strfmtUUIDToStrings(hn.HostIds),
			}
		}
	}
	cluster.Status.Hosts = len(c.Hosts)
	if c.Progress != nil {
		cluster.Status.Progress = adiiov1alpha1.ClusterProgressInfo{
			ProgressInfo: c.Progress.ProgressInfo,
		}
		SetTimeIfNotNill(&cluster.Status.Progress.LastProgressUpdateTime, c.Progress.ProgressUpdatedAt)
	}
	cluster.Status.ValidationsInfo = c.ValidationsInfo
	cluster.Status.ConnectivityMajorityGroups = c.ConnectivityMajorityGroups
	SetTimeIfNotNill(&cluster.Status.LastUpdateTime, c.UpdatedAt)
	SetTimeIfNotNill(&cluster.Status.ControllerLogsCollectionTime, c.ControllerLogsCollectedAt)
	cluster.Status.ID = c.ID.String()
	cluster.Status.Error = ""
}

func (r *ClusterReconciler) updateState(ctx context.Context, cluster *adiiov1alpha1.Cluster, c *common.Cluster,
	err error) (ctrl.Result, error) {

	reply := ctrl.Result{}
	if c != nil {
		r.syncClusterState(cluster, c)
	}

	if err != nil {
		cluster.Status.Error = err.Error()
		reply.RequeueAfter = defaultRequeueAfterOnError
	}

	if err := r.Status().Update(ctx, cluster); err != nil {
		r.Log.WithError(err).Errorf("failed set state for %s %s", cluster.Name, cluster.Namespace)
		return ctrl.Result{Requeue: true}, nil
	}

	return reply, nil
}

func (r *ClusterReconciler) notifyPullSecretUpdate(ctx context.Context, isPullSecretUpdate bool, c *common.Cluster) error {
	if isPullSecretUpdate {
		images := &adiiov1alpha1.ImageList{}
		if err := r.List(ctx, images); err != nil {
			return err
		}
		for _, image := range images.Items {
			r.Log.Infof("Nofify that image %s should be re-created for cluster %s",
				image.Name, image.UID)
			if image.Spec.ClusterRef.Name == c.KubeKeyName {
				r.PullSecretUpdatesChannel <- event.GenericEvent{
					Meta: &metav1.ObjectMeta{
						Namespace: image.Namespace,
						Name:      image.Name,
					},
				}
			}
		}
	}
	return nil
}

func (r *ClusterReconciler) deregisterClusterIfNeeded(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {

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

	if gorm.IsRecordNotFoundError(err) {
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

func (r *ClusterReconciler) updateIfNeeded(ctx context.Context, cluster *adiiov1alpha1.Cluster, c *common.Cluster) (bool, ctrl.Result, error) {
	update := false
	isPullSecretUpdate := false

	params := &models.ClusterUpdateParams{}

	spec := cluster.Spec

	updateString := func(new, old string, target **string) {
		if new != old {
			*target = swag.String(new)
			update = true
		}
	}

	updatePString := func(new *string, old string, target **string) {
		if new != nil && *new != old {
			*target = new
			update = true
		}
	}

	updateString(spec.Name, c.Name, &params.Name)

	if spec.OpenshiftVersion != c.OpenshiftVersion {
		return false, ctrl.Result{}, errors.Errorf("Openshift version cannot be updated")
	}

	updateString(spec.BaseDNSDomain, c.BaseDNSDomain, &params.BaseDNSDomain)
	updateString(spec.ClusterNetworkCidr, c.ClusterNetworkCidr, &params.ClusterNetworkCidr)

	if spec.ClusterNetworkHostPrefix != c.ClusterNetworkHostPrefix {
		params.ClusterNetworkHostPrefix = swag.Int64(spec.ClusterNetworkHostPrefix)
		update = true
	}

	updateString(spec.ServiceNetworkCidr, c.ServiceNetworkCidr, &params.ServiceNetworkCidr)
	updateString(spec.APIVip, c.APIVip, &params.APIVip)
	updateString(spec.APIVipDNSName, swag.StringValue(c.APIVipDNSName), &params.APIVipDNSName)
	updateString(spec.IngressVip, c.IngressVip, &params.IngressVip)
	updatePString(spec.MachineNetworkCidr, c.MachineNetworkCidr, &params.MachineNetworkCidr)
	updateString(spec.SSHPublicKey, c.SSHPublicKey, &params.SSHPublicKey)

	if spec.VIPDhcpAllocation != swag.BoolValue(c.VipDhcpAllocation) {
		params.VipDhcpAllocation = swag.Bool(spec.VIPDhcpAllocation)
	}

	updateString(spec.HTTPProxy, c.HTTPProxy, &params.HTTPProxy)
	updateString(spec.HTTPSProxy, c.HTTPSProxy, &params.HTTPSProxy)
	updateString(spec.NoProxy, c.NoProxy, &params.NoProxy)

	if spec.UserManagedNetworking != swag.BoolValue(c.UserManagedNetworking) {
		params.UserManagedNetworking = swag.Bool(spec.UserManagedNetworking)
	}

	updateString(spec.AdditionalNtpSource, c.AdditionalNtpSource, &params.AdditionalNtpSource)

	// TODO: handle InstallConfigOverrides

	data, err := r.getPullSecret(ctx, spec.PullSecretRef.Name, spec.PullSecretRef.Namespace)
	if err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, "failed to get pull secret for update")
	}
	if data != c.PullSecret {
		params.PullSecret = swag.String(data)
		update = true
		isPullSecretUpdate = true
	}

	if !update {
		return update, ctrl.Result{}, nil
	}

	updatedCluster, err := r.Installer.UpdateClusterInternal(ctx, installer.UpdateClusterParams{
		ClusterUpdateParams: params,
		ClusterID:           *c.ID,
	}, nil, nil)

	// TODO: check error type, retry for 5xx
	if err != nil {
		return update, ctrl.Result{}, errors.Wrap(err, "failed to update cluster")
	}

	if err = r.notifyPullSecretUpdate(ctx, isPullSecretUpdate, c); err != nil {
		return false, ctrl.Result{}, errors.Wrap(err, "failed to get a list of images to update")
	}

	r.Log.Infof("Updated cluster %s %s", cluster.Name, cluster.Namespace)
	reply, err := r.updateState(ctx, cluster, updatedCluster, nil)
	return update, reply, err

}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapSecretToCluster := handler.ToRequestsFunc(
		func(a handler.MapObject) []reconcile.Request {
			clusters := &adiiov1alpha1.ClusterList{}
			if err := r.List(context.Background(), clusters); err != nil {
				return []reconcile.Request{}
			}
			reply := make([]reconcile.Request, 0, len(clusters.Items))
			for _, cluster := range clusters.Items {
				if cluster.Spec.PullSecretRef.Name == a.Meta.GetName() &&
					cluster.Spec.PullSecretRef.Namespace == a.Meta.GetNamespace() {
					reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: cluster.Namespace,
						Name:      cluster.Name,
					}})
				}
			}
			return reply
		})

	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.Cluster{}).
		Watches(&source.Kind{Type: &corev1.Secret{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: mapSecretToCluster}).
		Complete(r)
}
