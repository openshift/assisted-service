package localclusterimport

import (
	"encoding/base64"
	"errors"
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type LocalClusterImport struct {
	clusterImportOperations ClusterImportOperations
	localClusterNamespace   string
	log                     *logrus.Logger
}

func NewLocalClusterImport(localClusterImportOperations ClusterImportOperations, localClusterNamespace string, log *logrus.Logger) LocalClusterImport {
	return LocalClusterImport{clusterImportOperations: localClusterImportOperations, log: log, localClusterNamespace: localClusterNamespace}
}

func (i *LocalClusterImport) createClusterImageSet(release_image string) error {
	var err error
	clusterImageSet := hivev1.ClusterImageSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "local-cluster-image-set",
		},
		Spec: hivev1.ClusterImageSetSpec{
			ReleaseImage: release_image,
		},
	}
	err = i.clusterImportOperations.CreateClusterImageSet(&clusterImageSet)
	if err != nil {
		i.log.Errorf("unable to create ClusterImageSet due to error: %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) createAdminKubeConfig(kubeConfigSecret *v1.Secret) error {
	var err error
	// Store the kubeconfig data in the local cluster namespace
	localClusterSecret := v1.Secret{}
	localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", i.localClusterNamespace)
	localClusterSecret.Namespace = i.localClusterNamespace
	localClusterSecret.Data = make(map[string][]byte)
	localClusterSecret.Data["kubeconfig"] = kubeConfigSecret.Data["lb-ext.kubeconfig"]
	err = i.clusterImportOperations.CreateSecret(i.localClusterNamespace, &localClusterSecret)
	if err != nil {
		i.log.Errorf("to store secret due to error %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) createLocalClusterPullSecret(sourceSecret *v1.Secret) error {
	var err error
	// Store the pull secret in the local cluster namespace
	hubPullSecret := v1.Secret{}
	hubPullSecret.Name = sourceSecret.Name
	hubPullSecret.Namespace = i.localClusterNamespace
	hubPullSecret.Data = make(map[string][]byte)
	// .dockerconfigjson is double base64 encoded for some reason.
	// simply obtaining the secret above will perform one layer of decoding.
	// we need to manually perform another to ensure that the data is correctly copied to the new secret.
	hubPullSecret.Data[".dockerconfigjson"], err = base64.StdEncoding.DecodeString(string(sourceSecret.Data[".dockerconfigjson"]))
	if err != nil {
		i.log.Errorf("unable to decode base64 pull secret data due to error %s", err.Error())
		return err
	}
	hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
	err = i.clusterImportOperations.CreateSecret(i.localClusterNamespace, &hubPullSecret)
	if err != nil {
		i.log.Errorf("unable to store hub pull secret due to error %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) createAgentClusterInstall(numberOfControlPlaneNodes int) error {
	//Create an AgentClusterInstall in the local cluster namespace
	userManagedNetworkingActive := true
	agentClusterInstall := &hiveext.AgentClusterInstall{
		Spec: hiveext.AgentClusterInstallSpec{
			Networking: hiveext.Networking{
				UserManagedNetworking: &userManagedNetworkingActive,
			},
			ClusterDeploymentRef: v1.LocalObjectReference{
				Name: i.localClusterNamespace,
			},
			ImageSetRef: &hivev1.ClusterImageSetReference{
				Name: "local-cluster-image-set",
			},
			ProvisionRequirements: hiveext.ProvisionRequirements{
				ControlPlaneAgents: numberOfControlPlaneNodes,
			},
		},
	}
	agentClusterInstall.Namespace = i.localClusterNamespace
	agentClusterInstall.Name = i.localClusterNamespace
	err := i.clusterImportOperations.CreateAgentClusterInstall(agentClusterInstall)
	if err != nil {
		i.log.Errorf("could not create AgentClusterInstall due to error %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) createClusterDeployment(pullSecret *v1.Secret, dns *configv1.DNS, kubeConfigSecret *v1.Secret, agentServiceConfig *aiv1beta1.AgentServiceConfig) error {
	if pullSecret == nil {
		i.log.Errorf("Skipping creation of cluster deployment due to nil pullsecret")
		return nil
	}
	if dns == nil {
		i.log.Errorf("Skipping creation of cluster deployment due to nil dns")
		return nil
	}
	if kubeConfigSecret == nil {
		i.log.Errorf("Skipping creation of cluster deployment due to nil kubeconfigsecret")
		return nil
	}
	if agentServiceConfig == nil {
		i.log.Errorf("Skipping creation of cluster deployment due to nil agentServiceConfig")
		return nil
	}
	// Create a cluster deployment in the local cluster namespace
	clusterDeployment := &hivev1.ClusterDeployment{
		Spec: hivev1.ClusterDeploymentSpec{
			Installed: true, // Let assisted know to import this cluster
			ClusterMetadata: &hivev1.ClusterMetadata{
				ClusterID:                "",
				InfraID:                  "",
				AdminKubeconfigSecretRef: v1.LocalObjectReference{Name: fmt.Sprintf("%s-admin-kubeconfig", i.localClusterNamespace)},
			},
			ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
				Name:    i.localClusterNamespace,
				Group:   "extensions.hive.openshift.io",
				Kind:    "AgentClusterInstall",
				Version: "v1beta1",
			},
			Platform: hivev1.Platform{
				AgentBareMetal: &agent.BareMetalPlatform{
					AgentSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"infraenv": "local-cluster"},
					},
				},
			},
			PullSecretRef: &v1.LocalObjectReference{
				Name: pullSecret.Name,
			},
		},
	}
	clusterDeployment.Name = i.localClusterNamespace
	clusterDeployment.Namespace = i.localClusterNamespace
	clusterDeployment.Spec.ClusterName = i.localClusterNamespace
	clusterDeployment.Spec.BaseDomain = dns.Spec.BaseDomain
	agentClusterInstallOwnerRef := metav1.OwnerReference{
		Kind:       "AgentServiceConfig",
		APIVersion: "agent-install.openshift.io/v1beta1",
		Name:       agentServiceConfig.Name,
		UID:        agentServiceConfig.UID,
	}
	clusterDeployment.OwnerReferences = []metav1.OwnerReference{agentClusterInstallOwnerRef}
	err := i.clusterImportOperations.CreateClusterDeployment(clusterDeployment)
	if err != nil {
		i.log.Errorf("could not create ClusterDeployment due to error %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) createNamespace(name string) error {
	err := i.clusterImportOperations.CreateNamespace(name)
	if err != nil {
		i.log.Errorf("could not create Namespace due to error %s", err.Error())
		return err
	}
	return nil
}

func (i *LocalClusterImport) ImportLocalCluster() error {

	var errorList error

	clusterVersion, err := i.clusterImportOperations.GetClusterVersion("version")
	if err != nil {
		i.log.Errorf("unable to find cluster version info due to error: %s", err.Error())
		errorList = multierror.Append(nil, err)
		return errorList
	}

	release_image := ""
	if clusterVersion != nil && clusterVersion.Status.History[0].State != configv1.CompletedUpdate {
		message := "the release image in the cluster version is not ready yet, please wait for installation to complete and then restart the assisted-service"
		i.log.Error(message)
		errorList = multierror.Append(errorList, errors.New(message))
	}

	kubeConfigSecret, err := i.clusterImportOperations.GetSecret("openshift-kube-apiserver", "node-kubeconfigs")
	if err != nil {
		i.log.Errorf("unable to fetch local cluster kubeconfigs due to error %s", err.Error())
		errorList = multierror.Append(errorList, err)
	}

	pullSecret, err := i.clusterImportOperations.GetSecret("openshift-machine-api", "pull-secret")
	if err != nil {
		i.log.Errorf("unable to fetch pull secret due to error %s", err.Error())
		errorList = multierror.Append(errorList, err)
	}

	dns, err := i.clusterImportOperations.GetClusterDNS()
	if err != nil {
		i.log.Errorf("could not fetch DNS due to error %s", err.Error())
		errorList = multierror.Append(errorList, err)
	}

	numberOfControlPlaneNodes, err := i.clusterImportOperations.GetNumberOfControlPlaneNodes()
	if err != nil {
		i.log.Errorf("unable to determine the number of control plane nodes due to error %s", err.Error())
		errorList = multierror.Append(errorList, err)
	}

	if clusterVersion.Status.History[0].Image == "" {
		i.log.Error("unable to determine release image")
		errorList = multierror.Append(errorList, err)
	}

	// If we already have errors before we start writing things, then let's stop
	// better to let the user fix the problems than have to delete things later.
	if errorList != nil {
		return errorList
	}

	release_image = clusterVersion.Status.History[0].Image

	err = i.createClusterImageSet(release_image)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		errorList = multierror.Append(errorList, err)
	}

	err = i.createNamespace(i.localClusterNamespace)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		errorList = multierror.Append(errorList, err)
	}

	if kubeConfigSecret != nil {
		err = i.createAdminKubeConfig(kubeConfigSecret)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			errorList = multierror.Append(errorList, err)
		}
	}

	if pullSecret != nil {
		err = i.createLocalClusterPullSecret(pullSecret)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			errorList = multierror.Append(errorList, err)
		}
	}

	if numberOfControlPlaneNodes > 0 {
		err := i.createAgentClusterInstall(numberOfControlPlaneNodes)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			errorList = multierror.Append(errorList, err)
		}

		asc, err := i.clusterImportOperations.GetAgentServiceConfig()
		if err != nil {
			i.log.Errorf("failed to fetch AgentServiceConfig due to error %s", err.Error())
			errorList = multierror.Append(errorList, err)
		}

		err = i.createClusterDeployment(pullSecret, dns, kubeConfigSecret, asc)
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			errorList = multierror.Append(errorList, err)
		}
	}

	if errorList != nil {
		return errorList
	}

	i.log.Info("completed Day2 import of hub cluster")
	return nil
}
