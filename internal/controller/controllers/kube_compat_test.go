package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ServerIsOpenShift", func() {
	var (
		client client.Client
		ctx    context.Context
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
		client = fake.NewClientBuilder().WithScheme(scheme).Build()
		ctx = context.Background()
	})

	It("returns true when the clusterversion CRD is present", func() {
		cvCRD := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterVersionCRDName,
			},
		}
		Expect(client.Create(ctx, cvCRD)).To(Succeed())
		isOCP, err := ServerIsOpenShift(ctx, client)
		Expect(err).To(BeNil())
		Expect(isOCP).To(BeTrue())
	})

	It("returns false when the clusterversion CRD is not present", func() {
		isOCP, err := ServerIsOpenShift(ctx, client)
		Expect(err).To(BeNil())
		Expect(isOCP).To(BeFalse())
	})
})
