package cluster

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/hashicorp/go-multierror"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
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
	ApiVipsName     = "api vips"
	IngressVipsName = "ingress vips"
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
	var multiErr error

	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "The Cluster Machine CIDR is not required: User Managed Networking"
	}
	if len(c.cluster.APIVips) == 0 && len(c.cluster.IngressVips) == 0 {
		return ValidationPending, "The Machine Network CIDR, API virtual IPs, or Ingress virtual IPs are undefined."
	}
	if !c.hasHostsWithInventories {
		return ValidationPending, "Hosts have not been discovered yet"
	}

	for i := range c.cluster.APIVips {
		cidr, err := network.CalculateMachineNetworkCIDR(network.GetApiVipById(c.cluster, i), network.GetIngressVipById(c.cluster, i), c.cluster.Hosts, true)
		if err != nil {
			multiErr = multierror.Append(multiErr, err)
			continue
		}
		c.calculateCidr = cidr
		machineCidrAvailable := network.IsMachineCidrAvailable(c.cluster)
		if machineCidrAvailable {
			if cidr != network.GetMachineCidrById(c.cluster, i) {
				multiErr = multierror.Append(multiErr, errors.Errorf("The Cluster Machine CIDR %s is different than the calculated CIDR %s.", network.GetMachineCidrById(c.cluster, i), c.calculateCidr))
			}
		}
	}
	if multiErr != nil && !strings.Contains(multiErr.Error(), "0 errors occurred") {
		return ValidationFailure, multiErr.Error()
	}
	return ValidationSuccess, "The Cluster Machine CIDR is equivalent to the calculated CIDR."
}

type VipsWrapper interface {
	Name() string
	Len() int
	IP(index int) string
	Verification(index int) *models.VipVerification
	GetVips() []string
}

type ApiVipsWrapper struct {
	c *clusterPreprocessContext
}

func (a *ApiVipsWrapper) Name() string        { return "API" }
func (a *ApiVipsWrapper) Len() int            { return len(a.c.cluster.APIVips) }
func (a *ApiVipsWrapper) IP(index int) string { return string(a.c.cluster.APIVips[index].IP) }
func (a *ApiVipsWrapper) Verification(index int) *models.VipVerification {
	return a.c.cluster.APIVips[index].Verification
}
func (a *ApiVipsWrapper) GetVips() []string {
	return funk.Map(a.c.cluster.APIVips, func(x *models.APIVip) string { return string(x.IP) }).([]string)
}

type IngressVipsWrapper struct {
	c *clusterPreprocessContext
}

func (i *IngressVipsWrapper) Name() string        { return "Ingress" }
func (i *IngressVipsWrapper) Len() int            { return len(i.c.cluster.IngressVips) }
func (i *IngressVipsWrapper) IP(index int) string { return string(i.c.cluster.IngressVips[index].IP) }
func (i *IngressVipsWrapper) Verification(index int) *models.VipVerification {
	return i.c.cluster.IngressVips[index].Verification
}
func (i *IngressVipsWrapper) GetVips() []string {
	return funk.Map(i.c.cluster.IngressVips, func(x *models.IngressVip) string { return string(x.IP) }).([]string)
}

func (v *clusterValidator) areVipsDefined(c *clusterPreprocessContext, vipsWrapper VipsWrapper) (ValidationStatus, string) {
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, fmt.Sprintf("%s virtual IPs are not required: User Managed Networking", vipsWrapper.Name())
	}
	if swag.StringValue(c.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		return ValidationSuccess, fmt.Sprintf("%s virtual IPs are not required: SNO", vipsWrapper.Name())
	}
	if vipsWrapper.Len() > 0 {
		return ValidationSuccess, fmt.Sprintf("%s virtual IPs are defined.", vipsWrapper.Name())
	}
	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if swag.BoolValue(c.cluster.VipDhcpAllocation) {
		if !validationStatusToBool(machineCidrDefined) {
			return ValidationPending, "The Machine Network CIDR is undefined"
		}
		if isDhcpLeaseAllocationTimedOut(c) {
			return ValidationFailure, fmt.Sprintf("%s virtual IPs are undefined; IP allocation from the DHCP server timed out.", vipsWrapper.Name())
		}
		return ValidationFailure, fmt.Sprintf("%[1]s virtual IPs are undefined; after the Machine Network CIDR has been defined, %[1]s virtual IPs are received from a DHCP lease allocation task which may take up to 2 minutes.",
			vipsWrapper.Name())

	}
	return ValidationFailure, fmt.Sprintf("%s virtual IPs are undefined and must be provided.", vipsWrapper.Name())
}

func (v *clusterValidator) areApiVipsDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	return v.areVipsDefined(c, &ApiVipsWrapper{c: c})
}

func (v *clusterValidator) areVipsValid(c *clusterPreprocessContext, vipsWrapper VipsWrapper) (ValidationStatus, string) {
	var (
		multiErr *multierror.Error
		msg      string
	)

	name := strings.ToLower(vipsWrapper.Name()) + " vips"
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, fmt.Sprintf("%s virtual IPs are not required: User Managed Networking", vipsWrapper.Name())
	}
	if swag.StringValue(c.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		return ValidationSuccess, fmt.Sprintf("%s virtual IPs are not required: SNO", vipsWrapper.Name())
	}

	if vipsWrapper.Len() == 0 {
		return ValidationPending, fmt.Sprintf("%s virtual IPs are undefined.", vipsWrapper.Name())
	}

	machineCidrDefined, _ := v.isMachineCidrDefined(c)
	if !c.hasHostsWithInventories || !validationStatusToBool(machineCidrDefined) {
		return ValidationPending, "Hosts have not been discovered yet"
	}

	failed := false
	for i := 0; i != vipsWrapper.Len(); i++ {
		verification, err := network.VerifyVip(c.cluster.Hosts, network.GetMachineCidrById(c.cluster, i), vipsWrapper.IP(i), name,
			vipsWrapper.Verification(i), v.log)
		failed = failed || verification != models.VipVerificationSucceeded
		if err != nil {
			multiErr = multierror.Append(multiErr, err)
		}
	}

	if multiErr != nil {
		msg = strings.Join(funk.Map(multiErr.Errors, func(e error) string { return e.Error() }).([]string), ";")
	}
	if failed {
		return ValidationFailure, msg
	}

	return ValidationSuccess, fmt.Sprintf("%s %s belongs to the Machine CIDR and is not in use.", name, strings.Join(vipsWrapper.GetVips(), `, `))
}

func (v *clusterValidator) areApiVipsValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	return v.areVipsValid(c, &ApiVipsWrapper{c: c})
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

func (v *clusterValidator) areIngressVipsDefined(c *clusterPreprocessContext) (ValidationStatus, string) {
	return v.areVipsDefined(c, &IngressVipsWrapper{c: c})
}

func (v *clusterValidator) areIngressVipsValid(c *clusterPreprocessContext) (ValidationStatus, string) {
	return v.areVipsValid(c, &IngressVipsWrapper{c: c})
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
		message = "The cluster has the exact amount of dedicated control plane nodes."
	case ValidationFailure:
		message = fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.", common.MinMasterHostsNeededForInstallation)
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

func (v *clusterValidator) platformRequirementsSatisfied(c *clusterPreprocessContext) (ValidationStatus, string) {
	// If cluster platform type is not OCI ignore that validation
	if c.cluster.Platform != nil && common.PlatformTypeValue(c.cluster.Platform.Type) != models.PlatformTypeOci {
		return ValidationSuccess, "Platform requirements satisfied"
	}

	usages, err := usage.Unmarshal(c.cluster.Cluster.FeatureUsage)
	if err != nil {
		v.log.Errorf("platform validation failure, failed to parse feature usages, %s", err.Error())
		return ValidationFailure, "Failed to parse feature usages"
	}

	for _, usg := range usages {
		if usg.Name == usage.CustomManifest {
			return ValidationSuccess, "Platform requirements satisfied"
		}
	}

	return ValidationFailure, "The custom manifest required for Oracle Cloud Infrastructure platform integration has not been added. Add a custom manifest to continue."
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
		return clusterNetwork != nil && (v.skipNetworkHostPrefixCheck(c, clusterNetwork.HostPrefix) ||
			network.VerifyNetworkHostPrefix(clusterNetwork.HostPrefix) == nil &&
				network.VerifyClusterCidrSize(int(clusterNetwork.HostPrefix), string(clusterNetwork.Cidr), len(c.cluster.Hosts)) == nil)
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

// Ignore hostPrefix set to 0 for non-OVN/SDN plugins.
func (v *clusterValidator) skipNetworkHostPrefixCheck(c *clusterPreprocessContext, prefix int64) bool {
	// list of known plugins that require hostPrefix to be set
	var pluginsUsingHostPrefix = []string{models.ClusterNetworkTypeOVNKubernetes, models.ClusterNetworkTypeOpenShiftSDN}

	if prefix != 0 {
		return false
	}

	if !funk.ContainsString(pluginsUsingHostPrefix, swag.StringValue(c.cluster.NetworkType)) {
		v.log.Infof("skipping network prefix check for %s", swag.StringValue(c.cluster.NetworkType))
		return true
	}

	// Also check networkType in install-config overrides
	if c.cluster.InstallConfigOverrides != "" {
		overrideDecoder := json.NewDecoder(strings.NewReader(c.cluster.InstallConfigOverrides))
		overrideDecoder.DisallowUnknownFields()

		cfg := &installcfg.InstallerConfigBaremetal{}
		if overrideDecoder.Decode(cfg) != nil {
			v.log.Infof("could not decode install-config overrides %s", c.cluster.InstallConfigOverrides)
			return false
		}

		if cfg.Networking.NetworkType != "" && !funk.ContainsString(pluginsUsingHostPrefix, cfg.Networking.NetworkType) {
			v.log.Infof("skipping network prefix check for %s", cfg.Networking.NetworkType)
			return true
		}
	}
	return false
}
