package controllers

import (
	"context"

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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -package=controllers -destination=mock_spoke_k8s_client_factory.go . SpokeK8sClientFactory
type SpokeK8sClientFactory interface {
	Create(secret *corev1.Secret) (SpokeK8sClient, error)
}

//go:generate mockgen -package=controllers -destination=mock_spoke_k8s_client.go . SpokeK8sClient
type SpokeK8sClient interface {
	client.Client
	ListCsrs() (*certificatesv1.CertificateSigningRequestList, error)
	ApproveCsr(csr *certificatesv1.CertificateSigningRequest) error
	GetNode(name string) (*corev1.Node, error)
}

type spokeK8sClient struct {
	client.Client
	csrClient   cerv1.CertificateSigningRequestInterface
	nodesClient typedcorev1.NodeInterface
	log         logrus.FieldLogger
}

type spokeK8sClientFactory struct {
	log logrus.FieldLogger
}

func NewSpokeK8sClientFactory(log logrus.FieldLogger) SpokeK8sClientFactory {
	return &spokeK8sClientFactory{
		log: log,
	}
}

func (cf *spokeK8sClientFactory) getRestConfig(secret *corev1.Secret) (*rest.Config, error) {
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
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	return restConfig, nil
}

func (cf *spokeK8sClientFactory) Create(secret *corev1.Secret) (SpokeK8sClient, error) {
	clientConfig, err := cf.getRestConfig(secret)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting client from kubeconfig cluster")
		return nil, err
	}
	config, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting kuberenetes config for cluster")
		return nil, err
	}
	schemes := GetKubeClientSchemes()
	targetClient, err := client.New(clientConfig, client.Options{Scheme: schemes})
	if err != nil {
		cf.log.WithError(err).Warnf("failed to get spoke kube client")
		return nil, err
	}
	data := spokeK8sClient{
		Client:      targetClient,
		csrClient:   config.CertificatesV1().CertificateSigningRequests(),
		nodesClient: config.CoreV1().Nodes(),
		log:         cf.log,
	}
	return &data, nil
}

func (c *spokeK8sClient) ListCsrs() (*certificatesv1.CertificateSigningRequestList, error) {
	return c.csrClient.List(context.TODO(), metav1.ListOptions{})
}

func (c *spokeK8sClient) ApproveCsr(csr *certificatesv1.CertificateSigningRequest) error {
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Reason:         "NodeCSRApprove",
		Message:        "This CSR was approved by the assisted-service",
		Status:         corev1.ConditionTrue,
		LastUpdateTime: metav1.Now(),
	})
	_, err := c.csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
	return err
}

func (c *spokeK8sClient) GetNode(name string) (*corev1.Node, error) {
	node, err := c.nodesClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		node = nil
	}
	return node, err
}
