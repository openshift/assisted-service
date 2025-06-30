// Generated-by: Cursor
package featuresupport

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("IsPlatformSupported API", func() {
	Context("Basic platform support", func() {
		DescribeTable("should return correct support status for basic platforms",
			func(platformType models.PlatformType, externalPlatformName *string, openshiftVersion string, cpuArchitecture string, expectedSupported bool) {
				supported, err := IsPlatformSupported(platformType, externalPlatformName, openshiftVersion, cpuArchitecture)

				Expect(err).ToNot(HaveOccurred())
				Expect(supported).To(Equal(expectedSupported))
			},
			// Baremetal platform - should be supported on all architectures and versions
			Entry("baremetal x86_64 4.13", models.PlatformTypeBaremetal, nil, "4.13.0", "x86_64", true),
			Entry("baremetal arm64 4.13", models.PlatformTypeBaremetal, nil, "4.13.0", "arm64", true),
			Entry("baremetal s390x 4.13", models.PlatformTypeBaremetal, nil, "4.13.0", "s390x", true),
			Entry("baremetal ppc64le 4.13", models.PlatformTypeBaremetal, nil, "4.13.0", "ppc64le", true),

			// None platform - should be supported on all architectures and versions
			Entry("none x86_64 4.13", models.PlatformTypeNone, nil, "4.13.0", "x86_64", true),
			Entry("none arm64 4.13", models.PlatformTypeNone, nil, "4.13.0", "arm64", true),
			Entry("none s390x 4.13", models.PlatformTypeNone, nil, "4.13.0", "s390x", true),
			Entry("none ppc64le 4.13", models.PlatformTypeNone, nil, "4.13.0", "ppc64le", true),

			// External platform - may have architecture-specific limitations
			Entry("external x86_64 4.13", models.PlatformTypeExternal, nil, "4.13.0", "x86_64", false),
			Entry("external arm64 4.13", models.PlatformTypeExternal, nil, "4.13.0", "arm64", false),
			Entry("external s390x 4.13", models.PlatformTypeExternal, nil, "4.13.0", "s390x", false),
			Entry("external ppc64le 4.13", models.PlatformTypeExternal, nil, "4.13.0", "ppc64le", false),

			// Nutanix platform - limited architecture support
			Entry("nutanix x86_64 4.13", models.PlatformTypeNutanix, nil, "4.13.0", "x86_64", true),
			Entry("nutanix arm64 4.13", models.PlatformTypeNutanix, nil, "4.13.0", "arm64", false),
			Entry("nutanix s390x 4.13", models.PlatformTypeNutanix, nil, "4.13.0", "s390x", false),
			Entry("nutanix ppc64le 4.13", models.PlatformTypeNutanix, nil, "4.13.0", "ppc64le", false),

			// vSphere platform - supported on more architectures than expected
			Entry("vsphere x86_64 4.13", models.PlatformTypeVsphere, nil, "4.13.0", "x86_64", true),
			Entry("vsphere arm64 4.13", models.PlatformTypeVsphere, nil, "4.13.0", "arm64", true),
			Entry("vsphere s390x 4.13", models.PlatformTypeVsphere, nil, "4.13.0", "s390x", false),
			Entry("vsphere ppc64le 4.13", models.PlatformTypeVsphere, nil, "4.13.0", "ppc64le", false),
		)
	})

	Context("External platform with platform names", func() {
		DescribeTable("should return correct support status for external platforms with platform names",
			func(platformType models.PlatformType, externalPlatformName *string, openshiftVersion string, cpuArchitecture string, expectedSupported bool, expectError bool) {
				supported, err := IsPlatformSupported(platformType, externalPlatformName, openshiftVersion, cpuArchitecture)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
					Expect(supported).To(Equal(expectedSupported))
				}
			},
			// OCI external platform - should be supported from 4.14 onwards
			Entry("external oci x86_64 4.14", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.14.0", "x86_64", true, false),
			Entry("external oci x86_64 4.15", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.15.0", "x86_64", true, false),
			Entry("external oci x86_64 4.13", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.13.0", "x86_64", false, false),
			Entry("external oci x86_64 4.12", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.12.0", "x86_64", false, false),

			// OCI external platform - architecture support
			Entry("external oci arm64 4.14", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.14.0", "arm64", true, false),
			Entry("external oci s390x 4.14", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.14.0", "s390x", false, false),
			Entry("external oci ppc64le 4.14", models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.14.0", "ppc64le", false, false),

			// Invalid external platform names
			Entry("external invalid-platform x86_64 4.14", models.PlatformTypeExternal, swag.String("invalid-platform"), "4.14.0", "x86_64", false, true),
			Entry("external empty-platform x86_64 4.14", models.PlatformTypeExternal, swag.String(""), "4.14.0", "x86_64", false, true),
		)
	})

	Context("Version-specific platform support", func() {
		It("should handle version boundaries for OCI platform", func() {
			// Test exact version boundaries for OCI support
			supported, err := IsPlatformSupported(models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.14.0", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())

			supported, err = IsPlatformSupported(models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.13.99", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeFalse())
		})

		It("should handle future versions", func() {
			// Test with future versions
			supported, err := IsPlatformSupported(models.PlatformTypeExternal, swag.String(common.ExternalPlatformNameOci), "4.30.0", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())

			supported, err = IsPlatformSupported(models.PlatformTypeBaremetal, nil, "4.30.0", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())
		})
	})

	Context("Invalid platform types", func() {
		It("should return error for invalid platform types", func() {
			// Test with invalid platform type
			supported, err := IsPlatformSupported("invalid-platform", nil, "4.13.0", "x86_64")
			Expect(err).To(HaveOccurred())
			Expect(supported).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("invalid platform type"))
		})
	})

	Context("Edge cases", func() {
		It("should handle external platform without platform name", func() {
			// External platform without platform name may not be supported on all versions
			supported, err := IsPlatformSupported(models.PlatformTypeExternal, nil, "4.13.0", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeFalse())
		})

		It("should handle non-external platform with platform name", func() {
			// Non-external platform with platform name should ignore the platform name
			supported, err := IsPlatformSupported(models.PlatformTypeBaremetal, swag.String("some-platform"), "4.13.0", "x86_64")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())
		})

		It("should handle various OpenShift versions", func() {
			// Test with various OpenShift version formats
			versions := []string{"4.13", "4.13.0", "4.13.1", "4.13.0-rc.1", "4.13.0-nightly"}
			for _, version := range versions {
				supported, err := IsPlatformSupported(models.PlatformTypeBaremetal, nil, version, "x86_64")
				Expect(err).ToNot(HaveOccurred())
				Expect(supported).To(BeTrue())
			}
		})
	})
})
