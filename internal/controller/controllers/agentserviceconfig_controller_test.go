package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testName                         = "agent"
	testAgentServiceConfigKind       = "testKind"
	testAgentServiceConfigAPIVersion = "testAPIVersion"
	testHost                         = "my.test"
	testConfigmapName                = "test-configmap"
)

func newTestReconciler(initObjs ...runtime.Object) *AgentServiceConfigReconciler {
	c := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjs...).Build()
	return &AgentServiceConfigReconciler{
		Client:    c,
		Scheme:    scheme.Scheme,
		Log:       ctrl.Log.WithName("testLog"),
		Namespace: testNamespace,
	}
}

var _ = Describe("ensureAgentLocalAuthSecret", func() {
	var (
		asc        *aiv1beta1.AgentServiceConfig
		ascr       *AgentServiceConfigReconciler
		ctx        = context.Background()
		privateKey = "test-private-key"
		publicKey  = "test-public-key"
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
	})

	Context("with an existing local auth secret", func() {
		It("should not modify existing keys", func() {
			localAuthSecret := &corev1.Secret{
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

var _ = Describe("ensureAssistedServiceDeployment", func() {
	var (
		asc   *aiv1beta1.AgentServiceConfig
		ascr  *AgentServiceConfigReconciler
		ctx   = context.Background()
		route = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: testNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: testHost,
			},
		}
	)

	Context("without annotation on AgentServiceConfig", func() {
		It("should not modify assisted-service deployment", func() {
			asc = newASCDefault()
			ascr = newTestReconciler(asc, route)
			Expect(ascr.ensureAssistedServiceDeployment(ctx, asc)).To(Succeed())

			found := &appsv1.Deployment{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: appName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(len(found.Spec.Template.Spec.Containers[0].EnvFrom)).To(Equal(0))
		})
	})

	Context("with annotation on AgentServiceConfig", func() {
		It("should modify assisted-service deployment", func() {
			asc = newASCWithCMAnnotation()
			ascr = newTestReconciler(asc, route)
			Expect(ascr.ensureAssistedServiceDeployment(ctx, asc)).To(Succeed())
			found := &appsv1.Deployment{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: appName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.Spec.Template.Spec.Containers[0].EnvFrom).To(Equal([]corev1.EnvFromSource{
				{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: testConfigmapName,
						},
					},
				},
			}))
		})
	})
})

var _ = Describe("ensureAgentService", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ctx  = context.Background()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
	})

	validatePorts := func(ports []corev1.ServicePort) {
		Expect(ports).NotTo(BeNil())

		for _, port := range ports {
			if port.Name == httpServiceName {
				Expect(port.Port).To(Equal(serviceHTTPPort))
				Expect(port.TargetPort).To(Equal(intstr.IntOrString{Type: intstr.Int, IntVal: serviceHTTPPort}))
				Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
			} else if port.Name == httpsServiceName {
				Expect(port.Port).To(Equal(serviceHTTPSPort))
				Expect(port.TargetPort).To(Equal(intstr.IntOrString{Type: intstr.Int, IntVal: serviceHTTPSPort}))
				Expect(port.Protocol).To(Equal(corev1.ProtocolTCP))
			}
		}
	}

	It("creates a new service", func() {
		Expect(ascr.ensureAgentService(ctx, asc)).To(Succeed())

		key := types.NamespacedName{Name: appName, Namespace: testNamespace}
		found := &corev1.Service{}
		Expect(ascr.Client.Get(ctx, key, found)).To(Succeed())

		validatePorts(found.Spec.Ports)
	})
})

func newASCDefault() *aiv1beta1.AgentServiceConfig {
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

func newASCWithCMAnnotation() *aiv1beta1.AgentServiceConfig {
	asc := newASCDefault()
	asc.ObjectMeta.Annotations = map[string]string{configmapAnnotation: testConfigmapName}
	return asc
}
