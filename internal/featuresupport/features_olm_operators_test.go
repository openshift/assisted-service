package featuresupport

import (
	"fmt"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("V2ListFeatureSupportLevels API", func() {
	lVMavailableVersions := []string{"4.11", "4.12", "4.13", "4.14", "4.15", "4.16", "4.30"}
	unspportedLVMVersions := []string{"4.10", "4.9", "4.8", "4.7", "4.6"}

	Context("Test LVM/Nutanix are not supported under 4.11", func() {
		features := []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM, models.FeatureSupportLevelIDNUTANIXINTEGRATION}
		for _, f := range features {
			feature := f
			It(fmt.Sprintf("%s test", feature), func() {
				for _, version := range unspportedLVMVersions {
					Expect(IsFeatureAvailable(feature, version, nil, nil)).To(BeFalse(),
						fmt.Sprintf("feature %v, should be False on version %v", feature, version))
				}
				for _, version := range lVMavailableVersions {
					Expect(IsFeatureAvailable(feature, version, nil, nil)).To(BeTrue(),
						fmt.Sprintf("feature %v, should be True on version %v", feature, version))
				}
			})
		}
	})

	Context("Test LVM feature", func() {
		lvmFeatureList := featuresList[models.FeatureSupportLevelIDLVM]
		feature := models.FeatureSupportLevelIDLVM
		It("Validate LVM on CPU arch", func() {
			supportedCpuArch := []string{
				models.ClusterCPUArchitectureArm64,
				models.ClusterCPUArchitectureMulti,
				models.ClusterCPUArchitectureX8664,
			}
			notSupportedCpuArch := []string{
				models.ClusterCPUArchitectureS390x,
				models.ClusterCPUArchitecturePpc64le,
			}
			for _, arch := range supportedCpuArch {
				Expect(IsFeatureAvailable(feature, "4.11", swag.String(arch), nil)).To(BeTrue())
			}
			for _, arch := range notSupportedCpuArch {
				Expect(IsFeatureAvailable(feature, "4.11", swag.String(arch), nil)).To(BeFalse())
			}
		})
		It("Validate Feature Support for LVM", func() {

			tests := []struct {
				id       int // used to know which test case failed
				version  string
				platform *models.PlatformType
				expected models.SupportLevel
			}{
				{
					id:       1,
					version:  "4.11",
					platform: models.PlatformTypeNone.Pointer(),
					expected: models.SupportLevelDevPreview,
				},
				{
					id:       2,
					version:  "4.9",
					platform: models.PlatformTypeBaremetal.Pointer(),
					expected: models.SupportLevelUnavailable,
				},
				{
					id:       3,
					version:  "4.11",
					platform: models.PlatformTypeVsphere.Pointer(),
					expected: models.SupportLevelUnavailable,
				},
				{
					id:       4,
					version:  "4.12",
					platform: models.PlatformTypeBaremetal.Pointer(),
					expected: models.SupportLevelSupported,
				},
				{
					id:       5,
					version:  "4.14",
					platform: models.PlatformTypeNone.Pointer(),
					expected: models.SupportLevelSupported,
				},
				{
					id:       6,
					version:  "4.15",
					platform: models.PlatformTypeNone.Pointer(),
					expected: models.SupportLevelSupported,
				},
			}

			for _, test := range tests {

				featureSupportParams := SupportLevelFilters{
					OpenshiftVersion: test.version,
					CPUArchitecture:  nil,
					PlatformType:     test.platform,
					// HighAvailabilityMode: avilabilityMode,
				}
				resultSupportLevel := GetSupportLevel(feature, featureSupportParams)
				Expect(fmt.Sprintf("id: %d, result: %s", test.id, resultSupportLevel)).To(Equal(fmt.Sprintf("id: %d, result: %s", test.id, test.expected)))
			}
		})
		It("Validate Compatible Features", func() {
			incompatibleFeatures := make(map[string]*[]models.FeatureSupportLevelID)

			incompatibleFeatures["4.11"] = &[]models.FeatureSupportLevelID{
				models.FeatureSupportLevelIDNUTANIXINTEGRATION,
				models.FeatureSupportLevelIDVSPHEREINTEGRATION,
				models.FeatureSupportLevelIDODF,
				models.FeatureSupportLevelIDVIPAUTOALLOC,
				models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
			}

			incompatibleFeatures["4.12"] = &[]models.FeatureSupportLevelID{
				models.FeatureSupportLevelIDNUTANIXINTEGRATION,
				models.FeatureSupportLevelIDVSPHEREINTEGRATION,
				models.FeatureSupportLevelIDODF,
				models.FeatureSupportLevelIDVIPAUTOALLOC,
				models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
			}

			incompatibleFeatures["4.15"] = &[]models.FeatureSupportLevelID{
				models.FeatureSupportLevelIDNUTANIXINTEGRATION,
				models.FeatureSupportLevelIDVSPHEREINTEGRATION,
				models.FeatureSupportLevelIDODF,
			}
			incompatibleFeatures["4.16.0-rc0"] = &[]models.FeatureSupportLevelID{
				models.FeatureSupportLevelIDNUTANIXINTEGRATION,
				models.FeatureSupportLevelIDVSPHEREINTEGRATION,
				models.FeatureSupportLevelIDODF,
			}

			testIncompatibleFeatures := []struct {
				id          int
				version     string
				featureList []models.FeatureSupportLevelID
			}{
				{
					id:          1,
					version:     "4.11",
					featureList: []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM},
				},
				{
					id:          2,
					version:     "4.12",
					featureList: []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM},
				},
				{
					id:          3,
					version:     "4.15",
					featureList: []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM},
				},
				{
					id:          4,
					version:     "4.16.0-rc0", // check pre release version
					featureList: []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM},
				},
			}

			for _, test := range testIncompatibleFeatures {
				for _, featureId := range test.featureList {
					result := featuresList[featureId].getIncompatibleFeatures(test.version)
					Expect(fmt.Sprintf("id: %d, result: %s", test.id, result)).To(Equal(fmt.Sprintf("id: %d, result: %s", test.id, incompatibleFeatures[test.version])))
				}
			}
		})

		It("Ensure LVM  multinode is supportted on version 4.15", func() {
			features := []models.FeatureSupportLevelID{
				models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING,
				models.FeatureSupportLevelIDVIPAUTOALLOC,
				models.FeatureSupportLevelIDSNO,
			}
			for _, feature := range features {
				Expect(isFeatureCompatible("4.15", featuresList[feature], lvmFeatureList)).To(BeNil())
			}
		})
	})

	Context("Test feature support levels for Nutanix platform", func() {
		featureFilter := SupportLevelFilters{
			OpenshiftVersion: "4.14",
			CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		}

		It("CNV should be unavailable", func() {
			featureFilter.PlatformType = common.PlatformTypePtr(models.PlatformTypeNutanix)
			featureSupportLevels := GetFeatureSupportList(featureFilter)

			Expect(featureSupportLevels[string(models.FeatureSupportLevelIDCNV)]).To(Equal(models.SupportLevelUnavailable),
				fmt.Sprintf("CNV unavailable on Nutanix in %v", featureFilter.HighAvailabilityMode))

			featureSupportLevels = GetFeatureSupportList(featureFilter)

			Expect(featureSupportLevels[string(models.FeatureSupportLevelIDCNV)]).To(Equal(models.SupportLevelUnavailable),
				fmt.Sprintf("CNV unavailable on Nutanix in %v", featureFilter.HighAvailabilityMode))
		})

		It("MCE should be unavailable", func() {
			featureSupportLevels := GetFeatureSupportList(featureFilter)
			Expect(featureSupportLevels[string(models.FeatureSupportLevelIDMCE)]).To(Equal(models.SupportLevelUnavailable))
		})

		It("CNV should be unavailable", func() {
			featureFilter.PlatformType = common.PlatformTypePtr(models.PlatformTypeVsphere)
			featureSupportLevels := GetFeatureSupportList(featureFilter)

			Expect(featureSupportLevels[string(models.FeatureSupportLevelIDCNV)]).To(Equal(models.SupportLevelUnavailable))
		})
	})
})
