package common

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/containers/image/v5/docker/reference"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	yamlpatch "github.com/krishicks/yaml-patch"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	EnvConfigPrefix = "myapp"

	MinMasterHostsNeededForInstallation    = 3
	AllowedNumberOfMasterHostsInNoneHaMode = 1
	AllowedNumberOfWorkersInNoneHaMode     = 0

	HostCACertPath = "/etc/assisted-service/service-ca-cert.crt"

	AdditionalTrustBundlePath = "/etc/pki/ca-trust/source/anchors/assisted-infraenv-additional-trust-bundle.pem"

	consoleUrlPrefix = "https://console-openshift-console.apps"

	MirrorRegistriesCertificateFile = "tls-ca-bundle.pem"
	MirrorRegistriesCertificatePath = "/etc/pki/ca-trust/extracted/pem/" + MirrorRegistriesCertificateFile
	MirrorRegistriesConfigDir       = "/etc/containers"
	MirrorRegistriesConfigFile      = "registries.conf"
	MirrorRegistriesConfigPath      = MirrorRegistriesConfigDir + "/" + MirrorRegistriesConfigFile
	MaximumAllowedTimeDiffMinutes   = 4

	IgnitionTokenKeyInSecret = "ignition-token"

	FamilyIPv4 int32 = 4
	FamilyIPv6 int32 = 6

	AMD64CPUArchitecture   = "amd64"
	X86CPUArchitecture     = "x86_64"
	DefaultCPUArchitecture = X86CPUArchitecture
	ARM64CPUArchitecture   = "arm64"
	// rchos is sending aarch64 and not arm as arm64 arch
	AARCH64CPUArchitecture = "aarch64"
	PowerCPUArchitecture   = "ppc64le"
	S390xCPUArchitecture   = "s390x"
	MultiCPUArchitecture   = "multi"
)

// Configuration to be injected by discovery ignition.  It will cause IPv6 DHCP client identifier to be the same
// after reboot.  This will cause the DHCP server to provide the same IP address after reboot.
const Ipv6DuidDiscoveryConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
`

// Configuration to be used by MCO manifest to get consistent IPv6 DHCP client identification.
const Ipv6DuidRuntimeConfPre410 = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
[keyfile]
path=/etc/NetworkManager/system-connections-merged
`

// Configuration of consistent IPv6 DHCP without the keyfile path set. This is because starting in
// RHCOS 4.10 it is breaking the Network Manager configuration.
const Ipv6DuidRuntimeConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
`

// configuration of NM to disable handling of /etc/resolv.conf
// used for configuration of bootstrap node during bootkube (before reboot)
// and of masters after reboot
const UnmanagedResolvConf = `
[main]
rc-manager=unmanaged
`

// NM configuration to be activated (set into discovery ignition) in case we want more logging for NM debugging purposes.
// This content needs to be set in the /etc/NetworkManager/conf.d/95-nm-debug.conf
// In addition, the line RateLimitBurst=0 must be uncommented in the /etc/systemd/journald.conf and systemctl restart systemd-journald run.
const NMDebugModeConf = `
[logging]
domains=ALL:DEBUG
`

func GetIgnoredValidations(validationsJSON string, clusterID string) ([]string, bool) {
	ignoredValidations := []string{}
	if validationsJSON != "" {
		var err error
		ignoredValidations, err = DeserializeJSONList(validationsJSON)
		if err != nil {
			return nil, false
		}
	}
	return ignoredValidations, true
}

func IgnoredValidationsAreSet(cluster *Cluster) bool {
	return cluster.IgnoredClusterValidations != "" || cluster.IgnoredHostValidations != ""
}

func DeserializeJSONList(jsonString string) ([]string, error) {
	var list []string
	if jsonString != "" {
		err := json.Unmarshal([]byte(jsonString), &list)
		if err != nil {
			return nil, err
		}
	}
	return list, nil
}

func NormalizeCPUArchitecture(arch string) string {
	switch arch {
	case AMD64CPUArchitecture:
		return X86CPUArchitecture
	case AARCH64CPUArchitecture:
		return ARM64CPUArchitecture
	default:
		return arch
	}
}

func AllStrings(vs []string, f func(string) bool) bool {
	for _, v := range vs {
		if !f(v) {
			return false
		}
	}
	return true
}

// GetBootstrapHost return host that was set as bootstrap
func GetBootstrapHost(cluster *Cluster) *models.Host {
	for _, host := range cluster.Hosts {
		if host.Bootstrap {
			return host
		}
	}
	return nil
}

func IsSingleNodeCluster(cluster *Cluster) bool {
	return swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
}

func IsDay2Cluster(cluster *Cluster) bool {
	return swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster
}

func IsImportedCluster(cluster *Cluster) bool {
	return swag.BoolValue(cluster.Imported)
}

func AreMastersSchedulable(cluster *Cluster) bool {
	return swag.BoolValue(cluster.SchedulableMastersForcedTrue) || swag.BoolValue(cluster.SchedulableMasters)
}

func GetEffectiveRole(host *models.Host) models.HostRole {
	if host.Role == models.HostRoleAutoAssign && host.SuggestedRole != "" {
		return host.SuggestedRole
	}
	return host.Role
}

func GetConsoleUrl(clusterName, baseDomain string) string {
	return fmt.Sprintf("%s.%s.%s", consoleUrlPrefix, clusterName, baseDomain)
}

func IsNtpSynced(c *Cluster) (bool, error) {
	var min int64
	var max int64
	for _, h := range c.Hosts {
		if *h.Status == models.HostStatusDisconnected ||
			*h.Status == models.HostStatusResettingPendingUserAction ||
			*h.Status == models.HostStatusDiscovering ||
			h.Timestamp == 0 {
			continue
		}
		if h.Timestamp < min || min == 0 {
			min = h.Timestamp
		}
		if h.Timestamp > max {
			max = h.Timestamp
		}
	}
	return (max-min)/60 <= MaximumAllowedTimeDiffMinutes, nil
}

func GetProxyConfigs(proxy *models.Proxy) (string, string, string) {
	if proxy == nil {
		return "", "", ""
	}
	return swag.StringValue(proxy.HTTPProxy), swag.StringValue(proxy.HTTPSProxy), swag.StringValue(proxy.NoProxy)
}

func GetNetworksCidrs(obj interface{}) []*string {
	addresses := make([]*string, 0)

	addresses = append(addresses, GetNetworkCidrAttr(obj, "ClusterNetworks")...)
	addresses = append(addresses, GetNetworkCidrAttr(obj, "ServiceNetworks")...)
	addresses = append(addresses, GetNetworkCidrAttr(obj, "MachineNetworks")...)

	return addresses
}

func GetNetworkCidrAttr(obj interface{}, fieldName string) []*string {
	addresses := make([]*string, 0)

	field := funk.Get(obj, fieldName)
	if field == nil {
		return addresses
	}

	funk.ForEach(field, func(elem interface{}) {
		address := funk.Get(elem, "Cidr")
		if address != nil {
			addresses = append(addresses, swag.String(string(address.(models.Subnet))))
		}
	})

	return addresses
}

// IsSliceNonEmpty checks whether the provided slice is non-empty. The slice is assumed to be
// non-empty if at least one of its elements contains a non-zero value for its respective type.
// Examples:
//   - `[]*models.MachineNetwork{{Cidr: "5.5.0.0/24"}, {Cidr: "6.6.0.0/24"}}` - valid, as we are
//     configuring two machine networks
//   - `[]*models.ClusterNetwork{}` - valid, as it means we are removing all the cluster networks
//     that may have been currently configured
//   - `[]*models.MachineNetwork{{}}` - invalid, as it means that we are trying to configure
//     a single machine network that is empty; a valid network contains at least a CIDR which is
//     missing in this case
//   - `[]*models.MachineNetwork{{Cidr: ""}}` - invalid, as it means we are trying to configure
//     a single machine network that has empty CIDR; a valid network should contain a non-empty
//     CIDR
//   - `[]*models.ClusterNetwork{{HostPrefix: 0}}` - invalid, as it means we are trying to configure
//     a single cluster network with host prefix with a value 0; this is not a valid subnet lenght
func IsSliceNonEmpty(arg interface{}) bool {
	res := false
	if reflect.ValueOf(arg).Kind() == reflect.Slice {
		funk.ForEach(arg, func(elem interface{}) {
			v := reflect.ValueOf(elem)
			if v.Kind() == reflect.Ptr {
				v = v.Elem()
			}
			for i := 0; i < v.NumField(); i++ {
				res = res || !v.Field(i).IsZero()
			}
		})
	}
	return res
}

func getUnifiedNTPSources(db *gorm.DB, clusterID strfmt.UUID) (string, error) {
	var sources []string
	err := db.Raw("select distinct(additional_ntp_sources) as sources from infra_envs where id in (select distinct(infra_env_id) from hosts where cluster_id = ?)", clusterID.String()).Pluck("sources", &sources).Error
	if err != nil {
		return "", err
	}
	combinedSources := make(map[string]bool)
	for _, singleSource := range sources {
		if singleSource != "" {
			for _, s := range strings.Split(singleSource, ",") {
				combinedSources[s] = true
			}
		}
	}
	var ret []string
	for k := range combinedSources {
		ret = append(ret, k)
	}
	return strings.Join(ret, ","), nil
}

func GetClusterNTPSources(db *gorm.DB, clusterID strfmt.UUID) (string, error) {
	cluster, err := GetClusterFromDB(db, clusterID, SkipEagerLoading)
	if err != nil {
		return "", err
	}
	if cluster.AdditionalNtpSource != "" {
		return cluster.AdditionalNtpSource, nil
	} else {
		return getUnifiedNTPSources(db, clusterID)
	}
}

func GetHostNTPSources(db *gorm.DB, host *models.Host) (string, error) {
	var (
		infraEnv *InfraEnv
		err      error
	)
	if host.ClusterID != nil {
		return GetClusterNTPSources(db, *host.ClusterID)
	}
	infraEnv, err = GetInfraEnvFromDB(db, host.InfraEnvID)
	if err != nil {
		return "", err
	}
	return infraEnv.AdditionalNtpSources, nil
}

// GetHostsByRole returns the list of hosts with the required role.
func GetHostsByRole(cluster *Cluster, role models.HostRole) []models.Host {
	var hosts []models.Host
	if cluster == nil {
		return hosts
	}
	for _, host := range cluster.Hosts {
		if GetEffectiveRole(host) == role {
			hosts = append(hosts, *host)
		}
	}
	return hosts
}

// ParsePemCerts returns a list of certificate objects extracted from a byte array
// The certificates within the byte array must be PEM encoded
func ParsePemCerts(pemCerts []byte) ([]x509.Certificate, bool) {
	var certs []x509.Certificate
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			return certs, true
		}
		if block.Type == "CERTIFICATE" && len(block.Headers) == 0 {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, false
			}
			certs = append(certs, *cert)
		}
	}
	return certs, true
}

// VerifyCaBundle verifies the full CA Chain contained in a byte array.
// The CA certs within the byte array must be PEM encoded
// starting with the leaf CA certificate followed by the
// subsequents intermediate CAs and ending with the root CA
func VerifyCaBundle(pemCerts []byte) error {
	var certs []x509.Certificate
	var ok bool
	certs, ok = ParsePemCerts(pemCerts)
	if !ok {
		return errors.New("unable to parse the CA Bundle")
	}
	if len(certs) == 0 {
		return errors.New("the CA bundle does not contain any PEM certificate")
	}
	rootCertPool := x509.NewCertPool()
	intermediateCertPool := x509.NewCertPool()

	// From all the certificates in the bundle we are building a store of Roots and Intermediaries
	// that we will be using later for the verification
	for i := range certs {
		if certs[i].Issuer.CommonName == certs[i].Subject.CommonName {
			rootCertPool.AddCert(&certs[i])
		} else {
			intermediateCertPool.AddCert(&certs[i])
		}
	}
	verifyOptions := x509.VerifyOptions{
		Roots:         rootCertPool,
		Intermediates: intermediateCertPool,
	}

	// For all the certificates provided we are performing a validation based on the store
	// that we have built above. In order for the bundle to be correct we need every certificate
	// to validate against a Root present in the bundle.
	for i := range certs {
		if _, err := certs[i].Verify(verifyOptions); err != nil {
			return errors.New("unable to verify the full CA Chain")
		}
	}

	return nil
}

func CanonizeStrings(slice []string) (ret []string) {
	if len(slice) == 0 {
		return
	}
	ret = append(ret, slice[0])
	for i := 1; i != len(slice); i++ {
		if slice[i] != slice[i-1] {
			ret = append(ret, slice[i])
		}
	}
	return
}

func GetHostKey(host *models.Host) string {
	return host.ID.String() + "@" + host.InfraEnvID.String()
}

func GetInventoryInterfaces(inventory string) (string, error) {
	startIndex := strings.Index(inventory, "\"interfaces\":")
	interfacesLocation := startIndex + len(`"interfaces:"`)
	if (startIndex) == -1 {
		return "", errors.New("unable to find interfaces in the inventory")
	}

	endIndex := strings.Index(inventory[interfacesLocation:], "}],")
	if (endIndex) == -1 {
		return "", errors.New("inventory is malformed")
	}

	endLocation := endIndex + len("}]")
	return inventory[interfacesLocation : interfacesLocation+endLocation], nil
}

// GetTagFromImageRef returns the tag of the given container image reference. For example, if the
// image reference is 'quay.io/my/image:latest' then the result will be 'latest'. If the image
// reference isn't valid or doesn't contain a tag then the result will be an empty string.
func GetTagFromImageRef(ref string) string {
	parsed, err := reference.ParseNamed(ref)
	if err != nil {
		return ""
	}
	switch typed := parsed.(type) {
	case reference.Tagged:
		return typed.Tag()
	default:
		return ""
	}
}

func GetConvertedClusterAPIVipDNSName(c *Cluster) string {
	// In case cluster that isn't configured with user-managed-networking
	// and api vip is set we should set api vip as APIVipDNSName
	if !swag.BoolValue(c.Cluster.UserManagedNetworking) && c.Cluster.APIVip != "" {
		return c.Cluster.APIVip
	}
	return fmt.Sprintf("api.%s.%s", c.Cluster.Name, c.Cluster.BaseDNSDomain)
}

func GetAPIHostname(c *Cluster) string {
	// Despite the confusing name of this parameter, in day-2 scenarios where
	// this function is used it could either be a DNS domain name that points
	// at the API's IP address (this is the default) or it could also not be a
	// DNS domain name at all - such as when the user chooses to override this
	// parameter with an IP address. Such override is commonly done by users in
	// day-2 SaaS imported clusters where the user never bothered to set up DNS
	// for their day-1 cluster in the first place and just wants the worker to
	// connect to the API directly. The UI even has a special dialog to help users
	// do that.
	return swag.StringValue(c.APIVipDNSName)
}

func ApplyYamlPatch(src []byte, ops []byte) ([]byte, error) {
	patch, err := yamlpatch.DecodePatch(ops)
	if err != nil {
		return []byte{}, err
	}

	patched, err := patch.Apply(src)
	if err != nil {
		return []byte{}, err
	}

	return patched, nil
}
