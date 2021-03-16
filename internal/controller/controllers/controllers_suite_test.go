package controllers

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func init() {
	_ = v1alpha1.AddToScheme(scheme.Scheme)
	_ = hivev1.AddToScheme(scheme.Scheme)
}

const (
	testNamespace     = "test-namespace"
	testPullSecretVal = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNzd29yZAo=","email":"r@r.com"}}}` //nolint:gosec
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "controllers tests")
}

func newSecret(name, namespace string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	return secret
}

func getDefaultTestPullSecret(name, namespace string) *corev1.Secret {
	return newSecret(name, namespace,
		map[string][]byte{corev1.DockerConfigJsonKey: []byte(testPullSecretVal)})
}
