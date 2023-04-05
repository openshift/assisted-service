package spoke_k8s_client

import (
	"context"
	"fmt"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	authzv1 "github.com/openshift/api/authorization/v1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	authorizationv1interfaces "k8s.io/client-go/kubernetes/typed/authorization/v1"
	cerv1 "k8s.io/client-go/kubernetes/typed/certificates/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client.go . SpokeK8sClient
type SpokeK8sClient interface {
	client.Client
	ListCsrs(ctx context.Context) (*certificatesv1.CertificateSigningRequestList, error)
	ApproveCsr(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) error
	GetNode(ctx context.Context, name string) (*corev1.Node, error)
	PatchNodeLabels(ctx context.Context, nodeName string, nodeLabels string) error
	PatchMachineConfigPoolPaused(ctx context.Context, pause bool, mcpName string) error
	DeleteNode(ctx context.Context, name string) error
}

type spokeK8sClient struct {
	client.Client
	csrClient   cerv1.CertificateSigningRequestInterface
	sarClient   authorizationv1interfaces.SelfSubjectAccessReviewInterface
	nodesClient typedcorev1.NodeInterface
	log         logrus.FieldLogger
}

func (c *spokeK8sClient) ListCsrs(ctx context.Context) (*certificatesv1.CertificateSigningRequestList, error) {
	return c.csrClient.List(ctx, metav1.ListOptions{})
}

func (c *spokeK8sClient) ApproveCsr(ctx context.Context, csr *certificatesv1.CertificateSigningRequest) error {
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Reason:         "NodeCSRApprove",
		Message:        "This CSR was approved by the assisted-service",
		Status:         corev1.ConditionTrue,
		LastUpdateTime: metav1.Now(),
	})
	_, err := c.csrClient.UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
	return err
}

func (c *spokeK8sClient) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	node, err := c.nodesClient.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		node = nil
	}
	return node, err
}

func (c *spokeK8sClient) PatchNodeLabels(ctx context.Context, nodeName string, nodeLabels string) error {
	data := []byte(`{"metadata": {"labels": ` + nodeLabels + `}}`)
	_, err := c.nodesClient.Patch(ctx, nodeName, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}

func (c *spokeK8sClient) PatchMachineConfigPoolPaused(ctx context.Context, pause bool, mcpName string) error {
	mcp := &mcfgv1.MachineConfigPool{}
	err := c.Get(ctx, types.NamespacedName{Name: mcpName}, mcp)
	if err != nil {
		return err
	}
	if mcp.Spec.Paused == pause {
		return nil
	}
	pausePatch := []byte(fmt.Sprintf("{\"spec\":{\"paused\":%t}}", pause))
	c.log.Infof("Setting pause MCP %s to %t", mcpName, pause)
	return c.Patch(ctx, mcp, client.RawPatch(types.MergePatchType, pausePatch))
}

func (c *spokeK8sClient) DeleteNode(ctx context.Context, name string) error {
	return c.nodesClient.Delete(ctx, name, metav1.DeleteOptions{})
}

func GetKubeClientSchemes() *runtime.Scheme {
	var schemes = runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(schemes))
	utilruntime.Must(corev1.AddToScheme(schemes))
	utilruntime.Must(aiv1beta1.AddToScheme(schemes))
	utilruntime.Must(hivev1.AddToScheme(schemes))
	utilruntime.Must(hiveext.AddToScheme(schemes))
	utilruntime.Must(bmh_v1alpha1.AddToScheme(schemes))
	utilruntime.Must(machinev1beta1.AddToScheme(schemes))
	utilruntime.Must(monitoringv1.AddToScheme(schemes))
	utilruntime.Must(routev1.Install(schemes))
	utilruntime.Must(apiregv1.AddToScheme(schemes))
	utilruntime.Must(configv1.Install(schemes))
	utilruntime.Must(metal3iov1alpha1.AddToScheme(schemes))
	utilruntime.Must(authzv1.AddToScheme(schemes))
	utilruntime.Must(apiextensionsv1.AddToScheme(schemes))
	utilruntime.Must(mcfgv1.AddToScheme(schemes))
	return schemes
}
