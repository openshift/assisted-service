package diskencryption

import (
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

func TestDiskEncryption(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Disk encryption tests")
}

var _ = Describe("RequestsConfiguration", func() {
	It("returns false for nil or disabled configuration", func() {
		Expect(RequestsConfiguration(nil)).To(BeFalse())
		Expect(RequestsConfiguration(&models.DiskEncryption{})).To(BeFalse())
		Expect(RequestsConfiguration(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		})).To(BeFalse())
	})

	It("returns true when enable_on requests encryption", func() {
		Expect(RequestsConfiguration(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
		})).To(BeTrue())
	})

	It("returns true when tang is configured without enable_on", func() {
		Expect(RequestsConfiguration(&models.DiskEncryption{
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: `[{"url":"http://tang.example.com:7500","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
		})).To(BeTrue())
		Expect(RequestsConfiguration(&models.DiskEncryption{
			TangServers: `[{"url":"http://tang.example.com:7500","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
		})).To(BeTrue())
	})
})

var _ = Describe("IsConfigured", func() {
	It("returns false when disk encryption is not configured", func() {
		Expect(IsConfigured(nil)).To(BeFalse())
		Expect(IsConfigured(&models.DiskEncryption{})).To(BeFalse())
		Expect(IsConfigured(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
		})).To(BeFalse())
	})

	It("returns true when disk encryption is enabled", func() {
		Expect(IsConfigured(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
		})).To(BeTrue())
	})
})

var _ = Describe("IsEnabled", func() {
	It("returns false for nil, empty, and none", func() {
		Expect(IsEnabled(nil)).To(BeFalse())
		Expect(IsEnabled(swag.String(""))).To(BeFalse())
		Expect(IsEnabled(swag.String(models.DiskEncryptionEnableOnNone))).To(BeFalse())
	})

	It("returns true when encryption is enabled", func() {
		Expect(IsEnabled(swag.String(models.DiskEncryptionEnableOnMasters))).To(BeTrue())
	})
})

var _ = Describe("DiskEncryptionFieldDefaults", func() {
	It("defaults nil fields", func() {
		enableOn, mode := DiskEncryptionFieldDefaults(nil, nil)
		Expect(enableOn).To(Equal(models.DiskEncryptionEnableOnNone))
		Expect(mode).To(Equal(models.DiskEncryptionModeTpmv2))
	})

	It("defaults empty strings", func() {
		enableOn, mode := DiskEncryptionFieldDefaults(swag.String(""), swag.String(""))
		Expect(enableOn).To(Equal(models.DiskEncryptionEnableOnNone))
		Expect(mode).To(Equal(models.DiskEncryptionModeTpmv2))
	})

	It("preserves explicit values", func() {
		enableOn, mode := DiskEncryptionFieldDefaults(
			swag.String(models.DiskEncryptionEnableOnMasters),
			swag.String(models.DiskEncryptionModeTang),
		)
		Expect(enableOn).To(Equal(models.DiskEncryptionEnableOnMasters))
		Expect(mode).To(Equal(models.DiskEncryptionModeTang))
	})
})

var _ = Describe("ApplyDiskEncryptionDefaults", func() {
	It("handles nil input", func() {
		Expect(func() { ApplyDiskEncryptionDefaults(nil) }).NotTo(Panic())
	})

	It("defaults nil fields", func() {
		diskEncryption := &models.DiskEncryption{}
		ApplyDiskEncryptionDefaults(diskEncryption)
		Expect(diskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnNone)))
		Expect(diskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTpmv2)))
	})

	It("defaults empty string fields", func() {
		diskEncryption := &models.DiskEncryption{
			EnableOn: swag.String(""),
			Mode:     swag.String(""),
		}
		ApplyDiskEncryptionDefaults(diskEncryption)
		Expect(diskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnNone)))
		Expect(diskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTpmv2)))
	})

	It("preserves explicit values", func() {
		diskEncryption := &models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:     swag.String(models.DiskEncryptionModeTang),
		}
		ApplyDiskEncryptionDefaults(diskEncryption)
		Expect(diskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnMasters)))
		Expect(diskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTang)))
	})
})

var _ = Describe("IsSetWithTpm", func() {
	It("returns false when TPM encryption is not configured", func() {
		Expect(IsSetWithTpm(nil)).To(BeFalse())
		Expect(IsSetWithTpm(&models.DiskEncryption{
			EnableOn: swag.String(""),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		})).To(BeFalse())
		Expect(IsSetWithTpm(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		})).To(BeFalse())
		Expect(IsSetWithTpm(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:     swag.String(models.DiskEncryptionModeTang),
		})).To(BeFalse())
	})

	It("returns true when TPM encryption is configured", func() {
		Expect(IsSetWithTpm(&models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		})).To(BeTrue())
	})
})

var _ = DescribeTable("EnabledForRole",
	func(enabledOn string, role models.HostRole, expectedResult bool) {
		diskEncryption := models.DiskEncryption{EnableOn: swag.String(enabledOn)}
		Expect(EnabledForRole(diskEncryption, role)).To(Equal(expectedResult))
	},
	Entry("enabledOn all, role master", models.DiskEncryptionEnableOnAll, models.HostRoleMaster, true),
	Entry("enabledOn all, role bootstrap", models.DiskEncryptionEnableOnAll, models.HostRoleBootstrap, true),
	Entry("enabledOn all, role arbiter", models.DiskEncryptionEnableOnAll, models.HostRoleArbiter, true),
	Entry("enabledOn all, role worker", models.DiskEncryptionEnableOnAll, models.HostRoleWorker, true),
	Entry("enabledOn masters,arbiters,workers, role master", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleMaster, true),
	Entry("enabledOn masters,arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,arbiters,workers, role arbiter", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleArbiter, true),
	Entry("enabledOn masters,arbiters,workers, role worker", models.DiskEncryptionEnableOnMastersArbitersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn masters,arbiters, role master", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleMaster, true),
	Entry("enabledOn masters,arbiters, role bootstrap", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,arbiters, role arbiter", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleArbiter, true),
	Entry("enabledOn masters,arbiters, role worker", models.DiskEncryptionEnableOnMastersArbiters, models.HostRoleWorker, false),
	Entry("enabledOn masters,workers, role master", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleMaster, true),
	Entry("enabledOn masters,workers, role bootstrap", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleBootstrap, true),
	Entry("enabledOn masters,workers, role arbiter", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleArbiter, false),
	Entry("enabledOn masters,workers, role worker", models.DiskEncryptionEnableOnMastersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn arbiters,workers, role master", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleMaster, false),
	Entry("enabledOn arbiters,workers, role bootstrap", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleBootstrap, false),
	Entry("enabledOn arbiters,workers, role arbiter", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleArbiter, true),
	Entry("enabledOn arbiters,workers, role worker", models.DiskEncryptionEnableOnArbitersWorkers, models.HostRoleWorker, true),
	Entry("enabledOn masters, role master", models.DiskEncryptionEnableOnMasters, models.HostRoleMaster, true),
	Entry("enabledOn masters, role bootstrap", models.DiskEncryptionEnableOnMasters, models.HostRoleBootstrap, true),
	Entry("enabledOn masters, role arbiter", models.DiskEncryptionEnableOnMasters, models.HostRoleArbiter, false),
	Entry("enabledOn masters, role worker", models.DiskEncryptionEnableOnMasters, models.HostRoleWorker, false),
	Entry("enabledOn arbiters, role master", models.DiskEncryptionEnableOnArbiters, models.HostRoleMaster, false),
	Entry("enabledOn arbiters, role bootstrap", models.DiskEncryptionEnableOnArbiters, models.HostRoleBootstrap, false),
	Entry("enabledOn arbiters, role arbiter", models.DiskEncryptionEnableOnArbiters, models.HostRoleArbiter, true),
	Entry("enabledOn arbiters, role worker", models.DiskEncryptionEnableOnArbiters, models.HostRoleWorker, false),
	Entry("enabledOn workers, role master", models.DiskEncryptionEnableOnWorkers, models.HostRoleMaster, false),
	Entry("enabledOn workers, role bootstrap", models.DiskEncryptionEnableOnWorkers, models.HostRoleBootstrap, false),
	Entry("enabledOn workers, role arbiter", models.DiskEncryptionEnableOnWorkers, models.HostRoleArbiter, false),
	Entry("enabledOn workers, role worker", models.DiskEncryptionEnableOnWorkers, models.HostRoleWorker, true),
	Entry("enabledOn none, role master", models.DiskEncryptionEnableOnNone, models.HostRoleMaster, false),
	Entry("enabledOn none, role bootstrap", models.DiskEncryptionEnableOnNone, models.HostRoleBootstrap, false),
	Entry("enabledOn none, role arbiter", models.DiskEncryptionEnableOnNone, models.HostRoleArbiter, false),
	Entry("enabledOn none, role worker", models.DiskEncryptionEnableOnNone, models.HostRoleWorker, false),
)
