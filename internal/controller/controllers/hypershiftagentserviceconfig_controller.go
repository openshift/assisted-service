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
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	logutil "github.com/openshift/assisted-service/pkg/log"
	pkgerror "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	SpokeClients SpokeClientCache
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

	// Creating spoke client using specified kubeconfig secret reference
	// TODO: use spoke client to create CRDs/CRs
	_, err := hr.createSpokeClient(ctx, instance.Spec.KubeconfigSecretRef.Name, instance.Namespace)
	if err != nil {
		log.WithError(err).Error("Failed to create client using remote kubeconfig", req.NamespacedName)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (hr *HypershiftAgentServiceConfigReconciler) createSpokeClient(ctx context.Context, kubeconfigSecretName, namespace string) (spoke_k8s_client.SpokeK8sClient, error) {
	// Fetch kubeconfig secret by specified secret reference
	kubeconfigSecret, err := hr.getKubeconfigSecret(ctx, kubeconfigSecretName, namespace)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to get secret '%s' in '%s' namespace", kubeconfigSecretName, namespace)
	}

	// Create spoke cluster client using kubeconfig secret
	spokeClient, err := hr.SpokeClients.Get(kubeconfigSecret)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to create client using kubeconfig secret '%s'", kubeconfigSecretName)
	}

	return spokeClient, nil
}

// Return kubeconfig secret by name and namespace
func (hr *HypershiftAgentServiceConfigReconciler) getKubeconfigSecret(ctx context.Context, kubeconfigSecretName, namespace string) (*corev1.Secret, error) {
	secretRef := types.NamespacedName{Namespace: namespace, Name: kubeconfigSecretName}
	secret, err := getSecret(ctx, hr.Client, hr, secretRef)
	if err != nil {
		return nil, pkgerror.Wrapf(err, "Failed to get '%s' secret in '%s' namespace", secretRef.Name, secretRef.Namespace)
	}
	_, ok := secret.Data["kubeconfig"]
	if !ok {
		return nil, pkgerror.Errorf("Secret '%s' does not contain '%s' key value", secretRef.Name, "kubeconfig")
	}
	return secret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (hr *HypershiftAgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.HypershiftAgentServiceConfig{}).
		Complete(hr)
}
