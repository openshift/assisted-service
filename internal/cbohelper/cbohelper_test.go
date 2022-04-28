package cbohelper

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCBOHelper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CBOHelper Suite")
}

// The implementation should change so the tests validate basic happy flow
var _ = Describe("CBOHelper", func() {
	var (
		c         client.Client
		mockCtrl  *gomock.Controller
		cboHelper CBOHelper
		log       = common.GetTestLog().WithField("pkg", "cluster_baremetal_operator_helper")
	)

	BeforeEach(func() {
		var schemes = runtime.NewScheme()
		utilruntime.Must(scheme.AddToScheme(schemes))
		utilruntime.Must(v1.Install(schemes))
		utilruntime.Must(metal3iov1alpha1.AddToScheme(schemes))
		c = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
		mockCtrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("Check converged flow availability", func() {
		It("converged flow available", func() {
			cboHelper = CBOHelper{
				Client:         c,
				log:            log,
				kubeAPIEnabled: true,
				config: Config{
					BaremetalIronicAgentImage:      "Ironic-image",
					MinimalVersionForConvergedFlow: "4.11.0"},
			}
			clusterOperator := &v1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: "baremetal",
				},
				Status: v1.ClusterOperatorStatus{
					Versions: []v1.OperandVersion{{Name: "baremetal", Version: "4.11.0"}},
				},
			}
			Expect(c.Create(context.Background(), clusterOperator)).To(BeNil())
			cboHelper.setConvergedFlowAvailability()
			Expect(cboHelper.ConvergedFlowAvailable()).Should(Equal(true))
		})
	})

	Context("GenerateIronicConfig", func() {
		It("success", func() {
			cboHelper = CBOHelper{
				Client:         c,
				log:            log,
				kubeAPIEnabled: true,
				config: Config{
					BaremetalIronicAgentImage:      "Ironic-image",
					MinimalVersionForConvergedFlow: "4.11.0"},
			}
			provisioningInfo := &metal3iov1alpha1.Provisioning{
				ObjectMeta: metav1.ObjectMeta{
					Name: metal3iov1alpha1.ProvisioningSingletonName,
				},
				Spec: metal3iov1alpha1.ProvisioningSpec{
					ProvisioningNetwork:            metal3iov1alpha1.ProvisioningNetworkManaged,
					VirtualMediaViaExternalNetwork: false,
					ProvisioningIP:                 "10.10.10.10",
				},
			}
			Expect(c.Create(context.Background(), provisioningInfo)).To(BeNil())

			conf, err := cboHelper.GenerateIronicConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(len(conf.Storage.Files)).Should(Equal(1))
			Expect(conf.Storage.Files[0].Path).Should(Equal("/etc/ironic-python-agent.conf"))
			Expect(len(conf.Systemd.Units)).Should(Equal(1))
			Expect(conf.Systemd.Units[0].Name).Should(Equal("ironic-agent.service"))
			Expect(*conf.Systemd.Units[0].Contents).Should(ContainSubstring("Ironic-image"))

		})
	})
})
