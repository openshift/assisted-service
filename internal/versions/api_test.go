package versions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	auth "github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/versions"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

var _ = Describe("V2ListComponentVersions", func() {
	var (
		h *apiHandler
	)

	BeforeEach(func() {
		versions := Versions{
			SelfVersion:     "self-version",
			AgentDockerImg:  "agent-image",
			InstallerImage:  "installer-image",
			ControllerImage: "controller-image",
			ReleaseTag:      "v1.2.3",
		}

		h = &apiHandler{
			log:             common.GetTestLog(),
			versions:        versions,
			authzHandler:    nil,
			versionsHandler: &handler{},
		}
	})

	It("returns the configured versions", func() {
		reply := h.V2ListComponentVersions(context.Background(), operations.V2ListComponentVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListComponentVersionsOK()))
		val, _ := reply.(*operations.V2ListComponentVersionsOK)

		Expect(val.Payload.Versions["assisted-installer-service"]).Should(Equal("self-version"))
		Expect(val.Payload.Versions["discovery-agent"]).Should(Equal("agent-image"))
		Expect(val.Payload.Versions["assisted-installer"]).Should(Equal("installer-image"))
		Expect(val.Payload.Versions["assisted-installer-controller"]).Should(Equal("controller-image"))
		Expect(val.Payload.ReleaseTag).Should(Equal("v1.2.3"))
	})
})

var _ = Describe("V2ListSupportedOpenshiftVersions", func() {
	var (
		db            *gorm.DB
		dbName        string
		ctrl          *gomock.Controller
		mockOcmAuthz  *ocm.MockOCMAuthorization
		mockOcmClient *ocm.Client
		mockRelease   *oc.MockRelease
		authCtx       context.Context
		orgID1        = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
		userName1     = "test_user_1"
		versions      Versions
		authzHandler  auth.Authorizer
		logger        = common.GetTestLog()
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		cfg := auth.GetConfigRHSSO()
		authzHandler = auth.NewAuthzHandler(cfg, nil, logger, db)

		payload := &ocm.AuthPayload{
			Username:     userName1,
			Organization: orgID1,
			Role:         ocm.UserRole,
		}
		authCtx = context.WithValue(context.Background(), restapi.AuthKey, payload)
		mockOcmClient = &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		err := createSupportLevelTable(db)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	readDefaultOsImages := func() OSImages {
		bytes, err := os.ReadFile("../../data/default_os_images.json")
		Expect(err).ShouldNot(HaveOccurred())

		osImages := models.OsImages{}
		err = json.Unmarshal(bytes, &osImages)
		Expect(err).ShouldNot(HaveOccurred())

		osi, err := NewOSImages(osImages)
		Expect(err).ShouldNot(HaveOccurred())

		return osi
	}

	readDefaultReleaseImages := func(osImages OSImages) *handler {
		bytes, err := os.ReadFile("../../data/default_release_images.json")
		Expect(err).ShouldNot(HaveOccurred())

		releaseImages := &models.ReleaseImages{}
		err = json.Unmarshal(bytes, releaseImages)
		Expect(err).ShouldNot(HaveOccurred())

		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			ReleaseImages:  *releaseImages,
			DB:             db,
		})
		Expect(err).ShouldNot(HaveOccurred())
		return versionsHandler
	}

	handlerWithAuthConfig := func(enableOrgBasedFeatureGates bool) restapi.VersionsAPI {
		cfg := auth.GetConfigRHSSO()
		cfg.EnableOrgBasedFeatureGates = enableOrgBasedFeatureGates
		authorizationHandler := auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog(), db)

		osImages, err := NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())

		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:           common.GetTestLog(),
			ReleaseImages: defaultReleaseImages,
			DB:            db,
		})
		Expect(err).ShouldNot(HaveOccurred())

		return NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             common.GetTestLog(),
			AuthzHandler:    authorizationHandler,
			Versions:        Versions{},
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})
	}

	apiHandlerWithHandlers := func(versionsHandler *handler, osImagesList OSImages) restapi.VersionsAPI {
		cfg := auth.GetConfigRHSSO()
		cfg.EnableOrgBasedFeatureGates = true
		authorizationHandler := auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog(), db)

		return NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             common.GetTestLog(),
			AuthzHandler:    authorizationHandler,
			Versions:        Versions{},
			VersionsHandler: versionsHandler,
			OSImages:        osImagesList,
		})
	}

	hasMultiarch := func(versions models.OpenshiftVersions) bool {
		hasMultiarch := false
		for _, version := range versions {
			if strings.HasSuffix(*version.DisplayName, "-multi") {
				hasMultiarch = true
				break
			}
		}
		return hasMultiarch
	}

	type listOpenshiftVersionsTestParams struct {
		releaseSources  models.ReleaseSources
		releaseImages   models.ReleaseImages
		osImages        models.OsImages
		dbReleases      []common.ReleaseImage
		ignoredReleases []string
		versionPattern  *string
		onlyLatest      *bool
		expectedPayload models.OpenshiftVersions
	}

	testListOpenshiftVerionsWithParams := func(tests []listOpenshiftVersionsTestParams, db *gorm.DB, dbName string) {
		for _, test := range tests {
			common.DeleteTestDB(db, dbName)
			db, dbName = common.PrepareTestDB()

			versionsHandler, err := NewHandler(NewVersionHandlerParams{
				Log:                  common.GetTestLog(),
				ReleaseImages:        test.releaseImages,
				DB:                   db,
				IgnoredReleaseImages: test.ignoredReleases,
				ReleaseSources:       test.releaseSources,
			})

			mockOcmAuthz.
				EXPECT().
				CapabilityReview(context.Background(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).
				Return(true, nil).
				Times(1)

			Expect(err).ToNot(HaveOccurred())

			osImagesList, err := NewOSImages(test.osImages)
			Expect(err).ToNot(HaveOccurred())

			if test.dbReleases != nil && len(test.dbReleases) > 0 {
				err = db.Create(&test.dbReleases).Error
				Expect(err).ToNot(HaveOccurred())
			}

			apiHandler := apiHandlerWithHandlers(versionsHandler, osImagesList)

			middlewareResponder := apiHandler.V2ListSupportedOpenshiftVersions(
				authCtx,
				operations.V2ListSupportedOpenshiftVersionsParams{
					VersionPattern: test.versionPattern,
					OnlyLatest:     test.onlyLatest,
				},
			)
			Expect(middlewareResponder).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			reply, ok := middlewareResponder.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(ok).To(BeTrue())

			payload := reply.Payload
			Expect(payload).To(Equal(test.expectedPayload))
		}
	}

	It("get_defaults from data directory", func() {
		osImages := readDefaultOsImages()
		versionsHandler := readDefaultReleaseImages(osImages)

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		defaultExists := false

		for _, releaseImage := range versionsHandler.releaseImages {
			key := *releaseImage.Version
			version := val.Payload[key]
			architecture := *releaseImage.CPUArchitecture
			architectures := releaseImage.CPUArchitectures
			defaultExists = defaultExists || releaseImage.Default
			if len(architectures) < 2 {
				// For single-arch release we require in the test that there is a matching
				// OS image for the provided release image. Otherwise the whole release image
				// is not usable and indicates a mistake.
				if architecture == "" {
					architecture = common.CPUArchitecture
				}
				if architecture == common.CPUArchitecture {
					Expect(version.Default).Should(Equal(releaseImage.Default))
				}

				Expect(version.CPUArchitectures).Should(ContainElement(architecture))
				Expect(version.DisplayName).Should(Equal(releaseImage.Version))
			} else {
				// For multi-arch release we don't require a strict matching for every
				// architecture supported by this image. As long as we have at least one OS
				// image that matches, we are okay. This is to allow setups where release
				// image supports more architectures than we have available RHCOS images.
				Expect(len(version.CPUArchitectures)).ShouldNot(Equal(0))
				Expect(*version.DisplayName).Should(ContainSubstring(*releaseImage.Version))
			}
		}

		// We want to make sure after parsing the default file, there is always a release
		// image marked as default. It is not desired to have a service without anything
		// to default to.
		Expect(defaultExists).Should(Equal(true))
	})

	It("missing release images", func() {
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			DB:             db,
		})
		Expect(err).ToNot(HaveOccurred())

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		Expect(val.Payload).Should(BeEmpty())
	})

	It("release image without cpu_architectures field", func() {
		releaseImages := models.ReleaseImages{
			// This image uses a syntax with missing "cpu_architectures". It is crafted
			// in order to make sure the change in MGMT-11494 is backwards-compatible.
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          true,
				Version:          swag.String("4.11.1"),
			},
		}

		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			ReleaseImages:  releaseImages,
			DB:             db,
		})
		Expect(err).ToNot(HaveOccurred())

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1")))
		Expect(version.Default).Should(Equal(true))
	})

	It("release image without matching OS image", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, "risc-v"},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          true,
				Version:          swag.String("4.11.1-multi"),
			},
		}
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			ReleaseImages:  releaseImages,
			DB:             db,
		})
		Expect(err).ToNot(HaveOccurred())

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1-multi"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(version.CPUArchitectures).ShouldNot(ContainElement("risc-v"))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-multi")))
		Expect(version.Default).Should(Equal(true))
	})

	It("single-arch and multi-arch for the same version", func() {
		releaseImages := models.ReleaseImages{
			// Those images provide the same architecture using single-arch as well as multi-arch
			// release images. This is to test if in this scenario we don't return duplicated
			// entries in the supported architectures list.
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Version:          swag.String("4.11.1"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Version:          swag.String("4.11.1"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          true,
				Version:          swag.String("4.11.1"),
			},
		}
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			ReleaseImages:  releaseImages,
			DB:             db,
		})
		Expect(err).ToNot(HaveOccurred())

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.ARM64CPUArchitecture))
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(len(version.CPUArchitectures)).Should(Equal(2))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1")))
		Expect(version.Default).Should(Equal(true))
	})

	It("append multi suffix for multi-arch image automatically", func() {
		releaseImages := models.ReleaseImages{
			// This image definition does not contain "-multi" suffix anywhere because CVO for it
			// does not return it. Nevertheless in the UI we want to display it, so we test for
			// autoappend.
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.12.1"),
				URL:              swag.String("release_4.12.1"),
				Default:          true,
				Version:          swag.String("4.12.1"),
			},
		}
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(NewVersionHandlerParams{
			Log:            logger,
			ReleaseHandler: mockRelease,
			ReleaseImages:  releaseImages,
			DB:             db,
		})
		Expect(err).ToNot(HaveOccurred())

		h := NewAPIHandler(NewVersionsAPIHandlerParams{
			Log:             logger,
			AuthzHandler:    authzHandler,
			Versions:        versions,
			VersionsHandler: versionsHandler,
			OSImages:        osImages,
		})

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.12.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.ARM64CPUArchitecture))
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(len(version.CPUArchitectures)).Should(Equal(2))
		Expect(version.DisplayName).Should(Equal(swag.String("4.12.1-multi")))
		Expect(version.Default).Should(Equal(true))
	})

	Context("Test list openshift versions with capability restrictions", func() {
		It("returns multiarch with multiarch capability", func() {
			h := handlerWithAuthConfig(true)
			mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)

			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(hasMultiarch(val.Payload)).To(BeTrue())
		})

		It("does not return multiarch without multiarch capability", func() {
			h := handlerWithAuthConfig(true)
			mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(hasMultiarch(val.Payload)).To(BeFalse())
		})

		It("returns internal error when capability query fails", func() {
			h := handlerWithAuthConfig(true)
			mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, errors.New("failed to query capability")).Times(1)

			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsInternalServerError()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsInternalServerError)
			Expect(val.Payload).To(Equal(common.GenerateError(http.StatusInternalServerError, errors.New("failed to query capability"))))
		})

		It("returns multiarch with org-based features disabled", func() {
			h := handlerWithAuthConfig(false)

			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(hasMultiarch(val.Payload)).To(BeTrue())
		})
	})

	Context("Test flow without filtering", func() {
		It("Get all releases different scenarios", func() {
			tests := []listOpenshiftVersionsTestParams{

				// No release sources
				// No configuration releases
				// Sufficient OS images
				// No releases to ignore
				// No query parameters
				// DB releases exists
				// Should get versions from DB
				{
					releaseSources: models.ReleaseSources{},
					releaseImages:  models.ReleaseImages{},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.AARCH64CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      nil,
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.1"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.1", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
					},
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// No query parameters
				// No DB releases
				// Should get releases from config, aggregate two of them on CPU arch
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
						},
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
							CPUArchitectures: []string{common.ARM64CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-aarch64"),
							Version:          swag.String("4.14.1"),
						},
						{
							OpenshiftVersion: swag.String("4.14.2"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.2"),
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.AARCH64CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-aarch64-live.aarch64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      nil,
					dbReleases:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Unsufficient OS images
				// No releases to ignore
				// No query parameters
				// DB releases exists
				// Should get only releases with matching OS image
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
						},
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
							CPUArchitectures: []string{common.ARM64CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-aarch64"),
							Version:          swag.String("4.14.1"),
						},
						{
							OpenshiftVersion: swag.String("4.14.2"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-aarch64"),
							Version:          swag.String("4.14.2"),
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      nil,
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3-multi"),
								CPUArchitecture: swag.String(common.MultiCPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.MultiCPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.1"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.1", common.X86CPUArchitecture)),
							},
						},
					},
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.3-multi": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.3-multi"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// No query parameters
				// DB releases exists
				// Should get releases from DB and config, precedence given to config
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
						},
						{
							OpenshiftVersion: swag.String("4.14.2"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-aarch64"),
							Version:          swag.String("4.14.2"),
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      nil,
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
					},
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// No query parameters
				// DB releases exists
				// Should get releases from DB and configuration overriding default
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.2"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.2"),
							Default:          true,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      nil,
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         true,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
					},
					expectedPayload: models.OpenshiftVersions{
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          true,
						},
						"4.14.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},
			}

			testListOpenshiftVerionsWithParams(tests, db, dbName)
		})
	})

	Context("Test flow with filtering", func() {
		It("Filter by only latest different scenrios", func() {
			// Should get releases from both DB and config, applying 'only latest' filter only on DB releases
			tests := []listOpenshiftVersionsTestParams{

				// Release sources exist (for fetching supported openshift versions)
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// only_latest is set
				// DB releases exists
				// Should get releases from both DB and config, applying 'only latest' filter only on DB releases
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      swag.Bool(true),
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.13.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.13.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// Release sources exist (for fetching supported openshift versions)
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// only_latest is set
				// DB releases exists
				// Should get releases from both DB and config, applying 'only latest' filter only on DB releases (none)
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases:      []common.ReleaseImage{},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      swag.Bool(true),
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// Release sources exist but missing (for fetching supported openshift versions)
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// only_latest is set
				// DB releases exists
				// Should get releases from both DB and config, applying 'only latest' filter only on DB releases, but getting only
				// releases that match release sources
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  nil,
					onlyLatest:      swag.Bool(true),
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.13.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.13.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},
			}

			testListOpenshiftVerionsWithParams(tests, db, dbName)
		})

		It("Filter by version pattern different scenrios", func() {
			// Should get releases from both DB and config, applying 'version pattern' filter only on DB releases
			tests := []listOpenshiftVersionsTestParams{

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '4.15'
				// DB releases exists
				// Should get releases from both DB and config, getting only 4.15 releases from db.
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("3.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("3.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("4.15"),
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.0-ec.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.0-ec.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '2'
				// DB releases exists
				// Should get releases from both DB and config, getting only releases that contain '2' from db.
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("3.14.4"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("3.14.4", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.5"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.5", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("2"),
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '2'
				// DB releases exists
				// Should get releases from both DB and config, getting only releases that contain '2' from db.
				// (Check that % is escaped correctly)
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("3.14.4"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("3.14.4", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.5"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.5", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("%"),
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// No release sources
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '9'
				// DB releases exists
				// Should get releases from both DB and config, getting only releases that contain '9' (none) from db.
				{
					releaseSources: models.ReleaseSources{},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("3.14.4"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("3.14.4", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.5"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.5", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("9"),
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},
			}

			testListOpenshiftVerionsWithParams(tests, db, dbName)
		})

		It("Filter by 'only latest'and 'version pattern' different scenrios", func() {
			tests := []listOpenshiftVersionsTestParams{

				// Release sources exist
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '4.15', only latest is set
				// DB releases exists
				// Should get releases from both DB and config, getting only latest of 4.15 releases from db.
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("3.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("3.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("4.15"),
					onlyLatest:      swag.Bool(true),
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// Release sources exist
				// Configuration releases exists
				// Sufficient OS images
				// No releases to ignore
				// version pattern is set to '4.15', only latest is set
				// DB releases exists
				// Should get releases from both DB and config, getting only latest of 4.14 releases from db.
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{},
					versionPattern:  swag.String("4.14"),
					onlyLatest:      swag.Bool(true),
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},
			}

			testListOpenshiftVerionsWithParams(tests, db, dbName)
		})

		It("Filter by ignored releases different scenrios", func() {
			tests := []listOpenshiftVersionsTestParams{

				// Release sources exist
				// Configuration releases exists
				// Sufficient OS images
				// Releases to ignore exists
				// no query parameters
				// DB releases exists
				// Should get releases from both DB and config, ignoring some db releases.
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{"3.14.2", "4.14.3", "4.13.3", "4.15.2", "4.14.1"},
					versionPattern:  nil,
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.14.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.13.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.13.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.0-ec.3": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.0-ec.3"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},

				// Release sources exist
				// Configuration releases exists
				// Sufficient OS images
				// Releases to ignore exists
				// No query parameters
				// DB releases exists
				// Should get releases from both DB and config, ignoring some db releases, some releases that should be ignored does not exist.
				{
					releaseSources: models.ReleaseSources{
						{
							OpenshiftVersion: swag.String("4.12"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.13"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.14"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
						{
							OpenshiftVersion: swag.String("4.15"),
							UpgradeChannels: []*models.UpgradeChannel{
								{
									CPUArchitecture: swag.String("x86_64"),
									Channels:        []string{"stable"},
								},
							},
						},
					},
					releaseImages: models.ReleaseImages{
						{
							OpenshiftVersion: swag.String("4.14.1"),
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
							URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
							Version:          swag.String("4.14.1"),
							Default:          false,
						},
					},
					osImages: models.OsImages{
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.13"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.0/rhcos-4.13.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("413.92.202307260246-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.14"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("414.92.202310170514-0"),
						},
						{
							CPUArchitecture:  swag.String(common.X86CPUArchitecture),
							OpenshiftVersion: swag.String("4.15"),
							URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.145/4.15.0/rhcos-4.15.0-x86_64-live.x86_64.iso"),
							Version:          swag.String("415.92.202310310037-0"),
						},
					},
					dbReleases: []common.ReleaseImage{
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.14.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.14.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.2", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.13.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.13.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.0-ec.3"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.3", common.X86CPUArchitecture)),
							},
						},
						{
							Channel: common.OpenshiftReleaseChannelStable,
							ReleaseImage: models.ReleaseImage{
								Version:         swag.String("4.15.2"),
								CPUArchitecture: swag.String(common.X86CPUArchitecture),
								SupportLevel:    models.OpenshiftVersionSupportLevelProduction,
								Default:         false,
								URL:             swag.String(common.GetURLForReleaseImageInSaaS("4.15.2", common.X86CPUArchitecture)),
							},
						},
					},
					ignoredReleases: []string{"3.14.2", "4.14.2", "4.14.3", "4.13.2", "4.13.3", "4.17.2", "4.15.0-ec.3"},
					versionPattern:  nil,
					onlyLatest:      nil,
					expectedPayload: models.OpenshiftVersions{
						"4.14.1": models.OpenshiftVersion{
							DisplayName:      swag.String("4.14.1"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
						"4.15.2": models.OpenshiftVersion{
							DisplayName:      swag.String("4.15.2"),
							SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelProduction),
							CPUArchitectures: []string{common.X86CPUArchitecture},
							Default:          false,
						},
					},
				},
			}

			testListOpenshiftVerionsWithParams(tests, db, dbName)
		})
	})
})
