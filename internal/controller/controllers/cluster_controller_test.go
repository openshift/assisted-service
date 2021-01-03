package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
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

func newCluster(name, namespace string) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		Spec: v1alpha1.ClusterSpec{},
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

func newClusterReconciler(c client.Client) *ClusterReconciler {
	return &ClusterReconciler{
		Client: c,
		Scheme: scheme.Scheme,
		Log:    getTestLog(),
	}
}

var _ = Describe("cluster reconcile", func() {
	var (
		c   client.Client
		cr  *ClusterReconciler
		ctx = context.Background()
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		cr = newClusterReconciler(c)
	})

	It("try reconcile", func() {
		cluster := newCluster("name", "namespace")
		Expect(c.Create(ctx, cluster)).ShouldNot(HaveOccurred())
		request := newClusterRequest(cluster)
		_, err := cr.Reconcile(request)
		Expect(err).ShouldNot(HaveOccurred())
		modifiedCluster := &v1alpha1.Cluster{}
		Expect(cr.Get(context.TODO(), request.NamespacedName, modifiedCluster)).ShouldNot(HaveOccurred())
		Expect(modifiedCluster.CreationTimestamp).Should(Equal(cluster.CreationTimestamp))
	})
})
