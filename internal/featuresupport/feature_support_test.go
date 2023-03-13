package featuresupport

import (
	"fmt"
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("V2ListFeatureSupportLevels API", func() {
	availableVersions := []string{"4.6", "4.7", "4.8", "4.9", "4.10", "4.11", "4.12", "4.13"}
	availableCpuArch := []string{
		models.ClusterCPUArchitectureX8664,
		models.ClusterCPUArchitectureArm64,
		models.ClusterCreateParamsCPUArchitectureAarch64,
		models.ClusterCPUArchitectureS390x,
		models.ClusterCPUArchitecturePpc64le,
		models.ClusterCPUArchitectureMulti,
	}
	featuresWithNoRestrictions := []models.FeatureSupportLevelID{
		models.FeatureSupportLevelIDADDITIONALNTPSOURCE,
		models.FeatureSupportLevelIDREQUESTEDHOSTNAME,
		models.FeatureSupportLevelIDPROXY,
		models.FeatureSupportLevelIDDAY2HOSTS,
		models.FeatureSupportLevelIDDISKSELECTION,
		models.FeatureSupportLevelIDOVNNETWORKTYPE,
		models.FeatureSupportLevelIDSDNNETWORKTYPE,
		models.FeatureSupportLevelIDSCHEDULABLEMASTERS,
		models.FeatureSupportLevelIDAUTOASSIGNROLE,
		models.FeatureSupportLevelIDCUSTOMMANIFEST,
		models.FeatureSupportLevelIDDISKENCRYPTION,
		models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKINGWITHVMS,
		models.FeatureSupportLevelIDUSERMANAGEDNETWORKINGWITHMULTINODE,
	}

	Context("Test IsFeatureSupported return tru for all features", func() {
		for _, f := range featuresWithNoRestrictions {
			for _, v := range availableVersions {
				for _, a := range availableCpuArch {
					feature := f
					version := v
					arch := a

					It(fmt.Sprintf("IsFeatureSupported %s, %s, %s", version, feature, arch), func() {
						Expect(IsFeatureSupported(feature, version, swag.String(arch))).To(Equal(true))
					})
				}
			}
		}
	})

	It("Test ARM64 is not supported under 4.10", func() {
		feature := models.ArchitectureSupportLevelIDARM64ARCHITECTURE
		Expect(isArchitectureSupported(feature, "4.6")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.7")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.8")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.9")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.10")).To(Equal(true))
		Expect(isArchitectureSupported(feature, "4.11")).To(Equal(true))
		Expect(isArchitectureSupported(feature, "4.12")).To(Equal(true))
		Expect(isArchitectureSupported(feature, "4.13")).To(Equal(true))

		// Check for feature release
		Expect(isArchitectureSupported(feature, "4.30")).To(Equal(true))
	})

	It("Test s390x is not supported under 4.13", func() {
		feature := models.ArchitectureSupportLevelIDS390XARCHITECTURE
		Expect(isArchitectureSupported(feature, "4.6")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.7")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.8")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.9")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.10")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.11")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.12")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.13")).To(Equal(true))

		// Check for feature release
		Expect(isArchitectureSupported(feature, "4.30")).To(Equal(true))

	})

	It("Test PPC64LE is not supported under 4.13", func() {
		feature := models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE
		Expect(isArchitectureSupported(feature, "4.6")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.7")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.8")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.9")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.10")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.11")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.12")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.13")).To(Equal(true))

		// Check for feature release
		Expect(isArchitectureSupported(feature, "4.30")).To(Equal(true))
	})

	Context("Test LVM/Nutanix are not supported under 4.11", func() {
		features := []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLVM, models.FeatureSupportLevelIDNUTANIXINTEGRATION}
		for _, f := range features {
			feature := f
			It(fmt.Sprintf("%s test", feature), func() {
				arch := "DoesNotMatter"
				Expect(IsFeatureSupported(feature, "4.6", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureSupported(feature, "4.7", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureSupported(feature, "4.8", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureSupported(feature, "4.9", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureSupported(feature, "4.11", swag.String(arch))).To(Equal(false))

				featureSupportParams := SupportLevelFilters{OpenshiftVersion: "4.11", CPUArchitecture: swag.String(arch)}
				Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelDevPreview))
				featureSupportParams = SupportLevelFilters{OpenshiftVersion: "4.11.20", CPUArchitecture: swag.String(arch)}
				Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelDevPreview))

				Expect(IsFeatureSupported(feature, "4.12", swag.String(arch))).To(Equal(true))
				Expect(IsFeatureSupported(feature, "4.13", swag.String(arch))).To(Equal(true))

				// Check for feature release
				Expect(IsFeatureSupported(feature, "4.30", swag.String(arch))).To(Equal(true))
			})
		}
	})

	Context("GetCpuArchitectureSupportList", func() {
		It("GetCpuArchitectureSupportList for openshift version 4.6", func() {
			openshiftVersion := "4.6"
			supportedArchitectures := GetCpuArchitectureSupportList(openshiftVersion)
			Expect(len(supportedArchitectures)).To(Equal(5))

			for key, value := range supportedArchitectures {
				if key == string(models.ArchitectureSupportLevelIDX8664ARCHITECTURE) {
					Expect(value).To(Equal(models.SupportLevelSupported))
				} else {
					Expect(value).To(Equal(models.SupportLevelUnsupported))
				}
			}
		})

		It("GetCpuArchitectureSupportList for openshift version 4.13", func() {
			openshiftVersion := "4.13"
			supportedArchitectures := GetCpuArchitectureSupportList(openshiftVersion)
			Expect(len(supportedArchitectures)).To(Equal(5))
			for key, value := range supportedArchitectures {
				if key == string(models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE) {
					Expect(value).To(Equal(models.SupportLevelTechPreview))
				} else {
					Expect(value).To(Equal(models.SupportLevelSupported))
				}
			}

		})
	})

	Context("GetSupportList", func() {
		It("GetFeatureSupportList 4.12", func() {
			list := GetFeatureSupportList("4.12", nil)
			Expect(len(list)).To(Equal(21))
		})

		It("GetFeatureSupportList 4.13", func() {
			list := GetFeatureSupportList("4.13", nil)
			Expect(len(list)).To(Equal(21))
		})

		It("GetCpuArchitectureSupportList 4.12", func() {
			list := GetCpuArchitectureSupportList("4.12")
			Expect(len(list)).To(Equal(5))
		})

		It("GetCpuArchitectureSupportList 4.13", func() {
			list := GetCpuArchitectureSupportList("4.13")
			Expect(len(list)).To(Equal(5))
		})

		It("GetFeatureSupportList 4.12 with not supported architecture", func() {
			featuresList := GetFeatureSupportList("4.12", swag.String(models.ClusterCPUArchitecturePpc64le))

			for _, supportLevel := range featuresList {
				Expect(supportLevel).To(Equal(models.SupportLevelUnsupported))
			}
		})

		It("GetFeatureSupportList 4.13 with supported architecture", func() {
			featuresList := GetFeatureSupportList("4.13", swag.String(models.ClusterCPUArchitecturePpc64le))
			Expect(featuresList[string(models.FeatureSupportLevelIDADDITIONALNTPSOURCE)]).To(Equal(models.SupportLevelSupported))
		})
	})

})

func TestOperators(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Feature-Support-Level tests")
}
