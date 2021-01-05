package controllers

import (
	"context"

	"github.com/jinzhu/gorm"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "k8s.io/api/core/v1"
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
		clusterName           = "test-cluster"
		pullSecretName        = "pull-secret"
	)

	defaultClusterSpec := getDefaultClusterSpec(clusterName, pullSecretName)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		cr = &ClusterReconciler{
			Client:    c,
			Scheme:    scheme.Scheme,
			Log:       getTestLog(),
			Installer: mockInstallerInternal,
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

	It("cluster not found", func() {
		cluster := newCluster(clusterName, testNamespace, v1alpha1.ClusterSpec{})
		request := newClusterRequest(cluster)
		result, err := cr.Reconcile(request)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})
