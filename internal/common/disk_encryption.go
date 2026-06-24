package common

import (
	"strings"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

// IsEnabled reports whether disk encryption is enabled for any role.
// Empty or "none" enable_on values are treated as disabled.
func IsEnabled(enableOn *string) bool {
	v := swag.StringValue(enableOn)
	return v != "" && v != models.DiskEncryptionEnableOnNone
}

// IsConfigured reports whether disk encryption is enabled on the cluster.
func IsConfigured(diskEncryption *models.DiskEncryption) bool {
	return diskEncryption != nil && IsEnabled(diskEncryption.EnableOn)
}

// RequestsConfiguration reports whether an API payload carries explicit disk encryption
// settings beyond the disabled defaults, including tang configuration without enable_on.
func RequestsConfiguration(diskEncryption *models.DiskEncryption) bool {
	if diskEncryption == nil {
		return false
	}
	return RequestsDiskEncryptionConfiguration(
		diskEncryption.EnableOn,
		diskEncryption.Mode,
		diskEncryption.TangServers,
	)
}

// RequestsDiskEncryptionConfiguration reports whether disk encryption fields carry explicit
// configuration beyond disabled defaults. Use this when the caller has separate fields
// instead of a models.DiskEncryption payload (for example AgentClusterInstall spec).
func RequestsDiskEncryptionConfiguration(enableOn, mode *string, tangServers string) bool {
	return IsEnabled(enableOn) ||
		HasMode(&models.DiskEncryption{Mode: mode}, models.DiskEncryptionModeTang) ||
		tangServers != ""
}

// DiskEncryptionFieldDefaults returns enable_on and mode with defaults for nil or empty values.
func DiskEncryptionFieldDefaults(enableOn, mode *string) (string, string) {
	enableOnValue := swag.StringValue(enableOn)
	if enableOnValue == "" {
		enableOnValue = models.DiskEncryptionEnableOnNone
	}
	modeValue := swag.StringValue(mode)
	if modeValue == "" {
		modeValue = models.DiskEncryptionModeTpmv2
	}
	return enableOnValue, modeValue
}

// ApplyDiskEncryptionDefaults normalizes nil or empty disk encryption fields to their defaults.
func ApplyDiskEncryptionDefaults(diskEncryption *models.DiskEncryption) {
	if diskEncryption == nil {
		return
	}
	enableOn, mode := DiskEncryptionFieldDefaults(diskEncryption.EnableOn, diskEncryption.Mode)
	diskEncryption.EnableOn = swag.String(enableOn)
	diskEncryption.Mode = swag.String(mode)
}

// HasMode reports whether disk encryption mode equals the given value.
func HasMode(diskEncryption *models.DiskEncryption, mode string) bool {
	if diskEncryption == nil {
		return false
	}
	return swag.StringValue(diskEncryption.Mode) == mode
}

// IsSetWithTpm reports whether TPM-based disk encryption is configured for any role.
func IsSetWithTpm(diskEncryption *models.DiskEncryption) bool {
	return IsConfigured(diskEncryption) && HasMode(diskEncryption, models.DiskEncryptionModeTpmv2)
}

// IsSetWithTang reports whether Tang-based disk encryption is configured for any role.
func IsSetWithTang(diskEncryption *models.DiskEncryption) bool {
	return IsConfigured(diskEncryption) && HasMode(diskEncryption, models.DiskEncryptionModeTang)
}

// EnabledForRole reports whether disk encryption is enabled for the given host role.
func EnabledForRole(encryption models.DiskEncryption, role models.HostRole) bool {
	if swag.StringValue(encryption.EnableOn) == models.DiskEncryptionEnableOnAll {
		return true
	}

	enabledGroups := strings.Split(swag.StringValue(encryption.EnableOn), ",")
	if role == models.HostRoleMaster || role == models.HostRoleBootstrap {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnMasters)
	}
	if role == models.HostRoleArbiter {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnArbiters)
	}
	if role == models.HostRoleWorker {
		return funk.ContainsString(enabledGroups, models.DiskEncryptionEnableOnWorkers)
	}
	return false
}
