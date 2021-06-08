package controllers

import (
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getSpokeClient(secret *corev1.Secret) (client.Client, error) {
	if secret.Data == nil {
		return nil, errors.Errorf("Secret %s/%s  does not contain any data", secret.Namespace, secret.Name)
	}
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		return nil, errors.Errorf("Secret data for %s/%s  does not contain kubeconfig", secret.Namespace, secret.Name)
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data in secret")
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for spoke kube client")
	}

	schemes := GetKubeClientSchemes()
	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get spoke kube client")
	}
	return targetClient, nil
}
