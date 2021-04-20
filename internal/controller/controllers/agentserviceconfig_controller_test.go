package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testName                         = "agent"
	testAgentServiceConfigKind       = "testKind"
	testAgentServiceConfigAPIVersion = "testAPIVersion"
)

func newTestReconciler(initObjs ...runtime.Object) *AgentServiceConfigReconciler {
	c := fakeclient.NewFakeClientWithScheme(scheme.Scheme, initObjs...)
	return &AgentServiceConfigReconciler{
		Client:    c,
		Scheme:    scheme.Scheme,
		Log:       ctrl.Log.WithName("testLog"),
		Namespace: testNamespace,
	}
}

var _ = Describe("agentserviceconfig_controller reconcile", func() {
	var (
		asc      *aiv1beta1.AgentServiceConfig
		ascr     *AgentServiceConfigReconciler
		ctx      = context.Background()
		mockCtrl *gomock.Controller
		route    *routev1.Route
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		asc = newDefaultAgentServiceConfig()
		ascr = newTestReconciler(asc)

		// AgentServiceConfig created route is missing Host.
		// We create one here with a value set for Host so that
		// reconcile does not fail.
		route, _ = ascr.newAgentRoute(asc)
		route.Spec.Host = "testHost"
		Expect(ascr.Client.Create(ctx, route)).To(BeNil())
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(ascr.Client.Delete(ctx, asc)).To(Succeed())
		Expect(ascr.Client.Delete(ctx, route)).To(Succeed())
	})

	It("reconcile should succeed", func() {
		result, err := ascr.Reconcile(newAgentServiceConfigRequest(asc))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})

var _ = Describe("ensureAgentLocalAuthSecret", func() {
	var (
		asc             *aiv1beta1.AgentServiceConfig
		ascr            *AgentServiceConfigReconciler
		ctx             = context.Background()
		mockCtrl        *gomock.Controller
		privateKey      = "test-private-key"
		publicKey       = "test-public-key"
		localAuthSecret *corev1.Secret
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		asc = newDefaultAgentServiceConfig()
		ascr = newTestReconciler(asc)
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(ascr.Client.Delete(ctx, asc)).To(Succeed())
	})

	Context("with an existing local auth secret", func() {
		It("should not modify existing keys", func() {
			localAuthSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentLocalAuthSecretName,
					Namespace: testNamespace,
				},
				StringData: map[string]string{
					"ec-private-key.pem": privateKey,
					"ec-public-key.pem":  publicKey,
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(ascr.Client.Create(ctx, localAuthSecret)).To(Succeed())

			err := ascr.ensureAgentLocalAuthSecret(ctx, asc)
			Expect(err).To(BeNil())

			found := &corev1.Secret{}
			err = ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["ec-private-key.pem"]).To(Equal(privateKey))
			Expect(found.StringData["ec-public-key.pem"]).To(Equal(publicKey))

			Expect(ascr.Client.Delete(ctx, localAuthSecret)).To(Succeed())
		})
	})

	Context("with no existing local auth secret", func() {
		It("should create new keys and not overwrite them in subsequent reconciles", func() {
			err := ascr.ensureAgentLocalAuthSecret(ctx, asc)
			Expect(err).To(BeNil())

			found := &corev1.Secret{}
			err = ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName,
				Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			foundPrivateKey := found.StringData["ec-private-key.pem"]
			foundPublicKey := found.StringData["ec-public-key.pem"]
			Expect(foundPrivateKey).ToNot(Equal(privateKey))
			Expect(foundPrivateKey).ToNot(BeNil())
			Expect(foundPublicKey).ToNot(Equal(publicKey))
			Expect(foundPublicKey).ToNot(BeNil())

			err = ascr.ensureAgentLocalAuthSecret(ctx, asc)
			Expect(err).To(BeNil())

			foundAfterNextEnsure := &corev1.Secret{}
			err = ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName,
				Namespace: testNamespace}, foundAfterNextEnsure)
			Expect(err).To(BeNil())

			Expect(foundAfterNextEnsure.StringData["ec-private-key.pem"]).To(Equal(foundPrivateKey))
			Expect(foundAfterNextEnsure.StringData["ec-public-key.pem"]).To(Equal(foundPublicKey))
		})
	})
})

func newAgentServiceConfigRequest(asc *aiv1beta1.AgentServiceConfig) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: asc.ObjectMeta.Namespace,
		Name:      asc.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newDefaultAgentServiceConfig() *aiv1beta1.AgentServiceConfig {
	return &aiv1beta1.AgentServiceConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       testAgentServiceConfigKind,
			APIVersion: testAgentServiceConfigAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: testName,
		},
		Spec: aiv1beta1.AgentServiceConfigSpec{
			FileSystemStorage: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("20Gi"),
					},
				},
			},
			DatabaseStorage: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		},
	}

}
