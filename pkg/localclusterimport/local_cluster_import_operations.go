package localclusterimport

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=local_cluster_import_operations.go -package=localclusterimport -destination=local_cluster_import_operations_mocks.go
type ClusterImportOperations interface {
	GetNamespace(name string) (*v1.Namespace, error)
	GetSecret(namespace string, name string) (*v1.Secret, error)
	GetClusterVersion(name string) (*configv1.ClusterVersion, error)
	GetClusterImageSet(name string) (*hivev1.ClusterImageSet, error)
	GetNodes() (*v1.NodeList, error)
	GetNumberOfControlPlaneNodes() (int, error)
	GetClusterDNS() (*configv1.DNS, error)
	GetClusterProxy() (*configv1.Proxy, error)
	GetAgentServiceConfig() (*aiv1beta1.AgentServiceConfig, error)
	CreateAgentClusterInstall(agentClusterInstall *hiveext.AgentClusterInstall) error
	CreateNamespace(name string) error
	CreateSecret(namespace string, secret *v1.Secret) error
	CreateClusterImageSet(clusterImageSet *hivev1.ClusterImageSet) error
	CreateClusterDeployment(clusterDeployment *hivev1.ClusterDeployment) error
	GetManagedCluster(name string) (*clusterv1.ManagedCluster, error)
	DeleteClusterDeployment(namespace string, name string) error
	DeleteAgentClusterInstall(namespace string, name string) error
	GetClusterDeployment(namespace string, name string) (*hivev1.ClusterDeployment, error)
	GetAgentClusterInstall(namespace string, name string) (*hiveext.AgentClusterInstall, error)
}

type LocalClusterImportOperations struct {
	context                context.Context
	client                 client.Client
	agentServiceConfigName string
}

func NewLocalClusterImportOperations(client client.Client, agentServiceConfigName string) LocalClusterImportOperations {
	return LocalClusterImportOperations{context: context.TODO(), client: client, agentServiceConfigName: agentServiceConfigName}
}

func (o *LocalClusterImportOperations) GetManagedCluster(name string) (*clusterv1.ManagedCluster, error) {
	managedCluster := &clusterv1.ManagedCluster{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, managedCluster)
	if err != nil {
		return nil, err
	}
	return managedCluster, nil
}

func (o *LocalClusterImportOperations) GetAgentServiceConfig() (*aiv1beta1.AgentServiceConfig, error) {
	agentServiceConfig := &aiv1beta1.AgentServiceConfig{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      o.agentServiceConfigName,
	}
	err := o.client.Get(o.context, namespacedName, agentServiceConfig)
	if err != nil {
		return nil, err
	}
	return agentServiceConfig, nil
}

func (o *LocalClusterImportOperations) GetClusterDeployment(namespace string, name string) (*hivev1.ClusterDeployment, error) {
	clusterDeployment := &hivev1.ClusterDeployment{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, clusterDeployment)
	if err != nil {
		return nil, err
	}
	return clusterDeployment, nil
}

func (o *LocalClusterImportOperations) GetAgentClusterInstall(namespace string, name string) (*hiveext.AgentClusterInstall, error) {
	agentClusterInstall := &hiveext.AgentClusterInstall{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, agentClusterInstall)
	if err != nil {
		return nil, err
	}
	return agentClusterInstall, nil
}

func (o *LocalClusterImportOperations) GetNamespace(name string) (*v1.Namespace, error) {
	ns := &v1.Namespace{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, ns)
	if err != nil {
		return nil, err
	}
	return ns, nil
}

func (o *LocalClusterImportOperations) CreateNamespace(name string) error {
	ns := &v1.Namespace{}
	ns.Name = name
	err := o.client.Create(o.context, ns)
	if err != nil {
		return err
	}
	return nil
}

func (o *LocalClusterImportOperations) GetSecret(namespace string, name string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, secret)
	if err != nil {
		return nil, errors.Wrapf(err, "Unable to fetch secret %s from namespace %s", name, namespace)
	}
	return secret, nil
}

func (o *LocalClusterImportOperations) GetClusterVersion(name string) (*configv1.ClusterVersion, error) {
	clusterVersion := &configv1.ClusterVersion{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, clusterVersion)
	if err != nil {
		return nil, err
	}
	return clusterVersion, nil
}

func (o *LocalClusterImportOperations) GetClusterImageSet(name string) (*hivev1.ClusterImageSet, error) {
	clusterImageSet := &hivev1.ClusterImageSet{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	err := o.client.Get(o.context, namespacedName, clusterImageSet)
	if err != nil {
		return nil, errors.Wrapf(err, "Unable to fetch cluster image set %s", name)
	}
	return clusterImageSet, nil
}

func (o *LocalClusterImportOperations) CreateAgentClusterInstall(agentClusterInstall *hiveext.AgentClusterInstall) error {
	err := o.client.Create(o.context, agentClusterInstall)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (o *LocalClusterImportOperations) CreateSecret(namespace string, secret *v1.Secret) error {
	err := o.client.Create(o.context, secret)
	if err != nil {
		return err
	}
	return nil
}

func (o *LocalClusterImportOperations) CreateClusterImageSet(clusterImageSet *hivev1.ClusterImageSet) error {
	err := o.client.Create(o.context, clusterImageSet)
	if err != nil {
		return err
	}
	return nil
}

func (o *LocalClusterImportOperations) CreateClusterDeployment(clusterDeployment *hivev1.ClusterDeployment) error {
	err := o.client.Create(o.context, clusterDeployment)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (o *LocalClusterImportOperations) GetNodes() (*v1.NodeList, error) {
	nodeList := &v1.NodeList{}
	err := o.client.List(o.context, nodeList)
	if err != nil {
		return nil, err
	}
	return nodeList, nil
}

func (o *LocalClusterImportOperations) GetNumberOfControlPlaneNodes() (int, error) {
	// Determine the number of control plane agents we have
	// Control plane nodes have a specific label that we can look for.,
	numberOfControlPlaneNodes := 0
	nodeList, err := o.GetNodes()
	if err != nil {
		return 0, err
	}
	for _, node := range nodeList.Items {
		for nodeLabelKey := range node.Labels {
			if nodeLabelKey == "node-role.kubernetes.io/control-plane" {
				numberOfControlPlaneNodes++
			}
		}
	}
	return numberOfControlPlaneNodes, nil
}

func (o *LocalClusterImportOperations) GetClusterDNS() (*configv1.DNS, error) {
	dns := &configv1.DNS{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      "cluster",
	}
	err := o.client.Get(o.context, namespacedName, dns)
	if err != nil {
		return nil, err
	}
	return dns, nil
}

func (o *LocalClusterImportOperations) GetClusterProxy() (*configv1.Proxy, error) {
	proxy := &configv1.Proxy{}
	namespacedName := types.NamespacedName{
		Namespace: "",
		Name:      "cluster",
	}
	err := o.client.Get(o.context, namespacedName, proxy)
	if err != nil {
		return nil, err
	}
	return proxy, nil
}

func (o *LocalClusterImportOperations) DeleteClusterDeployment(namespace string, name string) error {
	clusterDeployment, err := o.GetClusterDeployment(namespace, name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "error fetching ClusterDeployment %s in namespace %s for delete", name, namespace)
	}
	err = o.client.Delete(o.context, clusterDeployment)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "error deleting ClusterDeployment %s in namespace %s", name, namespace)
	}
	return nil
}

func (o *LocalClusterImportOperations) DeleteAgentClusterInstall(namespace string, name string) error {
	agentClusterInstall, err := o.GetAgentClusterInstall(namespace, name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "error fetching AgentClusterInstall %s in namespace %s for delete", name, namespace)
	}
	err = o.client.Delete(o.context, agentClusterInstall)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "error deleting AgentClusterInstall %s in namespace %s", name, namespace)
	}
	return nil
}
