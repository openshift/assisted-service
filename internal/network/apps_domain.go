package network

import (
	"fmt"
	"strings"

	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/models"
)

// GetClusterAppsDomain returns the domain used for application-ingress DNS checks.
// When IngressDomain is set (typically from ingress.config.openshift.io/cluster
// .spec.domain, or .spec.appsDomain when present), that value is used as-is.
// Otherwise the legacy apps.<cluster-name>.<base-dns-domain> formula is used.
func GetClusterAppsDomain(c *models.Cluster) string {
	if c == nil {
		return ""
	}
	if d := strings.TrimSpace(c.IngressDomain); d != "" {
		return strings.TrimPrefix(d, "*.")
	}
	if c.Name == "" || c.BaseDNSDomain == "" {
		return ""
	}
	return fmt.Sprintf("apps.%s.%s", c.Name, c.BaseDNSDomain)
}

// GetAppsDomainProbeHost returns the concrete hostname used to verify apps DNS
// resolution (console-openshift-console.<apps-domain>).
func GetAppsDomainProbeHost(c *models.Cluster) string {
	appsDomain := GetClusterAppsDomain(c)
	if appsDomain == "" {
		return ""
	}
	return fmt.Sprintf("%s.%s", constants.AppsSubDomainNameHostDNSValidation, appsDomain)
}
