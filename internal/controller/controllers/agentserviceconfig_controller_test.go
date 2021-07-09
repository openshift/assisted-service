package controllers

import (
	"context"
	"encoding/json"
	"net/url"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testName                         = "agent"
	testAgentServiceConfigKind       = "testKind"
	testAgentServiceConfigAPIVersion = "testAPIVersion"
	testHost                         = "my.test"
	testConfigmapName                = "test-configmap"
	testMirrorRegConfigmapName       = "test-mirror-configmap"
)

func newTestReconciler(initObjs ...runtime.Object) *AgentServiceConfigReconciler {
	c := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjs...).Build()
	return &AgentServiceConfigReconciler{
		Client: c,
		Scheme: scheme.Scheme,
		Log:    logrus.New(),
		// TODO(djzager): If we need to verify emitted events
		// https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/pkg/controller/statefulset/stateful_pod_control_test.go#L474
		Recorder:  record.NewFakeRecorder(10),
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
		ingressCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultIngressCertCMName,
				Namespace: defaultIngressCertCMNamespace,
			},
		}
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

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc, ingressCM, route)
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
		log  = logrus.New()
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

			Expect(ascr.ensureAgentRoute(ctx, log, asc)).To(Succeed())

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

			Expect(ascr.ensureAgentRoute(ctx, log, asc)).To(Succeed())

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
		log        = logrus.New()
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

			err := ascr.ensureAgentLocalAuthSecret(ctx, log, asc)
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
			err := ascr.ensureAgentLocalAuthSecret(ctx, log, asc)
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

			err = ascr.ensureAgentLocalAuthSecret(ctx, log, asc)
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
		log   = logrus.New()
		route = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: testNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: testHost,
			},
		}
		assistedCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"foo": "bar",
			},
		}
	)

	Describe("AgentServiceConfig Unsupported ConfigMap annotation", func() {
		Context("without annotation on AgentServiceConfig", func() {
			It("should not modify assisted-service deployment", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Spec.Containers[0].EnvFrom).To(Equal([]corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: serviceName,
							},
						},
					},
				}))
			})
		})

		Context("with annotation on AgentServiceConfig", func() {
			It("should modify assisted-service deployment", func() {
				asc = newASCWithCMAnnotation()
				userCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						"foo": "bar",
					},
				}
				ascr = newTestReconciler(asc, route, userCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())
				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())

				targetConfigMap := append(
					[]corev1.EnvFromSource{
						{
							ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: serviceName,
								},
							},
						},
					},
					[]corev1.EnvFromSource{
						{
							ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: testConfigmapName,
								},
							},
						},
					}...,
				)

				Expect(found.Spec.Template.Spec.Containers[0].EnvFrom).To(Equal(targetConfigMap))
			})
		})
	})

	Describe("MirrorRegistry Configuration", func() {
		Context("with registries.conf", func() {
			It("should add volume and volumeMount", func() {
				asc = newASCWithMirrorRegistryConfig()
				mirrorCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testMirrorRegConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						mirrorRegistryRefRegistryConfKey: "foo",
					},
				}

				ascr = newTestReconciler(asc, route, mirrorCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Spec.Volumes).To(HaveLen(5))
				Expect(found.Spec.Template.Spec.Containers[0].VolumeMounts).Should(ContainElement(
					corev1.VolumeMount{
						Name:      mirrorRegistryConfigVolume,
						MountPath: common.MirrorRegistriesConfigDir,
					}),
				)
				Expect(found.Spec.Template.Spec.Containers[0].VolumeMounts).ShouldNot(ContainElement(
					corev1.VolumeMount{
						Name:      mirrorRegistryConfigVolume,
						MountPath: common.MirrorRegistriesCertificatePath,
						SubPath:   common.MirrorRegistriesCertificateFile,
					}),
				)
			})
		})

		Context("with registries.conf and ca-bundle", func() {
			It("should add volume and volumeMount", func() {
				asc = newASCWithMirrorRegistryConfig()
				mirrorCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testMirrorRegConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						mirrorRegistryRefRegistryConfKey: "foo",
						mirrorRegistryRefCertKey:         "foo",
					},
				}

				ascr = newTestReconciler(asc, route, mirrorCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Spec.Volumes).To(HaveLen(5))
				Expect(found.Spec.Template.Spec.Containers[0].VolumeMounts).Should(ContainElement(
					corev1.VolumeMount{
						Name:      mirrorRegistryConfigVolume,
						MountPath: common.MirrorRegistriesConfigDir,
					}),
				)
				Expect(found.Spec.Template.Spec.Containers[0].VolumeMounts).Should(ContainElement(
					corev1.VolumeMount{
						Name:      mirrorRegistryConfigVolume,
						MountPath: common.MirrorRegistriesCertificatePath,
						SubPath:   common.MirrorRegistriesCertificateFile,
					}),
				)
			})
		})

		Context("without specifying registries.conf", func() {
			It("should error", func() {
				asc = newASCWithMirrorRegistryConfig()
				mirrorCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testMirrorRegConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						mirrorRegistryRefCertKey: "foo",
					},
				}

				ascr = newTestReconciler(asc, route, mirrorCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).ToNot(Succeed())
			})
		})
	})

	Describe("ConfigMap hashing as annotations on assisted-service deployment", func() {
		Context("with assisted-service configmap", func() {
			It("should fail if assisted configMap not found", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Not(Succeed()))
			})

			It("should only add assisted config hash annotation", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Annotations).To(HaveLen(3))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(assistedConfigHashAnnotation, Not(Equal(""))))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(mirrorConfigHashAnnotation, Equal("")))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(userConfigHashAnnotation, Equal("")))
			})
		})

		Context("with mirror configmap", func() {
			It("should fail if mirror configMap specified but not found", func() {
				asc = newASCWithMirrorRegistryConfig()
				ascr = newTestReconciler(asc, route, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Not(Succeed()))
			})

			It("should add assisted and mirror config hash annotations", func() {
				asc = newASCWithMirrorRegistryConfig()
				mirrorCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testMirrorRegConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						mirrorRegistryRefRegistryConfKey: "foo",
					},
				}
				ascr = newTestReconciler(asc, route, mirrorCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Annotations).To(HaveLen(3))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(assistedConfigHashAnnotation, Not(Equal(""))))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(mirrorConfigHashAnnotation, Not(Equal(""))))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(userConfigHashAnnotation, Equal("")))
			})
		})

		Context("with unsupported configMap", func() {
			It("should fail if not found", func() {
				asc = newASCWithCMAnnotation()
				ascr = newTestReconciler(asc, route, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Not(Succeed()))
			})

			It("should add user config hash annotation by default", func() {
				asc = newASCWithCMAnnotation()
				userCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigmapName,
						Namespace: testNamespace,
					},
					Data: map[string]string{
						"foo": "bar",
					},
				}
				ascr = newTestReconciler(asc, route, userCM, assistedCM)
				Expect(ascr.ensureAssistedServiceDeployment(ctx, log, asc)).To(Succeed())

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Annotations).To(HaveLen(3))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(assistedConfigHashAnnotation, Not(Equal(""))))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(mirrorConfigHashAnnotation, Equal("")))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(userConfigHashAnnotation, Not(Equal(""))))
			})
		})
	})
})

var _ = Describe("getOpenshiftVersions", func() {
	var (
		asc         *aiv1beta1.AgentServiceConfig
		ascr        *AgentServiceConfigReconciler
		log         = logrus.New()
		expectedEnv string
	)

	Context("with OpenShift versions not specified", func() {
		It("should return the default OpenShift versions", func() {
			expectedEnv = `{"x.y": { "foo": "bar" }}`
			// Sensible defaults are handled via operator packaging (ie in CSV).
			// Here we just need to ensure the env var is taken when
			// OpenShift versions are not specified on the AgentServiceConfig.
			os.Setenv(OpenshiftVersionsEnvVar, expectedEnv)
			defer os.Unsetenv(OpenshiftVersionsEnvVar)

			asc = newASCDefault()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OpenShift versions specified", func() {
		It("should build OpenShift versions", func() {
			asc, expectedEnv = newASCWithOpenshiftVersions()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with multiple OpenShift versions specified", func() {
		It("should build OpenShift versions with multiple keys", func() {
			asc, expectedEnv = newASCWithMultipleOpenshiftVersions()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with duplicate OpenShift versions specified", func() {
		It("should take the last specified version", func() {
			asc, expectedEnv = newASCWithDuplicateOpenshiftVersions()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with invalid OpenShift versions specified", func() {
		It("should return the default OpenShift versions", func() {
			expectedEnv = `{"x.y": { "foo": "bar" }}`
			// Sensible defaults are handled via operator packaging (ie in CSV).
			// Here we just need to ensure the env var is taken when
			// OpenShift versions are not specified on the AgentServiceConfig.
			os.Setenv(OpenshiftVersionsEnvVar, expectedEnv)
			defer os.Unsetenv(OpenshiftVersionsEnvVar)

			asc = newASCWithInvalidOpenshiftVersion()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OpenShift version x.y.z specified", func() {
		It("should only specify x.y", func() {
			asc, expectedEnv = newASCWithLongOpenshiftVersion()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOpenshiftVersions(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
})

var _ = Describe("Default ConfigMap values", func() {

	var (
		configMap *corev1.ConfigMap
		log       = logrus.New()
	)

	BeforeEach(func() {
		asc := newASCDefault()
		r := newTestReconciler(asc)
		cm, mutateFn := r.newAssistedCM(log, asc, &url.URL{Scheme: "https", Host: "localhost"})
		Expect(mutateFn()).ShouldNot(HaveOccurred())
		configMap = cm
	})

	It("INSTALL_INVOKER", func() {
		Expect(configMap.Data["INSTALL_INVOKER"]).To(Equal("assisted-installer-operator"))
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

func newASCWithMirrorRegistryConfig() *aiv1beta1.AgentServiceConfig {
	asc := newASCDefault()
	asc.Spec.MirrorRegistryRef = &corev1.LocalObjectReference{
		Name: testMirrorRegConfigmapName,
	}
	return asc
}

func newASCWithOpenshiftVersions() (*aiv1beta1.AgentServiceConfig, string) {
	asc := newASCDefault()

	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
	}

	s := func(s string) *string { return &s }
	encodedVersions, _ := json.Marshal(map[string]models.OpenshiftVersion{
		"4.8": {
			DisplayName:  s("4.8"),
			RhcosVersion: s("48"),
			RhcosImage:   s("4.8.iso"),
			RhcosRootfs:  s("4.8.img"),
		},
	})

	return asc, string(encodedVersions)
}

func newASCWithMultipleOpenshiftVersions() (*aiv1beta1.AgentServiceConfig, string) {
	asc := newASCDefault()
	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.7",
			Version:          "47",
			Url:              "4.7.iso",
			RootFSUrl:        "4.7.img",
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
	}

	s := func(s string) *string { return &s }
	encodedVersions, _ := json.Marshal(map[string]models.OpenshiftVersion{
		"4.7": {
			DisplayName:  s("4.7"),
			RhcosVersion: s("47"),
			RhcosImage:   s("4.7.iso"),
			RhcosRootfs:  s("4.7.img"),
		},
		"4.8": {
			DisplayName:  s("4.8"),
			RhcosVersion: s("48"),
			RhcosImage:   s("4.8.iso"),
			RhcosRootfs:  s("4.8.img"),
		},
	})

	return asc, string(encodedVersions)
}

func newASCWithDuplicateOpenshiftVersions() (*aiv1beta1.AgentServiceConfig, string) {
	asc := newASCDefault()
	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.7",
			Version:          "47",
			Url:              "4.7.iso",
			RootFSUrl:        "4.7.img",
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "loser",
			Url:              "loser",
			RootFSUrl:        "loser",
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
	}

	s := func(s string) *string { return &s }
	encodedVersions, _ := json.Marshal(map[string]models.OpenshiftVersion{
		"4.7": {
			DisplayName:  s("4.7"),
			RhcosVersion: s("47"),
			RhcosImage:   s("4.7.iso"),
			RhcosRootfs:  s("4.7.img"),
		},
		"4.8": {
			DisplayName:  s("4.8"),
			RhcosVersion: s("48"),
			RhcosImage:   s("4.8.iso"),
			RhcosRootfs:  s("4.8.img"),
		},
	})

	return asc, string(encodedVersions)
}

func newASCWithInvalidOpenshiftVersion() *aiv1beta1.AgentServiceConfig {
	asc := newASCDefault()

	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "invalidVersion",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
	}

	return asc
}

func newASCWithLongOpenshiftVersion() (*aiv1beta1.AgentServiceConfig, string) {
	asc := newASCDefault()

	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.8.0",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
	}

	s := func(s string) *string { return &s }
	encodedVersions, _ := json.Marshal(map[string]models.OpenshiftVersion{
		"4.8": {
			DisplayName:  s("4.8"),
			RhcosVersion: s("48"),
			RhcosImage:   s("4.8.iso"),
			RhcosRootfs:  s("4.8.img"),
		},
	})

	return asc, string(encodedVersions)
}
