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
	"net/http"

	hiveextv1beta2 "github.com/openshift/assisted-service/api/hiveextension/v1beta2"
	"github.com/openshift/assisted-service/internal/common"
	logutil "github.com/openshift/assisted-service/pkg/log"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// AgentClusterInstallReconciler reconciles a AgentClusterInstall object
type AgentClusterInstallReconciler struct {
	client.Client
	Log              logrus.FieldLogger
	CRDEventsHandler CRDEventsHandler
}

// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=agentclusterinstalls/status,verbs=get;update;patch

func (r *AgentClusterInstallReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"agent_cluster_install":           req.Name,
			"agent_cluster_install_namespace": req.Namespace,
		})

	defer func() {
		log.Info("AgentClusterInstall Reconcile ended")
	}()

	log.Info("AgentClusterInstall Reconcile started")

	// Retrieve AgentClusterInstall
	clusterInstall := &hiveextv1beta2.AgentClusterInstall{}
	if err := r.Get(ctx, req.NamespacedName, clusterInstall); err != nil {
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Retrieve ClusterDeployment
	clusterDeploymentKubeKey := types.NamespacedName{
		Namespace: clusterInstall.Namespace,
		Name:      clusterInstall.Spec.ClusterDeploymentRef.Name,
	}
	clusterDeployment := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, clusterDeploymentKubeKey, clusterDeployment); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.WithError(err).Error(fmt.Sprintf(
				"failed to get ClusterDeployment with name '%s' in namespace '%s'",
				clusterDeploymentKubeKey.Name, clusterDeploymentKubeKey.Namespace))
			return ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}, nil
		}

		apiErr := errors.New(fmt.Sprintf(
			"ClusterDeployment with name '%s' in namespace '%s' not found. Ensure that the CRD is already applied.",
			clusterDeploymentKubeKey.Name, clusterDeploymentKubeKey.Namespace))
		err = common.NewApiError(http.StatusBadRequest, apiErr)
		log.Error(err)

		// Set conditions
		clusterSpecSynced(clusterInstall, err)
		setClusterConditionsUnknown(clusterInstall)

		if updateErr := r.Status().Update(ctx, clusterInstall); updateErr != nil {
			log.WithError(updateErr).Error("failed to update AgentClusterInstall Status")
			return ctrl.Result{Requeue: true}, nil
		}
	}

	log.Info("AgentClusterInstall Reconcile successfully retrieved the associated ClusterDeployment")
	return ctrl.Result{}, nil
}

func (r *AgentClusterInstallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hiveextv1beta2.AgentClusterInstall{}).
		Watches(&hiveextv1beta2.AgentClusterInstall{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
