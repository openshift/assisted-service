package common

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/stretchr/testify/assert"
)

func TestDiskEncryptionFieldDefaults(t *testing.T) {
	enableOn, mode := DiskEncryptionFieldDefaults(nil, nil)
	assert.Equal(t, models.DiskEncryptionEnableOnNone, enableOn)
	assert.Equal(t, models.DiskEncryptionModeTpmv2, mode)

	enableOn, mode = DiskEncryptionFieldDefaults(swag.String(""), swag.String(""))
	assert.Equal(t, models.DiskEncryptionEnableOnNone, enableOn)
	assert.Equal(t, models.DiskEncryptionModeTpmv2, mode)

	enableOn, mode = DiskEncryptionFieldDefaults(swag.String(models.DiskEncryptionEnableOnMasters), swag.String(models.DiskEncryptionModeTang))
	assert.Equal(t, models.DiskEncryptionEnableOnMasters, enableOn)
	assert.Equal(t, models.DiskEncryptionModeTang, mode)
}

func TestApplyDiskEncryptionDefaults(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ApplyDiskEncryptionDefaults(nil)
		})
	})

	t.Run("nil fields", func(t *testing.T) {
		diskEncryption := &models.DiskEncryption{}
		ApplyDiskEncryptionDefaults(diskEncryption)
		assert.Equal(t, swag.String(models.DiskEncryptionEnableOnNone), diskEncryption.EnableOn)
		assert.Equal(t, swag.String(models.DiskEncryptionModeTpmv2), diskEncryption.Mode)
	})

	t.Run("empty string fields", func(t *testing.T) {
		diskEncryption := &models.DiskEncryption{
			EnableOn: swag.String(""),
			Mode:     swag.String(""),
		}
		ApplyDiskEncryptionDefaults(diskEncryption)
		assert.Equal(t, swag.String(models.DiskEncryptionEnableOnNone), diskEncryption.EnableOn)
		assert.Equal(t, swag.String(models.DiskEncryptionModeTpmv2), diskEncryption.Mode)
	})

	t.Run("explicit values", func(t *testing.T) {
		diskEncryption := &models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:     swag.String(models.DiskEncryptionModeTang),
		}
		ApplyDiskEncryptionDefaults(diskEncryption)
		assert.Equal(t, swag.String(models.DiskEncryptionEnableOnMasters), diskEncryption.EnableOn)
		assert.Equal(t, swag.String(models.DiskEncryptionModeTang), diskEncryption.Mode)
	})
}
