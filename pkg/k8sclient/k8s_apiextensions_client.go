package k8sclient

import (
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

//go:generate mockgen --build_flags=--mod=mod -package=k8sclient -destination=mock_k8s_apiextensions_client_factory.go . K8sApiExtensionsClientFactory
type K8sApiExtensionsClientFactory interface {
	CreateFromSecret(secret *corev1.Secret) (apiextensions.Interface, error)
	CreateFromInClusterConfig() (apiextensions.Interface, error)
}

type k8sApiExtensionsClientFactory struct {
	log logrus.FieldLogger
}

//go:generate mockgen --build_flags=--mod=mod -package=k8sclient -destination=mock_k8s_apiextensions_client.go . K8sApiExtensionsClient
type K8sApiExtensionsClient interface {
	apiextensions.Interface
}

func NewK8sApiExtensionsClientFactory(log logrus.FieldLogger) K8sApiExtensionsClientFactory {
	return &k8sApiExtensionsClientFactory{
		log: log,
	}
}

var getInClusterConfig = func() (*rest.Config, error) {
	return rest.InClusterConfig()
}

var getClientSet = func(restConfig *rest.Config) (*apiextensions.Clientset, error) {
	return apiextensions.NewForConfig(restConfig)
}

var getRestConfig = func(clientConfig clientcmd.ClientConfig) (*rest.Config, error) {
	return clientConfig.ClientConfig()
}

// Fetch external cluster rest.Config and create client (using kubeconfig specified in ASC)
func (c *k8sApiExtensionsClientFactory) CreateFromSecret(secret *corev1.Secret) (apiextensions.Interface, error) {
	restConfig, err := c.getRestConfigFromSecret(secret)
	if err != nil {
		c.log.WithError(err).Warnf("Getting client from kubeconfig cluster")
		return nil, err
	}
	kubeClient, err := getClientSet(restConfig)
	if err != nil {
		return nil, err
	}
	return kubeClient, err
}

// Fetch in-cluster rest.Config and create client
func (c *k8sApiExtensionsClientFactory) CreateFromInClusterConfig() (apiextensions.Interface, error) {
	restConfig, err := getInClusterConfig()
	if err != nil {
		c.log.WithError(err).Warnf("Failed getting config from kubeconfig cluster")
		return nil, err
	}
	kubeClient, err := getClientSet(restConfig)
	if err != nil {
		return nil, err
	}
	return kubeClient, err
}

func (c *k8sApiExtensionsClientFactory) getRestConfigFromSecret(secret *corev1.Secret) (*rest.Config, error) {
	if secret.Data == nil {
		return nil, errors.Errorf("Secret %s/%s does not contain any data", secret.Namespace, secret.Name)
	}
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		return nil, errors.Errorf("Secret data for %s/%s does not contain kubeconfig", secret.Namespace, secret.Name)
	}
	return c.getRestConfigFromKubeConfig(kubeconfigData)
}

func (c *k8sApiExtensionsClientFactory) getRestConfigFromKubeConfig(kubeconfig []byte) (*rest.Config, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data in secret")
	}
	restConfig, err := getRestConfig(clientConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}
	return restConfig, nil
}
