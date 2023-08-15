package localclusterimport

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/agent"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ImportLocalCluster", func() {

	var (
		ctrl                    *gomock.Controller
		clusterImportOperations *MockClusterImportOperations
		logger                  *logrus.Logger
		localClusterImport      LocalClusterImport
		nodeConfigsKubeConfig   string
		pullSecret              []byte
		pullSecretBase64Encoded []byte
		agentClusterInstallUID  types.UID
		releaseImage            string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clusterImportOperations = NewMockClusterImportOperations(ctrl)
		logger = logrus.New()
		localClusterImport = NewLocalClusterImport(clusterImportOperations, logger)
		nodeConfigsKubeConfig = "someKubeConfig"
		// The openshift-kube-apiserver pull-secret is double base64 encoded
		// The first layer of encoding is transparent to us when using the client
		// The final layer needs to be encoded/decoded
		pullSecret = []byte("pullSecret")
		pullSecretBase64Encoded = []byte(base64.StdEncoding.EncodeToString(pullSecret))
		agentClusterInstallUID = types.UID(uuid.NewString())
		releaseImage = "quay.io/openshift-release-dev/ocp-release@sha256:a266d3d65c433b460cdef7ab5d6531580f5391adbe85d9c475208a56452e4c0b"

	})

	var mockAgentClusterInstallNotPresent = func() *gomock.Call {
		name := fmt.Sprintf("%s-cluster-install", LocalClusterNamespace)
		return clusterImportOperations.EXPECT().
			GetAgentClusterInstall(LocalClusterNamespace, name).
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "hivev1", Resource: "AgentClusterInstall"},
				name,
			))
	}

	var mockAgentClusterInstallPresent = func() *gomock.Call {
		agentClusterInstall := &hiveext.AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				UID: agentClusterInstallUID,
			},
		}
		return clusterImportOperations.EXPECT().
			GetAgentClusterInstall(LocalClusterNamespace, fmt.Sprintf("%s-cluster-install", LocalClusterNamespace)).
			Return(agentClusterInstall, nil)
	}

	var mockNoNamespaceFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetNamespace(LocalClusterNamespace).
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "v1", Resource: "Namespace"},
				LocalClusterNamespace,
			))
	}

	var mockNamespaceFound = func() *gomock.Call {
		ns := &v1.Namespace{}
		return clusterImportOperations.EXPECT().
			GetNamespace(LocalClusterNamespace).
			Return(ns, nil)
	}

	var mockNoClusterVersionFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetClusterVersion("version").
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "v1", Resource: "ClusterVersion"},
				"version",
			))
	}

	var mockClusterVersionFound = func() *gomock.Call {
		cv := &configv1.ClusterVersion{
			Status: configv1.ClusterVersionStatus{
				History: []configv1.UpdateHistory{
					{Version: "4.13.0", Image: releaseImage, State: configv1.CompletedUpdate},
				},
			},
		}
		return clusterImportOperations.EXPECT().
			GetClusterVersion("version").
			Return(cv, nil)
	}

	var mockFailedToCreateClusterImageSet = func() *gomock.Call {
		clusterImageSet := hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "local-cluster-image-set",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: releaseImage,
			},
		}
		return clusterImportOperations.EXPECT().
			CreateClusterImageSet(&clusterImageSet).
			Return(nil, k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateClusterImageSet = func() *gomock.Call {
		clusterImageSet := hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "local-cluster-image-set",
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: releaseImage,
			},
		}
		return clusterImportOperations.EXPECT().
			CreateClusterImageSet(&clusterImageSet).
			Return(&clusterImageSet, nil)
	}

	var mockNodeKubeConfigsNotFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetSecret("openshift-kube-apiserver", "node-kubeconfigs").
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "v1", Resource: "Secret"},
				"node-kubeconfigs",
			))
	}

	var mockNodeKubeConfigsFound = func() *gomock.Call {
		s := &v1.Secret{
			Data: map[string][]byte{
				"lb-ext.kubeconfig": []byte(nodeConfigsKubeConfig),
			},
		}
		return clusterImportOperations.EXPECT().
			GetSecret("openshift-kube-apiserver", "node-kubeconfigs").
			Return(s, nil)
	}

	var mockLocalClusterPullSecretNotFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetSecret("openshift-machine-api", "pull-secret").
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "v1", Resource: "Secret"},
				"pull-secret",
			))
	}

	var mockLocalClusterPullSecretFound = func() *gomock.Call {
		s := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret",
				Namespace: LocalClusterNamespace,
			},
			Data: map[string][]byte{
				".dockerconfigjson": pullSecretBase64Encoded,
			},
		}
		return clusterImportOperations.EXPECT().
			GetSecret("openshift-machine-api", "pull-secret").
			Return(s, nil)
	}

	var mockClusterDNSNotFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetDNS("cluster").
			Return(nil, k8serrors.NewNotFound(
				schema.GroupResource{Group: "config.openshift.io/v1", Resource: "DNS"},
				"cluster",
			))
	}

	var mockClusterDNSFound = func() *gomock.Call {
		dns := &configv1.DNS{
			Spec: configv1.DNSSpec{
				BaseDomain: "foobar.local",
			},
		}
		return clusterImportOperations.EXPECT().
			GetDNS("cluster").
			Return(dns, nil)
	}

	var mockNumberOfControlPlaneNodesFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetNumberOfControlPlaneNodes().
			Return(3, nil)
	}

	var mockFailedToCreateLocalClusterAdminKubeConfig = func() *gomock.Call {
		localClusterSecret := v1.Secret{}
		localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", LocalClusterNamespace)
		localClusterSecret.Namespace = LocalClusterNamespace
		localClusterSecret.Data = make(map[string][]byte)
		localClusterSecret.Data["kubeconfig"] = []byte(nodeConfigsKubeConfig)
		return clusterImportOperations.EXPECT().
			CreateSecret(LocalClusterNamespace, &localClusterSecret).
			Return(nil, k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterAdminKubeConfig = func() *gomock.Call {
		localClusterSecret := v1.Secret{}
		localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", LocalClusterNamespace)
		localClusterSecret.Namespace = LocalClusterNamespace
		localClusterSecret.Data = make(map[string][]byte)
		localClusterSecret.Data["kubeconfig"] = []byte(nodeConfigsKubeConfig)
		return clusterImportOperations.EXPECT().
			CreateSecret(LocalClusterNamespace, &localClusterSecret).
			Return(&localClusterSecret, nil)
	}

	var mockFailedToCreateLocalClusterPullSecret = func() *gomock.Call {
		hubPullSecret := v1.Secret{}
		hubPullSecret.Name = "pull-secret"
		hubPullSecret.Namespace = LocalClusterNamespace
		hubPullSecret.Data = make(map[string][]byte)
		hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
		// .dockerconfigjson is double base64 encoded for some reason.
		// simply obtaining the secret above will perform one layer of decoding.
		// we need to manually perform another to ensure that the data is correctly copied to the new secret.
		hubPullSecret.Data[".dockerconfigjson"] = pullSecret
		return clusterImportOperations.EXPECT().
			CreateSecret(LocalClusterNamespace, &hubPullSecret).
			Return(nil, k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterPullSecret = func() *gomock.Call {
		hubPullSecret := v1.Secret{}
		hubPullSecret.Name = "pull-secret"
		hubPullSecret.Namespace = LocalClusterNamespace
		hubPullSecret.Data = make(map[string][]byte)
		hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
		// .dockerconfigjson is double base64 encoded for some reason.
		// simply obtaining the secret above will perform one layer of decoding.
		// we need to manually perform another to ensure that the data is correctly copied to the new secret.
		hubPullSecret.Data[".dockerconfigjson"] = pullSecret
		return clusterImportOperations.EXPECT().
			CreateSecret(LocalClusterNamespace, &hubPullSecret).
			Return(&hubPullSecret, nil)
	}

	var mockFailedToCreateAgentClusterInstall = func() *gomock.Call {
		userManagedNetworkingActive := true
		agentClusterInstall := hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{
					UserManagedNetworking: &userManagedNetworkingActive,
				},
				ClusterDeploymentRef: v1.LocalObjectReference{
					Name: LocalClusterNamespace + "-cluster-deployment",
				},
				ImageSetRef: &hivev1.ClusterImageSetReference{
					Name: "local-cluster-image-set",
				},
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
		}
		agentClusterInstall.Namespace = LocalClusterNamespace
		agentClusterInstall.Name = LocalClusterNamespace + "-cluster-install"
		return clusterImportOperations.EXPECT().
			CreateAgentClusterInstall(&agentClusterInstall).
			Return(k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateAgentClusterInstall = func() *gomock.Call {
		userManagedNetworkingActive := true
		agentClusterInstall := hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{
					UserManagedNetworking: &userManagedNetworkingActive,
				},
				ClusterDeploymentRef: v1.LocalObjectReference{
					Name: LocalClusterNamespace + "-cluster-deployment",
				},
				ImageSetRef: &hivev1.ClusterImageSetReference{
					Name: "local-cluster-image-set",
				},
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
		}
		agentClusterInstall.Namespace = LocalClusterNamespace
		agentClusterInstall.Name = LocalClusterNamespace + "-cluster-install"
		return clusterImportOperations.EXPECT().
			CreateAgentClusterInstall(&agentClusterInstall).
			Return(nil)
	}

	var mockFailedToCreateLocalClusterDeployment = func() *gomock.Call {
		clusterDeployment := &hivev1.ClusterDeployment{
			Spec: hivev1.ClusterDeploymentSpec{
				Installed: true, // Let ZTP know to import this cluster
				ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID:                "",
					InfraID:                  "",
					AdminKubeconfigSecretRef: v1.LocalObjectReference{Name: fmt.Sprintf("%s-admin-kubeconfig", LocalClusterNamespace)},
				},
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Name:    LocalClusterNamespace + "-cluster-install",
					Group:   "extensions.hive.openshift.io",
					Kind:    "AgentClusterInstall",
					Version: "v1beta1",
				},
				Platform: hivev1.Platform{
					AgentBareMetal: &agent.BareMetalPlatform{
						AgentSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"infraenv": fmt.Sprintf("%s-day2-infraenv", LocalClusterNamespace)},
						},
					},
				},
				PullSecretRef: &v1.LocalObjectReference{
					Name: "pull-secret",
				},
			},
		}
		clusterDeployment.Name = LocalClusterNamespace + "-cluster-deployment"
		clusterDeployment.Namespace = LocalClusterNamespace
		clusterDeployment.Spec.ClusterName = LocalClusterNamespace + "-cluster-deployment"
		clusterDeployment.Spec.BaseDomain = "foobar.local"
		//
		// Adding this ownership reference to ensure we can submit clusterDeployment without ManagedClusterSet/join permission
		//
		agentClusterInstallOwnerRef := metav1.OwnerReference{
			Kind:       "AgentCluster",
			APIVersion: "capi-provider.agent-install.openshift.io/v1alpha1",
			Name:       LocalClusterNamespace + "-cluster-install",
			UID:        agentClusterInstallUID,
		}
		clusterDeployment.OwnerReferences = []metav1.OwnerReference{agentClusterInstallOwnerRef}
		return clusterImportOperations.EXPECT().
			CreateClusterDeployment(clusterDeployment).
			Return(nil, k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterDeployment = func() *gomock.Call {
		clusterDeployment := &hivev1.ClusterDeployment{
			Spec: hivev1.ClusterDeploymentSpec{
				Installed: true, // Let ZTP know to import this cluster
				ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID:                "",
					InfraID:                  "",
					AdminKubeconfigSecretRef: v1.LocalObjectReference{Name: fmt.Sprintf("%s-admin-kubeconfig", LocalClusterNamespace)},
				},
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Name:    LocalClusterNamespace + "-cluster-install",
					Group:   "extensions.hive.openshift.io",
					Kind:    "AgentClusterInstall",
					Version: "v1beta1",
				},
				Platform: hivev1.Platform{
					AgentBareMetal: &agent.BareMetalPlatform{
						AgentSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"infraenv": fmt.Sprintf("%s-day2-infraenv", LocalClusterNamespace)},
						},
					},
				},
				PullSecretRef: &v1.LocalObjectReference{
					Name: "pull-secret",
				},
			},
		}
		clusterDeployment.Name = LocalClusterNamespace + "-cluster-deployment"
		clusterDeployment.Namespace = LocalClusterNamespace
		clusterDeployment.Spec.ClusterName = LocalClusterNamespace + "-cluster-deployment"
		clusterDeployment.Spec.BaseDomain = "foobar.local"
		//
		// Adding this ownership reference to ensure we can submit clusterDeployment without ManagedClusterSet/join permission
		//
		agentClusterInstallOwnerRef := metav1.OwnerReference{
			Kind:       "AgentCluster",
			APIVersion: "capi-provider.agent-install.openshift.io/v1alpha1",
			Name:       LocalClusterNamespace + "-cluster-install",
			UID:        agentClusterInstallUID,
		}
		clusterDeployment.OwnerReferences = []metav1.OwnerReference{agentClusterInstallOwnerRef}
		return clusterImportOperations.EXPECT().
			CreateClusterDeployment(clusterDeployment).
			Return(clusterDeployment, nil)
	}

	It("should not proceed if local cluster namespace does not exist", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNoNamespaceFound()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("Namespace.v1 \"%s\" not found", LocalClusterNamespace)))
	})

	It("should not proceed if AgentClusterInstall for local cluster already exists", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallPresent()
		Expect(localClusterImport.ImportLocalCluster()).To(Equal(errors.New("hubClusterDay2Importer: no need to import local cluster as registration already detected")))
	})

	It("should not proceed if cluster version does not exist", func() {
		mockNoClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("ClusterVersion.v1 \"%s\" not found", "version")))
	})

	It("should not proceed if node-kubeconfigs for the local cluster are not present", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsNotFound()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("Secret.v1 \"%s\" not found", "node-kubeconfigs")))
	})

	It("should not proceed if pull-secret for the local cluster is not present", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretNotFound()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("Secret.v1 \"%s\" not found", "pull-secret")))
	})

	It("should not proceed if dns for the local cluster is not present", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSNotFound()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("DNS.config.openshift.io/v1 \"%s\" not found", "cluster")))
	})

	It("should not proceed if unable to create image set", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockFailedToCreateClusterImageSet()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should not proceed if unable to create local cluster admin kubeconfig", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockFailedToCreateLocalClusterAdminKubeConfig()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should not proceed if unable to create local cluster pull secret", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockFailedToCreateLocalClusterPullSecret()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should not proceed if failed to create agent cluster install", func() {
		mockClusterVersionFound()
		mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockFailedToCreateAgentClusterInstall()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should give appropriate error if unable to create cluster deployment", func() {
		mockClusterVersionFound()
		aciNotPresent := mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateAgentClusterInstall()
		createdACIMock := mockAgentClusterInstallPresent()
		gomock.InOrder(aciNotPresent, createdACIMock)
		mockFailedToCreateLocalClusterDeployment()
		result := localClusterImport.ImportLocalCluster()
		apiStatus := result.(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should have created all entities required to add day2 local cluster to ACM", func() {
		mockClusterVersionFound()
		aciNotPresent := mockAgentClusterInstallNotPresent()
		mockNamespaceFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateAgentClusterInstall()
		createdACIMock := mockAgentClusterInstallPresent()
		gomock.InOrder(aciNotPresent, createdACIMock)
		mockCreateLocalClusterDeployment()
		result := localClusterImport.ImportLocalCluster()
		Expect(result).To(BeNil())
	})
})

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ImportLocalCluster")
}
