package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	schemes := GetKubeClientSchemes()
	c := fakeclient.NewClientBuilder().WithScheme(schemes).WithRuntimeObjects(initObjs...).Build()
	return &AgentServiceConfigReconciler{
		Client: c,
		Scheme: schemes,
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

func AssertReconcileSuccess(ctx context.Context, log logrus.FieldLogger, client client.Client, instance *aiv1beta1.AgentServiceConfig, fn NewComponentFn) {
	obj, mutateFn, err := fn(ctx, log, instance)
	Expect(err).To(BeNil())
	_, err = controllerutil.CreateOrUpdate(ctx, client, obj, mutateFn)
	Expect(err).To(BeNil())
}

func AssertReconcileFailure(ctx context.Context, log logrus.FieldLogger, client client.Client, instance *aiv1beta1.AgentServiceConfig, fn NewComponentFn) {
	_, _, err := fn(ctx, log, instance)
	Expect(err).ToNot(BeNil())
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
		imageRoute = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imageServiceName,
				Namespace: testNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: fmt.Sprintf("%s.images", testHost),
			},
		}
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc, ingressCM, route, imageRoute)
	})

	It("reconcile should succeed", func() {
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
		Expect(err).To(Succeed())
		Expect(result).To(Equal(ctrl.Result{}))
	})
})

var _ = Describe("newImageServiceService", func() {
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

	Context("with no existing service", func() {
		It("should create new service", func() {
			found := &corev1.Service{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceService)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(found.ObjectMeta.Annotations[servingCertAnnotation]).To(Equal(imageServiceName))
		})
	})

	Context("with existing service", func() {
		It("should add annotation", func() {
			s, _, _ := ascr.newImageServiceService(ctx, log, asc)
			service := s.(*corev1.Service)
			Expect(ascr.Client.Create(ctx, service)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceService)

			found := &corev1.Service{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(found.ObjectMeta.Annotations[servingCertAnnotation]).To(Equal(imageServiceName))
		})
	})
})

var _ = Describe("newImageServiceRoute", func() {
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
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceRoute)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing route", func() {
		It("should not change route Host", func() {
			routeHost := "route.example.com"
			r, _, _ := ascr.newImageServiceRoute(ctx, log, asc)
			route := r.(*routev1.Route)
			route.Spec.Host = routeHost
			Expect(ascr.Client.Create(ctx, route)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceRoute)

			found := &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.Spec.Host).To(Equal(routeHost))
		})
	})
})

var _ = Describe("newImageServiceServiceAccount", func() {
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

	Context("with no existing serviceAccount", func() {
		It("should create new serviceAccount", func() {
			found := &corev1.ServiceAccount{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceServiceAccount)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})
})

var _ = Describe("newImageServiceConfigMap", func() {
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

	Context("with no existing configmap", func() {
		It("should create new configmap", func() {
			found := &corev1.ConfigMap{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceConfigMap)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing configmap", func() {
		It("should add annotation", func() {
			cm, _, _ := ascr.newImageServiceConfigMap(ctx, log, asc)
			configMap := cm.(*corev1.ConfigMap)
			Expect(ascr.Client.Create(ctx, configMap)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceConfigMap)

			found := &corev1.ConfigMap{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(found.ObjectMeta.Annotations[injectCABundleAnnotation]).To(Equal("true"))
		})
	})
})

var _ = Describe("newImageServiceDeployment", func() {
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

	Context("with no existing deployment", func() {
		It("should create new deployment", func() {
			found := &appsv1.Deployment{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newImageServiceDeployment)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
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

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAgentRoute)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing route", func() {
		It("should not change route Host", func() {
			routeHost := "route.example.com"
			r, _, _ := ascr.newAgentRoute(ctx, log, asc)
			route := r.(*routev1.Route)
			route.Spec.Host = routeHost
			Expect(ascr.Client.Create(ctx, route)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAgentRoute)

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

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAgentLocalAuthSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["ec-private-key.pem"]).To(Equal(privateKey))
			Expect(found.StringData["ec-public-key.pem"]).To(Equal(publicKey))
		})
	})

	Context("with no existing local auth secret", func() {
		It("should create new keys and not overwrite them in subsequent reconciles", func() {
			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAgentLocalAuthSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName,
				Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			foundPrivateKey := found.StringData["ec-private-key.pem"]
			foundPublicKey := found.StringData["ec-public-key.pem"]
			Expect(foundPrivateKey).ToNot(Equal(privateKey))
			Expect(foundPrivateKey).ToNot(BeNil())
			Expect(foundPublicKey).ToNot(Equal(publicKey))
			Expect(foundPublicKey).ToNot(BeNil())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAgentLocalAuthSecret)
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

var _ = Describe("ensurePostgresSecret", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ctx  = context.Background()
		log  = logrus.New()
		pass = "password"
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
	})

	Context("with an existing postgres secret", func() {
		It("should not modify password", func() {
			dbSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              databaseName,
					Namespace:         testNamespace,
					CreationTimestamp: metav1.Now(),
				},
				StringData: map[string]string{
					"db.host":     "localhost",
					"db.user":     "admin",
					"db.password": pass,
					"db.name":     "installer",
					"db.port":     databasePort.String(),
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(ascr.Client.Create(ctx, dbSecret)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newPostgresSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: databaseName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["db.password"]).To(Equal(pass))
		})
	})

	Context("with no existing postgres secret", func() {
		It("should create new secret with password", func() {
			AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newPostgresSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: databaseName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["db.host"]).To(Equal("localhost"))
			Expect(found.StringData["db.user"]).To(Equal("admin"))
			Expect(found.StringData["db.name"]).To(Equal("installer"))
			Expect(found.StringData["db.port"]).To(Equal(databasePort.String()))
			// password will be random
			foundPass := found.StringData["db.password"]
			Expect(foundPass).ToNot(BeNil())
			Expect(foundPass).To(HaveLen(databasePasswordLength))
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

				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)
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
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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
				AssertReconcileFailure(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)
			})
		})
	})

	Describe("ConfigMap hashing as annotations on assisted-service deployment", func() {
		Context("with assisted-service configmap", func() {
			It("should fail if assisted configMap not found", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route)
				AssertReconcileFailure(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)
			})

			It("should only add assisted config hash annotation", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route, assistedCM)
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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
				AssertReconcileFailure(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)
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
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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
				AssertReconcileFailure(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)
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
				AssertReconcileSuccess(ctx, log, ascr.Client, asc, ascr.newAssistedServiceDeployment)

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

var _ = Describe("getMustGatherImages", func() {
	const MUST_GATHER_IMAGES_ENVVAR string = "MUST_GATHER_IMAGES"
	var defaultSpecMustGatherImages = []aiv1beta1.MustGatherImage{
		{
			OpenshiftVersion: "4.8",
			Name:             "cnv",
			Url:              "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
		},
	}
	var defaultEnvMustGatherImages = versions.MustGatherVersions{
		"4.8": versions.MustGatherVersion{
			"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
			"ocs": "registry.redhat.io/ocs4/ocs-must-gather-rhel8",
			"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
		},
	}
	var outSpecMustGatherImages = versions.MustGatherVersions{
		"4.8": versions.MustGatherVersion{
			"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
		},
	}
	var spec2string = func() string {
		bytes, err := json.Marshal(outSpecMustGatherImages)
		Expect(err).NotTo(HaveOccurred())
		return string(bytes)
	}
	var env2string = func() string {
		bytes, err := json.Marshal(defaultEnvMustGatherImages)
		Expect(err).NotTo(HaveOccurred())
		return string(bytes)
	}

	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		log  *logrus.Logger
	)

	tests := []struct {
		name     string
		spec     []aiv1beta1.MustGatherImage
		env      versions.MustGatherVersions
		expected string
	}{
		{
			name:     "spec is empty - return the env configuration",
			spec:     nil,
			env:      defaultEnvMustGatherImages,
			expected: env2string(),
		},
		{
			name:     "images in spec - return the spec configuration",
			spec:     defaultSpecMustGatherImages,
			env:      nil,
			expected: spec2string(),
		},
		{
			name:     "both sources - return the spec configuration",
			spec:     defaultSpecMustGatherImages,
			env:      defaultEnvMustGatherImages,
			expected: spec2string(),
		},
		{
			name:     "both empty - return empty string",
			spec:     nil,
			env:      nil,
			expected: "",
		},
	}

	BeforeEach(func() {
		asc = newASCDefault()
		log = logrus.New()
	})

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			//setup must gather image environment variable, if applicable
			defer os.Unsetenv(MUST_GATHER_IMAGES_ENVVAR)
			if t.env != nil {
				os.Setenv(MUST_GATHER_IMAGES_ENVVAR, env2string())
			}
			//setup must gather image SPEC configuration
			asc.Spec.MustGatherImages = t.spec
			ascr = newTestReconciler(asc)
			//verify the result
			if t.expected != "" {
				Expect(ascr.getMustGatherImages(log, asc)).To(MatchJSON(t.expected))
			} else {
				Expect(ascr.getMustGatherImages(log, asc)).To(Equal(""))
			}
		})
	}

})

var _ = Describe("getOSImages", func() {
	const OS_IMAGES_ENVVAR string = "OS_IMAGES"
	var defaultSpecOsImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.9",
			Url:              "rhcos_4.9",
			RootFSUrl:        "rhcos_rootfs_4.9",
			Version:          "version-49.123-0",
			CPUArchitecture:  "x86_64",
		},
		{
			OpenshiftVersion: "4.9",
			Url:              "rhcos_4.9",
			RootFSUrl:        "rhcos_rootfs_4.9",
			Version:          "version-49.123-0",
			CPUArchitecture:  "arm",
		},
	}
	var defaultEnvOsImages = models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String("x86_64"),
			OpenshiftVersion: swag.String("4.8"),
			URL:              swag.String("rhcos_4.8"),
			RootfsURL:        swag.String("rhcos_rootfs_4.8"),
			Version:          swag.String("version-48.123-0"),
		},
	}
	var outSpecOsImages = models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String("x86_64"),
			OpenshiftVersion: swag.String("4.9"),
			RootfsURL:        swag.String("rhcos_rootfs_4.9"),
			URL:              swag.String("rhcos_4.9"),
			Version:          swag.String("version-49.123-0"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String("arm"),
			OpenshiftVersion: swag.String("4.9"),
			RootfsURL:        swag.String("rhcos_rootfs_4.9"),
			URL:              swag.String("rhcos_4.9"),
			Version:          swag.String("version-49.123-0"),
		},
	}
	var spec2string = func() string {
		bytes, err := json.Marshal(outSpecOsImages)
		Expect(err).NotTo(HaveOccurred())
		return string(bytes)
	}
	var env2string = func() string {
		bytes, err := json.Marshal(defaultSpecOsImages)
		Expect(err).NotTo(HaveOccurred())
		return string(bytes)
	}

	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		log  *logrus.Logger
	)

	tests := []struct {
		name     string
		spec     []aiv1beta1.OSImage
		env      models.OsImages
		expected string
	}{
		{
			name:     "spec is empty - return the env configuration",
			spec:     nil,
			env:      defaultEnvOsImages,
			expected: env2string(),
		},
		{
			name:     "images in spec - return the spec configuration",
			spec:     defaultSpecOsImages,
			env:      nil,
			expected: spec2string(),
		},
		{
			name:     "both sources - return the spec configuration",
			spec:     defaultSpecOsImages,
			env:      defaultEnvOsImages,
			expected: spec2string(),
		},
		{
			name:     "both empty - return empty string",
			spec:     nil,
			env:      nil,
			expected: "",
		},
	}

	BeforeEach(func() {
		asc = newASCDefault()
		log = logrus.New()
	})

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			// setup OS_IMAGES environment variable, if applicable
			defer os.Unsetenv(OS_IMAGES_ENVVAR)
			if t.env != nil {
				os.Setenv(OS_IMAGES_ENVVAR, env2string())
			}
			// setup OSImages spec configuration
			asc.Spec.OSImages = t.spec
			ascr = newTestReconciler(asc)
			// verify the result
			if t.expected != "" {
				Expect(ascr.getOSImages(log, asc)).To(MatchJSON(t.expected))
			} else {
				Expect(ascr.getOSImages(log, asc)).To(Equal(""))
			}
		})
	}
})

var _ = Describe("getOSImages", func() {
	var (
		asc         *aiv1beta1.AgentServiceConfig
		ascr        *AgentServiceConfigReconciler
		log         = logrus.New()
		expectedEnv string
	)

	Context("with OS images not specified", func() {
		It("should return the default OS images", func() {
			expectedEnv = `{"x.y": { "foo": "bar" }}`
			// Sensible defaults are handled via operator packaging (ie in CSV).
			// Here we just need to ensure the env var is taken when
			// OS images are not specified on the AgentServiceConfig.
			os.Setenv(OsImagesEnvVar, expectedEnv)
			defer os.Unsetenv(OsImagesEnvVar)

			asc = newASCDefault()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOSImages(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OS images specified", func() {
		It("should build OS images", func() {
			asc, expectedEnv = newASCWithOSImages()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOSImages(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with multiple OS images specified", func() {
		It("should build OS images with multiple keys", func() {
			asc, expectedEnv = newASCWithMultipleOpenshiftVersions()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOSImages(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with duplicate OS images specified", func() {
		It("should take the last specified version", func() {
			asc, expectedEnv = newASCWithDuplicateOpenshiftVersions()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOSImages(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OS images x.y.z specified", func() {
		It("should only specify x.y", func() {
			asc, expectedEnv = newASCWithLongOpenshiftVersion()
			ascr = newTestReconciler(asc)
			Expect(ascr.getOSImages(log, asc)).To(MatchJSON(expectedEnv))
		})
	})
})

var _ = Describe("Default ConfigMap values", func() {

	var (
		configMap *corev1.ConfigMap
		log       = logrus.New()
		ctx       = context.Background()
		route     = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: testNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: testHost,
			},
		}
		imageRoute = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imageServiceName,
				Namespace: testNamespace,
			},
			Spec: routev1.RouteSpec{
				Host: fmt.Sprintf("%s.images", testHost),
			},
		}
	)

	BeforeEach(func() {
		asc := newASCDefault()
		r := newTestReconciler(asc, route, imageRoute)
		cm, mutateFn, err := r.newAssistedCM(ctx, log, asc)
		Expect(err).ToNot(HaveOccurred())
		Expect(mutateFn()).ShouldNot(HaveOccurred())
		configMap = cm.(*corev1.ConfigMap)
	})

	It("INSTALL_INVOKER", func() {
		Expect(configMap.Data["INSTALL_INVOKER"]).To(Equal("assisted-installer-operator"))
	})

	It("sets the base URLs", func() {
		Expect(configMap.Data["SERVICE_BASE_URL"]).To(Equal(fmt.Sprintf("https://%s", testHost)))
		Expect(configMap.Data["IMAGE_SERVICE_BASE_URL"]).To(Equal(fmt.Sprintf("https://%s.images", testHost)))
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

func newASCWithOSImages() (*aiv1beta1.AgentServiceConfig, string) {
	asc := newASCDefault()

	asc.Spec.OSImages = []aiv1beta1.OSImage{
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
			RootFSUrl:        "4.8.img",
		},
		{
			OpenshiftVersion: "4.9",
			Version:          "49",
			Url:              "4.9.iso",
			RootFSUrl:        "4.9.img",
		},
	}

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
			RootfsURL:        swag.String("4.8.img"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.9"),
			Version:          swag.String("49"),
			URL:              swag.String("4.9.iso"),
			RootfsURL:        swag.String("4.9.img"),
		},
	})

	return asc, string(encoded)
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

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.7"),
			Version:          swag.String("47"),
			URL:              swag.String("4.7.iso"),
			RootfsURL:        swag.String("4.7.img"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
			RootfsURL:        swag.String("4.8.img"),
		},
	})

	return asc, string(encoded)
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

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.7"),
			Version:          swag.String("47"),
			URL:              swag.String("4.7.iso"),
			RootfsURL:        swag.String("4.7.img"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("loser"),
			URL:              swag.String("loser"),
			RootfsURL:        swag.String("loser"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
			RootfsURL:        swag.String("4.8.img"),
		},
	})

	return asc, string(encoded)
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

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8.0"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
			RootfsURL:        swag.String("4.8.img"),
		},
	})

	return asc, string(encoded)
}
