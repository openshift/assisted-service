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
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func newTestReconciler(initObjs ...client.Object) *AgentServiceConfigReconciler {
	schemes := GetKubeClientSchemes()

	rtoList := []runtime.Object{}
	for i := range initObjs {
		rtoList = append(rtoList, initObjs[i])
	}

	c := fakeclient.NewClientBuilder().WithScheme(schemes).WithStatusSubresource(initObjs...).
		WithRuntimeObjects(rtoList...).Build()
	return &AgentServiceConfigReconciler{
		AgentServiceConfigReconcileContext: AgentServiceConfigReconcileContext{
			Scheme: schemes,
			Log:    logrus.New(),
			// TODO(djzager): If we need to verify emitted events
			// https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/pkg/controller/statefulset/stateful_pod_control_test.go#L474
			Recorder: record.NewFakeRecorder(10),
		},
		Client:    c,
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

func AssertReconcileSuccess(ctx context.Context, log logrus.FieldLogger, ascc ASC, fn NewComponentFn) {
	obj, mutateFn, err := fn(ctx, log, ascc)
	Expect(err).To(BeNil())
	_, err = controllerutil.CreateOrUpdate(ctx, ascc.Client, obj, mutateFn)
	Expect(err).To(BeNil())
}

func AssertReconcileFailure(ctx context.Context, log logrus.FieldLogger, ascc ASC, fn NewComponentFn) {
	_, _, err := fn(ctx, log, ascc)
	Expect(err).ToNot(BeNil())
}

var _ = Describe("agentserviceconfig_controller reconcile", func() {
	var (
		asc                                                        *aiv1beta1.AgentServiceConfig
		ascr                                                       *AgentServiceConfigReconciler
		agentinstalladmissionDeployment, assistedServiceDeployment *appsv1.Deployment

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
	})

	Context("with successful setup", func() {
		BeforeEach(func() {
			agentinstalladmissionDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agentinstalladmission",
					Namespace: testNamespace,
				},
			}
			var replicas int32 = 1
			imageServiceStatefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assisted-image-service",
					Namespace: testNamespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "image-service-data",
							},
							Spec: *asc.Spec.ImageStorage,
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:        1,
					ReadyReplicas:   1,
					CurrentReplicas: 1,
					UpdatedReplicas: 1,
				},
			}
			assistedServiceDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assisted-service",
					Namespace: testNamespace,
				},
			}
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("adds the finalizer", func() {
			instance := &aiv1beta1.AgentServiceConfig{}
			Expect(ascr.Get(ctx, types.NamespacedName{Name: "agent"}, instance)).To(Succeed())
			Expect(funk.ContainsString(instance.GetFinalizers(), agentServiceConfigFinalizerName)).To(BeTrue())
		})

		It("cleans up when agentserviceconfig is deleted", func() {
			instance := &aiv1beta1.AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: testName,
				},
			}
			Expect(ascr.Delete(ctx, instance)).To(Succeed())

			result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())
			Expect(result).To(Equal(ctrl.Result{}))

			By("ensure pvcs are deleted")
			pvcList := &corev1.PersistentVolumeClaimList{}
			Expect(ascr.List(ctx, pvcList, client.MatchingLabels{"app": imageServiceName})).To(Succeed())
			Expect(len(pvcList.Items)).To(Equal(0))

			By("ensure statefulset finalizer is removed")
			ss := &appsv1.StatefulSet{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())
			Expect(funk.ContainsString(ss.GetFinalizers(), imageServiceStatefulSetFinalizerName)).To(BeFalse())
		})
	})

	It("should set `DeploymentsHealthy` condition to `False` on AgentServiceConfig when a deployment is not Available", func() {
		agentinstalladmissionDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agentinstalladmission",
				Namespace: testNamespace,
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		var replicas int32 = 1
		imageServiceStatefulSet := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-image-service",
				Namespace: testNamespace,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "image-service-data",
						},
						Spec: *asc.Spec.ImageStorage,
					},
				},
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:        1,
				ReadyReplicas:   1,
				CurrentReplicas: 1,
				UpdatedReplicas: 1,
			},
		}
		assistedServiceDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-service",
				Namespace: testNamespace,
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}
		ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))

		Expect(err).NotTo(Succeed())
		Expect(result).NotTo(Equal(ctrl.Result{}))

		instance := &aiv1beta1.AgentServiceConfig{}
		err = ascr.Get(ctx, types.NamespacedName{Name: "agent"}, instance)

		Expect(err).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Reason).To(Equal(aiv1beta1.ReasonDeploymentFailure))
	})

	It("should set `DeploymentsHealthy` condition to `False` on AgentServiceConfig when the stateful set replicas are incorrect", func() {
		agentinstalladmissionDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agentinstalladmission",
				Namespace: testNamespace,
			},
		}
		var replicas int32 = 1
		imageServiceStatefulSet := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-image-service",
				Namespace: testNamespace,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "image-service-data",
						},
						Spec: *asc.Spec.ImageStorage,
					},
				},
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:        1,
				ReadyReplicas:   0,
				CurrentReplicas: 1,
				UpdatedReplicas: 1,
			},
		}
		assistedServiceDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-service",
				Namespace: testNamespace,
			},
		}
		ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))

		Expect(err).NotTo(Succeed())
		Expect(result).NotTo(Equal(ctrl.Result{}))

		instance := &aiv1beta1.AgentServiceConfig{}
		err = ascr.Get(ctx, types.NamespacedName{Name: "agent"}, instance)
		Expect(err).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Reason).To(Equal(aiv1beta1.ReasonDeploymentFailure))
	})

	It("should set `DeploymentsHealthy` condition to `True` on AgentServiceConfig when all the deployments are Available", func() {
		agentinstalladmissionDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agentinstalladmission",
				Namespace: testNamespace,
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		var replicas int32 = 1
		imageServiceStatefulSet := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-image-service",
				Namespace: testNamespace,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "image-service-data",
						},
						Spec: *asc.Spec.ImageStorage,
					},
				},
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:        1,
				ReadyReplicas:   1,
				CurrentReplicas: 1,
				UpdatedReplicas: 1,
			},
		}
		assistedServiceDeployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "assisted-service",
				Namespace: testNamespace,
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentAvailable,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))

		Expect(err).To(Succeed())
		Expect(result).To(Equal(ctrl.Result{}))

		instance := &aiv1beta1.AgentServiceConfig{}
		err = ascr.Get(ctx, types.NamespacedName{Name: "agent"}, instance)

		Expect(err).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Status).To(Equal(corev1.ConditionTrue))
		Expect(conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionDeploymentsHealthy).Reason).To(Equal(aiv1beta1.ReasonDeploymentSucceeded))
	})

	It("should set `ReconcileCompleted` condition to `False` on AgentServiceConfig when reconcileComponent fails", func() {
		ascr = newTestReconciler(asc)
		result, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))

		Expect(err).NotTo(Succeed())
		Expect(result).NotTo(Equal(ctrl.Result{}))

		instance := &aiv1beta1.AgentServiceConfig{}
		err = ascr.Get(ctx, types.NamespacedName{Name: "agent"}, instance)
		Expect(err).To(BeNil())
		condition := conditionsv1.FindStatusCondition(instance.Status.Conditions, aiv1beta1.ConditionReconcileCompleted)
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(corev1.ConditionFalse))
	})

	Context("IPXE routes", func() {
		var (
			imageServiceStatefulSet *appsv1.StatefulSet
		)
		BeforeEach(func() {
			agentinstalladmissionDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agentinstalladmission",
					Namespace: testNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:   appsv1.DeploymentAvailable,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			var replicas int32 = 1
			imageServiceStatefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assisted-image-service",
					Namespace: testNamespace,
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &replicas,
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "image-service-data",
							},
							Spec: *asc.Spec.ImageStorage,
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:        1,
					ReadyReplicas:   1,
					CurrentReplicas: 1,
					UpdatedReplicas: 1,
				},
			}
			assistedServiceDeployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assisted-service",
					Namespace: testNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:   appsv1.DeploymentAvailable,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
		})

		It("should not create plain http route by default", func() {
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found := &routev1.Route{}
			bootArtifactsRouteName := fmt.Sprintf("%s-ipxe", imageServiceName)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).NotTo(Succeed())

		})

		It("should create plain http route", func() {
			asc.Spec.IPXEHTTPRoute = aiv1beta1.IPXEHTTPRouteEnabled
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found := &routev1.Route{}
			bootArtifactsRouteName := fmt.Sprintf("%s-ipxe", imageServiceName)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.Spec.Host).To(Equal(imageRoute.Spec.Host))
			Expect(found.Spec.Path).To(Equal("/"))
			Expect(found.Spec.Port.TargetPort).To(Equal(intstr.FromString(fmt.Sprintf("%s-http", imageServiceName))))
		})

		It("should not create plain http route if ExposeIPXEHTTPRoute is explicitly disabled", func() {
			asc.Spec.IPXEHTTPRoute = aiv1beta1.IPXEHTTPRouteDisabled
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found := &routev1.Route{}
			bootArtifactsRouteName := fmt.Sprintf("%s-ipxe", imageServiceName)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).NotTo(Succeed())
		})

		It("should not create plain http route if ExposeIPXEHTTPRoute is not unknown", func() {
			asc.Spec.IPXEHTTPRoute = "foobar"
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found := &routev1.Route{}
			bootArtifactsRouteName := fmt.Sprintf("%s-ipxe", imageServiceName)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).NotTo(Succeed())
		})

		It("should remove http route after IPXEHTTPRouteEnabled changed to disabled", func() {
			asc.Spec.IPXEHTTPRoute = aiv1beta1.IPXEHTTPRouteEnabled
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err := ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found := &routev1.Route{}
			bootArtifactsRouteName := fmt.Sprintf("%s-ipxe", imageServiceName)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).To(Succeed())

			asc.Spec.IPXEHTTPRoute = aiv1beta1.IPXEHTTPRouteDisabled
			ascr = newTestReconciler(asc, ingressCM, route, imageRoute, agentinstalladmissionDeployment, imageServiceStatefulSet, assistedServiceDeployment)
			_, err = ascr.Reconcile(ctx, newAgentServiceConfigRequest(asc))
			Expect(err).To(Succeed())

			found = &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: bootArtifactsRouteName, Namespace: testNamespace}, found)).NotTo(Succeed())
		})
	})

})

var _ = Describe("newImageServiceService", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
	})

	Context("with no existing service", func() {
		It("should create new service", func() {
			found := &corev1.Service{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceService)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(found.ObjectMeta.Annotations[servingCertAnnotation]).To(Equal(imageServiceName))
		})
	})

	Context("with existing service", func() {
		It("should add annotation", func() {
			s, _, _ := newImageServiceService(ctx, log, ascc)
			service := s.(*corev1.Service)
			Expect(ascr.Client.Create(ctx, service)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceService)

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
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
	})

	Context("with no existing route", func() {
		It("should create new route", func() {
			found := &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceRoute)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing route", func() {
		It("should not change route Host", func() {
			routeHost := "route.example.com"
			r, _, _ := newImageServiceRoute(ctx, log, ascc)
			route := r.(*routev1.Route)
			route.Spec.Host = routeHost
			Expect(ascr.Client.Create(ctx, route)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceRoute)

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
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
	})

	Context("with no existing serviceAccount", func() {
		It("should create new serviceAccount", func() {
			found := &corev1.ServiceAccount{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceServiceAccount)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})
})

var _ = Describe("newImageServiceConfigMap", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
	})

	Context("with no existing configmap", func() {
		It("should create new configmap", func() {
			found := &corev1.ConfigMap{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceConfigMap)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing configmap", func() {
		It("should add annotation", func() {
			cm, _, _ := newImageServiceConfigMap(ctx, log, ascc)
			configMap := cm.(*corev1.ConfigMap)
			Expect(ascr.Client.Create(ctx, configMap)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newImageServiceConfigMap)

			found := &corev1.ConfigMap{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
			Expect(found.ObjectMeta.Annotations).NotTo(BeNil())
			Expect(found.ObjectMeta.Annotations[injectCABundleAnnotation]).To(Equal("true"))
		})
	})
})

var _ = Describe("reconcileImageServiceStatefulSet", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()

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
		ascr = newTestReconciler(asc, imageRoute)
		ascc = initASC(ascr, asc)
	})

	reconcileUntilDone := func(runs int) {
		for i := 0; i < runs; i++ {
			Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		}
	}

	It("is doesn't change the stateful set when agent service config is unchanged", func() {
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		initial := &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, initial)).To(Succeed())

		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		next := &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, next)).To(Succeed())

		Expect(equality.Semantic.DeepEqual(initial, next)).To(BeTrue())
	})

	It("deletes existing image service deployments", func() {
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: imageServiceName, Namespace: testNamespace},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"some": "label"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Name: imageServiceName, Labels: map[string]string{"some": "label"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: imageServiceName, Image: "example.com/thing/image:latest"},
						},
					},
				},
			},
		}
		Expect(ascr.Client.Create(ctx, deploy)).To(Succeed())

		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())

		found := &appsv1.Deployment{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).ToNot(Succeed())
	})

	It("reconciles other fields", func() {
		// create initial stateful set
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())

		// change replicas to some incorrect value
		var replicas int32 = 5
		ss := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      imageServiceName,
				Namespace: testNamespace,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
			},
		}
		Expect(ascr.Client.Patch(ctx, ss, client.MergeFrom(ss))).To(Succeed())

		// reconcile and check that replicas were set back to 1
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())
		Expect(*ss.Spec.Replicas).To(Equal(int32(1)))
	})

	It("removes empty dir volume and adds volume claim template when image storage is added", func() {
		asc.Spec.ImageStorage = nil
		ascc = initASC(ascr, asc)
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())

		ss := &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())

		// volume claim templates missing and volume is present to start
		Expect(ss.Spec.VolumeClaimTemplates).To(BeNil())
		var foundVol bool
		for _, v := range ss.Spec.Template.Spec.Volumes {
			if v.Name == "image-service-data" {
				foundVol = true
				Expect(v.VolumeSource.EmptyDir).NotTo(BeNil())
			}
		}
		Expect(foundVol).To(BeTrue())

		// add image storage and reconcile
		asc = newASCDefault()
		ascc = initASC(ascr, asc)
		// it takes several reconcile calls to handle this situation
		reconcileUntilDone(5)

		// ensure volume claim templates were added and volume was removed
		ss = &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())

		volumeTemplates := ss.Spec.VolumeClaimTemplates
		Expect(len(volumeTemplates)).To(Equal(1))
		Expect(volumeTemplates[0].ObjectMeta.Name).To(Equal("image-service-data"))

		for _, v := range ss.Spec.Template.Spec.Volumes {
			Expect(v.Name).ToNot(Equal("image-service-data"))
		}
	})

	It("removes volume claim templates and adds empty dir volume when image storage is removed", func() {
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		ss := &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())

		// volume template should exist to start
		volumeTemplates := ss.Spec.VolumeClaimTemplates
		Expect(len(volumeTemplates)).To(Equal(1))
		Expect(volumeTemplates[0].ObjectMeta.Name).To(Equal("image-service-data"))

		for _, v := range ss.Spec.Template.Spec.Volumes {
			Expect(v.Name).ToNot(Equal("image-service-data"))
		}

		// remove image storage and reconcile
		asc.Spec.ImageStorage = nil
		// it takes several reconcile calls to handle this situation
		reconcileUntilDone(5)

		// ensure there are no volume claim templates and volume was added
		ss = &appsv1.StatefulSet{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, ss)).To(Succeed())

		Expect(ss.Spec.VolumeClaimTemplates).To(BeNil())
		var foundVol bool
		for _, v := range ss.Spec.Template.Spec.Volumes {
			if v.Name == "image-service-data" {
				foundVol = true
				Expect(v.VolumeSource.EmptyDir).NotTo(BeNil())
			}
		}
		Expect(foundVol).To(BeTrue())
	})

	It("removes pvcs for pod volumes when volumes have been updated", func() {
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "image-service-volume-0",
				Namespace: testNamespace,
				Labels:    map[string]string{"app": imageServiceName},
			},
		}
		Expect(ascr.Client.Create(ctx, pvc)).To(Succeed())

		asc.Spec.ImageStorage.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("50Gi")
		// it takes several reconcile calls to handle this situation
		reconcileUntilDone(5)

		key := client.ObjectKeyFromObject(pvc)
		Expect(ascr.Client.Get(ctx, key, pvc)).ToNot(Succeed())
	})

	It("should set the proxy env vars", func() {
		os.Setenv("HTTP_PROXY", "http://proxy.example.com")
		os.Setenv("HTTPS_PROXY", "http://https-proxy.example.com")
		os.Setenv("NO_PROXY", "http://no-proxy.example.com")
		defer func() {
			os.Unsetenv("HTTP_PROXY")
			os.Unsetenv("HTTPS_PROXY")
			os.Unsetenv("NO_PROXY")
		}()

		found := &appsv1.StatefulSet{}
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		var httpProxy, httpsProxy, noProxy string
		for _, envVar := range found.Spec.Template.Spec.Containers[0].Env {
			switch envVar.Name {
			case "HTTP_PROXY":
				httpProxy = envVar.Value
			case "HTTPS_PROXY":
				httpsProxy = envVar.Value
			case "NO_PROXY":
				noProxy = envVar.Value
			}
		}
		Expect(httpProxy).To(Equal("http://proxy.example.com"))
		Expect(httpsProxy).To(Equal("http://https-proxy.example.com"))
		Expect(noProxy).To(Equal("http://no-proxy.example.com"))
	})

	It("should expose two ports for ipxe", func() {
		found := &appsv1.StatefulSet{}
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		Expect(found.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(found.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(2))
		Expect(found.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(imageHandlerPort.IntValue())))
		Expect(found.Spec.Template.Spec.Containers[0].Ports[1].ContainerPort).To(Equal(int32(imageHandlerHTTPPort.IntValue())))
	})

	It("should set image service scheme and host env vars", func() {
		found := &appsv1.StatefulSet{}
		Expect(reconcileImageServiceStatefulSet(ctx, log, ascc)).To(Succeed())
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: imageServiceName, Namespace: testNamespace}, found)).To(Succeed())
		var baseURL string
		for _, envVar := range found.Spec.Template.Spec.Containers[0].Env {
			switch envVar.Name {
			case "IMAGE_SERVICE_BASE_URL":
				baseURL = envVar.Value
			}
		}
		Expect(baseURL).To(Equal("https://my.test.images"))
	})
})

var _ = Describe("ensureAgentRoute", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
	})

	Context("with no existing route", func() {
		It("should create new route", func() {
			found := &routev1.Route{}
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).ToNot(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newAgentRoute)
			Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName,
				Namespace: testNamespace}, found)).To(Succeed())
		})
	})

	Context("with existing route", func() {
		It("should not change route Host", func() {
			routeHost := "route.example.com"
			r, _, _ := newAgentRoute(ctx, log, ascc)
			route := r.(*routev1.Route)
			route.Spec.Host = routeHost
			Expect(ascr.Client.Create(ctx, route)).To(Succeed())

			AssertReconcileSuccess(ctx, log, ascc, newAgentRoute)

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
		ascc       ASC
		ctx        = context.Background()
		log        = logrus.New()
		privateKey = "test-private-key"
		publicKey  = "test-public-key"
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
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

			AssertReconcileSuccess(ctx, log, ascc, newAgentLocalAuthSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["ec-private-key.pem"]).To(Equal(privateKey))
			Expect(found.StringData["ec-public-key.pem"]).To(Equal(publicKey))
		})
	})

	Context("with no existing local auth secret", func() {
		It("should create new keys and not overwrite them in subsequent reconciles", func() {
			AssertReconcileSuccess(ctx, log, ascc, newAgentLocalAuthSecret)

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

			AssertReconcileSuccess(ctx, log, ascc, newAgentLocalAuthSecret)
			Expect(err).To(BeNil())

			foundAfterNextEnsure := &corev1.Secret{}
			err = ascr.Client.Get(ctx, types.NamespacedName{Name: agentLocalAuthSecretName,
				Namespace: testNamespace}, foundAfterNextEnsure)
			Expect(err).To(BeNil())

			Expect(foundAfterNextEnsure.StringData["ec-private-key.pem"]).To(Equal(foundPrivateKey))
			Expect(foundAfterNextEnsure.StringData["ec-public-key.pem"]).To(Equal(foundPublicKey))
			Expect(foundAfterNextEnsure.Labels).To(HaveKeyWithValue(BackupLabel, BackupLabelValue))
		})
	})
})

var _ = Describe("ensurePostgresSecret", func() {
	var (
		asc  *aiv1beta1.AgentServiceConfig
		ascr *AgentServiceConfigReconciler
		ascc ASC
		ctx  = context.Background()
		log  = logrus.New()
		pass = "password"
	)

	BeforeEach(func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc)
		ascc = initASC(ascr, asc)
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

			AssertReconcileSuccess(ctx, log, ascc, newPostgresSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: databaseName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["db.password"]).To(Equal(pass))
		})
	})

	Context("with no existing postgres secret", func() {
		It("should create new secret with password", func() {
			AssertReconcileSuccess(ctx, log, ascc, newPostgresSecret)

			found := &corev1.Secret{}
			err := ascr.Client.Get(ctx, types.NamespacedName{Name: databaseName, Namespace: testNamespace}, found)
			Expect(err).To(BeNil())

			Expect(found.StringData["db.host"]).To(Equal("localhost"))
			Expect(found.StringData["db.user"]).To(Equal("admin"))
			Expect(found.StringData["db.name"]).To(Equal("installer"))
			Expect(found.StringData["db.port"]).To(Equal(databasePort.String()))
			Expect(found.Labels).To(HaveKeyWithValue(BackupLabel, BackupLabelValue))
			// password will be random
			foundPass := found.StringData["db.password"]
			Expect(foundPass).ToNot(BeNil())
			Expect(foundPass).To(HaveLen(databasePasswordLength))
		})
	})
})

var _ = Describe("newServiceMonitor", func() {
	It("sets tls config correctly", func() {
		ctx := context.Background()
		asc := newASCDefault()
		ascr := newTestReconciler(asc)
		ascc := initASC(ascr, asc)

		AssertReconcileSuccess(ctx, common.GetTestLog(), ascc, newServiceMonitor)

		found := &monitoringv1.ServiceMonitor{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
		Expect(len(found.Spec.Endpoints)).To(Equal(1))
		endpoint := found.Spec.Endpoints[0]
		Expect(endpoint.TLSConfig.CAFile).To(Equal("/etc/prometheus/configmaps/serving-certs-ca-bundle/service-ca.crt"))
		Expect(endpoint.Scheme).To(Equal("https"))
		Expect(endpoint.TLSConfig.ServerName).To(Equal("assisted-service.test-namespace.svc"))
	})
})

var _ = Describe("ensureAssistedServiceDeployment", func() {
	var (
		asc   *aiv1beta1.AgentServiceConfig
		ascr  *AgentServiceConfigReconciler
		ascc  ASC
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
				ascc = initASC(ascr, asc)

				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)
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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

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

			It("should be labelled for backup", func() {
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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

				found := &corev1.ConfigMap{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: testMirrorRegConfigmapName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Labels).To(HaveLen(2))
				Expect(found.Labels).To(HaveKeyWithValue(BackupLabel, BackupLabelValue))
				Expect(found.Labels).To(HaveKeyWithValue(WatchResourceLabel, WatchResourceValue))
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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

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
				ascc = initASC(ascr, asc)
				AssertReconcileFailure(ctx, log, ascc, newAssistedServiceDeployment)
			})
		})
	})

	Describe("ConfigMap hashing as annotations on assisted-service deployment", func() {
		Context("with assisted-service configmap", func() {
			It("should fail if assisted configMap not found", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route)
				ascc = initASC(ascr, asc)
				AssertReconcileFailure(ctx, log, ascc, newAssistedServiceDeployment)
			})

			It("should only add assisted config hash annotation", func() {
				asc = newASCDefault()
				ascr = newTestReconciler(asc, route, assistedCM)
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

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
				ascc = initASC(ascr, asc)
				AssertReconcileFailure(ctx, log, ascc, newAssistedServiceDeployment)
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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

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
				ascc = initASC(ascr, asc)
				AssertReconcileFailure(ctx, log, ascc, newAssistedServiceDeployment)
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
				ascc = initASC(ascr, asc)
				AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

				found := &appsv1.Deployment{}
				Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
				Expect(found.Spec.Template.Annotations).To(HaveLen(3))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(assistedConfigHashAnnotation, Not(Equal(""))))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(mirrorConfigHashAnnotation, Equal("")))
				Expect(found.Spec.Template.Annotations).To(HaveKeyWithValue(userConfigHashAnnotation, Not(Equal(""))))
			})
		})
	})

	It("should expose two ports for ipxe", func() {
		asc = newASCDefault()
		ascr = newTestReconciler(asc, route, assistedCM)
		ascc = initASC(ascr, asc)
		AssertReconcileSuccess(ctx, log, ascc, newAssistedServiceDeployment)

		found := &appsv1.Deployment{}
		Expect(ascr.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, found)).To(Succeed())
		Expect(found.Spec.Template.Spec.Containers).To(HaveLen(2))
		Expect(found.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(2))
		Expect(found.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(servicePort.IntValue())))
		Expect(found.Spec.Template.Spec.Containers[0].Ports[1].ContainerPort).To(Equal(int32(serviceHTTPPort.IntValue())))
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
			"odf": "registry.redhat.io/ocs4/ocs-must-gather-rhel8",
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
		asc *aiv1beta1.AgentServiceConfig
		log *logrus.Logger
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
			//verify the result
			if t.expected != "" {
				Expect(getMustGatherImages(log, &asc.Spec)).To(MatchJSON(t.expected))
			} else {
				Expect(getMustGatherImages(log, &asc.Spec)).To(Equal(""))
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
			Version:          "version-49.123-0",
			CPUArchitecture:  CpuArchitectureX86,
		},
		{
			OpenshiftVersion: "4.9",
			Url:              "rhcos_4.9",
			Version:          "version-49.123-0",
			CPUArchitecture:  CpuArchitectureArm,
		},
	}
	var defaultEnvOsImages = models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(CpuArchitectureX86),
			OpenshiftVersion: swag.String("4.8"),
			URL:              swag.String("rhcos_4.8"),
			Version:          swag.String("version-48.123-0"),
		},
	}
	var outSpecOsImages = models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(CpuArchitectureX86),
			OpenshiftVersion: swag.String("4.9"),
			URL:              swag.String("rhcos_4.9"),
			Version:          swag.String("version-49.123-0"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(CpuArchitectureArm),
			OpenshiftVersion: swag.String("4.9"),
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
		asc *aiv1beta1.AgentServiceConfig
		log *logrus.Logger
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
			// verify the result
			if t.expected != "" {
				Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(t.expected))
			} else {
				Expect(getOSImages(log, &asc.Spec)).To(Equal(""))
			}
		})
	}
})

var _ = Describe("getOSImages", func() {
	var (
		asc         *aiv1beta1.AgentServiceConfig
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
			Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OS images specified", func() {
		It("should build OS images", func() {
			asc, expectedEnv = newASCWithOSImages()
			Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with multiple OS images specified", func() {
		It("should build OS images with multiple keys", func() {
			asc, expectedEnv = newASCWithMultipleOpenshiftVersions()
			Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with duplicate OS images specified", func() {
		It("should take the last specified version", func() {
			asc, expectedEnv = newASCWithDuplicateOpenshiftVersions()
			Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(expectedEnv))
		})
	})
	Context("with OS images x.y.z specified", func() {
		It("should only specify x.y", func() {
			asc, expectedEnv = newASCWithLongOpenshiftVersion()
			Expect(getOSImages(log, &asc.Spec)).To(MatchJSON(expectedEnv))
		})
	})
})

var _ = Describe("newAssistedCM", func() {

	var (
		ascc ASC
		asc  *aiv1beta1.AgentServiceConfig
		ctx  context.Context
		log  logrus.FieldLogger

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
		registryConf = `
		unqualified-search-registries = ["registry.access.redhat.com","docker.io"]
		[[registry]]
			prefix = ""
			location = "quay.io/edge-infrastructure"
			mirror-by-digest-only = true
	
		[[registry.mirror]]
			location = "mirror1.registry.corp.com:5000/edge-infrastructure"`
		mirrorCM = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testMirrorRegConfigmapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				mirrorRegistryRefCertKey: "foo",
			},
		}
	)

	BeforeEach(func() {
		log = logrus.New()
		ctx = context.Background()
		asc = newASCDefault()
		ascr := newTestReconciler(asc, route, imageRoute)
		ascc = initASC(ascr, asc)
	})

	It("INSTALL_INVOKER", func() {
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "INSTALL_INVOKER", "assisted-installer-operator")
	})

	It("sets the base URLs", func() {
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "SERVICE_BASE_URL", fmt.Sprintf("https://%s", testHost))
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "IMAGE_SERVICE_BASE_URL", fmt.Sprintf("https://%s.images", testHost))
	})

	It("default public container registries", func() {
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "PUBLIC_CONTAINER_REGISTRIES", "quay.io,registry.svc.ci.openshift.org")
	})
	It("adds mirror registries", func() {
		asc.Spec.MirrorRegistryRef = &corev1.LocalObjectReference{Name: testMirrorRegConfigmapName}
		mirrorCM.Data[mirrorRegistryRefRegistryConfKey] = registryConf
		ascr := newTestReconciler(asc, route, imageRoute, mirrorCM)
		ascc = initASC(ascr, asc)
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "PUBLIC_CONTAINER_REGISTRIES", "quay.io,registry.svc.ci.openshift.org,registry.access.redhat.com,docker.io")
	})
	It("adds user-specified unauthenticated registries", func() {
		asc.Spec.UnauthenticatedRegistries = []string{"example.com"}
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "PUBLIC_CONTAINER_REGISTRIES", "quay.io,registry.svc.ci.openshift.org,example.com")
	})
	It("ignores duplicate values", func() {
		asc.Spec.UnauthenticatedRegistries = []string{"example.com", "quay.io", "docker.io"}
		asc.Spec.MirrorRegistryRef = &corev1.LocalObjectReference{Name: testMirrorRegConfigmapName}
		mirrorCM.Data[mirrorRegistryRefRegistryConfKey] = registryConf
		ascr := newTestReconciler(asc, route, imageRoute, mirrorCM)
		ascc = initASC(ascr, asc)
		ensureNewAssistedConfigmapValue(ctx, log, ascc, "PUBLIC_CONTAINER_REGISTRIES", "quay.io,registry.svc.ci.openshift.org,registry.access.redhat.com,docker.io,example.com")
	})
})

var _ = Describe("getDeploymentData", func() {

	const (
		acmDeployName      = "multiclusterhub-operator"
		acmDeployNamespace = "open-cluster-management"
		acmContainerName   = "multiclusterhub-operator"
		mceDeployName      = "multicluster-engine-operator"
		mceDeployNamespace = "multicluster-engine"
		mceContainerName   = "backplane-operator"
	)

	var (
		ascc ASC
		ctx  context.Context
		cm   *corev1.ConfigMap
		asc  = newASCDefault()
	)

	BeforeEach(func() {
		ctx = context.Background()
		ascr := newTestReconciler(asc)
		ascc = initASC(ascr, asc)
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: ascc.namespace,
			},
			Data: map[string]string{},
		}
	})
	It("doesn't change the DEPLOYMENT TYPE and VERSION if it's already set in the configmap", func() {
		cm.Data["DEPLOYMENT_TYPE"] = "TEST"
		cm.Data["DEPLOYMENT_VERSION"] = "1.0.0"
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("TEST"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("1.0.0"))
	})
	It("gets the ACM DEPLOYMENT TYPE and VERSION when the ACM deployment exists", func() {
		deploy := createDeploy(acmDeployName, acmDeployNamespace, acmContainerName, "1.1.1")
		ascr := newTestReconciler(asc, deploy)
		ascc = initASC(ascr, asc)
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("ACM"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("1.1.1"))
	})
	It("gets the MCE DEPLOYMENT TYPE and VERSION when the ACM deployment doesn't exists", func() {
		deploy := createDeploy(mceDeployName, mceDeployNamespace, mceContainerName, "1.2.3")
		ascr := newTestReconciler(asc, deploy)
		ascc = initASC(ascr, asc)
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("MCE"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("1.2.3"))
	})
	It("gets the ACM DEPLOYMENT TYPE and VERSION when both the ACM and MCE deployments exist", func() {
		acmDeploy := createDeploy(acmDeployName, acmDeployNamespace, acmContainerName, "1.1.1")
		mceDeploy := createDeploy(mceDeployName, mceDeployNamespace, mceContainerName, "1.2.3")
		ascr := newTestReconciler(asc, acmDeploy, mceDeploy)
		ascc = initASC(ascr, asc)
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("ACM"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("1.1.1"))
	})
	It("sets the DEPLOYMENT TYPE and VERSION to operator when both ACM/MCE don't exist", func() {
		getDeploymentData(ctx, cm, ascc)
		version := ServiceImage()
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("Operator"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal(version))
	})
	It("sets DEPLOYMENT VERSION to unknown when env var OPERATOR_VERSION doesn't exist in the deployment", func() {
		deploy := createDeploy(acmDeployName, acmDeployNamespace, acmContainerName, "")
		ascr := newTestReconciler(asc, deploy)
		ascc = initASC(ascr, asc)
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("ACM"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("Unknown"))
	})
	It("sets DEPLOYMENT VERSION to unknown when the container doesn't exist in the deployment", func() {
		deploy := createDeploy(acmDeployName, acmDeployNamespace, "", "1.1.1")
		ascr := newTestReconciler(asc, deploy)
		ascc = initASC(ascr, asc)
		getDeploymentData(ctx, cm, ascc)
		Expect(cm.Data["DEPLOYMENT_TYPE"]).To(Equal("ACM"))
		Expect(cm.Data["DEPLOYMENT_VERSION"]).To(Equal("Unknown"))
	})
})

func createDeploy(name, namespace, containerName, version string) *appsv1.Deployment {
	var envVar corev1.EnvVar
	if version != "" {
		envVar = corev1.EnvVar{
			Name:  "OPERATOR_VERSION",
			Value: version,
		}
	}
	var container corev1.Container
	if containerName != "" {
		container = corev1.Container{
			Name: containerName,
			Env:  []corev1.EnvVar{envVar},
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
		},
	}
}

func ensureNewAssistedConfigmapValue(ctx context.Context, log logrus.FieldLogger, ascc ASC, key, value string) {
	cm, mutateFn, err := newAssistedCM(ctx, log, ascc)

	Expect(err).ToNot(HaveOccurred())
	Expect(mutateFn()).To(Succeed())
	configMap := cm.(*corev1.ConfigMap)

	Expect(configMap.Data[key]).To(Equal(value))
}

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
			ImageStorage: &corev1.PersistentVolumeClaimSpec{
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
		},
		{
			OpenshiftVersion: "4.9",
			Version:          "49",
			Url:              "4.9.iso",
		},
	}

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.9"),
			Version:          swag.String("49"),
			URL:              swag.String("4.9.iso"),
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
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
		},
	}

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.7"),
			Version:          swag.String("47"),
			URL:              swag.String("4.7.iso"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
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
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "loser",
			Url:              "loser",
		},
		{
			OpenshiftVersion: "4.8",
			Version:          "48",
			Url:              "4.8.iso",
		},
	}

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.7"),
			Version:          swag.String("47"),
			URL:              swag.String("4.7.iso"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("loser"),
			URL:              swag.String("loser"),
		},
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
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
		},
	}

	encoded, _ := json.Marshal(models.OsImages{
		&models.OsImage{
			CPUArchitecture:  swag.String(""),
			OpenshiftVersion: swag.String("4.8.0"),
			Version:          swag.String("48"),
			URL:              swag.String("4.8.iso"),
		},
	})

	return asc, string(encoded)
}
