package validations

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/containers/image/v5/docker/reference"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-multierror"
	"github.com/openshift/assisted-image-service/pkg/isoeditor"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/tang"
	"github.com/openshift/assisted-service/pkg/validations"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"golang.org/x/crypto/ssh"
)

type Config struct {
	PublicRegistries string `envconfig:"PUBLIC_CONTAINER_REGISTRIES" default:""`
}

const (
	clusterNameRegex                = "^([a-z0-9]([-a-z0-9]*[a-z0-9])?)*$"
	clusterNameRegexForNonePlatform = "^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)*$"
	CloudOpenShiftCom               = "cloud.openshift.com"
	sshPublicKeyRegex               = "^(ssh-rsa AAAAB3NzaC1yc2|ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNT|ecdsa-sha2-nistp384 AAAAE2VjZHNhLXNoYTItbmlzdHAzODQAAAAIbmlzdHAzOD|ecdsa-sha2-nistp521 AAAAE2VjZHNhLXNoYTItbmlzdHA1MjEAAAAIbmlzdHA1Mj|ssh-ed25519 AAAAC3NzaC1lZDI1NTE5|ssh-dss AAAAB3NzaC1kc3)[0-9A-Za-z+/]+[=]{0,3}( .*)?$"
	dockerHubRegistry               = "docker.io"
	dockerHubLegacyAuth             = "https://index.docker.io/v1/"
	stageRegistry                   = "registry.stage.redhat.io"
	ignoreListSeparator             = ","

	// Size of the file used to embed an ignition config archive within an RHCOS ISO: 256 KiB
	// See: https://github.com/coreos/coreos-assembler/blob/d2c968a1f3c75713a4e1449e3da657c5d5a5d7e7/src/cmd-buildextend-live#L113-L114
	IgnitionImageSizePadding = 256 * 1024
)

var regexpSshPublicKey *regexp.Regexp

func init() {
	regexpSshPublicKey, _ = regexp.Compile(sshPublicKeyRegex)
}

// PullSecretValidator is used run validations on a provided pull secret
// it verifies the format of the pull secrete and access to required image registries
//
//go:generate mockgen -source=validations.go -package=validations -destination=mock_validations.go
type PullSecretValidator interface {
	ValidatePullSecret(secret string, username string) error
}

type registryPullSecretValidator struct {
	registriesWithAuth *map[string]bool
	authHandler        auth.Authenticator
}

type imagePullSecret struct {
	Auths map[string]map[string]interface{} `json:"auths"`
}

type PullSecretCreds struct {
	Username string
	Password string
	Registry string
	AuthRaw  string
	Email    string
}

// PullSecretError distinguishes secret validation errors produced by this package from other types of errors
type PullSecretError struct {
	Msg   string
	Cause error
}

func (e *PullSecretError) Error() string {
	return e.Msg
}

func (e *PullSecretError) Unwrap() error {
	return e.Cause
}

// ParsePullSecret validates the format of a pull secret and converts the secret string into individual credentail entries
func ParsePullSecret(secret string) (map[string]PullSecretCreds, error) {
	result := make(map[string]PullSecretCreds)
	var s imagePullSecret

	err := json.Unmarshal([]byte(strings.TrimSpace(secret)), &s)
	if err != nil {
		return nil, &PullSecretError{Msg: "pull secret must be a well-formed JSON", Cause: err}
	}

	if len(s.Auths) == 0 {
		return nil, &PullSecretError{Msg: "pull secret must contain 'auths' JSON-object field"}
	}

	for d, a := range s.Auths {

		_, authPresent := a["auth"]
		_, credsStorePresent := a["credsStore"]
		if !authPresent && !credsStorePresent {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: %q JSON-object requires either 'auth' or 'credsStore' field", d)}
		}

		data, err := base64.StdEncoding.DecodeString(a["auth"].(string))
		if err != nil {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: 'auth' fields of %q are not base64-encoded", d)}
		}

		res := bytes.Split(data, []byte(":"))
		if len(res) != 2 {
			return nil, &PullSecretError{Msg: fmt.Sprintf("invalid pull secret: 'auth' for %s is not in 'user:password' format", d)}
		}

		var email string
		_, emailExists := a["email"]
		if emailExists {
			email = a["email"].(string)
		}

		result[d] = PullSecretCreds{
			Password: string(res[1]),
			Username: string(res[0]),
			AuthRaw:  a["auth"].(string),
			Registry: d,
			Email:    email,
		}

	}
	return result, nil
}

func AddRHRegPullSecret(secret, rhCred string) (string, error) {
	if rhCred == "" {
		return "", errors.Errorf("invalid pull secret")
	}
	var s imagePullSecret
	err := json.Unmarshal([]byte(strings.TrimSpace(secret)), &s)
	if err != nil {
		return secret, errors.Errorf("invalid pull secret: %v", err)
	}
	s.Auths[stageRegistry] = make(map[string]interface{})
	s.Auths[stageRegistry]["auth"] = base64.StdEncoding.EncodeToString([]byte(rhCred))
	ps, err := json.Marshal(s)
	if err != nil {
		return secret, err
	}
	return string(ps), nil
}

// NewPullSecretValidator receives all images whose registries must have an entry in a user pull secret (auth)
func NewPullSecretValidator(config Config, authHandler auth.Authenticator, images ...string) (PullSecretValidator, error) {

	authRegList, err := getRegistriesWithAuth(config.PublicRegistries, ignoreListSeparator, images...)
	if err != nil {
		return nil, err
	}

	return &registryPullSecretValidator{
		registriesWithAuth: authRegList,
		authHandler:        authHandler,
	}, nil
}

// ValidatePullSecret validates that a pull secret is well formed and contains all required data
func (v *registryPullSecretValidator) ValidatePullSecret(secret string, username string) error {
	creds, err := ParsePullSecret(secret)
	if err != nil {
		return err
	}

	// only check for cloud creds if we're authenticating against Red Hat SSO
	if v.authHandler.AuthType() == auth.TypeRHSSO {

		r, ok := creds["cloud.openshift.com"]
		if !ok {
			return &PullSecretError{Msg: "pull secret must contain auth for \"cloud.openshift.com\""}
		}

		user, err := v.authHandler.AuthAgentAuth(r.AuthRaw)
		if err != nil {
			return &PullSecretError{Msg: "failed to authenticate the pull secret token"}
		}

		if (user.(*ocm.AuthPayload)).Username != username {
			return &PullSecretError{Msg: "pull secret token does not match current user"}
		}
	}

	for registry := range *v.registriesWithAuth {

		// Both "docker.io" and "https://index.docker.io/v1/" are acceptable for DockerHub login
		if registry == dockerHubRegistry {
			if _, ok := creds[dockerHubLegacyAuth]; ok {
				continue
			}
		}

		// We add auth for stage registry automatically
		if registry == stageRegistry {
			continue
		}

		if _, ok := creds[registry]; !ok {
			return &PullSecretError{Msg: fmt.Sprintf("pull secret must contain auth for %q", registry)}
		}
	}

	return nil
}

// ValidateClusterNameFormat validates specified cluster name format
func ValidateClusterNameFormat(name string, platform string) error {
	regex := clusterNameRegex
	if platform == string(models.PlatformTypeNone) {
		regex = clusterNameRegexForNonePlatform
	}
	if matched, _ := regexp.MatchString(regex, name); !matched {
		return errors.Errorf("Cluster name format is not valid: '%s'. "+
			"Name must start and end with either a letter or number and "+
			"consist of lower-case letters, numbers, and hyphens. "+
			"Subdomains in cluster names are only valid with %s platform.",
			name, models.PlatformTypeNone)
	}
	return nil
}

// ValidateNoProxyFormat validates the no-proxy format which should be a comma-separated list
// of destination domain names, domains, IP addresses or other network CIDRs. A domain can be
// prefaced with '.' to include all subdomains of that domain.
// Use '*' to bypass proxy for all destinations in OCP 4.8 or later.
func ValidateNoProxyFormat(noProxy string, ocpVersion string) error {
	// TODO MGMT-11401: Remove noProxy wildcard validation when OCP 4.8 gets deprecated.
	if strings.Contains(noProxy, "*") {
		if ocpVersion == "" { // a case where ValidateNoProxyFormat got called for InfraEnv
			return nil
		}
		if wildcardSupported, err := common.VersionGreaterOrEqual(ocpVersion, "4.8.0-fc.4"); err != nil {
			return err
		} else if wildcardSupported {
			return nil
		}
		return errors.Errorf("Sorry, no-proxy value '*' is not supported in release: %s", ocpVersion)
	}

	return validations.ValidateNoProxyFormat(noProxy)
}

func ValidateSSHPublicKey(sshPublicKeys string) error {
	if regexpSshPublicKey == nil {
		return fmt.Errorf("Can't parse SSH keys.")
	}

	for _, sshPublicKey := range strings.Split(sshPublicKeys, "\n") {
		sshPublicKey = strings.TrimSpace(sshPublicKey)
		keyBytes := []byte(sshPublicKey)
		isMatched := regexpSshPublicKey.Match(keyBytes)
		if !isMatched {
			return errors.Errorf(
				"SSH key: %s does not match any supported type: ssh-rsa, ssh-ed25519, ecdsa-[VARIANT]",
				sshPublicKey)
		} else if _, _, _, _, err := ssh.ParseAuthorizedKey(keyBytes); err != nil {
			return errors.Wrapf(err, fmt.Sprintf("Malformed SSH key: %s", sshPublicKey))
		}
	}

	return nil
}

func ValidatePEMCertificateBundle(bundle string) error {
	// From https://github.com/openshift/installer/blob/56e61f1df5aa51ff244465d4bebcd1649003b0c9/pkg/validate/validate.go#L29-L47
	rest := []byte(bundle)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return errors.Errorf("invalid block")
		}
		_, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("parse failed: %w", err)
		}
		if len(rest) == 0 {
			break
		}
	}
	return nil
}

// ParseRegistry extracts the registry from a full image name, or returns
// the default if the name does not start with a registry.
func ParseRegistry(image string) (string, error) {
	parsed, err := reference.ParseNormalizedNamed(strings.TrimSpace(image))
	if err != nil {
		return "", err
	}
	return reference.Domain(parsed), nil
}

// getRegistriesWithAuth returns container registries that may require authentication based
// on a list of used images and an ignore list. The ingore list comes as a string and a separator
// to make it easier to read from a configuration variable
func getRegistriesWithAuth(ignoreList string, ignoreSeparator string, images ...string) (*map[string]bool, error) {

	ignored := make(map[string]bool)
	for _, i := range strings.Split(ignoreList, ignoreSeparator) {
		ignored[i] = true
	}

	_, docLegacyIgnored := ignored[dockerHubLegacyAuth]

	registries := make(map[string]bool)
	for _, img := range images {
		if img == "" {
			continue
		}
		r, err := ParseRegistry(img)
		if err != nil {
			return &registries, err
		}

		if r == dockerHubRegistry && docLegacyIgnored {
			continue
		}

		if _, ok := ignored[r]; ok {
			continue
		}

		registries[r] = true
	}

	return &registries, nil
}

// ValidateVipDHCPAllocationWithIPv6 returns an error in case of VIP DHCP allocation
// being used with IPv6 machine network
func ValidateVipDHCPAllocationWithIPv6(vipDhcpAllocation bool, machineNetworkCIDR string) error {
	if !vipDhcpAllocation {
		return nil
	}
	if network.IsIPv6CIDR(machineNetworkCIDR) {
		return errors.Errorf("VIP DHCP allocation is unsupported with IPv6 network %s", machineNetworkCIDR)
	}
	return nil
}

func HandleApiVipBackwardsCompatibility(clusterId strfmt.UUID, apiVip string, apiVips []*models.APIVip) ([]*models.APIVip, error) {
	// APIVip provided, but APIVips were not.
	if apiVip != "" && len(apiVips) == 0 {
		return []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterId}}, nil
	}
	// Both APIVip and APIVips were provided.
	if apiVip != "" && len(apiVips) > 0 && apiVip != string(apiVips[0].IP) {
		return nil, errors.New("apiVIP must be the same as the first element of apiVIPs")
	}
	// APIVips were provided, but APIVip was not.
	if apiVip == "" && apiVips != nil && len(apiVips) > 0 {
		return nil, errors.New("request must include apiVIP alongside apiVIPs")
	}
	return apiVips, nil
}

func handleIngressVipUpdateBackwardsCompatibility(cluster *common.Cluster, params *models.V2ClusterUpdateParams) error {
	if cluster.IngressVip != "" {
		// IngressVip was cleared and IngressVips were not provided, clear both fields.
		if swag.StringValue(params.IngressVip) == "" && len(params.IngressVips) == 0 {
			params.IngressVip = nil
			params.IngressVips = nil
		}
		// IngressVip was changed (but not cleared), IngressVips will be forcefully set to the value of IngressVips as a one-element list.
		if params.IngressVip != nil && swag.StringValue(params.IngressVip) != "" && swag.StringValue(params.IngressVip) != cluster.IngressVip {
			if err := validateIngressVipAddressesInput(params.IngressVips); err != nil {
				return err
			}
			params.IngressVips = []*models.IngressVip{{IP: models.IP(swag.StringValue(params.IngressVip)), ClusterID: *cluster.ID}}
		}
	}
	return nil
}

func handleApiVipUpdateBackwardsCompatibility(cluster *common.Cluster, params *models.V2ClusterUpdateParams) error {
	if cluster.APIVip != "" {
		// APIVip was cleared and APIVips were not provided, clear both fields.
		if swag.StringValue(params.APIVip) == "" && len(params.APIVips) == 0 {
			params.APIVip = nil
			params.APIVips = nil
		}
		// APIVip was changed (but not cleared), APIVips will be forcefully set to the value of APIVip as a one-element list.
		if params.APIVip != nil && swag.StringValue(params.APIVip) != "" && swag.StringValue(params.APIVip) != cluster.APIVip {
			if err := validateApiVipAddressesInput(params.APIVips); err != nil {
				return err
			}
			params.APIVips = []*models.APIVip{{IP: models.IP(swag.StringValue(params.APIVip)), ClusterID: *cluster.ID}}
		}
	}
	return nil
}

func HandleIngressVipBackwardsCompatibility(clusterId strfmt.UUID, ingressVip string, ingressVips []*models.IngressVip) ([]*models.IngressVip, error) {
	// IngressVip provided, but IngressVips were not.
	if ingressVip != "" && len(ingressVips) == 0 {
		return []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterId}}, nil
	}
	// Both IngressVip and IngressVips were provided.
	if ingressVip != "" && len(ingressVips) > 0 && ingressVip != string(ingressVips[0].IP) {
		return nil, errors.New("ingressVIP must be the same as the first element of ingressVIPs")
	}
	// IngressVips were provided, but IngressVip was not.
	if ingressVip == "" && ingressVips != nil && len(ingressVips) > 0 {
		return nil, errors.New("request must include ingressVIP alongside ingressVIPs")
	}
	return ingressVips, nil
}

func ValidateClusterCreateIPAddresses(ipV6Supported bool, clusterId strfmt.UUID, params *models.ClusterCreateParams) error {
	var err error
	targetConfiguration := common.Cluster{}

	// Backwards compatibility: An old client is used and it can't send fields it doesn't know about.
	params.APIVips, err = HandleApiVipBackwardsCompatibility(clusterId, params.APIVip, params.APIVips)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	params.IngressVips, err = HandleIngressVipBackwardsCompatibility(clusterId, params.IngressVip, params.IngressVips)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if (len(params.APIVips) > 1 || len(params.IngressVips) > 1) &&
		!featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDDUALSTACKVIPS, swag.StringValue(params.OpenshiftVersion), swag.String(params.CPUArchitecture)) {

		return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s %s", "dual-stack VIPs are not supported in OpenShift", *params.OpenshiftVersion))
	}

	targetConfiguration.UserManagedNetworking = swag.Bool(false)
	if params.UserManagedNetworking != nil {
		targetConfiguration.UserManagedNetworking = params.UserManagedNetworking
	}
	targetConfiguration.VipDhcpAllocation = swag.Bool(false)
	if params.VipDhcpAllocation != nil {
		targetConfiguration.VipDhcpAllocation = params.VipDhcpAllocation
	}
	targetConfiguration.ID = &clusterId
	targetConfiguration.APIVip = params.APIVip
	targetConfiguration.IngressVip = params.IngressVip
	targetConfiguration.APIVips = params.APIVips
	targetConfiguration.IngressVips = params.IngressVips
	targetConfiguration.UserManagedNetworking = params.UserManagedNetworking
	targetConfiguration.VipDhcpAllocation = params.VipDhcpAllocation
	targetConfiguration.HighAvailabilityMode = params.HighAvailabilityMode
	targetConfiguration.ClusterNetworks = params.ClusterNetworks
	targetConfiguration.ServiceNetworks = params.ServiceNetworks
	targetConfiguration.MachineNetworks = params.MachineNetworks

	return validateVIPAddresses(ipV6Supported, targetConfiguration)
}

func validateVIPsWithUMA(cluster *common.Cluster, vipDhcpAllocation bool) error {
	var (
		apiVip      string
		ingressVip  string
		apiVips     []*models.APIVip
		ingressVips []*models.IngressVip
	)

	if !swag.BoolValue(cluster.VipDhcpAllocation) {
		apiVip = cluster.APIVip
		ingressVip = cluster.IngressVip
		apiVips = cluster.APIVips
		ingressVips = cluster.IngressVips
	}
	return ValidateVIPsWereNotSetUserManagedNetworking(
		apiVip, ingressVip, apiVips, ingressVips, vipDhcpAllocation,
	)
}

func ValidateClusterUpdateVIPAddresses(ipV6Supported bool, cluster *common.Cluster, params *models.V2ClusterUpdateParams) error {
	var err error
	targetConfiguration := common.Cluster{}

	// Backwards compatibility: An old client is used and it can't send fields it doesn't know about.
	params.APIVips, err = HandleApiVipBackwardsCompatibility(*cluster.ID, swag.StringValue(params.APIVip), params.APIVips)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	params.IngressVips, err = HandleIngressVipBackwardsCompatibility(*cluster.ID, swag.StringValue(params.IngressVip), params.IngressVips)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if (len(params.APIVips) > 1 || len(params.IngressVips) > 1) &&
		!featuresupport.IsFeatureAvailable(models.FeatureSupportLevelIDDUALSTACKVIPS, cluster.OpenshiftVersion, swag.String(cluster.CPUArchitecture)) {

		return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s %s", "dual-stack VIPs are not supported in OpenShift", cluster.OpenshiftVersion))
	}

	// Update-flow backwards compatibility: An old client is used and it can't send fields it doesn't know about.
	if err1 := handleApiVipUpdateBackwardsCompatibility(cluster, params); err1 != nil {
		err = multierror.Append(err, err1)
	}
	if err2 := handleIngressVipUpdateBackwardsCompatibility(cluster, params); err2 != nil {
		err = multierror.Append(err, err2)
	}
	if err != nil && !strings.Contains(err.Error(), "0 errors occurred") {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if swag.BoolValue(params.UserManagedNetworking) {
		vipDhcpAllocation := swag.BoolValue(cluster.VipDhcpAllocation)
		if params.VipDhcpAllocation != nil { // VipDhcpAllocation from update params should take precedence
			vipDhcpAllocation = swag.BoolValue(params.VipDhcpAllocation)
		}

		if err = validateVIPsWithUMA(cluster, vipDhcpAllocation); err != nil {
			// reformat error to match order of actions
			errParts := strings.Split(err.Error(), " cannot be set with ")
			err = errors.Errorf("%s cannot be set with %s", errParts[1], errParts[0])
			return common.NewApiError(http.StatusBadRequest, err)
		}

		if swag.BoolValue(cluster.VipDhcpAllocation) { // override VIPs that were allocated via DHCP
			params.APIVip = swag.String("")
			params.IngressVip = swag.String("")
			params.APIVips = []*models.APIVip{}
			params.IngressVips = []*models.IngressVip{}
		}
	}

	targetConfiguration.ID = cluster.ID
	targetConfiguration.VipDhcpAllocation = params.VipDhcpAllocation
	targetConfiguration.APIVip = swag.StringValue(params.APIVip)
	targetConfiguration.APIVips = params.APIVips
	targetConfiguration.IngressVip = swag.StringValue(params.IngressVip)
	targetConfiguration.IngressVips = params.IngressVips
	targetConfiguration.UserManagedNetworking = params.UserManagedNetworking
	targetConfiguration.VipDhcpAllocation = params.VipDhcpAllocation
	targetConfiguration.HighAvailabilityMode = cluster.HighAvailabilityMode
	targetConfiguration.ClusterNetworks = params.ClusterNetworks
	targetConfiguration.ServiceNetworks = params.ServiceNetworks
	targetConfiguration.MachineNetworks = params.MachineNetworks

	return validateVIPAddresses(ipV6Supported, targetConfiguration)
}

func VerifyParsableVIPs(apiVips []*models.APIVip, ingressVips []*models.IngressVip) error {
	var multiErr error

	for i := range apiVips {
		if string(apiVips[i].IP) != "" && net.ParseIP(string(apiVips[i].IP)) == nil {
			multiErr = multierror.Append(multiErr, errors.Errorf("Could not parse VIP ip %s", string(apiVips[i].IP)))
		}
	}
	for i := range ingressVips {
		if string(ingressVips[i].IP) != "" && net.ParseIP(string(ingressVips[i].IP)) == nil {
			multiErr = multierror.Append(multiErr, errors.Errorf("Could not parse VIP ip %s", string(ingressVips[i].IP)))
		}
	}
	if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
		return multiErr
	}

	return nil
}

func validateApiVipAddressesInput(apiVips []*models.APIVip) error {
	if len(apiVips) > 2 {
		return errors.Errorf("apiVIPs supports 2 vips. got: %d", len(apiVips))
	}

	if err := VerifyParsableVIPs(apiVips, nil); err != nil {
		return err
	}
	return nil
}

func validateIngressVipAddressesInput(ingressVips []*models.IngressVip) error {
	if len(ingressVips) > 2 {
		return errors.Errorf("ingressVips supports 2 vips. got: %d", len(ingressVips))
	}

	if err := VerifyParsableVIPs(nil, ingressVips); err != nil {
		return err
	}
	return nil
}

func validateIPAddressesInput(apiVips []*models.APIVip, ingressVips []*models.IngressVip) error {
	var err error
	var multiErr error

	if err = validateApiVipAddressesInput(apiVips); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	if err = validateIngressVipAddressesInput(ingressVips); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	if len(apiVips) != len(ingressVips) {
		err = errors.Errorf("configuration must include the same number of apiVIPs (got %d) and ingressVIPs (got %d)",
			len(apiVips), len(ingressVips))
		multiErr = multierror.Append(multiErr, err)

	}
	if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
		return multiErr
	}
	return nil
}

func validateNetworksIPAddressFamily(ipV6Supported bool, targetConfiguration common.Cluster) error {
	var (
		networks []*string
		err      error
		multiErr error
	)

	machineNetworks := network.DerefMachineNetworks(funk.Get(targetConfiguration, "MachineNetworks"))
	serviceNetworks := network.DerefServiceNetworks(funk.Get(targetConfiguration, "ServiceNetworks"))
	clusterNetworks := network.DerefClusterNetworks(funk.Get(targetConfiguration, "ClusterNetworks"))

	for i := range machineNetworks {
		networks = append(networks, swag.String(string(machineNetworks[i].Cidr)))
	}
	if err = ValidateIPAddressFamily(ipV6Supported, networks...); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	for i := range serviceNetworks {
		networks = append(networks, swag.String(string(serviceNetworks[i].Cidr)))
	}
	if err = ValidateIPAddressFamily(ipV6Supported, networks...); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	for i := range clusterNetworks {
		networks = append(networks, swag.String(string(clusterNetworks[i].Cidr)))
	}
	if err = ValidateIPAddressFamily(ipV6Supported, networks...); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}

	if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
		return multiErr
	}
	return nil
}

func validateVIPAddressFamily(ipV6Supported bool, targetConfiguration common.Cluster) ([]*string, error) {
	var allAddresses []*string
	var err error

	if len(targetConfiguration.APIVips) == 1 {
		if network.IsIPv6Addr(network.GetApiVipById(&targetConfiguration, 0)) && !ipV6Supported {
			err = errors.New("IPv6 is not supported in this setup")
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetApiVipById(&targetConfiguration, 0)))
	} else if len(targetConfiguration.APIVips) == 2 {
		if !network.IsIPv4Addr(network.GetApiVipById(&targetConfiguration, 0)) {
			err = errors.Errorf("the first element of apiVIPs must be an IPv4 address. got: %s", network.GetApiVipById(&targetConfiguration, 0))
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetApiVipById(&targetConfiguration, 0)))

		if !network.IsIPv6Addr(network.GetApiVipById(&targetConfiguration, 1)) {
			err = errors.Errorf("the second element of apiVIPs must be an IPv6 address. got: %s", network.GetApiVipById(&targetConfiguration, 1))
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetApiVipById(&targetConfiguration, 1)))
	}

	if len(targetConfiguration.IngressVips) == 1 {
		if network.IsIPv6Addr(network.GetIngressVipById(&targetConfiguration, 0)) && !ipV6Supported {
			err = errors.New("IPv6 is not supported in this setup")
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetIngressVipById(&targetConfiguration, 0)))
	} else if len(targetConfiguration.IngressVips) == 2 {
		if !network.IsIPv4Addr(network.GetIngressVipById(&targetConfiguration, 0)) {
			err = errors.Errorf("the first element of ingressVips must be an IPv4 address. got: %s", network.GetIngressVipById(&targetConfiguration, 0))
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetIngressVipById(&targetConfiguration, 0)))

		if !network.IsIPv6Addr(network.GetIngressVipById(&targetConfiguration, 1)) {
			err = errors.Errorf("the second element of ingressVips must be an IPv6 address. got: %s", network.GetIngressVipById(&targetConfiguration, 1))
			return nil, err
		}
		allAddresses = append(allAddresses, swag.String(network.GetIngressVipById(&targetConfiguration, 1)))
	}
	return allAddresses, nil
}

func validateVIPAddresses(ipV6Supported bool, targetConfiguration common.Cluster) error {
	var allAddresses []*string
	var multiErr error
	var err error

	// Basic input validations
	if err = validateIPAddressesInput(targetConfiguration.APIVips, targetConfiguration.IngressVips); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// In-depth input validations
	if err = network.ValidateNoVIPAddressesDuplicates(targetConfiguration.APIVips, targetConfiguration.IngressVips); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if err = validateNetworksIPAddressFamily(ipV6Supported, targetConfiguration); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if allAddresses, err = validateVIPAddressFamily(ipV6Supported, targetConfiguration); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	allAddresses = append(allAddresses, common.GetNetworksCidrs(targetConfiguration)...)
	err = ValidateIPAddressFamily(ipV6Supported, allAddresses...)
	if err != nil {
		return err
	}
	err = ValidateDualStackNetworks(targetConfiguration, false)
	if err != nil {
		return err
	}

	// When running with User Managed Networking we do not allow setting any advanced network
	// parameters via the Cluster configuration
	if swag.BoolValue(targetConfiguration.UserManagedNetworking) {
		if err = ValidateVIPsWereNotSetUserManagedNetworking(targetConfiguration.APIVip, targetConfiguration.IngressVip,
			targetConfiguration.APIVips, targetConfiguration.IngressVips, swag.BoolValue(targetConfiguration.VipDhcpAllocation)); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	reqDualStack := network.CheckIfClusterIsDualStack(&targetConfiguration)

	// In any case, if VIPs are provided, they must pass the validation for being part of the
	// primary Machine Network and for non-overlapping addresses
	if swag.BoolValue(targetConfiguration.VipDhcpAllocation) {
		if err = ValidateVIPsWereNotSetDhcpMode(targetConfiguration.APIVip, targetConfiguration.IngressVip,
			targetConfiguration.APIVips, targetConfiguration.IngressVips); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	} else {
		if len(targetConfiguration.MachineNetworks) > 0 {
			for i := range targetConfiguration.APIVips { // len of APIVips and IngressVips should be the same. asserted above.
				err = network.VerifyVips(nil, string(targetConfiguration.MachineNetworks[i].Cidr),
					string(targetConfiguration.APIVips[i].IP), string(targetConfiguration.IngressVips[i].IP), nil)
				if err != nil {
					multiErr = multierror.Append(multiErr, err)
				}
			}
			if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
				return multiErr
			}
		} else if reqDualStack {
			return errors.New("Dual-stack cluster cannot be created with empty Machine Networks")
		}
	}

	return nil
}

func ValidateVIPsWereNotSetUserManagedNetworking(apiVip string, ingressVip string, apiVips []*models.APIVip, ingressVips []*models.IngressVip, vipDhcpAllocation bool) error {
	if vipDhcpAllocation {
		err := errors.Errorf("VIP DHCP Allocation cannot be set with User Managed Networking")
		return err
	}
	if apiVip != "" {
		err := errors.New("API VIP cannot be set with User Managed Networking")
		return err
	}
	if len(apiVips) > 0 {
		err := errors.New("API VIPs cannot be set with User Managed Networking")
		return err
	}
	if ingressVip != "" {
		err := errors.New("Ingress VIP cannot be set with User Managed Networking")
		return err
	}
	if len(ingressVips) > 0 {
		err := errors.New("Ingress VIPs cannot be set with User Managed Networking")
		return err
	}
	return nil
}

func ValidateVIPsWereNotSetDhcpMode(apiVip string, ingressVip string, apiVips []*models.APIVip, ingressVips []*models.IngressVip) error {
	if apiVip != "" {
		err := errors.New("Setting API VIP is forbidden when cluster is in vip-dhcp-allocation mode")
		return err
	}
	if apiVips != nil {
		err := errors.New("Setting API VIPs is forbidden when cluster is in vip-dhcp-allocation mode")
		return err
	}
	if ingressVip != "" {
		err := errors.New("Setting Ingress VIP is forbidden when cluster is in vip-dhcp-allocation mode")
		return err
	}
	if ingressVips != nil {
		err := errors.New("Setting Ingress VIPs is forbidden when cluster is in vip-dhcp-allocation mode")
		return err
	}
	return nil
}

func ValidateDualStackNetworks(clusterParams interface{}, alreadyDualStack bool) error {
	var machineNetworks []*models.MachineNetwork
	var serviceNetworks []*models.ServiceNetwork
	var clusterNetworks []*models.ClusterNetwork
	var err error
	var ipv4, ipv6 bool
	reqDualStack := false

	machineNetworks = network.DerefMachineNetworks(funk.Get(clusterParams, "MachineNetworks"))
	serviceNetworks = network.DerefServiceNetworks(funk.Get(clusterParams, "ServiceNetworks"))
	clusterNetworks = network.DerefClusterNetworks(funk.Get(clusterParams, "ClusterNetworks"))

	ipv4, ipv6, err = network.GetAddressFamilies(machineNetworks)
	if err != nil {
		return err
	}
	reqDualStack = reqDualStack || (ipv4 && ipv6)

	if !reqDualStack {
		ipv4, ipv6, err = network.GetAddressFamilies(serviceNetworks)
		if err != nil {
			return err
		}
		reqDualStack = ipv4 && ipv6
	}

	if !reqDualStack {
		ipv4, ipv6, err = network.GetAddressFamilies(clusterNetworks)
		if err != nil {
			return err
		}
		reqDualStack = ipv4 && ipv6
	}

	// When creating a cluster, we are always first creating an object with empty Machine Networks
	// and only afterwards we update it with requested Machine Networks. Because of this, the
	// creation cluster payload never contains both Cluster/Service and Machine Networks. In order
	// to overcome this, we are checking for dual-stackness in the current payload as well as
	// in the current cluster object.
	if len(serviceNetworks) == 0 && len(clusterNetworks) == 0 && !reqDualStack {
		reqDualStack = alreadyDualStack
	}

	if reqDualStack {
		if common.IsSliceNonEmpty(machineNetworks) {
			if err := network.VerifyMachineNetworksDualStack(machineNetworks, true); err != nil {
				return err
			}
		}
		if common.IsSliceNonEmpty(serviceNetworks) {
			if err := network.VerifyServiceNetworksDualStack(serviceNetworks, true); err != nil {
				return err
			}
		}
		if common.IsSliceNonEmpty(clusterNetworks) {
			if err := network.VerifyClusterNetworksDualStack(clusterNetworks, true); err != nil {
				return err
			}
		}
	} else {
		if len(machineNetworks) > 1 {
			err := errors.Errorf("Single-stack cluster cannot contain multiple Machine Networks")
			return err
		}
	}
	return nil
}

// ValidateIPAddressFamily returns an error if the argument contains only IPv6 networks and IPv6
// support is turned off. Dual-stack setup is supported even if IPv6 support is turned off.
func ValidateIPAddressFamily(ipV6Supported bool, elements ...*string) error {
	if ipV6Supported {
		return nil
	}
	ipv4 := false
	ipv6 := false
	for _, e := range elements {
		if e == nil || *e == "" {
			continue
		}
		currRecordIPv6Stack := strings.Contains(*e, ":")
		ipv4 = ipv4 || !currRecordIPv6Stack
		ipv6 = ipv6 || currRecordIPv6Stack
	}
	if ipv6 && !ipv4 {
		return errors.Errorf("IPv6 is not supported in this setup")
	}
	return nil
}

func ValidateDiskEncryptionParams(diskEncryptionParams *models.DiskEncryption, DiskEncryptionSupport bool) error {
	if diskEncryptionParams == nil {
		return nil
	}
	if !DiskEncryptionSupport && swag.StringValue(diskEncryptionParams.EnableOn) != models.DiskEncryptionEnableOnNone {
		return errors.New("Disk encryption support is not enabled. Cannot apply configurations to the cluster")
	}
	if diskEncryptionParams.Mode != nil && swag.StringValue(diskEncryptionParams.Mode) == models.DiskEncryptionModeTang {
		if diskEncryptionParams.TangServers == "" {
			return errors.New("Setting Tang mode but tang_servers isn't set")
		}
		tangServers, err := tang.UnmarshalTangServers(diskEncryptionParams.TangServers)
		if err != nil {
			return err
		}
		for _, ts := range tangServers {
			if _, err := url.ParseRequestURI(ts.Url); err != nil {
				return errors.Wrap(err, "Tang URL isn't valid")
			}
			if ts.Thumbprint == "" {
				return errors.New("Tang thumbprint isn't set")
			}
		}
	}
	return nil
}

func ValidateHighAvailabilityModeWithPlatform(highAvailabilityMode *string, platform *models.Platform) error {
	if swag.StringValue(highAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		if platform != nil && platform.Type != nil && *platform.Type != models.PlatformTypeNone && !swag.BoolValue(platform.IsExternal) {
			return errors.Errorf("Single node cluster is not supported alongside %s platform", *platform.Type)
		}
	}

	return nil
}

func ValidateIgnitionImageSize(config string) error {
	var err error
	var data *bytes.Reader

	// Ensure that the ignition archive isn't larger than 256KiB
	configBytes := []byte(config)
	content := isoeditor.IgnitionContent{Config: configBytes}
	data, err = content.Archive()
	if err != nil {
		return errors.Wrap(err, "Failed to create ignition archive")
	}
	ignitionImageSize := data.Len()
	if ignitionImageSize > IgnitionImageSizePadding {
		return errors.New(fmt.Sprintf("The ignition archive size (%d KiB) is over the maximum allowable size (%d KiB)",
			ignitionImageSize/1024, IgnitionImageSizePadding/1024))
	}

	return nil
}

func ValidateArchitectureWithPlatform(architecture *string, platform *models.Platform) error {
	if platform != nil && platform.Type != nil && *platform.Type == models.PlatformTypeNutanix {
		if swag.StringValue(architecture) != common.X86CPUArchitecture {
			return errors.New("only x86-64 CPU architecture is supported on Nutanix clusters")
		}
	}

	return nil
}

func ValidatePlatformCapability(platform *models.Platform, ctx context.Context, authzHandler auth.Authorizer, log logrus.FieldLogger) error {
	if platform == nil {
		return nil
	}

	var capabilityName *string
	switch *platform.Type {
	case models.PlatformTypeOci:
		capabilityName = swag.String(ocm.PlatformOciCapabilityName)
	}

	if capabilityName == nil {
		return nil
	}

	available, err := authzHandler.HasOrgBasedCapability(ctx, *capabilityName)
	if err == nil && available {
		return nil
	}

	if err != nil {
		log.WithError(err).Errorf("error getting user %s capability", *capabilityName)
	}

	return common.NewApiError(http.StatusBadRequest, errors.Errorf("Platform %s is not available", *platform.Type))
}
