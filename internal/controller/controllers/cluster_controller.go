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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Log       logrus.FieldLogger
	Scheme    *runtime.Scheme
	Installer bminventory.InstallerInternals
}

// +kubebuilder:rbac:groups=adi.io.my.domain,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=adi.io.my.domain,resources=clusters/status,verbs=get;update;patch

func (r *ClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	cluster := &adiiov1alpha1.Cluster{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.Wrap(err, "failed to get cluster data")
	}

	// check if new cluster
	if cluster.Status.ID == "" {
		return r.createNewCluster(ctx, req.NamespacedName, cluster)
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) getPullSecret(ctx context.Context, name, namespace string) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	if err := r.Get(ctx, key, secret); err != nil {
		return "", errors.Wrap(err, "to get pull secret")
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

	spec := cluster.Spec

	pullSecret, err := r.getPullSecret(ctx, spec.PullSecretRef.Name, spec.PullSecretRef.Namespace)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get pull secret")
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
	if err != nil {
		cluster.Status.Error = err.Error()
		if err = r.Update(ctx, cluster); err != nil {
			r.Log.WithError(err).Error("failed to update error status")
		}
		return ctrl.Result{}, errors.Wrap(err, "failed to create cluster")
	}

	return r.updateStatus(ctx, cluster, c)
}

func (r *ClusterReconciler) updateStatus(ctx context.Context, cluster *adiiov1alpha1.Cluster, c *common.Cluster) (ctrl.Result, error) {
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

	if err := r.Status().Update(ctx, cluster); err != nil {
		r.Log.WithError(err).Error("failed to update error status")
		return ctrl.Result{}, errors.Wrap(err, "failed to create cluster")
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adiiov1alpha1.Cluster{}).
		Complete(r)
}
