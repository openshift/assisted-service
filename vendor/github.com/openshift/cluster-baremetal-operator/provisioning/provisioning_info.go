package provisioning

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
)

type NetworkStackType int

const (
	NetworkStackV4   NetworkStackType = 1 << iota
	NetworkStackV6   NetworkStackType = 1 << iota
	NetworkStackDual NetworkStackType = (NetworkStackV4 | NetworkStackV6)
)

func (ns NetworkStackType) IpOption() string {
	switch ns {
	case NetworkStackV4:
		return "ip=dhcp"
	case NetworkStackV6:
		return "ip=dhcp6"
	case NetworkStackDual:
		return "ip=dhcp,dhcp6"
	default:
		return ""
	}
}

type ProvisioningInfo struct {
	Client                  kubernetes.Interface
	EventRecorder           events.Recorder
	ProvConfig              *metal3iov1alpha1.Provisioning
	Scheme                  *runtime.Scheme
	Namespace               string
	Images                  *Images
	Proxy                   *configv1.Proxy
	NetworkStack            NetworkStackType
	MasterMacAddresses      []string
	SSHKey                  string
	BaremetalWebhookEnabled bool
	OSClient                osclientset.Interface
	ResourceCache           resourceapply.ResourceCache
}
