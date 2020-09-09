package common

import (
	"time"

	"github.com/openshift/assisted-service/models"
)

type Cluster struct {
	models.Cluster
	// The pull secret that obtained from the Pull Secret page on the Red Hat OpenShift Cluster Manager site.
	PullSecret string `json:"pull_secret" gorm:"type:TEXT"`

	// The compute hash value of the http-proxy, https-proxy and no-proxy attributes, used internally to indicate
	// if the proxy settings were changed while downloading ISO
	ProxyHash string `json:"proxy_hash"`

	// Used to detect if DHCP allocation task is timed out
	MachineNetworkCidrUpdatedAt time.Time
}
