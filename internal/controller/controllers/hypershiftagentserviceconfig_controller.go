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
	"fmt"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	logutil "github.com/openshift/assisted-service/pkg/log"
	pkgerror "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	// Namespace the operator is running in
	Namespace string

	SpokeClients SpokeClientCache
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=hypershiftagentserviceconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

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
	spokeClient, err := hr.createSpokeClient(ctx, instance.Spec.KubeconfigSecretRef.Name, instance.Namespace)
	if err != nil {
		log.WithError(err).Error("Failed to create client using specified kubeconfig", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Ensure agent-install CRDs exist on spoke cluster and clean stale ones
	if err := hr.syncSpokeAgentInstallCRDs(ctx, spokeClient); err != nil {
		log.WithError(err).Error(fmt.Sprintf("Failed to sync agent-install CRDs on spoke cluster in namespace '%s'", instance.Namespace))
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

func (hr *HypershiftAgentServiceConfigReconciler) syncSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client) error {
	// Fetch agent-install CRDs using in-cluster client
	localCRDs, err := hr.getAgentInstallCRDs(ctx, hr.Client)
	if err != nil {
		return err
	}
	if len(localCRDs.Items) == 0 {
		return pkgerror.New("agent-install CRDs are not available")
	}

	// Ensure local agent-install CRDs exist on the spoke cluster
	if err := hr.ensureSpokeAgentInstallCRDs(ctx, spokeClient, localCRDs); err != nil {
		return pkgerror.New("Failed to create agent-install CRDs on spoke cluster")
	}

	// Delete agent-install CRDs that don't exist locally
	if err := hr.cleanStaleSpokeAgentInstallCRDs(ctx, spokeClient, localCRDs); err != nil {
		hr.Log.WithError(err).Warn("Failed to remove stale agent-install CRDs from spoke cluster in namespace")
		return nil
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) ensureSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client, localCRDs *apiextensionsv1.CustomResourceDefinitionList) error {
	// Ensure CRDs exist on the spoke cluster
	for _, item := range localCRDs.Items {
		crd := item
		c := crd.DeepCopy()
		c.ResourceVersion = "" // ResourceVersion should not be set on objects to be created
		mutate := func() error {
			c.Spec = crd.Spec
			c.Annotations = crd.Annotations
			c.Labels = crd.Labels
			return nil
		}
		result, err := controllerutil.CreateOrUpdate(ctx, spokeClient, c, mutate)
		if err != nil {
			return pkgerror.Wrapf(err, "Failed to create CRD '%s' on spoke cluster", crd.Name)
		}
		if result != controllerutil.OperationResultNone {
			hr.Log.Infof("CRD '%s' %s on spoke cluster", crd.Name, result)
		}
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) cleanStaleSpokeAgentInstallCRDs(ctx context.Context, spokeClient client.Client, localCRDs *apiextensionsv1.CustomResourceDefinitionList) error {
	// Fetch agent-install CRDs using spoke client
	spokeCRDs, err := hr.getAgentInstallCRDs(ctx, spokeClient)
	if err != nil {
		return err
	}

	// Remove spoke CRDs that don't exist locally in-cluster
	for _, item := range spokeCRDs.Items {
		crd := item
		existsInCluster := funk.Contains(localCRDs.Items, func(localCRD apiextensionsv1.CustomResourceDefinition) bool {
			return localCRD.Name == crd.Name
		})
		if !existsInCluster {
			if err := spokeClient.Delete(ctx, crd.DeepCopy()); err != nil {
				hr.Log.WithError(err).Warnf("Failed to delete CRD '%s' from spoke cluster", crd.Name)
			}
		}
	}

	return nil
}

func (hr *HypershiftAgentServiceConfigReconciler) getAgentInstallCRDs(ctx context.Context, c client.Client) (*apiextensionsv1.CustomResourceDefinitionList, error) {
	crds := &apiextensionsv1.CustomResourceDefinitionList{}
	listOpts := []client.ListOption{
		client.HasLabels{fmt.Sprintf("operators.coreos.com/assisted-service-operator.%s", hr.Namespace)},
	}
	if err := c.List(ctx, crds, listOpts...); err != nil {
		return nil, pkgerror.Wrap(err, "Failed to list CRDs")
	}
	return crds, nil
}

// SetupWithManager sets up the controller with the Manager.
func (hr *HypershiftAgentServiceConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.HypershiftAgentServiceConfig{}).
		Complete(hr)
}
