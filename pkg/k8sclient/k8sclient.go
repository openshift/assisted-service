package k8sclient

import (
	"context"

	"k8s.io/client-go/tools/clientcmd"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

//go:generate mockgen -source=k8sclient.go -package=k8sclient -destination=mock_k8sclient.go
type K8SClient interface {
	GetConfigMap(namespace string, name string) (*v1.ConfigMap, error)
	GetClusterVersion(name string) (*configv1.ClusterVersion, error)
}

type k8sClient struct {
	log            logrus.FieldLogger
	client         *kubernetes.Clientset
	configV1Client *configv1client.ConfigV1Client
}

func NewK8SClient(configPath string, log logrus.FieldLogger) (K8SClient, error) {
	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return &k8sClient{}, errors.Wrap(err, "loading kubeconfig")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return &k8sClient{}, errors.Wrap(err, "creating a Kubernetes client")
	}
	configClient, err := configv1client.NewForConfig(config)
	if err != nil {
		return &k8sClient{}, errors.Wrap(err, "creating openshift config client")
	}
	return &k8sClient{log, client, configClient}, nil
}

func (c *k8sClient) GetConfigMap(namespace string, name string) (*v1.ConfigMap, error) {
	cm, err := c.client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func (c *k8sClient) GetClusterVersion(name string) (*configv1.ClusterVersion, error) {
	return c.configV1Client.ClusterVersions().Get(context.Background(), name, metav1.GetOptions{})
}
