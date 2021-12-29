package kubeclients

import (
	"context"
	"fmt"
	"time"

	"github.com/ReneKroon/ttlcache"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	cerv1 "k8s.io/client-go/kubernetes/typed/certificates/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

//go:generate mockgen -package=kubeclients -destination=mock_kubeclients.go . Kubeclients
type Kubeclients interface {
	GetClusterKubeClient(ctx context.Context, clusterId string) ClusterKubeClient
}

//go:generate mockgen -package=kubeclients -destination=mock_cluster_kubeclient.go . ClusterKubeClient
type ClusterKubeClient interface {
	ApproveAllCsrs() error
	ListNodes() (*corev1.NodeList, error)
}

type kubeclients struct {
	cache         *ttlcache.Cache
	objectHandler s3wrapper.API
	log           logrus.FieldLogger
}

type clusterKubeClient struct {
	csrClient   cerv1.CertificateSigningRequestInterface
	nodesClient typedcorev1.NodeInterface
	log         logrus.FieldLogger
}

func (k *kubeclients) getKubconfigForCluster(ctx context.Context, clusterId string) ([]byte, error) {
	respBody, contentLength, err := k.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterId, constants.Kubeconfig))
	if err != nil {
		return nil, err
	}
	//goland:noinspection GoUnhandledErrorResult
	defer respBody.Close()
	kubeconfigData := make([]byte, contentLength)
	for location := 0; location < int(contentLength); {
		n, err := respBody.Read(kubeconfigData[location:])
		if err != nil {
			return nil, err
		}
		location += n
	}
	return kubeconfigData, nil
}

func getClientConfigFromKubeconfig(kubeconfigData []byte) (*rest.Config, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data")
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}
	return restConfig, nil
}

func (k *kubeclients) getOrAllocate(ctx context.Context, clusterId string) *clusterKubeClient {
	clientsIf, exists := k.cache.Get(clusterId)
	if exists {
		if clientsIf == nil {
			return nil
		}
		return clientsIf.(*clusterKubeClient)
	}
	kubeconfigData, err := k.getKubconfigForCluster(ctx, clusterId)
	if err != nil {
		k.log.WithError(err).Warnf("Couldn't get kubeconfig for cluster %s", clusterId)
		k.cache.Set(clusterId, nil)
		return nil
	}
	clientConfig, err := getClientConfigFromKubeconfig(kubeconfigData)
	if err != nil {
		k.log.WithError(err).Warnf("Getting client from kubeconfig cluster %s", clusterId)
		k.cache.Set(clusterId, nil)
		return nil
	}
	config, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		k.log.WithError(err).Warnf("Getting kuberenetes config for cluster %s", clusterId)
		k.cache.Set(clusterId, nil)
		return nil
	}
	data := clusterKubeClient{
		csrClient:   config.CertificatesV1().CertificateSigningRequests(),
		nodesClient: config.CoreV1().Nodes(),
		log:         k.log,
	}
	k.cache.Set(clusterId, &data)
	return &data
}

func (k *kubeclients) GetClusterKubeClient(ctx context.Context, clusterId string) ClusterKubeClient {
	return k.getOrAllocate(ctx, clusterId)
}

func NewKubeclients(objectHandler s3wrapper.API, log logrus.FieldLogger) Kubeclients {
	cache := ttlcache.NewCache()
	cache.SetTTL(30 * time.Minute)
	return &kubeclients{
		cache:         cache,
		objectHandler: objectHandler,
		log:           log,
	}
}

func isCsrApproved(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
	}
	return false
}

func (c *clusterKubeClient) ApproveAllCsrs() error {
	csrList, err := c.csrClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range csrList.Items {
		csr := &csrList.Items[i]
		if !isCsrApproved(csr) {
			csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
				Type:           certificatesv1.CertificateApproved,
				Reason:         "NodeCSRApprove",
				Message:        "This CSR was approved by the assisted-service",
				Status:         corev1.ConditionTrue,
				LastUpdateTime: metav1.Now(),
			})
			if _, err := c.csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{}); err != nil {
				c.log.WithError(err).Errorf("Failed to approve CSR %v", csr)
				return err
			}
		}
	}
	return nil
}

func (c *clusterKubeClient) ListNodes() (*corev1.NodeList, error) {
	return c.nodesClient.List(context.TODO(), metav1.ListOptions{})
}
