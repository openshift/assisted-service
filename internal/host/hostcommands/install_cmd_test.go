package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	defaultMCOImage    = "mcoImage"
	ocpMustGatherImage = "mustGatherImage"
)

var defaultMustGatherVersion = versions.MustGatherVersion{
	"ocp": ocpMustGatherImage,
}

var DefaultInstructionConfig = InstructionConfig{
	InstallerImage:     "quay.io/ocpmetal/assisted-installer:latest",
	ControllerImage:    "quay.io/ocpmetal/assisted-installer-controller:latest",
	AgentImage:         "quay.io/ocpmetal/assisted-installer-agent:latest",
	ReleaseImageMirror: "local.registry:5000/ocp@sha256:eab93b4591699a5a4ff50ad3517892653f04fb840127895bb3609b3cc68f98f3",
}

var _ = Describe("installcmd", func() {
	var (
		ctx               = context.Background()
		host              models.Host
		cluster           common.Cluster
		db                *gorm.DB
		installCmd        *installCmd
		clusterId         strfmt.UUID
		infraEnvId        strfmt.UUID
		infraEnv          common.InfraEnv
		installCmdSteps   []*models.Step
		stepErr           error
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		mockRelease       *oc.MockRelease
		instructionConfig InstructionConfig
		dbName            string
		mockEvents        *eventsapi.MockHandler
		mockVersions      *versions.MockHandler
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instructionConfig = DefaultInstructionConfig
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		installCmd = NewInstallCmd(common.GetTestLog(), db, mockValidator, mockRelease, instructionConfig, mockEvents, mockVersions)
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeFull)
		clusterId = *cluster.ID
		infraEnv = createInfraEnvInDb(db, clusterId)
		infraEnvId = *infraEnv.ID
		host = createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, false, "")
	})

	mockGetReleaseImage := func(times int) {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(times)
	}

	mockImages := func(times int) {
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(times)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).Times(times)
	}

	Context("negative", func() {
		It("get_step_one_master", func() {
			mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("").Times(1)
		})

		It("get_step_one_master_no_mco_image", func() {
			mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
			mockGetReleaseImage(1)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("error")).Times(1)
		})

		AfterEach(func() {
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			Expect(installCmdSteps).To(BeNil())
			postvalidation(true, true, nil, stepErr, "")
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(BeEmpty())
		})
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		mockGetReleaseImage(1)
		mockImages(1)
		installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	It("get_step_three_master_success", func() {
		host2 := createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, false, "")
		host3 := createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, true, "some_hostname")
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(3)
		mockGetReleaseImage(3)
		mockImages(3)
		installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host2)
		postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host2.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host3)
		postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleBootstrap)
		validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleBootstrap, infraEnvId, clusterId, *host3.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
	})
	It("invalid_inventory", func() {
		host.Inventory = "blah"
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(true, true, nil, stepErr, "")
	})

	Context("Bootable disk formatting", func() {
		createDisk := func(name string, bootable bool) *models.Disk {
			return &models.Disk{DriveType: models.DriveTypeHDD, ID: fmt.Sprintf("/dev/disk/by-id/wwn-%s", name), Name: name,
				SizeBytes: int64(128849018880), Bootable: bootable}
		}

		createRemovableDisk := func(name string, removable bool) *models.Disk {
			disk := createDisk(name, true)
			disk.Removable = removable
			return disk
		}

		getInventory := func(disks []*models.Disk) string {
			inventory := models.Inventory{
				Disks: disks,
			}
			b, err := json.Marshal(&inventory)
			Expect(err).To(Not(HaveOccurred()))
			return string(b)
		}

		mockFormatEvent := func(disk *models.Disk, times int) {
			eventStatusInfo := "%s: Performing quick format of disk %s(%s)"
			message := fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), disk.Name, disk.ID)
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.QuickDiskFormatPerformedEventName),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithMessageMatcher(message),
				eventstest.WithHostIdMatcher(host.ID.String()))).Times(times)
		}

		mockSkipFormatEvent := func(disk *models.Disk, times int) {
			eventStatusInfo := "%s: Skipping quick format of disk %s(%s) due to user request. This could lead to boot order issues during installation"
			message := fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), disk.Name, disk.ID)
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.QuickDiskFormatSkippedEventName),
				eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String()),
				eventstest.WithClusterIdMatcher(host.ClusterID.String()),
				eventstest.WithMessageMatcher(message),
				eventstest.WithHostIdMatcher(host.ID.String()))).Times(times)
		}

		prepareGetStep := func(disk *models.Disk) {
			mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(disk.ID)
			mockGetReleaseImage(1)
			mockImages(1)
		}

		sda := createDisk("sda", true)
		sdb := createDisk("sdb", false)
		sdh := createDisk("sdh", false)
		sdc := createDisk("sdc", true)

		It("Doesn't format removable disks", func() {
			disks := []*models.Disk{
				createRemovableDisk("sda", true), //removable disk
				sdb,                              //installation disk
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(disks[0], 0)
			prepareGetStep(sdb)
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			verifyDiskFormatCommand(installCmdSteps[0], disks[0].ID, false)
		})

		It("Formats a single removable disk", func() {
			disks := []*models.Disk{
				sdb, //installation disk
				sda, //bootable disk
				sdh, //non-bootable, non-installation
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(sda, 1)
			prepareGetStep(sdb)
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sdb.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(installCmdSteps[0], sda.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sdb.ID, false)
			verifyDiskFormatCommand(installCmdSteps[0], sdh.ID, false)
		})

		It("Formats installation disk in case it is bootable", func() {
			sddd := createDisk("sddd", true)
			disks := []*models.Disk{
				sddd, //installation disk
				sda,  //bootable disk
				sdh,  //non-bootable, non-installation
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(sda, 1)
			mockFormatEvent(sddd, 1)
			prepareGetStep(sddd)
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sddd.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(installCmdSteps[0], sda.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sddd.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sdh.ID, false)
		})

		It("Skip formatting installation disk in case SaveDiskPartitionsIsSet is set", func() {
			sddd := createDisk("sddd", true)
			disks := []*models.Disk{
				sddd, //installation disk
				sda,  //bootable disk
				sdh,  //non-bootable, non-installation
			}
			host.Inventory = getInventory(disks)
			host.InstallerArgs = `["--save-partindex","5","--copy-network"]`
			mockFormatEvent(sda, 1)
			mockSkipFormatEvent(sddd, 1)
			prepareGetStep(sddd)
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sddd.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(installCmdSteps[0], sda.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sddd.ID, false)
			verifyDiskFormatCommand(installCmdSteps[0], sdh.ID, false)
		})

		It("Format multiple bootable disks with some skipped", func() {
			sdi := createDisk("sdi", true)
			sdi.DriveType = models.DriveTypeFC
			sdg := createDisk("sdg", true)
			sdg.DriveType = models.DriveTypeISCSI
			sdd := createDisk("sdd", false)
			sdd.IsInstallationMedia = true
			sdj := createDisk("sdj", true)
			sdj.ByPath = "/dev/mmcblk1boot1"
			sdk := createDisk("sdk", true)
			sdk.DriveType = models.DriveTypeLVM
			sdt := createDisk("sdt", true)
			sdq := createDisk("sdq", true)
			sdf := createDisk("sdf", false)

			disks := []*models.Disk{
				sdb, //installation disk
				sdh, //non-bootable-disk
				sda, //bootable disk #1
				sdc, //bootable disk #2
				sdi, //skip bootable disk FC
				sdg, //skip bootable disk iSCSI
				sdf, //non-bootable disk
				sdd, //skip installation media
				sdj, //skip mmcblk device
				sdk, //skip bootable disk LVM
				sdt, //skip because user asked
				sdq, //skip because user asked
			}
			host.Inventory = getInventory(disks)
			host.SkipFormattingDisks = "/dev/disk/by-id/wwn-sdt,/dev/disk/by-id/wwn-sdq"
			mockFormatEvent(sda, 1)
			mockFormatEvent(sdc, 1)
			mockSkipFormatEvent(sdt, 1)
			mockSkipFormatEvent(sdq, 1)
			prepareGetStep(sdb)
			installCmdSteps, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, installCmdSteps[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, installCmdSteps[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sdb.ID, []string{sda.ID, sdc.ID}, models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(installCmdSteps[0], sda.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sdc.ID, true)
			verifyDiskFormatCommand(installCmdSteps[0], sdi.ID, false)
			verifyDiskFormatCommand(installCmdSteps[0], sdg.ID, false)
			verifyDiskFormatCommand(installCmdSteps[0], sdj.ID, false)
			verifyDiskFormatCommand(installCmdSteps[0], sdk.ID, false)
		})
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
		installCmdSteps = nil
		stepErr = nil
	})
})

var _ = Describe("installcmd arguments", func() {

	var (
		ctx          = context.Background()
		cluster      common.Cluster
		host         models.Host
		db           *gorm.DB
		validator    *hardware.MockValidator
		mockRelease  *oc.MockRelease
		dbName       string
		ctrl         *gomock.Controller
		mockEvents   *eventsapi.MockHandler
		mockVersions *versions.MockHandler
		infraEnvId   strfmt.UUID
		infraEnv     common.InfraEnv
	)

	mockImages := func() {
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).AnyTimes()
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).AnyTimes()
	}

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeNone)
		infraEnv = createInfraEnvInDb(db, *cluster.ID)
		infraEnvId = *infraEnv.ID
		host = createHostInDb(db, infraEnvId, *cluster.ID, models.HostRoleMaster, false, "")
		ctrl = gomock.NewController(GinkgoT())
		validator = hardware.NewMockValidator(ctrl)
		validator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).AnyTimes()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).AnyTimes()
		mockImages()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("configuration_params", func() {
		It("check_cluster_version_is_false_by_default", func() {
			config := &InstructionConfig{}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(swag.BoolValue(request.CheckCvo)).To(BeFalse())
		})

		It("check_cluster_version_is_set_to_false", func() {
			config := &InstructionConfig{
				CheckClusterVersion: false,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(swag.BoolValue(request.CheckCvo)).To(BeFalse())
		})

		It("check_cluster_version_is_set_to_true", func() {
			config := &InstructionConfig{
				CheckClusterVersion: true,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(swag.BoolValue(request.CheckCvo)).To(BeTrue())
		})

		It("verify high-availability-mode is None", func() {
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(*request.HighAvailabilityMode).To(Equal(models.ClusterHighAvailabilityModeNone))
		})

		It("verify empty value", func() {
			mockRelease = oc.NewMockRelease(ctrl)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
			mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).AnyTimes()

			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.McoImage).To(Equal(""))
		})

		It("no must-gather , mco and openshift version in day2 installation", func() {
			db.Model(&cluster).Update("kind", models.ClusterKindAddHostsCluster)
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.McoImage).To(BeEmpty())
			Expect(request.OpenshiftVersion).To(BeEmpty())
			Expect(request.MustGatherImage).To(BeEmpty())
		})
	})

	Context("installer args", func() {
		var (
			installCmd        *installCmd
			instructionConfig InstructionConfig
		)

		BeforeEach(func() {
			instructionConfig = DefaultInstructionConfig
			installCmd = NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, instructionConfig, mockEvents, mockVersions)
		})

		It("valid installer args", func() {
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(host.InstallerArgs))
			Expect(request.SkipInstallationDiskCleanup).To(BeFalse())
		})
		It("empty installer args", func() {
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(BeEmpty())
		})

		It("empty installer args with static ip config", func() {
			db.Model(&infraEnv).Update("static_network_config", "{'test': 'test'}")
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(`["--copy-network"]`))
		})

		It("non-empty installer args with static ip config", func() {
			db.Model(&infraEnv).Update("static_network_config", "{'test': 'test'}")
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(`["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`))
		})

		It("non-empty installer args with copy network with static ip config", func() {
			db.Model(&cluster).Update("image_static_network_config", "rkhkjgdfd")
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(host.InstallerArgs))
		})

		It("non-empty installer args with save-partlabel value", func() {
			host.InstallerArgs = `["--save-partlabel","data","--copy-network"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(host.InstallerArgs))
			Expect(request.SkipInstallationDiskCleanup).To(BeTrue())
		})

		It("non-empty installer args with save-partindex value", func() {
			host.InstallerArgs = `["--save-partindex","5","--copy-network"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			request := generateRequestForStep(stepReply[0])
			Expect(request.InstallerArgs).To(Equal(host.InstallerArgs))
			Expect(request.SkipInstallationDiskCleanup).To(BeTrue())
		})
	})

	Context("must-gather arguments", func() {
		var (
			installCmd        *installCmd
			instructionConfig InstructionConfig
		)

		BeforeEach(func() {
			instructionConfig = DefaultInstructionConfig
			installCmd = NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, instructionConfig, mockEvents, mockVersions)
		})

		It("single argument with ocp image only", func() {
			args, err := installCmd.getMustGatherArgument(defaultMustGatherVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(args).To(Equal(ocpMustGatherImage))
		})

		It("multiple images", func() {
			versions := map[string]string{
				"cnv": "cnv-must-gather-image",
				"lso": "lso-must-gather-image",
			}
			args, err := installCmd.getMustGatherArgument(versions)
			Expect(err).NotTo(HaveOccurred())

			out := make(map[string]string)
			Expect(json.Unmarshal([]byte(args), &out)).NotTo(HaveOccurred())
			Expect(out["cnv"]).To(Equal(versions["cnv"]))
			Expect(out["lso"]).To(Equal(versions["lso"]))
		})
	})

	Context("proxy arguments", func() {
		var (
			installCmd        *installCmd
			instructionConfig InstructionConfig
		)

		BeforeEach(func() {
			instructionConfig = DefaultInstructionConfig
			installCmd = NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, instructionConfig, mockEvents, mockVersions)
		})
		It("no-proxy without httpProxy", func() {
			args := installCmd.getProxyArguments("t-cluster", "proxy.org", "", "", "domain.com,192.168.1.0/24")
			Expect(args).Should(BeNil())
		})

		It("default no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "")
			Expect(swag.StringValue(noProxy.HTTPProxy)).Should(Equal("http://10.56.20.90:8080"))
			Expect(swag.StringValue(noProxy.NoProxy)).Should(Equal(
				"127.0.0.1,localhost,.svc,.cluster.local,api-int.t-cluster.proxy.org"))
		})
		It("updated no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "domain.org,127.0.0.2")
			Expect(swag.StringValue(noProxy.HTTPProxy)).Should(Equal("http://10.56.20.90:8080"))
			Expect(swag.StringValue(noProxy.NoProxy)).Should(Equal(
				"domain.org,127.0.0.2,127.0.0.1,localhost,.svc,.cluster.local,api-int.t-cluster.proxy.org"))
		})
		It("all-excluded no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "*")
			Expect(swag.StringValue(noProxy.HTTPProxy)).Should(Equal("http://10.56.20.90:8080"))
			Expect(swag.StringValue(noProxy.NoProxy)).Should(Equal("*"))
		})
		It("all-excluded no-proxy with spaces", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", " * ")
			Expect(swag.StringValue(noProxy.HTTPProxy)).Should(Equal("http://10.56.20.90:8080"))
			Expect(swag.StringValue(noProxy.NoProxy)).Should(Equal("*"))
		})
	})
})

var _ = Describe("construct host install arguments", func() {
	var (
		cluster  *common.Cluster
		host     *models.Host
		infraEnv *common.InfraEnv
		log      = common.GetTestLog()
	)
	BeforeEach(func() {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID:        &clusterID,
				ImageInfo: &models.ImageInfo{},
			},
		}

		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv = &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:        &infraEnvID,
				ClusterID: clusterID,
			},
		}

		hostID := strfmt.UUID(uuid.New().String())
		host = &models.Host{
			ID:                 &hostID,
			InfraEnvID:         infraEnvID,
			InstallationDiskID: "install-id",
		}
		host.Inventory = `{
			"disks":[]
		}`
	})
	It("multipath installation disk", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = fmt.Sprintf(`{
			"disks":[
				{
					"id": "install-id",
					"drive_type": "%s"
				},
				{
					"id": "other-id",
					"drive_type": "%s"
				}
			],
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/25"]
				}
			]
		}`, models.DriveTypeMultipath, models.DriveTypeSSD)
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","root=/dev/disk/by-label/dm-mpath-root","--append-karg","rw","--append-karg","rd.multipath=default"]`))
	})
	It("non-multipath installation disk", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = fmt.Sprintf(`{
			"disks":[
				{
					"id": "other-id",
					"drive_type": "%s"
				},
				{
					"id": "install-id",
					"drive_type": "%s"
				}
			],
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/25"]
				}
			]
		}`, models.DriveTypeMultipath, models.DriveTypeSSD)
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(BeEmpty())
	})
	It("ip=<nic>:dhcp6 added when machine CIDR is IPv6", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/64"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2001:db8::a/120"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth0:dhcp6"]`))
	})
	It("ip=<nic>:dhcp6 not added when machine CIDR is IPv6 and no matching interface", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/64"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2002:db8::a/120"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/25"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4 and multiple addresses", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/24", "192.186.10.10/25"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4 and multiple interfaces", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv4_addresses":["10.56.20.80/24"]
				},
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/25"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp not added when machine CIDR is IPv4 and no matching interface", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/25"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp6 added when there's no machine CIDR and bootstrap is IPv6", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: true,
				Inventory: `{
					"interfaces":[
						{
							"ipv6_addresses":["2002:db8::a/64"]
						}
					]
				}`,
			},
		}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2002:db8::b/120"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth0:dhcp6"]`))
	})
	It("ip=<nic>:dhcp6 not added when there's no machine CIDR, bootstrap is IPv6, but no matching interface", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: true,
				Inventory: `{
					"interfaces":[
						{
							"ipv4_addresses":["2002:db8::a/64"]
						}
					]
				}`,
			},
		}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2001:db8::b/120"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp added when there's no machine CIDR and bootstrap is IPv4", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: true,
				Inventory: `{
					"interfaces":[
						{
							"ipv4_addresses":["192.186.10.12/24"]
						}
					]
				}`,
			},
		}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp not added when there's no machine CIDR, bootstrap is IPv4, but no matching interface", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: true,
				Inventory: `{
					"interfaces":[
						{
							"ipv4_addresses":["192.186.10.12/24"]
						}
					]
				}`,
			},
		}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp not added and copy-network added with static config", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		infraEnv.StaticNetworkConfig = "something"
		cluster.ImageInfo.StaticNetworkConfig = "something"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--copy-network"]`))
	})
	It("ip=<nic>:dhcp not added with static config and copy-network set by the user", func() {
		host.InstallerArgs = `["--copy-network"]`
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		infraEnv.StaticNetworkConfig = "something"
		cluster.ImageInfo.StaticNetworkConfig = "something"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--copy-network"]`))
	})
	It("ip=<nic>:dhcp added when copy-network set by the user without static config", func() {
		host.InstallerArgs = `["--copy-network"]`
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.Inventory = `{
			"interfaces":[
				{
					"name": "ens3",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--copy-network","--append-karg","ip=ens3:dhcp"]`))
	})
	It("existing args updated with ip=<nic>:dhcp6 when machine CIDR is IPv6", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/120"}}
		host.InstallerArgs = `["--append-karg","rd.break=cmdline"]`
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv6_addresses":["2001:db8::b/120"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","rd.break=cmdline","--append-karg","ip=eth1:dhcp6"]`))
	})
	It("existing args updated with ip=<nic>:dhcp when machine CIDR is IPv4", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		host.InstallerArgs = `["--append-karg","rd.break=cmdline"]`
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth2",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","rd.break=cmdline","--append-karg","ip=eth2:dhcp"]`))
	})
	It("don't add ip arg if ip=dhcp added by user", func() {
		kargs := `["--append-karg","ip=dhcp"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/120"}}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp6 added by user", func() {
		kargs := `["--append-karg","ip=dhcp6"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=eth0:any added by user", func() {
		kargs := `["--append-karg","ip=eth0:any"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp deleted by user", func() {
		kargs := `["--delete-karg","ip=dhcp"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/120"}}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp6 deleted by user", func() {
		kargs := `["--delete-karg","ip=dhcp6"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.186.10.0/24"}}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("error if machine CIDR not given and no hosts", func() {
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		_, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).To(HaveOccurred())
	})
	It("error if machine CIDR not given and no bootsrap", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: false,
				Inventory: `{"interfaces":[{"ipv4_addresses":["192.186.10.12/24"]}]}`,
			},
		}
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		_, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).To(HaveOccurred())
	})
	It("ip argument not set in day2 when ipv4", func() {
		cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
		host.Inventory = `{
				"interfaces":[
					{
						"name": "eth0",
						"ipv4_addresses":["192.186.10.12/24"]
					}
				]
			}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip argument not set in day2 when ipv6", func() {
		cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
		host.Inventory = `{
				"interfaces":[
					{
						"name": "eth0",
						"ipv6_addresses":["2002:db8::1/64"]
					}
				]
			}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip argument not set when dual stack in day2", func() {
		cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
		host.Inventory = `{
				"interfaces":[
					{
						"ipv4_addresses":["2002:db8::1/64"],
						"ipv6_addresses":["192.186.10.12/24"]
					}
				]
			}`
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		args, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("error when inventory corrupted", func() {
		cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
		host.Inventory = ""
		inventory, _ := common.UnmarshalInventory(host.Inventory)
		_, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, log)
		Expect(err).To(HaveOccurred())
	})
})

func getBootableDiskNames(disks []*models.Disk) []string {
	bootableDisks := funk.Filter(disks, func(disk *models.Disk) bool {
		return disk.Bootable
	}).([]*models.Disk)

	return funk.Map(bootableDisks, func(disk *models.Disk) string {
		return disk.ID
	}).([]string)
}

func generateRequestForStep(reply *models.Step) *models.InstallCmdRequest {
	request := models.InstallCmdRequest{}
	err := json.Unmarshal([]byte(reply.Args[0]), &request)
	Expect(err).NotTo(HaveOccurred())
	return &request
}

func verifyDiskFormatCommand(generatedStep *models.Step, diskID string, expectWillBeFormatted bool) {
	request := generateRequestForStep(generatedStep)

	diskAppearsInToFormatList := funk.ContainsString(request.DisksToFormat, diskID)

	if expectWillBeFormatted {
		Expect(diskAppearsInToFormatList).To(BeTrue())
	} else {
		Expect(diskAppearsInToFormatList).To(BeFalse())
	}
}

func createClusterInDb(db *gorm.DB, haMode string) common.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		ID:                   &clusterId,
		OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
		HighAvailabilityMode: &haMode,
		MachineNetworks:      []*models.MachineNetwork{{Cidr: "10.56.20.0/24"}},
	}}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
}

func createInfraEnvInDb(db *gorm.DB, clusterId strfmt.UUID) common.InfraEnv {
	infraEnvId := strfmt.UUID(uuid.New().String())
	infraEnv := common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID:        &infraEnvId,
			ClusterID: clusterId,
		},
	}
	Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
	return infraEnv
}

func createHostInDb(db *gorm.DB, infraEnvId, clusterId strfmt.UUID, role models.HostRole, bootstrap bool, hostname string) models.Host {
	id := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:                &id,
		ClusterID:         &clusterId,
		InfraEnvID:        infraEnvId,
		Status:            swag.String(models.HostStatusDiscovering),
		Role:              role,
		Bootstrap:         bootstrap,
		Inventory:         common.GenerateTestDefaultInventory(),
		RequestedHostname: hostname,
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	return host
}

func postvalidation(isstepreplynil bool, issteperrnil bool, expectedstepreply *models.Step, expectedsteperr error, expectedrole models.HostRole) {
	if issteperrnil {
		ExpectWithOffset(1, expectedsteperr).Should(HaveOccurred())
	} else {
		ExpectWithOffset(1, expectedsteperr).ShouldNot(HaveOccurred())
	}
	if isstepreplynil {
		ExpectWithOffset(1, expectedstepreply).Should(BeNil())
	} else {
		ExpectWithOffset(1, expectedstepreply.StepType).To(Equal(models.StepTypeInstall))
		ExpectWithOffset(1, strings.Contains(expectedstepreply.Args[0], string(expectedrole))).To(Equal(true))
	}
}

func validateInstallCommand(installCmd *installCmd, reply *models.Step, role models.HostRole, infraEnvId, clusterId, hostId strfmt.UUID,
	bootDevice string, bootableDisks []string, haMode string) {
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))
	mustGatherImage, _ := installCmd.getMustGatherArgument(defaultMustGatherVersion)
	request := models.InstallCmdRequest{}
	err := json.Unmarshal([]byte(reply.Args[0]), &request)
	Expect(err).NotTo(HaveOccurred())
	Expect(request.InfraEnvID.String()).To(Equal(infraEnvId.String()))
	Expect(request.ClusterID.String()).To(Equal(clusterId.String()))
	Expect(request.HostID.String()).To(Equal(hostId.String()))
	Expect(swag.StringValue(request.HighAvailabilityMode)).To(Equal(haMode))
	Expect(request.OpenshiftVersion).To(Equal(common.TestDefaultConfig.OpenShiftVersion))
	Expect(*request.Role).To(Equal(role))
	Expect(swag.StringValue(request.BootDevice)).To(Equal(bootDevice))
	Expect(request.McoImage).To(Equal(defaultMCOImage))
	Expect(swag.StringValue(request.ControllerImage)).To(Equal(installCmd.instructionConfig.ControllerImage))
	Expect(request.MustGatherImage).To(Equal(mustGatherImage))
}
