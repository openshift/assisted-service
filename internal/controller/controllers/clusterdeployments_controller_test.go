package controllers

import (
	"context"
	"fmt"

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
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/pkg/apis/hive/v1"
	"github.com/openshift/hive/pkg/apis/hive/v1/agent"
	"github.com/openshift/hive/pkg/apis/hive/v1/aws"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
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

func getDefaultClusterDeploymentSpec(clusterName, pullSecretName string) hivev1.ClusterDeploymentSpec {
	return hivev1.ClusterDeploymentSpec{
		ClusterName: clusterName,
		Provisioning: &hivev1.Provisioning{
			ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "openshift-v4.7.0"},
			InstallConfigSecretRef: v1.LocalObjectReference{Name: "cluster-install-config"},
		},
		InstallStrategy: &hivev1.InstallStrategy{
			Agent: &agent.InstallStrategy{
				Networking: agent.Networking{
					MachineNetwork: nil,
					ClusterNetwork: []agent.ClusterNetworkEntry{{
						CIDR:       "10.128.0.0/14",
						HostPrefix: 23,
					}},
					ServiceNetwork: []string{"172.30.0.0/16"},
				},
				SSHPublicKey: "some-key",
				ProvisionRequirements: agent.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       2,
				},
			},
		},
		Platform: hivev1.Platform{
			AgentBareMetal: &agent.Platform{
				APIVIP:            "1.2.3.8",
				APIVIPDNSName:     "example.com",
				IngressVIP:        "1.2.3.9",
				VIPDHCPAllocation: false,
			},
		},
		PullSecretRef: &v1.LocalObjectReference{
			Name: pullSecretName,
		},
	}
}

func getConditionByReason(reason string, cluster *hivev1.ClusterDeployment) hivev1.ClusterDeploymentCondition {
	index := findConditionIndexByReason(reason, &cluster.Status.Conditions)
	Expect(index >= 0).Should(BeTrue(), fmt.Sprintf("condition %s was not found in cluster deployment", reason))
	return cluster.Status.Conditions[index]
}

var _ = Describe("cluster reconcile", func() {
	var (
		c                     client.Client
		cr                    *ClusterDeploymentsReconciler
		ctx                   = context.Background()
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		mockClusterApi        *cluster.MockAPI
		mockHostApi           *host.MockAPI
		clusterName           = "test-cluster"
		pullSecretName        = "pull-secret"
	)

	defaultClusterSpec := getDefaultClusterDeploymentSpec(clusterName, pullSecretName)

	getTestCluster := func() *hivev1.ClusterDeployment {
		var cluster hivev1.ClusterDeployment
		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      clusterName,
		}
		Expect(c.Get(ctx, key, &cluster)).To(BeNil())
		return &cluster
	}

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockClusterApi = cluster.NewMockAPI(mockCtrl)
		mockHostApi = host.NewMockAPI(mockCtrl)
		cr = &ClusterDeploymentsReconciler{
			Client:     c,
			Scheme:     scheme.Scheme,
			Log:        common.GetTestLog(),
			Installer:  mockInstallerInternal,
			ClusterApi: mockClusterApi,
			HostApi:    mockHostApi,
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
		})

		It("create new cluster", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterReply := &common.Cluster{
				Cluster: models.Cluster{
					Status:     swag.String(models.ClusterStatusPendingForInput),
					StatusInfo: swag.String("User input required"),
					ID:         &id,
				},
			}
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(clusterReply, nil)

			cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(getConditionByReason(AgentPlatformState, cluster).Message).To(Equal(models.ClusterStatusPendingForInput))
			Expect(getConditionByReason(AgentPlatformStateInfo, cluster).Message).To(Equal("User input required"))
		})

		It("create new cluster backend failure", func() {
			expectedError := errors.Errorf("internal error")
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, expectedError)

			cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			cluster = getTestCluster()
			Expect(getConditionByReason(AgentPlatformError, cluster).Message).To(Equal(expectedError.Error()))
		})
	})

	It("not supported platform", func() {
		spec := hivev1.ClusterDeploymentSpec{
			ClusterName: clusterName,
			Provisioning: &hivev1.Provisioning{
				ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "openshift-v4.7.0"},
				InstallConfigSecretRef: v1.LocalObjectReference{Name: "cluster-install-config"},
			},
			Platform: hivev1.Platform{
				AWS: &aws.Platform{},
			},
			PullSecretRef: &v1.LocalObjectReference{
				Name: pullSecretName,
			},
		}
		cluster := newClusterDeployment(clusterName, testNamespace, spec)
		cluster.Status = hivev1.ClusterDeploymentStatus{}
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

		request := newClusterDeploymentRequest(cluster)
		result, err := cr.Reconcile(request)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(result).Should(Equal(ctrl.Result{}))
	})

	It("failed to get cluster from backend", func() {
		cluster := newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
		cluster.Status = hivev1.ClusterDeploymentStatus{}
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

		expectedErr := "expected-error"
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, errors.Errorf(expectedErr))

		request := newClusterDeploymentRequest(cluster)
		result, err := cr.Reconcile(request)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		cluster = getTestCluster()
		Expect(getConditionByReason(AgentPlatformError, cluster).Message).To(Equal(expectedErr))
	})

	Context("cluster deletion", func() {
		var (
			sId     strfmt.UUID
			cluster *hivev1.ClusterDeployment
		)

		BeforeEach(func() {
			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			cluster.Status = hivev1.ClusterDeploymentStatus{}
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())
		})

		It("cluster resource deleted - verify call to deregister cluster", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil)

			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))
		})

		It("cluster deregister failed - internal error", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(errors.New("internal error"))

			expectedErrMsg := fmt.Sprintf("failed to deregister cluster: %s: internal error", cluster.Name)

			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal(expectedErrMsg))
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		})

		It("cluster resource deleted and created again", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(nil)

			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
			request := newClusterDeploymentRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))

			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(backEndCluster, nil)

			cluster = newClusterDeployment(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			request = newClusterDeploymentRequest(cluster)
			result, err = cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})
})
