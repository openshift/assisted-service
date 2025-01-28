package host

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("isInstallationDiskSpeedCheckSuccessful", func() {
	var (
		validationCtx *validationContext
		v             *validator
	)

	BeforeEach(func() {
		validationCtx = &validationContext{
			host: &models.Host{},
		}
		v = &validator{
			log:         common.GetTestLog(),
			hwValidator: hardware.NewValidator(common.GetTestLog(), hardware.ValidatorCfg{}, nil, nil),
		}
	})

	It("returns false when the infraenv is set", func() {
		validationCtx.infraEnv = &common.InfraEnv{}
		Expect(v.isInstallationDiskSpeedCheckSuccessful(validationCtx)).To(BeFalse())
	})

	It("returns true when disk partitions are being saved", func() {
		validationCtx.host.InstallerArgs = "--save-partindex 1"
		Expect(v.isInstallationDiskSpeedCheckSuccessful(validationCtx)).To(BeTrue())
	})

	It("returns true when the boot disk is persistent", func() {
		validationCtx.inventory = &models.Inventory{Boot: &models.Boot{DeviceType: models.BootDeviceTypePersistent}}
		Expect(v.isInstallationDiskSpeedCheckSuccessful(validationCtx)).To(BeTrue())
	})

	It("fails when install disk path can't be found", func() {
		Expect(v.isInstallationDiskSpeedCheckSuccessful(validationCtx)).To(BeFalse())
	})

	It("succeeds when disk speed has been checked", func() {
		validationCtx.host.InstallationDiskPath = "/dev/sda"
		validationCtx.host.DisksInfo = "{\"/dev/sda\": {\"disk_speed\": {\"tested\": true, \"exit_code\": 0}, \"path\": \"/dev/sda\"}}"
		Expect(v.isInstallationDiskSpeedCheckSuccessful(validationCtx)).To(BeTrue())
	})
})
