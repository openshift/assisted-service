package validations

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/pkg/errors"

	"golang.org/x/crypto/ssh"

	"github.com/asaskevich/govalidator"
	"github.com/danielerez/go-dns-client/pkg/dnsproviders"

	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
)

const (
	clusterNameRegex  = "^([a-z]([-a-z0-9]*[a-z0-9])?)*$"
	dnsNameRegex      = "^([a-z0-9]+(-[a-z0-9]+)*[.])+[a-z]{2,}$"
	CloudOpenShiftCom = "cloud.openshift.com"
	sshPublicKeyRegex = "^(ssh-rsa AAAAB3NzaC1yc2|ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNT|ecdsa-sha2-nistp384 AAAAE2VjZHNhLXNoYTItbmlzdHAzODQAAAAIbmlzdHAzOD|ecdsa-sha2-nistp521 AAAAE2VjZHNhLXNoYTItbmlzdHA1MjEAAAAIbmlzdHA1Mj|ssh-ed25519 AAAAC3NzaC1lZDI1NTE5|ssh-dss AAAAB3NzaC1kc3)[0-9A-Za-z+/]+[=]{0,3}( .*)?$"
)

type imagePullSecret struct {
	Auths map[string]map[string]interface{} `json:"auths"`
}

type PullSecretCreds struct {
	Username string
	Password string
	Registry string
	AuthRaw  string
}

func ParsePullSecret(secret string) (map[string]PullSecretCreds, error) {
	result := make(map[string]PullSecretCreds)
	var s imagePullSecret
	err := json.Unmarshal([]byte(secret), &s)
	if err != nil {
		return nil, errors.Errorf("invalid pull secret: %v", err)
	}
	if len(s.Auths) == 0 {
		return nil, errors.Errorf("invalid pull secret: missing 'auths' JSON-object field")
	}

	for d, a := range s.Auths {
		_, authPresent := a["auth"]
		_, credsStorePresent := a["credsStore"]
		if !authPresent && !credsStorePresent {
			return nil, errors.Errorf("invalid pull secret, '%q' JSON-object requires either 'auth' or 'credsStore' field", d)
		}
		data, err := base64.StdEncoding.DecodeString(a["auth"].(string))
		if err != nil {
			return nil, errors.Errorf("invalid pull secret, 'auth' fiels of '%q' is not base64 decodable", d)
		}
		res := bytes.Split(data, []byte(":"))
		if len(res) != 2 {
			return nil, errors.Errorf("auth for %s has invalid format", d)
		}
		result[d] = PullSecretCreds{
			Password: string(res[1]),
			Username: string(res[0]),
			AuthRaw:  a["auth"].(string),
			Registry: d,
		}

	}
	return result, nil
}

func AddRHRegPullSecret(secret, rhCred string) (string, error) {
	if rhCred == "" {
		return "", errors.Errorf("invalid pull secret")
	}
	var s imagePullSecret
	err := json.Unmarshal([]byte(secret), &s)
	if err != nil {
		return secret, errors.Errorf("invalid pull secret: %v", err)
	}
	s.Auths["registry.stage.redhat.io"] = make(map[string]interface{})
	s.Auths["registry.stage.redhat.io"]["auth"] = base64.StdEncoding.EncodeToString([]byte(rhCred))
	ps, err := json.Marshal(s)
	if err != nil {
		return secret, err
	}
	return string(ps), nil
}

/*
const (
	registryCredsToCheck string = "registry.redhat.io"
)
*/

func ValidatePullSecret(secret string, username string, authHandler auth.AuthHandler) error {
	creds, err := ParsePullSecret(secret)
	if err != nil {
		return err
	}

	if authHandler.EnableAuth {
		r, ok := creds["cloud.openshift.com"]
		if !ok {
			return errors.Errorf("Pull secret does not contain auth for cloud.openshift.com")
		}
		user, err := authHandler.AuthAgentAuth(r.AuthRaw)
		if err != nil {
			return errors.Errorf("Failed to authenticate Pull Secret Token")
		}
		if (user.(*ocm.AuthPayload)).Username != username {
			return errors.Errorf("Pull Secret Token does not match User")
		}
	}
	/*
		Actual credentials check is disabled for not until we solve how to do it in tests and subsystem
		r, ok := creds[registryCredsToCheck]
		if !ok {
			return fmt.Errorf("Pull secret does not contain auth for %s", registryCredsToCheck)
		}
		dc, err := docker.NewEnvClient()
		if err != nil {
			return err
		}
		auth := types.AuthConfig{
			ServerAddress: r.Registry,
			Username:      r.Username,
			Password:      r.Password,
		}
		_, err = dc.RegistryLogin(context.Background(), auth)
		if err != nil {
			return err
		}
	*/
	return nil
}

func ValidateDomainNameFormat(dnsDomainName string) error {
	matched, err := regexp.MatchString(dnsNameRegex, dnsDomainName)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "DNS name validation for %s", dnsDomainName))
	}
	if !matched {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("DNS format mismatch: %s domain name is not valid", dnsDomainName))
	}
	return nil
}

// ValidateBaseDNS validates the specified base domain name
func ValidateBaseDNS(dnsDomainName, dnsDomainID, dnsProviderType string) error {
	var dnsProvider dnsproviders.Provider
	switch dnsProviderType {
	case "route53":
		dnsProvider = dnsproviders.Route53{
			HostedZoneID: dnsDomainID,
			SharedCreds:  true,
		}
	default:
		return nil
	}
	return validateBaseDNS(dnsDomainName, dnsDomainID, dnsProvider)
}

func validateBaseDNS(dnsDomainName, dnsDomainID string, dnsProvider dnsproviders.Provider) error {
	dnsNameFromService, err := dnsProvider.GetDomainName()
	if err != nil {
		return errors.Errorf("Can't validate base DNS domain: %v", err)
	}

	dnsNameFromCluster := strings.TrimSuffix(dnsDomainName, ".")
	if dnsNameFromService == dnsNameFromCluster {
		// Valid domain
		return nil
	}
	if matched, _ := regexp.MatchString(".*\\."+dnsNameFromService, dnsNameFromCluster); !matched {
		return errors.Errorf("Domain name isn't correlated properly to DNS service")
	}

	return nil
}

// CheckDNSRecordsExistence checks whether that specified record-set names already exist in the DNS service
func CheckDNSRecordsExistence(names []string, dnsDomainID, dnsProviderType string) error {
	var dnsProvider dnsproviders.Provider
	switch dnsProviderType {
	case "route53":
		dnsProvider = dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: "A",
			},
			HostedZoneID: dnsDomainID,
			SharedCreds:  true,
		}
	default:
		return nil
	}
	return checkDNSRecordsExistence(names, dnsProvider)
}

func checkDNSRecordsExistence(names []string, dnsProvider dnsproviders.Provider) error {
	for _, name := range names {
		res, err := dnsProvider.GetRecordSet(name)
		if err != nil {
			return errors.Errorf("Can't verify DNS record set existence: %v", err)
		}
		if res != "" {
			return errors.Errorf("DNS domain already exists")
		}
	}
	return nil
}

// ValidateClusterNameFormat validates specified cluster name format
func ValidateClusterNameFormat(name string) error {
	if matched, _ := regexp.MatchString(clusterNameRegex, name); !matched {
		return errors.Errorf("Cluster name format is not valid: '%s'. "+
			"Name must consist of lower-case letters, numbers and hyphens. "+
			"It must start with a letter and end with a letter or number.", name)
	}
	return nil
}

// ValidateHTTPProxyFormat validates the HTTP Proxy and HTTPS Proxy format
func ValidateHTTPProxyFormat(proxyURL string) error {
	if !govalidator.IsURL(proxyURL) {
		return errors.Errorf("Proxy URL format is not valid: '%s'", proxyURL)
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return errors.Errorf("Proxy URL format is not valid: '%s'", proxyURL)
	}
	if u.Scheme == "https" {
		return errors.Errorf("The URL scheme must be http; https is currently not supported: '%s'", proxyURL)
	}
	if u.Scheme != "http" {
		return errors.Errorf("The URL scheme must be http and specified in the URL: '%s'", proxyURL)
	}
	return nil
}

// ValidateNoProxyFormat validates the no-proxy format which should be a comma-separated list
// of destination domain names, domains, IP addresses or other network CIDRs. A domain can be
// prefaced with '.' to include all subdomains of that domain.
func ValidateNoProxyFormat(noProxy string) error {
	domains := strings.Split(noProxy, ",")
	for _, s := range domains {
		s = strings.TrimPrefix(s, ".")
		if govalidator.IsIP(s) {
			continue
		}

		if govalidator.IsCIDR(s) {
			continue
		}

		if govalidator.IsDNSName(s) {
			continue
		}
		return errors.Errorf("NO Proxy format is not valid: '%s'. "+
			"NO Proxy is a comma-separated list of destination domain names, domains, IP addresses or other network CIDRs. "+
			"A domain can be prefaced with '.' to include all subdomains of that domain.", noProxy)
	}
	return nil
}

func ValidateSSHPublicKey(sshPublicKey string) (err error) {
	keyBytes := []byte(sshPublicKey)
	isMatched, err := regexp.Match(sshPublicKeyRegex, keyBytes)
	if err != nil {
		err = errors.Wrapf(err, "Error parsing SSH key: %s", sshPublicKey)
	} else if !isMatched {
		err = errors.Errorf(
			"SSH key: %s does not match any supported type: ssh-rsa, ssh-ed25519, ecdsa-[VARIANT]",
			sshPublicKey)
	} else if _, _, _, _, err = ssh.ParseAuthorizedKey(keyBytes); err != nil {
		err = errors.Errorf("Malformed SSH key: %s", sshPublicKey)
	}
	return
}
