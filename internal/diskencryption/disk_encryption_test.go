package diskencryption

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/stretchr/testify/assert"
)

func TestIsEnabled(t *testing.T) {
	assert.False(t, IsEnabled(nil))
	assert.False(t, IsEnabled(swag.String("")))
	assert.False(t, IsEnabled(swag.String(models.DiskEncryptionEnableOnNone)))
	assert.True(t, IsEnabled(swag.String(models.DiskEncryptionEnableOnMasters)))
}

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

func TestIsSetWithTpm(t *testing.T) {
	assert.False(t, IsSetWithTpm(nil))
	assert.False(t, IsSetWithTpm(&models.DiskEncryption{
		EnableOn: swag.String(""),
		Mode:     swag.String(models.DiskEncryptionModeTpmv2),
	}))
	assert.False(t, IsSetWithTpm(&models.DiskEncryption{
		EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
		Mode:     swag.String(models.DiskEncryptionModeTpmv2),
	}))
	assert.False(t, IsSetWithTpm(&models.DiskEncryption{
		EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
		Mode:     swag.String(models.DiskEncryptionModeTang),
	}))
	assert.True(t, IsSetWithTpm(&models.DiskEncryption{
		EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
		Mode:     swag.String(models.DiskEncryptionModeTpmv2),
	}))
}

func TestEnabledForRole(t *testing.T) {
	testCases := []struct {
		name       string
		enabledOn  string
		role       models.HostRole
		isEnabled  bool
	}{
		{"enabledOn all, role master", models.DiskEncryptionEnableOnAll, models.HostRoleMaster, true},
		{"enabledOn all, role bootstrap", models.DiskEncryptionEnableOnAll, models.HostRoleBootstrap, true},
		{"enabledOn all, role arbiter", models.DiskEncryptionEnableOnAll, models.HostRoleArbiter, true},
		{"enabledOn all, role worker", models.DiskEncryptionEnableOnAll, models.HostRoleWorker, true},
		{"enabledOn masters,arbiters,workers, role master", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleMaster, true},
		{"enabledOn masters,arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleBootstrap, true},
		{"enabledOn masters,arbiters,workers, role arbiter", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleArbiter, true},
		{"enabledOn masters,arbiters,workers, role worker", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleWorker, true},
		{"enabledOn masters,arbiters, role master", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleMaster, true},
		{"enabledOn masters,arbiters, role bootstrap", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleBootstrap, true},
		{"enabledOn masters,arbiters, role arbiter", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleArbiter, true},
		{"enabledOn masters,arbiters, role worker", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleWorker, false},
		{"enabledOn masters,workers, role master", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleMaster, true},
		{"enabledOn masters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleBootstrap, true},
		{"enabledOn masters,workers, role arbiter", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleArbiter, false},
		{"enabledOn masters,workers, role worker", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleWorker, true},
		{"enabledOn arbiters,workers, role master", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleMaster, false},
		{"enabledOn arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleBootstrap, false},
		{"enabledOn arbiters,workers, role arbiter", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleArbiter, true},
		{"enabledOn arbiters,workers, role worker", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleWorker, true},
		{"enabledOn masters, role master", models.DiskEncryptionEnableOnMasters, models.HostRoleMaster, true},
		{"enabledOn masters, role bootstrap", models.DiskEncryptionEnableOnMasters, models.HostRoleBootstrap, true},
		{"enabledOn masters, role arbiter", models.DiskEncryptionEnableOnMasters, models.HostRoleArbiter, false},
		{"enabledOn masters, role worker", models.DiskEncryptionEnableOnMasters, models.HostRoleWorker, false},
		{"enabledOn arbiters, role master", models.DiskEncryptionEnableOnArbiters, models.HostRoleMaster, false},
		{"enabledOn arbiters, role bootstrap", models.DiskEncryptionEnableOnArbiters, models.HostRoleBootstrap, false},
		{"enabledOn arbiters, role arbiter", models.DiskEncryptionEnableOnArbiters, models.HostRoleArbiter, true},
		{"enabledOn arbiters, role worker", models.DiskEncryptionEnableOnArbiters, models.HostRoleWorker, false},
		{"enabledOn workers, role master", models.DiskEncryptionEnableOnWorkers, models.HostRoleMaster, false},
		{"enabledOn workers, role bootstrap", models.DiskEncryptionEnableOnWorkers, models.HostRoleBootstrap, false},
		{"enabledOn workers, role arbiter", models.DiskEncryptionEnableOnWorkers, models.HostRoleArbiter, false},
		{"enabledOn workers, role worker", models.DiskEncryptionEnableOnWorkers, models.HostRoleWorker, true},
		{"enabledOn none, role master", models.DiskEncryptionEnableOnNone, models.HostRoleMaster, false},
		{"enabledOn none, role bootstrap", models.DiskEncryptionEnableOnNone, models.HostRoleBootstrap, false},
		{"enabledOn none, role arbiter", models.DiskEncryptionEnableOnNone, models.HostRoleArbiter, false},
		{"enabledOn none, role worker", models.DiskEncryptionEnableOnNone, models.HostRoleWorker, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			diskEncryption := models.DiskEncryption{EnableOn: swag.String(tc.enabledOn)}
			assert.Equal(t, tc.isEnabled, EnabledForRole(diskEncryption, tc.role))
		})
	}
}
