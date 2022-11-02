package controllers

import (
	"context"
	"fmt"

	osconfigv1 "github.com/openshift/api/config/v1"
	v1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const MinimalVersionForConvergedFlow = "4.12.0-0.alpha"

type BMOUtils struct {
	// The methods of this receiver get called once before the cache is initialized hence we check the API directly
	c              client.Reader
	osClient       *osclientset.Clientset
	kubeClient     *kubernetes.Clientset
	log            logrus.FieldLogger
	kubeAPIEnabled bool
}

func NewBMOUtils(client client.Reader, osClient *osclientset.Clientset, kubeClient *kubernetes.Clientset, log logrus.FieldLogger, kubeAPIEnabled bool) *BMOUtils {
	return &BMOUtils{client, osClient, kubeClient, log, kubeAPIEnabled}
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get;list;watch
// +kubebuilder:rbac:groups=metal3.io,resources=provisionings,verbs=get

// ConvergedFlowAvailable checks the baremetal operator version and returns true if it's equal or higher than the minimal version for converged flow
func (r *BMOUtils) ConvergedFlowAvailable() bool {
	key := types.NamespacedName{
		Name: "baremetal",
	}
	clusterOperator := &v1.ClusterOperator{}
	if err := r.c.Get(context.TODO(), key, clusterOperator); err != nil {
		r.log.Errorf("Error querying api for baremetal operator status: %s", err)
		return false
	}
	if len(clusterOperator.Status.Versions) == 0 {
		r.log.Infof("no version found for baremetal operator")
		return false
	}
	version := clusterOperator.Status.Versions[0].Version
	r.log.Infof("The baremetal operator version is %s, the minimal version for the converged flow is %s", version, MinimalVersionForConvergedFlow)

	available, err := common.VersionGreaterOrEqual(version, MinimalVersionForConvergedFlow)
	if err != nil {
		r.log.WithError(err).Error("Failed to compare CBO version to minimal version for converged flow")
	}
	r.log.Infof("Converged flow enabled: %t", available)
	return available
}

func (r *BMOUtils) GetIronicServiceURL() (string, error) {
	provisioningInfo, err := r.readProvisioningCR()
	if err != nil {
		r.log.WithError(err).Error("unable to get provisioning CR")
		return "", err
	}
	ironicIP, _, err := getIronicIP(r.kubeClient, provisioningInfo.Namespace, &provisioningInfo.Spec, r.osClient)
	if err != nil || ironicIP == "" {
		r.log.WithError(err).Error("unable to determine Ironic's IP")
		return "", err
	}
	ironicURL := getUrlFromIP(ironicIP)
	r.log.Infof("Ironic URL is: %s", ironicURL)
	return ironicURL, nil
}

func (r *BMOUtils) readProvisioningCR() (*metal3iov1alpha1.Provisioning, error) {
	// Fetch the Provisioning instance
	instance := &metal3iov1alpha1.Provisioning{}
	namespacedName := types.NamespacedName{Name: metal3iov1alpha1.ProvisioningSingletonName, Namespace: ""}
	if err := r.c.Get(context.TODO(), namespacedName, instance); err != nil {
		return nil, errors.Wrap(err, "unable to read Provisioning CR")
	}
	return instance, nil
}

func getUrlFromIP(ipAddr string) string {
	if network.IsIPv6Addr(ipAddr) {
		return "https://" + fmt.Sprintf("[%s]", ipAddr)
	}
	if network.IsIPv4Addr(ipAddr) {
		return "https://" + ipAddr
	} else {
		return ""
	}
}

// TODO: replace this and following functions with GetIronicIP in BMO:
// https://github.com/openshift/cluster-baremetal-operator/blob/01f7f551e4d26e93bf9efe5c599859242820633e/provisioning/utils.go#L86
func getIronicIP(client kubernetes.Interface, targetNamespace string, config *metal3iov1alpha1.ProvisioningSpec, osclient osclientset.Interface) (ironicIP string, inspectorIP string, err error) {
	if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && !config.VirtualMediaViaExternalNetwork {
		inspectorIP = config.ProvisioningIP
	} else {
		inspectorIP, err = getPodHostIP(client.CoreV1(), targetNamespace)
		if err != nil {
			return
		}
	}

	if useIronicProxy(config) {
		ironicIP, err = getServerInternalIP(osclient)
		if ironicIP == "" {
			ironicIP = inspectorIP
		}
	} else {
		ironicIP = inspectorIP
	}

	return
}

func useIronicProxy(config *metal3iov1alpha1.ProvisioningSpec) bool {
	return config.ProvisioningNetwork == metal3iov1alpha1.ProvisioningNetworkDisabled || config.VirtualMediaViaExternalNetwork
}

func getServerInternalIP(osclient osclientset.Interface) (string, error) {
	infra, err := osclient.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("Cannot get the 'cluster' object from infrastructure API: %w", err)
		return "", err
	}
	switch infra.Status.PlatformStatus.Type {
	case osconfigv1.BareMetalPlatformType:
		return infra.Status.PlatformStatus.BareMetal.APIServerInternalIP, nil
	case osconfigv1.OpenStackPlatformType:
		return infra.Status.PlatformStatus.OpenStack.APIServerInternalIP, nil
	case osconfigv1.VSpherePlatformType:
		return infra.Status.PlatformStatus.VSphere.APIServerInternalIP, nil
	case osconfigv1.AWSPlatformType:
		return "", nil
	case osconfigv1.NonePlatformType:
		return "", nil
	default:
		err = fmt.Errorf("Cannot detect server API VIP: Attribute not supported on platform: %v", infra.Status.PlatformStatus.Type)
		return "", err
	}
}

func getPodHostIP(podClient coreclientv1.PodsGetter, targetNamespace string) (string, error) {
	metal3AppName := "metal3"
	stateService := "metal3-state"
	cboLabelName := "baremetal.openshift.io/cluster-baremetal-operator"

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: stateService,
		}}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return "", err
	}

	listOptions := metav1.ListOptions{
		LabelSelector: selector.String(),
	}

	podList, err := podClient.Pods(targetNamespace).List(context.Background(), listOptions)
	if err != nil {
		return "", err
	}

	// On fail-over, two copies of the pod will be present: the old
	// Terminating one and the new Running one. Ignore terminating pods.
	var pods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			pods = append(pods, pod)
		}
	}

	var hostIP string
	switch len(pods) {
	case 0:
		// Ironic IP not available yet, just return an empty string
	case 1:
		hostIP = pods[0].Status.HostIP
	default:
		// We expect only one pod with the above LabelSelector
		err = fmt.Errorf("there should be only one running pod listed for the given label")
	}

	return hostIP, err
}
