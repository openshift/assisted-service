package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
)

const (
	defaultReleaseImage    = "releaseImage"
	defaultMCOImage        = "mcoImage"
	defaultMustGatherImage = "mustGatherImage"
)

var DefaultInstructionConfig = InstructionConfig{
	ServiceBaseURL:      "http://10.35.59.36:30485",
	InstallerImage:      "quay.io/ocpmetal/assisted-installer:latest",
	ControllerImage:     "quay.io/ocpmetal/assisted-installer-controller:latest",
	AgentImage:          "quay.io/ocpmetal/assisted-installer-agent:latest",
	InstallationTimeout: 120,
	ReleaseImageMirror:  "local.registry:5000/ocp@sha256:eab93b4591699a5a4ff50ad3517892653f04fb840127895bb3609b3cc68f98f3",
}

var _ = Describe("installcmd", func() {
	var (
		ctx               = context.Background()
		host              models.Host
		cluster           common.Cluster
		db                *gorm.DB
		installCmd        *installCmd
		clusterId         strfmt.UUID
		stepReply         []*models.Step
		stepErr           error
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		mockRelease       *oc.MockRelease
		instructionConfig InstructionConfig
		dbName            string
		mockEvents        *events.MockHandler
		mockVersions      *versions.MockHandler
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instructionConfig = DefaultInstructionConfig
		mockEvents = events.NewMockHandler(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		installCmd = NewInstallCmd(common.GetTestLog(), db, mockValidator, mockRelease, instructionConfig, mockEvents, mockVersions)
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeFull)
		clusterId = *cluster.ID
		host = createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
	})

	mockGetReleaseImage := func(times int) {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(defaultReleaseImage, nil).Times(times)
	}

	mockImages := func(times int) {
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(times)
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherImage, nil).Times(times)
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
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			Expect(stepReply).To(BeNil())
			postvalidation(true, true, nil, stepErr, "")
			hostFromDb := hostutil.GetHostFromDB(*host.ID, clusterId, db)
			Expect(hostFromDb.InstallerVersion).Should(BeEmpty())
		})
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	It("get_step_three_master_success", func() {
		host2 := createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
		host3 := createHostInDb(db, clusterId, models.HostRoleMaster, true, "some_hostname")
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(3)
		mockGetReleaseImage(3)
		mockImages(3)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host2)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host2.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host3)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleBootstrap)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleBootstrap, clusterId, *host3.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
	})
	It("invalid_inventory", func() {
		host.Inventory = "blah"
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(true, true, nil, stepErr, "")
	})

	sda := createDisk("sda", true)
	sdb := createDisk("sdb", false)
	sdh := createDisk("sdh", false)
	sdc := createDisk("sdc", true)
	eventStatusInfo := "%s: Performing quick format of disk %s(%s)"

	It("format_one_bootable", func() {
		disks := []*models.Disk{
			sdb,
			sda,
			sdh,
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		message := fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), sda.Name, sda.ID)
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, message, gomock.Any())
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(sdb.ID)
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, sdb.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	It("format_multiple_bootable_skip", func() {
		sdi := createDisk("sdi", true)
		sdi.ByPath = "pci-0000:04:00.0-fc-0x5006016b08603d0d-lun-0"
		sdg := createDisk("sdg", true)
		sdg.ByPath = "ip-10.188.2.249:3260-iscsi-iqn.2001-05.com.equallogic:0-fe83b6-aaea957cc-b6e9d343a9758fdc-volume-50a72e0c-0a4a-4b2d-92ab-b0500dfe5c64-lun-0"
		sdd := createDisk("sdd", false)
		sdd.IsInstallationMedia = true

		disks := []*models.Disk{
			sdb,
			sda,
			sdc,
			sdi,
			createDisk("sdf", false),
			sdg,
			sdh,
			sdd,
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(sdb.ID)
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), sda.Name, sda.ID), gomock.Any())
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), sdc.Name, sdc.ID), gomock.Any())
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, sdb.ID, []string{sda.ID, sdc.ID}, models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	It("format_multiple_bootable", func() {
		disks := []*models.Disk{
			sdb,
			sda,
			sdh,
			sdc,
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)

		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), sda.Name, sda.ID), gomock.Any())
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, fmt.Sprintf(eventStatusInfo, hostutil.GetHostnameForMsg(&host), sdc.Name, sdc.ID), gomock.Any())
		mockGetReleaseImage(1)
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(sdb.ID)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, sdb.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
		stepReply = nil
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
		mockEvents   *events.MockHandler
		mockVersions *versions.MockHandler
	)

	mockImages := func() {
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).AnyTimes()
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherImage, nil).AnyTimes()
	}

	BeforeSuite(func() {
		db, dbName = common.PrepareTestDB()
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeNone)
		host = createHostInDb(db, *cluster.ID, models.HostRoleMaster, false, "")
		ctrl = gomock.NewController(GinkgoT())
		validator = hardware.NewMockValidator(ctrl)
		validator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).AnyTimes()
		mockEvents = events.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return(defaultReleaseImage, nil).AnyTimes()
		mockImages()
	})

	AfterSuite(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("configuration_params", func() {
		It("insecure_cert_is_false_by_default", func() {
			config := &InstructionConfig{}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeFalse())
		})

		It("insecure_cert_is_set_to_false", func() {
			config := &InstructionConfig{
				SkipCertVerification: false,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeFalse())
		})

		It("insecure_cert_is_set_to_true", func() {
			config := &InstructionConfig{
				SkipCertVerification: true,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeTrue())
		})

		It("check_cluster_version_is_false_by_default", func() {
			config := &InstructionConfig{}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--check-cluster-version")).Should(BeFalse())
		})

		It("check_cluster_version_is_set_to_false", func() {
			config := &InstructionConfig{
				CheckClusterVersion: false,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--check-cluster-version")).Should(BeFalse())
		})

		It("check_cluster_version_is_set_to_true", func() {
			config := &InstructionConfig{
				CheckClusterVersion: true,
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--check-cluster-version")).Should(BeTrue())
		})

		It("target_url_is_passed", func() {
			config := &InstructionConfig{
				ServiceBaseURL: "ws://remote-host:8080",
			}
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--url", config.ServiceBaseURL, 2)
		})

		It("verify high-availability-mode is None", func() {
			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--high-availability-mode", models.ClusterHighAvailabilityModeNone, 1)
		})

		It("verify empty value", func() {
			mockRelease = oc.NewMockRelease(ctrl)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherImage, nil).AnyTimes()

			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--mco-image", "''", 1)
		})

		It("verify escaped whitespace value", func() {
			value := "\nescaped_\n\t_value\n"
			mockRelease = oc.NewMockRelease(ctrl)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(value, nil).AnyTimes()
			mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherImage, nil).AnyTimes()

			installCmd := NewInstallCmd(common.GetTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			fmt.Println(stepReply[0].Args[1])
			verifyArgInCommand(stepReply[0].Args[1], "--mco-image", fmt.Sprintf("'%s'", value), 1)
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
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", host.InstallerArgs), 1)
		})
		It("empty installer args", func() {
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--installer-args")).Should(BeFalse())
		})

		It("empty installer args with static ip config", func() {
			db.Model(&cluster).Update("image_static_network_config", "rkhkjgdfd")
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", `["--copy-network"]`), 1)
		})

		It("non-empty installer args with static ip config", func() {
			db.Model(&cluster).Update("image_static_network_config", "rkhkjgdfd")
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", `["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`), 1)
		})
		It("non-empty installer args with copy network  with static ip config", func() {
			db.Model(&cluster).Update("image_static_ips_config", "rkhkjgdfd")
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", host.InstallerArgs), 1)
		})

	})
})

func createDisk(name string, bootable bool) *models.Disk {
	return &models.Disk{DriveType: "HDD", ID: fmt.Sprintf("/dev/disk/by-id/wwn-%s", name), Name: name,
		SizeBytes: int64(128849018880), Bootable: bootable}
}

func getBootableDiskNames(disks []*models.Disk) []string {
	bootableDisks := funk.Filter(disks, func(disk *models.Disk) bool {
		return disk.Bootable
	}).([]*models.Disk)

	return funk.Map(bootableDisks, func(disk *models.Disk) string {
		return disk.ID
	}).([]string)
}

func verifyArgInCommand(command, key, value string, count int) {
	r := regexp.MustCompile(fmt.Sprintf(`%s ([^ ]+)`, key))
	match := r.FindAllStringSubmatch(command, -1)
	Expect(match).NotTo(BeNil())
	Expect(match).To(HaveLen(count))
	Expect(strings.TrimSpace(match[0][1])).To(Equal(value))
}

func createClusterInDb(db *gorm.DB, haMode string) common.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		ID:                   &clusterId,
		OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
		HighAvailabilityMode: &haMode,
	}}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
}

func createHostInDb(db *gorm.DB, clusterId strfmt.UUID, role models.HostRole, bootstrap bool, hostname string) models.Host {
	id := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:                &id,
		ClusterID:         clusterId,
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
		ExpectWithOffset(1, strings.Contains(expectedstepreply.Args[1], string(expectedrole))).To(Equal(true))
	}
}

func validateInstallCommand(installCmd *installCmd, reply *models.Step, role models.HostRole, clusterId, hostId strfmt.UUID,
	bootDevice string, bootableDisks []string, haMode string) {
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))
	verifyArgInCommand(reply.Args[1], "--cluster-id", string(clusterId), 2)
	verifyArgInCommand(reply.Args[1], "--host-id", string(hostId), 2)
	verifyArgInCommand(reply.Args[1], "--high-availability-mode", haMode, 1)
	verifyArgInCommand(reply.Args[1], "--openshift-version", common.TestDefaultConfig.OpenShiftVersion, 1)
	verifyArgInCommand(reply.Args[1], "--role", string(role), 1)
	verifyArgInCommand(reply.Args[1], "--boot-device", bootDevice, 1)
	verifyArgInCommand(reply.Args[1], "--url", installCmd.instructionConfig.ServiceBaseURL, 2)
	verifyArgInCommand(reply.Args[1], "--mco-image", defaultMCOImage, 1)
	verifyArgInCommand(reply.Args[1], "--controller-image", installCmd.instructionConfig.ControllerImage, 1)
	verifyArgInCommand(reply.Args[1], "--agent-image", installCmd.instructionConfig.AgentImage, 1)
	verifyArgInCommand(reply.Args[1], "--installation-timeout", strconv.Itoa(int(installCmd.instructionConfig.InstallationTimeout)), 1)
	verifyArgInCommand(reply.Args[1], "--must-gather-image", defaultMustGatherImage, 1)

	fioPerfCheckCmd := "podman run --privileged --net=host --rm --quiet --name=assisted-installer -v /dev:/dev:rw -v /var/log:/var/log " +
		"-v /run/systemd/journal/socket:/run/systemd/journal/socket " +
		"--env PULL_SECRET_TOKEN --env HTTP_PROXY --env HTTPS_PROXY --env NO_PROXY --env http_proxy --env https_proxy --env no_proxy " +
		"quay.io/ocpmetal/assisted-installer-agent:latest fio_perf_check " +
		fmt.Sprintf("--url %s --cluster-id %s --host-id %s --agent-version quay.io/ocpmetal/assisted-installer-agent:latest ", installCmd.instructionConfig.ServiceBaseURL, string(clusterId), string(hostId)) +
		fmt.Sprintf("\"{\\\"duration_threshold_ms\\\":10,\\\"exit_code\\\":222,\\\"path\\\":\\\"%s\\\"}\" ; ", bootDevice)

	installCommandPrefix := fioPerfCheckCmd

	if bootableDisks != nil {
		formatCmds := ""
		for _, dev := range bootableDisks {
			formatCmds = formatCmds + fmt.Sprintf("dd if=/dev/zero of=%s bs=512 count=1 ; ", dev)
		}
		installCommandPrefix = formatCmds + installCommandPrefix
	}

	ExpectWithOffset(1, reply.Args[1]).To(ContainSubstring(installCommandPrefix))
}
