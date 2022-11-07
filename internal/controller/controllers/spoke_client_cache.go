package controllers

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

//go:generate mockgen --build_flags=--mod=mod -package=controllers -destination=mock_spoke_client_cache.generated_go . SpokeClientCache
type SpokeClientCache interface {
	Get(secret *corev1.Secret) (spoke_k8s_client.SpokeK8sClient, error)
}

type spokeClientCache struct {
	sync.Mutex
	clientFactory spoke_k8s_client.SpokeK8sClientFactory
	clientMap     map[string]*spokeClient
}

type spokeClient struct {
	spokeK8sClient *spoke_k8s_client.SpokeK8sClient
	kubeconfigHash string
}

func NewSpokeClientCache(clientFactory spoke_k8s_client.SpokeK8sClientFactory) SpokeClientCache {
	return &spokeClientCache{
		clientFactory: clientFactory,
		clientMap:     make(map[string]*spokeClient),
	}
}

// Get returns a SpokeK8sClient for the given secret.
// The client is returned from cache, or, a new client is created if not available.
func (c *spokeClientCache) Get(secret *corev1.Secret) (spoke_k8s_client.SpokeK8sClient, error) {
	c.Lock()
	defer c.Unlock()

	// Get kubeconfig data and compute hash
	kubeconfigData, err := c.getKubeconfigFromSecret(secret)
	if err != nil {
		return nil, err
	}
	kubeconfigHash := fmt.Sprintf("%x", sha256.New().Sum(kubeconfigData))

	// Get client from cache or create a new one if not available
	key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
	client, present := c.clientMap[key.String()]
	if !present || client.kubeconfigHash != kubeconfigHash {
		spokeK8sClient, err := c.clientFactory.CreateFromRawKubeconfig(kubeconfigData)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to create client using secret '%s'", secret.Name)
		}
		client = &spokeClient{
			spokeK8sClient: &spokeK8sClient,
			kubeconfigHash: kubeconfigHash,
		}
		c.clientMap[key.String()] = client
	}

	return *client.spokeK8sClient, nil
}

func (c *spokeClientCache) getKubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	if secret.Data == nil {
		return nil, errors.Errorf("Secret %s/%s does not contain any data", secret.Namespace, secret.Name)
	}

	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		return nil, errors.Errorf("Secret data for %s/%s does not contain kubeconfig", secret.Namespace, secret.Name)
	}

	return kubeconfigData, nil
}
