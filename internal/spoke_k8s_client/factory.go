package spoke_k8s_client

import (
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client_factory.go . SpokeK8sClientFactory
type SpokeK8sClientFactory interface {
	CreateFromSecret(secret *corev1.Secret) (SpokeK8sClient, error)
	ClientAndSetFromSecret(secret *corev1.Secret) (SpokeK8sClient, *kubernetes.Clientset, error)
}

type spokeK8sClientFactory struct {
	log logrus.FieldLogger
}

func NewSpokeK8sClientFactory(log logrus.FieldLogger) SpokeK8sClientFactory {
	return &spokeK8sClientFactory{
		log: log,
	}
}

func (cf *spokeK8sClientFactory) CreateFromSecret(secret *corev1.Secret) (SpokeK8sClient, error) {
	kubeconfigData, err := kubeconfigFromSecret(secret)
	if err != nil {
		return nil, err
	}
	client, _, err := cf.clientAndSetForKubeconfig(kubeconfigData)
	return client, err
}

func (cf *spokeK8sClientFactory) ClientAndSetFromSecret(secret *corev1.Secret) (SpokeK8sClient, *kubernetes.Clientset, error) {
	kubeconfig, err := kubeconfigFromSecret(secret)
	if err != nil {
		cf.log.WithError(err).Error("failed to get kubeconfig from secret")
		return nil, nil, err
	}

	return cf.clientAndSetForKubeconfig(kubeconfig)
}

func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	if secret.Data == nil {
		return nil, errors.Errorf("Secret %s/%s does not contain any data", secret.Namespace, secret.Name)
	}
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		return nil, errors.Errorf("Secret data for %s/%s does not contain kubeconfig", secret.Namespace, secret.Name)
	}
	return kubeconfigData, nil
}

func (cf *spokeK8sClientFactory) clientAndSetForKubeconfig(kubeconfig []byte) (SpokeK8sClient, *kubernetes.Clientset, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data")
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting kuberenetes config for cluster")
		return nil, nil, err
	}

	schemes := GetKubeClientSchemes()
	targetClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		cf.log.WithError(err).Warnf("failed to get spoke kube client")
		return nil, nil, err
	}

	spokeClient := &spokeK8sClient{
		Client:      targetClient,
		csrClient:   clientset.CertificatesV1().CertificateSigningRequests(),
		sarClient:   clientset.AuthorizationV1().SelfSubjectAccessReviews(),
		nodesClient: clientset.CoreV1().Nodes(),
		log:         cf.log,
	}
	return spokeClient, clientset, nil
}
