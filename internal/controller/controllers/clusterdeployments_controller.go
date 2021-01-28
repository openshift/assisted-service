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

	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/host"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// ClusterReconciler reconciles a Cluster object
type ClusterDeploymentsReconciler struct {
	client.Client
	Log                      logrus.FieldLogger
	Scheme                   *runtime.Scheme
	Installer                bminventory.InstallerInternals
	ClusterApi               cluster.API
	HostApi                  host.API
	PullSecretUpdatesChannel chan event.GenericEvent
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/status,verbs=get;update;patch

func (r *ClusterDeploymentsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	cluster := &hivev1.ClusterDeployment{}
	err := r.Get(ctx, req.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ClusterDeploymentsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hivev1.ClusterDeployment{}).
		Complete(r)
}
