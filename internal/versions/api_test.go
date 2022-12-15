package versions

import (
	"context"
	"encoding/json"
	"errors"
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

var _ = Describe("ListComponentVersions", func() {
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

var _ = Describe("ListSupportedOpenshiftVersions", func() {
	var (
		db           *gorm.DB
		dbName       string
		ctrl         *gomock.Controller
		mockRelease  *oc.MockRelease
		versions     Versions
		authzHandler auth.Authorizer

		logger          = common.GetTestLog()
		cpuArchitecture = common.TestDefaultConfig.CPUArchitecture
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())

		mockRelease = oc.NewMockRelease(ctrl)

		cfg := auth.GetConfigRHSSO()
		authzHandler = auth.NewAuthzHandler(cfg, nil, logger, db)
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

		versionsHandler, err := NewHandler(logger, mockRelease, osImages, *releaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())
		return versionsHandler
	}

	It("get_defaults from data directory", func() {
		osImages := readDefaultOsImages()
		versionsHandler := readDefaultReleaseImages(osImages)

		h := NewAPIHandler(logger, versions, authzHandler, versionsHandler, osImages)
		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		defaultExists := false

		for _, releaseImage := range versionsHandler.releaseImages {
			key := *releaseImage.OpenshiftVersion
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
				Expect(version.SupportLevel).Should(Equal(getSupportLevel(*releaseImage)))
			} else {
				// For multi-arch release we don't require a strict matching for every
				// architecture supported by this image. As long as we have at least one OS
				// image that matches, we are okay. This is to allow setups where release
				// image supports more architectures than we have available RHCOS images.
				Expect(len(version.CPUArchitectures)).ShouldNot(Equal(0))
				Expect(*releaseImage.Version).Should(ContainSubstring(*version.DisplayName))
				Expect(version.SupportLevel).Should(Equal(getSupportLevel(*releaseImage)))
			}
		}

		// We want to make sure after parsing the default file, there is always a release
		// image marked as default. It is not desired to have a service without anything
		// to default to.
		Expect(defaultExists).Should(Equal(true))
	})

	It("getSupportLevel", func() {
		releaseImage := models.ReleaseImage{
			CPUArchitecture:  &cpuArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			URL:              &common.TestDefaultConfig.ReleaseImageUrl,
			Version:          &common.TestDefaultConfig.ReleaseVersion,
		}

		// Production release version
		releaseImage.Version = swag.String("4.8.12")
		Expect(*getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))

		// Beta release version
		releaseImage.Version = swag.String("4.9.0-rc.4")
		Expect(*getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelBeta))

		// Support level specified in release image
		releaseImage.SupportLevel = models.OpenshiftVersionSupportLevelProduction
		Expect(*getSupportLevel(releaseImage)).Should(Equal(models.OpenshiftVersionSupportLevelProduction))
	})

	It("missing release images", func() {
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(logger, mockRelease, osImages, models.ReleaseImages{}, nil, "", nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, versionsHandler, osImages)
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
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
		}

		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(logger, mockRelease, osImages, releaseImages, nil, "", nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, versionsHandler, osImages)

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
		Expect(version.Default).Should(Equal(true))
	})

	It("release image without matching OS image", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.PowerCPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          true,
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
		}
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(logger, mockRelease, osImages, releaseImages, nil, "", nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, versionsHandler, osImages)

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(version.CPUArchitectures).ShouldNot(ContainElement(common.PowerCPUArchitecture))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
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
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          true,
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
		}
		osImages := readDefaultOsImages()
		versionsHandler, err := NewHandler(logger, mockRelease, osImages, releaseImages, nil, "", nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, versionsHandler, osImages)

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)

		version := val.Payload["4.11.1"]
		Expect(version.CPUArchitectures).Should(ContainElement(common.ARM64CPUArchitecture))
		Expect(version.CPUArchitectures).Should(ContainElement(common.X86CPUArchitecture))
		Expect(len(version.CPUArchitectures)).Should(Equal(2))
		Expect(version.DisplayName).Should(Equal(swag.String("4.11.1-chocobomb-for-test")))
		Expect(version.Default).Should(Equal(true))
	})
})

var _ = Describe("Test list versions with capability restrictions", func() {
	var (
		db            *gorm.DB
		dbName        string
		ctrl          *gomock.Controller
		mockOcmAuthz  *ocm.MockOCMAuthorization
		mockOcmClient *ocm.Client
		authCtx       context.Context
		orgID1        = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
		userName1     = "test_user_1"
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)

		payload := &ocm.AuthPayload{
			Username:     userName1,
			Organization: orgID1,
			Role:         ocm.UserRole,
		}
		authCtx = context.WithValue(context.Background(), restapi.AuthKey, payload)
		mockOcmClient = &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	handlerWithAuthConfig := func(enableOrgBasedFeatureGates bool) restapi.VersionsAPI {
		cfg := auth.GetConfigRHSSO()
		cfg.EnableOrgBasedFeatureGates = enableOrgBasedFeatureGates
		authzHandler := auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog(), db)

		osImages, err := NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())
		versionsHandler, err := NewHandler(common.GetTestLog(), nil, osImages, defaultReleaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())

		return NewAPIHandler(common.GetTestLog(), Versions{}, authzHandler, versionsHandler, osImages)
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

	Context("V2ListSupportedOpenshiftVersions", func() {
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
		It("does not return multiarch when capability query fails", func() {
			h := handlerWithAuthConfig(true)

			mockOcmAuthz.EXPECT().CapabilityReview(context.Background(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, errors.New("failed to query capability")).Times(1)
			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(hasMultiarch(val.Payload)).To(BeFalse())
		})
		It("returns multiarch with org-based features disabled", func() {
			h := handlerWithAuthConfig(false)

			reply := h.V2ListSupportedOpenshiftVersions(authCtx, operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))

			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(hasMultiarch(val.Payload)).To(BeTrue())
		})
	})
})
