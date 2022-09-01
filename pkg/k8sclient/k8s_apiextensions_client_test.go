package k8sclient

import (
	"errors"
	"testing"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

const (
	kubeconfigKeyInSecret = "kubeconfig"
	testKubeconfigName    = "external-cluster-kubeconfig"
	testNamespace         = "test-namespace"
)

func TestK8sClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "K8S client")
}

var _ = Describe("K8sApiExtensionsClient", func() {
	var (
		mockCtrl *gomock.Controller
		schemes  *runtime.Scheme
		log      logrus.FieldLogger
		f        K8sApiExtensionsClientFactory
	)

	BeforeEach(func() {
		log = logrus.New()
		mockCtrl = gomock.NewController(GinkgoT())
		schemes = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(schemes))
		f = NewK8sApiExtensionsClientFactory(log)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("CreateFromSecret", func() {
		mockSecret := func() *corev1.Secret {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testKubeconfigName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					kubeconfigKeyInSecret: getFakeKubeconfig(),
				},
				Type: corev1.SecretTypeOpaque,
			}
			return secret
		}

		It("Client created successfully", func() {
			secret := mockSecret()
			_, err := f.CreateFromSecret(secret)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Missing data in secret", func() {
			secret := mockSecret()
			secret.Data = nil
			_, err := f.CreateFromSecret(secret)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("does not contain any data"))
		})

		It("Missing kubeconfig in secret", func() {
			secret := mockSecret()
			secret.Data[kubeconfigKeyInSecret] = nil
			_, err := f.CreateFromSecret(secret)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("does not contain kubeconfig"))
		})

		It("Invalid kubeconfig in secret", func() {
			secret := mockSecret()
			secret.Data[kubeconfigKeyInSecret] = []byte("test")
			_, err := f.CreateFromSecret(secret)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to get clientconfig from kubeconfig data in secret"))
		})

		It("Failure getting in-cluster clientset", func() {
			old := getClientSet
			defer func() { getClientSet = old }()
			getClientSet = func(restConfig *rest.Config) (*apiextensions.Clientset, error) {
				return nil, errors.New("error")
			}
			secret := mockSecret()
			_, err := f.CreateFromSecret(secret)
			Expect(err).Should(HaveOccurred())
		})

		It("Failure getting rest config", func() {
			old := getRestConfig
			defer func() { getRestConfig = old }()
			getRestConfig = func(clientConfig clientcmd.ClientConfig) (*rest.Config, error) {
				return nil, errors.New("error")
			}
			secret := mockSecret()
			_, err := f.CreateFromSecret(secret)
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("CreateFromInClusterConfig", func() {
		It("Client created successfully", func() {
			old := getInClusterConfig
			defer func() { getInClusterConfig = old }()
			getInClusterConfig = func() (*rest.Config, error) {
				clientConfig, err := clientcmd.NewClientConfigFromBytes(getFakeKubeconfig())
				Expect(err).ShouldNot(HaveOccurred())
				return clientConfig.ClientConfig()
			}
			_, err := f.CreateFromInClusterConfig()
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Failure getting in-cluster config", func() {
			_, err := f.CreateFromInClusterConfig()
			Expect(err).Should(HaveOccurred())
		})

		It("Failure getting in-cluster clientset", func() {
			oldGetInClusterConfig := getInClusterConfig
			defer func() { getInClusterConfig = oldGetInClusterConfig }()
			getInClusterConfig = func() (*rest.Config, error) {
				return nil, nil
			}
			oldGetClientSet := getClientSet
			defer func() { getClientSet = oldGetClientSet }()
			getClientSet = func(restConfig *rest.Config) (*apiextensions.Clientset, error) {
				return nil, errors.New("error")
			}
			_, err := f.CreateFromInClusterConfig()
			Expect(err).Should(HaveOccurred())
		})
	})
})

func getFakeKubeconfig() []byte {
	return []byte(`
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://127.1.2.3:12345
  name: integration
contexts:
- context:
    cluster: integration
    user: test
  name: default-context
current-context: default-context
users:
- name: test
  user:
    password: test
    username: test
`)
}
