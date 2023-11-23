package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/ignition/v2/config/v3_2"
	ignition_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"github.com/vincent-petithory/dataurl"
	"gorm.io/gorm"
	"sigs.k8s.io/yaml"
)

type ValidationStatus string

const (
	ValidationSuccess               ValidationStatus = "success"
	ValidationSuccessSuppressOutput ValidationStatus = "success-suppress-output"
	ValidationFailure               ValidationStatus = "failure"
	ValidationPending               ValidationStatus = "pending"
	ValidationError                 ValidationStatus = "error"
	ValidationDisabled              ValidationStatus = "disabled"
	maxServiceAheadOfHostTimeDiff                    = 20 * time.Minute
	maxHostAheadOfServiceTimeDiff                    = 1 * time.Hour
	maxHostTimingMetrics                             = 4
	maxPingCommandExamples                           = 4
)

const FailedToFindAction = "failed to find action for step"
const OpenStackPlatform = "OpenStack Compute"

var (
	ImageStatusDownloadRateThreshold = 0.001

	invalidPlatforms = []string{
		OpenStackPlatform,
	}
)

func (v ValidationStatus) String() string {
	return string(v)
}

type InventoryCache map[string]*models.Inventory

func (inventoryCache InventoryCache) GetOrUnmarshal(host *models.Host) (inventory *models.Inventory, err error) {
	if host.Inventory == "" {
		return nil, nil
	}
	var ok bool
	key := host.ID.String() + "@" + host.InfraEnvID.String()
	inventory, ok = inventoryCache[key]
	if !ok {
		inventory, err = common.UnmarshalInventory(host.Inventory)
		if err != nil {
			return
		}
		inventoryCache[key] = inventory
	}
	return

}

type validationContext struct {
	host                    *models.Host
	cluster                 *common.Cluster
	infraEnv                *common.InfraEnv
	inventory               *models.Inventory
	db                      *gorm.DB
	inventoryCache          InventoryCache
	clusterHostRequirements *models.ClusterHostRequirements
	minCPUCoresRequirement  int64
	minRAMMibRequirement    int64
	kubeApiEnabled          bool
	objectHandler           s3wrapper.API
	ctx                     context.Context
}

type validationCondition func(context *validationContext) (ValidationStatus, string)

type validation struct {
	id            validationID
	condition     validationCondition
	skippedStates []models.HostStage
}

func (c *validationContext) loadCluster() error {
	var err error
	if c.cluster == nil {
		c.cluster, err = common.GetClusterFromDBWithHosts(c.db, *c.host.ClusterID)
	}
	return err
}

func (c *validationContext) loadInfraEnv() error {
	var err error
	if c.infraEnv == nil {
		c.infraEnv, err = common.GetInfraEnvFromDB(c.db, c.host.InfraEnvID)
	}
	return err
}

func (c *validationContext) loadInventory() error {
	inventory, err := c.inventoryCache.GetOrUnmarshal(c.host)
	if inventory == nil || err != nil {
		return err
	}
	if inventory.Memory == nil {
		return errors.New("Inventory is not valid, Memory not detected")
	}

	if inventory.CPU == nil {
		return errors.New("Inventory is not valid, CPU not detected")
	}
	c.inventory = inventory
	return nil
}

func (v *validator) getBootDeviceInfo(host *models.Host) (*models.DiskInfo, error) {
	bootDevice, err := hardware.GetBootDevice(v.hwValidator, host)
	if err != nil {
		return nil, err
	}
	info, err := common.GetDiskInfo(host.DisksInfo, bootDevice)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (c *validationContext) validateRole() error {
	switch common.GetEffectiveRole(c.host) {
	case models.HostRoleMaster, models.HostRoleWorker, models.HostRoleAutoAssign:
		return nil
	default:
		return errors.Errorf("Illegal role defined: %s", common.GetEffectiveRole(c.host))
	}
}

func (c *validationContext) validateMachineCIDR() error {
	var err error
	for _, machineNetwork := range c.cluster.MachineNetworks {
		_, _, err = net.ParseCIDR(string(machineNetwork.Cidr))
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *validationContext) loadClusterHostRequirements(hwValidator hardware.Validator) error {
	requirements, err := hwValidator.GetClusterHostRequirements(context.TODO(), c.cluster, c.host)
	c.clusterHostRequirements = requirements
	return err
}

func (c *validationContext) loadInfraEnvHostRequirements(hwValidator hardware.Validator) error {
	requirements, err := hwValidator.GetInfraEnvHostRequirements(context.TODO(), c.infraEnv)
	c.clusterHostRequirements = requirements
	return err
}

func (c *validationContext) loadGeneralMinRequirements(hwValidator hardware.Validator) error {
	requirements, err := hwValidator.GetPreflightHardwareRequirements(context.TODO(), c.cluster)
	if err != nil {
		return err
	}

	c.minCPUCoresRequirement = int64(math.Min(float64(requirements.Ocp.Master.Quantitative.CPUCores), float64(requirements.Ocp.Worker.Quantitative.CPUCores)))
	c.minRAMMibRequirement = int64(math.Min(float64(requirements.Ocp.Master.Quantitative.RAMMib), float64(requirements.Ocp.Worker.Quantitative.RAMMib)))

	return err
}

func (c *validationContext) loadGeneralInfraEnvMinRequirements(hwValidator hardware.Validator) error {
	requirements, err := hwValidator.GetPreflightInfraEnvHardwareRequirements(context.TODO(), c.infraEnv)
	if err != nil {
		return err
	}

	c.minCPUCoresRequirement = int64(math.Min(float64(requirements.Ocp.Master.Quantitative.CPUCores), float64(requirements.Ocp.Worker.Quantitative.CPUCores)))
	c.minRAMMibRequirement = int64(math.Min(float64(requirements.Ocp.Master.Quantitative.RAMMib), float64(requirements.Ocp.Worker.Quantitative.RAMMib)))

	return err
}

func newValidationContext(ctx context.Context, host *models.Host, c *common.Cluster, i *common.InfraEnv, db *gorm.DB, inventoryCache InventoryCache, hwValidator hardware.Validator, kubeApiEnabled bool, objectHandler s3wrapper.API) (*validationContext, error) {
	ret := &validationContext{
		ctx:            ctx,
		host:           host,
		db:             db,
		cluster:        c,
		infraEnv:       i,
		inventoryCache: inventoryCache,
		kubeApiEnabled: kubeApiEnabled,
		objectHandler:  objectHandler,
	}
	if host.ClusterID != nil {
		err := ret.loadCluster()
		if err != nil {
			return nil, err
		}
		err = ret.loadInventory()
		if err != nil {
			return nil, err
		}
		err = ret.validateRole()
		if err != nil {
			return nil, err
		}
		err = ret.validateMachineCIDR()
		if err != nil {
			return nil, err
		}
		err = ret.loadClusterHostRequirements(hwValidator)
		if err != nil {
			return nil, err
		}
		err = ret.loadGeneralMinRequirements(hwValidator)
		if err != nil {
			return nil, err
		}
	} else {
		err := ret.loadInfraEnv()
		if err != nil {
			return nil, err
		}
		err = ret.loadInventory()
		if err != nil {
			return nil, err
		}
		err = ret.validateRole()
		if err != nil {
			return nil, err
		}
		err = ret.loadInfraEnvHostRequirements(hwValidator)
		if err != nil {
			return nil, err
		}

		err = ret.loadGeneralInfraEnvMinRequirements(hwValidator)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func boolValue(b bool) ValidationStatus {
	if b {
		return ValidationSuccess
	} else {
		return ValidationFailure
	}
}

type validator struct {
	log              logrus.FieldLogger
	hwValidatorCfg   *hardware.ValidatorCfg
	hwValidator      hardware.Validator
	operatorsAPI     operators.API
	providerRegistry registry.ProviderRegistry
	versionHandler   versions.Handler
}

func (v *validator) isMediaConnected(c *validationContext) (ValidationStatus, string) {
	status := boolValue(c.host.MediaStatus == nil || *c.host.MediaStatus != models.HostMediaStatusDisconnected)
	switch status {
	case ValidationSuccess:
		return status, "Media device is connected"
	case ValidationFailure:
		return status, statusInfoMediaDisconnected
	default:
		return status, fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isConnected(c *validationContext) (ValidationStatus, string) {
	maxHostDisconnectionTime := v.hwValidatorCfg.MaxHostDisconnectionTime
	if c.host.Bootstrap {
		// In case of bootstrap we increase disconnection timeout as it's resolv.conf
		// will be recreated in the middle of installation and it can cause for dns issues
		// It can cause bootstrap to disconnect from assisted service
		maxHostDisconnectionTime = v.hwValidatorCfg.MaxHostDisconnectionTime + 2*time.Minute

	}
	rebootIndex := IndexOfStage(models.HostStageRebooting, BootstrapStages[:])
	hostIsPreReboot := c.host.Progress.CurrentStage == "" || funk.Contains(BootstrapStages[0:rebootIndex], c.host.Progress.CurrentStage)
	status := boolValue(!hostIsPreReboot || c.host.CheckedInAt.String() == "" || time.Since(time.Time(c.host.CheckedInAt)) <= maxHostDisconnectionTime)
	if status == ValidationSuccess {
		return status, "Host is connected"
	} else {
		return status, "Host is disconnected"
	}
}

func (v *validator) hasInventory(c *validationContext) (ValidationStatus, string) {
	status := boolValue(c.inventory != nil)
	switch status {
	case ValidationSuccess:
		return status, "Valid inventory exists for the host"
	case ValidationFailure:
		return status, "Inventory has not been received for the host"
	default:
		return status, fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinCpuCores(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "Missing inventory"
	}
	if c.inventory.CPU.Count >= c.minCPUCoresRequirement {
		return ValidationSuccess, "Sufficient CPU cores"
	}
	return ValidationFailure, fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is %d, found only %d", c.minCPUCoresRequirement, c.inventory.CPU.Count)

}

func (v *validator) hasMinMemory(c *validationContext) (ValidationStatus, string) {
	status := ValidationPending
	if c.inventory == nil {
		return status, "Missing inventory"
	}
	status = boolValue(c.inventory.Memory.PhysicalBytes >= conversions.MibToBytes(c.minRAMMibRequirement))
	if status == ValidationSuccess {
		return status, "Sufficient minimum RAM"
	}

	return ValidationFailure, fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is %s, found only %s",
		conversions.BytesToString(conversions.MibToBytes(c.minRAMMibRequirement)), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
}

func (v *validator) compatibleWithClusterPlatform(c *validationContext) (ValidationStatus, string) {
	// Late binding
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if *c.cluster.Kind == models.ClusterKindAddHostsCluster {
		return ValidationSuccess, fmt.Sprintf("Host is compatible with cluster platform %s", common.PlatformTypeValue(c.cluster.Platform.Type))
	}
	if c.inventory == nil || common.PlatformTypeValue(c.cluster.Platform.Type) == "" {
		return ValidationPending, "Missing inventory or platform isn't set"
	}
	supported, err := v.providerRegistry.IsHostSupported(c.cluster.Platform, c.host)
	if err != nil {
		return ValidationError, "Validation error"
	}
	if supported {
		return ValidationSuccess, fmt.Sprintf("Host is compatible with cluster platform %s", common.PlatformTypeValue(c.cluster.Platform.Type))
	}
	return ValidationFailure, fmt.Sprintf("Host is not compatible with cluster platform %s; either disable this host or discover a new compatible host.",
		common.PlatformTypeValue(c.cluster.Platform.Type))
}

func (v *validator) areTangServersReachable(c *validationContext) (ValidationStatus, string) {
	if c.host.TangConnectivity == "" {
		return ValidationPending, ""
	}
	// Older agents have no action for tang-connectivity-check.
	// The missing action result will not fail host validations to
	// keep backward compatibility with older agents who did not include tang-connectivity-check.
	if strings.Contains(c.host.TangConnectivity, FailedToFindAction) {
		v.log.Warningf(
			"host %s replied to StepType: %s with: %s. Validation will pass to keep backward compatibility with discovery agent version: %s",
			c.host.ID.String(), models.StepTypeTangConnectivityCheck, c.host.TangConnectivity, c.host.DiscoveryAgentVersion,
		)
		return ValidationSuccessSuppressOutput, ""
	}
	var response models.TangConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.TangConnectivity), &response); err == nil && response.IsSuccess {
		return ValidationSuccess, "Tang servers are reachable"
	}

	return ValidationFailure, fmt.Sprintf("Could not validate that all Tang servers are reachable and working: %s", c.host.TangConnectivity)
}

func (v *validator) diskEncryptionRequirementsSatisfied(c *validationContext) (ValidationStatus, string) {

	var status ValidationStatus
	var message string

	if c.infraEnv != nil || swag.StringValue(c.cluster.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone {
		return ValidationSuccessSuppressOutput, ""
	}
	if c.inventory == nil {
		return ValidationPending, "Missing host inventory"
	}
	if hostutil.IsDay2Host(c.host) {
		//day2 validation is taking the disk encryption data solely from
		//the host inventory and set the diskEncryption field on the cluster
		//according to that information
		luks, err := hostutil.GetDiskEncryptionForDay2(v.log, c.host)
		if err != nil {
			return ValidationPending, "Missing ignition information"
		}
		if luks == nil || luks.Clevis == nil {
			// Disk encryption is disabled for workers on day1 cluster
			return ValidationSuccessSuppressOutput, ""
		}
		c.cluster.DiskEncryption = &models.DiskEncryption{}
		if swag.BoolValue(luks.Clevis.Tpm2) {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTpmv2)
			// If Tpm2 is enabled for workers, check whether supported by the host.
			status = boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
		} else if len(luks.Clevis.Tang) != 0 {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTang)
			status, message = v.areTangServersReachable(c)
			if status == ValidationFailure {
				return status, message
			}
		} else {
			// Only Tpm2 and Tang are available for disk encryption
			status = ValidationFailure
		}

	} else {
		//day 1 validation is relying on the host's role and the user
		//configuration to check if the disk encryption setup is valid
		role := common.GetEffectiveRole(c.host)
		if role == models.HostRoleAutoAssign {
			return ValidationPending, "Missing role assignment"
		}
		if !hostutil.IsDiskEncryptionEnabledForRole(*c.cluster.DiskEncryption, role) {
			return ValidationSuccessSuppressOutput, ""
		}
		if swag.StringValue(c.cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTang {
			status, message = v.areTangServersReachable(c)
			if status == ValidationFailure {
				return status, message
			}
		} else { // Mode TPMv2
			status = boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
		}

	}

	switch status {
	case ValidationSuccess:
		return status, fmt.Sprintf("Installation disk can be encrypted using %s", *c.cluster.DiskEncryption.Mode)
	case ValidationFailure:
		// Mode TPMv2
		if c.inventory.TpmVersion == models.InventoryTpmVersionNone {
			return status, "TPM version could not be found, make sure TPM is enabled in host's BIOS"
		}
		if c.cluster.DiskEncryption.Mode == nil {
			return status, "Invalid LUKS object in ignition - both TPM2 and Tang are not available"
		}
		return status, fmt.Sprintf("The host's TPM version is not supported, expected-version: %s, actual-version: %s",
			models.InventoryTpmVersionNr20, c.inventory.TpmVersion)
	case ValidationPending:
		return status, "Disk encryption check was not performed yet"
	default:
		return status, fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinValidDisks(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "Missing inventory"
	}
	inventory, err := c.inventoryCache.GetOrUnmarshal(c.host)
	if err != nil {
		return ValidationError, "Failed to load inventory"
	}
	if len(inventory.Disks) == 0 {
		return ValidationError, "Failed to detected disks"
	}
	disks := v.hwValidator.ListEligibleDisks(c.inventory)
	if len(disks) > 0 {
		return ValidationSuccess, "Sufficient disk capacity"
	}
	return ValidationFailure, "No eligible disks were found, please check specific disks to see why they are not eligible"
}

func (v *validator) isMachineCidrDefined(c *validationContext) (ValidationStatus, string) {
	status := ValidationSuccessSuppressOutput
	if c.infraEnv != nil {
		return status, fmt.Sprintf("Unexpected status %s", status)
	}
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess, "No Machine Network CIDR needed: User Managed Networking"
	}
	if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster {
		return ValidationSuccess, "No Machine Network CIDR needed: Day2 cluster"
	}
	if network.IsMachineCidrAvailable(c.cluster) {
		return ValidationSuccess, "Machine Network CIDR is defined"
	}
	if swag.BoolValue(c.cluster.VipDhcpAllocation) {
		return ValidationFailure, "Machine Network CIDR is undefined"
	}
	return ValidationFailure, "Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs"
}

func (v *validator) hasCPUCoresForRole(c *validationContext) (ValidationStatus, string) {
	if c.inventory != nil {
		if c.inventory.CPU.Count >= c.clusterHostRequirements.Total.CPUCores {
			return ValidationSuccess, fmt.Sprintf("Sufficient CPU cores for role %s", common.GetEffectiveRole(c.host))
		}
		return ValidationFailure, fmt.Sprintf("Require at least %d CPU cores for %s role, found only %d", c.clusterHostRequirements.Total.CPUCores, common.GetEffectiveRole(c.host), c.inventory.CPU.Count)
	}
	return ValidationPending, "Missing inventory or role"
}

func (v *validator) hasMemoryForRole(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "Missing inventory or role"
	}
	requiredBytes := conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)
	if c.inventory.Memory.PhysicalBytes >= requiredBytes {
		return ValidationSuccess, fmt.Sprintf("Sufficient RAM for role %s", common.GetEffectiveRole(c.host))
	}
	return ValidationFailure, fmt.Sprintf("Require at least %s RAM for role %s, found only %s",
		conversions.BytesToString(conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)), common.GetEffectiveRole(c.host), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
}

func (v *validator) isHostnameUnique(c *validationContext) (ValidationStatus, string) {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if c.inventory == nil {
		return ValidationPending, "Missing inventory"
	}
	realHostname := getRealHostname(c.host, c.inventory)
	for _, h := range c.cluster.Hosts {
		if h.ID.String() != c.host.ID.String() && h.Inventory != "" {
			otherInventory, err := c.inventoryCache.GetOrUnmarshal(h)
			if err != nil || otherInventory == nil {
				v.log.WithError(err).Warnf("Illegal inventory for host %s", h.ID.String())
				// It is not our hostname
				continue
			}
			if realHostname == getRealHostname(h, otherInventory) {
				return ValidationFailure, fmt.Sprintf("Hostname %s is not unique in cluster", getRealHostname(c.host, c.inventory))
			}
		}
	}
	return ValidationSuccess, fmt.Sprintf("Hostname %s is unique in cluster", getRealHostname(c.host, c.inventory))
}

func (v *validator) isValidPlatformNetworkSettings(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "Missing inventory"
	}
	if c.inventory.SystemVendor == nil {
		return ValidationError, "Validation error"
	}
	if funk.ContainsString(invalidPlatforms, c.inventory.SystemVendor.ProductName) {
		// In case there is no cluster validation is pending
		if c.infraEnv != nil {
			return ValidationSuccessSuppressOutput, ""
		} else {
			//In case userManagedNetworking is true, we don't care about the platform
			if swag.BoolValue(c.cluster.UserManagedNetworking) {
				return ValidationSuccess, fmt.Sprintf("Platform %s is allowed", c.inventory.SystemVendor.ProductName)
			}
			return ValidationFailure, fmt.Sprintf("Platform %s is allowed only for Single Node OpenShift or user-managed networking", c.inventory.SystemVendor.ProductName)
		}
	}
	return ValidationSuccess, fmt.Sprintf("Platform %s is allowed", c.inventory.SystemVendor.ProductName)
}

func (v *validator) belongsToMachineCidr(c *validationContext) (ValidationStatus, string) {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}

	// In case cluster is multi node UMN no need to validate non bootstrap nodes at all
	// In boostrap case if machine cidr was not set by user no need to validate either as it machine cidr will be set from one of it networks
	if swag.BoolValue(c.cluster.UserManagedNetworking) && !common.IsSingleNodeCluster(c.cluster) && (!c.host.Bootstrap || !network.IsMachineCidrAvailable(c.cluster)) {
		return ValidationSuccess, "No machine network CIDR validation needed: User Managed Networking"
	}

	if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster {
		return ValidationSuccess, "No machine network CIDR validation needed: Day2 cluster"
	}
	if c.inventory == nil || !network.IsMachineCidrAvailable(c.cluster) {
		return ValidationPending, "Missing inventory or machine network CIDR"
	}
	if !network.IsHostInPrimaryMachineNetCidr(v.log, c.cluster, c.host) {
		return ValidationFailure, "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"
	}
	return ValidationSuccess, "Host belongs to all machine network CIDRs"
}

func getRealHostname(host *models.Host, inventory *models.Inventory) string {
	if host.RequestedHostname != "" {
		return host.RequestedHostname
	}
	return inventory.Hostname
}

func (v *validator) isHostnameValid(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationFailure, "Missing inventory"
	}

	if err := hostutil.ValidateHostname(getRealHostname(c.host, c.inventory)); err != nil {
		if funk.ContainsString(hostutil.ForbiddenHostnames, getRealHostname(c.host, c.inventory)) {
			return ValidationFailure, fmt.Sprintf("The host name %s is forbidden", getRealHostname(c.host, c.inventory))
		}
		return ValidationFailure, fmt.Sprintf("Hostname %s is forbidden, hostname should match pattern %s", getRealHostname(c.host, c.inventory), hostutil.HostnamePattern)
	}
	return ValidationSuccess, fmt.Sprintf("Hostname %s is allowed", getRealHostname(c.host, c.inventory))
}

func (v *validator) isIgnitionDownloadable(c *validationContext) (ValidationStatus, string) {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}

	if !hostutil.IsDay2Host(c.host) {
		return ValidationSuccessSuppressOutput, ""
	}

	if c.host.APIVipConnectivity == "" {
		return ValidationPending, "Ignition is not yet available, pending API connectivity"
	}

	var apiConnectivityResponse models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.APIVipConnectivity), &apiConnectivityResponse); err != nil {
		return ValidationError, "Internal error - failed to parse agent API connectivity response"
	}

	if apiConnectivityResponse.IsSuccess {
		return ValidationSuccess, "Ignition is downloadable"
	}

	if apiConnectivityResponse.URL == "" {
		// Missing URL means this is a response from an older agent version,
		// without much information about what went wrong with the download -
		// so return an undetailed error message
		return ValidationFailure, "This host has failed to download the ignition file from the cluster, please ensure the host can reach the cluster"
	}

	return ValidationFailure, fmt.Sprintf(
		"This host has failed to download the ignition file from %s with the following error: %s. "+
			"Please ensure the host can reach this URL",
		apiConnectivityResponse.URL, apiConnectivityResponse.DownloadError)
}

func (v *validator) belongsToL2MajorityGroup(c *validationContext, majorityGroups map[string][]strfmt.UUID) ValidationStatus {
	if !network.IsMachineCidrAvailable(c.cluster) {
		return ValidationPending
	}

	// TODO(mko) This rule should be revised as soon as OCP supports multiple machineNetwork
	//           entries using the same IP stack.
	areNetworksEqual := func(ipnet1, ipnet2 *net.IPNet) bool {
		return ipnet1.IP.Equal(ipnet2.IP) && bytes.Equal(ipnet1.Mask, ipnet2.Mask)
	}

	groupForNetwork := func(ipnet *net.IPNet) []strfmt.UUID {
		for key, groups := range majorityGroups {
			_, groupIpnet, err := net.ParseCIDR(key)

			// majority groups may contain keys other than CIDRS (For instance IPv4 for L3).  Therefore, in case of
			// parse error we can skip safely
			if err != nil {
				continue
			}
			if areNetworksEqual(ipnet, groupIpnet) {
				return groups
			}
		}
		return nil
	}

	for _, machineNet := range c.cluster.MachineNetworks {
		_, machineIpnet, err := net.ParseCIDR(string(machineNet.Cidr))
		if err != nil {
			return ValidationError
		}
		if !funk.Contains(groupForNetwork(machineIpnet), *c.host.ID) {
			return ValidationFailure
		}
	}

	return ValidationSuccess
}

func (v *validator) belongsToL3MajorityGroup(c *validationContext, majorityGroups map[string][]strfmt.UUID) ValidationStatus {
	ipv4, ipv6, err := network.GetConfiguredAddressFamilies(c.cluster)
	if err != nil {
		v.log.WithError(err).Warn("Get configured address families")
		return ValidationError
	}
	if !(ipv4 || ipv6) {
		return ValidationFailure
	}
	ret := true
	if ipv4 {
		ret = ret && funk.Contains(majorityGroups[network.IPv4.String()], *c.host.ID)
	}
	if ipv6 {
		ret = ret && funk.Contains(majorityGroups[network.IPv6.String()], *c.host.ID)
	}
	return boolValue(ret)
}

func (v *validator) belongsToMajorityGroup(c *validationContext) (ValidationStatus, string) {
	var message string
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if hostutil.IsDay2Host(c.host) {
		return ValidationSuccess, "Day2 host is not required to be connected to other hosts in the cluster"
	}
	if common.IsSingleNodeCluster(c.cluster) {
		return ValidationSuccess, "Host has connectivity to the majority of hosts in the cluster"
	}
	if c.cluster.ConnectivityMajorityGroups == "" {
		return ValidationPending, "Machine Network CIDR or Connectivity Majority Groups missing"
	}
	var majorityGroups map[string][]strfmt.UUID
	err := json.Unmarshal([]byte(c.cluster.ConnectivityMajorityGroups), &majorityGroups)
	if err != nil {
		v.log.WithError(err).Warn("Parse majority group")
		return ValidationError, "Parse error for connectivity majority group"
	}

	var status ValidationStatus
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		status = v.belongsToL3MajorityGroup(c, majorityGroups)
	} else {
		status = v.belongsToL2MajorityGroup(c, majorityGroups)
	}
	if status == ValidationFailure && len(c.cluster.Hosts) < 3 {
		return ValidationPending, "Not enough hosts in cluster to calculate connectivity groups"
	}

	switch status {
	case ValidationSuccess:
		message = "Host has connectivity to the majority of hosts in the cluster"
	case ValidationFailure:
		message = "No connectivity to the majority of hosts in the cluster"
	case ValidationPending:
		// Shouldn't happen
		message = "Not enough information to calculate host majority groups"
	default:
		message = fmt.Sprintf("Unexpected status %s", status)
	}

	return status, message
}

func (v *validator) missingNTPSyncResult(db *gorm.DB, host *models.Host) ValidationStatus {
	unboundStatuses := []string{
		models.HostStatusInsufficientUnbound,
		models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscoveringUnbound,
		models.HostStatusKnownUnbound,
	}
	if funk.ContainsString(unboundStatuses, swag.StringValue(host.Status)) {
		sources, err := common.GetHostNTPSources(db, host)
		if err != nil {
			v.log.WithError(err).Errorf("Failed to get sources for host %s", host.ID.String())
			return ValidationError
		}
		if sources == "" {
			return ValidationSuccessSuppressOutput
		}
	}
	return ValidationFailure
}

func (v *validator) isNTPSynced(c *validationContext) (ValidationStatus, string) {

	var status ValidationStatus
	var message string

	var sources []*models.NtpSource

	if c.host.NtpSources == "" {
		status = v.missingNTPSyncResult(c.db, c.host)
	} else if err := json.Unmarshal([]byte(c.host.NtpSources), &sources); err != nil {
		v.log.WithError(err).Warn("Parse NTP sources")
		status = ValidationError
	} else {
		status = v.missingNTPSyncResult(c.db, c.host)
		for _, source := range sources {
			if source.SourceState == models.SourceStateSynced {
				status = ValidationSuccess
			}
		}
	}

	switch status {
	case ValidationSuccess:
		message = "Host NTP is synced"
	case ValidationFailure:
		message = "Host couldn't synchronize with any NTP server"
	case ValidationError:
		message = "Parse error for NTP sources"
	default:
		message = fmt.Sprintf("Unexpected status %s", status)
	}

	return status, message
}

func (v *validator) isTimeSyncedBetweenHostAndService(c *validationContext) (ValidationStatus, string) {
	if c.host.Timestamp == 0 {
		return ValidationPending, "Missing host time, can't determine synchronization between host and service"
	}

	diff := time.Now().UTC().Sub(time.Unix(c.host.Timestamp, 0).UTC())
	if diff > maxServiceAheadOfHostTimeDiff {
		return ValidationFailure, fmt.Sprintf("Host clock is not synchronized, service time is ahead of host's at least for %.1f minutes, "+
			"please configure an NTP server via DHCP. Service time: %s", maxServiceAheadOfHostTimeDiff.Minutes(), time.Now().UTC())
	} else if diff < -maxHostAheadOfServiceTimeDiff {
		return ValidationFailure, fmt.Sprintf("Host clock is not synchronized, host time is ahead of service at least for %.1f minutes, "+
			"please configure an NTP server via DHCP. Service time: %s", maxHostAheadOfServiceTimeDiff.Minutes(), time.Now().UTC())
	}

	return ValidationSuccess, "Host clock is synchronized with service"
}

func (v *validator) sucessfullOrUnknownContainerImagesAvailability(c *validationContext) (ValidationStatus, string) {
	imageStatuses, err := common.UnmarshalImageStatuses(c.host.ImagesStatus)
	if err != nil {
		v.log.WithError(err).Warn("Parse container image statuses")
		return ValidationError, "Validation error"
	}
	if !allImagesValid(imageStatuses) {
		images, err := v.getFailedImagesNames(c.host)
		if err == nil {
			return ValidationFailure, fmt.Sprintf("Failed to fetch container images needed for installation from %s. "+
				"This may be due to a network hiccup. Retry to install again. If this problem persists, "+
				"check your network settings to make sure you’re not blocked.", strings.Join(images, ","))
		}
		return ValidationError, "Validation error"
	}
	return ValidationSuccess, "All required container images were either pulled successfully or no attempt was made to pull them"
}

func (v *validator) getFailedImagesNames(host *models.Host) ([]string, error) {
	imageStatuses, err := common.UnmarshalImageStatuses(host.ImagesStatus)
	if err != nil {
		return nil, err
	}

	imageNames := make([]string, 0)

	for _, imageStatus := range imageStatuses {
		if isInvalidImageStatus(imageStatus) {
			imageNames = append(imageNames, imageStatus.Name)
		}
	}

	return imageNames, nil
}

func isInvalidImageStatus(imageStatus *models.ContainerImageAvailability) bool {
	return imageStatus.Result == models.ContainerImageAvailabilityResultFailure ||
		(imageStatus.SizeBytes > 0 && imageStatus.DownloadRate < ImageStatusDownloadRateThreshold)
}

func allImagesValid(imageStatuses common.ImageStatuses) bool {
	for _, imageStatus := range imageStatuses {
		if isInvalidImageStatus(imageStatus) {
			return false
		}
	}
	return true
}

// This is a pre-install validation that checks that the boot device was either
// not tested for sufficient disk speed or the disk speed check has been
// successful.  Since disk speed test is performed after installation has
// started, in order to have result for such test, the result has to be from a
// previous installation attempt. Since all pre-install validations have to
// pass before starting installation, it is mandatory that in case installation
// on the current boot device has not been attempted yet, this validation must
// pass.
func (v *validator) sufficientOrUnknownInstallationDiskSpeed(c *validationContext) (ValidationStatus, string) {
	info, err := v.getBootDeviceInfo(c.host)
	if err != nil {
		return ValidationError, "Validation error"
	}
	if info == nil || info.DiskSpeed == nil || !info.DiskSpeed.Tested {
		return ValidationSuccess, "Speed of installation disk has not yet been measured"
	}
	if info.DiskSpeed.ExitCode == 0 {
		return ValidationSuccess, "Speed of installation disk is sufficient"
	}
	return ValidationFailure, "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"
}

type hostTimingMetric struct {
	otherHostName string
	timingMetric  float64
	timingSuffix  string
}

func (v *validator) summarizeHostTimingMetrics(packetLossInfo []hostTimingMetric, truncateMetrics bool) string {
	result := []string{}
	for i, p := range packetLossInfo {
		//If there a lot of hosts in the cluster, this list could be rather large, so we shorten it
		if truncateMetrics && i > maxHostTimingMetrics {
			result = append(result, fmt.Sprintf("%s (%.2f%s) and others...", p.otherHostName, p.timingMetric, p.timingSuffix))
			break
		}
		result = append(result, fmt.Sprintf("%s (%.2f%s)", p.otherHostName, p.timingMetric, p.timingSuffix))
	}
	return strings.Join(result, ", ")
}

type thresholdTestType int

const (
	thresholdTestL3AverageRTTMs thresholdTestType = 0
	thresholdTestL3PacketLoss   thresholdTestType = 1
)

func (v *validator) thresholdExceededTest(testType thresholdTestType, host *models.Host, clusterRoleReqs *models.ClusterHostRequirements, hosts []*models.Host, inventoryCache InventoryCache) (ValidationStatus, []hostTimingMetric, error) {

	connectivityReport, err := hostutil.UnmarshalConnectivityReport(host.Connectivity)
	if err != nil {
		v.log.Errorf("Unable to unmarshall host connectivity for %s:%s", host.ID, err)
		return ValidationError, nil, nil
	}
	failedHostIPs := map[string]struct{}{}
	failedHostMetrics := []hostTimingMetric{}
	for _, r := range connectivityReport.RemoteHosts {
		for _, l3 := range r.L3Connectivity {

			var hostHasExceededThreshold bool
			switch testType {
			case thresholdTestL3AverageRTTMs:
				hostHasExceededThreshold = l3.AverageRTTMs > *clusterRoleReqs.Total.NetworkLatencyThresholdMs
			case thresholdTestL3PacketLoss:
				hostHasExceededThreshold = l3.PacketLossPercentage > *clusterRoleReqs.Total.PacketLossPercentage
			default:
				return ValidationError, nil, fmt.Errorf("unexpected testType")
			}
			if hostHasExceededThreshold {
				if _, ok := failedHostIPs[l3.RemoteIPAddress]; !ok {
					hostname, role, err := GetHostnameAndEffectiveRoleByHostID(r.HostID, hosts, inventoryCache)
					if err != nil {
						v.log.Error(err)
						return ValidationFailure, nil, err
					}
					if role == common.GetEffectiveRole(host) {
						failedHostIPs[l3.RemoteIPAddress] = struct{}{}
						switch testType {
						case thresholdTestL3AverageRTTMs:
							failedHostMetrics = append(failedHostMetrics, hostTimingMetric{otherHostName: hostname, timingMetric: l3.AverageRTTMs, timingSuffix: " ms"})
						case thresholdTestL3PacketLoss:
							failedHostMetrics = append(failedHostMetrics, hostTimingMetric{otherHostName: hostname, timingMetric: l3.PacketLossPercentage, timingSuffix: "%"})
						}
					}
				}
			}
		}
	}
	if len(failedHostMetrics) > 0 {
		return ValidationFailure, failedHostMetrics, nil
	}
	return ValidationSuccess, nil, nil

}

func (v *validator) hasSufficientPacketLossRequirementForRole(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "The inventory is not available yet."
	}
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.PacketLossPercentage == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return ValidationSuccess, "Packet loss requirement has been satisfied."
	}
	if len(c.host.Connectivity) == 0 {
		return ValidationPending, "Missing packet loss information."
	}
	status, hostMetrics, err := v.thresholdExceededTest(thresholdTestL3PacketLoss, c.host, c.clusterHostRequirements, c.cluster.Hosts, c.inventoryCache)
	if err != nil {
		return status, fmt.Sprintf("Error while attempting to validate packet loss validation: %s", err)
	}

	switch status {
	case ValidationSuccess:
		return status, "Packet loss requirement has been satisfied."
	case ValidationFailure:
		// When logging, make sure the full timing metrics are logged.
		fullHostTimingMetrics := v.summarizeHostTimingMetrics(hostMetrics, false)
		v.log.Error(fmt.Sprintf(`A total packet loss above the tolerated threshold of %.2f%% was encountered when performing connectivity validation between host %s and %s\n`,
			*c.clusterHostRequirements.Total.PacketLossPercentage,
			c.host.ID,
			fullHostTimingMetrics,
		))
		// For the advisory message, a truncated summary of the timing metrics.
		shortHostTimingMetrics := v.summarizeHostTimingMetrics(hostMetrics, true)
		packetLossAdvisoryMessage := fmt.Sprintf(`A total packet loss above the tolerated threshold of %.2f%% was encountered when performing connectivity validation between host %s and %s\n`,
			*c.clusterHostRequirements.Total.PacketLossPercentage,
			c.host.ID,
			shortHostTimingMetrics,
		)
		packetLossAdvisoryMessage += v.generatePacketLossAdvisoryMessageForHost(c)
		return status, packetLossAdvisoryMessage
	case ValidationError:
		return status, "Parse error while attempting to process the connectivity report"
	}
	return status, fmt.Sprintf("Unexpected status %s", status)
}

func (v *validator) generatePingCommand(c *validationContext, interfaceName string, addresses []string) string {
	var message string

	for i, address := range addresses {
		//If there a lot of hosts in the cluster, this list could be rather large, so we shorten it
		if i > maxPingCommandExamples {
			message += "etc... \n"
			break
		}
		// This command must be kept in sync with the agent repo https://github.com/openshift/assisted-installer-agent/blob/a35f7c36951313f6a6948a190ca6b56d1472516b/src/connectivity_check/connectivity_check.go#L87
		message += fmt.Sprintf("ping -c 10 -W 3 -q -I %s %s\n", interfaceName, address)
	}

	return message
}

func (v *validator) generatePingCommandAdvisoryForInventory(c *validationContext, inventory *models.Inventory) string {
	var message string
	hostName := getRealHostname(c.host, inventory)
	if len(inventory.Interfaces) > 0 {
		message += fmt.Sprintf("2: Please try the following commands on the host %s to investigate the packet loss further\n\n", hostName)
		for _, intf := range inventory.Interfaces {
			message += v.generatePingCommand(c, intf.Name, intf.IPV4Addresses)
		}
	}
	return message
}

func (v *validator) generatePacketLossAdvisoryMessageForHost(c *validationContext) string {
	message := "Actions:\n"
	message += "1: Check if there are multiple devices on the same L2 network, if so then use the built-in nmstate-based advanced networking configuration to create a bond or disable all but one of the interfaces.\n"

	inventory, err := c.inventoryCache.GetOrUnmarshal(c.host)
	if err != nil {
		v.log.WithError(err).Warnf("Could not parse inventory of host %s\n", *c.host.ID)
	}
	return message + v.generatePingCommandAdvisoryForInventory(c, inventory)
}

func (v *validator) hasSufficientNetworkLatencyRequirementForRole(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "The inventory is not available yet."
	}
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.NetworkLatencyThresholdMs == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return ValidationSuccess, "Network latency requirement has been satisfied."
	}
	if len(c.host.Connectivity) == 0 {
		return ValidationPending, "Missing network latency information."
	}
	status, hostMetrics, err := v.thresholdExceededTest(thresholdTestL3AverageRTTMs, c.host, c.clusterHostRequirements, c.cluster.Hosts, c.inventoryCache)
	if status == ValidationFailure {
		if err != nil {
			return ValidationFailure, fmt.Sprintf("Error while attempting to validate network latency: %s", err)
		}

		// When logging, make sure the full timing metrics are logged.
		fullHostTimingMetrics := v.summarizeHostTimingMetrics(hostMetrics, false)
		v.log.Info(fmt.Sprintf(`A total network latency above the tolerated threshold of %.2f ms was encountered when performing network latency tests between host %s and %s\n`,
			*c.clusterHostRequirements.Total.NetworkLatencyThresholdMs,
			c.host.ID,
			fullHostTimingMetrics,
		))

		// For the advisory message, a truncated summary of the timing metrics.
		shortHostTimingMetrics := v.summarizeHostTimingMetrics(hostMetrics, true)
		networkLatencyAdvisoryMessage := fmt.Sprintf(`A total network latency above the tolerated threshold of %.2f ms was encountered when performing network latency tests between host %s and %s\n`,
			*c.clusterHostRequirements.Total.NetworkLatencyThresholdMs,
			c.host.ID,
			shortHostTimingMetrics,
		)

		networkLatencyAdvisoryMessage += v.generateExcessiveLatencyAdvisoryForHost(c)
		return ValidationFailure, networkLatencyAdvisoryMessage
	}
	if status == ValidationError {
		return ValidationError, "Parse error while attempting to process the connectivity report"
	}
	return ValidationSuccess, "Network latency requirement has been satisfied."
}

func (v *validator) generateExcessiveLatencyAdvisoryForHost(c *validationContext) string {
	var message string
	inventory, err := c.inventoryCache.GetOrUnmarshal(c.host)
	if err != nil {
		v.log.WithError(err).Warnf("Could not parse inventory of host %s\n", *c.host.ID)
	}
	if len(inventory.Interfaces) > 0 {
		message += fmt.Sprintf("Actions:\n1: Please try the following commands on the host %s and examine averages to investigate the latency issue further\n\n", getRealHostname(c.host, inventory))
		message += v.generatePingCommandAdvisoryForInventory(c, inventory)
	}
	return message
}

func (v *validator) hasDefaultRoute(c *validationContext) (ValidationStatus, string) {
	if c.inventory == nil {
		return ValidationPending, "Missing default routing information."
	}
	if len(c.inventory.Routes) == 0 {
		return ValidationFailure, "Host has not yet been configured with a default route."
	}
	if v.validateDefaultRoute(c.inventory.Routes) {
		return ValidationSuccess, "Host has been configured with at least one default route."
	}
	return ValidationFailure, "Host has not yet been configured with a default route."
}

func (v *validator) validateDefaultRoute(routes []*models.Route) bool {
	for _, r := range routes {
		if len(r.Destination) == 0 || len(r.Gateway) == 0 {
			continue
		}
		isDefault, err := network.IsDefaultRoute(r)
		if err != nil {
			v.log.Error(err)
			continue
		}
		if isDefault {
			return true
		}
	}
	return false
}

func ignitionHasFile(ignition *ignition_types.Config, path string) bool {
	for _, file := range ignition.Storage.Files {
		if file.Path == path {
			return true
		}
	}

	return false
}

func ignitionReadFile(ignition *ignition_types.Config, path string) ([]byte, error) {
	for _, file := range ignition.Storage.Files {
		if file.Path == path {
			contentSourceURL, err := url.Parse(*file.Contents.Source)
			if err != nil {
				return nil, fmt.Errorf("malformed content source URL: %w", err)
			}

			if contentSourceURL.Scheme != "data" {
				return nil, fmt.Errorf("scheme %s unsuported, only the data scheme is supported",
					contentSourceURL.Scheme)
			}

			parsedDataUrl, err := dataurl.DecodeString(contentSourceURL.String())
			if err != nil {
				return nil, fmt.Errorf("malformed data url: %w", err)
			}

			return parsedDataUrl.Data, nil
		}
	}

	return nil, errors.New("file not found")
}

func ignitionContainsManagedNetworkingFiles(config *ignition_types.Config) bool {
	if ignitionHasFile(config, "/etc/kubernetes/manifests/coredns.yaml") ||
		ignitionHasFile(config, "/etc/kubernetes/manifests/keepalived.yaml") {
		// There's no official stable way to tell whether an ignition came from
		// a cluster with managed networking or not, so as a heuristic we
		// assume that the presence of any one of these files implies that.
		// Hopefully in the future a more official way to do this will be
		// available (e.g. a magic empty file placed for this purpose inside
		// the ignition that we can rely on to be stable), then we can slowly
		// move to using only that file instead.
		return true
	}

	return false
}

// workerIgnitionKubeletKubeconfigServerIsIPAddress looks for the kubelet
// kubeconfig file within an ignition config and checks whether the server of
// the cluster it points to is an IP address or a domain name
func workerIgnitionKubeletKubeconfigServerIsIPAddress(config *ignition_types.Config) bool {
	const kubeletKubeconfigIgnitionPath string = "/etc/kubernetes/kubeconfig"

	if !ignitionHasFile(config, kubeletKubeconfigIgnitionPath) {
		return false
	}

	kubeconfigBytes, err := ignitionReadFile(config, kubeletKubeconfigIgnitionPath)
	if err != nil {
		return false
	}

	type KubeconfigClusterInternal struct {
		Server string `json:"server"`
	}
	type KubeconfigCluster struct {
		Cluster KubeconfigClusterInternal `json:"cluster"`
	}
	type Kubeconfig struct {
		Clusters []KubeconfigCluster `json:"clusters"`
	}
	var kubeconfig Kubeconfig

	err = yaml.Unmarshal(kubeconfigBytes, &kubeconfig)
	if err != nil {
		return false
	}

	if len(kubeconfig.Clusters) != 1 {
		return false
	}

	serverURL, err := url.Parse(kubeconfig.Clusters[0].Cluster.Server)
	if err != nil {
		return false
	}

	serverHostnameIsIPAddress := net.ParseIP(serverURL.Hostname()) != nil
	return serverHostnameIsIPAddress
}

// importedClusterHasManagedNetworking checks imported clusters in order to
// determine whether they have managed networking or not. It does so by
// inspecting the ignition file returned by each of the day-2 host agent's API
// connectivity step responses and looking for file entries within them which
// indicate that the cluster they originated from has managed networking.
func (v *validator) importedClusterHasManagedNetworking(cluster *common.Cluster) bool {
	for _, day2Host := range cluster.Hosts {
		if day2Host.APIVipConnectivity == "" {
			continue
		}

		var apiConnectivityResponse models.APIVipConnectivityResponse
		if err := json.Unmarshal([]byte(day2Host.APIVipConnectivity), &apiConnectivityResponse); err != nil {
			v.log.WithError(err).Warnf("Invalid API connectivity response")
			continue
		}

		if !apiConnectivityResponse.IsSuccess {
			continue
		}

		config, _, err := v3_2.Parse([]byte(apiConnectivityResponse.Ignition))
		if err != nil {
			v.log.WithError(err).Warn("Ignition is empty or invalid")
			continue
		}

		if ignitionContainsManagedNetworkingFiles(&config) {
			return true
		}

		if workerIgnitionKubeletKubeconfigServerIsIPAddress(&config) {
			// The kubelet kubeconfig within the ignition looks like it's using
			// an IP address rather than a DNS domain name to connect to the
			// cluster API. We consider this to be "managed networking", as DNS
			// resolution is not needed. This is the case for some hypershift
			// clusters.
			return true
		}

		return false
	}

	// We've exhausted all day-2 hosts, none of them seem to indicate that the
	// cluster doesn't (or does) have managed networking, so we're forced to
	// assume that it does in order to preserve our existing behavior of
	// assuming the cluster has managed networking
	return true
}

// shouldValidateDNSResolution is utilized by the various DNS validation
// functions determine whether they should perform DNS validation or not. If
// not, it also returns the validation status they should have, which is
// "pending" in some circumstances and "success" in others.
//
// In general, managed networking clusters don't require the user to sort out
// DNS in advance, so we disable DNS validations while installing or adding
// workers to managed networking clusters.
//
// If our SNO dnsmaq hack is enabled (ENABLE_SINGLE_NODE_DNSMASQ), then day-1
// SNO clusters are an exception to the rule above. Our dnsmasq hack is enabled
// for OCP and disabled for OKD.
func (v *validator) shouldValidateDNSResolution(cluster *common.Cluster, domainName, target string) (bool, ValidationStatus, string) {
	var hasManagedNetworking bool
	if !common.IsImportedCluster(cluster) {
		hasManagedNetworking = !swag.BoolValue(cluster.UserManagedNetworking)
	} else {
		// The value of cluster.UserManagedNetworking is only correct for
		// non-imported clusters, as imported clusters always have it set to
		// false, regardless of its true value.
		// This is why we have to use the following methods to try and guess
		// the actual value.
		// If `cluster.Imported` is nil we assume it's not imported, in order
		// to maintain the older behavior for older clusters that may be running
		// older agents that don't include the necessary information inside the
		// ignition.
		if !v.canDetermineImportedClusterManagedNetworking(cluster) {
			// Wait until one of the day-2 hosts gets an API connectivity
			// response so we can determine whether the cluster has managed
			// networking or not
			return false, ValidationPending, fmt.Sprintf("DNS validation for the %s domain cannot be completed at the moment. This could be due to other validations", domainName)
		}

		// Check if the files within the ignition indicate the cluster has managed networking
		hasManagedNetworking = v.importedClusterHasManagedNetworking(cluster)
	}

	if hasManagedNetworking {
		// Clusters with managed networking never need DNS validations as they
		// automatically take care of the required DNS configuration within the
		// host
		return false, ValidationSuccess, fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", domainName)
	}

	if common.IsDay2Cluster(cluster) {
		// All day 2 clusters that don't have managed networking, regardless of
		// SNO or not, need DNS validations, as day-2 workers cannot benefit
		// for the SNO dnsmasq hack that is leveraged by the day-1 cluster.
	} else {
		// This is a day-1, user-managed-networking cluster
		networkCfg, err := network.NewConfig()
		if err != nil {
			// This should never happen, so the values chosen here are arbitrary
			// and have no effect on anything
			return false, ValidationSuccess, fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", domainName)
		}

		if common.IsSingleNodeCluster(cluster) && networkCfg.EnableSingleNodeDnsmasq {
			// day-1 SNO clusters don't need to perform DNS validation when our
			// dnsmasq hack is enabled, as it takes care of having the required DNS
			// entries automatically configured within the host
			return false, ValidationSuccess, fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", domainName)
		}
	}

	if cluster.BaseDNSDomain == "" {
		// It's impossible for us to perform DNS validation without knowing the
		// cluster's base DNS domain. To fix this the user would have to set
		// this parameter using V2UpdateCluster or import the cluster again
		// while setting the API hostname to a valid API DNS hostname (as
		// opposed to an IP address). This should only happen to day-2 imported
		// clusters where the user specified an IP address as the API hostname
		// rather than a DNS domain, so a base domain for the cluster cannot
		// not be derived, but it's a check/error we perform for all cluster types.
		return false, ValidationError, fmt.Sprintf("DNS validation for the %s domain cannot be completed because the cluster does not have base_dns_domain set. Please update the cluster with the correct base_dns_domain", target)
	}
	return true, "", ""
}

// canDetermineImportedClusterManagedNetworking checks if at-least one of the
// day-2 cluster hosts has obtained the API-connectivity-check ignition file
// successfully.
// This is a pre-requisite before we can call importedClusterHasManagedNetworking
// to determine whether the cluster has managed networking or not.
func (v *validator) canDetermineImportedClusterManagedNetworking(cluster *common.Cluster) bool {
	for _, host := range cluster.Hosts {
		if host.APIVipConnectivity != "" {
			var response models.APIVipConnectivityResponse
			if err := json.Unmarshal([]byte(host.APIVipConnectivity), &response); err != nil {
				v.log.WithError(err).Warnf("Invalid API connectivity response")
				continue
			}

			if !response.IsSuccess {
				continue
			}

			return true
		}
	}

	return false
}

func domainNameToResolve(c *validationContext, name string) string {
	if c.infraEnv != nil {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s", name, c.cluster.Name, c.cluster.BaseDNSDomain)
}

func dottedDomainNameToResolve(c *validationContext, name string) string {
	dn := domainNameToResolve(c, name)
	if dn != "" {
		return dn + "."
	}
	return dn
}

func printableDomain(c *validationContext, domainName, target string) string {
	if c.infraEnv != nil {
		return ""
	}
	if c.cluster.BaseDNSDomain == "" {
		return target
	}
	return domainName
}

func (v *validator) isDomainNameResolvedCorrectly(c *validationContext, domainName, printableDomain, target string) (ValidationStatus, string) {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}

	shouldValidate, status, printStr := v.shouldValidateDNSResolution(c.cluster, printableDomain, target)
	if shouldValidate {
		status, printStr = checkDomainNameResolution(c, domainName, printableDomain, target)
	}

	return status, printStr
}

func (v *validator) isClusterNonWilcardDomainResolvedCorrectly(c *validationContext, subdomain, target string) (ValidationStatus, string) {
	domainName := domainNameToResolve(c, subdomain)
	printableDomain := printableDomain(c, domainName, target)
	return v.isDomainNameResolvedCorrectly(c, domainName, printableDomain, target)
}

func (v *validator) isAPIDomainNameResolvedCorrectly(c *validationContext) (ValidationStatus, string) {
	return v.isClusterNonWilcardDomainResolvedCorrectly(c, constants.APIClusterSubdomain, "API load balancer")
}

func (v *validator) isAPIInternalDomainNameResolvedCorrectly(c *validationContext) (ValidationStatus, string) {
	return v.isClusterNonWilcardDomainResolvedCorrectly(c, constants.InternalAPIClusterSubdomain, "internal API load balancer")
}

func (v *validator) isAppsDomainNameResolvedCorrectly(c *validationContext) (ValidationStatus, string) {
	target := "application ingress"
	domainName := domainNameToResolve(c, fmt.Sprintf("%s.apps", constants.AppsSubDomainNameHostDNSValidation))
	printableDomain := printableDomain(c, domainNameToResolve(c, "*.apps"), target)

	return v.isDomainNameResolvedCorrectly(c, domainName, printableDomain, target)
}

func getFirstMatchingResolution(c *validationContext, domainName string) (*models.DomainResolutionResponseDomain, error) {
	var response *models.DomainResolutionResponse

	if err := json.Unmarshal([]byte(c.host.DomainNameResolutions), &response); err != nil {
		return nil, err
	}

	for _, domain := range response.Resolutions {
		if domain.DomainName != nil && *domain.DomainName == domainName {
			if len(domain.IPV4Addresses) != 0 || len(domain.IPV6Addresses) != 0 {
				return domain, nil
			}
		}
	}

	return nil, nil
}

func domainResolvesToInventoryIP(domain *models.DomainResolutionResponseDomain, inventory *models.Inventory) (string, bool) {
	ipV4Addresses, ipV6Addresses := network.GetInventoryIPAddresses(inventory)
	for _, v4Addr := range domain.IPV4Addresses {
		if funk.Contains(ipV4Addresses, (string)(v4Addr)) {
			return (string)(v4Addr), true
		}
	}
	for _, v6Addr := range domain.IPV6Addresses {
		if funk.Contains(ipV6Addresses, (string)(v6Addr)) {
			return (string)(v6Addr), true
		}
	}
	return "", false
}

func checkDomainNameResolution(c *validationContext, domainName, printableDomain, target string) (ValidationStatus, string) {
	domain, err := getFirstMatchingResolution(c, domainName)
	if err != nil {
		return ValidationError, "Error while evaluating DNS resolution on this host"
	}
	if domain != nil {
		ip, domainResolved := domainResolvesToInventoryIP(domain, c.inventory)
		if domainResolved {
			return ValidationFailure, fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", printableDomain, ip)
		}
		return ValidationSuccess, fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", printableDomain)
	}
	return ValidationFailure, fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", printableDomain, target)
}

/*
 * MGMT-15213: The release domain is not resolved correctly when there is a mirror or proxy.  In this case
 * validation might fail, but the installation may succeed.
 * TODO: MGMT-15213 - Fix the validation bug

func (v *validator) isReleaseDomainResolvedCorrectly(c *validationContext) (ValidationStatus, string) {
	if c.cluster == nil {
		return ValidationSuccess, "host is unbound"
	}
	if common.IsDay2Cluster(c.cluster) && swag.StringValue(c.host.Kind) == models.HostKindAddToExistingClusterHost {
		return ValidationSuccess, "host belongs to day2 cluster"
	}
	releaseImageHost, err := versions.GetReleaseImageHost(c.cluster, v.versionHandler)
	if err != nil {
		return ValidationError, fmt.Sprintf("failed to get release domain for cluster %s", c.cluster.ID.String())
	}
	if net.ParseIP(releaseImageHost) != nil {
		return ValidationSuccess, fmt.Sprintf("Release host %s is an IP address", releaseImageHost)
	}
	return checkDomainNameResolution(c, releaseImageHost, releaseImageHost, "release image host")
}
*/

func checkUnexpectedDomain(c *validationContext, response *models.DomainResolutionResponse, domainName, messageFormat string) (ValidationStatus, string) {
	domainResolution, _ := funk.Find(response.Resolutions,
		func(d *models.DomainResolutionResponseDomain) bool {
			return swag.StringValue(d.DomainName) == domainName
		}).(*models.DomainResolutionResponseDomain)

	if domainResolution == nil {
		return ValidationError, "Internal error - DNS wildcard check cannot be performed because the DNS validation result doesn't contain the wildcard domain"
	}

	if len(domainResolution.IPV4Addresses) == 0 && len(domainResolution.IPV6Addresses) == 0 {
		return ValidationSuccess, "DNS wildcard check was successful"
	}

	// Compile a list of IP addresses that the bad wildcard domain resolves to
	// so they can be included in the message. This might help users understand
	// where the bad wildcard record is coming from.
	addressStrings := []string{}
	for _, ipv4Address := range domainResolution.IPV4Addresses {
		addressStrings = append(addressStrings, string(ipv4Address))
	}
	for _, ipv6Address := range domainResolution.IPV6Addresses {
		addressStrings = append(addressStrings, string(ipv6Address))
	}

	return ValidationFailure, fmt.Sprintf(messageFormat, c.cluster.Name, c.cluster.BaseDNSDomain, strings.Join(addressStrings, ", "))
}

func (v *validator) isDNSWildcardNotConfigured(c *validationContext) (ValidationStatus, string) {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput, ""
	}
	if hostutil.IsDay2Host(c.host) {
		return ValidationSuccess, "DNS wildcard check is not required for day2"
	}
	if c.host.DomainNameResolutions == "" {
		return ValidationPending, "DNS wildcard check cannot be performed yet because the host has not yet performed DNS resolution"
	}
	var response *models.DomainResolutionResponse
	if err := json.Unmarshal([]byte(c.host.DomainNameResolutions), &response); err != nil {
		return ValidationError, "Error while parsing DNS resolution response"
	}

	status, message := checkUnexpectedDomain(c, response, dottedDomainNameToResolve(c, constants.DNSWildcardFalseDomainName),
		"DNS wildcard configuration was detected for domain *.%s.%s - the installation will not be able to complete while this record exists. Please remove it to proceed. The domain resolves to addresses %s")
	if status == ValidationSuccess {
		status, message = checkUnexpectedDomain(c, response, domainNameToResolve(c, constants.DNSWildcardFalseDomainName),
			"Unexpected domain name resolution was detected for the relative domain name with the sub-domain *.%s.%s despite the fact that no resolution exists for a Fully Qualified Domain Name (FQDN) with same sub-domain. This is usually a sign of DHCP-provided domain-search configuration. The installation will not be able to complete with this configuration in place. Please remove it to proceed. The relative domain name resolves to addresses %s")
	}
	return status, message
}

func areNetworksOverlapping(c *validationContext) (ValidationStatus, error) {
	if c.inventory == nil {
		return ValidationPending, errors.New("Missing inventory")
	}
	if c.cluster == nil {
		return ValidationSuccessSuppressOutput, errors.New("host is not bound to a cluster")
	}
	families, err := network.GetClusterAddressFamilies(c.cluster)
	if err != nil {
		return ValidationError, err
	}
	for _, family := range families {
		var networks []network.CidrInfo
		switch family {
		case network.IPv4:
			networks, err = network.GetIPv4Networks(c.inventory)
		case network.IPv6:
			networks, err = network.GetIPv6Networks(c.inventory)
		}
		if err != nil {
			return ValidationError, err
		}
		for i := 0; i < len(networks); i++ {
			for j := i + 1; j < len(networks); j++ {
				ci1 := networks[i]
				ci2 := networks[j]
				if ci1.Cidr == ci2.Cidr {
					return ValidationFailure, errors.Errorf("Address network %s appears on multiple interfaces [%s, %s]", ci1.Cidr, ci1.InterfaceName, ci2.InterfaceName)
				}
				overlap, err := network.NetworksOverlap(ci1.Cidr, ci2.Cidr)
				if err != nil {
					return ValidationError, err
				}
				if overlap {
					return ValidationFailure, errors.Errorf("Address networks %s in interface %s and %s in interface %s overlap", ci1.Cidr, ci1.InterfaceName,
						ci2.Cidr, ci2.InterfaceName)
				}
			}
		}
	}
	return ValidationSuccess, nil
}

func (v *validator) nonOverlappingSubnets(c *validationContext) (ValidationStatus, string) {
	status, err := areNetworksOverlapping(c)
	if err != nil && !funk.Contains([]ValidationStatus{ValidationPending, ValidationSuccess, ValidationSuccessSuppressOutput}, status) {
		v.log.WithError(err).Errorf("Failed to check if CIDRs are overlapping for host %s infra-env %s", c.host.ID.String(), c.host.InfraEnvID.String())
	}
	switch status {
	case ValidationSuccessSuppressOutput:
		return status, ""
	case ValidationSuccess:
		return status, "Host subnets are not overlapping"
	case ValidationPending:
		return status, err.Error()
	case ValidationFailure:
		return status, fmt.Sprintf("Address networks are overlapping: %s", err.Error())
	case ValidationError:
		return status, fmt.Sprintf("Unexpected error: %s", err.Error())
	default:
		return status, fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isVSphereDiskUUIDEnabled(c *validationContext) (ValidationStatus, string) {
	if c.cluster == nil {
		return ValidationSuccessSuppressOutput, "no cluster"
	}

	supported, err := v.providerRegistry.IsHostSupported(&models.Platform{Type: models.PlatformTypeVsphere.Pointer()}, c.host)
	if err != nil {
		return ValidationError, "Validation error"
	}

	if !supported {
		return ValidationSuccessSuppressOutput, ""
	}

	if c.inventory == nil {
		return ValidationPending, "no inventory"
	}

	if c.inventory.Disks == nil {
		return ValidationPending, "no disks"
	}
	for _, disk := range c.inventory.Disks {
		// vSphere only adds a UUID to disks which can potentially be used for storage,
		// if any of them doesn't have that flag, it's likely because the user has forgotten to
		// enable `disk.EnableUUID` for this virtual machine
		// See https://access.redhat.com/solutions/4606201
		if v.hwValidator.IsValidStorageDeviceType(disk, c.inventory.CPU.Architecture, "", false) && !disk.HasUUID {
			return ValidationFailure, "VSphere disk.EnableUUID isn't enabled for this virtual machine, it's necessary for disks to be mounted properly"
		}
	}
	return ValidationSuccess, "VSphere disk.EnableUUID is enabled for this virtual machine"
}

func (v *validator) compatibleAgent(c *validationContext) (ValidationStatus, string) {
	if !v.hwValidatorCfg.EnableUpgradeAgent {
		return ValidationSuccess, "Host agent compatibility checking is disabled"
	}
	if common.IsAgentCompatible(v.hwValidatorCfg.AgentDockerImage, c.host.DiscoveryAgentVersion) {
		return ValidationSuccess, "Host agent is compatible with the service"
	}
	return ValidationFailure, "This host's agent is in the process of being upgraded to a compatible " +
		"version. This might take a few minutes"
}

func (v *validator) noSkipInstallationDisk(c *validationContext) (ValidationStatus, string) {
	const (
		successMessage string = "No request to skip formatting of the installation disk"
		failureMessage string = "Requesting to skip the formatting of the installation disk is not allowed. The installation disk must be formatted. Please either change this host's installation disk or do not skip the formatting of the installation disk"
	)

	// Make sure the user didn't ask us to skip the formatting of the current
	// installation disk
	if c.host.InstallationDiskID == "" || !funk.ContainsString(
		common.GetSkippedFormattingDiskIdentifiers(c.host), c.host.InstallationDiskID) {

		return ValidationSuccess, successMessage
	}

	return ValidationFailure, failureMessage
}

func (v *validator) noSkipMissingDisk(c *validationContext) (ValidationStatus, string) {
	const (
		pendingMessage string = "Host inventory not available yet"
		successMessage string = "All disks that have skipped formatting are present in the host inventory"
		failureMessage string = "One or more of the disks that you have requested to skip the formatting of are no longer present on this host. To ensure they haven't just changed their identity, please remove your request to skip their formatting and then if needed add them back using the new ID"
		errorMessage   string = "Failed to unmarshal this host's inventory"
	)

	if c.host.Inventory == "" {
		return ValidationPending, pendingMessage
	}

	// Make sure that all skip formatting disks are in the inventory. This is
	// to ensure that if the user reboots the system and somehow the disk they
	// asked to skip the formatting of changed ID, then they would have to
	// manually and consciously remove it from the `skip_formatting_disks` list,
	// so we don't accidentally erase it under its new ID.
	for _, skipFormattingDiskID := range common.GetSkippedFormattingDiskIdentifiers(c.host) {
		inventory, err := c.inventoryCache.GetOrUnmarshal(c.host)
		if err != nil || inventory == nil {
			return ValidationError, errorMessage
		}

		found := false
		for _, inventoryDisk := range inventory.Disks {
			if skipFormattingDiskID == common.GetDeviceIdentifier(inventoryDisk) {
				found = true
				break
			}
		}

		if !found {
			return ValidationFailure, failureMessage
		}
	}
	return ValidationSuccess, successMessage
}

func (v *validator) noIPCollisionsInNetwork(c *validationContext) (ValidationStatus, string) {
	if c.cluster == nil {
		return ValidationSuccess, "Cluster has not yet been defined, skipping validation."
	}
	if c.inventory == nil {
		return ValidationSuccess, "Host inventory has not yet been defined, skipping validation."
	}
	if common.IsDay2Cluster(c.cluster) {
		return ValidationSuccess, fmt.Sprintf("Skipping validation for day 2 host %s", c.host.ID)
	}
	if len(c.cluster.IPCollisions) == 0 {
		return ValidationSuccess, "IP collisions have not yet been evaluated"
	}

	var ipCollisions map[string][]string
	err := json.Unmarshal([]byte(c.cluster.IPCollisions), &ipCollisions)
	if err != nil {
		message := "Unable to unmarshall ip collision report for cluster"
		v.log.Errorf(message)
		return ValidationError, message
	}

	var collisionValidationText string
	hasCollisions := false
	for ip, macs := range ipCollisions {
		hasIP, err := v.inventoryHasIP(c.inventory, ip)
		if err != nil {
			message := fmt.Sprintf("inventory of host %s contains bad CIDR: %s", c.host.ID, err.Error())
			v.log.Errorf(message)
			return ValidationError, message
		}
		if hasIP {
			hasCollisions = true
			message := fmt.Sprintf("Collisions detected for host ID %s, IP address: %s Mac addresses: %s", c.host.ID, ip, strings.Join(macs[:], ","))
			v.log.Errorf(message)
			collisionValidationText += fmt.Sprintf("%s\n", message)
		}
	}
	if hasCollisions {
		return ValidationFailure, collisionValidationText
	}

	return ValidationSuccess, fmt.Sprintf("No IP collisions were detected by host %s", c.host.ID)
}

func (v *validator) inventoryHasIP(inventory *models.Inventory, ipAddress string) (bool, error) {
	ip := net.ParseIP(ipAddress)
	if ip != nil {
		for _, intf := range inventory.Interfaces {
			addresses := intf.IPV4Addresses
			addresses = append(addresses, intf.IPV6Addresses...)
			for _, address := range addresses {
				addrIP, _, err := net.ParseCIDR(address)
				if err != nil {
					return false, err
				}
				if addrIP.Equal(ip) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
