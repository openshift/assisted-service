package constants

const Kubeconfig = "kubeconfig"
const KubeconfigNoIngress = "kubeconfig-noingress"

// an arbitrary subdomain of *.apps.<cluster-name>.<base-domain> used by DNS
// validations to verify that *.apps wildcard is configured properly
const AppsSubDomainNameHostDNSValidation = "console-openshift-console"

// Standard cluster API subdomains
const APIClusterSubdomain = "api"
const InternalAPIClusterSubdomain = "api-int"

// Arbitrary, non-existing subdomain directly under *.<cluster-name>.<base-domain> (as opposed to
// directly under *.apps.<cluster-name>.<base-domain>) for wildcard configuration check. If this
// domain *does* resolve then the validation failed, as it indicates a wildcard configuration that
// is known to be problematic for OCP
const DNSWildcardFalseDomainName = "validateNoWildcardDNS"

// Plain http machine config server port
const InsecureMCSPort = 22624
