package constants

const Kubeconfig = "kubeconfig"
const KubeconfigNoIngress = "kubeconfig-noingress"

//A Sub domain of apps.clusterName.baseDomain used by DNS validations to verify that *.apps wildcard configured properly.
const AppsSubDomainNameHostDNSValidation = "console-openshift-console"
const APIName = "api"
const APIInternalName = "api-int"

//Non existing domain name under clusterName.baseDomain for wildcard configuration check
const DNSWildcardFalseDomainName = "validateNoWildcardDNS"
