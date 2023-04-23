package subsystem

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
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
		registerNewCluster := func(version, cpuArchitecture, highAvailabilityMode string, userManagedNetworking *bool) (*installer.V2RegisterClusterCreated, error) {
			cluster, errRegisterCluster := user2BMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                  swag.String("test-cluster"),
					OpenshiftVersion:      swag.String(version),
					PullSecret:            swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					BaseDNSDomain:         "example.com",
					CPUArchitecture:       cpuArchitecture,
					HighAvailabilityMode:  swag.String(highAvailabilityMode),
					UserManagedNetworking: userManagedNetworking,
				},
			})

			if errRegisterCluster != nil {
				return nil, errRegisterCluster
			}

			return cluster, nil
		}

		registerNewInfraEnv := func(id *strfmt.UUID, version, cpuArchitecture string) (*installer.RegisterInfraEnvCreated, error) {
			infraEnv, errRegisterInfraEnv := user2BMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env"),
					OpenshiftVersion: version,
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					SSHAuthorizedKey: swag.String(sshPublicKey),
					ImageType:        models.ImageTypeFullIso,
					ClusterID:        id,
					CPUArchitecture:  cpuArchitecture,
				},
			})

			return infraEnv, errRegisterInfraEnv
		}

		Context("Update cluster", func() {
			It("Update umn true won't fail on 4.13 with s390x without infra-env", func() {
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeFull, swag.Bool(true))
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))

				_, err = user2BMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						UserManagedNetworking: swag.Bool(false),
					},
					ClusterID: *cluster.Payload.ID,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			It("Update umn true fail on 4.13 with s390x with infra-env", func() {
				expectedError := "cannot use Cluster Managed Networking because it's not compatible with the s390x architecture on version 4.13"
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeFull, swag.Bool(true))
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))

				infraEnv, err := registerNewInfraEnv(cluster.Payload.ID, "4.13", "s390x")
				Expect(err).NotTo(HaveOccurred())
				Expect(infraEnv.Payload.CPUArchitecture).To(Equal("s390x"))

				_, err = user2BMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						UserManagedNetworking: swag.Bool(false),
					},
					ClusterID: *cluster.Payload.ID,
				})
				Expect(err).To(HaveOccurred())
				err2 := err.(*installer.V2UpdateClusterBadRequest)
				ExpectWithOffset(1, *err2.Payload.Reason).To(ContainSubstring(expectedError))
			})

			It("Create infra-env after updating OLM operators on s390x architecture ", func() {
				expectedError := "cannot use OpenShift Virtualization because it's not compatible with the s390x architecture on version 4.13"
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeFull, swag.Bool(true))
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))

				_, err = user2BMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: "odf"},
							{Name: "cnv"},
							{Name: "lso"},
						},
					},
					ClusterID: *cluster.Payload.ID,
				})
				Expect(err).ToNot(HaveOccurred())

				_, err = registerNewInfraEnv(cluster.Payload.ID, "4.13", "s390x")
				err2 := err.(*installer.RegisterInfraEnvBadRequest)
				ExpectWithOffset(1, *err2.Payload.Reason).To(ContainSubstring(expectedError))
			})
			Context("UpdateInfraEnv", func() {
				It("Update ppc64le infra env minimal iso without cluster", func() {
					infraEnv, err := registerNewInfraEnv(nil, "4.12", models.ClusterCPUArchitecturePpc64le)
					Expect(err).ToNot(HaveOccurred())
					Expect(common.ImageTypeValue(infraEnv.Payload.Type)).ToNot(Equal(models.ImageTypeMinimalIso))

					updatedInfraEnv, err := user2BMClient.Installer.UpdateInfraEnv(ctx, &installer.UpdateInfraEnvParams{
						InfraEnvID: *infraEnv.Payload.ID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
							ImageType: models.ImageTypeMinimalIso,
						}})
					Expect(err).ToNot(HaveOccurred())
					Expect(common.ImageTypeValue(updatedInfraEnv.Payload.Type)).To(Equal(models.ImageTypeMinimalIso))
				})
				It("Update ppc64le infra env minimal iso with cluster", func() {
					cluster, err := registerNewCluster("4.12", models.ClusterCPUArchitecturePpc64le, models.ClusterHighAvailabilityModeFull, swag.Bool(true))
					Expect(err).NotTo(HaveOccurred())

					infraEnv, err := registerNewInfraEnv(cluster.Payload.ID, "4.12", models.ClusterCPUArchitecturePpc64le)
					Expect(err).ToNot(HaveOccurred())
					Expect(common.ImageTypeValue(infraEnv.Payload.Type)).ToNot(Equal(models.ImageTypeMinimalIso))

					updatedInfraEnv, err := user2BMClient.Installer.UpdateInfraEnv(ctx, &installer.UpdateInfraEnvParams{
						InfraEnvID: *infraEnv.Payload.ID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
							ImageType: models.ImageTypeMinimalIso,
						}})
					Expect(err).ToNot(HaveOccurred())
					Expect(common.ImageTypeValue(updatedInfraEnv.Payload.Type)).To(Equal(models.ImageTypeMinimalIso))
				})
				It("Update s390x infra env minimal iso with cluster - fail", func() {
					cluster, err := registerNewCluster("4.12", "s390x", models.ClusterHighAvailabilityModeFull, swag.Bool(true))
					Expect(err).NotTo(HaveOccurred())

					infraEnv, err := registerNewInfraEnv(cluster.Payload.ID, "4.12", models.ClusterCPUArchitectureS390x)
					Expect(err).ToNot(HaveOccurred())
					Expect(common.ImageTypeValue(infraEnv.Payload.Type)).ToNot(Equal(models.ImageTypeMinimalIso))

					_, err = user2BMClient.Installer.UpdateInfraEnv(ctx, &installer.UpdateInfraEnvParams{
						InfraEnvID: *infraEnv.Payload.ID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
							ImageType: models.ImageTypeMinimalIso,
						}})
					Expect(err).To(HaveOccurred())
					err2 := err.(*installer.UpdateInfraEnvBadRequest)
					ExpectWithOffset(1, *err2.Payload.Reason).To(ContainSubstring("cannot use Minimal ISO because it's not compatible with the s390x architecture on version 4.12"))
				})
			})
		})

		Context("Register cluster", func() {
			It("Register cluster won't fail on 4.13 with s390x", func() {
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeFull, swag.Bool(true))
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))
			})

			It("Register cluster won't fail on 4.13 with s390x without UMN", func() {
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeFull, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))
			})

			It("SNO with s390x 4.10 fails on architecture- failure", func() {
				expectedError := "Requested CPU architecture s390x is not available"
				_, err := registerNewCluster("4.10", "s390x", models.ClusterHighAvailabilityModeNone, swag.Bool(true))
				Expect(err).To(HaveOccurred())
				err2 := err.(*installer.V2RegisterClusterBadRequest)
				ExpectWithOffset(1, *err2.Payload.Reason).To(ContainSubstring(expectedError))
			})
			It("SNO with s390x fails on SNO isn't compatible with architecture success on 4.13", func() {
				cluster, err := registerNewCluster("4.13", "s390x", models.ClusterHighAvailabilityModeNone, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(cluster.Payload.CPUArchitecture).To(Equal("multi"))
				Expect(swag.StringValue(cluster.Payload.HighAvailabilityMode)).To(Equal(models.ClusterHighAvailabilityModeNone))

			})
			It("SNO with s390x fails on SNO isn't compatible with architecture on 4.12 - failure", func() {
				expectedError := "cannot use Single Node OpenShift because it's not compatible with the s390x architecture on version 4.12"
				_, err := registerNewCluster("4.12", "s390x", models.ClusterHighAvailabilityModeNone, swag.Bool(true))
				Expect(err).To(HaveOccurred())
				err2 := err.(*installer.V2RegisterClusterBadRequest)
				ExpectWithOffset(1, *err2.Payload.Reason).To(ContainSubstring(expectedError))
			})
		}) // Register cluster

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
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDARM64ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnavailable))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDS390XARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnavailable))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelUnavailable))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDMULTIARCHRELEASEIMAGE)]).To(BeEquivalentTo(models.SupportLevelUnavailable))
			})

			It("GetSupportedArchitectures with OCP version 4.12", func() {
				version := "4.12"
				params.OpenshiftVersion = version

				response, err := userBMClient.Installer.GetSupportedArchitectures(ctx, &params)
				Expect(err).ShouldNot(HaveOccurred())

				architecturesSupportLevel := response.Payload.Architectures

				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDX8664ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDARM64ARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelSupported))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDS390XARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelTechPreview))
				Expect(architecturesSupportLevel[string(models.ArchitectureSupportLevelIDPPC64LEARCHITECTURE)]).To(BeEquivalentTo(models.SupportLevelTechPreview))
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
