package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

var _ = Describe("downloadBootArtifactsCmd.GetSteps", func() {
	var (
		ctx            = context.Background()
		ctrl           *gomock.Controller
		host           models.Host
		db             *gorm.DB
		downloadCmd    *downloadBootArtifactsCmd
		mockVersions   *versions.MockHandler
		id             strfmt.UUID
		infraEnvID     strfmt.UUID
		dbName         string
		imgSvcURL      string
		hostFSMountDir string
	)

	BeforeEach(func() {
		imgSvcURL = common.TestDefaultConfig.BaseDNSDomain
		db, dbName = common.PrepareTestDB()
		hostFSMountDir = "/host"
		ctrl = gomock.NewController(GinkgoT())

		mockVersions = versions.NewMockHandler(ctrl)
		id = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostWithInfraEnv(id, infraEnvID, models.HostStatusInsufficient, models.HostRoleWorker)
		host.Inventory = hostutil.GenerateMasterInventory()
		Expect(db.Create(&host).Error).To(BeNil())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("returns a request with the correct content", func() {
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
		infraEnv.CPUArchitecture = *common.TestDefaultConfig.OsImage.CPUArchitecture
		infraEnv.OpenshiftVersion = *common.TestDefaultConfig.OsImage.OpenshiftVersion
		Expect(db.Create(infraEnv).Error).To(BeNil())
		downloadCmd = NewDownloadBootArtifactsCmd(common.GetTestLog(), imgSvcURL, auth.TypeNone, mockVersions, db, time.Duration(9000), hostFSMountDir)
		mockVersions.EXPECT().GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture).Return(common.TestDefaultConfig.OsImage, nil).Times(1)

		initrdUrl := fmt.Sprintf("%s/images/%s/pxe-initrd?arch=%s&version=%s", imgSvcURL, infraEnvID, infraEnv.CPUArchitecture, infraEnv.OpenshiftVersion)
		rootfsUrl := fmt.Sprintf("%s/boot-artifacts/rootfs?arch=%s&version=%s", imgSvcURL, infraEnv.CPUArchitecture, infraEnv.OpenshiftVersion)
		kernelUrl := fmt.Sprintf("%s/boot-artifacts/kernel?arch=%s&version=%s", imgSvcURL, infraEnv.CPUArchitecture, infraEnv.OpenshiftVersion)

		stepReply, stepErr := downloadCmd.GetSteps(ctx, &host)
		Expect(stepErr).To(BeNil())
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply).To(HaveLen(1))
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeDownloadBootArtifacts))
		Expect(stepReply[0].Args).To(HaveLen(1))

		var req models.DownloadBootArtifactsRequest
		Expect(json.Unmarshal([]byte(stepReply[0].Args[0]), &req)).To(Succeed())
		Expect(*req.InitrdURL).To(Equal(initrdUrl))
		Expect(*req.RootfsURL).To(Equal(rootfsUrl))
		Expect(*req.KernelURL).To(Equal(kernelUrl))
		Expect(*req.HostFsMountDir).To(Equal(hostFSMountDir))
	})

	It("fails when the host's infra-env doesn't exist", func() {
		downloadCmd = NewDownloadBootArtifactsCmd(common.GetTestLog(), imgSvcURL, auth.TypeNone, mockVersions, db, time.Duration(9000), hostFSMountDir)
		_, stepErr := downloadCmd.GetSteps(ctx, &host)
		Expect(stepErr).ToNot(BeNil())
	})

	It("fails when an infra-env specifies an invalid openshift version", func() {
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
		infraEnv.CPUArchitecture = *common.TestDefaultConfig.OsImage.CPUArchitecture
		infraEnv.OpenshiftVersion = "5.10"
		Expect(db.Create(infraEnv).Error).To(BeNil())
		downloadCmd = NewDownloadBootArtifactsCmd(common.GetTestLog(), imgSvcURL, auth.TypeNone, mockVersions, db, time.Duration(9000), hostFSMountDir)
		versionsErr := errors.Errorf("The requested OS image for version (%s) and CPU architecture (%s) isn't specified in OS images list", infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
		mockVersions.EXPECT().GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture).Return(nil, versionsErr).Times(1)
		_, stepErr := downloadCmd.GetSteps(ctx, &host)
		Expect(stepErr).ToNot(BeNil())
	})

	It("fails when an infra-env specifies an invalid cpu arch", func() {
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
		infraEnv.CPUArchitecture = "x866"
		Expect(db.Create(infraEnv).Error).To(BeNil())
		downloadCmd = NewDownloadBootArtifactsCmd(common.GetTestLog(), imgSvcURL, auth.TypeNone, mockVersions, db, time.Duration(9000), hostFSMountDir)
		versionsErr := errors.Errorf("No OS images are available")
		mockVersions.EXPECT().GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture).Return(nil, versionsErr).Times(1)
		_, stepErr := downloadCmd.GetSteps(ctx, &host)
		Expect(stepErr).ToNot(BeNil())
	})

	It("fails when an infra-env specifies no openshift version or cpu arch", func() {
		infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
		Expect(db.Create(infraEnv).Error).To(BeNil())
		downloadCmd = NewDownloadBootArtifactsCmd(common.GetTestLog(), imgSvcURL, auth.TypeNone, mockVersions, db, time.Duration(9000), hostFSMountDir)
		versionsErr := errors.Errorf("No OS images are available")
		mockVersions.EXPECT().GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture).Return(nil, versionsErr).Times(1)
		_, stepErr := downloadCmd.GetSteps(ctx, &host)
		Expect(stepErr).ToNot(BeNil())
	})
})
