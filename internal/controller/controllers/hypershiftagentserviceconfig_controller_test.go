package controllers

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("HypershiftAgentServiceConfig reconcile", func() {
	var (
		ctx                  = context.Background()
		hr                   *HypershiftAgentServiceConfigReconciler
		hsc                  *aiv1beta1.HypershiftAgentServiceConfig
		crd                  *apiextensionsv1.CustomResourceDefinition
		kubeconfigSecret     *corev1.Secret
		mockCtrl             *gomock.Controller
		mockSpokeClient      *spoke_k8s_client.MockSpokeK8sClient
		mockSpokeClientCache *MockSpokeClientCache
	)

	const (
		testKubeconfigSecretName = "test-secret"
		testCRDName              = "agent-install"
	)

	newHypershiftAgentServiceConfigRequest := func(asc *aiv1beta1.HypershiftAgentServiceConfig) ctrl.Request {
		namespacedName := types.NamespacedName{
			Namespace: asc.ObjectMeta.Namespace,
			Name:      asc.ObjectMeta.Name,
		}
		return ctrl.Request{NamespacedName: namespacedName}
	}

	newHSCDefault := func() *aiv1beta1.HypershiftAgentServiceConfig {
		baseAsc := newASCDefault()
		return &aiv1beta1.HypershiftAgentServiceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testName,
				Namespace: testNamespace,
			},
			Spec: aiv1beta1.HypershiftAgentServiceConfigSpec{
				AgentServiceConfigSpec: aiv1beta1.AgentServiceConfigSpec{
					FileSystemStorage: baseAsc.Spec.FileSystemStorage,
					DatabaseStorage:   baseAsc.Spec.DatabaseStorage,
					ImageStorage:      baseAsc.Spec.ImageStorage,
				},

				KubeconfigSecretRef: corev1.LocalObjectReference{
					Name: testKubeconfigSecretName,
				},
			},
		}
	}

	newKubeconfigSecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testKubeconfigSecretName,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"kubeconfig": []byte(BASIC_KUBECONFIG),
			},
			Type: corev1.SecretTypeOpaque,
		}
	}

	newHSCTestReconciler := func(mockSpokeClientCache *MockSpokeClientCache, initObjs ...runtime.Object) *HypershiftAgentServiceConfigReconciler {
		schemes := GetKubeClientSchemes()
		c := fakeclient.NewClientBuilder().WithScheme(schemes).WithRuntimeObjects(initObjs...).Build()
		return &HypershiftAgentServiceConfigReconciler{
			Client:       c,
			Scheme:       c.Scheme(),
			Log:          common.GetTestLog(),
			SpokeClients: mockSpokeClientCache,
		}
	}

	newAgentInstallCRD := func() *apiextensionsv1.CustomResourceDefinition {
		c := &apiextensionsv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: testCRDName,
				Labels: map[string]string{
					fmt.Sprintf("operators.coreos.com/assisted-service-operator.%s", testNamespace): "",
				},
			},
			Spec:   apiextensionsv1.CustomResourceDefinitionSpec{},
			Status: apiextensionsv1.CustomResourceDefinitionStatus{},
		}
		c.ResourceVersion = ""
		return c
	}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockSpokeClient = spoke_k8s_client.NewMockSpokeK8sClient(mockCtrl)
		mockSpokeClientCache = NewMockSpokeClientCache(mockCtrl)

		hsc = newHSCDefault()
		kubeconfigSecret = newKubeconfigSecret()
		crd = newAgentInstallCRD()
		hr = newHSCTestReconciler(mockSpokeClientCache, hsc, kubeconfigSecret, crd)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("runs without error", func() {
		mockSpokeClientCache.EXPECT().Get(gomock.Any()).Return(mockSpokeClient, nil)
		mockSpokeClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		mockSpokeClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		crdKey := client.ObjectKeyFromObject(crd)
		Expect(hr.Client.Get(ctx, crdKey, crd)).To(Succeed())
		res, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("fails due to missing kubeconfig secret", func() {
		Expect(hr.Client.Delete(ctx, kubeconfigSecret)).To(Succeed())
		_, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("Failed to get '%s' secret in '%s' namespace",
				hsc.Spec.KubeconfigSecretRef.Name, testNamespace)))
	})

	It("fails due to invalid key in kubeconfig secret", func() {
		hsc.Spec.KubeconfigSecretRef.Name = "invalid"
		secret := newKubeconfigSecret()
		secret.ObjectMeta.Name = hsc.Spec.KubeconfigSecretRef.Name
		secret.Data = map[string][]byte{
			"invalid": []byte(BASIC_KUBECONFIG),
		}
		Expect(hr.Client.Create(ctx, secret)).To(Succeed())
		_, err := hr.createSpokeClient(ctx, secret.Name, secret.Namespace)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("Secret '%s' does not contain '%s' key value",
				hsc.Spec.KubeconfigSecretRef.Name, "kubeconfig")))
	})

	It("fails due to an error getting client", func() {
		mockSpokeClientCache.EXPECT().Get(gomock.Any()).Return(mockSpokeClient, errors.Errorf("error"))
		_, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("Failed to create client"))
	})

	It("fails due to missing agent-install CRDs on management cluster", func() {
		mockSpokeClientCache.EXPECT().Get(gomock.Any()).Return(mockSpokeClient, nil)
		Expect(hr.Client.Delete(ctx, crd)).To(Succeed())
		_, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("agent-install CRDs are not available"))
	})

	It("ignores error listing CRD on spoke cluster (warns for failed cleanup)", func() {
		mockSpokeClientCache.EXPECT().Get(gomock.Any()).Return(mockSpokeClient, nil)
		mockSpokeClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		mockSpokeClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("error"))
		res, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("successfully creates CRD on spoke cluster", func() {
		mockSpokeClientCache.EXPECT().Get(gomock.Any()).Return(mockSpokeClient, nil)
		notFoundError := k8serrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "CustomResourceDefinition"}, testCRDName)
		mockSpokeClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(notFoundError)
		mockSpokeClient.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		mockSpokeClient.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		res, err := hr.Reconcile(ctx, newHypershiftAgentServiceConfigRequest(hsc))
		Expect(err).To(BeNil())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("successfully updates existing CRD on spoke cluster", func() {
		schemes := GetKubeClientSchemes()
		spokeCRD := newAgentInstallCRD()
		fakeSpokeClient := fakeclient.NewClientBuilder().WithScheme(schemes).WithRuntimeObjects(spokeCRD).Build()

		c := crd.DeepCopy()
		c.Labels["new"] = "label"
		Expect(hr.Client.Update(ctx, c)).To(Succeed())
		Expect(hr.syncSpokeAgentInstallCRDs(ctx, fakeSpokeClient)).To(Succeed())

		crdKey := client.ObjectKeyFromObject(crd)
		spokeCrd := apiextensionsv1.CustomResourceDefinition{}
		Expect(fakeSpokeClient.Get(ctx, crdKey, &spokeCrd)).To(Succeed())
		Expect(spokeCrd.Labels["new"]).To(Equal("label"))
	})

	It("successfully removes redundant CRD from spoke cluster", func() {
		schemes := GetKubeClientSchemes()
		crd = newAgentInstallCRD()
		crd.Name = "redundant"
		fakeSpokeClient := fakeclient.NewClientBuilder().WithScheme(schemes).WithRuntimeObjects(crd).Build()
		crdKey := client.ObjectKeyFromObject(crd)
		spokeCrd := apiextensionsv1.CustomResourceDefinition{}
		Expect(fakeSpokeClient.Get(ctx, crdKey, &spokeCrd)).To(Succeed())
		Expect(hr.syncSpokeAgentInstallCRDs(ctx, fakeSpokeClient)).To(Succeed())
		Expect(fakeSpokeClient.Get(ctx, crdKey, &spokeCrd)).To(Not(Succeed()))
	})
})
