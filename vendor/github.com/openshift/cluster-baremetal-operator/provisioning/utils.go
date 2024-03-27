package provisioning

import (
	"context"
	"errors"
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	osconfigv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
)

func getPod(podClient coreclientv1.PodsGetter, targetNamespace string) (corev1.Pod, error) {
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: stateService,
		}}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return corev1.Pod{}, err
	}

	listOptions := metav1.ListOptions{
		LabelSelector: selector.String(),
	}

	podList, err := podClient.Pods(targetNamespace).List(context.Background(), listOptions)
	if err != nil {
		return corev1.Pod{}, err
	}

	// On fail-over, two copies of the pod will be present: the old
	// Terminating one and the new Running one. Ignore terminating pods.
	var pods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			pods = append(pods, pod)
		}
	}

	if len(pods) == 0 {
		return corev1.Pod{}, nil
	}

	if len(pods) > 1 {
		return corev1.Pod{}, fmt.Errorf("there should be only one running pod listed for the given label")
	}

	return pods[0], nil
}

// getPodIPs returns pod IPs for the Metal3 pod (and thus Ironic and its httpd).
func getPodIPs(podClient coreclientv1.PodsGetter, targetNamespace string) (ips []string, err error) {
	pod, err := getPod(podClient, targetNamespace)
	if err != nil {
		return nil, err
	}
	// NOTE(dtantsur): we can use PodIPs here because with host networking they're identical to HostIP
	for _, ip := range pod.Status.PodIPs {
		if ip.IP != "" {
			ips = append(ips, ip.IP)
		}
	}
	if len(ips) == 0 {
		// This is basically a safeguard to be able to assume that the returned slice is not empty later on
		err = errors.New("the metal3 pod does not have any podIP's yet")
	}
	return
}

// getServerInternalIPs returns virtual IPs on which Kubernetes is accessible.
// These are the IPs on which the proxied services (currently Ironic and Inspector) should be accessed by external consumers.
func getServerInternalIPs(osclient osclientset.Interface) ([]string, error) {
	infra, err := osclient.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("Cannot get the 'cluster' object from infrastructure API: %w", err)
		return nil, err
	}
	switch infra.Status.PlatformStatus.Type {
	case osconfigv1.BareMetalPlatformType:
		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.BareMetal == nil {
			return nil, nil
		}
		return infra.Status.PlatformStatus.BareMetal.APIServerInternalIPs, nil
	case osconfigv1.OpenStackPlatformType:
		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.OpenStack == nil {
			return nil, nil
		}
		return infra.Status.PlatformStatus.OpenStack.APIServerInternalIPs, nil
	case osconfigv1.VSpherePlatformType:
		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.VSphere == nil {
			return nil, nil
		}
		return infra.Status.PlatformStatus.VSphere.APIServerInternalIPs, nil
	case osconfigv1.AWSPlatformType:
		return nil, nil
	case osconfigv1.AzurePlatformType:
		return nil, nil
	case osconfigv1.GCPPlatformType:
		return nil, nil
	case osconfigv1.NonePlatformType:
		return nil, nil
	default:
		err = fmt.Errorf("Cannot detect server API VIP: Attribute not supported on platform: %v", infra.Status.PlatformStatus.Type)
		return nil, err
	}
}

// GetRealIronicIPs returns the actual IPs on which Ironic is accessible without a proxy.
// The provisioning IP is used when present and not disallowed for virtual media via configuration.
func GetRealIronicIPs(info *ProvisioningInfo) ([]string, error) {
	config := info.ProvConfig.Spec
	if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && !config.VirtualMediaViaExternalNetwork {
		return []string{config.ProvisioningIP}, nil
	}

	return getPodIPs(info.Client.CoreV1(), info.Namespace)
}

// GetIronicIPs returns Ironic IPs for external consumption, potentially behind an HA proxy.
// Without a proxy, the provisioning IP is used when present and not disallowed for virtual media via configuration.
func GetIronicIPs(info *ProvisioningInfo) (ironicIPs []string, inspectorIPs []string, err error) {
	podIPs, err := GetRealIronicIPs(info)
	if err != nil {
		return
	}

	if UseIronicProxy(&info.ProvConfig.Spec) {
		ironicIPs, err = getServerInternalIPs(info.OSClient)
		if err != nil {
			err = fmt.Errorf("error fetching internalIPs: %w", err)
			return
		}

		if ironicIPs == nil {
			ironicIPs = podIPs
		}
	} else {
		ironicIPs = podIPs
	}

	inspectorIPs = ironicIPs // keep returning separate variables for future enhancements
	return ironicIPs, inspectorIPs, err
}

func IpOptionForProvisioning(config *metal3iov1alpha1.ProvisioningSpec, networkStack NetworkStackType) string {
	var optionValue string
	ip := net.ParseIP(config.ProvisioningIP)
	if config.ProvisioningNetwork == metal3iov1alpha1.ProvisioningNetworkDisabled || ip == nil {
		// It ProvisioningNetworkDisabled or no valid IP to check, fallback to the external network
		return networkStack.IpOption()
	}
	if ip.To4() != nil {
		optionValue = "ip=dhcp"
	} else {
		optionValue = "ip=dhcp6"
	}
	return optionValue
}
