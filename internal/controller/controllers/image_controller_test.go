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

func newImageRequest(image *v1alpha1.Image) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: image.ObjectMeta.Namespace,
		Name:      image.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newImage(name, namespace string, spec v1alpha1.ImageSpec) *v1alpha1.Image {
	return &v1alpha1.Image{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Image",
			APIVersion: "adi.io.my.domain/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("image reconcile", func() {
	var (
		c   client.Client
		ir  *ImageReconciler
		ctx = context.Background()
	)

	BeforeEach(func() {
		c = fakeclient.NewFakeClientWithScheme(scheme.Scheme)
		ir = &ImageReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    getTestLog(),
		}
	})

	It("none exiting image", func() {
		image := newImage("image", "namespace", v1alpha1.ImageSpec{})
		Expect(c.Create(ctx, image)).To(BeNil())

		noneExistingImage := newImage("image2", "namespace", v1alpha1.ImageSpec{})

		result, err := ir.Reconcile(newImageRequest(noneExistingImage))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})
