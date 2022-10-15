package controllers

import (
	"testing"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func init() {
	_ = v1beta1.AddToScheme(scheme.Scheme)
	_ = hivev1.AddToScheme(scheme.Scheme)
	_ = hiveext.AddToScheme(scheme.Scheme)
	_ = bmh_v1alpha1.AddToScheme(scheme.Scheme)
	_ = routev1.AddToScheme(scheme.Scheme)
}

const (
	testNamespace     = "test-namespace"
	testPullSecretVal = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNzd29yZAo=","email":"r@r.com"}}}` //nolint:gosec
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "controllers tests")
}

var (
	logrusLogger *logrus.Logger
	logger       logr.Logger
)

var _ = BeforeSuite(func() {
	// Configure the Kubernetes and controller-runtime libraries so that they write log messages
	// to the Ginkgo writer, this way those messages are automatically associated to the right
	// test.
	logrusLogger = logrus.New()
	logrusLogger.Out = GinkgoWriter
	logger = logrusr.New(logrusLogger)
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)
})

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

func newBMH(name string, spec *bmh_v1alpha1.BareMetalHostSpec) *bmh_v1alpha1.BareMetalHost {
	return &bmh_v1alpha1.BareMetalHost{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BareMetalHost",
			APIVersion: "metal3.io/v1beta1",
		}, ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: *spec,
	}
}

func newBMHRequest(host *bmh_v1alpha1.BareMetalHost) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: host.ObjectMeta.Namespace,
		Name:      host.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newImageSet(name, releaseImage string) *hivev1.ClusterImageSet {
	imageSet := &hivev1.ClusterImageSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterImageSet",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "",
		},
		Spec: hivev1.ClusterImageSetSpec{
			ReleaseImage: releaseImage,
		},
	}

	return imageSet
}

func getDefaultTestImageSet(name, releaseImage string) *hivev1.ClusterImageSet {
	return newImageSet(name, releaseImage)
}
