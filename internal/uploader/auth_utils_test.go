package uploader

import (
	"encoding/base64"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("getPullSecret", func() {
	var (
		ctrl                   *gomock.Controller
		mockK8sClient          *k8sclient.MockK8SClient
		clusterPullSecretToken string
		OCMPullSecretToken     string
		pullSecretFormat       string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockK8sClient = k8sclient.NewMockK8SClient(ctrl)
		clusterPullSecretToken = base64.StdEncoding.EncodeToString([]byte("clustersecret:clustercredentials"))
		OCMPullSecretToken = base64.StdEncoding.EncodeToString([]byte("ocmuser:ocmcredentials"))
		pullSecretFormat = `{"auths":{"%s":{"auth":"%s","email":"r@example.com"}}}` // #nosec
	})

	It("successfully gets a pull secret when the OCM pull secret is present", func() {
		OCMPullSecret := fmt.Sprintf(pullSecretFormat, openshiftTokenKey, OCMPullSecretToken)
		data := map[string][]byte{corev1.DockerConfigJsonKey: []byte(OCMPullSecret)}

		OCMSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-config",
				Name:      "pull-secret",
			},
			Data: data,
			Type: corev1.SecretTypeDockerConfigJson,
		}
		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(OCMSecret, nil).Times(1)
		pullSecret, err := getPullSecret("", mockK8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(pullSecret.Identity.Username).To(Equal("ocmuser"))
		Expect(pullSecret.Identity.EmailDomain).To(Equal("example.com"))
		Expect(pullSecret.APIAuth.AuthRaw).To(Equal(OCMPullSecretToken))
	})
	It("successfully gets a pull secret from the cluster when the OCM pull secret doesn't exist", func() {
		clusterPullSecret := fmt.Sprintf(pullSecretFormat, openshiftTokenKey, clusterPullSecretToken)

		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(
			nil, apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "Secret"}, "pullsecret")).Times(1)
		pullSecret, err := getPullSecret(clusterPullSecret, mockK8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(pullSecret.Identity.Username).To(Equal("clustersecret"))
		Expect(pullSecret.Identity.EmailDomain).To(Equal("example.com"))
		Expect(pullSecret.APIAuth.AuthRaw).To(Equal(clusterPullSecretToken))
	})
	It("fails to gets a pull secret when the OCM pull secret exists, but doesn't contain the correct credentials", func() {
		OCMPullSecret := fmt.Sprintf(pullSecretFormat, "clouds.openshift.com", OCMPullSecretToken)
		data := map[string][]byte{corev1.DockerConfigJsonKey: []byte(OCMPullSecret)}

		OCMSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-config",
				Name:      "pull-secret",
			},
			Data: data,
			Type: corev1.SecretTypeDockerConfigJson,
		}
		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(OCMSecret, nil).Times(1)
		_, err := getPullSecret("", mockK8sClient)
		Expect(err).To(HaveOccurred())
	})
	It("fails to gets a pull secret when the token is incorrectly formatted", func() {
		token := fmt.Sprintf("%s\n%s", OCMPullSecretToken, "token")
		OCMPullSecret := fmt.Sprintf(pullSecretFormat, "cloud.openshift.com", token)
		data := map[string][]byte{corev1.DockerConfigJsonKey: []byte(OCMPullSecret)}

		OCMSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "openshift-config",
				Name:      "pull-secret",
			},
			Data: data,
			Type: corev1.SecretTypeDockerConfigJson,
		}
		mockK8sClient.EXPECT().GetSecret("openshift-config", "pull-secret").Return(OCMSecret, nil).Times(1)
		_, err := getPullSecret("", mockK8sClient)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("getEmailDomain", func() {
	It("successfully gets the email domain from a valid email", func() {
		emailDomain := "example.com"
		email := fmt.Sprintf("%s@%s", "aUser", emailDomain)
		domain := getEmailDomain(email)
		Expect(domain).NotTo(BeEmpty())
		Expect(domain).To(Equal(emailDomain))
	})
	It("fails to get an email domain from an invalid email", func() {
		email := "fakeemail.com"
		domain := getEmailDomain(email)
		Expect(domain).To(BeEmpty())
	})
})
