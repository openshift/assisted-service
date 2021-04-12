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
		asc             *aiv1beta1.AgentServiceConfig
		ascr            *AgentServiceConfigReconciler
		ctx             = context.Background()
		mockCtrl        *gomock.Controller
		privateKey      = "test-private-key"
		publicKey       = "test-public-key"
		localAuthSecret *corev1.Secret
		route           *routev1.Route
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
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

		asc = newDefaultAgentServiceConfig()
		ascr = newTestReconciler(asc)
		Expect(ascr.Client.Create(ctx, localAuthSecret)).To(BeNil())

		// AgentServiceConfig created route is missing Host
		route, _ = ascr.newAgentRoute(asc)
		route.Spec.Host = "testHost"
		Expect(ascr.Client.Create(ctx, route)).To(BeNil())
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(ascr.Client.Delete(ctx, asc)).ShouldNot(HaveOccurred())
		Expect(ascr.Client.Delete(ctx, localAuthSecret)).ShouldNot(HaveOccurred())
		Expect(ascr.Client.Delete(ctx, route)).ShouldNot(HaveOccurred())
	})

	Describe("reconcile local auth secret", func() {

		Context("with an existing local auth secret", func() {
			It("should not modify existing keys", func() {
				result, err := ascr.Reconcile(newAgentServiceConfigRequest(asc))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				found := &corev1.Secret{}
				err = ascr.Client.Get(context.TODO(), types.NamespacedName{Name: agentLocalAuthSecretName, Namespace: testNamespace}, found)
				Expect(err).To(BeNil())

				Expect(found.StringData["ec-private-key.pem"]).To(Equal(privateKey))
				Expect(found.StringData["ec-public-key.pem"]).To(Equal(publicKey))
			})
		})

		Context("with no existing local auth secret", func() {
			It("should create new keys and not overwrite them in subsequent reconciles", func() {
				Expect(ascr.Client.Delete(ctx, localAuthSecret)).ShouldNot(HaveOccurred())

				result, err := ascr.Reconcile(newAgentServiceConfigRequest(asc))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				found := &corev1.Secret{}
				err = ascr.Client.Get(context.TODO(), types.NamespacedName{Name: agentLocalAuthSecretName,
					Namespace: testNamespace}, found)
				Expect(err).To(BeNil())

				foundPrivateKey := found.StringData["ec-private-key.pem"]
				foundPublicKey := found.StringData["ec-public-key.pem"]
				Expect(foundPrivateKey).ToNot(Equal(privateKey))
				Expect(foundPrivateKey).ToNot(BeNil())
				Expect(foundPublicKey).ToNot(Equal(publicKey))
				Expect(foundPublicKey).ToNot(BeNil())

				result, err = ascr.Reconcile(newAgentServiceConfigRequest(asc))
				Expect(err).To(BeNil())
				Expect(result).To(Equal(ctrl.Result{}))

				foundNextReconcile := &corev1.Secret{}
				err = ascr.Client.Get(context.TODO(), types.NamespacedName{Name: agentLocalAuthSecretName,
					Namespace: testNamespace}, foundNextReconcile)
				Expect(err).To(BeNil())

				Expect(foundNextReconcile.StringData["ec-private-key.pem"]).To(Equal(foundPrivateKey))
				Expect(foundNextReconcile.StringData["ec-public-key.pem"]).To(Equal(foundPublicKey))
			})
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
