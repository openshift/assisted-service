package controllers

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"strings"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	nodeUser       = "system:node"
	nodeGroup      = "system:nodes"
	nodeUserPrefix = nodeUser + ":"
)

func isNodeReady(node *corev1.Node) bool {
	if node != nil {
		for _, c := range node.Status.Conditions {
			if c.Status == corev1.ConditionTrue && c.Type == corev1.NodeReady {
				return true
			}
		}
	}
	return false
}

func getAgentHostname(agent *aiv1beta1.Agent) string {
	if agent.Spec.Hostname != "" {
		return agent.Spec.Hostname
	}
	return agent.Status.Inventory.Hostname
}

func getNodeIPs(node *corev1.Node) (ret []string) {
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalIP, corev1.NodeExternalIP:
			ip := net.ParseIP(addr.Address)
			if ip != nil {
				ret = append(ret, addr.Address)
			}
		}
	}
	return
}

func getNodeDNSNames(node *corev1.Node) (ret []string) {
	for _, addr := range node.Status.Addresses {
		switch addr.Type {
		case corev1.NodeInternalDNS, corev1.NodeExternalDNS, corev1.NodeHostName:
			ret = append(ret, addr.Address)
		default:
		}
	}
	return
}

func getX509ParsedRequest(csr *certificatesv1.CertificateSigningRequest) (*x509.CertificateRequest, error) {
	decodedCSR, _ := pem.Decode(csr.Spec.Request)
	if decodedCSR == nil {
		return nil, errors.Errorf("Failed to decode request of CSR %s", csr.Name)
	}
	cr, err := x509.ParseCertificateRequest(decodedCSR.Bytes)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse CSR %s", csr.Name)
	}
	return cr, nil
}

func hasExactUsages(csr *certificatesv1.CertificateSigningRequest, usages []certificatesv1.KeyUsage) bool {
	if len(usages) != len(csr.Spec.Usages) {
		return false
	}

	usageMap := map[certificatesv1.KeyUsage]struct{}{}
	for _, u := range usages {
		usageMap[u] = struct{}{}
	}

	for _, u := range csr.Spec.Usages {
		if _, ok := usageMap[u]; !ok {
			return false
		}
	}

	return true
}

func isReqFromNodeBootstrapper(req *certificatesv1.CertificateSigningRequest) bool {
	nodeBootstrapperUsername := "system:serviceaccount:openshift-machine-config-operator:node-bootstrapper"
	nodeBootstrapperGroups := sets.NewString(
		"system:serviceaccounts:openshift-machine-config-operator",
		"system:serviceaccounts",
		"system:authenticated",
	)

	return req.Spec.Username == nodeBootstrapperUsername && nodeBootstrapperGroups.Equal(sets.NewString(req.Spec.Groups...))
}

// Type representing a function that validates CSR for client or server.
type nodeCsrValidator func(agent *aiv1beta1.Agent, csr *certificatesv1.CertificateSigningRequest, x509cr *x509.CertificateRequest) (bool, error)

// Recognize if the CSR is a client CSR, that is sent to allow the node to join the cluster
func validateNodeClientCSR(agent *aiv1beta1.Agent, csr *certificatesv1.CertificateSigningRequest, x509cr *x509.CertificateRequest) (bool, error) {

	// Organization must contain signle element "system:nodes"
	if len(x509cr.Subject.Organization) != 1 || x509cr.Subject.Organization[0] != nodeGroup {
		return false, errors.Errorf("CSR %s agent %s/%s: Organization %v does not contain single element %s", csr.Name, agent.Namespace, agent.Name,
			x509cr.Subject.Organization, nodeGroup)
	}

	// Since it is client CSR, DNSNames/EmailAddresses/IPAddresses must be empty
	if (len(x509cr.DNSNames) > 0) || (len(x509cr.EmailAddresses) > 0) || (len(x509cr.IPAddresses) > 0) {
		return false, errors.Errorf("CSR %s agent %s/%s: DNS names %v Email addresses %v IPAddresses %v are not empty", csr.Name, agent.Namespace, agent.Name,
			x509cr.DNSNames, x509cr.EmailAddresses, x509cr.IPAddresses)
	}

	kubeletClientUsagesLegacy := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	kubeletClientUsages := []certificatesv1.KeyUsage{
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	if !hasExactUsages(csr, kubeletClientUsages) && !hasExactUsages(csr, kubeletClientUsagesLegacy) {
		return false, errors.Errorf("CSR %s agent %s/%s: No exact match between CSR %v and required usages", csr.Name, agent.Namespace, agent.Name,
			csr.Spec.Usages)
	}

	// CN must have prefix ""system:node:"
	if !strings.HasPrefix(x509cr.Subject.CommonName, nodeUserPrefix) {
		return false, errors.Errorf("CSR %s agent %s/%s: Common name %s does not have prefix %s", csr.Name, agent.Namespace, agent.Name,
			x509cr.Subject.CommonName, nodeUserPrefix)
	}

	// This CSR must be from node bootstrapper
	if !isReqFromNodeBootstrapper(csr) {
		return false, errors.Errorf("CSR %s agent %s/%s: Not from bootstrapper", csr.Name, agent.Namespace, agent.Name)
	}
	return true, nil
}

// Validate that server CSR can be approved.  Server CSR can be approved after node joins the cluster  and alternative names are matched
func validateNodeServerCSR(agent *aiv1beta1.Agent, node *corev1.Node, csr *certificatesv1.CertificateSigningRequest, x509CSR *x509.CertificateRequest) (bool, error) {

	// Username must consist from ""system:node:" and the node name
	nodeAsking := strings.TrimPrefix(csr.Spec.Username, nodeUserPrefix)
	if len(nodeAsking) == 0 {
		return false, errors.Errorf("CSR %s agent %s/%s: CSR does not appear to be a node serving CSR. Empty node name after %s prefix", csr.Name, agent.Namespace, agent.Name,
			nodeUserPrefix)
	}

	// Check groups, we need at least:
	// - system:authenticated
	if len(csr.Spec.Groups) < 2 || !funk.ContainsString(csr.Spec.Groups, "system:authenticated") {
		return false, errors.Errorf("CSR %s agent %s/%s: %v is too small or not contains %q", csr.Name, agent.Namespace, agent.Name,
			csr.Spec.Groups, "system:authenticated")
	}

	serverUsagesLegacy := []certificatesv1.KeyUsage{
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageServerAuth,
	}
	serverUsages := []certificatesv1.KeyUsage{
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageServerAuth,
	}

	if !hasExactUsages(csr, serverUsages) && !hasExactUsages(csr, serverUsagesLegacy) {
		return false, errors.Errorf("CSR %s agent %s/%s: No exact match between CSR %v and required usages", csr.Name, agent.Namespace, agent.Name,
			csr.Spec.Usages)
	}

	// "system:nodes" must be one of the elements of Organization
	if !funk.ContainsString(x509CSR.Subject.Organization, nodeGroup) {
		return false, errors.Errorf("CSR %s agent %s/%s: Organization %v doesn't include %s", csr.Name, agent.Namespace, agent.Name,
			x509CSR.Subject.Organization, nodeGroup)
	}

	// CN and Username must be equal
	if x509CSR.Subject.CommonName != csr.Spec.Username {
		return false, errors.Errorf("CSR %s agent %s/%s: Mismatched CommonName %s != %s for CSR %s", csr.Name, agent.Namespace, agent.Name,
			x509CSR.Subject.CommonName, csr.Spec.Username, csr.Name)
	}
	nodeDNSNames := append(getNodeDNSNames(node), node.Name)

	// Any DNS name in CSR must exist in the nodeDNSNames
	// TODO: May need to modify for IPv6 only node
	for _, dnsName := range x509CSR.DNSNames {
		if !funk.ContainsString(nodeDNSNames, dnsName) {
			return false, errors.Errorf("CSR %s agent %s/%s: DNS name %s missing from available DNS names %v", csr.Name, agent.Namespace, agent.Name,
				dnsName, nodeDNSNames)
		}
	}
	nodeIPs := getNodeIPs(node)
	// Any IP address in CSR must exist in nodeIPs
	for _, ip := range x509CSR.IPAddresses {
		if !funk.ContainsString(nodeIPs, ip.String()) {
			return false, errors.Errorf("CSR %s agent %s/%s: IP address %s missing from available node IPs %v", csr.Name, agent.Namespace, agent.Name,
				ip.String(), nodeIPs)
		}
	}
	return true, nil
}

func createNodeServerCsrValidator(node *corev1.Node) nodeCsrValidator {
	return func(agent *aiv1beta1.Agent, csr *certificatesv1.CertificateSigningRequest, x509cr *x509.CertificateRequest) (bool, error) {
		return validateNodeServerCSR(agent, node, csr, x509cr)
	}
}

// If the concatenation of ["system:node:", agent's host name] is equal the the CSR CN, the CSR is associated with the agent
// TODO: Verify IPV6 CN to follow this condition
func isCsrAssociatedWithAgent(x509CSR *x509.CertificateRequest, agent *aiv1beta1.Agent) bool {
	return strings.EqualFold(nodeUserPrefix+getAgentHostname(agent), x509CSR.Subject.CommonName)
}

func isCsrApproved(csr *certificatesv1.CertificateSigningRequest) bool {
	for _, c := range csr.Status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
	}
	return false
}
