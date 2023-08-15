package k8sclient

import (
	"context"

	"k8s.io/client-go/tools/clientcmd"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/client-go/config/clientset/versioned/scheme"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

//go:generate mockgen -source=k8sclient.go -package=k8sclient -destination=mock_k8sclient.go
type K8SClient interface {
	GetConfigMap(namespace string, name string) (*v1.ConfigMap, error)
	GetClusterVersion(name string) (*configv1.ClusterVersion, error)
	ListNodes() (*v1.NodeList, error)
	GetSecret(namespace, name string) (*v1.Secret, error)
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
	return c.client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (c *k8sClient) GetClusterVersion(name string) (*configv1.ClusterVersion, error) {
	return c.configV1Client.ClusterVersions().Get(context.Background(), name, metav1.GetOptions{})
}

func (c *k8sClient) ListNodes() (*v1.NodeList, error) {
	return c.client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
}

func (c *k8sClient) GetSecret(namespace, name string) (*v1.Secret, error) {
	return c.client.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (c *k8sClient) CreateSecret(namespace string, secret *v1.Secret) (*v1.Secret, error) {
	return c.client.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
}

func (c *k8sClient) GetNameSpace(namespace string) (*v1.Namespace, error) {
	return c.client.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
}

func (c *k8sClient) CreateNameSpace(ns *v1.Namespace) (*v1.Namespace, error) {
	return c.client.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
}

func (c *k8sClient) CreateAgentClusterInstall(agentClusterInstall *hiveext.AgentClusterInstall) (*hiveext.AgentClusterInstall, error) {
	result := &hiveext.AgentClusterInstall{}
	err := c.client.RESTClient().Post().
		Resource(agentClusterInstall.Kind).
		Body(agentClusterInstall).
		Do(context.TODO()).
		Into(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c* k8sClient) CreateClusterImageSet(clusterImageSet *hivev1.ClusterImageSet) (*hivev1.ClusterImageSet, error) {
	result := &hivev1.ClusterImageSet{}
	err := c.client.AppsV1().RESTClient().Post().
		Resource(clusterImageSet.Kind).
		Body(clusterImageSet).
		Do(context.TODO()).
		Into(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c* k8sClient) GetNodes() (*v1.NodeList, error) {
	result := &v1.NodeList{}
	err := c.client.RESTClient().
	Get().
	Resource(result.Kind).
	Do(context.TODO()).
	Into(result)
if err != nil {
	return nil, err
}	
	return result, nil
}

func (c *k8sClient) CreateClusterDeployment(clusterDeployment *hivev1.ClusterDeployment) (*hivev1.ClusterDeployment, error) {
	result := &hivev1.ClusterDeployment{}
	err := c.client.RESTClient().Post().
	Resource(result.Kind).
	VersionedParams(nil, scheme.ParameterCodec).
	Body(result).
	Do(context.TODO()).
	Into(result)
if err != nil {
	return nil, err
}	
	return result, nil
}
