package spoke_k8s_client

import (
	"context"
	"fmt"
	"io"

	"github.com/go-openapi/strfmt"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	authzv1 "github.com/openshift/api/authorization/v1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	"github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	authorizationv1 "k8s.io/api/authorization/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	authorizationv1interfaces "k8s.io/client-go/kubernetes/typed/authorization/v1"
	cerv1 "k8s.io/client-go/kubernetes/typed/certificates/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client_factory.go . SpokeK8sClientFactory
type SpokeK8sClientFactory interface {
	CreateFromSecret(secret *corev1.Secret) (SpokeK8sClient, error)
	CreateFromRawKubeconfig(kubeconfig []byte) (SpokeK8sClient, error)
	CreateFromStorageKubeconfig(ctx context.Context, clusterId *strfmt.UUID, objectHandler s3wrapper.API) (SpokeK8sClient, error)
}

//go:generate mockgen --build_flags=--mod=mod -package=spoke_k8s_client -destination=mock_spoke_k8s_client.go . SpokeK8sClient
type SpokeK8sClient interface {
	client.Client
	ListCsrs() (*certificatesv1.CertificateSigningRequestList, error)
	ApproveCsr(csr *certificatesv1.CertificateSigningRequest) error
	CreateSubjectAccessReview(subjectAccessReview *authorizationv1.SelfSubjectAccessReview) (*authorizationv1.SelfSubjectAccessReview, error)
	IsActionPermitted(verb string, resource string) (bool, error)
	GetNode(name string) (*corev1.Node, error)
	PatchNodeLabels(nodeName string, nodeLabels string) error
	PatchMachineConfigPoolPaused(pause bool, mcpName string) error
}

type spokeK8sClient struct {
	client.Client
	csrClient   cerv1.CertificateSigningRequestInterface
	sarClient   authorizationv1interfaces.SelfSubjectAccessReviewInterface
	nodesClient typedcorev1.NodeInterface
	log         logrus.FieldLogger
}

type spokeK8sClientFactory struct {
	log logrus.FieldLogger
}

func NewSpokeK8sClientFactory(log logrus.FieldLogger) SpokeK8sClientFactory {
	return &spokeK8sClientFactory{
		log: log,
	}
}

func (cf *spokeK8sClientFactory) CreateFromSecret(secret *corev1.Secret) (SpokeK8sClient, error) {
	clientConfig, err := cf.getRestConfigFromSecret(secret)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting client from kubeconfig cluster")
		return nil, err
	}
	return cf.createFromClientConfig(clientConfig)
}

func (cf *spokeK8sClientFactory) CreateFromRawKubeconfig(kubeconfig []byte) (SpokeK8sClient, error) {
	clientConfig, err := cf.getRestConfigFromKubeConfig(kubeconfig)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting client from kubeconfig cluster")
		return nil, err
	}
	return cf.createFromClientConfig(clientConfig)
}

func (cf *spokeK8sClientFactory) CreateFromStorageKubeconfig(ctx context.Context, clusterId *strfmt.UUID, objectHandler s3wrapper.API) (SpokeK8sClient, error) {
	kubeConfigReader, contentLength, err := objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterId, constants.Kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("could not load kubeconfig from internal storage with cluster id %s and filename %s: %w", clusterId, constants.Kubeconfig, err)
	}

	kubeconfig := make([]byte, contentLength)
	bytesRead, err := io.ReadAtLeast(kubeConfigReader, kubeconfig, int(contentLength))
	if err != nil {
		return nil, fmt.Errorf("could not read spoke cluster kubeconfig from internal storage with cluster id %s and filename %s: %w", clusterId, constants.Kubeconfig, err)
	}
	if bytesRead > int(contentLength) {
		return nil, fmt.Errorf("too many bytes read when reading spoke cluster kubeconfig from internal storage with cluster id %s and filename %s", clusterId, constants.Kubeconfig)
	}
	return cf.CreateFromRawKubeconfig(kubeconfig)
}

func (cf *spokeK8sClientFactory) getRestConfigFromSecret(secret *corev1.Secret) (*rest.Config, error) {
	if secret.Data == nil {
		return nil, errors.Errorf("Secret %s/%s  does not contain any data", secret.Namespace, secret.Name)
	}
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfigData) == 0 {
		return nil, errors.Errorf("Secret data for %s/%s  does not contain kubeconfig", secret.Namespace, secret.Name)
	}
	return cf.getRestConfigFromKubeConfig(kubeconfigData)
}

func (cf *spokeK8sClientFactory) getRestConfigFromKubeConfig(kubeconfig []byte) (*rest.Config, error) {
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get clientconfig from kubeconfig data in secret")
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get restconfig for kube client")
	}

	return restConfig, nil
}

func (cf *spokeK8sClientFactory) createFromClientConfig(clientConfig *rest.Config) (SpokeK8sClient, error) {
	config, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		cf.log.WithError(err).Warnf("Getting kuberenetes config for cluster")
		return nil, err
	}
	schemes := GetKubeClientSchemes()
	targetClient, err := client.New(clientConfig, client.Options{Scheme: schemes})
	if err != nil {
		cf.log.WithError(err).Warnf("failed to get spoke kube client")
		return nil, err
	}
	data := spokeK8sClient{
		Client:      targetClient,
		csrClient:   config.CertificatesV1().CertificateSigningRequests(),
		sarClient:   config.AuthorizationV1().SelfSubjectAccessReviews(),
		nodesClient: config.CoreV1().Nodes(),
		log:         cf.log,
	}
	return &data, nil
}

// Create a subject access review and get a response in order to determine capabilities.
func (c *spokeK8sClient) CreateSubjectAccessReview(subjectAccessReview *authorizationv1.SelfSubjectAccessReview) (*authorizationv1.SelfSubjectAccessReview, error) {
	return c.sarClient.Create(context.TODO(), subjectAccessReview, metav1.CreateOptions{})
}

func (c *spokeK8sClient) IsActionPermitted(verb string, resource string) (bool, error) {
	sar := authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     verb,
				Resource: resource,
			},
		},
	}
	sarResponse, err := c.CreateSubjectAccessReview(&sar)
	if err != nil {
		return false, fmt.Errorf("could not create subject access review to detemine %s %s permissions %w", verb, resource, err)
	}

	if sarResponse.Status.Allowed {
		return true, nil
	}
	return false, nil
}

func (c *spokeK8sClient) ListCsrs() (*certificatesv1.CertificateSigningRequestList, error) {
	return c.csrClient.List(context.TODO(), metav1.ListOptions{})
}

func (c *spokeK8sClient) ApproveCsr(csr *certificatesv1.CertificateSigningRequest) error {
	csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Reason:         "NodeCSRApprove",
		Message:        "This CSR was approved by the assisted-service",
		Status:         corev1.ConditionTrue,
		LastUpdateTime: metav1.Now(),
	})
	_, err := c.csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
	return err
}

func (c *spokeK8sClient) GetNode(name string) (*corev1.Node, error) {
	node, err := c.nodesClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		node = nil
	}
	return node, err
}

func (c *spokeK8sClient) PatchNodeLabels(nodeName string, nodeLabels string) error {
	data := []byte(`{"metadata": {"labels": ` + nodeLabels + `}}`)
	_, err := c.nodesClient.Patch(context.Background(), nodeName, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}

func (c *spokeK8sClient) PatchMachineConfigPoolPaused(pause bool, mcpName string) error {
	mcp := &mcfgv1.MachineConfigPool{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: mcpName}, mcp)
	if err != nil {
		return err
	}
	if mcp.Spec.Paused == pause {
		return nil
	}
	pausePatch := []byte(fmt.Sprintf("{\"spec\":{\"paused\":%t}}", pause))
	c.log.Infof("Setting pause MCP %s to %t", mcpName, pause)
	return c.Patch(context.TODO(), mcp, client.RawPatch(types.MergePatchType, pausePatch))
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
