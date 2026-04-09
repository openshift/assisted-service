package agentbasedinstaller

import (
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = Describe("applyRootDeviceHints", func() {
	Context("when multiple disks match WWN hint and one is multipath", func() {
		It("should prefer the multipath disk over raw FC paths", func() {
			testLogger, _ := test.NewNullLogger()

			wwn := "0x1111111111111111"
			rdh := &bmh_v1alpha1.RootDeviceHints{
				WWN: wwn,
			}

			// Simulate FC multipath: two raw FC paths and one multipath device, all sharing the same WWN
			inventory := &models.Inventory{
				Disks: []*models.Disk{
					{
						ID:        "/dev/sda",
						Path:      "/dev/sda",
						DriveType: models.DriveTypeFC,
						Wwn:       wwn,
						InstallationEligibility: models.DiskInstallationEligibility{
							Eligible: true,
						},
					},
					{
						ID:        "/dev/sdb",
						Path:      "/dev/sdb",
						DriveType: models.DriveTypeFC,
						Wwn:       wwn,
						InstallationEligibility: models.DiskInstallationEligibility{
							Eligible: true,
						},
					},
					{
						ID:        "/dev/dm-0",
						Path:      "/dev/dm-0",
						DriveType: models.DriveTypeMultipath,
						Wwn:       wwn,
						InstallationEligibility: models.DiskInstallationEligibility{
							Eligible: true,
						},
					},
				},
			}

			host := &models.Host{
				InstallationDiskID: "",
			}

			updateParams := &models.HostUpdateParams{}

			applied := applyRootDeviceHints(testLogger, host, inventory, rdh, updateParams)

			Expect(applied).To(BeTrue())
			Expect(updateParams.DisksSelectedConfig).To(HaveLen(1))
			Expect(*updateParams.DisksSelectedConfig[0].ID).To(Equal("/dev/dm-0"), "should prefer multipath device over raw FC paths")
		})

		It("should select the first disk when no multipath device exists", func() {
			testLogger, _ := test.NewNullLogger()

			wwn := "0x1111111111111111"
			rdh := &bmh_v1alpha1.RootDeviceHints{
				WWN: wwn,
			}

			// Two FC paths, no multipath device
			inventory := &models.Inventory{
				Disks: []*models.Disk{
					{
						ID:        "/dev/sda",
						Path:      "/dev/sda",
						DriveType: models.DriveTypeFC,
						Wwn:       wwn,
						InstallationEligibility: models.DiskInstallationEligibility{
							Eligible: true,
						},
					},
					{
						ID:        "/dev/sdb",
						Path:      "/dev/sdb",
						DriveType: models.DriveTypeFC,
						Wwn:       wwn,
						InstallationEligibility: models.DiskInstallationEligibility{
							Eligible: true,
						},
					},
				},
			}

			host := &models.Host{
				InstallationDiskID: "",
			}

			updateParams := &models.HostUpdateParams{}

			applied := applyRootDeviceHints(testLogger, host, inventory, rdh, updateParams)

			Expect(applied).To(BeTrue())
			Expect(updateParams.DisksSelectedConfig).To(HaveLen(1))
			Expect(*updateParams.DisksSelectedConfig[0].ID).To(Equal("/dev/sda"), "should select first disk when no multipath exists")
		})
	})
})
