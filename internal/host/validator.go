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
	"github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	internalmodels "github.com/openshift/assisted-service/internal/models"
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

const OpenStackPlatform = "OpenStack Compute"

var (
	ImageStatusDownloadRateThreshold = 0.001

	invalidPlatforms = []string{
		OpenStackPlatform,
	}

	forbiddenHostnames = []string{
		"localhost",
		"localhost.localdomain",
		"localhost4",
		"localhost4.localdomain4",
		"localhost6",
		"localhost6.localdomain6",
	}
)

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

type validationCondition func(context *validationContext) internalmodels.HostValidationStatus
type validationStringFormatter func(context *validationContext, status internalmodels.HostValidationStatus) string

type validation struct {
	id            internalmodels.HostValidationID
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

func boolValue(b bool) internalmodels.HostValidationStatus {
	if b {
		return internalmodels.HostValidationSuccess
	} else {
		return internalmodels.HostValidationFailure
	}
}

type validator struct {
	log              logrus.FieldLogger
	hwValidatorCfg   *hardware.ValidatorCfg
	hwValidator      hardware.Validator
	operatorsAPI     operators.API
	providerRegistry registry.ProviderRegistry
}

func (v *validator) isMediaConnected(c *validationContext) internalmodels.HostValidationStatus {
	return boolValue(c.host.MediaStatus == nil || *c.host.MediaStatus != models.HostMediaStatusDisconnected)
}

func (v *validator) isConnected(c *validationContext) internalmodels.HostValidationStatus {
	return boolValue(c.host.CheckedInAt.String() == "" || time.Since(time.Time(c.host.CheckedInAt)) <= v.hwValidatorCfg.MaxHostDisconnectionTime)
}

func (v *validator) printConnected(context *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Host is connected"
	case internalmodels.HostValidationFailure:
		return "Host is disconnected"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printMediaConnected(context *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Media device is connected"
	case internalmodels.HostValidationFailure:
		return statusInfoMediaDisconnected
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasInventory(c *validationContext) internalmodels.HostValidationStatus {
	return boolValue(c.inventory != nil)
}

func (v *validator) printHasInventory(context *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Valid inventory exists for the host"
	case internalmodels.HostValidationFailure:
		return "Inventory has not been received for the host"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinCpuCores(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}
	return boolValue(c.inventory.CPU.Count >= c.minCPUCoresRequirement)
}

func (v *validator) printHasMinCpuCores(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Sufficient CPU cores"
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is %d, found only %d", c.minCPUCoresRequirement, c.inventory.CPU.Count)
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinMemory(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}

	return boolValue(c.inventory.Memory.PhysicalBytes >= conversions.MibToBytes(c.minRAMMibRequirement))
}

func (v *validator) compatibleWithClusterPlatform(c *validationContext) internalmodels.HostValidationStatus {
	// Late binding
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if *c.cluster.Kind == models.ClusterKindAddHostsCluster {
		return internalmodels.HostValidationSuccess
	}

	if c.inventory == nil || common.PlatformTypeValue(c.cluster.Platform.Type) == "" {
		return internalmodels.HostValidationPending
	}
	supported, err := v.providerRegistry.IsHostSupported(common.PlatformTypeValue(c.cluster.Platform.Type), c.host)
	if err != nil {
		return internalmodels.HostValidationError
	}
	if supported {
		return internalmodels.HostValidationSuccess
	}
	return internalmodels.HostValidationFailure
}

func (v *validator) printCompatibleWithClusterPlatform(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Host is compatible with cluster platform %s", common.PlatformTypeValue(c.cluster.Platform.Type))
	case internalmodels.HostValidationFailure:
		hostAvailablePlatforms, _ := v.providerRegistry.GetSupportedProvidersByHosts([]*models.Host{c.host})
		return fmt.Sprintf("Host is not compatible with cluster platform %s; either disable this host or choose a compatible cluster platform (%v)",
			common.PlatformTypeValue(c.cluster.Platform.Type), hostAvailablePlatforms)
	case internalmodels.HostValidationPending:
		return "Missing inventory or platform isn't set"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func isDiskEncryptionEnabledForRole(encryption models.DiskEncryption, role models.HostRole) bool {
	switch swag.StringValue(encryption.EnableOn) {
	case models.DiskEncryptionEnableOnAll:
		return true
	case models.DiskEncryptionEnableOnMasters:
		return role == models.HostRoleMaster || role == models.HostRoleBootstrap
	case models.DiskEncryptionEnableOnWorkers:
		return role == models.HostRoleWorker
	default:
		return false
	}
}

func (v *validator) getDiskEncryptionForDay2(host *models.Host) (*types.Luks, error) {
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

func (v *validator) diskEncryptionRequirementsSatisfied(c *validationContext) internalmodels.HostValidationStatus {

	if c.infraEnv != nil || swag.StringValue(c.cluster.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone {
		return internalmodels.HostValidationSuccessSuppressOutput
	}

	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}

	//day2 validation is taking the disk encryption data solely from
	//the host inventory and set the diskEncryption field on the cluster
	//according to that information
	if hostutil.IsDay2Host(c.host) {
		luks, err := v.getDiskEncryptionForDay2(c.host)
		if err != nil {
			return internalmodels.HostValidationPending
		}
		if luks == nil {
			// Disk encryption is disabled for workers on day1 cluster
			return internalmodels.HostValidationSuccessSuppressOutput
		}

		c.cluster.DiskEncryption = &models.DiskEncryption{}
		if swag.BoolValue(luks.Clevis.Tpm2) {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTpmv2)
			// If Tpm2 is enabled for workers, check whether supported by the host.
			return boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
		} else if len(luks.Clevis.Tang) != 0 {
			c.cluster.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTang)
			// No nee to validate Tang
			return internalmodels.HostValidationSuccessSuppressOutput
		} else {
			// Only Tpm2 and Tang are available for disk encryption
			return internalmodels.HostValidationFailure
		}
	}

	//day 1 validation is relying on the host's role and the user
	//configuration to check if the disk encryption setup is valid
	role := common.GetEffectiveRole(c.host)
	if role == models.HostRoleAutoAssign {
		return internalmodels.HostValidationPending
	}

	if !isDiskEncryptionEnabledForRole(*c.cluster.DiskEncryption, role) {
		return internalmodels.HostValidationSuccessSuppressOutput
	}

	if *c.cluster.DiskEncryption.Mode != models.DiskEncryptionModeTpmv2 {
		return internalmodels.HostValidationSuccess
	}

	return boolValue(c.inventory.TpmVersion == models.InventoryTpmVersionNr20)
}

func (v *validator) printDiskEncryptionRequirementsSatisfied(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Installation disk can be encrypted using %s", *c.cluster.DiskEncryption.Mode)
	case internalmodels.HostValidationFailure:
		if c.inventory.TpmVersion == models.InventoryTpmVersionNone {
			return "TPM version could not be found, make sure TPM is enabled in host's BIOS"
		} else if c.cluster.DiskEncryption.Mode == nil {
			return "Invalid LUKS object in ignition - both TPM2 and Tang are not available"
		} else {
			return fmt.Sprintf("The host's TPM version is not supported, expected-version: %s, actual-version: %s",
				models.InventoryTpmVersionNr20, c.inventory.TpmVersion)
		}
	case internalmodels.HostValidationPending:
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

func (v *validator) printHasMinMemory(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Sufficient minimum RAM"
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is %s, found only %s",
			conversions.BytesToString(conversions.MibToBytes(c.minRAMMibRequirement)), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMinValidDisks(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}

	disks := v.hwValidator.ListEligibleDisks(c.inventory)
	return boolValue(len(disks) > 0)
}

func (v *validator) printHasMinValidDisks(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Sufficient disk capacity"
	case internalmodels.HostValidationFailure:
		return "No eligible disks were found, please check specific disks to see why they are not eligible"
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isMachineCidrDefined(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	return boolValue(swag.BoolValue(c.cluster.UserManagedNetworking) || swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || network.IsMachineCidrAvailable(c.cluster))
}

func (v *validator) printIsMachineCidrDefined(context *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "No Machine Network CIDR needed: User Managed Networking"
		}
		if swag.StringValue(context.cluster.Kind) == models.ClusterKindAddHostsCluster {
			return "No Machine Network CIDR needed: Day2 cluster"
		}
		return "Machine Network CIDR is defined"
	case internalmodels.HostValidationFailure:
		if swag.BoolValue(context.cluster.VipDhcpAllocation) {
			return "Machine Network CIDR is undefined"
		} else {
			return "Machine Network CIDR is undefined; the Machine Network CIDR can be defined by setting either the API or Ingress virtual IPs"
		}
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasCPUCoresForRole(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}
	return boolValue(c.inventory.CPU.Count >= c.clusterHostRequirements.Total.CPUCores)
}

func (v *validator) printHasCPUCoresForRole(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Sufficient CPU cores for role %s", common.GetEffectiveRole(c.host))
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Require at least %d CPU cores for %s role, found only %d", c.clusterHostRequirements.Total.CPUCores, common.GetEffectiveRole(c.host), c.inventory.CPU.Count)
	case internalmodels.HostValidationPending:
		return "Missing inventory or role"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasMemoryForRole(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}
	requiredBytes := conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)
	return boolValue(c.inventory.Memory.PhysicalBytes >= requiredBytes)
}

func (v *validator) isValidPlatformNetworkSettings(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}
	if c.inventory.SystemVendor == nil {
		return internalmodels.HostValidationError
	}
	if funk.ContainsString(invalidPlatforms, c.inventory.SystemVendor.ProductName) {
		// In case there is no cluster validation is pending
		if c.infraEnv != nil {
			return internalmodels.HostValidationSuccessSuppressOutput
		} else {
			//In case userManagedNetworking is true, we don't care about the platform
			return boolValue(swag.BoolValue(c.cluster.UserManagedNetworking))
		}
	}
	return internalmodels.HostValidationSuccess
}

func (v *validator) printValidPlatformNetworkSettings(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Platform %s is allowed", c.inventory.SystemVendor.ProductName)
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Platform %s is allowed only for Single Node OpenShift or user-managed networking", c.inventory.SystemVendor.ProductName)
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printHasMemoryForRole(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Sufficient RAM for role %s", common.GetEffectiveRole(c.host))
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Require at least %s RAM for role %s, found only %s",
			conversions.BytesToString(conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)), common.GetEffectiveRole(c.host), conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
	case internalmodels.HostValidationPending:
		return "Missing inventory or role"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) belongsToMachineCidr(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || (swag.BoolValue(c.cluster.UserManagedNetworking) && !common.IsSingleNodeCluster(c.cluster)) {
		return internalmodels.HostValidationSuccess
	}
	if c.inventory == nil || !network.IsMachineCidrAvailable(c.cluster) {
		return internalmodels.HostValidationPending
	}
	return boolValue(network.IsHostInPrimaryMachineNetCidr(v.log, c.cluster, c.host))
}

func (v *validator) printBelongsToMachineCidr(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No machine network CIDR validation needed: User Managed Networking"
		}
		if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster {
			return "No machine network CIDR validation needed: Day2 cluster"
		}
		return "Host belongs to all machine network CIDRs"
	case internalmodels.HostValidationFailure:
		return "Host does not belong to machine network CIDRs. Verify that the host belongs to every CIDR listed under machine networks"
	case internalmodels.HostValidationPending:
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

func (v *validator) isHostnameUnique(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if c.inventory == nil {
		return internalmodels.HostValidationPending
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
				return internalmodels.HostValidationFailure
			}
		}
	}
	return internalmodels.HostValidationSuccess
}

func (v *validator) printHostnameUnique(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Hostname %s is unique in cluster", getRealHostname(c.host, c.inventory))
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Hostname %s is not unique in cluster", getRealHostname(c.host, c.inventory))
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isHostnameValid(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}
	return boolValue(!funk.ContainsString(forbiddenHostnames, getRealHostname(c.host, c.inventory)))
}

func (v *validator) printHostnameValid(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return fmt.Sprintf("Hostname %s is allowed", getRealHostname(c.host, c.inventory))
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Hostname %s is forbidden", getRealHostname(c.host, c.inventory))
	case internalmodels.HostValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isIgnitionDownloadable(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if !hostutil.IsDay2Host(c.host) || swag.BoolValue(c.cluster.UserManagedNetworking) {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if c.host.APIVipConnectivity == "" {
		return internalmodels.HostValidationPending
	}
	var response models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.APIVipConnectivity), &response); err != nil {
		return internalmodels.HostValidationFailure
	}

	return boolValue(response.IsSuccess)
}

func (v *validator) printIgnitionDownloadable(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No API VIP needed: User Managed Networking"
		}
		return "Ignition is downloadable"
	case internalmodels.HostValidationFailure:
		return "Ignition is not downloadable. Please ensure host connectivity to the cluster's API VIP."
	case internalmodels.HostValidationPending:
		return "Ignition is not ready, pending API VIP connectivity."
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) belongsToL2MajorityGroup(c *validationContext, majorityGroups map[string][]strfmt.UUID) internalmodels.HostValidationStatus {
	if !network.IsMachineCidrAvailable(c.cluster) {
		return internalmodels.HostValidationPending
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
			return internalmodels.HostValidationError
		}
		if !funk.Contains(groupForNetwork(machineIpnet), *c.host.ID) {
			return internalmodels.HostValidationFailure
		}
	}

	return internalmodels.HostValidationSuccess
}

func (v *validator) belongsToL3MajorityGroup(c *validationContext, majorityGroups map[string][]strfmt.UUID) internalmodels.HostValidationStatus {
	ipv4, ipv6, err := network.GetConfiguredAddressFamilies(c.cluster)
	if err != nil {
		v.log.WithError(err).Warn("Get configured address families")
		return internalmodels.HostValidationError
	}
	if !(ipv4 || ipv6) {
		return internalmodels.HostValidationFailure
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

func (v *validator) belongsToMajorityGroup(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if hostutil.IsDay2Host(c.host) || common.IsSingleNodeCluster(c.cluster) {
		return internalmodels.HostValidationSuccess
	}
	if c.cluster.ConnectivityMajorityGroups == "" {
		return internalmodels.HostValidationPending
	}
	var majorityGroups map[string][]strfmt.UUID
	err := json.Unmarshal([]byte(c.cluster.ConnectivityMajorityGroups), &majorityGroups)
	if err != nil {
		v.log.WithError(err).Warn("Parse majority group")
		return internalmodels.HostValidationError
	}
	var ret internalmodels.HostValidationStatus
	if swag.BoolValue(c.cluster.UserManagedNetworking) {
		ret = v.belongsToL3MajorityGroup(c, majorityGroups)
	} else {
		ret = v.belongsToL2MajorityGroup(c, majorityGroups)
	}
	if ret == internalmodels.HostValidationFailure && len(c.cluster.Hosts) < 3 {
		return internalmodels.HostValidationPending
	}
	return ret
}

func (v *validator) printBelongsToMajorityGroup(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if hostutil.IsDay2Host(c.host) {
			return "Day2 host is not required to be connected to other hosts in the cluster"
		}
		return "Host has connectivity to the majority of hosts in the cluster"
	case internalmodels.HostValidationFailure:
		return "No connectivity to the majority of hosts in the cluster"
	case internalmodels.HostValidationError:
		return "Parse error for connectivity majority group"
	case internalmodels.HostValidationPending:
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

func (v *validator) missingNTPSyncResult(db *gorm.DB, host *models.Host) internalmodels.HostValidationStatus {
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
			return internalmodels.HostValidationError
		}
		if sources == "" {
			return internalmodels.HostValidationSuccessSuppressOutput
		}
	}
	return internalmodels.HostValidationFailure
}

func (v *validator) isNTPSynced(c *validationContext) internalmodels.HostValidationStatus {
	var sources []*models.NtpSource

	if c.host.NtpSources == "" {
		return v.missingNTPSyncResult(c.db, c.host)
	}

	if err := json.Unmarshal([]byte(c.host.NtpSources), &sources); err != nil {
		v.log.WithError(err).Warn("Parse NTP sources")
		return internalmodels.HostValidationError
	}

	for _, source := range sources {
		if source.SourceState == models.SourceStateSynced {
			return internalmodels.HostValidationSuccess
		}
	}

	return v.missingNTPSyncResult(c.db, c.host)
}

func (v *validator) printNTPSynced(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Host NTP is synced"
	case internalmodels.HostValidationFailure:
		return "Host couldn't synchronize with any NTP server"
	case internalmodels.HostValidationError:
		return "Parse error for NTP sources"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) sucessfullOrUnknownContainerImagesAvailability(c *validationContext) internalmodels.HostValidationStatus {
	imageStatuses, err := common.UnmarshalImageStatuses(c.host.ImagesStatus)
	if err != nil {
		v.log.WithError(err).Warn("Parse container image statuses")
		return internalmodels.HostValidationError
	}

	return boolValue(allImagesValid(imageStatuses))
}

func (v *validator) printSucessfullOrUnknownContainerImagesAvailability(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "All required container images were either pulled successfully or no attempt was made to pull them"
	case internalmodels.HostValidationFailure:
		images, err := v.getFailedImagesNames(c.host)
		if err == nil {
			return fmt.Sprintf("Failed to fetch container images needed for installation from %s. "+
				"This may be due to a network hiccup. Retry to install again. If this problem persists, "+
				"check your network settings to make sure you’re not blocked.", strings.Join(images, ","))
		}
		fallthrough
	case internalmodels.HostValidationError:
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
func (v *validator) sufficientOrUnknownInstallationDiskSpeed(c *validationContext) internalmodels.HostValidationStatus {
	info, err := v.getBootDeviceInfo(c.host)
	if err != nil {
		return internalmodels.HostValidationError
	}

	return boolValue(info == nil || info.DiskSpeed == nil || !info.DiskSpeed.Tested || info.DiskSpeed.ExitCode == 0)
}

func (v *validator) printSufficientOrUnknownInstallationDiskSpeed(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		info, _ := v.getBootDeviceInfo(c.host)
		if info == nil || info.DiskSpeed == nil || !info.DiskSpeed.Tested {
			return "Speed of installation disk has not yet been measured"
		}
		return "Speed of installation disk is sufficient"
	case internalmodels.HostValidationFailure:
		return "While preparing the previous installation the installation disk speed measurement failed or was found to be insufficient"
	case internalmodels.HostValidationError:
		return "Error occurred while getting boot device"
	default:
		return fmt.Sprintf("Unexpected status %s", status.String())
	}

}

func (v *validator) hasSufficientNetworkLatencyRequirementForRole(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}

	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.NetworkLatencyThresholdMs == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return internalmodels.HostValidationSuccess
	}

	if len(c.host.Connectivity) == 0 {
		return internalmodels.HostValidationPending
	}

	status, _, _ := v.validateNetworkLatencyForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
	return status
}

func (v *validator) validateNetworkLatencyForRole(host *models.Host, clusterRoleReqs *models.ClusterHostRequirements, hosts []*models.Host) (internalmodels.HostValidationStatus, []string, error) {
	connectivityReport, err := hostutil.UnmarshalConnectivityReport(host.Connectivity)
	if err != nil {
		v.log.Errorf("Unable to unmarshall host connectivity for %s:%s", host.ID, err)
		return internalmodels.HostValidationError, nil, nil
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
						return internalmodels.HostValidationFailure, nil, err
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
		return internalmodels.HostValidationFailure, failedHostLatencies, nil
	}
	return internalmodels.HostValidationSuccess, nil, nil
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

func (v *validator) printSufficientNetworkLatencyRequirementForRole(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Network latency requirement has been satisfied."
	case internalmodels.HostValidationFailure:
		_, hostLatencies, err := v.validateNetworkLatencyForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
		if err != nil {
			return fmt.Sprintf("Error while attempting to validate network latency: %s", err)
		}
		return fmt.Sprintf("Network latency requirements of %s %.2f ms not met for connectivity between %s and%s.", comparisonBuilder(*c.clusterHostRequirements.Total.NetworkLatencyThresholdMs), *c.clusterHostRequirements.Total.NetworkLatencyThresholdMs, c.host.ID, strings.Join(hostLatencies, ","))
	case internalmodels.HostValidationPending:
		return "Missing network latency information."
	case internalmodels.HostValidationError:
		return "Parse error while attempting to process the connectivity report"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasSufficientPacketLossRequirementForRole(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}

	if len(c.cluster.Hosts) == 1 || c.clusterHostRequirements.Total.PacketLossPercentage == nil || common.GetEffectiveRole(c.host) == models.HostRoleAutoAssign || hostutil.IsDay2Host(c.host) {
		// Single Node use case || no requirements defined || role is auto assign
		return internalmodels.HostValidationSuccess
	}

	if len(c.host.Connectivity) == 0 {
		return internalmodels.HostValidationPending
	}

	status, _, _ := v.validatePacketLossForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
	return status
}

func (v *validator) validatePacketLossForRole(host *models.Host, clusterRoleReqs *models.ClusterHostRequirements, hosts []*models.Host) (internalmodels.HostValidationStatus, []string, error) {
	connectivityReport, err := hostutil.UnmarshalConnectivityReport(host.Connectivity)
	if err != nil {
		v.log.Errorf("Unable to unmarshall host connectivity for %s:%s", host.ID, err)
		return internalmodels.HostValidationError, nil, nil
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
						return internalmodels.HostValidationFailure, nil, err
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
		return internalmodels.HostValidationFailure, failedHostPacketLoss, nil
	}
	return internalmodels.HostValidationSuccess, nil, nil
}

func (v *validator) printSufficientPacketLossRequirementForRole(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Packet loss requirement has been satisfied."
	case internalmodels.HostValidationFailure:
		_, hostPacketLoss, err := v.validatePacketLossForRole(c.host, c.clusterHostRequirements, c.cluster.Hosts)
		if err != nil {
			return fmt.Sprintf("Error while attempting to validate packet loss validation: %s", err)
		}
		return fmt.Sprintf("Packet loss percentage requirement of %s %.2f%% not met for connectivity between %s and%s.", comparisonBuilder(*c.clusterHostRequirements.Total.PacketLossPercentage), *c.clusterHostRequirements.Total.PacketLossPercentage, c.host.ID, strings.Join(hostPacketLoss, ","))
	case internalmodels.HostValidationPending:
		return "Missing packet loss information."
	case internalmodels.HostValidationError:
		return "Parse error while attempting to process the connectivity report"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) hasDefaultRoute(c *validationContext) internalmodels.HostValidationStatus {

	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}

	if len(c.inventory.Routes) == 0 {
		return internalmodels.HostValidationFailure
	}
	if v.validateDefaultRoute(c.inventory.Routes) {
		return internalmodels.HostValidationSuccess
	}
	return internalmodels.HostValidationFailure
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

func (v *validator) printDefaultRoute(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Host has been configured with at least one default route."
	case internalmodels.HostValidationFailure:
		return "Host has not yet been configured with a default route."
	case internalmodels.HostValidationPending:
		return "Missing default routing information."
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func shouldValidateDnsResolution(c *validationContext) bool {
	// Skip DNS resolution checks in IPI network mode
	if !swag.BoolValue(c.cluster.UserManagedNetworking) {
		return false
	}

	// If its an SNO cluster with DNSMasq manifests enabled the check should be skipped
	networkCfg, err := network.NewConfig()
	if err != nil {
		return false
	}
	return !(common.IsSingleNodeCluster(c.cluster) && networkCfg.EnableSingleNodeDnsmasq)
}

func domainNameToResolve(c *validationContext, name string) string {
	return fmt.Sprintf("%s.%s.%s", name, c.cluster.Name, c.cluster.BaseDNSDomain)
}

func (v *validator) isAPIDomainNameResolvedCorrectly(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if !shouldValidateDnsResolution(c) {
		return internalmodels.HostValidationSuccess
	}
	apiDomainName := domainNameToResolve(c, constants.APIName)
	return checkDomainNameResolution(c, apiDomainName)
}

func (v *validator) printIsAPIDomainNameResolvedCorrectly(c *validationContext, status internalmodels.HostValidationStatus) string {
	apiDomainName := domainNameToResolve(c, constants.APIName)
	return printIsDomainNameResolvedCorrectly(c, status, apiDomainName, "API load balancer")
}

func (v *validator) isAPIInternalDomainNameResolvedCorrectly(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if !shouldValidateDnsResolution(c) {
		return internalmodels.HostValidationSuccess
	}
	apiInternalDomainName := domainNameToResolve(c, constants.APIInternalName)
	return checkDomainNameResolution(c, apiInternalDomainName)
}

func (v *validator) printIsAPIInternalDomainNameResolvedCorrectly(c *validationContext, status internalmodels.HostValidationStatus) string {
	apiInternalDomainName := domainNameToResolve(c, constants.APIInternalName)
	return printIsDomainNameResolvedCorrectly(c, status, apiInternalDomainName, "API load balancer")
}

func (v *validator) isAppsDomainNameResolvedCorrectly(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if !shouldValidateDnsResolution(c) {
		return internalmodels.HostValidationSuccess
	}
	appsDomainName := fmt.Sprintf("%s.apps.%s.%s", constants.AppsSubDomainNameHostDNSValidation, c.cluster.Name, c.cluster.BaseDNSDomain)
	return checkDomainNameResolution(c, appsDomainName)
}

func (v *validator) printIsAppsDomainNameResolvedCorrectly(c *validationContext, status internalmodels.HostValidationStatus) string {
	appsDomainName := domainNameToResolve(c, "*.apps")
	return printIsDomainNameResolvedCorrectly(c, status, appsDomainName, "application Ingress load balancer")
}

func checkDomainNameResolution(c *validationContext, domainName string) internalmodels.HostValidationStatus {
	var response *models.DomainResolutionResponse

	if err := json.Unmarshal([]byte(c.host.DomainNameResolutions), &response); err != nil {
		return internalmodels.HostValidationError
	}

	for _, domain := range response.Resolutions {
		if domain.DomainName != nil && *domain.DomainName == domainName {
			if len(domain.IPV4Addresses) != 0 || len(domain.IPV6Addresses) != 0 {
				return internalmodels.HostValidationSuccess
			}
		}
	}
	return internalmodels.HostValidationFailure
}

func printIsDomainNameResolvedCorrectly(c *validationContext, status internalmodels.HostValidationStatus, domainName string, destination string) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if !swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "Domain name resolution is not required (managed networking)"
		}
		return fmt.Sprintf("Domain name resolution was successful for domain %s", domainName)
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your %s.", domainName, destination)
	case internalmodels.HostValidationError:
		return "Parse error for domain name resolutions result"
	default:
		return "Unexpected status"
	}
}

func (v *validator) isDNSWildcardNotConfigured(c *validationContext) internalmodels.HostValidationStatus {
	if c.infraEnv != nil {
		return internalmodels.HostValidationSuccessSuppressOutput
	}
	if hostutil.IsDay2Host(c.host) {
		return internalmodels.HostValidationSuccess
	}
	var response *models.DomainResolutionResponse
	if err := json.Unmarshal([]byte(c.host.DomainNameResolutions), &response); err != nil {
		return internalmodels.HostValidationError
	}
	dnsWildcardName := domainNameToResolve(c, constants.DNSWildcardFalseDomainName)

	// Note that we're validating that the wildcard DNS *.<cluster_name>.<base_domain> is NOT configured, since this causes known problems for OpenShift
	for _, domain := range response.Resolutions {
		if domain.DomainName != nil && *domain.DomainName == dnsWildcardName {
			if len(domain.IPV4Addresses) == 0 && len(domain.IPV6Addresses) == 0 {
				return internalmodels.HostValidationSuccess
			}
		}
	}
	return internalmodels.HostValidationFailure
}

func (v *validator) printIsDNSWildcardNotConfigured(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		if hostutil.IsDay2Host(c.host) {
			return "DNS wildcard check is not required for day2"
		}
		return "DNS wildcard check was successful"
	case internalmodels.HostValidationFailure:
		return fmt.Sprintf("DNS wildcard configuration was detected for domain *.%s.%s The installation will not be able to complete while the entry exists. Please remove it to proceed.", c.cluster.Name, c.cluster.BaseDNSDomain)
	case internalmodels.HostValidationError:
		return "Parse error for domain name resolutions result"
	default:
		return "Unexpected status"
	}
}

func areNetworksOverlapping(c *validationContext) (internalmodels.HostValidationStatus, error) {
	if c.inventory == nil || c.cluster == nil {
		return internalmodels.HostValidationPending, nil
	}
	families, err := network.GetClusterAddressFamilies(c.cluster)
	if err != nil {
		return internalmodels.HostValidationError, err
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
			return internalmodels.HostValidationError, err
		}
		for i := 0; i < len(networks); i++ {
			for j := i + 1; j < len(networks); j++ {
				ci1 := networks[i]
				ci2 := networks[j]
				if ci1.Cidr == ci2.Cidr {
					return internalmodels.HostValidationFailure, errors.Errorf("Address network %s appears on multiple interfaces [%s, %s]", ci1.Cidr, ci1.InterfaceName, ci2.InterfaceName)
				}
				overlap, err := network.NetworksOverlap(ci1.Cidr, ci2.Cidr)
				if err != nil {
					return internalmodels.HostValidationError, err
				}
				if overlap {
					return internalmodels.HostValidationFailure, errors.Errorf("Address networks %s in interface %s and %s in interface %s overlap", ci1.Cidr, ci1.InterfaceName,
						ci2.Cidr, ci2.InterfaceName)
				}
			}
		}
	}
	return internalmodels.HostValidationSuccess, nil
}

func (v *validator) nonOverlappingSubnets(c *validationContext) internalmodels.HostValidationStatus {
	ret, err := areNetworksOverlapping(c)
	if err != nil {
		v.log.WithError(err).Errorf("Failed to check if CIDRs are overlapping for host %s infra-env %s", c.host.ID.String(), c.host.InfraEnvID.String())
	}
	return ret
}

func (v *validator) printNonOverlappingSubnets(c *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "Host subnets are not overlapping"
	case internalmodels.HostValidationPending:
		return "Missing inventory, or missing cluster"
	case internalmodels.HostValidationFailure:
		_, err := areNetworksOverlapping(c)
		return fmt.Sprintf("Address networks are overlapping: %s", err.Error())
	case internalmodels.HostValidationError:
		_, err := areNetworksOverlapping(c)
		return fmt.Sprintf("Unexpected error: %s", err.Error())
	}
	return fmt.Sprintf("Unexpected status %s", status)
}

func (v *validator) isVSphereDiskUUIDEnabled(c *validationContext) internalmodels.HostValidationStatus {
	if c.inventory == nil {
		return internalmodels.HostValidationPending
	}

	if c.cluster == nil {
		return internalmodels.HostValidationPending
	}

	if c.cluster.Platform == nil || c.cluster.Platform.Type == nil || *c.cluster.Platform.Type != models.PlatformTypeVsphere {
		return internalmodels.HostValidationSuccess
	}

	if c.inventory.Disks == nil {
		return internalmodels.HostValidationPending
	}

	for _, disk := range c.inventory.Disks {
		// vSphere only adds a UUID to disks which can potentially be used for storage,
		// if any of them doesn't have that flag, it's likely because the user has forgotten to
		// enable `disk.EnableUUID` for this virtual machine
		// See https://access.redhat.com/solutions/4606201
		if v.hwValidator.IsValidStorageDeviceType(disk) && !disk.HasUUID {
			return internalmodels.HostValidationFailure
		}
	}

	return internalmodels.HostValidationSuccess
}

func (v *validator) printVSphereUUIDEnabled(_ *validationContext, status internalmodels.HostValidationStatus) string {
	switch status {
	case internalmodels.HostValidationSuccess:
		return "VSphere disk.EnableUUID is enabled for this virtual machine"
	case internalmodels.HostValidationFailure:
		return "VSphere disk.EnableUUID isn't enabled for this virtual machine, it's necessary for disks to be mounted properly"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}
