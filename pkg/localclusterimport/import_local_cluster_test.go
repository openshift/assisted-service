package localclusterimport

import (
	"encoding/base64"
	"fmt"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
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
		localClusterNamespace   string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clusterImportOperations = NewMockClusterImportOperations(ctrl)
		logger = logrus.New()
		localClusterNamespace = "some-cluster-namespace"
		localClusterImport = NewLocalClusterImport(clusterImportOperations, localClusterNamespace, logger)
		nodeConfigsKubeConfig = "someKubeConfig"
		// The openshift-kube-apiserver pull-secret is double base64 encoded
		// The first layer of encoding is transparent to us when using the client
		// The final layer needs to be encoded/decoded
		pullSecret = []byte("pullSecret")
		pullSecretBase64Encoded = []byte(base64.StdEncoding.EncodeToString(pullSecret))
		agentClusterInstallUID = types.UID(uuid.NewString())
		releaseImage = "quay.io/openshift-release-dev/ocp-release@sha256:a266d3d65c433b460cdef7ab5d6531580f5391adbe85d9c475208a56452e4c0b"

	})

	var mockCreateNamespace = func() *gomock.Call {
		namespace := &v1.Namespace{}
		namespace.Name = localClusterNamespace
		return clusterImportOperations.EXPECT().
			CreateNamespace(localClusterNamespace).
			Return(nil)
	}

	var mockAgentClusterInstallPresent = func() *gomock.Call {
		agentClusterInstall := &hiveext.AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				UID: agentClusterInstallUID,
			},
		}
		return clusterImportOperations.EXPECT().
			GetAgentClusterInstall(localClusterNamespace, fmt.Sprintf("%s-cluster-install", localClusterNamespace)).
			Return(agentClusterInstall, nil)
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
			Return(k8serrors.NewBadRequest("Invalid parameters"))
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
			Return(nil)
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
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-kube-apiserver",
				Name:      "node-kubeconfigs",
			},
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
				Namespace: localClusterNamespace,
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
			GetClusterDNS().
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
			GetClusterDNS().
			Return(dns, nil)
	}

	var mockNumberOfControlPlaneNodesFound = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			GetNumberOfControlPlaneNodes().
			Return(3, nil)
	}

	var mockFailedToCreateLocalClusterAdminKubeConfig = func() *gomock.Call {
		localClusterSecret := v1.Secret{}
		localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", localClusterNamespace)
		localClusterSecret.Namespace = localClusterNamespace
		localClusterSecret.Data = make(map[string][]byte)
		localClusterSecret.Data["kubeconfig"] = []byte(nodeConfigsKubeConfig)
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &localClusterSecret).
			Return(k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterAdminKubeConfig = func() *gomock.Call {
		localClusterSecret := v1.Secret{}
		localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", localClusterNamespace)
		localClusterSecret.Namespace = localClusterNamespace
		localClusterSecret.Data = make(map[string][]byte)
		localClusterSecret.Data["kubeconfig"] = []byte(nodeConfigsKubeConfig)
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &localClusterSecret).
			Return(nil)
	}

	var mockFailedToCreateLocalClusterPullSecret = func() *gomock.Call {
		hubPullSecret := v1.Secret{}
		hubPullSecret.Name = "pull-secret"
		hubPullSecret.Namespace = localClusterNamespace
		hubPullSecret.Data = make(map[string][]byte)
		hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
		// .dockerconfigjson is double base64 encoded for some reason.
		// simply obtaining the secret above will perform one layer of decoding.
		// we need to manually perform another to ensure that the data is correctly copied to the new secret.
		hubPullSecret.Data[".dockerconfigjson"] = pullSecret
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &hubPullSecret).
			Return(k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterPullSecret = func() *gomock.Call {
		hubPullSecret := v1.Secret{}
		hubPullSecret.Name = "pull-secret"
		hubPullSecret.Namespace = localClusterNamespace
		hubPullSecret.Data = make(map[string][]byte)
		hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
		// .dockerconfigjson is double base64 encoded for some reason.
		// simply obtaining the secret above will perform one layer of decoding.
		// we need to manually perform another to ensure that the data is correctly copied to the new secret.
		hubPullSecret.Data[".dockerconfigjson"] = pullSecret
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &hubPullSecret).
			Return(nil)
	}

	var mockAgentClusterInstall = func() hiveext.AgentClusterInstall {
		userManagedNetworkingActive := true
		agentClusterInstall := hiveext.AgentClusterInstall{
			Spec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{
					UserManagedNetworking: &userManagedNetworkingActive,
				},
				ClusterDeploymentRef: v1.LocalObjectReference{
					Name: localClusterNamespace + "-cluster-deployment",
				},
				ImageSetRef: &hivev1.ClusterImageSetReference{
					Name: "local-cluster-image-set",
				},
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
		}
		agentClusterInstall.Namespace = localClusterNamespace
		agentClusterInstall.Name = localClusterNamespace + "-cluster-install"
		return agentClusterInstall
	}

	var mockFailedToCreateAgentClusterInstall = func() *gomock.Call {
		agentClusterInstall := mockAgentClusterInstall()
		return clusterImportOperations.EXPECT().
			CreateAgentClusterInstall(&agentClusterInstall).
			Return(k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateAgentClusterInstall = func() *gomock.Call {
		agentClusterInstall := mockAgentClusterInstall()
		return clusterImportOperations.EXPECT().
			CreateAgentClusterInstall(&agentClusterInstall).
			Return(nil)
	}

	var mockClusterDeployment = func() *hivev1.ClusterDeployment {
		clusterDeployment := &hivev1.ClusterDeployment{
			Spec: hivev1.ClusterDeploymentSpec{
				Installed: true, // Let assisted know to import this cluster
				ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID:                "",
					InfraID:                  "",
					AdminKubeconfigSecretRef: v1.LocalObjectReference{Name: fmt.Sprintf("%s-admin-kubeconfig", localClusterNamespace)},
				},
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Name:    localClusterNamespace + "-cluster-install",
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
					Name: "pull-secret",
				},
			},
		}
		clusterDeployment.Name = localClusterNamespace + "-cluster-deployment"
		clusterDeployment.Namespace = localClusterNamespace
		clusterDeployment.Spec.ClusterName = localClusterNamespace + "-cluster-deployment"
		clusterDeployment.Spec.BaseDomain = "foobar.local"
		//
		// Adding this ownership reference to ensure we can submit clusterDeployment without ManagedClusterSet/join permission
		//
		clusterDeployment.OwnerReferences = []metav1.OwnerReference{{
			Kind:       "AgentCluster",
			APIVersion: "capi-provider.agent-install.openshift.io/v1alpha1",
			Name:       localClusterNamespace + "-cluster-install",
			UID:        agentClusterInstallUID,
		}}
		return clusterDeployment
	}

	var mockFailedToCreateLocalClusterDeployment = func() *gomock.Call {
		return clusterImportOperations.EXPECT().
			CreateClusterDeployment(mockClusterDeployment()).
			Return(k8serrors.NewBadRequest("Invalid parameters"))
	}

	var mockCreateLocalClusterDeployment = func() *gomock.Call {
		clusterDeployment := mockClusterDeployment()
		return clusterImportOperations.EXPECT().
			CreateClusterDeployment(clusterDeployment).
			Return(nil)
	}

	var mockAlreadyExistsOnCreateNamespace = func() *gomock.Call {
		namespace := &v1.Namespace{}
		namespace.Name = localClusterNamespace
		return clusterImportOperations.EXPECT().
			CreateNamespace(localClusterNamespace).
			Return(
				k8serrors.NewAlreadyExists(schema.GroupResource{Group: "v1", Resource: "Namespace"},
					localClusterNamespace))
	}

	var mockAlreadyExistsOnCreateClusterImageSet = func() *gomock.Call {
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
			Return(
				k8serrors.NewAlreadyExists(schema.GroupResource{Group: "hivev1", Resource: "ClusterImageSet"},
					"local-cluster-image-set"))
	}

	var mockAlreadyExistsOnCreateLocalClusterAdminKubeConfig = func() *gomock.Call {
		localClusterSecret := v1.Secret{}
		localClusterSecret.Name = fmt.Sprintf("%s-admin-kubeconfig", localClusterNamespace)
		localClusterSecret.Namespace = localClusterNamespace
		localClusterSecret.Data = make(map[string][]byte)
		localClusterSecret.Data["kubeconfig"] = []byte(nodeConfigsKubeConfig)
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &localClusterSecret).
			Return(k8serrors.NewAlreadyExists(schema.GroupResource{Group: "v1", Resource: "Secret"},
				localClusterSecret.Name))
	}

	var mockAlreadyExistsOnCreateLocalClusterPullSecret = func() *gomock.Call {
		hubPullSecret := v1.Secret{}
		hubPullSecret.Name = "pull-secret"
		hubPullSecret.Namespace = localClusterNamespace
		hubPullSecret.Data = make(map[string][]byte)
		hubPullSecret.OwnerReferences = []metav1.OwnerReference{}
		// .dockerconfigjson is double base64 encoded for some reason.
		// simply obtaining the secret above will perform one layer of decoding.
		// we need to manually perform another to ensure that the data is correctly copied to the new secret.
		hubPullSecret.Data[".dockerconfigjson"] = pullSecret
		return clusterImportOperations.EXPECT().
			CreateSecret(localClusterNamespace, &hubPullSecret).
			Return(k8serrors.NewAlreadyExists(schema.GroupResource{Group: "v1", Resource: "Secret"},
				hubPullSecret.Name))
	}

	var mockAlreadyExistsOnCreateAgentClusterInstall = func() *gomock.Call {
		agentClusterInstall := mockAgentClusterInstall()
		return clusterImportOperations.EXPECT().
			CreateAgentClusterInstall(&agentClusterInstall).
			Return(k8serrors.NewAlreadyExists(schema.GroupResource{Group: "hiveext", Resource: "AgentClusterInstall"},
				agentClusterInstall.Name))
	}

	var mockAlreadyExistsOnCreateLocalClusterDeployment = func() *gomock.Call {
		clusterDeployment := mockClusterDeployment()
		return clusterImportOperations.EXPECT().
			CreateClusterDeployment(clusterDeployment).
			Return(k8serrors.NewAlreadyExists(schema.GroupResource{Group: "hivev1", Resource: "ClusterDeployment"},
				clusterDeployment.Name))
	}

	It("if no cluster version could be found then this should appear in multierrors", func() {
		mockNoClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("ClusterVersion.v1 \"%s\" not found", "version")))
	})

	It("if node-kubeconfigs for the local cluster are not present then this should appear in multierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsNotFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("Secret.v1 \"%s\" not found", "node-kubeconfigs")))
	})

	It("if pull-secret for the local cluster is not present then this should appear in multierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretNotFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("Secret.v1 \"%s\" not found", "pull-secret")))
	})

	It("if dns for the local cluster is not present then this should appear in multierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSNotFound()
		mockNumberOfControlPlaneNodesFound()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonNotFound))
		Expect(apiStatus.Status().Message).To(Equal(fmt.Sprintf("DNS.config.openshift.io/v1 \"%s\" not found", "cluster")))
	})

	It("if unable to create image set, this should appear in multierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockFailedToCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateLocalClusterDeployment()
		mockCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("if unable to create local cluster admin kubeconfig, this should appear in mutierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockFailedToCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateLocalClusterDeployment()
		mockCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("if unable to create local cluster pull secret, this should appear in mutierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockFailedToCreateLocalClusterPullSecret()
		mockCreateLocalClusterDeployment()
		mockCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("if failed to create agent cluster install, this should appear in mutierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateLocalClusterDeployment()
		mockFailedToCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("if unable to create cluster deployment, this should appear in mutierrors", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockFailedToCreateLocalClusterDeployment()
		mockCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		multiErrors := result.(*multierror.Error)
		apiStatus := multiErrors.Errors[0].(k8serrors.APIStatus)
		Expect(apiStatus.Status().Reason).To(Equal(metav1.StatusReasonBadRequest))
		Expect(apiStatus.Status().Message).To(Equal("Invalid parameters"))
	})

	It("should have created all entities required to import local cluster", func() {
		mockClusterVersionFound()
		mockCreateNamespace()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockCreateClusterImageSet()
		mockCreateLocalClusterAdminKubeConfig()
		mockCreateLocalClusterPullSecret()
		mockCreateAgentClusterInstall()
		mockAgentClusterInstallPresent()
		mockCreateLocalClusterDeployment()
		result := localClusterImport.ImportLocalCluster()
		Expect(result).To(BeNil())
	})

	It("should not raise an error if entities already exist when attempting create", func() {
		mockClusterVersionFound()
		mockNodeKubeConfigsFound()
		mockLocalClusterPullSecretFound()
		mockClusterDNSFound()
		mockNumberOfControlPlaneNodesFound()
		mockAlreadyExistsOnCreateNamespace()
		mockAlreadyExistsOnCreateClusterImageSet()
		mockAlreadyExistsOnCreateLocalClusterAdminKubeConfig()
		mockAlreadyExistsOnCreateLocalClusterPullSecret()
		mockAlreadyExistsOnCreateAgentClusterInstall()
		mockAlreadyExistsOnCreateLocalClusterDeployment()
		mockAgentClusterInstallPresent()
		result := localClusterImport.ImportLocalCluster()
		Expect(result).To(BeNil())
	})
})

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ImportLocalCluster")
}
