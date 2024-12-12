package spoke_k8s_client

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Factory", func() {
	var logger *logrus.Logger

	BeforeEach(func() {
		logger = logrus.New()
		logger.SetOutput(GinkgoWriter)
	})

	Describe("Create from secret", func() {
		It("Fails if secret doesn't contain data", func() {
			factory := NewSpokeK8sClientFactory(logger)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "myns",
					Name:      "mysecret",
				},
				Data: nil,
			}
			_, err := factory.CreateFromSecret(secret)
			Expect(err).To(MatchError("Secret myns/mysecret does not contain any data"))
		})

		It("Fails if secret doesn't contain a 'kubeconfig' data item", func() {
			factory := NewSpokeK8sClientFactory(logger)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "myns",
					Name:      "mysecret",
				},
				Data: map[string][]byte{
					"mydata": []byte("myvalue"),
				},
			}
			_, err := factory.CreateFromSecret(secret)
			Expect(err).To(MatchError("Secret data for myns/mysecret does not contain kubeconfig"))
		})

		It("Fails if secret contains a 'kubeconfig' data item with junk", func() {
			factory := NewSpokeK8sClientFactory(logger)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "myns",
					Name:      "mysecret",
				},
				Data: map[string][]byte{
					"kubeconfig": []byte("junk"),
				},
			}
			_, err := factory.CreateFromSecret(secret)
			Expect(err).To(MatchError(ContainSubstring("cannot unmarshal")))
		})
	})
})
