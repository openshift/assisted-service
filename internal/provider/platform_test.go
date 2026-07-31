package provider

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("GetActualCreateClusterPlatformParams", func() {
	controlPlaneCount := int64(3)

	Context("s390x architecture", func() {
		DescribeTable("platform selection",
			func(platform *models.Platform, userManagedNetworking *bool, expectedPlatformType models.PlatformType, expectedUMN bool, expectError bool) {
				resultPlatform, resultUMN, err := GetActualCreateClusterPlatformParams(platform, userManagedNetworking, &controlPlaneCount, models.ClusterCPUArchitectureS390x)
				if expectError {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(*resultPlatform.Type).To(Equal(expectedPlatformType))
				Expect(*resultUMN).To(Equal(expectedUMN))
			},
			Entry("nil platform defaults to none",
				nil, nil,
				models.PlatformTypeNone, true, false),
			Entry("none platform is accepted",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}, nil,
				models.PlatformTypeNone, true, false),
			Entry("external platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("my-platform"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external OCI platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String(common.ExternalPlatformNameOci),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external Nutanix-named platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("nutanix"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external platform with UMN explicitly false is rejected",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("my-platform"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, swag.Bool(false),
				models.PlatformType(""), false, true),
			Entry("baremetal platform is rejected",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}, nil,
				models.PlatformType(""), false, true),
			Entry("vsphere platform is rejected",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)}, nil,
				models.PlatformType(""), false, true),
			Entry("nutanix platform is rejected",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)}, nil,
				models.PlatformType(""), false, true),
			Entry("UMN explicitly true with nil platform defaults to none",
				nil, swag.Bool(true),
				models.PlatformTypeNone, true, false),
			Entry("UMN explicitly false is rejected",
				nil, swag.Bool(false),
				models.PlatformType(""), false, true),
			Entry("UMN explicitly false with none platform is rejected",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}, swag.Bool(false),
				models.PlatformType(""), false, true),
		)
	})

	Context("ppc64le architecture", func() {
		DescribeTable("platform selection",
			func(platform *models.Platform, userManagedNetworking *bool, expectedPlatformType models.PlatformType, expectedUMN bool, expectError bool) {
				resultPlatform, resultUMN, err := GetActualCreateClusterPlatformParams(platform, userManagedNetworking, &controlPlaneCount, models.ClusterCPUArchitecturePpc64le)
				if expectError {
					Expect(err).To(HaveOccurred())
					return
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(*resultPlatform.Type).To(Equal(expectedPlatformType))
				Expect(*resultUMN).To(Equal(expectedUMN))
			},
			Entry("nil platform defaults to none",
				nil, nil,
				models.PlatformTypeNone, true, false),
			Entry("none platform is accepted",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}, nil,
				models.PlatformTypeNone, true, false),
			Entry("external platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("my-platform"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external OCI platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String(common.ExternalPlatformNameOci),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external Nutanix-named platform is accepted and UMN is set to true",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("nutanix"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, nil,
				models.PlatformTypeExternal, true, false),
			Entry("external platform with UMN explicitly false is rejected",
				&models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           swag.String("my-platform"),
						CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
					},
				}, swag.Bool(false),
				models.PlatformType(""), false, true),
			Entry("baremetal platform is rejected",
				&models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}, nil,
				models.PlatformType(""), false, true),
			Entry("UMN explicitly false is rejected",
				nil, swag.Bool(false),
				models.PlatformType(""), false, true),
		)
	})

	Context("x86_64 architecture", func() {
		It("nil platform with HA count defaults to baremetal", func() {
			resultPlatform, resultUMN, err := GetActualCreateClusterPlatformParams(nil, nil, &controlPlaneCount, models.ClusterCPUArchitectureX8664)
			Expect(err).ToNot(HaveOccurred())
			Expect(*resultPlatform.Type).To(Equal(models.PlatformTypeBaremetal))
			Expect(*resultUMN).To(BeFalse())
		})

		It("nil platform with SNO count defaults to none", func() {
			sno := int64(1)
			resultPlatform, resultUMN, err := GetActualCreateClusterPlatformParams(nil, nil, &sno, models.ClusterCPUArchitectureX8664)
			Expect(err).ToNot(HaveOccurred())
			Expect(*resultPlatform.Type).To(Equal(models.PlatformTypeNone))
			Expect(*resultUMN).To(BeTrue())
		})

		It("external platform with HA count is kept as-is", func() {
			platform := &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           swag.String("my-platform"),
					CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
				},
			}
			resultPlatform, resultUMN, err := GetActualCreateClusterPlatformParams(platform, nil, &controlPlaneCount, models.ClusterCPUArchitectureX8664)
			Expect(err).ToNot(HaveOccurred())
			Expect(*resultPlatform.Type).To(Equal(models.PlatformTypeExternal))
			Expect(*resultUMN).To(BeTrue())
		})
	})
})
