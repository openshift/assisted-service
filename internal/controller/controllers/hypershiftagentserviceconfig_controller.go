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
	_ "embed"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// HypershiftAgentServiceConfigReconciler reconciles a HypershiftAgentServiceConfig object
type HypershiftAgentServiceConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logrus.FieldLogger

	// selector and tolerations the Operator runs in and propagates to its deployments
	// on the management cluster
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs/status,verbs=get;update;patch

func (hr *HypershiftAgentServiceConfigReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, hr.Log).WithFields(
		logrus.Fields{
			"hypershift_service_config":    req.Name,
			"hypershift_service_namespace": req.Namespace,
		})

	log.Info("HypershiftAgentServiceConfig Reconcile started")
	defer log.Info("HypershiftAgentServiceConfig Reconcile ended")

	//read the resource from k8s
	instance := &aiv1beta1.HypershiftAgentServiceConfig{}
	if err := hr.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (hr *HypershiftAgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.HypershiftAgentServiceConfig{}).
		Complete(hr)
}
