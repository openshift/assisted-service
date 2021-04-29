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

func newAgentServiceConfigRequest(asc *aiv1beta1.AgentServiceConfig) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: asc.ObjectMeta.Namespace,
		Name:      asc.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

var _ = Describe("agentserviceconfig_controller reconcile", func() {
	var (
		asc       *aiv1beta1.AgentServiceConfig
		ascr      *AgentServiceConfigReconciler
		ctx       = context.Background()
		route     *routev1.Route
		routeHost = "testHost"
	)

	BeforeEach(func() {
		asc = newASCWithCMAnnotation()
		ascr = newTestReconciler(asc)

		// The operator searches for the ingress cert config map.
		// If the config map isn't available the test runner will show
		// Message: "configmaps \"default-ingress-cert\" not found
		ingressCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultIngressCertCMName,
				Namespace: defaultIngressCertCMNamespace,
			},
		}
		Expect(ascr.Client.Create(ctx, ingressCM)).To(Succeed())

		// AgentServiceConfig created route is missing Host.
		// We create one here with a value set for Host so that
		// reconcile does not fail.
		route, _ = ascr.newAgentRoute(asc)
		route.Spec.Host = routeHost
		Expect(ascr.Client.Create(ctx, route)).To(Succeed())
	})

	It("reconcile should succeed", func() {
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
		Expect(err).To(Succeed())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})

var _ = Describe("ensureAgentRoute", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ctx  = context.Background()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
	})

	Context("with no existing route", func() {
		It("should create new route", func() {
			found := &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).ToNot(Succeed())

			Expect(ascr.ensureAgentRoute(ctx, asc)).To(Succeed())

			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing route", func() {
		It("should not change route Host", func() {
			routeHost := "route.example.com"
			route, _ := ascr.newAgentRoute(asc)
			route.Spec.Host = routeHost
			Expect(ascr.Client.Create(ctx, route)).To(Succeed())

			Expect(ascr.ensureAgentRoute(ctx, asc)).To(Succeed())

			found := &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.Spec.Host).To(Equal(routeHost))
		})
	})
})

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

	Context("validate mirror registries config mao", func() {
		It("valid config map", func() {
			mirrorMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-configmap",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"ca-bundle.crt":   "ca-bundle-value",
					"registries.conf": "registries-conf-value",
				},
			}
			Expect(ascr.Client.Create(ctx, mirrorMap)).To(Succeed())
			asc.Spec.MirrorRegistryRef = &corev1.LocalObjectReference{Name: "user-configmap"}
			err := ascr.validateMirrorRegistriesConfigMap(ctx, asc)
			Expect(err).To(BeNil())
		})
		It("invalid config map, keys", func() {
			mirrorMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-configmap",
					Namespace: testNamespace,
				},
				Data: map[string]string{
					"some_key":        "ca-bundle-value",
					"registries.conf": "registries-conf-value",
				},
			}
			Expect(ascr.Client.Create(ctx, mirrorMap)).To(Succeed())
			asc.Spec.MirrorRegistryRef = &corev1.LocalObjectReference{Name: "user-configmap"}
			err := ascr.validateMirrorRegistriesConfigMap(ctx, asc)
			Expect(err).To(HaveOccurred())
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
				Name:      serviceName,
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
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(len(found.Spec.Template.Spec.Containers[0].EnvFrom)).To(Equal(0))
		})
	})

	Context("with annotation on AgentServiceConfig", func() {
		It("should modify assisted-service deployment", func() {
			asc = newASCWithCMAnnotation()
			ascr = newTestReconciler(asc, route)
			Expect(ascr.ensureAssistedServiceDeployment(ctx, asc)).To(Succeed())
			found := &appsv1.Deployment{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
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
