package spoke_k8s_client

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bombsimon/logrusr/v3"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	. "github.com/openshift/assisted-service/internal/testing"
)

func TestSpokeK8SClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spoke client")
}

var (
	ctx          context.Context
	logger       *logrus.Logger
	hubEnv       *envtest.Environment
	hubClient    ctrlclient.Client
	hubNamespace *corev1.Namespace
)

var _ = BeforeSuite(func() {
	// Create a context:
	ctx = context.Background()

	// Create a logger that writes to the Ginkgo output:
	logger = logrus.New()
	logger.SetOutput(GinkgoWriter)

	// Configure the controller-runtime library to use our logger:
	adapter := logrusr.New(logger)
	ctrl.SetLogger(adapter)
	klog.SetLogger(adapter)

	// Start the hub environment:
	hubScheme := runtime.NewScheme()
	scheme.AddToScheme(hubScheme)
	corev1.AddToScheme(hubScheme)
	hivev1.AddToScheme(hubScheme)
	hubEnv = SetupEnvtest(&envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "hack", "crds"),
		},
		Scheme: hubScheme,
	})
	hubConfig, err := hubEnv.Start()
	Expect(err).ToNot(HaveOccurred())

	// Create the hub client:
	hubOpts := ctrlclient.Options{
		Scheme: hubScheme,
	}
	hubClient, err = ctrlclient.New(hubConfig, hubOpts)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	// Stop the hub environment:
	err := hubEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = BeforeEach(func() {
	// Create the namespace:
	hubNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "myns-",
		},
	}
	err := hubClient.Create(ctx, hubNamespace)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterEach(func() {
	// Delete the namespace:
	err := hubClient.Delete(ctx, hubNamespace)
	Expect(err).ToNot(HaveOccurred())
})
