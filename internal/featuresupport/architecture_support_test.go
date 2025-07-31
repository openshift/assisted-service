// Generated-by: Cursor
package featuresupport

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("IsArchitectureSupported", func() {
	Context("Valid architectures", func() {
		DescribeTable("should return correct support status",
			func(architecture string, openshiftVersion string, expectedSupported bool) {
				supported, err := IsArchitectureSupported(architecture, openshiftVersion)

				Expect(err).ToNot(HaveOccurred())
				Expect(supported).To(Equal(expectedSupported))

			},
			// x86_64 should be supported on all versions
			Entry("x86_64 on 4.9", models.ClusterCPUArchitectureX8664, "4.9.0", true),
			Entry("x86_64 on 4.10", models.ClusterCPUArchitectureX8664, "4.10.0", true),
			Entry("x86_64 on 4.11", models.ClusterCPUArchitectureX8664, "4.11.0", true),
			Entry("x86_64 on 4.12", models.ClusterCPUArchitectureX8664, "4.12.0", true),
			Entry("x86_64 on 4.13", models.ClusterCPUArchitectureX8664, "4.13.0", true),
			Entry("x86_64 on 4.14", models.ClusterCPUArchitectureX8664, "4.14.0", true),
			Entry("x86_64 on 4.15", models.ClusterCPUArchitectureX8664, "4.15.0", true),

			// ARM64 should be supported from 4.10 onwards
			Entry("arm64 on 4.6", models.ClusterCPUArchitectureArm64, "4.6.0", false),
			Entry("arm64 on 4.7", models.ClusterCPUArchitectureArm64, "4.7.0", false),
			Entry("arm64 on 4.8", models.ClusterCPUArchitectureArm64, "4.8.0", false),
			Entry("arm64 on 4.9", models.ClusterCPUArchitectureArm64, "4.9.0", false),
			Entry("arm64 on 4.10", models.ClusterCPUArchitectureArm64, "4.10.0", true),
			Entry("arm64 on 4.11", models.ClusterCPUArchitectureArm64, "4.11.0", true),
			Entry("arm64 on 4.12", models.ClusterCPUArchitectureArm64, "4.12.0", true),
			Entry("arm64 on 4.13", models.ClusterCPUArchitectureArm64, "4.13.0", true),
			Entry("arm64 on 4.14", models.ClusterCPUArchitectureArm64, "4.14.0", true),

			// S390x should be supported from 4.12 onwards (TechPreview on 4.12, Supported from 4.13)
			Entry("s390x on 4.9", models.ClusterCPUArchitectureS390x, "4.9.0", false),
			Entry("s390x on 4.10", models.ClusterCPUArchitectureS390x, "4.10.0", false),
			Entry("s390x on 4.11", models.ClusterCPUArchitectureS390x, "4.11.0", false),
			Entry("s390x on 4.12", models.ClusterCPUArchitectureS390x, "4.12.0", true),
			Entry("s390x on 4.13", models.ClusterCPUArchitectureS390x, "4.13.0", true),
			Entry("s390x on 4.14", models.ClusterCPUArchitectureS390x, "4.14.0", true),

			// PPC64LE should be supported from 4.12 onwards
			Entry("ppc64le on 4.9", models.ClusterCPUArchitecturePpc64le, "4.9.0", false),
			Entry("ppc64le on 4.10", models.ClusterCPUArchitecturePpc64le, "4.10.0", false),
			Entry("ppc64le on 4.11", models.ClusterCPUArchitecturePpc64le, "4.11.0", false),
			Entry("ppc64le on 4.12", models.ClusterCPUArchitecturePpc64le, "4.12.0", true),
			Entry("ppc64le on 4.13", models.ClusterCPUArchitecturePpc64le, "4.13.0", true),
			Entry("ppc64le on 4.14", models.ClusterCPUArchitecturePpc64le, "4.14.0", true),

			// Multi-arch should be supported
			Entry("multi on 4.13", models.ClusterCPUArchitectureMulti, "4.13.0", true),
			Entry("multi on 4.14", models.ClusterCPUArchitectureMulti, "4.14.0", true),
		)
	})

	Context("Invalid architectures", func() {
		DescribeTable("should return error for invalid architectures",
			func(architecture string, openshiftVersion string) {
				supported, err := IsArchitectureSupported(architecture, openshiftVersion)
				Expect(err).To(HaveOccurred())
				Expect(supported).To(BeFalse())
				Expect(err.Error()).To(ContainSubstring("invalid cpu architecture"))
			},
			Entry("invalid architecture", "invalid-arch", "4.13.0"),
			Entry("empty architecture", "", "4.13.0"),
			Entry("random string", "random", "4.13.0"),
		)
	})

	Context("Edge cases", func() {
		It("should handle version edge cases", func() {
			// Test with exact version boundaries
			supported, err := IsArchitectureSupported(models.ClusterCPUArchitectureArm64, "4.10.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())

			supported, err = IsArchitectureSupported(models.ClusterCPUArchitectureArm64, "4.9.99")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeFalse())
		})

		It("should handle future versions", func() {
			// Test with future versions
			supported, err := IsArchitectureSupported(models.ClusterCPUArchitectureX8664, "4.30.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())

			supported, err = IsArchitectureSupported(models.ClusterCPUArchitectureArm64, "4.30.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(supported).To(BeTrue())
		})
	})
})
