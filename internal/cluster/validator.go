package cluster

import (
	"encoding/json"
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
	ValidationSuccess             validationStatus = "success"
	ValidationFailure             validationStatus = "failure"
	ValidationPending             validationStatus = "pending"
	MaximumAllowedTimeDiffMinutes                  = 4
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
	cluster, err := common.GetClusterFromDBWithoutDisabledHosts(c.db, c.clusterId)
	if err == nil {
		c.cluster = cluster
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
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	return boolValue(c.cluster.MachineNetworkCidr != "")
}

func (v *clusterValidator) printIsMachineCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			return "The Machine Network CIDR is undefined; setting the Machine Network CIDR initiates the VIPs DHCP lease allocation."
		} else {
			return "The Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs."
		}
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "No Machine Network CIDR needed: User Managed Networking"
		}
		return "The Machine Network CIDR is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isClusterCidrDefined(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.ClusterNetworkCidr != "")
}

func (v *clusterValidator) printIsClusterCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "The Cluster Network CIDR is undefined."
	case ValidationSuccess:
		return "The Cluster Network CIDR is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isServiceCidr(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.ServiceNetworkCidr != "")
}

func (v *clusterValidator) printIsServiceCidrDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "The Service Network CIDR is undefined."
	case ValidationSuccess:
		return "The Service Network CIDR is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isMachineCidrEqualsToCalculatedCidr(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
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
		return "The Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined."
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "The Cluster Machine CIDR is not required: User Managed Networking"
		}
		return "The Cluster Machine CIDR is equivalent to the calculated CIDR."
	case ValidationFailure:
		return fmt.Sprintf("The Cluster Machine CIDR %s is different than the calculated CIDR %s.", context.cluster.MachineNetworkCidr, context.calculateCidr)
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isApiVipDefined(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	if swag.BoolValue(c.cluster.VipDhcpAllocation) && c.cluster.MachineNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(c.cluster.APIVip != "")
}

func (v *clusterValidator) printIsApiVipDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "The Machine Network CIDR is undefined"
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			if isDhcpLeaseAllocationTimedOut(context) {
				return "The API virtual IP is undefined; IP allocation from the DHCP server timed out."
			} else {
				return "The API virtual IP is undefined; after the Machine Network CIDR has been defined, the API virtual IP is received from a DHCP lease allocation task which may take up to 2 minutes."
			}
		} else {
			return "The API virtual IP is undefined and must be provided."
		}
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "The API virtual IP is not required: User Managed Networking"
		}
		return "The API virtual IP is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isApiVipValid(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
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
		return "The API virtual IP is undefined."
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "The API virtual IP is not required: User Managed Networking"
		}
		return fmt.Sprintf("%s %s belongs to the Machine CIDR and is not in use.", ApiVipName, context.cluster.APIVip)
	case ValidationFailure:
		return fmt.Sprintf("%s %s does not belong to the Machine CIDR or is already in use.", ApiVipName, context.cluster.APIVip)
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isIngressVipDefined(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	if swag.BoolValue(c.cluster.VipDhcpAllocation) && c.cluster.MachineNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(c.cluster.IngressVip != "")
}

func (v *clusterValidator) printIsIngressVipDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationPending:
		return "The Machine Network CIDR is undefined"
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			if isDhcpLeaseAllocationTimedOut(context) {
				return "The Ingress virtual IP is undefined; IP allocation from the DHCP server timed out."
			} else {
				return "The Ingress virtual IP is undefined; after the Machine Network CIDR has been defined, the Ingress virtual IP is received from a DHCP lease allocation task which may take up to 2 minutes."
			}
		} else {
			return "The Ingress virtual IP is undefined and must be provided."
		}
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "The Ingress virtual IP is not required: User Managed Networking"
		}
		return "The Ingress virtual IP is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}
func (v *clusterValidator) isIngressVipValid(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
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
		return "The Ingress virtual IP is undefined."
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "The Ingress virtual IP is not required: User Managed Networking"
		}
		return fmt.Sprintf("%s %s belongs to the Machine CIDR and is not in use.", IngressVipName, context.cluster.IngressVip)
	case ValidationFailure:
		return fmt.Sprintf("%s %s does not belong to the Machine CIDR or is already in use.", IngressVipName, context.cluster.IngressVip)
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

// conditions to have a valid number of masters
// 1. have exactly three masters
// 2. have less then 3 masters but enough to auto-assign hosts that can become masters
// 3. have at least 2 workers or auto-assign hosts that can become workers, if workers configured
// 4. having more then 3 known masters is illegal
func (v *clusterValidator) sufficientMastersCount(c *clusterPreprocessContext) validationStatus {
	minMastersNeededForInstallation := common.MinMasterHostsNeededForInstallation
	nonHAMode := swag.StringValue(c.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
	if nonHAMode {
		minMastersNeededForInstallation = common.AllowedNumberOfMasterHostsInNoneHaMode
	}

	hosts := make([]*models.Host, 0)
	for k, h := range MapHostsByStatus(c.cluster) {
		if k != models.HostStatusDisabled {
			hosts = append(hosts, h...)
		}
	}
	masters := make([]*models.Host, 0)
	workers := make([]*models.Host, 0)
	candidates := make([]*models.Host, 0)

	for _, host := range hosts {
		switch role := host.Role; role {
		case models.HostRoleMaster:
			//add pre-assigned master hosts to the masters list
			masters = append(masters, host)
		case models.HostRoleWorker:
			//add pre-assigned worker hosts to the worker list
			workers = append(workers, host)
		default:
			//auto-assign hosts and other types go to the candidate list
			candidates = append(candidates, host)
		}
	}

	for _, h := range candidates {
		//if allocated masters count is less than the desired count, find eligable hosts
		//from the candidate pool to match the master count criteria, up to 3
		if len(masters) < minMastersNeededForInstallation {
			if isValid, err := v.hostAPI.IsValidMasterCandidate(h, c.db, v.log); isValid && err == nil {
				masters = append(masters, h)
				continue
			}
		}
		//otherwise, add the host candidate to the worker count
		workers = append(workers, h)
	}

	//validate master candidates count
	if len(masters) != minMastersNeededForInstallation {
		return boolValue(false)
	}

	//validate worker candidates count
	if len(workers) == common.IllegalWorkerHostsCount {
		return boolValue(false)
	}

	// if non ha mode, none workers allowed
	if nonHAMode && len(workers) != common.AllowedNumberOfWorkersInNoneHaMode {
		return boolValue(false)
	}

	return boolValue(true)
}

func (v *clusterValidator) printSufficientMastersCount(context *clusterPreprocessContext, status validationStatus) string {
	noneHAMode := swag.StringValue(context.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
	switch status {
	case ValidationSuccess:
		return "The cluster has a sufficient number of master candidates."
	case ValidationFailure:
		if noneHAMode {
			return "Single-node clusters must have a single master node and no workers."
		}
		return fmt.Sprintf("Clusters with less than %d dedicated masters or a single worker are not supported. Please either add hosts, or disable the worker host",
			common.MinMasterHostsNeededForInstallation)
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
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
		return "All hosts in the cluster are ready to install."
	case ValidationFailure:
		return "The cluster has hosts that are not ready to install."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isDNSDomainDefined(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.BaseDNSDomain != "")
}

func (v *clusterValidator) printIsDNSDomainDefined(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "The base domain is undefined and must be provided."
	case ValidationSuccess:
		return "The base domain is defined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) noCidrsOverlapping(c *clusterPreprocessContext) validationStatus {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		if c.cluster.ClusterNetworkCidr == "" || c.cluster.ServiceNetworkCidr == "" {
			return ValidationPending
		}
	} else {
		if c.cluster.MachineNetworkCidr == "" || c.cluster.ClusterNetworkCidr == "" || c.cluster.ServiceNetworkCidr == "" {
			return ValidationPending
		}
	}
	return boolValue(network.VerifyClusterCIDRsNotOverlap(c.cluster.MachineNetworkCidr, c.cluster.ClusterNetworkCidr, c.cluster.ServiceNetworkCidr, swag.BoolValue(c.cluster.UserManagedNetworking)) == nil)
}

func (v *clusterValidator) printNoCidrsOverlapping(c *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "No CIDRS are overlapping."
	case ValidationFailure:
		if err := network.VerifyClusterCIDRsNotOverlap(c.cluster.MachineNetworkCidr, c.cluster.ClusterNetworkCidr, c.cluster.ServiceNetworkCidr, swag.BoolValue(c.cluster.UserManagedNetworking)); err != nil {
			return fmt.Sprintf("CIDRS Overlapping: %s.", err.Error())
		}
		return ""
	case ValidationPending:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "At least one of the CIDRs (Cluster Network, Service Network) is undefined."
		}
		return "At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isPullSecretSet(c *clusterPreprocessContext) validationStatus {
	return boolValue(c.cluster.PullSecretSet)
}

func (v *clusterValidator) printIsPullSecretSet(context *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationFailure:
		return "The pull secret is not set."
	case ValidationSuccess:
		return "The pull secret is set."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
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
		return "The Cluster Network prefix is valid."
	case ValidationFailure:
		var err error
		if err = network.VerifyNetworkHostPrefix(c.cluster.ClusterNetworkHostPrefix); err != nil {
			return fmt.Sprintf("Invalid Cluster Network prefix: %s.", err.Error())
		} else if err = network.VerifyClusterCidrSize(int(c.cluster.ClusterNetworkHostPrefix), c.cluster.ClusterNetworkCidr, len(c.cluster.Hosts)); err != nil {
			return err.Error()
		}
		return ""
	case ValidationPending:
		return "The Cluster Network CIDR is undefined."
	default:
		return fmt.Sprintf("Unexpected status %s.", status)
	}
}

func (v *clusterValidator) isNtpServerConfigured(c *clusterPreprocessContext) validationStatus {
	var min int64
	var max int64
	for _, h := range c.cluster.Hosts {
		var inventory models.Inventory
		if err := json.Unmarshal([]byte(h.Inventory), &inventory); err != nil {
			v.log.WithError(err).Warnf("Illegal inventory for host %s", h.ID.String())
			continue
		}
		if inventory.Timestamp == 0 || *h.Status == models.HostStatusDisconnected ||
			*h.Status == models.HostStatusDisabled || *h.Status == models.HostStatusResettingPendingUserAction ||
			*h.Status == models.HostStatusDiscovering {
			continue
		}

		if inventory.Timestamp < min || min == 0 {
			min = inventory.Timestamp
		}
		if inventory.Timestamp > max {
			max = inventory.Timestamp
		}
	}
	if (max-min)/60 > MaximumAllowedTimeDiffMinutes {
		return ValidationFailure
	}
	return ValidationSuccess
}

func (v *clusterValidator) printNtpServerConfigured(c *clusterPreprocessContext, status validationStatus) string {
	switch status {
	case ValidationSuccess:
		return "No ntp problems found"
	case ValidationFailure:
		return "Host clocks are not synchronized, please configure an NTP server via DHCP."
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}
