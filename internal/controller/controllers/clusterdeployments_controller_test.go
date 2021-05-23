package controllers

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	hiveext "github.com/openshift/assisted-service/internal/controller/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/hive/apis/hive/v1/aws"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newClusterDeploymentRequest(cluster *hivev1.ClusterDeployment) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      cluster.ObjectMeta.Name,
			Namespace: cluster.ObjectMeta.Namespace,
		},
	}
}

func newClusterDeployment(name, namespace string, spec hivev1.ClusterDeploymentSpec) *hivev1.ClusterDeployment {
	return &hivev1.ClusterDeployment{
		Spec: spec,
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterDeployment",
			APIVersion: "hive.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func getDefaultSNOAgentClusterInstallSpec(clusterName string) hiveext.AgentClusterInstallSpec {
	return hiveext.AgentClusterInstallSpec{
		Networking: hiveext.Networking{
			MachineNetwork: nil,
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "10.128.0.0/14",
				HostPrefix: 23,
			}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		SSHPublicKey: "some-key",
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 1,
			WorkerAgents:       0,
		},
		ImageSetRef:          hivev1.ClusterImageSetReference{Name: "openshift-v4.8.0"},
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterName},
	}
}

func newAgentClusterInstall(name, namespace string, spec hiveext.AgentClusterInstallSpec, cd *hivev1.ClusterDeployment) *hiveext.AgentClusterInstall {
	return &hiveext.AgentClusterInstall{
		Spec: spec,
		TypeMeta: metav1.TypeMeta{
			Kind:       "AgentClusterInstall",
			APIVersion: "hiveextension/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         cd.APIVersion,
				Kind:               cd.Kind,
				Name:               cd.Name,
				UID:                cd.UID,
				BlockOwnerDeletion: swag.Bool(true),
			}},
		},
	}
}

func getDefaultAgentClusterInstallSpec(clusterName string) hiveext.AgentClusterInstallSpec {
	return hiveext.AgentClusterInstallSpec{
		APIVIP:     "1.2.3.8",
		IngressVIP: "1.2.3.9",
		Networking: hiveext.Networking{
			MachineNetwork: nil,
			ClusterNetwork: []hiveext.ClusterNetworkEntry{{
				CIDR:       "10.128.0.0/14",
				HostPrefix: 23,
			}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		SSHPublicKey: "some-key",
		ProvisionRequirements: hiveext.ProvisionRequirements{
			ControlPlaneAgents: 3,
			WorkerAgents:       2,
		},
		ImageSetRef:          hivev1.ClusterImageSetReference{Name: "openshift-v4.8.0"},
		ClusterDeploymentRef: corev1.LocalObjectReference{Name: clusterName},
	}
}

func getDefaultClusterDeploymentSpec(clusterName, aciName, pullSecretName string) hivev1.ClusterDeploymentSpec {
	return hivev1.ClusterDeploymentSpec{
		BaseDomain:  "hive.example.com",
		ClusterName: clusterName,
		PullSecretRef: &corev1.LocalObjectReference{
			Name: pullSecretName,
		},
		ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
			Group:   hiveext.Group,
			Version: hiveext.Version,
			Kind:    "AgentClusterInstall",
			Name:    aciName,
		},
	}
}

func kubeTimeNow() *metav1.Time {
	t := metav1.NewTime(time.Now())
	return &t
}

func simulateACIDeletionWithFinalizer(ctx context.Context, c client.Client, aci *hiveext.AgentClusterInstall) {
	// simulate ACI deletion with finalizer
	aci.ObjectMeta.Finalizers = []string{AgentClusterInstallFinalizerName}
	aci.ObjectMeta.DeletionTimestamp = kubeTimeNow()
	Expect(c.Update(ctx, aci)).Should(BeNil())
}

var _ = Describe("cluster reconcile", func() {
	var (
		c                              client.Client
		cr                             *ClusterDeploymentsReconciler
		ctx                            = context.Background()
		mockCtrl                       *gomock.Controller
		mockInstallerInternal          *bminventory.MockInstallerInternals
		mockClusterApi                 *cluster.MockAPI
		mockHostApi                    *host.MockAPI
		mockManifestsApi               *manifests.MockClusterManifestsInternals
		mockCRDEventsHandler           *MockCRDEventsHandler
		defaultClusterSpec             hivev1.ClusterDeploymentSpec
		clusterName                    = "test-cluster"
		agentClusterInstallName        = "test-cluster-aci"
		defaultAgentClusterInstallSpec hiveext.AgentClusterInstallSpec
		pullSecretName                 = "pull-secret"
		imageSetName                   = "openshift-v4.8.0"
		releaseImage                   = "quay.io/openshift-release-dev/ocp-release:4.8.0-x86_64"
		ocpReleaseVersion              = "4.8.0"
		openshiftVersion               = &models.OpenshiftVersion{
			DisplayName:    new(string),
			ReleaseImage:   new(string),
			ReleaseVersion: &ocpReleaseVersion,
			RhcosImage:     new(string),
			RhcosVersion:   new(string),
			SupportLevel:   new(string),
		}
	)

	getTestCluster := func() *hivev1.ClusterDeployment {
		var cluster hivev1.ClusterDeployment
		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      clusterName,
		}
		Expect(c.Get(ctx, key, &cluster)).To(BeNil())
		return &cluster
	}

	getTestClusterInstall := func() *hiveext.AgentClusterInstall {
		clusterInstall := &hiveext.AgentClusterInstall{}
		Expect(c.Get(ctx,
			types.NamespacedName{
				Namespace: testNamespace,
				Name:      agentClusterInstallName,
			},
			clusterInstall)).To(BeNil())
		return clusterInstall
	}

	getSecret := func(namespace, name string) *corev1.Secret {
		var secret corev1.Secret
		key := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}
		Expect(c.Get(ctx, key, &secret)).To(BeNil())
		return &secret
	}

	BeforeEach(func() {
		defaultClusterSpec = getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName)
		defaultAgentClusterInstallSpec = getDefaultAgentClusterInstallSpec(clusterName)
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockClusterApi = cluster.NewMockAPI(mockCtrl)
		mockHostApi = host.NewMockAPI(mockCtrl)
		mockCRDEventsHandler = NewMockCRDEventsHandler(mockCtrl)
		mockManifestsApi = manifests.NewMockClusterManifestsInternals(mockCtrl)
		cr = &ClusterDeploymentsReconciler{
			Client:           c,
			Scheme:           scheme.Scheme,
			Log:              common.GetTestLog(),
			Installer:        mockInstallerInternal,
			ClusterApi:       mockClusterApi,
			HostApi:          mockHostApi,
			CRDEventsHandler: mockCRDEventsHandler,
			Manifests:        mockManifestsApi,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("create cluster", func() {
		BeforeEach(func() {
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())
			imageSet := getDefaultTestImageSet(imageSetName, releaseImage)
			Expect(c.Create(ctx, imageSet)).To(BeNil())
		})

		Context("successful creation", func() {
			var clusterReply *common.Cluster

			BeforeEach(func() {
				id := strfmt.UUID(uuid.New().String())
				clusterReply = &common.Cluster{
					Cluster: models.Cluster{
						Status:     swag.String(models.ClusterStatusPendingForInput),
						StatusInfo: swag.String("User input required"),
						ID:         &id,
					},
				}
			})

			validateCreation := func(cluster *hivev1.ClusterDeployment) {
				request := newClusterDeploymentRequest(cluster)
				result, err := cr.Reconcile(ctx, request)
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				aci := getTestClusterInstall()
				Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
				Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterNotReadyReason))
				Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterNotReadyMsg))
				Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
			}

			It("create new cluster", func() {
				mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(arg1, arg2 interface{}, params installer.RegisterClusterParams) {
						Expect(swag.StringValue(params.NewClusterParams.OpenshiftVersion)).To(Equal(*openshiftVersion.ReleaseVersion))
						Expect(params.NewClusterParams.OcpReleaseImage).To(Equal(*openshiftVersion.ReleaseImage))
					}).Return(clusterReply, nil)
				mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

				cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
				Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
				aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
				Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
				validateCreation(cluster)
			})

			It("create sno cluster", func() {
				mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(arg1, arg2 interface{}, params installer.RegisterClusterParams) {
						Expect(swag.StringValue(params.NewClusterParams.OpenshiftVersion)).To(Equal(ocpReleaseVersion))
					}).Return(clusterReply, nil)
				mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

				cluster := newClusterDeployment(clusterName, testNamespace,
					getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName))
				Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

				aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, getDefaultSNOAgentClusterInstallSpec(clusterName), cluster)
				Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

				validateCreation(cluster)
			})

			It("create single node cluster", func() {
				mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
					Do(func(ctx, kubeKey interface{}, params installer.RegisterClusterParams) {
						Expect(swag.StringValue(params.NewClusterParams.HighAvailabilityMode)).
							To(Equal(HighAvailabilityModeNone))
					}).Return(clusterReply, nil)
				mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

				cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
				Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

				aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
				aci.Spec.ProvisionRequirements.WorkerAgents = 0
				aci.Spec.ProvisionRequirements.ControlPlaneAgents = 1
				Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

				validateCreation(cluster)
			})
		})

		It("create new cluster backend failure", func() {
			errString := "internal error"
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, errors.Errorf(errString))
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

			cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, getDefaultSNOAgentClusterInstallSpec(clusterName), cluster)
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
		})
	})

	It("not supported platform", func() {
		spec := hivev1.ClusterDeploymentSpec{
			ClusterName: clusterName,
			Provisioning: &hivev1.Provisioning{
				ImageSetRef:            &hivev1.ClusterImageSetReference{Name: imageSetName},
				InstallConfigSecretRef: &corev1.LocalObjectReference{Name: "cluster-install-config"},
			},
			Platform: hivev1.Platform{
				AWS: &aws.Platform{},
			},
			PullSecretRef: &corev1.LocalObjectReference{
				Name: pullSecretName,
			},
		}
		cluster := newClusterDeployment(clusterName, testNamespace, spec)
		cluster.Status = hivev1.ClusterDeploymentStatus{}
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

		request := newClusterDeploymentRequest(cluster)
		result, err := cr.Reconcile(ctx, request)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result).Should(Equal(ctrl.Result{}))
	})

	It("validate owner reference creation", func() {
		sId := strfmt.UUID(uuid.New().String())
		backEndCluster := &common.Cluster{
			Cluster: models.Cluster{
				ID: &sId,
			},
		}
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

		cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
		aci.ObjectMeta.OwnerReferences = nil
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
		request := newClusterDeploymentRequest(cluster)
		_, err := cr.Reconcile(ctx, request)
		Expect(err).ShouldNot(HaveOccurred())
		clusterInstall := &hiveext.AgentClusterInstall{}
		agentClusterInstallKey := types.NamespacedName{
			Namespace: testNamespace,
			Name:      agentClusterInstallName,
		}
		ownref := metav1.OwnerReference{
			APIVersion: cluster.APIVersion,
			Kind:       cluster.Kind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}
		Expect(c.Get(ctx, agentClusterInstallKey, clusterInstall)).To(BeNil())
		Expect(clusterInstall.ObjectMeta.OwnerReferences).NotTo(BeNil())
		Expect(clusterInstall.ObjectMeta.OwnerReferences[0]).To(Equal(ownref))
	})

	It("validate Event URL", func() {
		_, priv, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).NotTo(HaveOccurred())
		os.Setenv("EC_PRIVATE_KEY_PEM", priv)
		defer os.Unsetenv("EC_PRIVATE_KEY_PEM")
		Expect(err).NotTo(HaveOccurred())
		serviceBaseURL := "http://acme.com"
		cr.ServiceBaseURL = serviceBaseURL
		sId := strfmt.UUID(uuid.New().String())
		backEndCluster := &common.Cluster{
			Cluster: models.Cluster{
				ID: &sId,
			},
		}
		expectedEventUrlPrefix := fmt.Sprintf("%s/api/assisted-install/v1/clusters/%s/events", serviceBaseURL, sId)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

		cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
		request := newClusterDeploymentRequest(cluster)
		_, err = cr.Reconcile(ctx, request)
		Expect(err).ShouldNot(HaveOccurred())
		clusterInstall := &hiveext.AgentClusterInstall{}
		agentClusterInstallKey := types.NamespacedName{
			Namespace: testNamespace,
			Name:      agentClusterInstallName,
		}
		Expect(c.Get(ctx, agentClusterInstallKey, clusterInstall)).To(BeNil())
		Expect(clusterInstall.Status.DebugInfo.EventsURL).NotTo(BeNil())
		Expect(clusterInstall.Status.DebugInfo.EventsURL).To(HavePrefix(expectedEventUrlPrefix))
	})

	It("failed to get cluster from backend", func() {
		cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		cluster.Status = hivev1.ClusterDeploymentStatus{}
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

		expectedErr := "expected-error"
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, errors.Errorf(expectedErr))

		request := newClusterDeploymentRequest(cluster)
		result, err := cr.Reconcile(ctx, request)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		aci = getTestClusterInstall()
		expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
		Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
		Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
	})

	It("create cluster without pull secret reference", func() {
		cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		cluster.Spec.PullSecretRef = nil
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
		request := newClusterDeploymentRequest(cluster)

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)

		_, err := cr.Reconcile(ctx, request)
		Expect(err).To(BeNil())

		aci = getTestClusterInstall()

		Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(InputErrorReason))
		Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	Context("cluster deletion", func() {
		var (
			sId strfmt.UUID
			cd  *hivev1.ClusterDeployment
			aci *hiveext.AgentClusterInstall
		)

		BeforeEach(func() {
			defaultClusterSpec = getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName)
			cd = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			cd.Status = hivev1.ClusterDeploymentStatus{}
			defaultAgentClusterInstallSpec = getDefaultAgentClusterInstallSpec(clusterName)
			aci = newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cd)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			mockCtrl = gomock.NewController(GinkgoT())
			mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
			mockClusterApi = cluster.NewMockAPI(mockCtrl)
			mockHostApi = host.NewMockAPI(mockCtrl)
			mockCRDEventsHandler = NewMockCRDEventsHandler(mockCtrl)
			mockManifestsApi = manifests.NewMockClusterManifestsInternals(mockCtrl)
			cr = &ClusterDeploymentsReconciler{
				Client:           c,
				Scheme:           scheme.Scheme,
				Log:              common.GetTestLog(),
				Installer:        mockInstallerInternal,
				ClusterApi:       mockClusterApi,
				HostApi:          mockHostApi,
				CRDEventsHandler: mockCRDEventsHandler,
				Manifests:        mockManifestsApi,
			}
			Expect(c.Create(ctx, cd)).ShouldNot(HaveOccurred())
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())
			imageSet := getDefaultTestImageSet(imageSetName, releaseImage)
			Expect(c.Create(ctx, imageSet)).To(BeNil())
		})

		It("agentClusterInstall resource deleted - verify call to deregister cluster", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil)

			simulateACIDeletionWithFinalizer(ctx, c, aci)
			request := newClusterDeploymentRequest(cd)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))
		})

		It("cluster deregister failed - internal error", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(errors.New("internal error"))

			expectedErrMsg := fmt.Sprintf("failed to deregister cluster: %s: internal error", cd.Name)

			simulateACIDeletionWithFinalizer(ctx, c, aci)
			Expect(c.Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cd)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal(expectedErrMsg))
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		})

		It("agentClusterInstall resource deleted and created again", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil)
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)

			simulateACIDeletionWithFinalizer(ctx, c, aci)
			request := newClusterDeploymentRequest(cd)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))

			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(backEndCluster, nil)

			Expect(c.Delete(ctx, aci)).ShouldNot(HaveOccurred())
			aci = newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cd)
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

			request = newClusterDeploymentRequest(cd)
			result, err = cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})

	Context("cluster installation", func() {
		var (
			sId            strfmt.UUID
			cluster        *hivev1.ClusterDeployment
			aci            *hiveext.AgentClusterInstall
			backEndCluster *common.Cluster
		)

		BeforeEach(func() {
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())
			imageSet := getDefaultTestImageSet(imageSetName, releaseImage)
			Expect(c.Create(ctx, imageSet)).To(BeNil())
			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
			aci = newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
			backEndCluster = &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusReady),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
					Kind:                     swag.String(models.ClusterKindCluster),
				},
				PullSecret: testPullSecretVal,
			}
			hosts := make([]*models.Host, 0, 5)
			for i := 0; i < 5; i++ {
				id := strfmt.UUID(uuid.New().String())
				h := &models.Host{
					ID:     &id,
					Status: swag.String(models.HostStatusKnown),
				}
				hosts = append(hosts, h)
			}
			backEndCluster.Hosts = hosts
		})

		It("success", func() {
			backEndCluster.Status = swag.String(models.ClusterStatusReady)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil).Times(1)

			installClusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:         backEndCluster.ID,
					Status:     swag.String(models.ClusterStatusPreparingForInstallation),
					StatusInfo: swag.String("Waiting for control plane"),
				},
			}
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(installClusterReply, nil)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstallationInProgressReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Message).To(Equal(InstallationInProgressMsg + " Waiting for control plane"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("installed", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(3)
			password := "test"
			username := "admin"
			kubeconfig := "kubeconfig content"
			cred := &models.Credentials{
				Password: password,
				Username: username,
			}
			id := strfmt.UUID(uuid.New().String())
			clusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:     &id,
					Status: swag.String(models.ClusterStatusAddingHosts),
				},
			}
			mockInstallerInternal.EXPECT().GetCredentialsInternal(gomock.Any(), gomock.Any()).Return(cred, nil).Times(1)
			mockInstallerInternal.EXPECT().DownloadClusterKubeconfigInternal(gomock.Any(), gomock.Any()).Return(ioutil.NopCloser(strings.NewReader(kubeconfig)), int64(len(kubeconfig)), nil).Times(1)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockInstallerInternal.EXPECT().RegisterAddHostsClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(clusterReply, nil)
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			cluster = getTestCluster()
			Expect(aci.Spec.ClusterMetadata.ClusterID).To(Equal(openshiftID.String()))
			secretAdmin := getSecret(cluster.Namespace, aci.Spec.ClusterMetadata.AdminPasswordSecretRef.Name)
			Expect(string(secretAdmin.Data["password"])).To(Equal(password))
			Expect(string(secretAdmin.Data["username"])).To(Equal(username))
			secretKubeConfig := getSecret(cluster.Namespace, aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name)
			Expect(string(secretKubeConfig.Data["kubeconfig"])).To(Equal(kubeconfig))

			By("Call reconcile again to test delete of day1 cluster")
			result, err = cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("installed SNO no day2", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.StatusInfo = swag.String("Done")
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(3)
			password := "test"
			username := "admin"
			kubeconfig := "kubeconfig content"
			cred := &models.Credentials{
				Password: password,
				Username: username,
			}
			mockInstallerInternal.EXPECT().GetCredentialsInternal(gomock.Any(), gomock.Any()).Return(cred, nil).Times(1)
			mockInstallerInternal.EXPECT().DownloadClusterKubeconfigInternal(gomock.Any(), gomock.Any()).Return(ioutil.NopCloser(strings.NewReader(kubeconfig)), int64(len(kubeconfig)), nil).Times(1)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			aci.Spec.ProvisionRequirements.WorkerAgents = 0
			aci.Spec.ProvisionRequirements.ControlPlaneAgents = 1
			cluster.Spec.BaseDomain = "hive.example.com"
			Expect(c.Update(ctx, cluster)).Should(BeNil())
			Expect(c.Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstalledReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Message).To(Equal(InstalledMsg + " Done"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionTrue))

			cluster = getTestCluster()
			Expect(aci.Spec.ClusterMetadata.ClusterID).To(Equal(openshiftID.String()))
			secretAdmin := getSecret(cluster.Namespace, aci.Spec.ClusterMetadata.AdminPasswordSecretRef.Name)
			Expect(string(secretAdmin.Data["password"])).To(Equal(password))
			Expect(string(secretAdmin.Data["username"])).To(Equal(username))
			secretKubeConfig := getSecret(cluster.Namespace, aci.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name)
			Expect(string(secretKubeConfig.Data["kubeconfig"])).To(Equal(kubeconfig))

			By("Call reconcile again to test delete of day1 cluster")
			result, err = cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("Fail to delete day1", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(2)
			expectedError := errors.New("internal error")
			expectedErrMsg := fmt.Sprintf("failed to deregister cluster: %s: %s", cluster.Name, expectedError)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(expectedError).Times(1)
			setClusterCondition(&aci.Status.Conditions, hivev1.ClusterInstallCondition{
				Type:    ClusterCompletedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  InstalledReason,
				Message: InstalledMsg,
			})
			Expect(c.Status().Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErrMsg)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstalledReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("Fail to create day2", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(2)
			expectedErr := "internal error"
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockInstallerInternal.EXPECT().RegisterAddHostsClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New(expectedErr))
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
			setClusterCondition(&aci.Status.Conditions, hivev1.ClusterInstallCondition{
				Type:    ClusterCompletedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  InstalledReason,
				Message: InstalledMsg,
			})
			Expect(c.Status().Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(NotAvailableReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionUnknown))
		})

		It("Create day2 if day1 is already deleted none SNO", func() {
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
			id := strfmt.UUID(uuid.New().String())
			clusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:     &id,
					Status: swag.String(models.ClusterStatusAddingHosts),
				},
			}
			mockInstallerInternal.EXPECT().RegisterAddHostsClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(clusterReply, nil)
			mockInstallerInternal.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(openshiftVersion, nil)
			setClusterCondition(&aci.Status.Conditions, hivev1.ClusterInstallCondition{
				Type:    ClusterCompletedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  InstalledReason,
				Message: InstalledMsg,
			})
			Expect(c.Status().Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		})

		It("installed - fail to get kube config", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			password := "test"
			username := "admin"
			cred := &models.Credentials{
				Password: password,
				Username: username,
			}
			mockInstallerInternal.EXPECT().GetCredentialsInternal(gomock.Any(), gomock.Any()).Return(cred, nil).Times(1)
			expectedErr := "internal error"
			mockInstallerInternal.EXPECT().DownloadClusterKubeconfigInternal(gomock.Any(), gomock.Any()).Return(nil, int64(0), errors.New(expectedErr)).Times(1)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(cluster.Spec.ClusterMetadata).To(BeNil())
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstalledReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("installed - fail to get admin password", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindCluster)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			expectedErr := "internal error"
			mockInstallerInternal.EXPECT().GetCredentialsInternal(gomock.Any(), gomock.Any()).Return(nil, errors.New(expectedErr)).Times(1)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			Expect(cluster.Spec.ClusterMetadata).To(BeNil())
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstalledReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("failed to start installation", func() {
			expectedErr := "internal error"
			backEndCluster.Status = swag.String(models.ClusterStatusReady)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(nil, errors.Errorf(expectedErr))
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil).Times(1)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("not ready for installation", func() {
			backEndCluster.Status = swag.String(models.ClusterStatusPendingForInput)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(false, "").Times(1)
			Expect(c.Update(ctx, cluster)).Should(BeNil())
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterNotReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterNotReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("not ready for installation - hosts not approved", func() {
			backEndCluster.Status = swag.String(models.ClusterStatusPendingForInput)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil).Times(5)

			Expect(c.Update(ctx, cluster)).Should(BeNil())
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterNotReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterNotReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("install day2 host", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			backEndCluster.Status = swag.String(models.ClusterStatusAddingHosts)
			id := strfmt.UUID(uuid.New().String())
			h := &models.Host{
				ID:     &id,
				Status: swag.String(models.HostStatusKnown),
			}
			backEndCluster.Hosts = []*models.Host{h}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(1)
			mockInstallerInternal.EXPECT().InstallSingleDay2HostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterAlreadyInstallingReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterAlreadyInstallingMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("install failure day2 host", func() {
			openshiftID := strfmt.UUID(uuid.New().String())
			backEndCluster.Status = swag.String(models.ClusterStatusInstalled)
			backEndCluster.OpenshiftClusterID = openshiftID
			backEndCluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
			backEndCluster.Status = swag.String(models.ClusterStatusAddingHosts)
			id := strfmt.UUID(uuid.New().String())
			h := &models.Host{
				ID:     &id,
				Status: swag.String(models.HostStatusKnown),
			}
			backEndCluster.Hosts = []*models.Host{h}
			expectedErr := "internal error"
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(1)
			mockInstallerInternal.EXPECT().InstallSingleDay2HostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErr))

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterAlreadyInstallingReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterAlreadyInstallingMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("Install with manifests - no configmap", func() {
			aci.Spec.ManifestsConfigMapRef = &corev1.LocalObjectReference{Name: "cluster-install-config"}
			Expect(c.Update(ctx, aci)).Should(BeNil())

			backEndCluster.Status = swag.String(models.ClusterStatusReady)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil).Times(1)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Minute}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).NotTo(Equal(""))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("Update manifests - manifests exists , create failed", func() {
			ref := &corev1.LocalObjectReference{Name: "cluster-install-config"}
			data := map[string]string{"test.yaml": "test"}
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.ObjectMeta.Namespace,
					Name:      "cluster-install-config",
				},
				Data: data,
			}
			Expect(c.Create(ctx, cm)).To(BeNil())

			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil).Times(1)
			mockManifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf("error")).Times(1)
			request := newClusterDeploymentRequest(cluster)
			aci = getTestClusterInstall()
			aci.Spec.ManifestsConfigMapRef = ref
			Expect(c.Update(ctx, aci)).Should(BeNil())
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Minute}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, "error")
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("Update manifests - manifests exists , list failed", func() {
			ref := &corev1.LocalObjectReference{Name: "cluster-install-config"}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf("error")).Times(1)

			request := newClusterDeploymentRequest(cluster)
			cluster = getTestCluster()
			aci.Spec.ManifestsConfigMapRef = ref
			Expect(c.Update(ctx, aci)).Should(BeNil())
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{Requeue: true, RequeueAfter: 1 * time.Minute}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, "error")
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionTrue))
		})

		It("Update manifests - succeed", func() {
			ref := &corev1.LocalObjectReference{Name: "cluster-install-config"}
			data := map[string]string{"test.yaml": "test"}
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.ObjectMeta.Namespace,
					Name:      "cluster-install-config",
				},
				Data: data,
			}
			Expect(c.Create(ctx, cm)).To(BeNil())

			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockManifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{}, nil).Times(1)

			installClusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:         backEndCluster.ID,
					Status:     swag.String(models.ClusterStatusPreparingForInstallation),
					StatusInfo: swag.String("Waiting for control plane"),
				},
			}
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(installClusterReply, nil)

			cluster = getTestCluster()
			aci.Spec.ManifestsConfigMapRef = ref
			Expect(c.Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstallationInProgressReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Message).To(Equal(InstallationInProgressMsg + " Waiting for control plane"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("Update manifests - no manifests", func() {

			installClusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:         backEndCluster.ID,
					Status:     swag.String(models.ClusterStatusPreparingForInstallation),
					StatusInfo: swag.String("Waiting for control plane"),
				},
			}
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(installClusterReply, nil).Times(1)

			By("no manifests")
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstallationInProgressReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Message).To(Equal(InstallationInProgressMsg + " Waiting for control plane"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("Update manifests - delete old + error should be ignored", func() {
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockManifestsApi.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(models.ListManifests{&models.Manifest{FileName: "test", Folder: "test"}, &models.Manifest{FileName: "test2", Folder: "test2"}}, nil).Times(1)
			mockManifestsApi.EXPECT().DeleteClusterManifestInternal(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockManifestsApi.EXPECT().DeleteClusterManifestInternal(gomock.Any(), gomock.Any()).Return(errors.Errorf("ignore it")).Times(1)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(5)
			mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: true}, nil).Times(5)

			installClusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:         backEndCluster.ID,
					Status:     swag.String(models.ClusterStatusPreparingForInstallation),
					StatusInfo: swag.String("Waiting for control plane"),
				},
			}
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(installClusterReply, nil)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Reason).To(Equal(InstallationInProgressReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Message).To(Equal(InstallationInProgressMsg + " Waiting for control plane"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterCompletedCondition).Status).To(Equal(corev1.ConditionFalse))

		})
	})

	It("reconcile on installed sno cluster should not return an error or requeue", func() {
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
		cluster := newClusterDeployment(clusterName, testNamespace,
			getDefaultClusterDeploymentSpec(clusterName, agentClusterInstallName, pullSecretName))
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

		aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, getDefaultSNOAgentClusterInstallSpec(clusterName), cluster)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

		request := newClusterDeploymentRequest(cluster)
		result, err := cr.Reconcile(ctx, request)
		Expect(err).To(BeNil())
		Expect(result.Requeue).To(BeFalse())
	})

	Context("cluster update", func() {
		var (
			sId     strfmt.UUID
			cluster *hivev1.ClusterDeployment
			aci     *hiveext.AgentClusterInstall
		)

		BeforeEach(func() {
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())

			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())

			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			aci = newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
		})

		It("update pull-secret network cidr and cluster name", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     "different-cluster-name",
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       "11.129.0.0/14",
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),

					Status: swag.String(models.ClusterStatusPendingForInput),
				},
				PullSecret: "different-pull-secret",
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			updateReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:         &sId,
					Status:     swag.String(models.ClusterStatusInsufficient),
					StatusInfo: swag.String(models.ClusterStatusInsufficient),
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, param installer.UpdateClusterParams) {
					Expect(swag.StringValue(param.ClusterUpdateParams.PullSecret)).To(Equal(testPullSecretVal))
					Expect(swag.StringValue(param.ClusterUpdateParams.Name)).To(Equal(defaultClusterSpec.ClusterName))
					Expect(swag.StringValue(param.ClusterUpdateParams.ClusterNetworkCidr)).
						To(Equal(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR))
				}).Return(updateReply, nil)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterNotReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterNotReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("only state changed", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusInsufficient),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
					Kind:                     swag.String(models.ClusterKindCluster),
					ValidationsInfo:          "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Check3 is not OK\"}]}",
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(false, "").Times(1)

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			aci = getTestClusterInstall()
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Reason).To(Equal(ClusterNotReadyReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Message).To(Equal(ClusterNotReadyMsg))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterRequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterValidatedCondition).Reason).To(Equal(ValidationsFailingReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterValidatedCondition).Message).To(Equal(ClusterValidationsFailingMsg + " Check1 is not OK,Check3 is not OK"))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterValidatedCondition).Status).To(Equal(corev1.ConditionFalse))
		})

		It("failed getting cluster", func() {
			expectedErr := "some internal error"
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).
				Return(nil, errors.Errorf(expectedErr))

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, expectedErr)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
		})

		It("update internal error", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                 &sId,
					Name:               "different-cluster-name",
					OpenshiftVersion:   "4.8",
					ClusterNetworkCidr: "11.129.0.0/14",
					Status:             swag.String(models.ClusterStatusPendingForInput),
				},
				PullSecret: "different-pull-secret",
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

			errString := "update internal error"
			mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
				Return(nil, errors.Errorf(errString))
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			aci = getTestClusterInstall()
			expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
			Expect(FindStatusCondition(aci.Status.Conditions, ClusterSpecSyncedCondition).Message).To(Equal(expectedState))
		})

		It("add install config overrides annotation", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusInsufficient),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			installConfigOverrides := `{"controlPlane": {"hyperthreading": "Disabled"}}`
			updateReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:                     &sId,
					Status:                 swag.String(models.ClusterStatusInsufficient),
					InstallConfigOverrides: installConfigOverrides,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().UpdateClusterInstallConfigInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, param installer.UpdateClusterInstallConfigParams) {
					Expect(param.ClusterID).To(Equal(sId))
					Expect(param.InstallConfigParams).To(Equal(installConfigOverrides))
				}).Return(updateReply, nil)
			// Add annotation
			aci.ObjectMeta.SetAnnotations(map[string]string{InstallConfigOverrides: installConfigOverrides})
			Expect(c.Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("Remove existing install config overrides annotation", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusInsufficient),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
					InstallConfigOverrides:   `{"controlPlane": {"hyperthreading": "Disabled"}}`,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			updateReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:                     &sId,
					Status:                 swag.String(models.ClusterStatusInsufficient),
					InstallConfigOverrides: "",
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().UpdateClusterInstallConfigInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, param installer.UpdateClusterInstallConfigParams) {
					Expect(param.ClusterID).To(Equal(sId))
					Expect(param.InstallConfigParams).To(Equal(""))
				}).Return(updateReply, nil)
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("Update install config overrides annotation", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusInsufficient),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
					InstallConfigOverrides:   `{"controlPlane": {"hyperthreading": "Disabled"}}`,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			installConfigOverrides := `{"controlPlane": {"hyperthreading": "Enabled"}}`
			updateReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:                     &sId,
					Status:                 swag.String(models.ClusterStatusInsufficient),
					InstallConfigOverrides: installConfigOverrides,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().UpdateClusterInstallConfigInternal(gomock.Any(), gomock.Any()).
				Do(func(ctx context.Context, param installer.UpdateClusterInstallConfigParams) {
					Expect(param.ClusterID).To(Equal(sId))
					Expect(param.InstallConfigParams).To(Equal(installConfigOverrides))
				}).Return(updateReply, nil)
			// Add annotation
			aci.ObjectMeta.SetAnnotations(map[string]string{InstallConfigOverrides: installConfigOverrides})
			Expect(c.Update(ctx, aci)).Should(BeNil())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})

	})

	Context("cluster update not needed", func() {
		var (
			sId strfmt.UUID
		)

		BeforeEach(func() {
			id := uuid.New()
			sId = strfmt.UUID(id.String())
		})

		It("SSHPublicKey in ClusterDeployment has spaces in suffix", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         "4.8",
					ClusterNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].CIDR,
					ClusterNetworkHostPrefix: int64(defaultAgentClusterInstallSpec.Networking.ClusterNetwork[0].HostPrefix),
					Status:                   swag.String(models.ClusterStatusInsufficient),
					ServiceNetworkCidr:       defaultAgentClusterInstallSpec.Networking.ServiceNetwork[0],
					IngressVip:               defaultAgentClusterInstallSpec.IngressVIP,
					APIVip:                   defaultAgentClusterInstallSpec.APIVIP,
					BaseDNSDomain:            defaultClusterSpec.BaseDomain,
					SSHPublicKey:             defaultAgentClusterInstallSpec.SSHPublicKey,
					Hyperthreading:           models.ClusterHyperthreadingAll,
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())

			cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			aci := newAgentClusterInstall(agentClusterInstallName, testNamespace, defaultAgentClusterInstallSpec, cluster)
			sshPublicKeySuffixSpace := fmt.Sprintf("%s ", defaultAgentClusterInstallSpec.SSHPublicKey)
			aci.Spec.SSHPublicKey = sshPublicKeySuffixSpace
			Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})
})

var _ = Describe("TestConditions", func() {
	var (
		c                      client.Client
		cr                     *ClusterDeploymentsReconciler
		ctx                    = context.Background()
		mockCtrl               *gomock.Controller
		backEndCluster         *common.Cluster
		clusterRequest         ctrl.Request
		clusterKey             types.NamespacedName
		agentClusterInstallKey types.NamespacedName
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal := bminventory.NewMockInstallerInternals(mockCtrl)
		cr = &ClusterDeploymentsReconciler{
			Client:    c,
			Scheme:    scheme.Scheme,
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
		sId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
		backEndCluster = &common.Cluster{}
		clusterKey = types.NamespacedName{
			Namespace: testNamespace,
			Name:      "clusterDeployment",
		}
		agentClusterInstallKey = types.NamespacedName{
			Namespace: testNamespace,
			Name:      "agentClusterInstall",
		}
		clusterDeployment := newClusterDeployment(clusterKey.Name, clusterKey.Namespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", agentClusterInstallKey.Name, "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		aci := newAgentClusterInstall(agentClusterInstallKey.Name, agentClusterInstallKey.Namespace, getDefaultAgentClusterInstallSpec(clusterKey.Name), clusterDeployment)
		Expect(c.Create(ctx, aci)).ShouldNot(HaveOccurred())
		clusterRequest = newClusterDeploymentRequest(clusterDeployment)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	tests := []struct {
		name           string
		clusterStatus  string
		statusInfo     string
		validationInfo string
		conditions     []hivev1.ClusterInstallCondition
	}{
		{
			name:           "Unsufficient",
			clusterStatus:  models.ClusterStatusInsufficient,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Check3 is not OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterNotReadyMsg,
					Reason:  ClusterNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsFailingMsg + " Check1 is not OK,Check3 is not OK",
					Reason:  ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterNotStoppedMsg,
					Reason:  ClusterNotStoppedReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "PendingForInput",
			clusterStatus:  models.ClusterStatusPendingForInput,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Check3 is not OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterNotReadyMsg,
					Reason:  ClusterNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsFailingMsg + " Check1 is not OK,Check3 is not OK",
					Reason:  ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterNotStoppedMsg,
					Reason:  ClusterNotStoppedReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "AddingHosts",
			clusterStatus:  models.ClusterStatusAddingHosts,
			statusInfo:     "Done",
			validationInfo: "",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterAlreadyInstallingMsg,
					Reason:  ClusterAlreadyInstallingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstalledMsg + " Done",
					Reason:  InstalledReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsOKMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterNotStoppedMsg,
					Reason:  ClusterNotStoppedReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Installed",
			clusterStatus:  models.ClusterStatusInstalled,
			statusInfo:     "Done",
			validationInfo: "{\"some-check\":[{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterInstallationStoppedMsg,
					Reason:  ClusterInstallationStoppedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstalledMsg + " Done",
					Reason:  InstalledReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsOKMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterStoppedCompletedMsg,
					Reason:  ClusterStoppedCompletedReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Installing",
			clusterStatus:  models.ClusterStatusInstalling,
			statusInfo:     "Phase 1",
			validationInfo: "{\"some-check\":[{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterAlreadyInstallingMsg,
					Reason:  ClusterAlreadyInstallingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstallationInProgressMsg + " Phase 1",
					Reason:  InstallationInProgressReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsOKMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterNotStoppedMsg,
					Reason:  ClusterNotStoppedReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Ready",
			clusterStatus:  models.ClusterStatusReady,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterReadyMsg,
					Reason:  ClusterReadyReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsOKMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterNotFailedMsg,
					Reason:  ClusterNotFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterNotStoppedMsg,
					Reason:  ClusterNotStoppedReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Error",
			clusterStatus:  models.ClusterStatusError,
			statusInfo:     "failed due to some error",
			validationInfo: "{\"some-check\":[{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Check2 is OK\"}]}",
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    ClusterRequirementsMetCondition,
					Message: ClusterInstallationStoppedMsg,
					Reason:  ClusterInstallationStoppedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterCompletedCondition,
					Message: InstallationFailedMsg + " failed due to some error",
					Reason:  InstallationFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ClusterValidatedCondition,
					Message: ClusterValidationsOKMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterFailedCondition,
					Message: ClusterFailedMsg + " failed due to some error",
					Reason:  ClusterFailedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ClusterStoppedCondition,
					Message: ClusterStoppedFailedMsg,
					Reason:  ClusterStoppedFailedReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			backEndCluster.Status = swag.String(t.clusterStatus)
			backEndCluster.StatusInfo = swag.String(t.statusInfo)
			backEndCluster.ValidationsInfo = t.validationInfo
			cid := strfmt.UUID(uuid.New().String())
			backEndCluster.ID = &cid
			_, err := cr.Reconcile(ctx, clusterRequest)
			Expect(err).To(BeNil())
			cluster := &hivev1.ClusterDeployment{}
			Expect(c.Get(ctx, clusterKey, cluster)).To(BeNil())
			clusterInstall := &hiveext.AgentClusterInstall{}
			Expect(c.Get(ctx, agentClusterInstallKey, clusterInstall)).To(BeNil())
			for _, cond := range t.conditions {
				Expect(FindStatusCondition(clusterInstall.Status.Conditions, cond.Type).Message).To(Equal(cond.Message))
				Expect(FindStatusCondition(clusterInstall.Status.Conditions, cond.Type).Reason).To(Equal(cond.Reason))
				Expect(FindStatusCondition(clusterInstall.Status.Conditions, cond.Type).Status).To(Equal(cond.Status))
			}

		})
	}
})
