package common

import (
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
)

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
