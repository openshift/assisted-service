package controllers

import (
	"context"

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

	Context("create cluster", func() {
		BeforeEach(func() {
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

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      clusterName,
			}
			Expect(c.Get(ctx, key, cluster)).To(BeNil())
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
			Expect(result).To(Equal(ctrl.Result{}))

			key := types.NamespacedName{
				Namespace: testNamespace,
				Name:      clusterName,
			}
			Expect(c.Get(ctx, key, cluster)).To(BeNil())
			Expect(cluster.Status.Error).To(Equal(expectedError.Error()))
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
