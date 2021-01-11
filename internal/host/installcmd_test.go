package host

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/versions"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

const defaultMCOImage = "mcoImage"

var DefaultInstructionConfig = InstructionConfig{
	ServiceBaseURL:      "http://10.35.59.36:30485",
	InstallerImage:      "quay.io/ocpmetal/assisted-installer:latest",
	ControllerImage:     "quay.io/ocpmetal/assisted-installer-controller:latest",
	InventoryImage:      "quay.io/ocpmetal/assisted-installer-agent:latest",
	FioPerfCheckImage:   "quay.io/ocpmetal/assisted-installer-agent:latest",
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
		disks             []*models.Disk
		dbName            = "install_cmd"
		validDiskSize     = int64(128849018880)
		mockEvents        *events.MockHandler
		mockVersions      *versions.MockHandler
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instructionConfig = DefaultInstructionConfig
		mockEvents = events.NewMockHandler(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		installCmd = NewInstallCmd(getTestLog(), db, mockValidator, mockRelease, instructionConfig, mockEvents, mockVersions)
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeFull)
		clusterId = *cluster.ID
		host = createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
		}
	})

	mockGetReleaseImage := func(times int) {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return("releaseImage", nil).Times(times)
	}

	Context("negative", func() {
		It("get_step_one_master", func() {
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, errors.New("error")).Times(1)
		})

		It("get_step_one_master_no_disks", func() {
			var emptydisks []*models.Disk
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(emptydisks, nil).Times(1)
		})

		It("get_step_one_master_no_mco_image", func() {
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
			mockGetReleaseImage(1)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("error")).Times(1)
		})

		AfterEach(func() {
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			Expect(stepReply).To(BeNil())
			postvalidation(true, true, nil, stepErr, "")
			hostFromDb := getHost(*host.ID, clusterId, db)
			Expect(hostFromDb.InstallerVersion).Should(BeEmpty())
			Expect(hostFromDb.InstallationDiskPath).Should(BeEmpty())
		})
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		mockGetReleaseImage(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, nil, models.ClusterHighAvailabilityModeFull)

		hostFromDb := getHost(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
		Expect(hostFromDb.InstallationDiskPath).Should(Equal(GetDeviceFullName(disks[0].Name)))
	})

	It("get_step_three_master_success", func() {
		host2 := createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
		host3 := createHostInDb(db, clusterId, models.HostRoleMaster, true, "some_hostname")
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(3)
		mockGetReleaseImage(3)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(3)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host2)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host2.ID, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host3)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleBootstrap)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleBootstrap, clusterId, *host3.ID, nil, models.ClusterHighAvailabilityModeFull)
	})
	It("invalid_inventory", func() {
		host.Inventory = "blah"
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		mockGetReleaseImage(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(true, true, nil, stepErr, "")
	})

	It("format_one_bootable", func() {
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize, Bootable: true},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, "Performing quick format of disk /dev/sda", gomock.Any())
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		mockGetReleaseImage(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, []string{"/dev/sda"}, models.ClusterHighAvailabilityModeFull)

		hostFromDb := getHost(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
		Expect(hostFromDb.InstallationDiskPath).Should(Equal(GetDeviceFullName(disks[0].Name)))
	})

	It("format_multiple_bootable_skip", func() {
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize, Bootable: true},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdc", SizeBytes: validDiskSize, Bootable: true},
			{DriveType: "HDD", Name: "sdi", SizeBytes: validDiskSize, Bootable: true, ByPath: "pci-0000:04:00.0-fc-0x5006016b08603d0d-lun-0"},
			{DriveType: "HDD", Name: "sdf", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdg", SizeBytes: validDiskSize, Bootable: true, ByPath: "ip-10.188.2.249:3260-iscsi-iqn.2001-05.com.equallogic:0-fe83b6-aaea957cc-b6e9d343a9758fdc-volume-50a72e0c-0a4a-4b2d-92ab-b0500dfe5c64-lun-0"},
			{DriveType: "HDD", Name: "sdg", SizeBytes: validDiskSize, Bootable: true, IsInstallationMedia: true},
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, "Performing quick format of disk /dev/sda", gomock.Any())
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, "Performing quick format of disk /dev/sdc", gomock.Any())
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		mockGetReleaseImage(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, []string{"/dev/sda", "/dev/sdc"}, models.ClusterHighAvailabilityModeFull)

		hostFromDb := getHost(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
		Expect(hostFromDb.InstallationDiskPath).Should(Equal(GetDeviceFullName(disks[0].Name)))
	})

	It("format_multiple_bootable", func() {
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize, Bootable: true},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdc", SizeBytes: validDiskSize, Bootable: true},
		}
		inventory := models.Inventory{
			Disks: disks,
		}
		b, err := json.Marshal(&inventory)
		Expect(err).To(Not(HaveOccurred()))
		host.Inventory = string(b)
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, "Performing quick format of disk /dev/sda", gomock.Any())
		mockEvents.EXPECT().AddEvent(gomock.Any(), host.ClusterID, host.ID, models.EventSeverityInfo, "Performing quick format of disk /dev/sdc", gomock.Any())
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		mockGetReleaseImage(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, clusterId, *host.ID, []string{"/dev/sda", "/dev/sdc"}, models.ClusterHighAvailabilityModeFull)

		hostFromDb := getHost(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
		Expect(hostFromDb.InstallationDiskPath).Should(Equal(GetDeviceFullName(disks[0].Name)))
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
		dbName       = "installcmd_args"
		ctrl         *gomock.Controller
		mockEvents   *events.MockHandler
		mockVersions *versions.MockHandler
	)

	BeforeSuite(func() {
		db = common.PrepareTestDB(dbName)
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeNone)
		host = createHostInDb(db, *cluster.ID, models.HostRoleMaster, false, "")
		disks := []*models.Disk{{Name: "Disk1"}}
		ctrl = gomock.NewController(GinkgoT())
		validator = hardware.NewMockValidator(ctrl)
		validator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).AnyTimes()
		mockEvents = events.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any()).Return("releaseImage", nil).AnyTimes()
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).AnyTimes()
	})

	AfterSuite(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("configuration_params", func() {
		It("insecure_cert_is_false_by_default", func() {
			config := &InstructionConfig{}
			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeFalse())
		})

		It("insecure_cert_is_set_to_false", func() {
			config := &InstructionConfig{
				SkipCertVerification: false,
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeFalse())
		})

		It("insecure_cert_is_set_to_true", func() {
			config := &InstructionConfig{
				SkipCertVerification: true,
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			Expect(strings.Contains(stepReply[0].Args[1], "--insecure")).Should(BeTrue())
		})

		It("target_url_is_passed", func() {
			config := &InstructionConfig{
				ServiceBaseURL: "ws://remote-host:8080",
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, *config, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--url", config.ServiceBaseURL, 2)
		})

		It("verify high-availability-mode is None", func() {
			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--high-availability-mode", models.ClusterHighAvailabilityModeNone, 1)
		})

		It("verify empty value", func() {
			mockRelease = oc.NewMockRelease(ctrl)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--mco-image", "''", 1)
		})

		It("verify escaped whitespace value", func() {
			value := "\nescaped_\n\t_value\n"
			mockRelease = oc.NewMockRelease(ctrl)
			mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(value, nil).AnyTimes()

			installCmd := NewInstallCmd(getTestLog(), db, validator, mockRelease, InstructionConfig{}, mockEvents, mockVersions)
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
			installCmd = NewInstallCmd(getTestLog(), db, validator, mockRelease, instructionConfig, mockEvents, mockVersions)
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
			db.Model(&cluster).Update("image_static_ips_config", "rkhkjgdfd")
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", `["--copy-network"]`), 1)
		})

		It("non-empty installer args with static ip config", func() {
			db.Model(&cluster).Update("image_static_ips_config", "rkhkjgdfd")
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
		OpenshiftVersion:     common.DefaultTestOpenShiftVersion,
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
		Inventory:         defaultInventory(),
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
	bootableDisks []string, haMode string) {
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))

	bootDevice := "/dev/sdb"

	verifyArgInCommand(reply.Args[1], "--cluster-id", string(clusterId), 2)
	verifyArgInCommand(reply.Args[1], "--host-id", string(hostId), 2)
	verifyArgInCommand(reply.Args[1], "--high-availability-mode", haMode, 1)
	verifyArgInCommand(reply.Args[1], "--openshift-version", common.DefaultTestOpenShiftVersion, 1)
	verifyArgInCommand(reply.Args[1], "--role", string(role), 1)
	verifyArgInCommand(reply.Args[1], "--boot-device", bootDevice, 1)
	verifyArgInCommand(reply.Args[1], "--url", installCmd.instructionConfig.ServiceBaseURL, 2)
	verifyArgInCommand(reply.Args[1], "--mco-image", defaultMCOImage, 1)
	verifyArgInCommand(reply.Args[1], "--controller-image", installCmd.instructionConfig.ControllerImage, 1)
	verifyArgInCommand(reply.Args[1], "--agent-image", installCmd.instructionConfig.InventoryImage, 1)
	verifyArgInCommand(reply.Args[1], "--installation-timeout", strconv.Itoa(int(installCmd.instructionConfig.InstallationTimeout)), 1)

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
