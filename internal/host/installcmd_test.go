package host

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
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

var DefaultInstructionConfig = InstructionConfig{
	ServiceBaseURL:      "http://10.35.59.36:30485",
	InstallerImage:      "quay.io/ocpmetal/assisted-installer:latest",
	ControllerImage:     "quay.io/ocpmetal/assisted-installer-controller:latest",
	InventoryImage:      "quay.io/ocpmetal/assisted-installer-agent:latest",
	InstallationTimeout: 120,
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
		instructionConfig InstructionConfig
		disks             []*models.Disk
		dbName            = "install_cmd"
		validDiskSize     = int64(128849018880)
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instructionConfig = DefaultInstructionConfig
		installCmd = NewInstallCmd(getTestLog(), db, mockValidator, instructionConfig)
		cluster = createClusterInDb(db)
		clusterId = *cluster.ID
		host = createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
		}

	})

	Context("negative", func() {
		It("get_step_one_master", func() {
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, errors.New("error")).Times(1)
		})

		It("get_step_one_master_no_disks", func() {
			var emptydisks []*models.Disk
			mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(emptydisks, nil).Times(1)
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
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply[0], models.HostRoleMaster, string(clusterId), string(*host.ID), "", "", nil)

		hostFromDb := getHost(*host.ID, clusterId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
		Expect(hostFromDb.InstallationDiskPath).Should(Equal(GetDeviceFullName(disks[0].Name)))
	})

	It("get_step_three_master_success", func() {

		host2 := createHostInDb(db, clusterId, models.HostRoleMaster, false, "")
		host3 := createHostInDb(db, clusterId, models.HostRoleMaster, true, "some_hostname")
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(3)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply[0], models.HostRoleMaster, string(clusterId), string(*host.ID), "", "", nil)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host2)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply[0], models.HostRoleMaster, string(clusterId), string(*host2.ID), "", "", nil)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host3)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleBootstrap)
		validateInstallCommand(stepReply[0], models.HostRoleBootstrap, string(clusterId), string(*host3.ID), "some_hostname", "", nil)
	})

	It("invalid_inventory", func() {
		host.Inventory = "blah"
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
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
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply[0], models.HostRoleMaster, string(clusterId), string(*host.ID), "", "", []string{"/dev/sda"})

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
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply[0], models.HostRoleMaster, string(clusterId), string(*host.ID), "", "", []string{"/dev/sda", "/dev/sdc"})

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
		ctx        = context.Background()
		host       models.Host
		db         *gorm.DB
		validator  *hardware.MockValidator
		dbName     = "installcmd_args"
		controller *gomock.Controller
	)

	BeforeSuite(func() {
		db = common.PrepareTestDB(dbName)
		cluster := createClusterInDb(db)
		host = createHostInDb(db, *cluster.ID, models.HostRoleMaster, false, "")
		disks := []*models.Disk{{Name: "Disk1"}}
		controller = gomock.NewController(GinkgoT())
		validator = hardware.NewMockValidator(controller)
		validator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).AnyTimes()
	})

	AfterSuite(func() {
		controller.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("configuration_params", func() {

		It("insecure_cert_is_false_by_default", func() {
			config := &InstructionConfig{}
			installCmd := NewInstallCmd(getTestLog(), db, validator, *config)
			reply, err := installCmd.GetSteps(ctx, &host)
			verifyStepArg(reply[0], err, `--insecure[ =\w]*`, "--insecure=false")
		})

		It("insecure_cert_is_set_to_false", func() {
			config := &InstructionConfig{
				SkipCertVerification: false,
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, *config)
			reply, err := installCmd.GetSteps(ctx, &host)
			verifyStepArg(reply[0], err, `--insecure[ =\w]*`, "--insecure=false")
		})

		It("insecure_cert_is_set_to_true", func() {
			config := &InstructionConfig{
				SkipCertVerification: true,
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, *config)
			reply, err := installCmd.GetSteps(ctx, &host)
			verifyStepArg(reply[0], err, `--insecure[ =\w]*`, "--insecure=true")
		})

		It("target_url_is_passed", func() {
			config := &InstructionConfig{
				ServiceBaseURL: "ws://remote-host:8080",
			}
			installCmd := NewInstallCmd(getTestLog(), db, validator, *config)
			stepReply, err := installCmd.GetSteps(ctx, &host)
			verifyStepArg(stepReply[0], err, `-url [\w\d:/-]+`, fmt.Sprintf("-url %s", config.ServiceBaseURL))
		})
	})
})

func verifyStepArg(reply *models.Step, err error, expr string, expected string) {
	Expect(err).NotTo(HaveOccurred())
	Expect(reply).NotTo(BeNil())
	r := regexp.MustCompile(expr)
	match := r.FindAllStringSubmatch(reply.Args[1], -1)
	Expect(match).NotTo(BeNil())
	Expect(match).To(HaveLen(1))
	Expect(strings.TrimSpace(match[0][0])).To(Equal(expected))
}

func createClusterInDb(db *gorm.DB) common.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		ID:               &clusterId,
		OpenshiftVersion: "4.5",
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

func validateInstallCommand(reply *models.Step, role models.HostRole, clusterId string, hostId string, hostname string, proxy string, bootableDisks []string) {
	template := "podman run -v /dev:/dev:rw -v /opt:/opt:rw -v /run/systemd/journal/socket:/run/systemd/journal/socket " +
		"--privileged --pid=host " +
		"--net=host -v /var/log:/var/log:rw --env PULL_SECRET_TOKEN " +
		"--name assisted-installer quay.io/ocpmetal/assisted-installer:latest --role %s " +
		"--cluster-id %s " +
		"--boot-device /dev/sdb --host-id %s --openshift-version 4.5 " +
		"--controller-image %s --url %s --insecure=false --agent-image %s --installation-timeout %s"

	if hostname != "" {
		installCommand := template + " --host-name %s"
		ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId,
			hostId, DefaultInstructionConfig.ControllerImage, DefaultInstructionConfig.ServiceBaseURL, DefaultInstructionConfig.InventoryImage,
			strconv.Itoa(int(DefaultInstructionConfig.InstallationTimeout)), hostname)))
	} else if proxy != "" {
		installCommand := template + fmt.Sprintf(" %s", proxy)
		ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId,
			hostId, DefaultInstructionConfig.ControllerImage, DefaultInstructionConfig.ServiceBaseURL, DefaultInstructionConfig.InventoryImage,
			strconv.Itoa(int(DefaultInstructionConfig.InstallationTimeout)))))
	} else if bootableDisks != nil {
		formatCmds := ""
		for _, dev := range bootableDisks {
			formatCmds = formatCmds + fmt.Sprintf("wipefs --force --all %s ; ", dev)
			formatCmds = formatCmds + fmt.Sprintf("dd if=/dev/zero of=%s bs=512 count=33 ; ", dev)
			formatCmds = formatCmds + fmt.Sprintf("dd if=/dev/zero of=%s bs=512 count=33 seek=$((`blockdev --getsz %s` - 33)) ; ", dev, dev)
			formatCmds = formatCmds + fmt.Sprintf("sgdisk -Z %s ; ", dev)
		}
		installCommand := formatCmds + template
		ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId,
			hostId, DefaultInstructionConfig.ControllerImage, DefaultInstructionConfig.ServiceBaseURL, DefaultInstructionConfig.InventoryImage,
			strconv.Itoa(int(DefaultInstructionConfig.InstallationTimeout)))))
	} else {
		installCommand := template
		ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId,
			hostId, DefaultInstructionConfig.ControllerImage, DefaultInstructionConfig.ServiceBaseURL,
			DefaultInstructionConfig.InventoryImage, strconv.Itoa(int(DefaultInstructionConfig.InstallationTimeout)))))
	}
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))
}
