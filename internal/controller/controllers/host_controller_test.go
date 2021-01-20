package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newHostRequest(host *v1alpha1.Host) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: host.ObjectMeta.Namespace,
		Name:      host.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newHost(name, namespace string, spec v1alpha1.HostSpec) *v1alpha1.Host {
	return &v1alpha1.Host{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Host",
			APIVersion: "adi.io.my.domain/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("host reconcile", func() {
	var (
		c   client.Client
		hr  *HostReconciler
		ctx = context.Background()
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		hr = &HostReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    common.GetTestLog(),
		}
	})

	It("none exiting host", func() {
		host := newHost("host", "namespace", v1alpha1.HostSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())

		noneExistingHost := newHost("host2", "namespace", v1alpha1.HostSpec{})

		result, err := hr.Reconcile(newHostRequest(noneExistingHost))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})
