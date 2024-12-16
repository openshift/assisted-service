package spoke_k8s_client

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client_factory.go . SpokeK8sClientFactory
type SpokeK8sClientFactory interface {
	CreateFromSecret(ctx context.Context, secret *corev1.Secret) (SpokeK8sClient, error)
	ClientAndSetFromSecret(ctx context.Context, secret *corev1.Secret) (SpokeK8sClient, *kubernetes.Clientset, error)
}

// SpokeK8sClientFactoryBuilder contains the data and logic needed to create a spoke client factory. Don't create
// instances of this directly, use the NewFactory function instead.
type SpokeK8sClientFactoryBuilder struct {
	logger            logrus.FieldLogger
	hubClient         ctrlclient.Client
	transportWrappers []func(http.RoundTripper) http.RoundTripper
}

type spokeK8sClientFactory struct {
	logger            logrus.FieldLogger
	hubClient         ctrlclient.Client
	transportWrappers []func(http.RoundTripper) http.RoundTripper
}

// NewFactory creates a builder that can then be used to configure and create a spoke client factory.
func NewFactory() *SpokeK8sClientFactoryBuilder {
	return &SpokeK8sClientFactoryBuilder{}
}

// SetLogger sets the object that will be used to write to the log. This is mandatory.
func (b *SpokeK8sClientFactoryBuilder) SetLogger(value logrus.FieldLogger) *SpokeK8sClientFactoryBuilder {
	b.logger = value
	return b
}

// SetHubClient sets the client that will be used to call the API of the hub cluster. This is mandatory.
func (b *SpokeK8sClientFactoryBuilder) SetHubClient(value ctrlclient.Client) *SpokeK8sClientFactoryBuilder {
	b.hubClient = value
	return b
}

// AddTransportWrapper adds a function that will be called to potentially modify the original HTTP transport. This is
// optional and by default there are no wrappers.
func (b *SpokeK8sClientFactoryBuilder) AddTransportWrapper(value func(
	http.RoundTripper) http.RoundTripper) *SpokeK8sClientFactoryBuilder {
	b.transportWrappers = append(b.transportWrappers, value)
	return b
}

// Build uses the data stored in the builder to create and configure a new factory.
func (b *SpokeK8sClientFactoryBuilder) Build() (SpokeK8sClientFactory, error) {
	// Check parameters:
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.hubClient == nil {
		return nil, errors.New("hub client is mandatory")
	}

	// Create and populate the object:
	result := &spokeK8sClientFactory{
		logger:            b.logger,
		hubClient:         b.hubClient,
		transportWrappers: slices.Clone(b.transportWrappers),
	}
	return result, nil
}

func (f *spokeK8sClientFactory) CreateFromSecret(ctx context.Context, secret *corev1.Secret) (SpokeK8sClient, error) {
	client, _, err := f.ClientAndSetFromSecret(ctx, secret)
	return client, err
}

func (f *spokeK8sClientFactory) ClientAndSetFromSecret(ctx context.Context, secret *corev1.Secret) (SpokeK8sClient,
	*kubernetes.Clientset, error) {
	// Create the REST configuration from the content of the secret:
	restConfig, err := f.restConfigFromSecret(secret)
	if err != nil {
		return nil, nil, err
	}

	// If the secret is from a hosted cluster then the API server address can be replaced by a
	// 'kube-apiserver.*.svc' address that uses the service network and therefore reduces the number of round trips
	// and doesn't need to go via proxies.
	isHostedCluster, clusterDeploymentKey, err := f.isFromHostedCluster(ctx, secret)
	if err != nil {
		return nil, nil, err
	}
	if isHostedCluster {
		err = f.modifyRestConfigForHostedCluster(clusterDeploymentKey, restConfig)
		if err != nil {
			return nil, nil, err
		}
	}

	// Add the transport wrappers:
	for _, transportWrapper := range f.transportWrappers {
		restConfig.Wrap(transportWrapper)
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

// isFromHostedCluster determines if this cluster corresponding to the given kubeconfig secret is a hosted cluster.
// Returns a boolean flag and, when that flag is true, the namespace and name of the cluster deployment of the hosted
// cluster.
func (f *spokeK8sClientFactory) isFromHostedCluster(ctx context.Context, kubeconfigSecret *corev1.Secret) (bool,
	types.NamespacedName, error) {
	logger := f.logger.WithFields(logrus.Fields{
		"namespace": kubeconfigSecret.Namespace,
		"name":      kubeconfigSecret.Name,
	})

	// Try to find the cluster deployment. If we can't, for whatever the reason, explain it in the log and assume
	// it isn't a hosted cluster.
	clusterDeployment, err := f.findClusterDeploymentForKubeconfigSecret(ctx, kubeconfigSecret)
	if err != nil {
		logger.WithError(err).Warn(
			"Failed to find cluster deployment corresponding to kubeconfig secret, will assume it isn't " +
				"a hosted cluster",
		)
		return false, types.NamespacedName{}, err
	}
	if clusterDeployment == nil {
		logger.Warn(
			"Cluster deployment corresponding to kubeconfig secret doesn't exist, will assume it isn't " +
				"a hosted cluster",
		)
		return false, types.NamespacedName{}, err
	}

	// We assume this is a hosted cluster if it has the 'agentClusterRef' label, no matter the value.
	_, result := clusterDeployment.Labels["agentClusterRef"]
	var clusterDeploymentKey types.NamespacedName
	if result {
		clusterDeploymentKey.Namespace = clusterDeployment.Namespace
		clusterDeploymentKey.Name = clusterDeployment.Name
	}
	return result, clusterDeploymentKey, nil
}

// findClusterDeploymentForKubeconfigSecret finds the cluster deployment that corresponds to the given kubeconfig
// secret. It returns nil if there is no such cluster deployment.
func (f *spokeK8sClientFactory) findClusterDeploymentForKubeconfigSecret(ctx context.Context,
	kubeconfigSecret *corev1.Secret) (*hivev1.ClusterDeployment, error) {
	// The cluster deployment should be in the same namespace than the secret, because the cluster deployment
	// references the secret from the 'spec.clusterMetadata.adminKubeconfigSecretRef' field, and that is a local
	// object reference. So to find the cluster deployment we can get all the instances inside the namespace of the
	// secret and then select the first one that references it.
	clusterDeploymentList := &hivev1.ClusterDeploymentList{}
	err := f.hubClient.List(ctx, clusterDeploymentList, ctrlclient.InNamespace(kubeconfigSecret.Namespace), ctrlclient.MatchingFields{})
	if err != nil {
		err = errors.Wrapf(
			err,
			"failed to find cluster deployment correspondig to kubeconfig secret '%s/%s'",
			kubeconfigSecret.Namespace, kubeconfigSecret.Name,
		)
		return nil, err
	}
	for _, clusterDeployment := range clusterDeploymentList.Items {
		clusterDeploymentMetadata := clusterDeployment.Spec.ClusterMetadata
		if clusterDeploymentMetadata == nil {
			continue
		}
		if clusterDeploymentMetadata.AdminKubeconfigSecretRef.Name == kubeconfigSecret.Name {
			return &clusterDeployment, nil
		}
	}
	return nil, nil
}

func (f *spokeK8sClientFactory) modifyRestConfigForHostedCluster(clusterDeploymentKey types.NamespacedName,
	restConfig *rest.Config) error {
	originalHost := restConfig.Host
	restConfig.Host = fmt.Sprintf("https://kube-apiserver.%s.svc:6443", clusterDeploymentKey.Namespace)
	f.logger.WithFields(logrus.Fields{
		"namespace": clusterDeploymentKey.Namespace,
		"name":      clusterDeploymentKey.Name,
		"original":  originalHost,
		"modified":  restConfig.Host,
	}).Info("Modified hosted cluster API server address to connect via the service network")
	return nil
}
