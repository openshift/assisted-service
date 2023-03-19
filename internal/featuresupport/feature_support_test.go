package featuresupport

import (
	"fmt"
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
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

	Context("Feature compatibility", func() {
		for _, f := range featuresList {
			for _, v := range availableVersions {
				for _, a := range availableCpuArch {
					feature := f
					version := v
					arch := a

					It(fmt.Sprintf("isFeatureCompatibleWithArchitecture %s, %s, %s", version, feature, arch), func() {
						filters := SupportLevelFilters{OpenshiftVersion: version, CPUArchitecture: swag.String(arch)}
						isFeatureCompatibleWithArchitecture(feature, filters.OpenshiftVersion, swag.StringValue(filters.CPUArchitecture))
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
				Expect(IsFeatureSupported(feature, "4.11", swag.String(arch))).To(Equal(true))

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
			Expect(len(list)).To(Equal(12))
		})

		It("GetFeatureSupportList 4.13", func() {
			list := GetFeatureSupportList("4.13", nil)
			Expect(len(list)).To(Equal(12))
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

		It("GetFeatureSupportList 4.13 with unsupported architecture", func() {
			featuresList := GetFeatureSupportList("4.13", swag.String(models.ClusterCPUArchitecturePpc64le))
			Expect(featuresList[string(models.FeatureSupportLevelIDSNO)]).To(Equal(models.SupportLevelUnsupported))
		})

		It("GetFeatureSupportList 4.13 with unsupported architecture", func() {
			featuresList := GetFeatureSupportList("4.13", swag.String(models.ClusterCPUArchitectureX8664))
			Expect(featuresList[string(models.FeatureSupportLevelIDSNO)]).To(Equal(models.SupportLevelSupported))
		})
	})

	Context("ValidateIncompatibleFeatures", func() {
		It("No feature is activated", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.6",
				CPUArchitecture:  models.ClusterCPUArchitectureX8664,
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil)).To(BeNil())
		})
		It("Single compatible feature is activated", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.8",
				CPUArchitecture:       models.ClusterCPUArchitectureX8664,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil)).To(BeNil())
		})
		It("SNO feature is activated with incompatible architecture ppc64le", func() {
			expectedError := "cannot use Single Node OpenShift because it's not compatible with the ppc64le architecture on version 4.13 of OpenShift"
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.13",
				CPUArchitecture:       models.ClusterCPUArchitecturePpc64le,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil).Error()).To(Equal(expectedError))
		})
		It("SNO feature is activated with incompatible architecture s390x", func() {
			expectedError := "cannot use Single Node OpenShift because it's not compatible with the s390x architecture on version 4.13 of OpenShift"
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.13",
				CPUArchitecture:       models.ClusterCPUArchitectureS390x,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil).Error()).To(Equal(expectedError))
		})
		It("Nutanix feature is activated with incompatible architecture", func() {
			expectedError := "cannot use arm64 architecture because it's not compatible on version 4.8 of OpenShift"
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.8",
				CPUArchitecture:       models.ClusterCPUArchitectureArm64,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil).Error()).To(Equal(expectedError))
		})
		It("ClusterManagedNetworking feature is activated with compatible architecture on 4.11", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.11",
				CPUArchitecture:       models.ClusterCPUArchitectureArm64,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
				UserManagedNetworking: swag.Bool(false),
			}}
			Expect(ValidateIncompatibleFeatures(cluster, nil)).To(BeNil())
		})
	})

	Context("Incompatibilities", func() {
		Context("IsFeatureActivated", func() {
			It("Activated features in cluster - Sno, VipAutoAlloc, UserManagedNetworking, NutanixIntegration", func() {
				operators := []*models.MonitoredOperator{
					{
						Name:             "cnv",
						Namespace:        "openshift-cnv",
						OperatorType:     models.OperatorTypeOlm,
						SubscriptionName: "hco-operatorhub",
						TimeoutSeconds:   60 * 60,
					},
				}

				cluster := common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion:      "4.8",
					CPUArchitecture:       models.ClusterCPUArchitecturePpc64le,
					HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
					UserManagedNetworking: swag.Bool(true),
					Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
					VipDhcpAllocation:     swag.Bool(true),
					MonitoredOperators:    operators,
				},
				}

				activatedFeatures := []SupportLevelFeature{
					&VipAutoAllocFeature{}, &SnoFeature{}, &UserManagedNetworkingFeature{}, &NutanixIntegrationFeature{}, &CnvFeature{},
				}

				for _, feature := range activatedFeatures {
					Expect(feature.getFeatureActiveLevel(cluster, nil)).To(Equal(activeLevelActive))
				}
			})

			It("Disable activated features in cluster - Sno, VipAutoAlloc, UserManagedNetworking, NutanixIntegration, Cnv", func() {
				operators := []*models.MonitoredOperator{
					{
						Name:             "cnv",
						Namespace:        "openshift-cnv",
						OperatorType:     models.OperatorTypeOlm,
						SubscriptionName: "hco-operatorhub",
						TimeoutSeconds:   60 * 60,
					},
				}

				cluster := common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion:      "4.8",
					CPUArchitecture:       models.ClusterCPUArchitecturePpc64le,
					HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
					UserManagedNetworking: swag.Bool(true),
					Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
					VipDhcpAllocation:     swag.Bool(true),
					MonitoredOperators:    operators,
				}}
				params := models.V2ClusterUpdateParams{
					VipDhcpAllocation:     swag.Bool(false),
					Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
					UserManagedNetworking: swag.Bool(false),
					OlmOperators: []*models.OperatorCreateParams{
						{Name: "lvm"},
					},
				}

				activatedFeatures := []SupportLevelFeature{
					&VipAutoAllocFeature{}, &UserManagedNetworkingFeature{}, &NutanixIntegrationFeature{}, &CnvFeature{},
				}

				for _, feature := range activatedFeatures {
					Expect(feature.getFeatureActiveLevel(cluster, nil)).To(Equal(activeLevelActive))
					Expect(feature.getFeatureActiveLevel(cluster, &params)).To(Equal(activeLevelNotActive))
				}
				Expect((&SnoFeature{}).getFeatureActiveLevel(cluster, &params)).To(Equal(activeLevelActive))
				Expect((&LvmFeature{}).getFeatureActiveLevel(cluster, &params)).To(Equal(activeLevelActive))
				Expect((&ClusterManagedNetworkingFeature{}).getFeatureActiveLevel(cluster, &params)).To(Equal(activeLevelActive))
			})
			It("Disable olm operator activated features in cluster", func() {
				operators := []*models.MonitoredOperator{
					{
						Name:             "cnv",
						Namespace:        "openshift-cnv",
						OperatorType:     models.OperatorTypeOlm,
						SubscriptionName: "hco-operatorhub",
						TimeoutSeconds:   60 * 60,
					},
				}

				cluster := common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion:   "4.8",
					CPUArchitecture:    models.ClusterCPUArchitecturePpc64le,
					MonitoredOperators: operators,
				}}
				params := models.V2ClusterUpdateParams{
					OlmOperators: []*models.OperatorCreateParams{},
				}

				Expect((&CnvFeature{}).getFeatureActiveLevel(cluster, nil)).To(Equal(activeLevelActive))
				Expect((&CnvFeature{}).getFeatureActiveLevel(cluster, &params)).To(Equal(activeLevelNotActive))
			})
		})

		Context("GetIncompatibleFeatures", func() {
			It("Features without any restrictions", func() {
				features := []models.FeatureSupportLevelID{
					models.FeatureSupportLevelIDVIPAUTOALLOC,
					models.FeatureSupportLevelIDCUSTOMMANIFEST,
					models.FeatureSupportLevelIDDUALSTACKVIPS,
					models.FeatureSupportLevelIDSINGLENODEEXPANSION,
					models.FeatureSupportLevelIDLVM,
					models.FeatureSupportLevelIDCNV,
				}
				for _, featureId := range features {
					Expect(featuresList[featureId].GetIncompatibleFeatures()).To(BeNil())
				}
			})

			It("Features with restrictions - Nutanix and UserManagedNetworking", func() {
				umnFeature := featuresList[models.FeatureSupportLevelIDUSERMANAGEDNETWORKING]
				nutanixFeature := featuresList[models.FeatureSupportLevelIDNUTANIXINTEGRATION]

				isUmnIncompatibleWithNutanix := isFeatureCompatible(umnFeature, nutanixFeature)
				isNutanixIncompatibleWithUmn := isFeatureCompatible(nutanixFeature, umnFeature)

				Expect((*isUmnIncompatibleWithNutanix).GetId()).To(Equal(nutanixFeature.GetId()))
				Expect((*isNutanixIncompatibleWithUmn).GetId()).To(Equal(umnFeature.GetId()))
			})

			It("Features with restrictions - Sno", func() {
				snoFeature := featuresList[models.FeatureSupportLevelIDSNO]
				nutanixFeature := featuresList[models.FeatureSupportLevelIDNUTANIXINTEGRATION]
				vsphereFeature := featuresList[models.FeatureSupportLevelIDVSPHEREINTEGRATION]
				cmnFeature := featuresList[models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING]

				isSnoIncompatibleWithNutanix := isFeatureCompatible(snoFeature, nutanixFeature)
				isSnoIncompatibleWithVsphere := isFeatureCompatible(snoFeature, vsphereFeature)
				isSnoIncompatibleWithCmn := isFeatureCompatible(snoFeature, cmnFeature)

				isNutanixIncompatibleWithSno := isFeatureCompatible(nutanixFeature, snoFeature)
				isVsphereIncompatibleWithSno := isFeatureCompatible(vsphereFeature, snoFeature)
				isCmnIncompatibleWithSno := isFeatureCompatible(cmnFeature, snoFeature)

				Expect((*isSnoIncompatibleWithNutanix).GetId()).To(Equal(nutanixFeature.GetId()))
				Expect((*isSnoIncompatibleWithVsphere).GetId()).To(Equal(vsphereFeature.GetId()))
				Expect((*isSnoIncompatibleWithCmn).GetId()).To(Equal(cmnFeature.GetId()))
				Expect((*isNutanixIncompatibleWithSno).GetId()).To(Equal(snoFeature.GetId()))
				Expect((*isVsphereIncompatibleWithSno).GetId()).To(Equal(snoFeature.GetId()))
				Expect((*isCmnIncompatibleWithSno).GetId()).To(Equal(snoFeature.GetId()))
			})
		})
	})
})

func TestOperators(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Feature-Support-Level tests")
}
