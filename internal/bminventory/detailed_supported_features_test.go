// Assisted-by: Cursor
package bminventory

import (
	"context"
	"net/http"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

var _ = Describe("GetDetailedSupportedFeatures", func() {
	var (
		bm  *bareMetalInventory
		ctx = context.Background()
	)

	BeforeEach(func() {
		bm = createInventory(nil, Config{})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	// Mock operator dependencies data
	mockOperatorDependencies := func() {
		mockOperatorManager.EXPECT().GetOperatorDependenciesFeatureID().Return([]operators.OperatorFeatureSupportID{
			{
				OperatorName:     "lvm",
				FeatureSupportID: models.FeatureSupportLevelIDLVM,
				Dependencies:     []models.FeatureSupportLevelID{},
			},
			{
				OperatorName:     "odf",
				FeatureSupportID: models.FeatureSupportLevelIDODF,
				Dependencies:     []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLSO},
			},
			{
				OperatorName:     "cnv",
				FeatureSupportID: models.FeatureSupportLevelIDCNV,
				Dependencies:     []models.FeatureSupportLevelID{models.FeatureSupportLevelIDLSO, models.FeatureSupportLevelIDLVM},
			},
		}).AnyTimes()
	}

	Context("Successful responses", func() {
		It("should return detailed features for x86_64 and OpenShift 4.13", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload
			Expect(payload).ToNot(BeNil())
			Expect(payload.Features).ToNot(BeNil())
			Expect(payload.Operators).ToNot(BeNil())

			// Should have a good mix of features and operators
			Expect(len(payload.Features)).To(BeNumerically(">", 30))
			Expect(len(payload.Operators)).To(Equal(3))
		})

		It("should return detailed features for ARM64 and OpenShift 4.13", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("arm64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload
			Expect(payload).ToNot(BeNil())

			// ARM64 should have different support levels for some features
			for _, feature := range payload.Features {
				Expect(feature.FeatureSupportLevelID).ToNot(BeEmpty())
				Expect(feature.SupportLevel).ToNot(BeEmpty())
			}

			for _, operator := range payload.Operators {
				Expect(operator.FeatureSupportLevelID).ToNot(BeEmpty())
				Expect(operator.SupportLevel).ToNot(BeEmpty())
				Expect(operator.Name).ToNot(BeNil())
				Expect(operator.Dependencies).ToNot(BeNil())
			}
		})

		It("should return detailed features without any platform type", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
				PlatformType:     swag.String("baremetal"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload
			Expect(payload).ToNot(BeNil())

			for _, feature := range payload.Features {
				Expect(feature.FeatureSupportLevelID).NotTo(BeElementOf([]models.FeatureSupportLevelID{
					models.FeatureSupportLevelIDNUTANIXINTEGRATION,
					models.FeatureSupportLevelIDVSPHEREINTEGRATION,
					models.FeatureSupportLevelIDEXTERNALPLATFORMOCI,
					models.FeatureSupportLevelIDBAREMETALPLATFORM,
					models.FeatureSupportLevelIDNONEPLATFORM,
					models.FeatureSupportLevelIDEXTERNALPLATFORM,
				}))
			}
		})

		It("should return detailed features with external platform and platform name", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion:     "4.18.0",
				CPUArchitecture:      swag.String("x86_64"),
				PlatformType:         swag.String("external"),
				ExternalPlatformName: swag.String("oci"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload
			Expect(payload).ToNot(BeNil())
		})

		It("should return features with incompatibilities", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Find SNO feature and check its incompatibilities
			foundSNO := false
			for _, feature := range payload.Features {
				if feature.FeatureSupportLevelID == models.FeatureSupportLevelIDSNO {
					foundSNO = true
					Expect(feature.Incompatibilities).To(ContainElement(models.FeatureSupportLevelIDODF))
					Expect(feature.Incompatibilities).To(ContainElement(models.FeatureSupportLevelIDCLUSTERMANAGEDNETWORKING))
					break
				}
			}
			Expect(foundSNO).To(BeTrue())
		})

		It("should return operators with dependencies", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Find ODF operator and check its dependencies
			foundODF := false
			for _, operator := range payload.Operators {
				if operator.FeatureSupportLevelID == models.FeatureSupportLevelIDODF {
					foundODF = true
					Expect(*operator.Name).To(Equal("odf"))
					Expect(operator.Dependencies).To(ContainElement(models.FeatureSupportLevelIDLSO))
					break
				}
			}
			Expect(foundODF).To(BeTrue())
		})
	})

	Context("Parameter validation errors", func() {
		It("should return error for invalid OpenShift version", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "invalid-version",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			verifyApiError(response, http.StatusBadRequest)
		})

		It("should return error for missing CPU architecture", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  nil,
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			verifyApiErrorString(response, http.StatusBadRequest, "cpu architecture is required")
		})

		It("should return error for unsupported CPU architecture", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.9.0", // ARM64 not supported before 4.10
				CPUArchitecture:  swag.String("arm64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			verifyApiErrorString(response, http.StatusBadRequest, "cpu architecture arm64 is not supported for openshift version 4.9.0")
		})

		It("should return error for unsupported platform", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.10.0", // Nutanix not supported before 4.11
				CPUArchitecture:  swag.String("x86_64"),
				PlatformType:     swag.String("nutanix"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			verifyApiErrorString(response, http.StatusBadRequest, "platform nutanix is not supported for openshift version 4.10.0")
		})

		It("should return error for invalid external platform name", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion:     "4.13.0",
				CPUArchitecture:      swag.String("x86_64"),
				PlatformType:         swag.String("external"),
				ExternalPlatformName: swag.String("invalid-platform"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			verifyApiErrorString(response, http.StatusBadRequest, "invalid external platform name")
		})
	})

	Context("Architecture-specific features", func() {
		It("should show ARM64 features as unavailable for OpenShift 4.9", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.9.0",
				CPUArchitecture:  swag.String("x86_64"), // Valid arch, but some features check for ARM64 compatibility
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Check that some features have correct incompatibility reasons
			for _, feature := range payload.Features {
				// Features that check architecture compatibility should have proper reasons
				if feature.SupportLevel == models.SupportLevelUnavailable {
					Expect(feature.Reason).To(BeElementOf(
						models.IncompatibilityReasonCPUArchitecture,
						models.IncompatibilityReasonPlatform,
						models.IncompatibilityReasonOpenshiftVersion,
					))
				}
			}
		})

		It("should show S390x features correctly for OpenShift 4.13", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("s390x"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// SNO should be available for S390x on 4.13
			foundSNO := false
			for _, feature := range payload.Features {
				if feature.FeatureSupportLevelID == models.FeatureSupportLevelIDSNO {
					foundSNO = true
					Expect(feature.SupportLevel).To(Equal(models.SupportLevelDevPreview))
					break
				}
			}
			Expect(foundSNO).To(BeTrue())
		})
	})

	Context("Platform-specific features", func() {
		It("should show platform-specific features for baremetal", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Check for platform-specific features
			foundBaremetalPlatform := false
			for _, feature := range payload.Features {
				if feature.FeatureSupportLevelID == models.FeatureSupportLevelIDBAREMETALPLATFORM {
					foundBaremetalPlatform = true
					break
				}
			}
			Expect(foundBaremetalPlatform).To(BeTrue())
		})

		It("should show correct features for vsphere platform", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
				PlatformType:     swag.String("vsphere"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// SNO should be unavailable for vSphere
			foundSNO := false
			for _, feature := range payload.Features {
				if feature.FeatureSupportLevelID == models.FeatureSupportLevelIDSNO {
					foundSNO = true
					Expect(feature.SupportLevel).To(Equal(models.SupportLevelUnavailable))
					Expect(feature.Reason).To(Equal(models.IncompatibilityReasonPlatform))
					break
				}
			}
			Expect(foundSNO).To(BeTrue())
		})
	})

	Context("Version-specific features", func() {
		It("should show version-specific support levels", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.12.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Check that features have proper support levels
			for _, feature := range payload.Features {
				Expect(feature.SupportLevel).To(BeElementOf(
					models.SupportLevelSupported,
					models.SupportLevelUnsupported,
					models.SupportLevelUnavailable,
					models.SupportLevelTechPreview,
					models.SupportLevelDevPreview,
				))
			}
		})
	})

	Context("Empty operator dependencies", func() {
		It("should handle empty operator dependencies gracefully", func() {
			mockOperatorManager.EXPECT().GetOperatorDependenciesFeatureID().Return([]operators.OperatorFeatureSupportID{}).Times(1)

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion: "4.13.0",
				CPUArchitecture:  swag.String("x86_64"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload
			Expect(payload.Features).ToNot(BeNil())
			Expect(payload.Operators).To(HaveLen(0))
		})
	})

	Context("OCI external platform", func() {
		It("should show OCI platform features correctly", func() {
			mockOperatorDependencies()

			params := installer.GetDetailedSupportedFeaturesParams{
				OpenshiftVersion:     "4.18.0",
				CPUArchitecture:      swag.String("x86_64"),
				PlatformType:         swag.String("external"),
				ExternalPlatformName: swag.String("oci"),
			}

			response := bm.GetDetailedSupportedFeatures(ctx, params)
			Expect(response).Should(BeAssignableToTypeOf(&installer.GetDetailedSupportedFeaturesOK{}))

			payload := response.(*installer.GetDetailedSupportedFeaturesOK).Payload

			// Full ISO should be unavailable for OCI
			foundFullIso := false
			for _, feature := range payload.Features {
				if feature.FeatureSupportLevelID == models.FeatureSupportLevelIDFULLISO {
					foundFullIso = true
					Expect(feature.SupportLevel).To(Equal(models.SupportLevelUnavailable))
					Expect(feature.Reason).To(Equal(models.IncompatibilityReasonOciExternalIntegrationDisabled))
					break
				}
			}
			Expect(foundFullIso).To(BeTrue())
		})
	})
})
