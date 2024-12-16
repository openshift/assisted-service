package spoke_k8s_client

import (
	"fmt"
	"net/http"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client_factory.go . SpokeK8sClientFactory
type SpokeK8sClientFactory interface {
	CreateFromSecret(deployment *hivev1.ClusterDeployment, secret *corev1.Secret) (SpokeK8sClient, error)
	ClientAndSetFromSecret(deployment *hivev1.ClusterDeployment, secret *corev1.Secret) (SpokeK8sClient, *kubernetes.Clientset, error)
}

type spokeK8sClientFactory struct {
	logger           logrus.FieldLogger
	transportWrapper func(http.RoundTripper) http.RoundTripper
}

// NewFactory creates a spoke client factory. The logger and the hubClient are mandatory, the transport wrappers are
// optional.
func NewFactory(logger logrus.FieldLogger,
	transportWrapper func(http.RoundTripper) http.RoundTripper) (SpokeK8sClientFactory, error) {
	// Check parameters:
	if logger == nil {
		return nil, errors.New("logger is mandatory")
	}

	// Create and populate the object:
	result := &spokeK8sClientFactory{
		logger:           logger,
		transportWrapper: transportWrapper,
	}
	return result, nil
}

func (f *spokeK8sClientFactory) CreateFromSecret(deployment *hivev1.ClusterDeployment,
	secret *corev1.Secret) (SpokeK8sClient, error) {
	client, _, err := f.ClientAndSetFromSecret(deployment, secret)
	return client, err
}

func (f *spokeK8sClientFactory) ClientAndSetFromSecret(deployment *hivev1.ClusterDeployment,
	secret *corev1.Secret) (SpokeK8sClient, *kubernetes.Clientset, error) {
	// Create the REST configuration from the content of the secret:
	restConfig, err := f.restConfigFromSecret(secret)
	if err != nil {
		return nil, nil, err
	}

	// If the secret is from a hosted cluster then the API server address can be replaced by a
	// 'kube-apiserver.*.svc' address that uses the service network and therefore reduces the number of round trips
	// and doesn't need to go via proxies.
	if f.isHostedCluster(deployment) {
		err = f.modifyRestConfigForHostedCluster(deployment, restConfig)
		if err != nil {
			return nil, nil, err
		}
	}

	// Add the transport wrapper:
	if f.transportWrapper != nil {
		restConfig.Wrap(f.transportWrapper)
	}

	// Create the controller-runtime client:
	client, err := f.clientFromRestConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	// Create the client-go client:
	clientSet, err := f.clientSetFromRestConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	// Create and populate the object:
	result := &spokeK8sClient{
		logger:      f.logger,
		Client:      client,
		csrClient:   clientSet.CertificatesV1().CertificateSigningRequests(),
		sarClient:   clientSet.AuthorizationV1().SelfSubjectAccessReviews(),
		nodesClient: clientSet.CoreV1().Nodes(),
	}
	return result, clientSet, nil
}

func (f *spokeK8sClientFactory) restConfigFromSecret(secret *corev1.Secret) (*rest.Config, error) {
	kubeConfig, err := f.kubeConfigFromSecret(secret)
	if err != nil {
		return nil, err
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeConfig)
	if err != nil {
		return nil, err
	}
	return clientConfig.ClientConfig()
}

func (f *spokeK8sClientFactory) kubeConfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	if len(secret.Data) == 0 {
		err := errors.Errorf("secret '%s/%s' is empty", secret.Namespace, secret.Name)
		return nil, err
	}
	result, ok := secret.Data["kubeconfig"]
	if !ok || len(result) == 0 {
		err := errors.Errorf(
			"secret '%s/%s' doesn't contain the 'kubeconfig' key",
			secret.Namespace, secret.Name,
		)
		return nil, err
	}
	return result, nil
}

func (f *spokeK8sClientFactory) clientFromRestConfig(restConfig *rest.Config) (ctrlclient.Client, error) {
	schemes := GetKubeClientSchemes()
	return ctrlclient.New(restConfig, ctrlclient.Options{Scheme: schemes})
}

func (f *spokeK8sClientFactory) clientSetFromRestConfig(restConfig *rest.Config) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(restConfig)
}

// isHostedCluster determines if the given cluster deployment belongs to a hosted cluster.
func (f *spokeK8sClientFactory) isHostedCluster(deployment *hivev1.ClusterDeployment) bool {
	// We assume that it isn't a hosted cluster if the caller didn't pass a cluster deployment:
	if deployment == nil {
		return false
	}

	// We assume this is a hosted cluster if it has the 'agentClusterRef' label, no matter the value.
	_, result := deployment.Labels["agentClusterRef"]
	return result
}

func (f *spokeK8sClientFactory) modifyRestConfigForHostedCluster(deployment *hivev1.ClusterDeployment,
	restConfig *rest.Config) error {
	originalHost := restConfig.Host
	restConfig.Host = fmt.Sprintf("https://kube-apiserver.%s.svc:6443", deployment.Namespace)
	f.logger.WithFields(logrus.Fields{
		"namespace": deployment.Namespace,
		"name":      deployment.Name,
		"original":  originalHost,
		"modified":  restConfig.Host,
	}).Info("Modified hosted cluster API server address to connect via the service network")
	return nil
}
