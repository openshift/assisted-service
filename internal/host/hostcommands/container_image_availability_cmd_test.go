package hostcommands

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/alessio/shellescape"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
)

const (
	defaultImageAvailabilityTimeoutSeconds = 60 * 30
)

var _ = Describe("container_image_availability_cmd", func() {
	var (
		ctx           = context.Background()
		host          models.Host
		cluster       common.Cluster
		db            *gorm.DB
		cmd           *imageAvailabilityCmd
		id, clusterID strfmt.UUID
		dbName        string
		ctrl          *gomock.Controller
		mockRelease   *oc.MockRelease
		mockVersions  *versions.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)

		db, dbName = common.PrepareTestDB()
		cmd = NewImageAvailabilityCmd(common.GetTestLog(), db, mockRelease, mockVersions, DefaultInstructionConfig, defaultImageAvailabilityTimeoutSeconds)

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostAddedToCluster(id, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(defaultReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherImage, nil).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(step).NotTo(BeNil())

		request := &models.ContainerImageAvailabilityRequest{
			Images:  []string{cmd.instructionConfig.InstallerImage, defaultMCOImage, defaultMustGatherImage},
			Timeout: defaultImageAvailabilityTimeoutSeconds,
		}

		b, err := json.Marshal(&request)
		Expect(err).ShouldNot(HaveOccurred())

		verifyArgInCommand(step[0].Args[1], "--request", shellescape.QuoteCommand([]string{string(b)}), 1)
	})

	It("get_step_release_image_failure", func() {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return("", errors.New("err")).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).To(HaveOccurred())
		Expect(step).To(BeNil())
	})

	It("get_step_get_mco_failure", func() {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(defaultReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("err")).Times(1)

		step, err := cmd.GetSteps(ctx, &host)
		Expect(err).To(HaveOccurred())
		Expect(step).To(BeNil())
	})

	It("get_step_get_must_gather_failure", func() {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(defaultReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("err")).Times(1)

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

	It("get_step_get_must_gather_failure", func() {
		release := "image-rel"
		mco := "image-mco"
		mg := "image-must-gather"
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(release, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mco, nil).Times(1)
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(mg, nil).Times(1)
		expected := Images{
			ReleaseImage:    release,
			MCOImage:        mco,
			MustGatherImage: mg,
		}
		images, err := cmd.getImages(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(images).To(Equal(expected))
	})
})
