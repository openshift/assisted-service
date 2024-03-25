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
			versionsHandler: &restAPIVersionsHandler{},
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
		mockRelease   *oc.MockRelease
		versions      Versions
		authzHandler  auth.Authorizer
		logger        = common.GetTestLog()
		enableKubeAPI = false
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

	It("Should read defaults from data directory successfully", func() {
		bytes, err := os.ReadFile("../../data/default_os_images.json")
		Expect(err).ShouldNot(HaveOccurred())

		osImages := models.OsImages{}
		err = json.Unmarshal(bytes, &osImages)
		Expect(err).ShouldNot(HaveOccurred())

		// validate fields
		_, err = NewOSImages(osImages)
		Expect(err).ShouldNot(HaveOccurred())

		bytes, err = os.ReadFile("../../data/default_release_images.json")
		Expect(err).ShouldNot(HaveOccurred())

		releaseImages := &models.ReleaseImages{}
		err = json.Unmarshal(bytes, releaseImages)
		Expect(err).ShouldNot(HaveOccurred())

		// validate fields
		_, err = NewHandler(logger, mockRelease, *releaseImages, nil, "", nil, nil, nil, enableKubeAPI, nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("Should not cause an error with no release images in the DB", func() {
		handler, err := NewHandler(logger, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
		Expect(err).ShouldNot(HaveOccurred())

		apiHandler := NewAPIHandler(logger, versions, authzHandler, handler, nil, nil)

		reply := apiHandler.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		Expect(val.Payload).Should(BeEmpty())
	})

	It("Should retrieve only release images with matching OS image", func() {
		releaseImages := models.ReleaseImages{
			{

				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.14"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.11-x86_64"),
				Version:          swag.String("4.14.11"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{

				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.14"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.11-aarch64"),
				Version:          swag.String("4.14.11"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}
		err := db.Create(&releaseImages).Error
		Expect(err).ToNot(HaveOccurred())

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.14"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.14/4.14.0/rhcos-4.14.0-x86_64-live.x86_64.iso"),
				Version:          swag.String("414.92.202310170514-0"),
			},
		}

		expectedPayload := models.OpenshiftVersions{
			"4.14.11": {
				DisplayName:      swag.String("4.14.11"),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
				Default:          false,
			},
		}

		handler, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, handler, osImages, nil)

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		Expect(val.Payload).To(Equal(expectedPayload))
	})

	It("Should have two different keys for single-arch and multi-arch of the same version", func() {
		releaseImages := models.ReleaseImages{
			// Those images provide the same architecture using single-arch as well as multi-arch
			// release images. This is to test if in this scenario we don't return duplicated
			// entries in the supported architectures list.
			{

				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{

				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				CPUArchitectures: []string{common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-aarch64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11-multi"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-multi"),
				Version:          swag.String("4.11.1-multi"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}
		err := db.Create(&releaseImages).Error
		Expect(err).ToNot(HaveOccurred())

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-aarch64-live.aarch64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
		}
		expectedPayload := models.OpenshiftVersions{
			"4.11.1": {
				DisplayName:      swag.String("4.11.1"),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
				Default:          false,
			},
			"4.11.1-multi": {
				DisplayName:      swag.String("4.11.1-multi"),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
				Default:          false,
			},
		}

		handler, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, handler, osImages, nil)

		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		Expect(val.Payload).To(Equal(expectedPayload))

	})

	It("Should append multi suffix for multi-arch image automatically", func() {
		releaseImages := models.ReleaseImages{
			{
				// This image definition does not contain "-multi" suffix anywhere because CVO for it
				// does not return it. Nevertheless in the UI we want to display it, so we test for
				// autoappend.
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-multi"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}
		err := db.Create(&releaseImages).Error
		Expect(err).ToNot(HaveOccurred())

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/aarch64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-aarch64-live.aarch64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
		}
		expectedPayload := models.OpenshiftVersions{
			"4.11.1-multi": {
				DisplayName:      swag.String("4.11.1-multi"),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
				Default:          false,
			},
		}

		handler, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
		Expect(err).ToNot(HaveOccurred())
		h := NewAPIHandler(logger, versions, authzHandler, handler, osImages, nil)
		reply := h.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
		Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
		val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
		Expect(val.Payload).To(Equal(expectedPayload))
	})

	Context("Test filter by version_pattern query parameter", func() {
		releaseImages := models.ReleaseImages{
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
				Version:          swag.String("4.11.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-x86_64"),
				Version:          swag.String("4.12.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.111"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11121-x86_64"),
				Version:          swag.String("4.111.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.12"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.12/4.12.48/rhcos-4.12.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("412.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.111"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.111/4.111.48/rhcos-4.111.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("4111.86.202308081056-0"),
			},
		}

		var handler restapi.VersionsAPI

		BeforeEach(func() {
			err := db.Create(&releaseImages).Error
			Expect(err).ToNot(HaveOccurred())
			h, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler = NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)
		})

		It("Should return no results when nothing matches", func() {
			expectedPayload := models.OpenshiftVersions{}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{Version: swag.String("4.10")},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should return all results when everything matches", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.1": {
					DisplayName:      swag.String("4.11.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.2": {
					DisplayName:      swag.String("4.12.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.111.2": {
					DisplayName:      swag.String("4.111.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{Version: swag.String("4.")},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should be ignored when is nil", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.1": {
					DisplayName:      swag.String("4.11.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.2": {
					DisplayName:      swag.String("4.12.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.111.2": {
					DisplayName:      swag.String("4.111.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should filter out non-matching release images", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.1": {
					DisplayName:      swag.String("4.11.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{Version: swag.String("4.11.")},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})
	})

	Context("Filter by only_latest query parameter", func() {
		releaseImages := models.ReleaseImages{
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
				Version:          swag.String("4.11.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.1-x86_64"),
				Version:          swag.String("4.12.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-x86_64"),
				Version:          swag.String("4.12.2"),
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.1-x86_64"),
				Version:          swag.String("4.13.1"),
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.2-x86_64"),
				Version:          swag.String("4.13.2"),
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
			},
		}

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.12"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.12/4.12.48/rhcos-4.12.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("412.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.48/rhcos-4.13.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("413.86.202308081056-0"),
			},
		}

		var handler restapi.VersionsAPI

		BeforeEach(func() {
			err := db.Create(&releaseImages).Error
			Expect(err).ToNot(HaveOccurred())

			h, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler = NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)
		})

		It("Should be ignored when is nil", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.1": {
					DisplayName:      swag.String("4.11.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.1": {
					DisplayName:      swag.String("4.12.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.2": {
					DisplayName:      swag.String("4.12.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelBeta),
					Default:          false,
				},
				"4.13.1": {
					DisplayName:      swag.String("4.13.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelBeta),
					Default:          false,
				},
				"4.13.2": {
					DisplayName:      swag.String("4.13.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelBeta),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should return only the latest (stable) for each minor when set to true", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.1": {
					DisplayName:      swag.String("4.12.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.13.2": {
					DisplayName:      swag.String("4.13.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.OpenshiftVersionSupportLevelBeta),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{OnlyLatest: swag.Bool(true)},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})
	})

	Context("Filter by both version_pattern and only_latest parameters", func() {
		releaseImages := models.ReleaseImages{
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
				Version:          swag.String("4.11.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.1-x86_64"),
				Version:          swag.String("4.12.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.2-x86_64"),
				Version:          swag.String("4.12.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.1-x86_64"),
				Version:          swag.String("4.13.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.12"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.12/4.12.48/rhcos-4.12.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("412.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.48/rhcos-4.13.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("413.86.202308081056-0"),
			},
		}

		var handler restapi.VersionsAPI

		BeforeEach(func() {
			err := db.Create(&releaseImages).Error
			Expect(err).ToNot(HaveOccurred())
			h, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler = NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)
		})

		It("Should get the latest 4.12 versions successfully", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.12.2": {
					DisplayName:      swag.String("4.12.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{
					Version: swag.String("4.12"), OnlyLatest: swag.Bool(true),
				},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should get the latest 4 versions successfully", func() {
			expectedPayload := models.OpenshiftVersions{
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.2": {
					DisplayName:      swag.String("4.12.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.13.1": {
					DisplayName:      swag.String("4.13.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{
					Version: swag.String("4"), OnlyLatest: swag.Bool(true),
				},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Should get the latest 4.14 versions successfully", func() {
			expectedPayload := models.OpenshiftVersions{}

			reply := handler.V2ListSupportedOpenshiftVersions(
				context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{
					Version: swag.String("4.14"), OnlyLatest: swag.Bool(true),
				},
			)
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})
	})

	Context("Test ignoring versions", func() {
		releaseImages := models.ReleaseImages{
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
				Version:          swag.String("4.11.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.11"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
				Version:          swag.String("4.11.2"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
			{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12.1-x86_64"),
				Version:          swag.String("4.12.1"),
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
			},
		}

		osImages := osImageList{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.11/4.11.48/rhcos-4.11.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("411.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.12"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.12/4.12.48/rhcos-4.12.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("412.86.202308081056-0"),
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/4.13/4.13.48/rhcos-4.13.48-x86_64-live.x86_64.iso"),
				Version:          swag.String("413.86.202308081056-0"),
			},
		}

		BeforeEach(func() {
			err := db.Create(&releaseImages).Error
			Expect(err).ToNot(HaveOccurred())
		})

		It("Ignore all versions should be successful", func() {
			ignoredVersions := []string{"4.11.1", "4.11.2", "4.12.1"}
			expectedPayload := models.OpenshiftVersions{}

			h, err := NewHandler(nil, nil, nil, nil, "", nil, ignoredVersions, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler := NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)

			reply := handler.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Ignore some off the versions should be successful", func() {
			ignoredVersions := []string{"4.11.1", "4.12.1"}
			expectedPayload := models.OpenshiftVersions{
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			h, err := NewHandler(nil, nil, nil, nil, "", nil, ignoredVersions, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler := NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)

			reply := handler.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})

		It("Ignore none of the versions should be successful", func() {
			ignoredVersions := []string{"4.13.1", "4.13.2"}
			expectedPayload := models.OpenshiftVersions{
				"4.11.1": {
					DisplayName:      swag.String("4.11.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.11.2": {
					DisplayName:      swag.String("4.11.2"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
				"4.12.1": {
					DisplayName:      swag.String("4.12.1"),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					SupportLevel:     swag.String(models.ReleaseImageSupportLevelProduction),
					Default:          false,
				},
			}

			h, err := NewHandler(nil, nil, nil, nil, "", nil, ignoredVersions, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())
			handler := NewAPIHandler(logger, versions, authzHandler, h, osImages, nil)

			reply := handler.V2ListSupportedOpenshiftVersions(context.Background(), operations.V2ListSupportedOpenshiftVersionsParams{})
			Expect(reply).Should(BeAssignableToTypeOf(operations.NewV2ListSupportedOpenshiftVersionsOK()))
			val, _ := reply.(*operations.V2ListSupportedOpenshiftVersionsOK)
			Expect(val.Payload).To(Equal(expectedPayload))
		})
	})

	Context("Test list versions with capability restrictions", func() {
		var (
			mockOcmAuthz  *ocm.MockOCMAuthorization
			mockOcmClient *ocm.Client
			authCtx       context.Context
			orgID1        = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
			userName1     = "test_user_1"
		)

		BeforeEach(func() {
			mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
			payload := &ocm.AuthPayload{
				Username:     userName1,
				Organization: orgID1,
				Role:         ocm.UserRole,
			}
			authCtx = context.WithValue(context.Background(), restapi.AuthKey, payload)
			mockOcmClient = &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		})

		handlerWithAuthConfig := func(enableOrgBasedFeatureGates bool) restapi.VersionsAPI {
			cfg := auth.GetConfigRHSSO()
			cfg.EnableOrgBasedFeatureGates = enableOrgBasedFeatureGates
			authzHandler := auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog(), db)

			osImages, err := NewOSImages(defaultOsImages)
			Expect(err).ShouldNot(HaveOccurred())

			dbReleaseImages := models.ReleaseImages{
				{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
					OpenshiftVersion: swag.String("4.11"),
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-multi"),
					Version:          swag.String("4.11.1"),
					SupportLevel:     models.ReleaseImageSupportLevelProduction,
				},
			}
			err = db.Create(&dbReleaseImages).Error
			Expect(err).ToNot(HaveOccurred())

			h, err := NewHandler(nil, nil, nil, nil, "", nil, nil, db, enableKubeAPI, nil)
			Expect(err).ToNot(HaveOccurred())

			return NewAPIHandler(common.GetTestLog(), Versions{}, authzHandler, h, osImages, nil)
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

var _ = Describe("V2ListReleaseSources", func() {

	var (
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("Test success with non-empty release sources", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion: swag.String("4.14"),
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
					},
				},
			},
			{
				OpenshiftVersion: swag.String("4.15"),
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
					},
				},
			},
		}

		apiHandler := NewAPIHandler(nil, Versions{}, nil, nil, nil, releaseSources)

		middlewareResponder := apiHandler.V2ListReleaseSources(
			context.Background(),
			operations.V2ListReleaseSourcesParams{},
		)
		Expect(middlewareResponder).Should(BeAssignableToTypeOf(operations.NewV2ListReleaseSourcesOK()))

		reply, ok := middlewareResponder.(*operations.V2ListReleaseSourcesOK)
		Expect(ok).To(BeTrue())

		payload := reply.Payload
		Expect(payload).To(Equal(releaseSources))
	})

	It("Test success with empty release sources", func() {
		releaseSources := models.ReleaseSources{}
		apiHandler := NewAPIHandler(nil, Versions{}, nil, nil, nil, releaseSources)

		middlewareResponder := apiHandler.V2ListReleaseSources(
			context.Background(),
			operations.V2ListReleaseSourcesParams{},
		)
		Expect(middlewareResponder).Should(BeAssignableToTypeOf(operations.NewV2ListReleaseSourcesOK()))

		reply, ok := middlewareResponder.(*operations.V2ListReleaseSourcesOK)
		Expect(ok).To(BeTrue())

		payload := reply.Payload
		Expect(payload).To(Equal(releaseSources))
	})
})
