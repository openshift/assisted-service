package subsystem

import (
	"context"
	"fmt"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("V2ListFeatureSupportLevels API", func() {
	It("Should return the feature list", func() {
		response, err := userBMClient.Installer.V2ListFeatureSupportLevels(context.Background(), installer.NewV2ListFeatureSupportLevelsParams())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(response.Payload).To(BeEquivalentTo(featuresupport.SupportLevelsList))
	})
	It("Should respond with an error for unauth user", func() {
		_, err := unallowedUserBMClient.Installer.V2ListFeatureSupportLevels(context.Background(), installer.NewV2ListFeatureSupportLevelsParams())
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Feature support levels API", func() {
	var ctx context.Context
	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("Support Level", func() {
		Context("Supported features", func() {
			availableVersions := []string{"4.8", "4.9", "4.10", "4.11", "4.12", "4.13"}

			var params installer.GetSupportedFeaturesParams
			arch := models.ClusterCPUArchitectureX8664

			for _, v := range availableVersions {
				version := v

				It(fmt.Sprintf("GetSupportedFeatures x86 CPU architectrue, OCP version %s", version), func() {
					params = installer.GetSupportedFeaturesParams{
						OpenshiftVersion: version,
						CPUArchitecture:  swag.String(arch),
					}
					response, err := userBMClient.Installer.GetSupportedFeatures(ctx, &params)
					Expect(err).ShouldNot(HaveOccurred())

					for featureID, supportLevel := range response.Payload.Features {
						filters := featuresupport.SupportLevelFilters{OpenshiftVersion: version, CPUArchitecture: swag.String(arch)}
						featureSupportLevel := featuresupport.GetSupportLevel(models.FeatureSupportLevelID(featureID), filters)
						Expect(featureSupportLevel).To(BeEquivalentTo(supportLevel))
					}
				})

				It(fmt.Sprintf("GetSupportedFeatures with empty CPU architectrue, OCP version %s", version), func() {
					response, err := userBMClient.Installer.GetSupportedFeatures(ctx, &installer.GetSupportedFeaturesParams{OpenshiftVersion: version})
					Expect(err).ShouldNot(HaveOccurred())

					for featureID, supportLevel := range response.Payload.Features {
						filters := featuresupport.SupportLevelFilters{OpenshiftVersion: version, CPUArchitecture: swag.String(common.DefaultCPUArchitecture)}
						featureSupportLevel := featuresupport.GetSupportLevel(models.FeatureSupportLevelID(featureID), filters)
						Expect(featureSupportLevel).To(BeEquivalentTo(supportLevel))
					}
				})
			}
		})

		Context("Supported architectures", func() {
			var params installer.GetSupportedArchitecturesParams

			It("GetSupportedArchitectures with OCP version 4.6", func() {
				version := "4.6"
				params.OpenshiftVersion = version

				response, err := userBMClient.Installer.GetSupportedArchitectures(ctx, &params)
				Expect(err).ShouldNot(HaveOccurred())

				architecturesSupportLevel := response.Payload.Architectures

				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDX8664ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDARM64ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDS390XARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
			})

			It("GetSupportedArchitectures with OCP version 4.12", func() {
				version := "4.12"
				params.OpenshiftVersion = version

				response, err := userBMClient.Installer.GetSupportedArchitectures(ctx, &params)
				Expect(err).ShouldNot(HaveOccurred())

				architecturesSupportLevel := response.Payload.Architectures

				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDX8664ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDARM64ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDS390XARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnsupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE)]).To(BeEquivalentTo(models.SupportLevelTechPreview))
			})

			It("GetSupportedArchitectures with OCP version 4.13", func() {
				version := "4.13"
				params.OpenshiftVersion = version

				response, err := userBMClient.Installer.GetSupportedArchitectures(ctx, &params)
				Expect(err).ShouldNot(HaveOccurred())

				architecturesSupportLevel := response.Payload.Architectures

				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDX8664ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDARM64ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDS390XARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE)]).To(BeEquivalentTo(models.SupportLevelTechPreview))
			})
		})

	})
})
