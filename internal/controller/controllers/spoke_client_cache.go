package controllers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

//go:generate mockgen --build_flags=--mod=mod -package=controllers -destination=mock_spoke_client_cache.go . SpokeClientCache
type SpokeClientCache interface {
	Get(clusterDeployment *hivev1.ClusterDeployment, secret *corev1.Secret) (spoke_k8s_client.SpokeK8sClient, error)
}

type spokeClientCache struct {
	sync.Mutex
	clientFactory spoke_k8s_client.SpokeK8sClientFactory
	clientMap     map[string]*spokeClient
}

type spokeClient struct {
	spokeK8sClient *spoke_k8s_client.SpokeK8sClient
	secretHash     string
}

func NewSpokeClientCache(clientFactory spoke_k8s_client.SpokeK8sClientFactory) SpokeClientCache {
	return &spokeClientCache{
		clientFactory: clientFactory,
		clientMap:     map[string]*spokeClient{},
	}
}

// Get returns a SpokeK8sClient for the given secret.
// The client is returned from cache, or, a new client is created if not available.
func (c *spokeClientCache) Get(clusterDeployment *hivev1.ClusterDeployment,
	secret *corev1.Secret) (spoke_k8s_client.SpokeK8sClient, error) {
	c.Lock()
	defer c.Unlock()

	// Compute a hash for the content of the secret:
	secretHash, err := c.calculateSecretHash(secret)
	if err != nil {
		return nil, err
	}

	// Get client from cache or create a new one if not available
	key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
	client, present := c.clientMap[key.String()]
	if !present || client.secretHash != secretHash {
		spokeK8sClient, err := c.clientFactory.CreateFromSecret(clusterDeployment, secret)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to create client using secret '%s'", secret.Name)
		}
		client = &spokeClient{
			spokeK8sClient: &spokeK8sClient,
			secretHash:     secretHash,
		}
		c.clientMap[key.String()] = client
	}

	return *client.spokeK8sClient, nil
}

func (c *spokeClientCache) calculateSecretHash(secret *corev1.Secret) (string, error) {
	data, err := json.Marshal(secret.Data)
	if err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", sha256.New().Sum(data))
	return hash, nil
}
