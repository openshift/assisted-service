package kubernetes_test

import (
	"context"
	"os"
	"testing"

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

func TestKubePodIntrospector(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KubePodIntrospector")
}

var _ = Describe("KubePodIntrospector", func() {
	var (
		ctx           context.Context
		fakeClient    client.Client
		testPodName   string
		testNamespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		testPodName = "test-operator-pod"
		testNamespace = "test-namespace"

		// Set up fake client with scheme
		scheme := runtime.NewScheme()
		err := corev1.AddToScheme(scheme)
		Expect(err).ToNot(HaveOccurred())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})
	AfterEach(func() {
		os.Unsetenv("POD_NAME")
		os.Unsetenv("NAMESPACE")
	})

	Describe("NewKubePodIntrospector", func() {
		Context("when environment variables are set correctly", func() {
			It("should create a new KubePodIntrospector instance successfully", func() {
				os.Setenv("POD_NAME", testPodName)
				os.Setenv("NAMESPACE", testNamespace)

				introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
				Expect(err).ToNot(HaveOccurred())
				Expect(introspector).ToNot(BeNil())
			})
		})

		Context("when POD_NAME environment variable is missing", func() {
			It("should return an error", func() {
				os.Setenv("NAMESPACE", testNamespace)

				introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when NAMESPACE environment variable is missing", func() {
			It("should return an error", func() {
				os.Setenv("POD_NAME", testPodName)

				introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when both environment variables are missing", func() {
			It("should return an error", func() {
				introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})

		Context("when environment variables are empty strings", func() {
			It("should return an error", func() {
				os.Setenv("POD_NAME", "")
				os.Setenv("NAMESPACE", "")

				introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("POD_NAME or NAMESPACE environment variables not set"))
				Expect(introspector).To(BeNil())
			})
		})
	})

	Describe("GetImagePullSecrets", func() {
		var introspector *kubernetes.KubePodIntrospector

		BeforeEach(func() {
			os.Setenv("POD_NAME", testPodName)
			os.Setenv("NAMESPACE", testNamespace)

			var err error
			introspector, err = kubernetes.NewKubePodIntrospector(fakeClient)
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
				introspector, err = kubernetes.NewKubePodIntrospector(errorClient)
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

			introspector, err := kubernetes.NewKubePodIntrospector(fakeClient)
			Expect(err).ToNot(HaveOccurred())

			var _ kubernetes.PodIntrospector = introspector
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
