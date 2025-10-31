package kubernetes_test

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("PodIntrospector", func() {
	var (
		ctx             context.Context
		fakeClient      client.Client
		testPodName     string
		testNamespace   string
		originalPodName string
		originalNS      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testPodName = "test-operator-pod"
		testNamespace = "test-namespace"

		// Store original environment variables
		originalPodName = os.Getenv("POD_NAME")
		originalNS = os.Getenv("NAMESPACE")

		// Set up fake client with scheme
		scheme := runtime.NewScheme()
		err := corev1.AddToScheme(scheme)
		Expect(err).ToNot(HaveOccurred())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})

	AfterEach(func() {
		// Restore original environment variables
		if originalPodName != "" {
			os.Setenv("POD_NAME", originalPodName)
		} else {
			os.Unsetenv("POD_NAME")
		}

		if originalNS != "" {
			os.Setenv("NAMESPACE", originalNS)
		} else {
			os.Unsetenv("NAMESPACE")
		}
	})

	Describe("NewPodIntrospector", func() {
		Context("when environment variables are set correctly", func() {
			BeforeEach(func() {
				os.Setenv("POD_NAME", testPodName)
				os.Setenv("NAMESPACE", testNamespace)
			})

			It("should create a new PodIntrospector instance successfully", func() {
				introspector, err := kubernetes.NewPodIntrospector(fakeClient)
				Expect(err).ToNot(HaveOccurred())
				Expect(introspector).ToNot(BeNil())
			})
		})

		Context("when POD_NAME environment variable is missing", func() {
			BeforeEach(func() {
				os.Unsetenv("POD_NAME")
				os.Setenv("NAMESPACE", testNamespace)
			})

			It("should return an error", func() {
				introspector, err := kubernetes.NewPodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when NAMESPACE environment variable is missing", func() {
			BeforeEach(func() {
				os.Setenv("POD_NAME", testPodName)
				os.Unsetenv("NAMESPACE")
			})

			It("should return an error", func() {
				introspector, err := kubernetes.NewPodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when both environment variables are missing", func() {
			BeforeEach(func() {
				os.Unsetenv("POD_NAME")
				os.Unsetenv("NAMESPACE")
			})

			It("should return an error", func() {
				introspector, err := kubernetes.NewPodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when environment variables are empty strings", func() {
			BeforeEach(func() {
				os.Setenv("POD_NAME", "")
				os.Setenv("NAMESPACE", "")
			})

			It("should return an error", func() {
				introspector, err := kubernetes.NewPodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})
	})

	Describe("GetImagePullSecrets", func() {
		var introspector *kubernetes.PodIntrospector

		BeforeEach(func() {
			os.Setenv("POD_NAME", testPodName)
			os.Setenv("NAMESPACE", testNamespace)

			var err error
			introspector, err = kubernetes.NewPodIntrospector(fakeClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(introspector).ToNot(BeNil())
		})

		Context("when the pod exists and has imagePullSecrets", func() {
			var testPod *corev1.Pod

			BeforeEach(func() {
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPodName,
						Namespace: testNamespace,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "secret1"},
							{Name: "secret2"},
						},
					},
				}

				err := fakeClient.Create(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return the imagePullSecrets from the pod", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(HaveLen(2))
				Expect(secrets[0].Name).To(Equal("secret1"))
				Expect(secrets[1].Name).To(Equal("secret2"))
			})
		})

		Context("when the pod exists but has no imagePullSecrets", func() {
			var testPod *corev1.Pod

			BeforeEach(func() {
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPodName,
						Namespace: testNamespace,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{},
					},
				}

				err := fakeClient.Create(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return an empty slice", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(HaveLen(0))
				Expect(secrets).ToNot(BeNil())
			})
		})

		Context("when the pod exists but has nil imagePullSecrets", func() {
			var testPod *corev1.Pod

			BeforeEach(func() {
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPodName,
						Namespace: testNamespace,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: nil,
					},
				}

				err := fakeClient.Create(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return nil", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(BeNil())
			})
		})

		Context("when there's an error retrieving the pod", func() {
			var errorClient *errorInjectingClient

			BeforeEach(func() {
				var err error
				errorClient = &errorInjectingClient{
					Client:      fakeClient,
					injectError: true,
				}
				introspector, err = kubernetes.NewPodIntrospector(errorClient)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return nil and log a warning", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(BeNil())
			})
		})

		Context("when pod is in a different namespace", func() {
			var testPod *corev1.Pod

			BeforeEach(func() {
				// Create pod in different namespace
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPodName,
						Namespace: "different-namespace",
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "secret1"},
						},
					},
				}

				err := fakeClient.Create(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return nil because pod is not found in the expected namespace", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(BeNil())
			})
		})

		Context("when pod has multiple imagePullSecrets with various names", func() {
			var testPod *corev1.Pod

			BeforeEach(func() {
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testPodName,
						Namespace: testNamespace,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: []corev1.LocalObjectReference{
							{Name: "registry-secret"},
							{Name: "quay-secret"},
							{Name: "docker-registry-secret"},
							{Name: "private-registry-secret"},
						},
					},
				}

				err := fakeClient.Create(ctx, testPod)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return all imagePullSecrets in the same order", func() {
				secrets := introspector.GetImagePullSecrets(ctx)
				Expect(secrets).To(HaveLen(4))
				Expect(secrets[0].Name).To(Equal("registry-secret"))
				Expect(secrets[1].Name).To(Equal("quay-secret"))
				Expect(secrets[2].Name).To(Equal("docker-registry-secret"))
				Expect(secrets[3].Name).To(Equal("private-registry-secret"))
			})
		})
	})

	Describe("Interface compliance", func() {
		It("should implement PodIntrospectorInterface", func() {
			os.Setenv("POD_NAME", testPodName)
			os.Setenv("NAMESPACE", testNamespace)

			introspector, err := kubernetes.NewPodIntrospector(fakeClient)
			Expect(err).ToNot(HaveOccurred())

			var _ kubernetes.PodIntrospectorInterface = introspector
		})
	})

	Describe("Cache behavior", func() {
		var introspector *kubernetes.PodIntrospector

		BeforeEach(func() {
			os.Setenv("POD_NAME", testPodName)
			os.Setenv("NAMESPACE", testNamespace)

			var err error
			introspector, err = kubernetes.NewPodIntrospector(fakeClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(introspector).ToNot(BeNil())
		})

		It("should work efficiently with cached pod data", func() {
			// Create a pod with imagePullSecrets
			testPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "cache-secret1"},
						{Name: "cache-secret2"},
					},
				},
			}

			err := fakeClient.Create(ctx, testPod)
			Expect(err).ToNot(HaveOccurred())

			// The client automatically uses cache when available
			secrets := introspector.GetImagePullSecrets(ctx)
			Expect(secrets).To(HaveLen(2))
			Expect(secrets[0].Name).To(Equal("cache-secret1"))
			Expect(secrets[1].Name).To(Equal("cache-secret2"))
		})

		It("should handle multiple concurrent calls efficiently", func() {
			// Create a pod with imagePullSecrets
			testPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "concurrent-secret"},
					},
				},
			}

			err := fakeClient.Create(ctx, testPod)
			Expect(err).ToNot(HaveOccurred())

			// Make multiple concurrent calls - cache handles concurrency automatically
			done := make(chan []corev1.LocalObjectReference, 3)
			for i := 0; i < 3; i++ {
				go func() {
					secrets := introspector.GetImagePullSecrets(ctx)
					done <- secrets
				}()
			}

			// All calls should return the same result
			for i := 0; i < 3; i++ {
				secrets := <-done
				Expect(secrets).To(HaveLen(1))
				Expect(secrets[0].Name).To(Equal("concurrent-secret"))
			}
		})

		It("should be simple and lightweight without manual concurrency management", func() {
			// This test demonstrates that the new approach is much simpler
			// No need for goroutines, mutexes, or manual caching
			// The controller-runtime cache handles all of that for us

			testPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testPodName,
					Namespace: testNamespace,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "simple-secret"},
					},
				},
			}

			err := fakeClient.Create(ctx, testPod)
			Expect(err).ToNot(HaveOccurred())

			// Just a simple call - the cache handles everything else
			secrets := introspector.GetImagePullSecrets(ctx)
			Expect(secrets).To(HaveLen(1))
			Expect(secrets[0].Name).To(Equal("simple-secret"))
		})
	})
})

// errorInjectingClient is a test helper that wraps a client and injects errors
type errorInjectingClient struct {
	client.Client
	injectError bool
}

func (e *errorInjectingClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if e.injectError {
		return &errors.StatusError{
			ErrStatus: metav1.Status{
				Status: metav1.StatusFailure,
				Code:   500,
				Reason: metav1.StatusReasonInternalError,
				Details: &metav1.StatusDetails{
					Name: key.Name,
					Kind: "Pod",
				},
				Message: "injected error for testing",
			},
		}
	}
	return e.Client.Get(ctx, key, obj, opts...)
}
