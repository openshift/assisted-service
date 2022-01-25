package cluster

import (
	"fmt"
	"reflect"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type ValidationStatus string

const (
	ValidationSuccess ValidationStatus = "success"
	ValidationFailure ValidationStatus = "failure"
	ValidationPending ValidationStatus = "pending"
	ValidationError   ValidationStatus = "error"
)

const (
	ApiVipName     = "api vip"
	IngressVipName = "ingress vip"
)

func (v ValidationStatus) String() string {
	return string(v)
}

type clusterPreprocessContext struct {
	clusterId               strfmt.UUID
	cluster                 *common.Cluster
	db                      *gorm.DB
	calculateCidr           string
	hasHostsWithInventories bool
}

type validationConditon func(context *clusterPreprocessContext) (ValidationStatus, string)

type validation struct {
	id        ValidationID
	condition validationConditon
}

func hasHostsWithInventories(c *common.Cluster) bool {
	for _, h := range c.Hosts {
		if h.Inventory != "" {
			return true
		}
	}
	return false
}

func newClusterValidationContext(c *common.Cluster, db *gorm.DB) *clusterPreprocessContext {
	return &clusterPreprocessContext{
		clusterId:               *c.ID,
		cluster:                 c,
		db:                      db,
		hasHostsWithInventories: hasHostsWithInventories(c),
	}
}

func isDhcpLeaseAllocationTimedOut(c *clusterPreprocessContext) bool {
	return c.cluster.MachineNetworkCidrUpdatedAt.String() != "" && time.Since(c.cluster.MachineNetworkCidrUpdatedAt) > DhcpLeaseTimeoutMinutes*time.Minute
}

func validationStatusToBool(v ValidationStatus) bool {
	return v == ValidationSuccess
}

type clusterValidator struct {
	log     logrus.FieldLogger
	hostAPI host.API
}

func (v *clusterValidator) isMachineCidrDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) && !common.IsSingleNodeCluster(c.cluster) {
		return ValidationSuccess, "No Machine Network CIDR needed: User Managed Networking"
	} else if network.IsMachineCidrAvailable(c.cluster) {
		return ValidationSuccess, "The Machine Network CIDR is defined."
	}
	if !c.hasHostsWithInventories {
		return ValidationPending, "Hosts have not been discovered yet"
	}
	if swag.BoolValue(c.cluster.VipDhcpAllocation) {
		return ValidationFailure, "The Machine Network CIDR is undefined; setting the Machine Network CIDR initiates the VIPs DHCP lease allocation."
	}
	if common.IsSingleNodeCluster(c.cluster) {
		return ValidationFailure, "The Machine Network CIDR is undefined; Setting Machine Network CIDR is required for single node cluster"
	}
	return ValidationFailure, "The Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs."
}

func (v *clusterValidator) isClusterCidrDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if common.IsSliceNonEmpty(c.cluster.ClusterNetworks) {
		return ValidationSuccess, "The Cluster Network CIDR is defined."
	}
	return ValidationFailure, "The Cluster Network CIDR is undefined."
}

func (v *clusterValidator) isServiceCidrDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if common.IsSliceNonEmpty(c.cluster.ServiceNetworks) {
		return ValidationSuccess, "The Service Network CIDR is defined."
	}
	return ValidationFailure, "The Service Network CIDR is undefined."
}

func (v *clusterValidator) isMachineCidrEqualsToCalculatedCidr(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The Cluster Machine CIDR is not required: User Managed Networking"
	}
	if c.cluster.APIVip == "" && c.cluster.IngressVip == "" {
		return ValidationPending, "The Machine Network CIDR, API virtual IP, or Ingress virtual IP is undefined."
	}
	if !c.hasHostsWithInventories {
		return ValidationPending, "Hosts have not been discovered yet"
	}
	cidr, err := network.CalculateMachineNetworkCIDR(c.cluster.APIVip, c.cluster.IngressVip, c.cluster.Hosts, true)
	c.calculateCidr = cidr
	machineCidrAvailable := network.IsMachineCidrAvailable(c.cluster)
	machineCidr := ""
	if err == nil && machineCidrAvailable {
		machineCidr = network.GetMachineCidrById(c.cluster, 0)
		if machineCidr == cidr {
			return ValidationSuccess, "The Cluster Machine CIDR is equivalent to the calculated CIDR."
		}
	}
	return ValidationFailure, fmt.Sprintf("The Cluster Machine CIDR %s is different than the calculated CIDR %s.", machineCidr, c.calculateCidr)
}

func (v *clusterValidator) isApiVipDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The API virtual IP is not required: User Managed Networking"
	}
	if c.cluster.APIVip != "" {
		return ValidationSuccess, "The API virtual IP is defined."
	}
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if swag.BoolValue(c.cluster.VipDhcpAllocation) {
		if !validationStatusToBool(machineCidrDefined) {
			return ValidationPending, "The Machine Network CIDR is undefined"
		}
		if isDhcpLeaseAllocationTimedOut(c) {
			return ValidationFailure, "The API virtual IP is undefined; IP allocation from the DHCP server timed out."
		}
		return ValidationFailure, "The API virtual IP is undefined; after the Machine Network CIDR has been defined, the API virtual IP is received from a DHCP lease allocation task which may take up to 2 minutes."

	}
	return ValidationFailure, "The API virtual IP is undefined and must be provided."
}

func (v *clusterValidator) isApiVipValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The API virtual IP is not required: User Managed Networking"
	}
	if c.cluster.APIVip == "" {
		return ValidationPending, "The API virtual IP is undefined."
	}
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if !c.hasHostsWithInventories || !validationStatusToBool(machineCidrDefined) {
		return ValidationPending, "Hosts have not been discovered yet"
	}
	err := network.VerifyVip(c.cluster.Hosts, network.GetMachineCidrById(c.cluster, 0), c.cluster.APIVip, ApiVipName,
		true, v.log)
	if err == nil {
		return ValidationSuccess, fmt.Sprintf("%s %s belongs to the Machine CIDR and is not in use.", ApiVipName, c.cluster.APIVip)
	}
	return ValidationFailure, fmt.Sprintf("%s %s does not belong to the Machine CIDR or is already in use.", ApiVipName, c.cluster.APIVip)
}

func (v *clusterValidator) isNetworkTypeValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	validNetworkTypes := []string{models.ClusterNetworkTypeOVNKubernetes, models.ClusterNetworkTypeOpenShiftSDN}
	if !funk.ContainsString(validNetworkTypes, swag.StringValue(c.cluster.NetworkType)) && c.cluster.NetworkType != nil {
		return ValidationFailure, "The network type is not valid; the valid network types are OpenShiftSDN or OVNKubernetes"
	}
	if hasClusterNetworksUnsupportedByNetworkType(c.cluster) {
		return ValidationFailure, "The cluster is configured with IPv6 which is not supported by OpenShiftSDN; use OVNKubernetes instead"
	}
	if isHighAvailabilityModeUnsupportedByNetworkType(c.cluster) {
		return ValidationFailure, "High-availability mode 'None' (SNO) is not supported by OpenShiftSDN; use another network type instead"
	}
	if isVipDhcpAllocationAndOVN(c.cluster) {
		return ValidationFailure, "VIP DHCP allocation is not supported when the cluster is configured to use OVNKubernetes."
	}
	return ValidationSuccess, "The cluster has a valid network type"
}

func hasClusterNetworksUnsupportedByNetworkType(cluster *common.Cluster) bool {
	return funk.Any(funk.Filter(common.GetNetworksCidrs(cluster), func(ip *string) bool {
		if ip == nil {
			return false
		}
		return network.IsIPv6CIDR(*ip)
	})) && cluster.NetworkType != nil && swag.StringValue(cluster.NetworkType) != models.ClusterNetworkTypeOVNKubernetes
}

func isHighAvailabilityModeUnsupportedByNetworkType(cluster *common.Cluster) bool {
	return swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone &&
		cluster.NetworkType != nil && swag.StringValue(cluster.NetworkType) == models.ClusterNetworkTypeOpenShiftSDN
}

func isVipDhcpAllocationAndOVN(cluster *common.Cluster) bool {
	isVipDhcpAllocation := swag.BoolValue(cluster.VipDhcpAllocation)
	isNetworkTypeOVN := swag.StringValue(cluster.NetworkType) == models.ClusterNetworkTypeOVNKubernetes
	return isVipDhcpAllocation && isNetworkTypeOVN
}

func (v *clusterValidator) isIngressVipDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The Ingress virtual IP is not required: User Managed Networking"
	}
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if swag.BoolValue(c.cluster.VipDhcpAllocation) && !validationStatusToBool(machineCidrDefined) {
		return ValidationPending, "The Machine Network CIDR is undefined"
	}

	if c.cluster.IngressVip != "" {
		return ValidationSuccess, "The Ingress virtual IP is defined."
	}

	if swag.BoolValue(c.cluster.VipDhcpAllocation) {
		if isDhcpLeaseAllocationTimedOut(c) {
			return ValidationFailure, "The Ingress virtual IP is undefined; IP allocation from the DHCP server timed out."
		}
		return ValidationFailure, "The Ingress virtual IP is undefined; after the Machine Network CIDR has been defined, the Ingress virtual IP is received from a DHCP lease allocation task which may take up to 2 minutes."
	}
	return ValidationFailure, "The Ingress virtual IP is undefined and must be provided."

}

func (v *clusterValidator) isIngressVipValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The Ingress virtual IP is not required: User Managed Networking"
	}
	if c.cluster.IngressVip == "" {
		return ValidationPending, "The Ingress virtual IP is undefined."
	}
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if !c.hasHostsWithInventories || !validationStatusToBool(machineCidrDefined) {
		return ValidationPending, "Hosts have not been discovered yet"
	}
	err := network.VerifyVip(c.cluster.Hosts, network.GetMachineCidrById(c.cluster, 0), c.cluster.IngressVip, IngressVipName,
		true, v.log)
	if err == nil {
		return ValidationSuccess, fmt.Sprintf("%s %s belongs to the Machine CIDR and is not in use.", IngressVipName, c.cluster.IngressVip)
	}
	return ValidationFailure, fmt.Sprintf("%s %s does not belong to the Machine CIDR or is already in use.", IngressVipName, c.cluster.IngressVip)
}

// conditions to have a valid number of masters
// 1. have exactly three masters
// 2. have less then 3 masters but enough to auto-assign hosts that can become masters
// 3. have at least 2 workers or auto-assign hosts that can become workers, if workers configured
// 4. having more then 3 known masters is illegal
func (v *clusterValidator) sufficientMastersCount(c *clusterPreprocessContext) (ValidationStatus, string) {
	status := ValidationSuccess
	var message string

	minMastersNeededForInstallation := common.MinMasterHostsNeededForInstallation
	nonHAMode := swag.StringValue(c.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
	if nonHAMode {
		minMastersNeededForInstallation = common.AllowedNumberOfMasterHostsInNoneHaMode
	}

	hosts := make([]*models.Host, 0)
	for _, h := range MapHostsByStatus(c.cluster) {
		hosts = append(hosts, h...)
	}
	masters := make([]*models.Host, 0)
	workers := make([]*models.Host, 0)
	candidates := make([]*models.Host, 0)

	for _, host := range hosts {
		switch role := common.GetEffectiveRole(host); role {
		case models.HostRoleMaster:
			//add pre-assigned/suggested master hosts to the masters list
			masters = append(masters, host)
		case models.HostRoleWorker:
			//add pre-assigned/suggested worker hosts to the worker list
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
			candidate := *h
			if isValid, err := v.hostAPI.IsValidMasterCandidate(&candidate, c.cluster, c.db, v.log); isValid && err == nil {
				masters = append(masters, h)
				continue
			}
		}
		//otherwise, add the host candidate to the worker count
		workers = append(workers, h)
	}

	numWorkers := len(workers)
	if len(masters) != minMastersNeededForInstallation ||
		nonHAMode && numWorkers != common.AllowedNumberOfWorkersInNoneHaMode {
		status = ValidationFailure
	}

	switch status {
	case ValidationSuccess:
		message = "The cluster has a sufficient number of master candidates."
	case ValidationFailure:
		message = fmt.Sprintf("Clusters must have exactly %d dedicated masters and if workers are added, there should be at least 2 workers. Please check your configuration and add or remove hosts as to meet the above requirement.",
			common.MinMasterHostsNeededForInstallation)
		if nonHAMode {
			message = "Single-node clusters must have a single control plane node and no workers."
		}
	default:
		message = fmt.Sprintf("Unexpected status %s", status)
	}

	return status, message
}

func isReadyToInstall(status string) bool {
	allowedStatuses := []string{
		models.HostStatusKnown,
		models.HostStatusPreparingForInstallation,
		models.HostStatusPreparingSuccessful,
	}
	return funk.ContainsString(allowedStatuses, status)
}

func (v *clusterValidator) allHostsAreReadyToInstall(c *clusterPreprocessContext) (ValidationStatus, string) {
	readyToInstall := true
	for _, host := range c.cluster.Hosts {
		if !isReadyToInstall(swag.StringValue(host.Status)) {
			readyToInstall = false
			break
		}
	}
	if readyToInstall {
		return ValidationSuccess, "All hosts in the cluster are ready to install."
	}
	return ValidationFailure, "The cluster has hosts that are not ready to install."
}

func (v *clusterValidator) isDNSDomainDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	if c.cluster.BaseDNSDomain != "" {
		return ValidationSuccess, "The base domain is defined."
	}
	return ValidationFailure, "The base domain is undefined and must be provided."
}

func checkCidrsOverlapping(cluster *common.Cluster) error {
	//Currently, the networks arrays can hold up to 2 subnets, one for each family
	//in the same order. If machine networks is defined we assume it follows the
	//same convension
	var machineNetworkCidr, clusterNetworkCidr, serviceNetworkCidr string
	for index := range cluster.ClusterNetworks {
		if index < len(cluster.MachineNetworks) {
			machineNetworkCidr = string(cluster.MachineNetworks[index].Cidr)
		}
		clusterNetworkCidr = string(cluster.ClusterNetworks[index].Cidr)
		serviceNetworkCidr = string(cluster.ServiceNetworks[index].Cidr)

		if err := network.VerifyClusterCIDRsNotOverlap(machineNetworkCidr,
			clusterNetworkCidr, serviceNetworkCidr,
			network.IsMachineNetworkRequired(cluster)); err != nil {
			return err
		}
	}
	return nil
}

func (v *clusterValidator) noCidrsOverlapping(c *clusterPreprocessContext) (ValidationStatus, string) {
	clusterCidrDefined, _ := v.isClusterCidrDefined(c)
	serviceCidrDefined, _ := v.isServiceCidrDefined(c)
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	//If one of the required Cidr fields is empty return Pending status
	if !validationStatusToBool(clusterCidrDefined) || !validationStatusToBool(serviceCidrDefined) || (network.IsMachineNetworkRequired(c.cluster) && !validationStatusToBool(machineCidrDefined)) {
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return ValidationPending, "At least one of the CIDRs (Cluster Network, Service Network) is undefined."
		}
		return ValidationPending, "At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined."
	}
	if len(c.cluster.ClusterNetworks) != len(c.cluster.ServiceNetworks) {
		// TODO MGMT-7587: Support any number of subnets
		// Assumes that the number of cluster networks equal to the number of service networks
		return ValidationError, "A mismatch between the number of Cluster and Service networks"
	}
	if err := checkCidrsOverlapping(c.cluster); err != nil {
		return ValidationFailure, fmt.Sprintf("CIDRS Overlapping: %s.", err.Error())
	}
	return ValidationSuccess, "No CIDRS are overlapping."
}

func (v *clusterValidator) isNetworksSameAddressFamilies(c *clusterPreprocessContext) (ValidationStatus, string) {
	var clusterCidrDefined ValidationStatus
	clusterCidrDefined, _ = v.isClusterCidrDefined(c)

	var serviceCidrDefined ValidationStatus
	serviceCidrDefined, _ = v.isServiceCidrDefined(c)
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	machineNetworkRequired := network.IsMachineNetworkRequired(c.cluster)

	if !machineNetworkRequired && (!validationStatusToBool(clusterCidrDefined) || !validationStatusToBool(serviceCidrDefined)) {
		return ValidationPending, "At least one of the CIDRs (Cluster Network, Service Network) is undefined."
	}

	if machineNetworkRequired && (!validationStatusToBool(machineCidrDefined) || !validationStatusToBool(clusterCidrDefined) || !validationStatusToBool(serviceCidrDefined)) {
		return ValidationPending, "At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined."
	}

	serviceNetworkFamilies, err := network.CidrsToAddressFamilies(network.GetServiceNetworkCidrs(c.cluster))
	if err != nil {
		v.log.WithError(err).Errorf("Getting service address families for cluster %s", c.cluster.ID.String())
		return ValidationError, "Bad CIDR(s) appears in one of the networks"
	}
	clusterNetworkFamilies, err := network.CidrsToAddressFamilies(network.GetClusterNetworkCidrs(c.cluster))
	if err != nil {
		v.log.WithError(err).Errorf("Getting cluster address families for cluster %s", c.cluster.ID.String())
		return ValidationError, "Bad CIDR(s) appears in one of the networks"
	}
	// serviceNetworkFamilies should only ever have a maximum of two distinct families.
	// clusterNetworkFamilies may have multiple indistinct families so need to be reduced to a maximum of two distinct families before comparison.
	clusterNetworkFamilies = network.CanonizeAddressFamilies(clusterNetworkFamilies)
	if !reflect.DeepEqual(serviceNetworkFamilies, clusterNetworkFamilies) {
		return ValidationFailure, "Address families of networks (ServiceNetworks, ClusterNetworks) are not the same."
	}
	if machineNetworkRequired {
		machineNetworkFamilies, err := network.CidrsToAddressFamilies(network.GetMachineNetworkCidrs(c.cluster))
		if err != nil {
			v.log.WithError(err).Errorf("Getting machine address families for cluster %s", c.cluster.ID.String())
			return ValidationError, fmt.Sprintf("Error getting machine address families for cluster %s", c.cluster.ID.String())
		}
		machineNetworkFamilies = network.CanonizeAddressFamilies(machineNetworkFamilies)
		if !reflect.DeepEqual(serviceNetworkFamilies, machineNetworkFamilies) {
			return ValidationFailure, "Address families of networks (MachineNetworks, ServiceNetworks, ClusterNetworks) are not the same."
		}
	}
	return ValidationSuccess, "Same address families for all networks."
}

func (v *clusterValidator) isPullSecretSet(c *clusterPreprocessContext) (ValidationStatus, string) {
	if c.cluster.PullSecretSet {
		return ValidationSuccess, "The pull secret is set."
	}
	return ValidationFailure, "The pull secret is not set."
}

func (v *clusterValidator) networkPrefixValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	var clusterCidrDefined ValidationStatus
	clusterCidrDefined, _ = v.isClusterCidrDefined(c)

	if !validationStatusToBool(clusterCidrDefined) {
		return ValidationPending, "The Cluster Network CIDR is undefined."
	}
	validClusterNetworks := funk.Filter(c.cluster.ClusterNetworks, func(clusterNetwork *models.ClusterNetwork) bool {
		return clusterNetwork != nil &&
			network.VerifyNetworkHostPrefix(clusterNetwork.HostPrefix) == nil &&
			network.VerifyClusterCidrSize(int(clusterNetwork.HostPrefix), string(clusterNetwork.Cidr), len(c.cluster.Hosts)) == nil
	}).([]*models.ClusterNetwork)

	if len(validClusterNetworks) == len(c.cluster.ClusterNetworks) {
		return ValidationSuccess, "The Cluster Network prefix is valid."
	}

	var err error
	for _, clusterNetwork := range c.cluster.ClusterNetworks {
		if err = network.VerifyNetworkHostPrefix(clusterNetwork.HostPrefix); err != nil {
			return ValidationFailure, fmt.Sprintf("Invalid Cluster Network prefix: %s.", err.Error())
		}
		if err = network.VerifyClusterCidrSize(int(clusterNetwork.HostPrefix), string(clusterNetwork.Cidr), len(c.cluster.Hosts)); err != nil {
			return ValidationFailure, err.Error()
		}
	}
	return ValidationError, "Unexpected status ValidationError"
}

func (v *clusterValidator) isNtpServerConfigured(c *clusterPreprocessContext) (ValidationStatus, string) {
	synced, err := common.IsNtpSynced(c.cluster)
	if err != nil {
		return ValidationError, fmt.Sprintf("Unexpected error %s", err.Error())
	}
	if synced {
		return ValidationSuccess, "No ntp problems found"
	}
	return ValidationFailure, fmt.Sprintf("Hosts' clocks are not synchronized (there's more than a %d minutes gap between clocks), "+
		"please configure an NTP server via DHCP or set clocks manually.", common.MaximumAllowedTimeDiffMinutes)
}
