package dns

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/validations"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	apiDomainNameFormat    = "api.%s.%s"
	apiINTDomainNameFormat = "api-int.%s.%s"
	appsDomainNameFormat   = "apps.%s.%s"
	dnsDomainLabelLen      = 63
	dnsDomainTotalLen      = 255
	dnsDomainPrefixMaxLen  = dnsDomainTotalLen - dnsDomainLabelLen - 1 // reserve for another label and the separating '.'
)

type DNSDomain struct {
	Name              string
	ID                string
	Provider          string
	APIDomainName     string
	APIINTDomainName  string
	IngressDomainName string
}

type DNSApi interface {
	CreateDNSRecordSets(ctx context.Context, cluster *common.Cluster) error
	DeleteDNSRecordSets(ctx context.Context, cluster *common.Cluster) error
	GetDNSDomain(clusterName, baseDNSDomainName string) (*DNSDomain, error)
	ValidateDNSName(clusterName, baseDNSDomainName string) error
	ValidateBaseDNS(domain *DNSDomain) error
	ValidateDNSRecords(cluster common.Cluster, domain *DNSDomain) error
}

//go:generate mockgen -source=dns.go -package=dns -destination=mock_dns.generated_go
type DNSProviderFactory interface {
	GetProviderByRecordType(domain *DNSDomain, recordType string) dnsproviders.Provider
	GetProvider(domain *DNSDomain) dnsproviders.Provider
}

type defaultDNSProviderFactory struct {
	log logrus.FieldLogger
}

type handler struct {
	log             logrus.FieldLogger
	baseDNSDomains  map[string]string
	providerFactory DNSProviderFactory
}

func NewDNSHandler(baseDNSDomains map[string]string, log logrus.FieldLogger) DNSApi {
	return NewDNSHandlerWithProviders(baseDNSDomains, log, &defaultDNSProviderFactory{log})
}

func NewDNSHandlerWithProviders(baseDNSDomains map[string]string, log logrus.FieldLogger, providers DNSProviderFactory) DNSApi {
	return &handler{
		baseDNSDomains:  baseDNSDomains,
		log:             log,
		providerFactory: providers,
	}
}

func (h *handler) CreateDNSRecordSets(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, h.log)
	ok, err := h.updateDNSRecordSet(log, cluster, h.createDNSRecord)
	if err != nil {
		return err
	} else if ok {
		log.Infof("Successfully created DNS records for base domain: %s", cluster.BaseDNSDomain)
	}
	return nil
}

func (h *handler) DeleteDNSRecordSets(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, h.log)
	ok, err := h.updateDNSRecordSet(log, cluster, h.deleteDNSRecord)
	if err != nil {
		return err
	} else if ok {
		log.Infof("Successfully deleted DNS records for base domain: %s", cluster.BaseDNSDomain)
	}
	return nil
}

func (h *handler) updateDNSRecordSet(log logrus.FieldLogger, cluster *common.Cluster, updateRecordFunc func(logrus.FieldLogger, *DNSDomain, string, string) error) (bool, error) {
	domain, err := h.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
	if err != nil {
		return false, err
	}
	if domain == nil {
		log.Debug("No supported base DNS domain specified")
		return false, nil
	}

	apiVip := cluster.APIVip
	ingressVip := cluster.IngressVip
	if common.IsSingleNodeCluster(cluster) {
		apiVip, err = network.GetIpForSingleNodeInstallation(cluster, log)
		if err != nil {
			log.WithError(err).Errorf("failed to find ip for single node installation")
			return false, err
		}

		ingressVip = apiVip
		if err := updateRecordFunc(log, domain, domain.APIINTDomainName, apiVip); err != nil {
			return false, err
		}
	}

	if err := updateRecordFunc(log, domain, domain.APIDomainName, apiVip); err != nil {
		return false, err
	}
	if err := updateRecordFunc(log, domain, domain.IngressDomainName, ingressVip); err != nil {
		return false, err
	}
	return true, nil
}

func (h *handler) createDNSRecord(log logrus.FieldLogger, domain *DNSDomain, name, ip string) error {
	recordType := getDNSRecordType(ip)
	if provider := h.providerFactory.GetProviderByRecordType(domain, recordType); provider != nil {
		_, err := provider.CreateRecordSet(name, ip)
		if err != nil {
			log.WithError(err).Errorf("failed to create DNS record: (%s, %s)", name, ip)
			return err
		}
	}
	return nil
}

func getDNSRecordType(ipAddress string) string {
	if network.IsIPv4Addr(ipAddress) {
		return "A"
	} else {
		return "AAAA"
	}
}

func (h *handler) deleteDNSRecord(log logrus.FieldLogger, domain *DNSDomain, name, ip string) error {
	recordType := getDNSRecordType(ip)
	if provider := h.providerFactory.GetProviderByRecordType(domain, recordType); provider != nil {
		_, err := provider.DeleteRecordSet(name, ip)
		if err != nil {
			log.WithError(err).Errorf("failed to delete DNS record: (%s, %s)", name, ip)
			return err
		}
	}
	return nil
}

func (h *handler) GetDNSDomain(clusterName, baseDNSDomainName string) (*DNSDomain, error) {
	var dnsDomainID string
	var dnsProvider string

	// Parse base domains from config
	if val, ok := h.baseDNSDomains[baseDNSDomainName]; ok {
		re := regexp.MustCompile("/")
		if !re.MatchString(val) {
			return nil, errors.New(fmt.Sprintf("Invalid DNS domain: %s", val))
		}
		s := re.Split(val, 2)
		dnsDomainID = s[0]
		dnsProvider = s[1]
	} else {
		h.log.Debugf("No DNS configuration for base domain '%s'", baseDNSDomainName)
		return nil, nil
	}
	if dnsDomainID == "" || dnsProvider == "" {
		h.log.Debugf("DNS domain '%s' is not fully defined in the configuration. Domain ID: %s, DNS provider: %s", baseDNSDomainName, dnsDomainID, dnsProvider)
		return nil, nil
	}

	return &DNSDomain{
		Name:              baseDNSDomainName,
		ID:                dnsDomainID,
		Provider:          dnsProvider,
		APIDomainName:     fmt.Sprintf(apiDomainNameFormat, clusterName, baseDNSDomainName),
		APIINTDomainName:  fmt.Sprintf(apiINTDomainNameFormat, clusterName, baseDNSDomainName),
		IngressDomainName: fmt.Sprintf("*.%s", fmt.Sprintf(appsDomainNameFormat, clusterName, baseDNSDomainName)),
	}, nil
}

// ValidateDNSName checks baseDNSDomainName and the combination of cluster name and base DNS domain
// leaves enough room for automatically added domain names,
// e.g. "alertmanager-main-openshift-monitoring.apps.test-infra-cluster-assisted-installer.example.com").
// The max total length of a domain name is 255 bytes, including the dots. An individual label can be
// up to 63 bytes. A single char may occupy more than one byte in Internationalized Domain Names (IDNs).
func (h *handler) ValidateDNSName(clusterName, baseDNSDomainName string) error {
	appsDomainNameSuffix := fmt.Sprintf(appsDomainNameFormat, clusterName, baseDNSDomainName)
	apiErrorCode, err := validations.ValidateDomainNameFormat(baseDNSDomainName)
	if err != nil {
		return common.NewApiError(apiErrorCode, err)
	}
	if len(appsDomainNameSuffix) > dnsDomainPrefixMaxLen {
		return errors.Errorf("Combination of cluster name and base DNS domain too long")
	}
	for _, label := range strings.Split(appsDomainNameSuffix, ".") {
		if len(label) > dnsDomainLabelLen {
			return errors.Errorf("DNS label '%s' is longer than 63 bytes", label)
		}
	}
	return nil
}

// ValidateBaseDNS validates the specified base domain name
func (h *handler) ValidateBaseDNS(domain *DNSDomain) error {
	dnsProvider := h.providerFactory.GetProvider(domain)
	dnsNameFromService, err := dnsProvider.GetDomainName()
	if err != nil {
		return errors.Errorf("Can't validate base DNS domain: %v", err)
	}

	dnsNameFromCluster := strings.TrimSuffix(domain.Name, ".")
	if dnsNameFromService == dnsNameFromCluster {
		// Valid domain
		return nil
	}
	if matched, _ := regexp.MatchString(".*\\."+dnsNameFromService, dnsNameFromCluster); !matched {
		return errors.Errorf("Domain name isn't correlated properly to DNS service")
	}
	return nil
}

func (h *handler) ValidateDNSRecords(cluster common.Cluster, domain *DNSDomain) error {
	vipAddresses := []string{domain.APIDomainName, domain.IngressDomainName}
	if swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		vipAddresses = append(vipAddresses, domain.APIINTDomainName)
	}
	if err := h.checkDNSRecordsExists(vipAddresses, domain, "A"); err != nil {
		return err
	}
	if err := h.checkDNSRecordsExists(vipAddresses, domain, "AAAA"); err != nil {
		return err
	}
	return nil
}

func (h *handler) checkDNSRecordsExists(names []string, domain *DNSDomain, recordType string) error {
	dnsProvider := h.providerFactory.GetProviderByRecordType(domain, recordType)
	if dnsProvider == nil {
		return nil
	}
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

func (f *defaultDNSProviderFactory) GetProviderByRecordType(domain *DNSDomain, recordType string) dnsproviders.Provider {
	switch domain.Provider {
	case "route53":
		return dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: recordType,
				TTL:           60,
			},
			HostedZoneID: domain.ID,
			SharedCreds:  true,
		}
	}
	f.log.Debugf("No suitable implementation for DNS provider %s", domain.Provider)
	return nil
}

func (f *defaultDNSProviderFactory) GetProvider(domain *DNSDomain) dnsproviders.Provider {
	switch domain.Provider {
	case "route53":
		return dnsproviders.Route53{
			HostedZoneID: domain.ID,
			SharedCreds:  true,
		}
	}
	f.log.Debugf("No suitable implementation for DNS provider %s", domain.Provider)
	return nil
}
