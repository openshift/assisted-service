package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

type ValidationStatus string

const (
	ValidationSuccess ValidationStatus = "success"
	ValidationFailure ValidationStatus = "failure"
	ValidationPending ValidationStatus = "pending"
	ValidationError   ValidationStatus = "error"
)

var (
	ImageStatusDownloadRateThreshold = 0.001

	invalidPlatforms = []string{
		"OpenStack Compute",
	}

	forbiddenHostnames = []string{
		"localhost",
	}
)

func (v ValidationStatus) String() string {
	return string(v)
}

type validationContext struct {
	host                    *models.Host
	cluster                 *common.Cluster
	inventory               *models.Inventory
	db                      *gorm.DB
	clusterHostRequirements *models.ClusterHostRequirements
}

type validationConditon func(context *validationContext) ValidationStatus
type validationStringFormatter func(context *validationContext, status ValidationStatus) string

type validation struct {
	id        validationID
	condition validationConditon
	formatter validationStringFormatter
}

func (c *validationContext) loadCluster() error {
	clusterFromDB, err := common.GetClusterFromDBWithoutDisabledHosts(c.db, c.host.ClusterID)
	if err == nil {
		c.cluster = clusterFromDB
	}
	return err
}

func (c *validationContext) loadInventory() error {
	if c.host.Inventory != "" {
		var inventory models.Inventory
		err := json.Unmarshal([]byte(c.host.Inventory), &inventory)
		if err != nil {
			return err
		}
		if inventory.CPU == nil || inventory.Memory == nil || len(inventory.Disks) == 0 {
			return errors.Errorf("Inventory is not valid")
		}
		c.inventory = &inventory
	}
	return nil
}

func (v *validator) getBootDeviceInfo(host *models.Host) (*models.DiskInfo, error) {
	bootDevice, err := hardware.GetBootDevice(v.log, v.hwValidator, host)
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
	switch c.host.Role {
	case models.HostRoleMaster, models.HostRoleWorker, models.HostRoleAutoAssign:
		return nil
	default:
		return errors.Errorf("Illegal role defined: %s", c.host.Role)
	}
}

func (c *validationContext) validateMachineCIDR() error {
	var err error
	if c.cluster.MachineNetworkCidr != "" {
		_, _, err = net.ParseCIDR(c.cluster.MachineNetworkCidr)
	}
	return err
}

func (c *validationContext) loadClusterHostRequirements(hwValidator hardware.Validator) error {
	requirements, err := hwValidator.GetClusterHostRequirements(context.TODO(), c.cluster, c.host)
	c.clusterHostRequirements = requirements
	return err
}

func newValidationContext(host *models.Host, db *gorm.DB, hwValidator hardware.Validator) (*validationContext, error) {
	ret := &validationContext{
		host: host,
		db:   db,
	}
	err := ret.loadCluster()
	if err == nil {
		err = ret.loadInventory()
	}
	if err == nil {
		err = ret.validateRole()
	}
	if err == nil {
		err = ret.validateMachineCIDR()
	}
	if err == nil {
		err = ret.loadClusterHostRequirements(hwValidator)
	}
	if err != nil {
		return nil, err
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
	log            logrus.FieldLogger
	hwValidatorCfg *hardware.ValidatorCfg
	hwValidator    hardware.Validator
	operatorsAPI   operators.API
}

func (v *validator) isConnected(c *validationContext) ValidationStatus {
	return boolValue(c.host.CheckedInAt.String() == "" || time.Since(time.Time(c.host.CheckedInAt)) <= MaxHostDisconnectionTime)
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
	return boolValue(c.inventory.CPU.Count >= v.hwValidatorCfg.MinCPUCores)
}

func (v *validator) printHasMinCpuCores(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Sufficient CPU cores"
	case ValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required CPU cores for any role is %d, found only %d", v.hwValidatorCfg.MinCPUCores, c.inventory.CPU.Count)
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
	return boolValue(c.inventory.Memory.PhysicalBytes >= conversions.GibToBytes(v.hwValidatorCfg.MinRamGib))
}

func (v *validator) printHasMinMemory(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "Sufficient minimum RAM"
	case ValidationFailure:
		return fmt.Sprintf("The host is not eligible to participate in Openshift Cluster because the minimum required RAM for any role is %s, found only %s", conversions.BytesToString(conversions.GibToBytes(v.hwValidatorCfg.MinRamGib)),
			conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
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
		return fmt.Sprintf("Require a disk of at least %d GB", v.hwValidatorCfg.MinDiskSizeGb)
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isMachineCidrDefined(c *validationContext) ValidationStatus {
	return boolValue(swag.BoolValue(c.cluster.UserManagedNetworking) || swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || c.cluster.MachineNetworkCidr != "")
}

func (v *validator) printIsMachineCidrDefined(context *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(context.cluster.UserManagedNetworking) {
			return "No Machine Network CIDR needed: User Managed Networking"
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
		return fmt.Sprintf("Sufficient CPU cores for role %s", c.host.Role)
	case ValidationFailure:
		return fmt.Sprintf("Require at least %d CPU cores for %s role, found only %d", c.clusterHostRequirements.Total.CPUCores, c.host.Role, c.inventory.CPU.Count)
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

func (v *validator) isValidPlatform(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	if c.inventory.SystemVendor == nil {
		return ValidationError
	}
	return boolValue(!funk.ContainsString(invalidPlatforms, c.inventory.SystemVendor.ProductName))
}

func (v *validator) printValidPlatform(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Platform %s is allowed", c.inventory.SystemVendor.ProductName)
	case ValidationFailure:
		return fmt.Sprintf("Platform %s is forbidden", c.inventory.SystemVendor.ProductName)
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) printHasMemoryForRole(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Sufficient RAM for role %s", c.host.Role)
	case ValidationFailure:
		return fmt.Sprintf("Require at least %s RAM for role %s, found only %s",
			conversions.BytesToString(conversions.MibToBytes(c.clusterHostRequirements.Total.RAMMib)), c.host.Role, conversions.BytesToString(c.inventory.Memory.PhysicalBytes))
	case ValidationPending:
		return "Missing inventory or role"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) belongsToMachineCidr(c *validationContext) ValidationStatus {
	if swag.StringValue(c.cluster.Kind) == models.ClusterKindAddHostsCluster || swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	if c.inventory == nil || c.cluster.MachineNetworkCidr == "" {
		return ValidationPending
	}
	return boolValue(network.IsHostInMachineNetCidr(v.log, c.cluster, c.host))
}

func (v *validator) printBelongsToMachineCidr(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No machine network CIDR validation needed: User Managed Networking"
		}
		return fmt.Sprintf("Host belongs to machine network CIDR %s", c.cluster.MachineNetworkCidr)
	case ValidationFailure:
		return fmt.Sprintf("Host does not belong to machine network CIDR %s", c.cluster.MachineNetworkCidr)
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
	if c.inventory == nil {
		return ValidationPending
	}
	realHostname := getRealHostname(c.host, c.inventory)
	for _, h := range c.cluster.Hosts {
		if h.ID.String() != c.host.ID.String() && h.Inventory != "" {
			var otherInventory models.Inventory
			if err := json.Unmarshal([]byte(h.Inventory), &otherInventory); err != nil {
				v.log.WithError(err).Warnf("Illegal inventory for host %s", h.ID.String())
				// It is not our hostname
				continue
			}
			if realHostname == getRealHostname(h, &otherInventory) {
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
	return boolValue(!funk.ContainsString(forbiddenHostnames, getRealHostname(c.host, c.inventory)))
}

func (v *validator) printHostnameValid(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return fmt.Sprintf("Hostname %s is allowed", getRealHostname(c.host, c.inventory))
	case ValidationFailure:
		return fmt.Sprintf("Hostname %s is forbidden", getRealHostname(c.host, c.inventory))
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isAPIVipConnected(c *validationContext) ValidationStatus {
	if c.inventory == nil {
		return ValidationPending
	}
	if !hostutil.IsDay2Host(c.host) || swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	var response models.APIVipConnectivityResponse
	if err := json.Unmarshal([]byte(c.host.APIVipConnectivity), &response); err != nil {
		return ValidationFailure
	}
	return boolValue(response.IsSuccess)
}

func (v *validator) printAPIVipConnected(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "No API VIP needed: User Managed Networking"
		}
		return "API VIP connectivity success"
	case ValidationFailure:
		return "API VIP connectivity failure"
	case ValidationPending:
		return "Missing inventory"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func getNumEnabledHosts(hosts []*models.Host) int {
	ret := 0
	for _, h := range hosts {
		if swag.StringValue(h.Status) != models.HostStatusDisabled {
			ret++
		}
	}
	return ret
}

func (v *validator) belongsToMajorityGroup(c *validationContext) ValidationStatus {
	if hostutil.IsDay2Host(c.host) || swag.BoolValue(c.cluster.UserManagedNetworking) {
		return ValidationSuccess
	}
	if c.cluster.MachineNetworkCidr == "" || c.cluster.ConnectivityMajorityGroups == "" {
		return ValidationPending
	}
	var majorityGroups map[string][]strfmt.UUID
	err := json.Unmarshal([]byte(c.cluster.ConnectivityMajorityGroups), &majorityGroups)
	if err != nil {
		v.log.WithError(err).Warn("Parse majority group")
		return ValidationError
	}
	if funk.Contains(majorityGroups[c.cluster.MachineNetworkCidr], *c.host.ID) {
		return ValidationSuccess
	} else if getNumEnabledHosts(c.cluster.Hosts) < 3 {
		// The minimum non disabled hosts for connectivity check is 3
		return ValidationPending
	}
	return ValidationFailure
}

func (v *validator) printBelongsToMajorityGroup(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		if hostutil.IsDay2Host(c.host) {
			return "Day2 host is not required to be connected to other hosts in the cluster"
		}
		if swag.BoolValue(c.cluster.UserManagedNetworking) {
			return "L2 connectivy validation skipped: User Managed Networking"
		}
		return "Host has connectivity to the majority of hosts in the cluster"
	case ValidationFailure:
		return "No connectivity to the majority of hosts in the cluster"
	case ValidationError:
		return "Parse error for connectivity majority group"
	case ValidationPending:
		if c.cluster.MachineNetworkCidr == "" || c.cluster.ConnectivityMajorityGroups == "" {
			return "Machine Network CIDR or Connectivity Majority Groups missing"
		} else if getNumEnabledHosts(c.cluster.Hosts) < 3 {
			return "Not enough enabled hosts in cluster to calculate connectivity groups"
		}
		// Shouldn't happen
		return "Not enough information to calculate host majority groups"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) isNTPSynced(c *validationContext) ValidationStatus {
	var sources []*models.NtpSource

	if c.host.NtpSources == "" {
		return ValidationFailure
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

	return ValidationFailure
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

func (v *validator) areImagesAvailable(c *validationContext) ValidationStatus {
	var imageStatuses map[string]*models.ContainerImageAvailability

	if c.host.ImagesStatus == "" {
		return ValidationPending
	}

	if err := json.Unmarshal([]byte(c.host.ImagesStatus), &imageStatuses); err != nil {
		v.log.WithError(err).Warn("Parse container image statuses")
		return ValidationError
	}

	if len(imageStatuses) == 0 {
		return ValidationPending
	}

	for _, imageStatus := range imageStatuses {
		if isInvalidImageStatus(imageStatus) {
			return ValidationFailure
		}
	}

	return ValidationSuccess
}

func (v *validator) printImageAvailability(c *validationContext, status ValidationStatus) string {
	switch status {
	case ValidationSuccess:
		return "All required container images were pulled and are available"
	case ValidationFailure:
		images, err := v.getFailedImagesNames(c.host)
		if err == nil {
			return fmt.Sprintf("Failed to fetch container images needed for installation from %s", strings.Join(images, ","))
		}
		fallthrough
	case ValidationError:
		return "Parse error for container image statuses"
	case ValidationPending:
		return "Missing container images statuses"
	default:
		return fmt.Sprintf("Unexpected status %s", status)
	}
}

func (v *validator) getFailedImagesNames(host *models.Host) ([]string, error) {
	var imageStatuses map[string]*models.ContainerImageAvailability

	if host.ImagesStatus == "" {
		return []string{}, nil
	}

	if err := json.Unmarshal([]byte(host.ImagesStatus), &imageStatuses); err != nil {
		return []string{}, err
	}

	if len(imageStatuses) == 0 {
		return []string{}, nil
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
		return "Insufficient disk speed or error occurred during disk speed measurement"
	case ValidationError:
		return "Error occured while getting boot device"
	default:
		return fmt.Sprintf("Unexpected status %s", status.String())
	}
}
