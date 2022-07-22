package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
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
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type ValidationStatus string

const (
	ValidationSuccess               ValidationStatus = "success"
	ValidationSuccessSuppressOutput ValidationStatus = "success-suppress-output"
	ValidationFailure               ValidationStatus = "failure"
	ValidationPending               ValidationStatus = "pending"
	ValidationError                 ValidationStatus = "error"
	ValidationDisabled              ValidationStatus = "disabled"
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

func (ic InventoryCache) GetOrUnmarshal(host *models.Host) (inventory *models.Inventory, err error) {
	if host.Inventory == "" {
		return nil, nil
	}
	var ok bool
	key := host.ID.String() + "@" + host.InfraEnvID.String()
	inventory, ok = ic[key]
	if !ok {
		inventory, err = common.UnmarshalInventory(host.Inventory)
		if err != nil {
			return
		}
		ic[key] = inventory
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
}

type validationCondition func(context *validationContext) ValidationStatus
type validationStringFormatter func(context *validationContext, status ValidationStatus) string

type validation struct {
	id            validationID
	condition     validationCondition
	formatter     validationStringFormatter
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
	if inventory.CPU == nil || inventory.Memory == nil || len(inventory.Disks) == 0 {
		return errors.New("Inventory is not valid")
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

func newValidationContext(host *models.Host, c *common.Cluster, i *common.InfraEnv, db *gorm.DB, inventoryCache InventoryCache, hwValidator hardware.Validator) (*validationContext, error) {
	ret := &validationContext{
		host:           host,
		db:             db,
		cluster:        c,
		infraEnv:       i,
		inventoryCache: inventoryCache,
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
}

func (v *validator) isMediaConnected(c *validationContext) ValidationStatus {
	return boolValue(c.host.MediaStatus == nil || *c.host.MediaStatus != models.HostMediaStatusDisconnected)
}

func (v *validator) isConnected(c *validationContext) ValidationStatus {
	return boolValue(c.host.CheckedInAt.String() == "" || time.Since(time.Time(c.host.CheckedInAt)) <= v.hwValidatorCfg.MaxHostDisconnectionTime)
}

func (v *validator) printConnected(context *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Host is connected"
	case ValidationFailure:
		return "Host is disconnected"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printMediaConnected(context *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Media device is connected"
	case ValidationFailure:
		return statusInfoMediaDisconnected
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasInventory(c *validationContext) ValidationStatus {
	return boolValue(c.inventory != nil)
}

func (v *validator) printHasInventory(context *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Valid inventory exists for the host"
	case ValidationFailure:
		return "Inventory has not been received for the host"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinCpuCores(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	return boolValue(c.inventory.CPU.Count >= c.minCPUCoresRequirement)
}

func (v *validator) printHasMinCpuCores(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Sufficient CPU cores"
	case ValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is %d, found only %d", c.minCPUCoresRequirement, c.inventory.CPU.Count)
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinMemory(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}

	return boolValue(c.inventory.Memory.PhysicalBytes >= conversions.MibToBytes(c.minRAMMibRequirement))
}

func (v *validator) compatibleWithClusterPlatform(c *validationContext) ValidationStatus {
	// Late binding
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if *c.cluster.Kind == models.ClusterKindAddHostsCluster {
		return ValidationSuccess
	}

	if c.inventory == nil || common.PlatformTypeValue(c.cluster.Platform.Type) == "" {
		return ValidationPending
	}
	supported, err := v.providerRegistry.IsHostSupported(common.PlatformTypeValue(c.cluster.Platform.Type), c.host)
	if err != nil {
		return ValidationError
	}
	if supported {
		return ValidationSuccess
	}
	return ValidationFailure
}

func (v *validator) printCompatibleWithClusterPlatform(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Host is compatible with cluster platform %s", common.PlatformTypeValue(c.cluster.Platform.Type))
	case ValidationFailure:
		hostAvailablePlatforms, _ := v.providerRegistry.GetSupportedProvidersByHosts([]*models.Host{c.host})
		return fmt.Sprintf("Host is not compatible with cluster platform %s; either disable this host or choose a compatible cluster platform (%v)",
			common.PlatformTypeValue(c.cluster.Platform.Type), hostAvailablePlatforms)
	case ValidationPending:
		return "Missing inventory or platform isn't set"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) getDiskEncryptionForDay2(host *models.Host) (*ignition_types.Luks, error) {
	var response models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(host.APIVipConnectivity), &response); err != nil {
		// APIVipConnectivityResponse is not available yet - retrying.
		return nil, err
	}

	// Parse ignition from APIVipConnectivity (LUKS is supported in version >= 3.2)
	config, _, err := v3_2.Parse([]byte(response.Ignition))
	if err != nil {
		v.log.WithError(err).Warn("Ignition is empty or invalid - can't get disk encryption")
		return nil, nil
	}

	// Checks if LUKS (disk encryption) exists
	if config.Storage.Luks == nil || len(config.Storage.Luks) == 0 {
		// Disk encryption is disabled
		return nil, nil
	}

	// Return LUKS object
	return &config.Storage.Luks[0], nil
}

func (v *validator) areTangServersReachable(c *validationContext) ValidationStatus {
	if c.host.TangConnectivity == "" {
		return ValidationPending
	}
	// Older agents have no action for tang-connectivity-check.
	// The missing action result will not fail host validations to
	// keep backward compatibility with older agents who did not include tang-connectivity-check.
	if strings.Contains(c.host.TangConnectivity, FailedToFindAction) {
		v.log.Warningf(
			"host %s replied to StepType: %s with: %s. Validation will pass to keep backward compatibility with discovery agent version: %s",
			c.host.ID.String(), models.StepTypeTangConnectivityCheck, c.host.TangConnectivity, c.host.DiscoveryAgentVersion,
		)
		return ValidationSuccessSuppressOutput
	}

	var response models.TangConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.TangConnectivity), &response); err != nil {
		return ValidationFailure
	}

	return boolValue(response.IsSuccess)
}

func (v *validator) diskEncryptionRequirementsSatisfied(c *validationContext) ValidationStatus {

	if c.infraEnv != nil || swag.StringValue(c.cluster.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone {
		return ValidationSuccessSuppressOutput
	}

	if c.inventory == nil {
		return ValidationPending
	}

	//day2 validation is taking the disk encryption data solely from
	//the host inventory and set the diskEncryption field on the cluster
	//according to that information
	if hostutil.IsDay2Host(c.host) {
		luks, err := v.getDiskEncryptionForDay2(c.host)
		if err != nil {
			return ValidationPending
		}
		if luks == nil || luks.Clevis == nil {
			// Disk encryption is disabled for workers on day1 cluster
			return ValidationSuccessSuppressOutput
		}

		c.cluster.DiskEncryption = &models.DiskEncryption{}
		if swag.BoolValue(luks.Clevis.Tpm2) {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTpmv2)
			// If Tpm2 is enabled for workers, check whether supported by the host.
			return boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
		} else if len(luks.Clevis.Tang) != 0 {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTang)
			return v.areTangServersReachable(c)
		} else {
			// Only Tpm2 and Tang are available for disk encryption
			return ValidationFailure
		}
	}

	//day 1 validation is relying on the host's role and the user
	//configuration to check if the disk encryption setup is valid
	role := common.GetEffectiveRole(c.host)
	if role == models.HostRoleAutoAssign {
		return ValidationPending
	}

	if !hostutil.IsDiskEncryptionEnabledForRole(*c.cluster.DiskEncryption, role) {
		return ValidationSuccessSuppressOutput
	}

	if swag.StringValue(c.cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTang {
		return v.areTangServersReachable(c)
	} else { // Mode TPMv2
		return boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
	}
}

func (v *validator) printDiskEncryptionRequirementsSatisfied(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Installation disk can be encrypted using %s", *c.cluster.DiskEncryption.Mode)
	case ValidationFailure:
		if swag.StringValue(c.cluster.DiskEncryption.Mode) == models.DiskEncryptionModeTang {
			return fmt.Sprintf("Could not validate that all Tang servers are reachable and working: %s", c.host.TangConnectivity)
		} else { // Mode TPMv2
			if c.inventory.TpmVersion == models.InventoryTpmVersionNone {
				return "TPM version could not be found, make sure TPM is enabled in host's BIOS"
			} else if c.cluster.DiskEncryption.Mode == nil {
				return "Invalid LUKS object in ignition - both TPM2 and Tang are not available"
			} else {
				return fmt.Sprintf("The host's TPM version is not supported, expected-version: %s, actual-version: %s",
					models.InventoryTpmVersionNr20, c.inventory.TpmVersion)
			}
		}

	case ValidationPending:
		if c.inventory == nil {
			return "Missing host inventory"
		}
		if common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign {
			return "Missing role assignment"
		}
		return "Missing ignition information"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printHasMinMemory(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Sufficient minimum RAM"
	case ValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is %s, found only %s",
			conversions.BytesToString(conversions.MibToBytes(c.minRAMMibRequirement)), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinValidDisks(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}

	disks := v.hwValidator.ListEligibleDisks(c.inventory)
	return boolValue(len(disks) > 0)
}

func (v *validator) printHasMinValidDisks(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Sufficient disk capacity"
	case ValidationFailure:
		return "No eligible disks were found, please check specific disks to see why they are not eligible"
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isMachineCidrDefined(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	return boolValue(swag.BoolValue(c.cluster.UserManagedNetworking) || swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || network.IsMachineCidrAvailable(c.cluster))
}

func (v *validator) printIsMachineCidrDefined(context *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "No Machine Network CIDR needed: User Managed Networking"
		}
		if swag.StringValue(context.cluster.Kind) == models.ClusterKindAddHostsCluster {
			return "No Machine Network CIDR needed: Day2 cluster"
		}
		return "Machine Network CIDR is defined"
	case ValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			return "Machine Network CIDR is undefined"
		} else {
			return "Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs"
		}
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasCPUCoresForRole(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	return boolValue(c.inventory.CPU.Count >= c.clusterHostRequirements.Total.CPUCores)
}

func (v *validator) printHasCPUCoresForRole(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Sufficient CPU cores for role %s", common.GetEffectiveRole(c.host))
	case ValidationFailure:
		return fmt.Sprintf("Require at least %d CPU cores for %s role, found only %d", c.clusterHostRequirements.Total.CPUCores, common.GetEffectiveRole(c.host), c.inventory.CPU.Count)
	case ValidationPending:
		return "Missing inventory or role"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMemoryForRole(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	requiredBytes := conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)
	return boolValue(c.inventory.Memory.PhysicalBytes >= requiredBytes)
}

func (v *validator) isValidPlatformNetworkSettings(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	if c.inventory.SystemVendor == nil {
		return ValidationError
	}
	if funk.ContainsString(invalidPlatforms, c.inventory.SystemVendor.ProductName) {
		// In case there is no cluster validation is pending
		if c.infraEnv != nil {
			return ValidationSuccessSuppressOutput
		} else {
			//In case userManagedNetworking is true, we don't care about the platform
			return boolValue(swag.BoolValue(c.cluster.UserManagedNetworking))
		}
	}
	return ValidationSuccess
}

func (v *validator) printValidPlatformNetworkSettings(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Platform %s is allowed", c.inventory.SystemVendor.ProductName)
	case ValidationFailure:
		return fmt.Sprintf("Platform %s is allowed only for Single Node OpenShift or user-managed networking", c.inventory.SystemVendor.ProductName)
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printHasMemoryForRole(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Sufficient RAM for role %s", common.GetEffectiveRole(c.host))
	case ValidationFailure:
		return fmt.Sprintf("Require at least %s RAM for role %s, found only %s",
			conversions.BytesToString(conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)), common.GetEffectiveRole(c.host), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
	case ValidationPending:
		return "Missing inventory or role"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) belongsToMachineCidr(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || (swag.BoolValue(c.cluster.UserManagedNetworking) && !common.IsSingleNodeCluster(c.cluster)) {
		return ValidationSuccess
	}
	if c.inventory == nil || !network.IsMachineCidrAvailable(c.cluster) {
		return ValidationPending
	}
	return boolValue(network.IsHostInPrimaryMachineNetCidr(v.log, c.cluster, c.host))
}

func (v *validator) printBelongsToMachineCidr(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No machine network CIDR validation needed: User Managed Networking"
		}
		if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster {
			return "No machine network CIDR validation needed: Day2 cluster"
		}
		return "Host belongs to all machine network CIDRs"
	case ValidationFailure:
		return "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"
	case ValidationPending:
		return "Missing inventory or machine network CIDR"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func getRealHostname(host *models.Host, inventory *models.Inventory) string {
	if host.RequestedHostname != "" {
		return host.RequestedHostname
	}
	return inventory.Hostname
}

func (v *validator) isHostnameUnique(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if c.inventory == nil {
		return ValidationPending
	}
	realHostname := getRealHostname(c.host, c.inventory)
	for _, h := range c.cluster.Hosts {
		if h.ID.String() != c.host.ID.String() && h.Inventory != "" {
			otherInventory, err := c.inventoryCache.GetOrUnmarshal(h)
			if err != nil {
				v.log.WithError(err).Warnf("Illegal inventory for host %s", h.ID.String())
				// It is not our hostname
				continue
			}
			if realHostname == getRealHostname(h, otherInventory) {
				return ValidationFailure
			}
		}
	}
	return ValidationSuccess
}

func (v *validator) printHostnameUnique(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Hostname %s is unique in cluster", getRealHostname(c.host, c.inventory))
	case ValidationFailure:
		return fmt.Sprintf("Hostname %s is not unique in cluster", getRealHostname(c.host, c.inventory))
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isHostnameValid(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}

	if err := hostutil.ValidateHostname(getRealHostname(c.host, c.inventory)); err != nil {
		return ValidationFailure
	}

	return ValidationSuccess
}

func (v *validator) printHostnameValid(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Hostname %s is allowed", getRealHostname(c.host, c.inventory))
	case ValidationFailure:
		if funk.ContainsString(hostutil.ForbiddenHostnames, getRealHostname(c.host, c.inventory)) {
			return fmt.Sprintf("The host name %s is forbidden", getRealHostname(c.host, c.inventory))
		}
		return fmt.Sprintf("Hostname %s is forbidden, hostname should match pattern %s", getRealHostname(c.host, c.inventory), hostutil.HostnamePattern)
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isIgnitionDownloadable(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if !hostutil.IsDay2Host(c.host) || swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccessSuppressOutput
	}
	if c.host.APIVipConnectivity == "" {
		return ValidationPending
	}
	var response models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.APIVipConnectivity), &response); err != nil {
		return ValidationFailure
	}

	return boolValue(response.IsSuccess)
}

func (v *validator) printIgnitionDownloadable(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No API connectivity needed: User Managed Networking"
		}
		return "Ignition is downloadable"
	case ValidationFailure:
		return "Ignition is not downloadable. Please ensure host connectivity to the cluster's API"
	case ValidationPending:
		return "Ignition is not yet available, pending API connectivity"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
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

func (v *validator) belongsToMajorityGroup(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if hostutil.IsDay2Host(c.host) || common.IsSingleNodeCluster(c.cluster) {
		return ValidationSuccess
	}
	if c.cluster.ConnectivityMajorityGroups == "" {
		return ValidationPending
	}
	var majorityGroups map[string][]strfmt.UUID
	err := json.Unmarshal([]byte(c.cluster.ConnectivityMajorityGroups), &majorityGroups)
	if err != nil {
		v.log.WithError(err).Warn("Parse majority group")
		return ValidationError
	}
	var ret ValidationStatus
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		ret = v.belongsToL3MajorityGroup(c, majorityGroups)
	} else {
		ret = v.belongsToL2MajorityGroup(c, majorityGroups)
	}
	if ret == ValidationFailure && len(c.cluster.Hosts) < 3 {
		return ValidationPending
	}
	return ret
}

func (v *validator) printBelongsToMajorityGroup(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if hostutil.IsDay2Host(c.host) {
			return "Day2 host is not required to be connected to other hosts in the cluster"
		}
		return "Host has connectivity to the majority of hosts in the cluster"
	case ValidationFailure:
		return "No connectivity to the majority of hosts in the cluster"
	case ValidationError:
		return "Parse error for connectivity majority group"
	case ValidationPending:
		if !network.IsMachineCidrAvailable(c.cluster) || c.cluster.ConnectivityMajorityGroups == "" {
			return "Machine Network CIDR or Connectivity Majority Groups missing"
		} else if len(c.cluster.Hosts) < 3 {
			return "Not enough hosts in cluster to calculate connectivity groups"
		}
		// Shouldn't happen
		return "Not enough information to calculate host majority groups"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
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

func (v *validator) isNTPSynced(c *validationContext) ValidationStatus {
	var sources []*models.NtpSource

	if c.host.NtpSources == "" {
		return v.missingNTPSyncResult(c.db, c.host)
	}

	if err := json.Unmarshal([]byte(c.host.NtpSources), &sources); err != nil {
		v.log.WithError(err).Warn("Parse NTP sources")
		return ValidationError
	}

	for _, source := range sources {
		if source.SourceState == models.SourceStateSynced {
			return ValidationSuccess
		}
	}

	return v.missingNTPSyncResult(c.db, c.host)
}

func (v *validator) printNTPSynced(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Host NTP is synced"
	case ValidationFailure:
		return "Host couldn't synchronize with any NTP server"
	case ValidationError:
		return "Parse error for NTP sources"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) sucessfullOrUnknownContainerImagesAvailability(c *validationContext) ValidationStatus {
	imageStatuses, err := common.UnmarshalImageStatuses(c.host.ImagesStatus)
	if err != nil {
		v.log.WithError(err).Warn("Parse container image statuses")
		return ValidationError
	}

	return boolValue(allImagesValid(imageStatuses))
}

func (v *validator) printSucessfullOrUnknownContainerImagesAvailability(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "All required container images were either pulled successfully or no attempt was made to pull them"
	case ValidationFailure:
		images, err := v.getFailedImagesNames(c.host)
		if err == nil {
			return fmt.Sprintf("Failed to fetch container images needed for installation from %s. "+
				"This may be due to a network hiccup. Retry to install again. If this problem persists, "+
				"check your network settings to make sure you’re not blocked.", strings.Join(images, ","))
		}
		fallthrough
	case ValidationError:
		return "Parse error for container image statuses"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
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

/*
   This is a pre-install validation that checks that the boot device was either not tested for sufficient disk speed
   or the disk speed check has been successful.  Since disk speed test is performed after installation has started,
   in order to have result for such test, the result has to be from a previous installation attempt.
   Since all pre-install validations have to pass before starting installation, it is mandatory that in case installation
   on the current boot device has not been attempted yet, this validation must pass.
*/
func (v *validator) sufficientOrUnknownInstallationDiskSpeed(c *validationContext) ValidationStatus {
	info, err := v.getBootDeviceInfo(c.host)
	if err != nil {
		return ValidationError
	}

	return boolValue(info == nil || info.DiskSpeed == nil || !info.DiskSpeed.Tested || info.DiskSpeed.ExitCode == 0)
}

func (v *validator) printSufficientOrUnknownInstallationDiskSpeed(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		info, _ := v.getBootDeviceInfo(c.host)
		if info == nil || info.DiskSpeed == nil || !info.DiskSpeed.Tested {
			return "Speed of installation disk has not yet been measured"
		}
		return "Speed of installation disk is sufficient"
	case ValidationFailure:
		return "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"
	case ValidationError:
		return "Error occurred while getting boot device"
	default:
		return fmt.Sprintf("Unexpected status %s", status.String())
	}

}

func (v *validator) hasSufficientNetworkLatencyRequirementForRole(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}

	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.NetworkLatencyThresholdMs == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return ValidationSuccess
	}

	if len(c.host.Connectivity) == 0 {
		return ValidationPending
	}

	status, _, _ := v.validateNetworkLatencyForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
	return status
}

func (v *validator) validateNetworkLatencyForRole(host *models.Host, clusterRoleReqs *models.ClusterHostRequirements, hosts []*models.Host) (ValidationStatus, []string, error) {
	connectivityReport, err := hostutil.UnmarshalConnectivityReport(host.Connectivity)
	if err != nil {
		v.log.Errorf("Unable to unmarshall host connectivity for %s:%s", host.ID, err)
		return ValidationError, nil, nil
	}
	failedHostIPs := map[string]struct{}{}
	failedHostLatencies := []string{}
	for _, r := range connectivityReport.RemoteHosts {
		for _, l3 := range r.L3Connectivity {
			if l3.AverageRTTMs > *clusterRoleReqs.Total.NetworkLatencyThresholdMs {
				if _, ok := failedHostIPs[l3.RemoteIPAddress]; !ok {
					hostname, role, err := GetHostnameAndEffectiveRoleByIP(l3.RemoteIPAddress, hosts)
					if err != nil {
						v.log.Error(err)
						return ValidationFailure, nil, err
					}
					if role == common.GetEffectiveRole(host) {
						failedHostIPs[l3.RemoteIPAddress] = struct{}{}
						failedHostLatencies = append(failedHostLatencies, fmt.Sprintf(" %s (%.2f ms)", hostname, l3.AverageRTTMs))
					}
				}
			}
		}
	}
	if len(failedHostLatencies) > 0 {
		return ValidationFailure, failedHostLatencies, nil
	}
	return ValidationSuccess, nil, nil
}

const (
	lessThanOr = "less than or"
	equals     = "equals"
)

func comparisonBuilder(value float64) string {
	if value > 0 {
		return fmt.Sprintf("%s %s", lessThanOr, equals)
	}
	return equals
}

func (v *validator) printSufficientNetworkLatencyRequirementForRole(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Network latency requirement has been satisfied."
	case ValidationFailure:
		_, hostLatencies, err := v.validateNetworkLatencyForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
		if err != nil {
			return fmt.Sprintf("Error while attempting to validate network latency: %s", err)
		}
		return fmt.Sprintf("Network latency requirements of %s %.2f ms not met for connectivity between %s and%s.", comparisonBuilder(*c.clusterHostRequirements.Total.NetworkLatencyThresholdMs), *c.clusterHostRequirements.Total.NetworkLatencyThresholdMs, c.host.ID, strings.Join(hostLatencies, ","))
	case ValidationPending:
		return "Missing network latency information."
	case ValidationError:
		return "Parse error while attempting to process the connectivity report"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasSufficientPacketLossRequirementForRole(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}

	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.PacketLossPercentage == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return ValidationSuccess
	}

	if len(c.host.Connectivity) == 0 {
		return ValidationPending
	}

	status, _, _ := v.validatePacketLossForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
	return status
}

func (v *validator) validatePacketLossForRole(host *models.Host, clusterRoleReqs *models.ClusterHostRequirements, hosts []*models.Host) (ValidationStatus, []string, error) {
	connectivityReport, err := hostutil.UnmarshalConnectivityReport(host.Connectivity)
	if err != nil {
		v.log.Errorf("Unable to unmarshall host connectivity for %s:%s", host.ID, err)
		return ValidationError, nil, nil
	}
	failedHostIPs := map[string]struct{}{}
	failedHostPacketLoss := []string{}
	for _, r := range connectivityReport.RemoteHosts {
		for _, l3 := range r.L3Connectivity {
			if l3.PacketLossPercentage > *clusterRoleReqs.Total.PacketLossPercentage {
				if _, ok := failedHostIPs[l3.RemoteIPAddress]; !ok {
					hostname, role, err := GetHostnameAndEffectiveRoleByIP(l3.RemoteIPAddress, hosts)
					if err != nil {
						v.log.Error(err)
						return ValidationFailure, nil, err
					}
					if role == common.GetEffectiveRole(host) {
						failedHostIPs[l3.RemoteIPAddress] = struct{}{}
						failedHostPacketLoss = append(failedHostPacketLoss, fmt.Sprintf(" %s (%.2f%%)", hostname, l3.PacketLossPercentage))
					}
				}
			}
		}
	}
	if len(failedHostPacketLoss) > 0 {
		return ValidationFailure, failedHostPacketLoss, nil
	}
	return ValidationSuccess, nil, nil
}

func (v *validator) printSufficientPacketLossRequirementForRole(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Packet loss requirement has been satisfied."
	case ValidationFailure:
		_, hostPacketLoss, err := v.validatePacketLossForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
		if err != nil {
			return fmt.Sprintf("Error while attempting to validate packet loss validation: %s", err)
		}
		return fmt.Sprintf("Packet loss percentage requirement of %s %.2f%% not met for connectivity between %s and%s.", comparisonBuilder(*c.clusterHostRequirements.Total.PacketLossPercentage), *c.clusterHostRequirements.Total.PacketLossPercentage, c.host.ID, strings.Join(hostPacketLoss, ","))
	case ValidationPending:
		return "Missing packet loss information."
	case ValidationError:
		return "Parse error while attempting to process the connectivity report"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasDefaultRoute(c *validationContext) ValidationStatus {

	if c.inventory == nil {
		return ValidationPending
	}

	if len(c.inventory.Routes) == 0 {
		return ValidationFailure
	}
	if v.validateDefaultRoute(c.inventory.Routes) {
		return ValidationSuccess
	}
	return ValidationFailure
}

func (v *validator) validateDefaultRoute(routes []*models.Route) bool {
	for _, r := range routes {
		if len(r.Destination) == 0 || len(r.Gateway) == 0 {
			continue
		}
		dst := net.ParseIP(r.Destination)
		if dst == nil {
			v.log.Errorf("unable to parse destination IP: %s", r.Destination)
			continue
		}
		gw := net.ParseIP(r.Gateway)
		if gw == nil {
			v.log.Errorf("unable to parse gateway IP: %s", r.Gateway)
			continue
		}
		if dst.IsUnspecified() && !gw.IsUnspecified() {
			return true
		}
	}
	return false
}

func (v *validator) printDefaultRoute(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Host has been configured with at least one default route."
	case ValidationFailure:
		return "Host has not yet been configured with a default route."
	case ValidationPending:
		return "Missing default routing information."
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func ignitionHasFile(ignition *ignition_types.Config, path string) bool {
	for _, file := range ignition.Storage.Files {
		if file.Path == path {
			return true
		}
	}

	return false
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

		return false
	}

	// We've exhausted all day-2 hosts, none of them seem to indicate that the
	// cluster doesn't (or does) have managed networking, so we're forced to
	// assume that it does in order to preserve our existing behavior of
	// assuming the cluster has managed networking
	return true
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
//
// Notes about cluster base DNS domain -
func (v *validator) shouldValidateDNSResolution(cluster *common.Cluster) (bool, ValidationStatus) {
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
			return false, ValidationPending
		}

		hasManagedNetworking = v.importedClusterHasManagedNetworking(cluster)
	}

	if hasManagedNetworking {
		// Clusters with managed networking never need DNS validations as they
		// automatically take care of the required DNS configuration within the
		// host
		return false, ValidationSuccess
	}

	if common.IsDay2Cluster(cluster) {
		// All day 2 clusters that don't have managed networking, regardless of
		// SNO or not, need DNS validations, as day-2 workers cannot benefit
		// for the SNO dnsmasq hack that is levereged by the day-1 cluster.
	} else {
		// This is a day-1, user-managed-networking cluster
		networkCfg, err := network.NewConfig()
		if err != nil {
			// This should never happen, so the values chosen here are arbitrary
			// and have no effect on anything
			return false, ValidationSuccess
		}

		if common.IsSingleNodeCluster(cluster) && networkCfg.EnableSingleNodeDnsmasq {
			// day-1 SNO clusters don't need to perform DNS validation when our
			// dnsmasq hack is enabled, as it takes care of having the required DNS
			// entries automatically configured within the host
			return false, ValidationSuccess
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
		return false, ValidationError
	}
	return true, ""
}

func domainNameToResolve(c *validationContext, name string) string {
	return fmt.Sprintf("%s.%s.%s", name, c.cluster.Name, c.cluster.BaseDNSDomain)
}

func (v *validator) isAPIDomainNameResolvedCorrectly(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if shouldValidate, ret := v.shouldValidateDNSResolution(c.cluster); !shouldValidate {
		return ret
	}
	apiDomainName := domainNameToResolve(c, constants.APIClusterSubdomain)
	return checkDomainNameResolution(c, apiDomainName)
}

func (v *validator) printIsAPIDomainNameResolvedCorrectly(c *validationContext, status ValidationStatus) string {
	apiDomainName := domainNameToResolve(c, constants.APIClusterSubdomain)
	return printIsDomainNameResolvedCorrectly(c, status, apiDomainName, "API")
}

func (v *validator) isAPIInternalDomainNameResolvedCorrectly(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if shouldValidate, ret := v.shouldValidateDNSResolution(c.cluster); !shouldValidate {
		return ret
	}
	apiInternalDomainName := domainNameToResolve(c, constants.InternalAPIClusterSubdomain)
	return checkDomainNameResolution(c, apiInternalDomainName)
}

func (v *validator) printIsAPIInternalDomainNameResolvedCorrectly(c *validationContext, status ValidationStatus) string {
	apiInternalDomainName := domainNameToResolve(c, constants.InternalAPIClusterSubdomain)
	return printIsDomainNameResolvedCorrectly(c, status, apiInternalDomainName, "internal API")
}

func (v *validator) isAppsDomainNameResolvedCorrectly(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if shouldValidate, ret := v.shouldValidateDNSResolution(c.cluster); !shouldValidate {
		return ret
	}
	appsDomainName := fmt.Sprintf("%s.apps.%s.%s", constants.AppsSubDomainNameHostDNSValidation, c.cluster.Name, c.cluster.BaseDNSDomain)
	return checkDomainNameResolution(c, appsDomainName)
}

func (v *validator) printIsAppsDomainNameResolvedCorrectly(c *validationContext, status ValidationStatus) string {
	appsDomainName := domainNameToResolve(c, "*.apps")
	return printIsDomainNameResolvedCorrectly(c, status, appsDomainName, "application ingress")
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

func checkDomainNameResolution(c *validationContext, domainName string) ValidationStatus {
	domain, err := getFirstMatchingResolution(c, domainName)
	if err != nil {
		return ValidationError
	}
	if domain != nil {
		_, domainResolved := domainResolvesToInventoryIP(domain, c.inventory)
		return boolValue(!domainResolved)
	}
	return ValidationFailure
}

func printIsDomainNameResolvedCorrectlySuccess(c *validationContext, status ValidationStatus, domainName string, destination string) string {
	domain := domainName
	if c.cluster.BaseDNSDomain == "" {
		domain = destination
	}

	return fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", domain)
}

func printIsDomainNameResolvedCorrectlyFailure(c *validationContext, status ValidationStatus, domainName string, destination string) string {
	domainResolution, err := getFirstMatchingResolution(c, domainName)
	if err != nil {
		return fmt.Sprintf("There was a problem while attempting to resolve the domain name %s. Error: %s", domainName, err)
	}

	if domainResolution != nil {
		ip, resolved := domainResolvesToInventoryIP(domainResolution, c.inventory)
		if resolved {
			return fmt.Sprintf("Domain %s must not point at %s as it is the API address of this host. This domain must instead point at the IP address of a load balancer when using user managed networking in a multi control-plane node cluster", domainName, ip)
		}
	}

	return fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address", domainName, destination)
}

func printIsDomainNameResolvedCorrectlyError(c *validationContext, status ValidationStatus, domainName string, destination string) string {
	if c.cluster.BaseDNSDomain == "" {
		return fmt.Sprintf("DNS validation for the %s domain cannot be completed because the cluster does not have base_dns_domain set. Please update the cluster with the correct base_dns_domain", destination)
	}

	return "Error while evaluating DNS resolution on this host"
}

func printIsDomainNameResolvedCorrectlyPending(c *validationContext, status ValidationStatus, domainName string, destination string) string {
	domain := domainName
	if c.cluster.BaseDNSDomain == "" {
		domain = destination
	}

	return fmt.Sprintf("DNS validation for the %s domain cannot be completed at the moment. This could be due to other validations", domain)
}

func printIsDomainNameResolvedCorrectly(c *validationContext, status ValidationStatus, domainName string, destination string) string {
	switch status {
	case ValidationPending:
		return printIsDomainNameResolvedCorrectlyPending(c, status, domainName, destination)
	case ValidationSuccess:
		return printIsDomainNameResolvedCorrectlySuccess(c, status, domainName, destination)
	case ValidationFailure:
		return printIsDomainNameResolvedCorrectlyFailure(c, status, domainName, destination)
	case ValidationError:
		return printIsDomainNameResolvedCorrectlyError(c, status, domainName, destination)
	default:
		return "Unexpected status"
	}
}

func (v *validator) isDNSWildcardNotConfigured(c *validationContext) ValidationStatus {
	if c.infraEnv != nil {
		return ValidationSuccessSuppressOutput
	}
	if hostutil.IsDay2Host(c.host) {
		return ValidationSuccess
	}
	var response *models.DomainResolutionResponse
	if err := json.Unmarshal([]byte(c.host.DomainNameResolutions), &response); err != nil {
		return ValidationError
	}
	dnsWildcardName := domainNameToResolve(c, constants.DNSWildcardFalseDomainName)

	// Note that we're validating that the wildcard DNS *.<cluster_name>.<base_domain> is NOT configured, since this causes known problems for OpenShift
	for _, domain := range response.Resolutions {
		if domain.DomainName != nil && *domain.DomainName == dnsWildcardName {
			if len(domain.IPV4Addresses) == 0 && len(domain.IPV6Addresses) == 0 {
				return ValidationSuccess
			}
		}
	}
	return ValidationFailure
}

func (v *validator) printIsDNSWildcardNotConfigured(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if hostutil.IsDay2Host(c.host) {
			return "DNS wildcard check is not required for day2"
		}
		return "DNS wildcard check was successful"
	case ValidationFailure:
		return fmt.Sprintf("DNS wildcard configuration was detected for domain *.%s.%s The installation will not be able to complete while the entry exists. Please remove it to proceed.", c.cluster.Name, c.cluster.BaseDNSDomain)
	case ValidationError:
		return "Parse error for domain name resolutions result"
	default:
		return "Unexpected status"
	}
}

func areNetworksOverlapping(c *validationContext) (ValidationStatus, error) {
	if c.inventory == nil || c.cluster == nil {
		return ValidationPending, nil
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

func (v *validator) nonOverlappingSubnets(c *validationContext) ValidationStatus {
	ret, err := areNetworksOverlapping(c)
	if err != nil {
		v.log.WithError(err).Errorf("Failed to check if CIDRs are overlapping for host %s infra-env %s", c.host.ID.String(), c.host.InfraEnvID.String())
	}
	return ret
}

func (v *validator) printNonOverlappingSubnets(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Host subnets are not overlapping"
	case ValidationPending:
		return "Missing inventory, or missing cluster"
	case ValidationFailure:
		_, err := areNetworksOverlapping(c)
		return fmt.Sprintf("Address networks are overlapping: %s", err.Error())
	case ValidationError:
		_, err := areNetworksOverlapping(c)
		return fmt.Sprintf("Unexpected error: %s", err.Error())
	}
	return fmt.Sprintf("Unexpected status %s", status)
}

func (v *validator) isVSphereDiskUUIDEnabled(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}

	if c.cluster == nil {
		return ValidationPending
	}

	if c.cluster.Platform == nil || c.cluster.Platform.Type == nil || *c.cluster.Platform.Type != models.PlatformTypeVsphere {
		return ValidationSuccess
	}

	if c.inventory.Disks == nil {
		return ValidationPending
	}

	for _, disk := range c.inventory.Disks {
		// vSphere only adds a UUID to disks which can potentially be used for storage,
		// if any of them doesn't have that flag, it's likely because the user has forgotten to
		// enable `disk.EnableUUID` for this virtual machine
		// See https://access.redhat.com/solutions/4606201
		if v.hwValidator.IsValidStorageDeviceType(disk) && !disk.HasUUID {
			return ValidationFailure
		}
	}

	return ValidationSuccess
}

func (v *validator) printVSphereUUIDEnabled(_ *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "VSphere disk.EnableUUID is enabled for this virtual machine"
	case ValidationFailure:
		return "VSphere disk.EnableUUID isn't enabled for this virtual machine, it's necessary for disks to be mounted properly"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) compatibleAgent(c *validationContext) ValidationStatus {
	if !v.hwValidatorCfg.EnableUpgradeAgent {
		return ValidationSuccess
	}
	if common.IsAgentCompatible(v.hwValidatorCfg.AgentDockerImage, c.host.DiscoveryAgentVersion) {
		return ValidationSuccess
	}
	return ValidationFailure
}

func (v *validator) printCompatibleAgent(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if v.hwValidatorCfg.EnableUpgradeAgent {
			return "Host agent is compatible with the service"
		}
		return "Host agent compatibility checking is disabled"
	case ValidationFailure:
		return "This host's agent is in the process of being upgraded to a compatible " +
			"version. This might take a few minutes"
	}
	return fmt.Sprintf("Unexpected status %s", status)
}
