package cluster

import (
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type validationStatus string

const (
	ValidationSuccess validationStatus = "success"
	ValidationFailure validationStatus = "failure"
	ValidationPending validationStatus = "pending"
)

const (
	ApiVipName     = "api vip"
	IngressVipName = "ingress vip"
)

func (v validationStatus) String() string {
	return string(v)
}

type clusterPreprocessContext struct {
	clusterId     strfmt.UUID
	cluster       *common.Cluster
	db            *gorm.DB
	calculateCidr string
}

type validationConditon func(context *clusterPreprocessContext) validationStatus
type validationStringFormatter func(context *clusterPreprocessContext, status validationStatus) string

type validation struct {
	id        validationID
	condition validationConditon
	formatter validationStringFormatter
}

func (c *clusterPreprocessContext) loadCluster() error {
	var cluster common.Cluster
	err := c.db.Preload("Hosts", "status <> ?", models.HostStatusDisabled).Take(&cluster, "id = ?", c.clusterId.String()).Error
	if err == nil {
		c.cluster = &cluster
	}
	return err
}

func newClusterValidationContext(clusterId strfmt.UUID, db *gorm.DB) (*clusterPreprocessContext, error) {
	ret := &clusterPreprocessContext{
		clusterId: clusterId,
		db:        db,
	}
	err := ret.loadCluster()
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func isDhcpLeaseAllocationTimedOut(c *clusterPreprocessContext) bool {
	return c.cluster.MachineNetworkCidrUpdatedAt.String() != "" && time.Since(c.cluster.MachineNetworkCidrUpdatedAt) > DhcpLeaseTimeoutMinutes*time.Minute
}

func boolValue(b bool) validationStatus {
	if b {
		return ValidationSuccess
	} else {
		return ValidationFailure
	}
}

type clusterValidator struct {
	log     logrus.FieldLogger
	hostAPI host.API
}

func (v *clusterValidator) isMachineCidrDefined(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.MachineNetworkCidr != "")
}

func (v *clusterValidator) printIsMachineCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			return "Machine network CIDR is undefined; setting the machine network CIDR initiates the VIPs DHCP lease allocation"
		} else {
			return "Machine network CIDR is undefined; the machine network CIDR can be defined by setting either the API VIP or the Ingress VIP"
		}
	case ValidationSuccess:
		return "Machine network CIDR is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isClusterCidrDefined(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.ClusterNetworkCidr != "")
}

func (v *clusterValidator) printIsClusterCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "Cluster Network CIDR is undefined"
	case ValidationSuccess:
		return "Cluster Network CIDR is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isServiceCidr(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.ServiceNetworkCidr != "")
}

func (v *clusterValidator) printIsServiceCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "Service Network CIDR is undefined"
	case ValidationSuccess:
		return "Service Network CIDR is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isMachineCidrEqualsToCalculatedCidr(c *clusterPreprocessContext) validationStatus {
	if c.cluster.APIVip == "" && c.cluster.IngressVip == "" {
		return ValidationPending
	}
	cidr, err := network.CalculateMachineNetworkCIDR(c.cluster.APIVip, c.cluster.IngressVip, c.cluster.Hosts)
	c.calculateCidr = cidr
	return boolValue(err == nil && cidr == c.cluster.MachineNetworkCidr)
}

func (v *clusterValidator) printIsMachineCidrEqualsToCalculatedCidr(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "Machine network CIDR or API VIP or Ingress VIP is undefined"
	case ValidationSuccess:
		return "Cluster machine CIDR equals to the calculated CIDR "
	case ValidationFailure:
		return fmt.Sprintf("Cluster machine CIDR %s is different than the calculated CIDR %s", context.cluster.MachineNetworkCidr, context.calculateCidr)
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isApiVipDefined(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.VipDhcpAllocation) && c.cluster.MachineNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(c.cluster.APIVip != "")
}

func (v *clusterValidator) printIsApiVipDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "Machine network CIDR is undefined"
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			if isDhcpLeaseAllocationTimedOut(context) {
				return "API VIP is undefined; API VIP IP allocation from DHCP server has been timed out"
			} else {
				return "API VIP is undefined; after the machine network CIDR has been defined, the API VIP is received from DHCP lease allocation task which may take up to 2 minutes"
			}
		} else {
			return "API VIP is undefined and must be provided"
		}
	case ValidationSuccess:
		return "API VIP is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isApiVipValid(c *clusterPreprocessContext) validationStatus {
	if c.cluster.APIVip == "" {
		return ValidationPending
	}
	err := network.VerifyVip(c.cluster.Hosts, c.cluster.MachineNetworkCidr, c.cluster.APIVip, ApiVipName,
		true, v.log)
	return boolValue(err == nil)
}

func (v *clusterValidator) printIsApiVipValid(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "API VIP is undefined"
	case ValidationSuccess:
		return fmt.Sprintf("%s %s belongs to machine CIDR and not in use ", ApiVipName, context.cluster.APIVip)
	case ValidationFailure:
		return fmt.Sprintf("%s %s does not belong to machine CIDR or already in use ", ApiVipName, context.cluster.APIVip)
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isIngressVipDefined(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.VipDhcpAllocation) && c.cluster.MachineNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(c.cluster.IngressVip != "")
}

func (v *clusterValidator) printIsIngressVipDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "Machine network CIDR is undefined"
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			if isDhcpLeaseAllocationTimedOut(context) {
				return "Ingress VIP is undefined; Ingress VIP IP allocation from DHCP server has been timed out"
			} else {
				return "Ingress VIP is undefined; after the machine network CIDR has been defined, the Ingress VIP is received from DHCP lease allocation task which may take up to 2 minutes"
			}
		} else {
			return "Ingress VIP is undefined and must be provided"
		}
	case ValidationSuccess:
		return "Ingress VIP is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}
func (v *clusterValidator) isIngressVipValid(c *clusterPreprocessContext) validationStatus {
	if c.cluster.IngressVip == "" {
		return ValidationPending
	}
	err := network.VerifyVip(c.cluster.Hosts, c.cluster.MachineNetworkCidr, c.cluster.IngressVip, IngressVipName,
		true, v.log)
	return boolValue(err == nil)
}

func (v *clusterValidator) printIsIngressVipValid(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "Ingress VIP is undefined"
	case ValidationSuccess:
		return fmt.Sprintf("%s %s belongs to machine CIDR and not in use ", IngressVipName, context.cluster.IngressVip)
	case ValidationFailure:
		return fmt.Sprintf("%s %s does not belong to machine CIDR or already in use ", IngressVipName, context.cluster.IngressVip)
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

// conditions to have a valid number of masters
// 1. have exactly three masters
// 2. have less then 3 master but enough auto-assign hosts that can become masters
// having more then 3 known masters is failure
func (v *clusterValidator) sufficientMastersCount(c *clusterPreprocessContext) validationStatus {
	mappedMastersByRole := MapMasterHostsByStatus(c.cluster)
	mastersInKnown, ok := mappedMastersByRole[models.HostStatusKnown]

	if ok && len(mastersInKnown) == common.MinMasterHostsNeededForInstallation {
		return boolValue(true)
	}

	if ok && len(mastersInKnown) > common.MinMasterHostsNeededForInstallation {
		return boolValue(false)
	}

	candidates := 0
	for _, h := range c.cluster.Hosts {
		if isValid, err := v.hostAPI.IsValidMasterCandidate(h, c.db, v.log); isValid && err == nil {
			candidates++
		}
	}

	return boolValue(candidates >= common.MinMasterHostsNeededForInstallation)
}

func (v *clusterValidator) printSufficientMastersCount(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Cluster has sufficient number of master candidates"
	case ValidationFailure:
		return fmt.Sprintf("no sufficient count of master hosts candidates expected %d",
			common.MinMasterHostsNeededForInstallation)
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) allHostsAreReadyToInstall(c *clusterPreprocessContext) validationStatus {
	foundNotKnownHost := false
	for _, host := range c.cluster.Hosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled && swag.StringValue(host.Status) != models.HostStatusKnown {
			foundNotKnownHost = true
			break
		}
	}
	return boolValue(!foundNotKnownHost)
}

func (v *clusterValidator) printAllHostsAreReadyToInstall(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "All hosts in the cluster are ready to install"
	case ValidationFailure:
		return "Cluster has hosts that are not ready to install"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isDNSDomainDefined(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.BaseDNSDomain != "")
}

func (v *clusterValidator) printIsDNSDomainDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "Base DNS Domain is undefined"
	case ValidationSuccess:
		return "Base DNS Domain is defined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) noCidrsOverlapping(c *clusterPreprocessContext) validationStatus {
	if c.cluster.MachineNetworkCidr == "" || c.cluster.ClusterNetworkCidr == "" || c.cluster.ServiceNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(network.VerifyClusterCIDRsNotOverlap(c.cluster.MachineNetworkCidr, c.cluster.ClusterNetworkCidr, c.cluster.ServiceNetworkCidr) == nil)
}

func (v *clusterValidator) printNoCidrsOverlapping(c *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "No CIDRS overlapping"
	case ValidationFailure:
		if err := network.VerifyClusterCIDRsNotOverlap(c.cluster.MachineNetworkCidr, c.cluster.ClusterNetworkCidr, c.cluster.ServiceNetworkCidr); err != nil {
			return fmt.Sprintf("CIDRS Overlapping: %s", err.Error())
		}
		return ""
	case ValidationPending:
		return "At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) isPullSecretSet(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.PullSecretSet)
}

func (v *clusterValidator) printIsPullSecretSet(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "Pull secret is not set"
	case ValidationSuccess:
		return "Pull secret is set"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *clusterValidator) networkPrefixValid(c *clusterPreprocessContext) validationStatus {
	if c.cluster.ClusterNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(network.VerifyNetworkHostPrefix(c.cluster.ClusterNetworkHostPrefix) == nil &&
		network.VerifyClusterCidrSize(int(c.cluster.ClusterNetworkHostPrefix), c.cluster.ClusterNetworkCidr, len(c.cluster.Hosts)) == nil)
}

func (v *clusterValidator) printNetworkPrefixValid(c *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Cluster Network Prefix valid"
	case ValidationFailure:
		var err error
		if err = network.VerifyNetworkHostPrefix(c.cluster.ClusterNetworkHostPrefix); err != nil {
			return fmt.Sprintf("Invalid cluster network prefix: %s", err.Error())
		} else if err = network.VerifyClusterCidrSize(int(c.cluster.ClusterNetworkHostPrefix), c.cluster.ClusterNetworkCidr, len(c.cluster.Hosts)); err != nil {
			return err.Error()
		}
		return ""
	case ValidationPending:
		return "Cluster Network CIDR is undefined"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}
