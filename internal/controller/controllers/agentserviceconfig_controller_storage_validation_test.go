package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/testing"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Agent service config controller storage validation", func() {
	var (
		ctx        context.Context
		cluster    *testing.FakeCluster
		client     clnt.Client
		subject    *aiv1beta1.AgentServiceConfig
		key        clnt.ObjectKey
		request    ctrl.Request
		reconciler *AgentServiceConfigReconciler
	)

	BeforeEach(func() {
		// Create a context for the test:
		ctx = context.Background()

		// Create the fake cluster:
		cluster = testing.NewFakeCluster().
			Logger(logger).
			Build()

		// Create the client:
		client = cluster.Client()

		// Create the default object to be reconciled, with a storage configuration that
		// passes all validations. Specific tests will later adapt these settings as needed.
		subject = &aiv1beta1.AgentServiceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "agent",
			},
			Spec: aiv1beta1.AgentServiceConfigSpec{
				DatabaseStorage: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				FileSystemStorage: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
				ImageStorage: &corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ti"),
						},
					},
				},
			},
		}
		key = clnt.ObjectKeyFromObject(subject)
		request = ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: subject.Name,
			},
		}

		// Create the reconciler:
		reconciler = &AgentServiceConfigReconciler{
			Client:    client,
			Scheme:    cluster.Scheme(),
			Log:       logrusLogger,
			Recorder:  cluster.Recorder(),
			Namespace: "assisted-installer",
		}
	})

	AfterEach(func() {
		cluster.Stop()
	})

	When("Objects haven't been created", func() {
		It("Rejects small database storage", func() {
			subject.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(aiv1beta1.ReasonStorageFailure))
				g.Expect(condition.Message).To(ContainSubstring(
					"Database storage 1Mi is too small",
				))
			}).Should(Succeed())
		})

		It("Rejects small filesystem storage", func() {
			subject.Spec.FileSystemStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(aiv1beta1.ReasonStorageFailure))
				g.Expect(condition.Message).To(ContainSubstring(
					"Filesystem storage 1Mi is too small",
				))
			}).Should(Succeed())
		})

		It("Rejects small image storage", func() {
			subject.Spec.ImageStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(aiv1beta1.ReasonStorageFailure))
				g.Expect(condition.Message).To(ContainSubstring(
					"Image storage 1Mi is too small",
				))
			}).Should(Succeed())
		})

		It("Rejects small database and filesystem storage (two failures)", func() {
			subject.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			subject.Spec.FileSystemStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(aiv1beta1.ReasonStorageFailure))
				g.Expect(condition.Message).To(ContainSubstring(
					"Database storage 1Mi is too small",
				))
				g.Expect(condition.Message).To(ContainSubstring(
					"Filesystem storage 1Mi is too small",
				))
			}).Should(Succeed())
		})

		It("Accepts large database storage", func() {
			subject.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Pi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts large filesystem storage", func() {
			subject.Spec.FileSystemStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Pi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts large image storage", func() {
			subject.Spec.ImageStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Pi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts unspecified image storage (it is optional)", func() {
			subject.Spec.ImageStorage = nil
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Resumes reconciling when configration is fixed", func() {
			By("Creating invalid configuration")
			subject.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Mi"),
			}
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err = reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(aiv1beta1.ReasonStorageFailure))
			}).Should(Succeed())

			By("Fixing configuration")
			patched := subject.DeepCopy()
			patched.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Pi"),
			}
			err = client.Patch(ctx, patched, clnt.MergeFrom(subject))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})
	})

	When("Objects have been created before", func() {
		BeforeEach(func() {
			// Run the reconciler once so that it succeeds and therefore it creates the
			// objects, in particular the persistent volume claims and the stateful set
			// for the image service, as that is what we are interested on in this case.
			err := client.Create(ctx, subject)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts small database storage", func() {
			patched := subject.DeepCopy()
			patched.Spec.DatabaseStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1M"),
			}
			err := client.Patch(ctx, patched, clnt.MergeFrom(subject))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts small filesystem storage", func() {
			patched := subject.DeepCopy()
			patched.Spec.FileSystemStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1M"),
			}
			err := client.Patch(ctx, patched, clnt.MergeFrom(subject))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts small image storage", func() {
			patched := subject.DeepCopy()
			patched.Spec.ImageStorage.Resources.Requests = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1M"),
			}
			err := client.Patch(ctx, patched, clnt.MergeFrom(subject))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})

		It("Accepts unspecified image storage (it is optional)", func() {
			patched := subject.DeepCopy()
			patched.Spec.ImageStorage = nil
			err := client.Patch(ctx, patched, clnt.MergeFrom(subject))
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				_, err := reconciler.Reconcile(ctx, request)
				g.Expect(err).ToNot(HaveOccurred())
				err = client.Get(ctx, key, subject)
				g.Expect(err).ToNot(HaveOccurred())
				condition := conditionsv1.FindStatusCondition(
					subject.Status.Conditions,
					aiv1beta1.ConditionReconcileCompleted,
				)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			}).Should(Succeed())
		})
	})
})
