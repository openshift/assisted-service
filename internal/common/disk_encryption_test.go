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
