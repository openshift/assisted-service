package controllers

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

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
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newClusterRequest(cluster *v1alpha1.Cluster) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      cluster.ObjectMeta.Name,
			Namespace: cluster.ObjectMeta.Namespace,
		},
	}
}

func newCluster(name, namespace string, spec v1alpha1.ClusterSpec) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: spec,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "adi.io.my.domain/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func getDefaultClusterSpec(clusterName, pullSecretName string) v1alpha1.ClusterSpec {
	return v1alpha1.ClusterSpec{
		Name:             clusterName,
		OpenshiftVersion: "4.7",
		PullSecretRef: &v1.SecretReference{
			Name:      pullSecretName,
			Namespace: testNamespace,
		},
		ClusterNetworkCidr:       "10.128.0.0/14",
		ClusterNetworkHostPrefix: 23,
	}
}

var _ = Describe("cluster reconcile", func() {
	var (
		c                     client.Client
		cr                    *ClusterReconciler
		ctx                   = context.Background()
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		mockClusterApi        *cluster.MockAPI
		mockHostApi           *host.MockAPI
		clusterName           = "test-cluster"
		pullSecretName        = "pull-secret"
	)

	defaultClusterSpec := getDefaultClusterSpec(clusterName, pullSecretName)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		mockClusterApi = cluster.NewMockAPI(mockCtrl)
		mockHostApi = host.NewMockAPI(mockCtrl)
		cr = &ClusterReconciler{
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

	getTestCluster := func() *v1alpha1.Cluster {
		var cluster v1alpha1.Cluster
		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      clusterName,
		}
		Expect(c.Get(ctx, key, &cluster)).To(BeNil())
		return &cluster
	}

	getTestClusterExpectError := func() error {
		var cluster v1alpha1.Cluster
		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      clusterName,
		}
		err := c.Get(ctx, key, &cluster)
		Expect(err).Should(HaveOccurred())
		return err
	}

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

			cluster := newCluster(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(cluster.Status.ID).To(Equal(id.String()))
			Expect(cluster.Status.State).To(Equal(models.ClusterStatusPendingForInput))
			Expect(cluster.Status.StateInfo).To(Equal("User input required"))
		})

		It("create new cluster backend failure", func() {
			expectedError := errors.Errorf("internal error")
			mockInstallerInternal.EXPECT().RegisterClusterInternal(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, expectedError)

			cluster := newCluster(clusterName, testNamespace, defaultClusterSpec)
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			cluster = getTestCluster()
			Expect(cluster.Status.Error).To(Equal(expectedError.Error()))
		})
	})

	Context("cluster update", func() {
		var (
			sId     strfmt.UUID
			cluster *v1alpha1.Cluster
		)

		BeforeEach(func() {
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())

			cluster = newCluster(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			cluster.Status = v1alpha1.ClusterStatus{
				State: models.ClusterStatusPendingForInput,
				ID:    id.String(),
			}
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("update pull-secret network cidr and cluster name", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     "different-cluster-name",
					OpenshiftVersion:         defaultClusterSpec.OpenshiftVersion,
					ClusterNetworkCidr:       "11.129.0.0/14",
					ClusterNetworkHostPrefix: defaultClusterSpec.ClusterNetworkHostPrefix,
					Status:                   swag.String(models.ClusterStatusPendingForInput),
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
			mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).Return(updateReply, nil)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(cluster.Status.State).To(Equal(models.ClusterStatusInsufficient))
			Expect(cluster.Status.StateInfo).To(Equal(models.ClusterStatusInsufficient))
		})

		It("only state changed", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         defaultClusterSpec.OpenshiftVersion,
					ClusterNetworkCidr:       defaultClusterSpec.ClusterNetworkCidr,
					ClusterNetworkHostPrefix: defaultClusterSpec.ClusterNetworkHostPrefix,
					Status:                   swag.String(models.ClusterStatusInsufficient),
				},
				PullSecret: testPullSecretVal,
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(false, "").Times(1)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(cluster.Status.State).To(Equal(models.ClusterStatusInsufficient))
		})

		It("failed getting cluster", func() {
			expectedError := errors.Errorf("some internal errro")
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).
				Return(nil, expectedError)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
			cluster = getTestCluster()
			Expect(cluster.Status.Error).To(Equal(expectedError.Error()))
		})

		It("update internal error", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     "different-cluster-name",
					OpenshiftVersion:         defaultClusterSpec.OpenshiftVersion,
					ClusterNetworkCidr:       "11.129.0.0/14",
					ClusterNetworkHostPrefix: defaultClusterSpec.ClusterNetworkHostPrefix,
					Status:                   swag.String(models.ClusterStatusPendingForInput),
				},
				PullSecret: "different-pull-secret",
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

			expectedUpdateError := errors.Errorf("update internal error")
			mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
				Return(nil, expectedUpdateError)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			cluster = getTestCluster()
			Expect(cluster.Status.Error).NotTo(Equal(""))
			Expect(cluster.Status.State).To(Equal(models.ClusterStatusPendingForInput))
		})
	})

	Context("cluster installation", func() {
		var (
			sId            strfmt.UUID
			cluster        *v1alpha1.Cluster
			backEndCluster *common.Cluster
		)
		BeforeEach(func() {
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())
			cluster = newCluster(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			cluster.Status = v1alpha1.ClusterStatus{
				State: models.ClusterStatusPendingForInput,
				ID:    id.String(),
			}
			cluster.Spec.ProvisionRequirements = v1alpha1.ProvisionRequirements{
				ControlPlaneAgents: 3,
				WorkerAgents:       1,
			}
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
			backEndCluster = &common.Cluster{
				Cluster: models.Cluster{
					ID:                       &sId,
					Name:                     clusterName,
					OpenshiftVersion:         defaultClusterSpec.OpenshiftVersion,
					ClusterNetworkCidr:       defaultClusterSpec.ClusterNetworkCidr,
					ClusterNetworkHostPrefix: defaultClusterSpec.ClusterNetworkHostPrefix,
				},
				PullSecret: testPullSecretVal,
			}
			hosts := []*models.Host{}
			for i := 0; i < 4; i++ {
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
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(4)

			installClusterReply := &common.Cluster{
				Cluster: models.Cluster{
					ID:     backEndCluster.ID,
					Status: swag.String(models.ClusterStatusPreparingForInstallation),
				},
			}
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(installClusterReply, nil)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(cluster.Status.State).Should(Equal(models.ClusterStatusPreparingForInstallation))
		})

		It("failed to start installation", func() {
			backEndCluster.Status = swag.String(models.ClusterStatusReady)
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().InstallClusterInternal(gomock.Any(), gomock.Any()).
				Return(nil, errors.Errorf("error"))
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
			mockHostApi.EXPECT().IsInstallable(gomock.Any()).Return(true).Times(4)

			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))

			cluster = getTestCluster()
			Expect(cluster.Status.State).Should(Equal(models.ClusterStatusReady))
		})

		It("not ready for installation", func() {
			backEndCluster.Status = swag.String(models.ClusterStatusPendingForInput)
			mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(false, "").Times(1)
			Expect(c.Update(ctx, cluster)).Should(BeNil())
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))

			cluster = getTestCluster()
			Expect(cluster.Status.State).Should(Equal(models.ClusterStatusPendingForInput))
		})

	})

	It("cluster not found", func() {
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound)
		cluster := newCluster(clusterName, testNamespace, v1alpha1.ClusterSpec{})
		request := newClusterRequest(cluster)
		result, err := cr.Reconcile(request)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	Context("cluster deletion", func() {
		var (
			sId     strfmt.UUID
			cluster *v1alpha1.Cluster
		)

		BeforeEach(func() {
			pullSecret := getDefaultTestPullSecret("pull-secret", testNamespace)
			Expect(c.Create(ctx, pullSecret)).To(BeNil())

			cluster = newCluster(clusterName, testNamespace, defaultClusterSpec)
			id := uuid.New()
			sId = strfmt.UUID(id.String())
			cluster.Status = v1alpha1.ClusterStatus{
				State: models.ClusterStatusPendingForInput,
				ID:    id.String(),
			}
			Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		})

		It("cluster deleted from db - verify CRD deletion", func() {
			deletedAt := strfmt.DateTime(time.Now())
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:        &sId,
					DeletedAt: &deletedAt,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))

			err = getTestClusterExpectError()
			Expect(k8serrors.IsNotFound(err)).Should(Equal(true))
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
			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(result).Should(Equal(ctrl.Result{}))
		})

		It("cluster resource deleted failed - internal error", func() {
			backEndCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &sId,
				},
			}
			mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
			mockInstallerInternal.EXPECT().DeregisterClusterInternal(gomock.Any(), gomock.Any()).Return(errors.New("internal error"))

			expectedErrMsg := fmt.Sprintf("failed to delete cluster from db: %s: internal error", sId.String())

			Expect(c.Delete(ctx, cluster)).ShouldNot(HaveOccurred())
			request := newClusterRequest(cluster)
			result, err := cr.Reconcile(request)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal(expectedErrMsg))
			Expect(result).Should(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		})
	})
})
