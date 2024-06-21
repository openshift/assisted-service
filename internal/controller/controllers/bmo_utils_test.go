package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The implementation should change so the tests validate basic happy flow
var _ = Describe("bmoUtils", func() {
	var (
		c        client.Client
		mockCtrl *gomock.Controller
		log      = common.GetTestLog().WithField("pkg", "cluster_baremetal_operator_helper")
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
	Context("ConvergedFlowAvailable", func() {
		DescribeTable("returns true with",
			func(version string) {
				bmoUtils := &bmoUtils{
					c:              c,
					log:            log,
					kubeAPIEnabled: true,
				}
				clusterOperator := CreateCBO(version)
				Expect(c.Create(context.Background(), clusterOperator)).To(BeNil())
				Expect(bmoUtils.ConvergedFlowAvailable()).Should(Equal(true))
			},
			Entry("version 4.12.0", "4.12.0"),
			Entry("version 4.12.1", "4.12.1"),
			Entry("version 4.12.0-ec.4", "4.12.0-ec.4"),
			Entry("version 4.12.0-0.nightly-2022-10-25-210451", "4.12.0-0.nightly-2022-10-25-210451"),
		)
		It("returns false when version is lower than minimal version", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			clusterOperator := CreateCBO("4.10.0")
			Expect(c.Create(context.Background(), clusterOperator)).To(BeNil())
			Expect(bmoUtils.ConvergedFlowAvailable()).Should(Equal(false))
		})
		It("returns false when it fails to find cluster version", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			Expect(bmoUtils.ConvergedFlowAvailable()).Should(Equal(false))
		})
	})
	Context("Get GetIronicServiceURL", func() {
		It("success", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			ironicIP := "10.10.10.10"
			provisioningInfo := &metal3iov1alpha1.Provisioning{
				ObjectMeta: metav1.ObjectMeta{
					Name: metal3iov1alpha1.ProvisioningSingletonName,
				},
				Spec: metal3iov1alpha1.ProvisioningSpec{
					ProvisioningNetwork:            metal3iov1alpha1.ProvisioningNetworkManaged,
					VirtualMediaViaExternalNetwork: false,
					ProvisioningIP:                 ironicIP,
				},
			}
			Expect(c.Create(context.Background(), provisioningInfo)).To(BeNil())
			serviceIPs, inspectorIPs, err := bmoUtils.GetIronicIPs()
			Expect(err).Should(BeNil())
			Expect(serviceIPs[0]).Should(Equal(ironicIP))
			Expect(inspectorIPs[0]).Should(Equal(ironicIP))
		})
		It("failed to determine inspector URL", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			provisioningInfo := &metal3iov1alpha1.Provisioning{
				ObjectMeta: metav1.ObjectMeta{
					Name: metal3iov1alpha1.ProvisioningSingletonName,
				},
				Spec: metal3iov1alpha1.ProvisioningSpec{
					ProvisioningNetwork:            metal3iov1alpha1.ProvisioningNetworkManaged,
					VirtualMediaViaExternalNetwork: false,
					ProvisioningIP:                 "",
				},
			}
			Expect(c.Create(context.Background(), provisioningInfo)).To(BeNil())
			serviceIPs, inspectorIPs, err := bmoUtils.GetIronicIPs()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unable to determine inspector IP, check if metal3 pod is running"))
			Expect(serviceIPs).Should(BeNil())
			Expect(inspectorIPs).Should(BeNil())
		})

	})
	Context("GetICCConfig", func() {
		It("success", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			ironicURLs := getUrlFromIP("10.10.10.11")
			inspectorURLs := getUrlFromIP("10.10.10.10")
			agentImage := "quay.io/some/agent:image"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      iccSecretName,
					Namespace: iccNamespace,
				},
				Data: map[string][]byte{
					ironicBaseURLKey:          []byte(ironicURLs),
					ironicInspectorBaseURLKey: []byte(inspectorURLs),
					ironicAgentImageKey:       []byte(agentImage),
				},
			}
			Expect(c.Create(context.Background(), secret)).To(BeNil())
			iccConfig, err := bmoUtils.GetICCConfig()
			Expect(err).Should(BeNil())
			Expect(iccConfig.IronicBaseURL).Should(Equal(ironicURLs))
			Expect(iccConfig.IronicInspectorBaseUrl).Should(Equal(inspectorURLs))
			Expect(iccConfig.IronicAgentImage).Should(Equal(agentImage))
		})
		It("throws an error when secret is missing", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			_, err := bmoUtils.GetICCConfig()
			Expect(err).Should(Not(BeNil()))
		})

		DescribeTable("throws an error when config is incomplete",
			func(ironicURLs []byte, inspectorURLs []byte, agentImage []byte) {
				bmoUtils := &bmoUtils{
					c:              c,
					log:            log,
					kubeAPIEnabled: true,
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      iccSecretName,
						Namespace: iccNamespace,
					},
					Data: map[string][]byte{},
				}
				if ironicURLs != nil {
					secret.Data[ironicBaseURLKey] = ironicURLs
				}
				if inspectorURLs != nil {
					secret.Data[ironicInspectorBaseURLKey] = inspectorURLs
				}
				if agentImage != nil {
					secret.Data[ironicAgentImageKey] = agentImage
				}

				Expect(c.Create(context.Background(), secret)).To(BeNil())
				_, err := bmoUtils.GetICCConfig()
				Expect(err).Should(Not(BeNil()))
			},
			Entry("ironicURLs is missing", nil, []byte("some"), []byte("some")),
			Entry("ironicInspectorURLs is missing", []byte("some"), nil, []byte("some")),
			Entry("ironicAgentImage is missing", []byte("some"), []byte("some"), nil),
		)
		It("throws an error when the configuration is incomplete", func() {
			bmoUtils := &bmoUtils{
				c:              c,
				log:            log,
				kubeAPIEnabled: true,
			}
			_, err := bmoUtils.GetICCConfig()
			Expect(err).Should(Not(BeNil()))
		})

	})
})

func CreateCBO(version string) *v1.ClusterOperator {

	clusterOperator := &v1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: "baremetal",
		},
		Status: v1.ClusterOperatorStatus{
			Versions: []v1.OperandVersion{{Name: "baremetal", Version: version}},
		},
	}

	return clusterOperator
}
