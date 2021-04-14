package dns

import (
	"context"
	"fmt"
	"regexp"

	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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
	ChangeDNSRecordSets(ctx context.Context, cluster *common.Cluster, delete bool) error
	GetDNSDomain(clusterName, baseDNSDomainName string) (*DNSDomain, error)
	ValidateBaseDNS(domain *DNSDomain) error
	ValidateDNSRecords(cluster common.Cluster, domain *DNSDomain) error
}

type handler struct {
	log            logrus.FieldLogger
	baseDNSDomains map[string]string
}

func NewDNSHandler(baseDNSDomains map[string]string, log logrus.FieldLogger) DNSApi {
	return &handler{
		baseDNSDomains: baseDNSDomains,
		log:            log,
	}
}

func (h *handler) ChangeDNSRecordSets(ctx context.Context, cluster *common.Cluster, delete bool) error {
	log := logutil.FromContext(ctx, h.log)

	domain, err := h.GetDNSDomain(cluster.Name, cluster.BaseDNSDomain)
	if err != nil {
		return err
	}
	if domain == nil {
		// No supported base DNS domain specified
		return nil
	}
	switch domain.Provider {
	case "route53":
		var dnsProvider dnsproviders.Provider = dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: "A",
				TTL:           60,
			},
			HostedZoneID: domain.ID,
			SharedCreds:  true,
		}

		dnsRecordSetFunc := dnsProvider.CreateRecordSet
		if delete {
			dnsRecordSetFunc = dnsProvider.DeleteRecordSet
		}

		apiVip := cluster.APIVip
		ingressVip := cluster.IngressVip
		if common.IsSingleNodeCluster(cluster) {
			apiVip, err = network.GetIpForSingleNodeInstallation(cluster, log)
			if err != nil {
				log.WithError(err).Errorf("failed to find ip for single node installation")
				return err
			}

			ingressVip = apiVip
			// Create/Delete A record for API-INT virtual IP
			_, err := dnsRecordSetFunc(domain.APIINTDomainName, apiVip)
			if err != nil {
				log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
					domain.APIINTDomainName, apiVip)
				return err
			}
		}

		// Create/Delete A record for API virtual IP
		_, err := dnsRecordSetFunc(domain.APIDomainName, apiVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.APIDomainName, apiVip)
			return err
		}
		// Create/Delete A record for Ingress virtual IP
		_, err = dnsRecordSetFunc(domain.IngressDomainName, ingressVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.IngressDomainName, ingressVip)
			return err
		}
		log.Infof("Successfully created DNS records for base domain: %s", cluster.BaseDNSDomain)
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
		// No base domains defined in config
		return nil, nil
	}
	if dnsDomainID == "" || dnsProvider == "" {
		// Specified domain is not defined in config
		return nil, nil
	}

	return &DNSDomain{
		Name:              baseDNSDomainName,
		ID:                dnsDomainID,
		Provider:          dnsProvider,
		APIDomainName:     fmt.Sprintf("%s.%s.%s", "api", clusterName, baseDNSDomainName),
		APIINTDomainName:  fmt.Sprintf("%s.%s.%s", "api-int", clusterName, baseDNSDomainName),
		IngressDomainName: fmt.Sprintf("*.%s.%s.%s", "apps", clusterName, baseDNSDomainName),
	}, nil
}

func (h *handler) ValidateBaseDNS(domain *DNSDomain) error {
	return validations.ValidateBaseDNS(domain.Name, domain.ID, domain.Provider)
}

func (b *handler) ValidateDNSRecords(cluster common.Cluster, domain *DNSDomain) error {
	vipAddresses := []string{domain.APIDomainName, domain.IngressDomainName}
	if swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		vipAddresses = append(vipAddresses, domain.APIINTDomainName)
	}
	return validations.CheckDNSRecordsExistence(vipAddresses, domain.ID, domain.Provider)
}
