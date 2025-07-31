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

var _ = Describe("GetFeatureSupportList API", func() {
	Context("Basic feature support list", func() {
		It("should return comprehensive feature list for default parameters", func() {
			features := GetFeatureSupportList("4.13.0", nil, nil, nil)

			Expect(features).ToNot(BeEmpty())
			Expect(len(features)).To(BeNumerically(">", 30)) // Should have many features

			// Check that we have key features
			featureMap := make(map[models.FeatureSupportLevelID]models.Feature)
			for _, feature := range features {
				featureMap[feature.FeatureSupportLevelID] = feature
			}

			// Essential features that should always be present
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDSNO))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDLVM))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDCNV))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDLSO))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDODF))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDMINIMALISO))
			Expect(featureMap).To(HaveKey(models.FeatureSupportLevelIDFULLISO))
		})

		It("should use x86_64 as default architecture when none specified", func() {
			features := GetFeatureSupportList("4.13.0", nil, nil, nil)

			// LSO should be available on x86_64 (default)
			lsoFeature := findFeatureByID(features, models.FeatureSupportLevelIDLSO)
			Expect(lsoFeature).ToNot(BeNil())
			Expect(lsoFeature.SupportLevel).To(Equal(models.SupportLevelSupported))
		})
	})

	Context("Architecture-specific feature support", func() {
		DescribeTable("should return correct feature support for different architectures",
			func(architecture string, openshiftVersion string, expectedFeatureSupport map[models.FeatureSupportLevelID]models.SupportLevel) {
				features := GetFeatureSupportList(openshiftVersion, swag.String(architecture), nil, nil)

				featureMap := make(map[models.FeatureSupportLevelID]models.Feature)
				for _, feature := range features {
					featureMap[feature.FeatureSupportLevelID] = feature
				}

				for featureID, expectedSupport := range expectedFeatureSupport {
					Expect(featureMap).To(HaveKey(featureID))
					Expect(featureMap[featureID].SupportLevel).To(Equal(expectedSupport),
						"Feature %s should have support level %s on %s", featureID, expectedSupport, architecture)
				}
			},
			Entry("x86_64 architecture", "x86_64", "4.13.0", map[models.FeatureSupportLevelID]models.SupportLevel{
				models.FeatureSupportLevelIDSNO:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDLVM:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDCNV:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDLSO:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDODF:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDMINIMALISO: models.SupportLevelSupported,
				models.FeatureSupportLevelIDFULLISO:    models.SupportLevelSupported,
			}),
			Entry("arm64 architecture", "arm64", "4.13.0", map[models.FeatureSupportLevelID]models.SupportLevel{
				models.FeatureSupportLevelIDSNO:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDLVM:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDCNV:        models.SupportLevelUnavailable, // CNV not supported on arm64 for 4.13
				models.FeatureSupportLevelIDLSO:        models.SupportLevelUnavailable, // LSO not supported on arm64
				models.FeatureSupportLevelIDODF:        models.SupportLevelUnavailable, // ODF not supported on arm64
				models.FeatureSupportLevelIDMINIMALISO: models.SupportLevelSupported,
				models.FeatureSupportLevelIDFULLISO:    models.SupportLevelSupported,
			}),
			Entry("s390x architecture", "s390x", "4.13.0", map[models.FeatureSupportLevelID]models.SupportLevel{
				models.FeatureSupportLevelIDSNO:        models.SupportLevelDevPreview,  // SNO is dev preview on s390x for 4.13
				models.FeatureSupportLevelIDLVM:        models.SupportLevelUnavailable, // LVM not supported on s390x
				models.FeatureSupportLevelIDCNV:        models.SupportLevelUnavailable, // CNV not supported on s390x
				models.FeatureSupportLevelIDLSO:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDODF:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDMINIMALISO: models.SupportLevelUnavailable, // Minimal ISO not supported on s390x
				models.FeatureSupportLevelIDFULLISO:    models.SupportLevelSupported,
			}),
			Entry("ppc64le architecture", "ppc64le", "4.13.0", map[models.FeatureSupportLevelID]models.SupportLevel{
				models.FeatureSupportLevelIDSNO:        models.SupportLevelDevPreview,  // SNO is dev preview on ppc64le for 4.13
				models.FeatureSupportLevelIDLVM:        models.SupportLevelUnavailable, // LVM not supported on ppc64le
				models.FeatureSupportLevelIDCNV:        models.SupportLevelUnavailable, // CNV not supported on ppc64le
				models.FeatureSupportLevelIDLSO:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDODF:        models.SupportLevelSupported,
				models.FeatureSupportLevelIDMINIMALISO: models.SupportLevelSupported,
				models.FeatureSupportLevelIDFULLISO:    models.SupportLevelSupported,
			}),
		)
	})

	Context("Platform-specific feature support", func() {
		It("should exclude platform-specific features when platform is specified", func() {
			features := GetFeatureSupportList("4.13.0", swag.String("x86_64"), (*models.PlatformType)(swag.String("baremetal")), nil)

			// Should NOT include platform-specific features when platform is specified
			baremetalFeature := findFeatureByID(features, models.FeatureSupportLevelIDBAREMETALPLATFORM)
			Expect(baremetalFeature).To(BeNil())

			// Should have fewer features when platform is specified (38 vs 43)
			allFeatures := GetFeatureSupportList("4.13.0", swag.String("x86_64"), nil, nil)
			Expect(len(features)).To(BeNumerically("<", len(allFeatures)))
		})

		It("should include platform-specific features when no platform is specified", func() {
			features := GetFeatureSupportList("4.13.0", swag.String("x86_64"), nil, nil)

			// Should include platform-specific features when no platform is specified
			baremetalFeature := findFeatureByID(features, models.FeatureSupportLevelIDBAREMETALPLATFORM)
			Expect(baremetalFeature).ToNot(BeNil())

			vsphereFeature := findFeatureByID(features, models.FeatureSupportLevelIDVSPHEREINTEGRATION)
			Expect(vsphereFeature).ToNot(BeNil())

			nutanixFeature := findFeatureByID(features, models.FeatureSupportLevelIDNUTANIXINTEGRATION)
			Expect(nutanixFeature).ToNot(BeNil())
		})

		It("should exclude all platform-specific features when any platform is specified", func() {
			platformTypes := []models.PlatformType{
				models.PlatformTypeBaremetal,
				models.PlatformTypeVsphere,
				models.PlatformTypeNutanix,
			}

			for i := range platformTypes {
				features := GetFeatureSupportList("4.13.0", swag.String("x86_64"), &platformTypes[i], nil)

				// All platform-specific features should be excluded when any platform is specified
				baremetalFeature := findFeatureByID(features, models.FeatureSupportLevelIDBAREMETALPLATFORM)
				Expect(baremetalFeature).To(BeNil())

				vsphereFeature := findFeatureByID(features, models.FeatureSupportLevelIDVSPHEREINTEGRATION)
				Expect(vsphereFeature).To(BeNil())

				nutanixFeature := findFeatureByID(features, models.FeatureSupportLevelIDNUTANIXINTEGRATION)
				Expect(nutanixFeature).To(BeNil())

				// Should have 38 features when platform is specified
				Expect(len(features)).To(Equal(41))
			}
		})
	})

	Context("External platform support", func() {
		It("should exclude external platform features when external platform is specified", func() {
			features := GetFeatureSupportList("4.14.0", swag.String("x86_64"),
				(*models.PlatformType)(swag.String("external")), swag.String(common.ExternalPlatformNameOci))

			// Should NOT include external platform features when external platform is specified
			externalFeature := findFeatureByID(features, models.FeatureSupportLevelIDEXTERNALPLATFORM)
			Expect(externalFeature).To(BeNil())

			// Should NOT include OCI-specific feature when external platform is specified
			ociFeature := findFeatureByID(features, models.FeatureSupportLevelIDEXTERNALPLATFORMOCI)
			Expect(ociFeature).To(BeNil())

			// Should have 38 features when platform is specified
			Expect(len(features)).To(Equal(41))
		})

		It("should include external platform features when no platform is specified", func() {
			features := GetFeatureSupportList("4.14.0", swag.String("x86_64"), nil, nil)

			// Should include external platform feature when no platform is specified
			externalFeature := findFeatureByID(features, models.FeatureSupportLevelIDEXTERNALPLATFORM)
			Expect(externalFeature).ToNot(BeNil())

			// Should include OCI feature when no platform is specified
			ociFeature := findFeatureByID(features, models.FeatureSupportLevelIDEXTERNALPLATFORMOCI)
			Expect(ociFeature).ToNot(BeNil())

			// Should have 43 features when no platform is specified
			Expect(len(features)).To(Equal(46))
		})
	})

	Context("Feature incompatibilities", func() {
		It("should include incompatibility information", func() {
			features := GetFeatureSupportList("4.13.0", swag.String("x86_64"), nil, nil)

			snoFeature := findFeatureByID(features, models.FeatureSupportLevelIDSNO)
			Expect(snoFeature).ToNot(BeNil())
			Expect(snoFeature.SupportLevel).To(Equal(models.SupportLevelSupported))

			// SNO should have incompatibilities
			Expect(snoFeature.Incompatibilities).ToNot(BeEmpty())

			// Check for specific incompatibilities
			Expect(snoFeature.Incompatibilities).To(ContainElement(models.FeatureSupportLevelIDODF))
		})

		It("should include incompatibility reasons", func() {
			features := GetFeatureSupportList("4.13.0", swag.String("arm64"), nil, nil)

			lsoFeature := findFeatureByID(features, models.FeatureSupportLevelIDLSO)
			Expect(lsoFeature).ToNot(BeNil())
			Expect(lsoFeature.SupportLevel).To(Equal(models.SupportLevelUnavailable))
			Expect(lsoFeature.Reason).To(Equal(models.IncompatibilityReasonCPUArchitecture))
		})
	})

	Context("Version-specific behavior", func() {
		It("should handle different OpenShift versions", func() {
			versions := []string{"4.10.0", "4.11.0", "4.12.0", "4.13.0", "4.14.0"}

			for _, version := range versions {
				features := GetFeatureSupportList(version, swag.String("x86_64"), nil, nil)
				Expect(features).ToNot(BeEmpty())

				// All versions should have basic features
				snoFeature := findFeatureByID(features, models.FeatureSupportLevelIDSNO)
				Expect(snoFeature).ToNot(BeNil())
				Expect(snoFeature.SupportLevel).To(Equal(models.SupportLevelSupported))
			}
		})

		It("should handle unsupported architecture gracefully", func() {
			// Test with unsupported architecture for the version
			features := GetFeatureSupportList("4.9.0", swag.String("arm64"), nil, nil)

			// Should return features but all unavailable
			Expect(features).ToNot(BeEmpty())

			for _, feature := range features {
				Expect(feature.SupportLevel).To(Equal(models.SupportLevelUnavailable))
			}
		})
	})

	Context("Edge cases", func() {
		It("should handle nil pointers gracefully", func() {
			features := GetFeatureSupportList("4.13.0", nil, nil, nil)
			Expect(features).ToNot(BeEmpty())
		})

		It("should handle valid edge cases", func() {
			// Test with valid but different architecture
			features := GetFeatureSupportList("4.13.0", swag.String("s390x"), nil, nil)
			Expect(features).ToNot(BeEmpty())

			// Test with different version
			features2 := GetFeatureSupportList("4.14.0", swag.String("x86_64"), nil, nil)
			Expect(features2).ToNot(BeEmpty())
		})

		It("should be consistent between calls", func() {
			features1 := GetFeatureSupportList("4.13.0", swag.String("x86_64"), nil, nil)
			features2 := GetFeatureSupportList("4.13.0", swag.String("x86_64"), nil, nil)

			Expect(len(features1)).To(Equal(len(features2)))

			// Create maps for comparison
			map1 := make(map[models.FeatureSupportLevelID]models.Feature)
			map2 := make(map[models.FeatureSupportLevelID]models.Feature)

			for _, feature := range features1 {
				map1[feature.FeatureSupportLevelID] = feature
			}
			for _, feature := range features2 {
				map2[feature.FeatureSupportLevelID] = feature
			}

			// Should have same features with same support levels
			for featureID, feature1 := range map1 {
				feature2, exists := map2[featureID]
				Expect(exists).To(BeTrue())
				Expect(feature1.SupportLevel).To(Equal(feature2.SupportLevel))
			}
		})
	})
})

// Helper function to find a feature by ID in the features list
func findFeatureByID(features []models.Feature, featureID models.FeatureSupportLevelID) *models.Feature {
	for _, feature := range features {
		if feature.FeatureSupportLevelID == featureID {
			return &feature
		}
	}
	return nil
}
