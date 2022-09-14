package controllers

import (
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/spoke_k8s_client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Context("with kubeconfig test secret", func() {
	const (
		testKubeconfigSecretName      = "test-secret"
		testKubeconfigSecretNamespace = "test-namespace"
	)

	var (
		mockCtrl         *gomock.Controller
		mockSpokeClient  *spoke_k8s_client.MockSpokeK8sClient
		mockSpokeFactory *spoke_k8s_client.MockSpokeK8sClientFactory
		clientCache      SpokeClientCache
		kubeconfigSecret *corev1.Secret
	)

	newKubeconfigSecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testKubeconfigSecretName,
				Namespace: testKubeconfigSecretNamespace,
			},
			Data: map[string][]byte{
				"kubeconfig": []byte(BASIC_KUBECONFIG),
			},
			Type: corev1.SecretTypeOpaque,
		}
	}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockSpokeClient = spoke_k8s_client.NewMockSpokeK8sClient(mockCtrl)
		mockSpokeFactory = spoke_k8s_client.NewMockSpokeK8sClientFactory(mockCtrl)
		clientCache = NewSpokeClientCache(mockSpokeFactory)
		kubeconfigSecret = newKubeconfigSecret()
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("Get", func() {
		It("successfully creates a new client", func() {
			mockSpokeFactory.EXPECT().CreateFromRawKubeconfig([]byte(BASIC_KUBECONFIG)).Return(mockSpokeClient, nil)
			client, err := clientCache.Get(kubeconfigSecret)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(client).To(Equal(mockSpokeClient))
		})

		It("successfully returns an existing client", func() {
			// create a client
			mockSpokeFactory.EXPECT().CreateFromRawKubeconfig([]byte(BASIC_KUBECONFIG)).Return(mockSpokeClient, nil)
			client, err := clientCache.Get(kubeconfigSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(client).To(Equal(mockSpokeClient))

			// get created client
			client, err = clientCache.Get(kubeconfigSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(client).To(Equal(mockSpokeClient))
		})

		It("successfully creates a new client on hash mismatch", func() {
			// create a client
			mockSpokeFactory.EXPECT().CreateFromRawKubeconfig([]byte(BASIC_KUBECONFIG)).Return(mockSpokeClient, nil)
			client, err := clientCache.Get(kubeconfigSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(client).To(Equal(mockSpokeClient))

			// create a client from a new kubeconfig
			kubeconfigSecret.Data["kubeconfig"] = []byte("new")
			mockSpokeFactory.EXPECT().CreateFromRawKubeconfig([]byte("new")).Return(mockSpokeClient, nil)
			client, err = clientCache.Get(kubeconfigSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(client).To(Equal(mockSpokeClient))
		})

		It("fails due failure on create client from kubeconfig", func() {
			mockSpokeFactory.EXPECT().CreateFromRawKubeconfig(gomock.Any()).Return(nil, errors.New("error"))
			_, err := clientCache.Get(kubeconfigSecret)

			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("Failed to create client using secret"))
		})

		It("fails due to missing data in kubeconfig secret", func() {
			kubeconfigSecret.Data = nil
			_, err := clientCache.Get(kubeconfigSecret)

			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("does not contain any data"))
		})

		It("fails due to missing kubeconfig key in secret", func() {
			kubeconfigSecret.Data = make(map[string][]byte)
			_, err := clientCache.Get(kubeconfigSecret)

			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("does not contain kubeconfig"))
		})
	})
})
