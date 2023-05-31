package featuresupport

import (
	"fmt"
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
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

	It("Test s390x is not supported under 4.12", func() {
		feature := models.ArchitectureSupportLevelIDS390XARCHITECTURE
		Expect(isArchitectureSupported(feature, "4.6")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.7")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.8")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.9")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.10")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.11")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.12")).To(Equal(true))
		Expect(isArchitectureSupported(feature, "4.13")).To(Equal(true))

		// Check for feature release
		Expect(isArchitectureSupported(feature, "4.30")).To(Equal(true))

	})

	It("Test PPC64LE is not supported under 4.12", func() {
		feature := models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE
		Expect(isArchitectureSupported(feature, "4.6")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.7")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.8")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.9")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.10")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.11")).To(Equal(false))
		Expect(isArchitectureSupported(feature, "4.12")).To(Equal(true))
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
				Expect(IsFeatureAvailable(feature, "4.6", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureAvailable(feature, "4.7", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureAvailable(feature, "4.8", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureAvailable(feature, "4.9", swag.String(arch))).To(Equal(false))
				Expect(IsFeatureAvailable(feature, "4.11", swag.String(arch))).To(Equal(true))

				featureSupportParams := SupportLevelFilters{OpenshiftVersion: "4.11", CPUArchitecture: swag.String(arch)}
				Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelDevPreview))
				featureSupportParams = SupportLevelFilters{OpenshiftVersion: "4.11.20", CPUArchitecture: swag.String(arch)}
				Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelDevPreview))

				Expect(IsFeatureAvailable(feature, "4.12", swag.String(arch))).To(Equal(true))
				Expect(IsFeatureAvailable(feature, "4.13", swag.String(arch))).To(Equal(true))

				// Check for feature release
				Expect(IsFeatureAvailable(feature, "4.30", swag.String(arch))).To(Equal(true))
			})
		}
	})

	Context("Test LSO CPU compatibility", func() {
		feature := models.FeatureSupportLevelIDLSO
		It("LSO IsFeatureAvailable", func() {
			Expect(IsFeatureAvailable(feature, "Does not matter", swag.String(models.ClusterCPUArchitecturePpc64le))).To(Equal(true))
			Expect(IsFeatureAvailable(feature, "Does not matter", swag.String(models.ClusterCPUArchitectureX8664))).To(Equal(true))
			Expect(IsFeatureAvailable(feature, "Does not matter", swag.String(models.ClusterCPUArchitectureS390x))).To(Equal(true))
			Expect(IsFeatureAvailable(feature, "Does not matter", swag.String(models.ClusterCPUArchitectureArm64))).To(Equal(false))
		})
		It("LSO GetSupportLevel on architecture", func() {
			featureSupportParams := SupportLevelFilters{OpenshiftVersion: "Any", CPUArchitecture: swag.String(models.ClusterCPUArchitectureX8664)}
			Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelSupported))

			featureSupportParams.CPUArchitecture = swag.String(models.ClusterCPUArchitectureS390x)
			Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelSupported))

			featureSupportParams.CPUArchitecture = swag.String(models.ClusterCPUArchitecturePpc64le)
			Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelSupported))

			featureSupportParams.CPUArchitecture = swag.String(models.ClusterCPUArchitectureArm64)
			Expect(GetSupportLevel(feature, featureSupportParams)).To(Equal(models.SupportLevelUnavailable))
		})

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
					Expect(value).To(Equal(models.SupportLevelUnavailable))
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
			Expect(len(list)).To(Equal(14))
		})

		It("GetFeatureSupportList 4.13", func() {
			list := GetFeatureSupportList("4.13", nil)
			Expect(len(list)).To(Equal(14))
		})

		It("GetCpuArchitectureSupportList 4.12", func() {
			list := GetCpuArchitectureSupportList("4.12")
			Expect(len(list)).To(Equal(5))
		})

		It("GetCpuArchitectureSupportList 4.13", func() {
			list := GetCpuArchitectureSupportList("4.13")
			Expect(len(list)).To(Equal(5))
		})

		It("GetFeatureSupportList 4.11 with not supported architecture", func() {
			featuresList := GetFeatureSupportList("4.11", swag.String(models.ClusterCPUArchitecturePpc64le))

			for _, supportLevel := range featuresList {
				Expect(supportLevel).To(Equal(models.SupportLevelUnavailable))
			}
		})

		It("GetFeatureSupportList 4.13 with unsupported architecture", func() {
			featuresList := GetFeatureSupportList("4.12", swag.String(models.ClusterCPUArchitecturePpc64le))
			Expect(featuresList[string(models.FeatureSupportLevelIDSNO)]).To(Equal(models.SupportLevelUnavailable))

			featuresList = GetFeatureSupportList("4.13", swag.String(models.ClusterCPUArchitecturePpc64le))
			Expect(featuresList[string(models.FeatureSupportLevelIDSNO)]).To(Equal(models.SupportLevelDevPreview))
		})

		It("GetFeatureSupportList 4.13 with unsupported architecture", func() {
			featuresList := GetFeatureSupportList("4.13", swag.String(models.ClusterCPUArchitectureX8664))
			Expect(featuresList[string(models.FeatureSupportLevelIDSNO)]).To(Equal(models.SupportLevelSupported))
		})
	})

	Context("ValidateIncompatibleFeatures", func() {
		log := logrus.New()

		It("No feature is activated", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.6",
				CPUArchitecture:  models.ClusterCPUArchitectureX8664,
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureX8664, &cluster, nil, nil)).To(BeNil())
		})

		It("No OCP version with CPU architecture that depends on OCP version", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				CPUArchitecture:       models.ClusterCPUArchitectureArm64,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureArm64, &cluster, nil, nil)).To(BeNil())
		})
		It("Single compatible feature is activated", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.8",
				CPUArchitecture:       models.ClusterCPUArchitectureX8664,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureX8664, &cluster, nil, nil)).To(BeNil())
		})
		It("Update s390x cluster", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.8",
				CPUArchitecture:       models.ClusterCPUArchitectureS390x,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			params := models.V2ClusterUpdateParams{UserManagedNetworking: swag.Bool(false)}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, nil, &params)).To(Not(BeNil()))
		})
		It("Update s390x cluster", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.13",
				CPUArchitecture:       models.ClusterCPUArchitectureS390x,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			infraEnv := models.InfraEnv{Type: common.ImageTypePtr(models.ImageTypeFullIso)}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, &infraEnv, nil)).To(BeNil())

			params := models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}
			err := ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, &infraEnv, &params)
			Expect(err).To(Not(BeNil()))
			Expect(err.Error()).To(ContainSubstring("cannot use Minimal ISO because it's not compatible with the s390x architecture on version 4.13 of OpenShift"))
		})
		It("SNO feature is activated with incompatible architecture ppc64le on 4.12", func() {
			expectedError := "cannot use Single Node OpenShift because it's not compatible with the ppc64le architecture on version 4.12 of OpenShift"
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.12",
				CPUArchitecture:       models.ClusterCPUArchitecturePpc64le,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitecturePpc64le, &cluster, nil, nil).Error()).To(Equal(expectedError))
		})
		It("SNO feature is compatible on ppc64le architecture at 4.13", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.13",
				CPUArchitecture:       models.ClusterCPUArchitecturePpc64le,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitecturePpc64le, &cluster, nil, nil)).To(BeNil())
		})
		It("SNO feature is activated with incompatible architecture s390x on 4.12", func() {
			expectedError := "cannot use Single Node OpenShift because it's not compatible with the s390x architecture on version 4.12 of OpenShift"
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.12",
				CPUArchitecture:       models.ClusterCPUArchitectureS390x,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, nil, nil).Error()).To(Equal(expectedError))
		})
		It("SNO feature is activated with compatible architecture s390x on 4.13", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.13",
				CPUArchitecture:       models.ClusterCPUArchitectureS390x,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
				UserManagedNetworking: swag.Bool(true),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, nil, nil)).To(BeNil())
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
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureArm64, &cluster, nil, nil).Error()).To(Equal(expectedError))
		})
		It("ClusterManagedNetworking feature is activated with compatible architecture on 4.11", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion:      "4.11",
				CPUArchitecture:       models.ClusterCPUArchitectureArm64,
				HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
				Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
				UserManagedNetworking: swag.Bool(false),
			}}
			Expect(ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureArm64, &cluster, nil, nil)).To(BeNil())
		})
		It("Ppc64le with CMN - fail", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.12",
				CPUArchitecture:  models.ClusterCPUArchitecturePpc64le,
			}}
			infraEnv := models.InfraEnv{CPUArchitecture: models.ClusterCPUArchitecturePpc64le, Type: common.ImageTypePtr(models.ImageTypeFullIso)}

			err := ValidateIncompatibleFeatures(log, models.ClusterCPUArchitecturePpc64le, &cluster, nil, nil)
			Expect(err).To(Not(BeNil()))
			cluster.UserManagedNetworking = swag.Bool(true)
			err = ValidateIncompatibleFeatures(log, models.ClusterCPUArchitecturePpc64le, &cluster, &infraEnv, nil)
			Expect(err).To(BeNil())
		})
		It("s390x with CMN and minimal iso - fail", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.12",
				CPUArchitecture:  models.ClusterCPUArchitectureS390x,
			}}
			infraEnv := models.InfraEnv{CPUArchitecture: models.ClusterCPUArchitectureS390x, Type: common.ImageTypePtr(models.ImageTypeMinimalIso)}

			err := ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, nil, nil)
			Expect(err).To(Not(BeNil()))
			cluster.UserManagedNetworking = swag.Bool(true)
			err = ValidateIncompatibleFeatures(log, models.ClusterCPUArchitectureS390x, &cluster, &infraEnv, nil)
			Expect(err).To(Not(BeNil()))
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
					Expect(feature.getFeatureActiveLevel(&cluster, nil, nil, nil)).To(Equal(activeLevelActive))
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
					Expect(feature.getFeatureActiveLevel(&cluster, nil, nil, nil)).To(Equal(activeLevelActive))
					Expect(feature.getFeatureActiveLevel(&cluster, nil, &params, nil)).To(Equal(activeLevelNotActive))
				}
				Expect((&SnoFeature{}).getFeatureActiveLevel(&cluster, nil, &params, nil)).To(Equal(activeLevelActive))
				Expect((&LvmFeature{}).getFeatureActiveLevel(&cluster, nil, &params, nil)).To(Equal(activeLevelActive))
				Expect((&ClusterManagedNetworkingFeature{}).getFeatureActiveLevel(&cluster, nil, &params, nil)).To(Equal(activeLevelActive))
			})
			It("ppc supporting minimal-iso", func() {
				cpuArchitecture := models.ClusterCPUArchitecturePpc64le
				cluster := common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion: "4.12",
					CPUArchitecture:  cpuArchitecture,
				}}
				infraEnv := models.InfraEnv{Type: common.ImageTypePtr(models.ImageTypeMinimalIso)}
				Expect((&MinimalIso{}).getFeatureActiveLevel(&cluster, &infraEnv, nil, nil)).To(Equal(activeLevelActive))

				filters := SupportLevelFilters{OpenshiftVersion: "4.12", CPUArchitecture: &cpuArchitecture}
				Expect((&MinimalIso{}).getSupportLevel(filters)).To(Equal(models.SupportLevelSupported))
			})
			It("s390x not supporting minimal-iso", func() {
				cpuArchitecture := models.ClusterCPUArchitectureS390x
				cluster := common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion: "4.12",
					CPUArchitecture:  cpuArchitecture,
				}}
				infraEnv := models.InfraEnv{Type: common.ImageTypePtr(models.ImageTypeMinimalIso)}
				Expect((&MinimalIso{}).getFeatureActiveLevel(&cluster, &infraEnv, nil, nil)).To(Equal(activeLevelActive))

				filters := SupportLevelFilters{OpenshiftVersion: "4.12", CPUArchitecture: &cpuArchitecture}
				Expect((&MinimalIso{}).getSupportLevel(filters)).To(Equal(models.SupportLevelUnavailable))
			})

			It("s390x not supporting minimal-iso without cluster", func() {
				cpuArchitecture := models.ClusterCPUArchitectureS390x

				infraEnv := models.InfraEnv{Type: common.ImageTypePtr(models.ImageTypeMinimalIso)}
				Expect((&MinimalIso{}).getFeatureActiveLevel(nil, &infraEnv, nil, nil)).To(Equal(activeLevelActive))

				filters := SupportLevelFilters{OpenshiftVersion: "", CPUArchitecture: &cpuArchitecture}
				Expect((&MinimalIso{}).getSupportLevel(filters)).To(Equal(models.SupportLevelUnavailable))
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

				Expect((&CnvFeature{}).getFeatureActiveLevel(&cluster, nil, nil, nil)).To(Equal(activeLevelActive))
				Expect((&CnvFeature{}).getFeatureActiveLevel(&cluster, nil, &params, nil)).To(Equal(activeLevelNotActive))
			})
		})

		Context("getSupportLevel", func() {
			It("featuressupport.getSupportLevel equal to Feature.getSupportLevel", func() {
				featureA := ClusterManagedNetworkingFeature{}
				openshiftVersion := "4.13"
				cpuArchitecture := models.ClusterCPUArchitectureS390x
				filters := SupportLevelFilters{OpenshiftVersion: openshiftVersion, CPUArchitecture: &cpuArchitecture}
				supportLevel := featureA.getSupportLevel(filters)
				equalSupportLevel := GetSupportLevel(featureA.getId(), filters)
				Expect(supportLevel).To(Equal(equalSupportLevel))
			})
		})

		Context("getIncompatibleFeatures", func() {
			It("Features without any restrictions", func() {
				features := []models.FeatureSupportLevelID{
					models.FeatureSupportLevelIDVIPAUTOALLOC,
					models.FeatureSupportLevelIDCUSTOMMANIFEST,
					models.FeatureSupportLevelIDDUALSTACKVIPS,
					models.FeatureSupportLevelIDSINGLENODEEXPANSION,
					models.FeatureSupportLevelIDCNV,
				}
				for _, featureId := range features {
					Expect(featuresList[featureId].getIncompatibleFeatures()).To(BeNil())
				}
			})

			It("Features with restrictions - Nutanix and UserManagedNetworking", func() {
				umnFeature := featuresList[models.FeatureSupportLevelIDUSERMANAGEDNETWORKING]
				nutanixFeature := featuresList[models.FeatureSupportLevelIDNUTANIXINTEGRATION]

				isUmnIncompatibleWithNutanix := isFeatureCompatible(umnFeature, nutanixFeature)
				isNutanixIncompatibleWithUmn := isFeatureCompatible(nutanixFeature, umnFeature)

				Expect((*isUmnIncompatibleWithNutanix).getId()).To(Equal(nutanixFeature.getId()))
				Expect((*isNutanixIncompatibleWithUmn).getId()).To(Equal(umnFeature.getId()))
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

				Expect((*isSnoIncompatibleWithNutanix).getId()).To(Equal(nutanixFeature.getId()))
				Expect((*isSnoIncompatibleWithVsphere).getId()).To(Equal(vsphereFeature.getId()))
				Expect((*isSnoIncompatibleWithCmn).getId()).To(Equal(cmnFeature.getId()))
				Expect((*isNutanixIncompatibleWithSno).getId()).To(Equal(snoFeature.getId()))
				Expect((*isVsphereIncompatibleWithSno).getId()).To(Equal(snoFeature.getId()))
				Expect((*isCmnIncompatibleWithSno).getId()).To(Equal(snoFeature.getId()))
			})
		})
	})
})

func TestOperators(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Feature-Support-Level tests")
}
