package hostcommands

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

const (
	defaultImageAvailabilityTimeoutSeconds = 60 * 30
)

var _ = Describe("container_image_availability_cmd", func() {
	var (
		ctx                       = context.Background()
		host                      models.Host
		cluster                   common.Cluster
		db                        *gorm.DB
		cmd                       *imageAvailabilityCmd
		id, clusterID, infraEnvID strfmt.UUID
		dbName                    string
		ctrl                      *gomock.Controller
		mockRelease               *oc.MockRelease
		mockVersions              *versions.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)

		db, dbName = common.PrepareTestDB()
		cmd = NewImageAvailabilityCmd(common.GetTestLog(), db, mockRelease, mockVersions, DefaultInstructionConfig, defaultImageAvailabilityTimeoutSeconds)

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			OcpReleaseImage:  common.TestDefaultConfig.ReleaseImageUrl,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), cluster.OcpReleaseImage, gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(step).NotTo(BeNil())

		defaultReleaseImage := common.TestDefaultConfig.ReleaseImageUrl
		request := &models.ContainerImageAvailabilityRequest{
			Images:  []string{defaultReleaseImage, defaultMCOImage, ocpMustGatherImage, cmd.instructionConfig.InstallerImage},
			Timeout: defaultImageAvailabilityTimeoutSeconds,
		}

		b, err := json.Marshal(&request)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(strings.Join(step[0].Args, " ")).To(ContainSubstring(string(b)))
	})

	It("get_step_release_image_failure", func() {
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), cluster.OcpReleaseImage, gomock.Any()).Return(nil, errors.New("err")).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).To(HaveOccurred())
		Expect(step).To(BeNil())
	})

	It("get_step_get_mco_failure", func() {
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), cluster.OcpReleaseImage, gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("err")).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).To(HaveOccurred())
		Expect(step).To(BeNil())
	})

	It("get_step_get_must_gather_failure", func() {
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), cluster.OcpReleaseImage, gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("err")).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).To(HaveOccurred())
		Expect(step).To(BeNil())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("get images", func() {
	var (
		db           *gorm.DB
		cmd          *imageAvailabilityCmd
		cluster      *common.Cluster
		ctrl         *gomock.Controller
		mockRelease  *oc.MockRelease
		mockVersions *versions.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		db = &gorm.DB{}
		cluster = &common.Cluster{}
		cmd = NewImageAvailabilityCmd(common.GetTestLog(), db, mockRelease, mockVersions, DefaultInstructionConfig, defaultImageAvailabilityTimeoutSeconds)
	})

	It("get_step_get_all_images", func() {
		cluster.OcpReleaseImage = common.TestDefaultConfig.ReleaseImageUrl
		mco := "image-mco"
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), cluster.OcpReleaseImage, gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mco, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).Times(1)
		release := common.TestDefaultConfig.ReleaseImageUrl
		expected := []string{release, mco, defaultMustGatherVersion["ocp"]}
		images, err := cmd.getImages(context.Background(), cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(images).To(Equal(expected))
	})

	It("uses mirrored release image from cluster OcpReleaseImage field", func() {
		// Setup cluster with mirrored release image URL
		mirroredReleaseImageURL := "registry.mirror.example.com:5000/openshift-release-dev/ocp-release:4.20.8-x86_64"
		cluster.OcpReleaseImage = mirroredReleaseImageURL
		cluster.OpenshiftVersion = "4.20.8"
		cluster.CPUArchitecture = "x86_64"
		cluster.PullSecret = "test-pull-secret"

		// Mock the release image with mirrored URL
		mirroredReleaseImage := &models.ReleaseImage{
			URL:              &mirroredReleaseImageURL,
			Version:          &cluster.OpenshiftVersion,
			CPUArchitecture:  &cluster.CPUArchitecture,
			OpenshiftVersion: &cluster.OpenshiftVersion,
		}

		mcoImage := "registry.mirror.example.com:5000/openshift-release-dev/ocp-v4.0-art-dev@sha256:abc123"

		// Expect GetReleaseImageByURL to be called with the cluster's OcpReleaseImage
		mockVersions.EXPECT().GetReleaseImageByURL(gomock.Any(), mirroredReleaseImageURL, cluster.PullSecret).Return(mirroredReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), mirroredReleaseImageURL, gomock.Any(), cluster.PullSecret).Return(mcoImage, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret).Return(defaultMustGatherVersion, nil).Times(1)

		images, err := cmd.getImages(context.Background(), cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(images).To(HaveLen(3))
		// Verify that the release image uses the mirrored URL, not upstream quay.io
		Expect(images[0]).To(Equal(mirroredReleaseImageURL))
		Expect(images[0]).To(ContainSubstring("registry.mirror.example.com"))
		Expect(images[0]).NotTo(ContainSubstring("quay.io"))
	})
})
