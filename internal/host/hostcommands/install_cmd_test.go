package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	defaultMCOImage    = "mcoImage"
	ocpMustGatherImage = "mustGatherImage"
)

var defaultMustGatherVersion = versions.MustGatherVersion{
	"ocp": ocpMustGatherImage,
}

var DefaultInstructionConfig = InstructionConfig{
	ServiceBaseURL:     "http://10.35.59.36:30485",
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
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, false, "")
	})

	mockGetReleaseImage := func(times int) {
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(times)
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
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			Expect(stepReply).To(BeNil())
			postvalidation(true, true, nil, stepErr, "")
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(BeEmpty())
		})
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
		Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
	})

	It("get_step_three_master_success", func() {
		host2 := createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, false, "")
		host3 := createHostInDb(db, infraEnvId, clusterId, models.HostRoleMaster, true, "some_hostname")
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(3)
		mockGetReleaseImage(3)
		mockImages(3)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host2)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, infraEnvId, clusterId, *host2.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host3)
		postvalidation(false, false, stepReply[0], stepErr, models.HostRoleBootstrap)
		validateInstallCommand(installCmd, stepReply[0], models.HostRoleBootstrap, infraEnvId, clusterId, *host3.ID, common.TestDiskId, nil, models.ClusterHighAvailabilityModeFull)
	})
	It("invalid_inventory", func() {
		host.Inventory = "blah"
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).Times(1)
		mockGetReleaseImage(1)
		mockImages(1)
		stepReply, stepErr = installCmd.GetSteps(ctx, &host)
		postvalidation(true, true, nil, stepErr, "")
	})

	Context("Bootable_Disks", func() {
		createDisk := func(name string, bootable bool) *models.Disk {
			return &models.Disk{DriveType: "HDD", ID: fmt.Sprintf("/dev/disk/by-id/wwn-%s", name), Name: name,
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
			mockEvents.EXPECT().AddEvent(gomock.Any(), *host.ClusterID, host.ID, models.EventSeverityInfo, message, gomock.Any()).Times(times)
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

		It("format_removable_disk_should_not_occur", func() {
			disks := []*models.Disk{
				createRemovableDisk("sda", true), //removable disk
				sdb,                              //installation disk
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(disks[0], 0)
			prepareGetStep(sdb)
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			verifyDiskFormatCommand(stepReply[0].Args[1], disks[0].ID, false)
		})

		It("format_one_bootable", func() {
			disks := []*models.Disk{
				sdb, //installation disk
				sda, //bootable disk
				sdh, //non-bootable, non-installation
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(sda, 1)
			prepareGetStep(sdb)
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sdb.ID, getBootableDiskNames(disks), models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(stepReply[0].Args[1], sda.ID, true)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdb.ID, false)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdh.ID, false)
		})

		It("format_multiple_bootable_skip", func() {
			sdi := createDisk("sdi", true)
			sdi.ByPath = "pci-0000:04:00.0-fc-0x5006016b08603d0d-lun-0"
			sdg := createDisk("sdg", true)
			sdg.ByPath = "ip-10.188.2.249:3260-iscsi-iqn.2001-05.com.equallogic:0-fe83b6-aaea957cc-b6e9d343a9758fdc-volume-50a72e0c-0a4a-4b2d-92ab-b0500dfe5c64-lun-0"
			sdd := createDisk("sdd", false)
			sdd.IsInstallationMedia = true
			sdj := createDisk("sdj", true)
			sdj.ByPath = "/dev/mmcblk1boot1"

			disks := []*models.Disk{
				sdb,                      //installation disk
				sdh,                      //non-bootable-disk
				sda,                      //bootable disk #1
				sdc,                      //bootable disk #2
				sdi,                      //skip bootable disk -fc-
				sdg,                      //skip bootable disk -iscsi-
				createDisk("sdf", false), //non-bootable disk
				sdd,                      //skip installation media
				sdj,                      //skip mmcblk device
			}
			host.Inventory = getInventory(disks)
			mockFormatEvent(sda, 1)
			mockFormatEvent(sdc, 1)
			prepareGetStep(sdb)
			stepReply, stepErr = installCmd.GetSteps(ctx, &host)
			postvalidation(false, false, stepReply[0], stepErr, models.HostRoleMaster)
			validateInstallCommand(installCmd, stepReply[0], models.HostRoleMaster, infraEnvId, clusterId, *host.ID, sdb.ID, []string{sda.ID, sdc.ID}, models.ClusterHighAvailabilityModeFull)
			hostFromDb := hostutil.GetHostFromDB(*host.ID, infraEnvId, db)
			Expect(hostFromDb.InstallerVersion).Should(Equal(DefaultInstructionConfig.InstallerImage))
			verifyDiskFormatCommand(stepReply[0].Args[1], sda.ID, true)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdc.ID, true)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdi.ID, false)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdg.ID, false)
			verifyDiskFormatCommand(stepReply[0].Args[1], sdj.ID, false)
		})
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
		infraEnvId   strfmt.UUID
	)

	mockImages := func() {
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).AnyTimes()
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).AnyTimes()
	}

	BeforeSuite(func() {
		db, dbName = common.PrepareTestDB()
		cluster = createClusterInDb(db, models.ClusterHighAvailabilityModeNone)
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = createHostInDb(db, infraEnvId, *cluster.ID, models.HostRoleMaster, false, "")
		ctrl = gomock.NewController(GinkgoT())
		validator = hardware.NewMockValidator(ctrl)
		validator.EXPECT().GetHostInstallationPath(gomock.Any()).Return(common.TestDiskId).AnyTimes()
		mockEvents = events.NewMockHandler(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).AnyTimes()
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
			verifyArgInCommand(stepReply[0].Args[1], "--url", config.ServiceBaseURL, 1)
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
			mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).AnyTimes()

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
			mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).AnyTimes()

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
			db.Model(&cluster).Update("static_network_configured", true)
			host.InstallerArgs = ""
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", `["--copy-network"]`), 1)
		})

		It("non-empty installer args with static ip config", func() {
			db.Model(&cluster).Update("static_network_configured", true)
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", `["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`), 1)
		})
		It("non-empty installer args with copy network with static ip config", func() {
			db.Model(&cluster).Update("image_static_ips_config", "rkhkjgdfd")
			host.InstallerArgs = `["--append-karg","nameserver=8.8.8.8","-n","--copy-network"]`
			stepReply, err := installCmd.GetSteps(ctx, &host)
			Expect(err).NotTo(HaveOccurred())
			Expect(stepReply).NotTo(BeNil())
			verifyArgInCommand(stepReply[0].Args[1], "--installer-args", fmt.Sprintf("'%s'", host.InstallerArgs), 1)
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
			Expect(args).Should(Equal([]string{}))
		})

		It("default no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "")
			Expect(noProxy).Should(Equal([]string{
				"--http-proxy",
				"http://10.56.20.90:8080",
				"--no-proxy",
				"127.0.0.1,localhost,.svc,.cluster.local,api-int.t-cluster.proxy.org",
			}))
		})
		It("updated no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "domain.org,127.0.0.2")
			Expect(noProxy).Should(Equal([]string{
				"--http-proxy",
				"http://10.56.20.90:8080",
				"--no-proxy",
				"domain.org,127.0.0.2,127.0.0.1,localhost,.svc,.cluster.local,api-int.t-cluster.proxy.org",
			}))
		})
		It("all-excluded no-proxy", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", "*")
			Expect(noProxy).Should(Equal([]string{
				"--http-proxy",
				"http://10.56.20.90:8080",
				"--no-proxy",
				"*",
			}))

		})
		It("all-excluded no-proxy with spaces", func() {
			noProxy := installCmd.getProxyArguments("t-cluster", "proxy.org", "http://10.56.20.90:8080", "", " * ")
			Expect(noProxy).Should(Equal([]string{
				"--http-proxy",
				"http://10.56.20.90:8080",
				"--no-proxy",
				"*",
			}))
		})
	})
})

var _ = Describe("construct host install arguments", func() {
	var (
		cluster *common.Cluster
		host    *models.Host
		log     = common.GetTestLog()
	)
	BeforeEach(func() {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID:        &clusterID,
				ImageInfo: &models.ImageInfo{},
			},
		}
		hostID := strfmt.UUID(uuid.New().String())
		host = &models.Host{
			ID: &hostID,
		}
	})
	It("ip=<nic>:dhcp6 added when machine CIDR is IPv6", func() {
		cluster.MachineNetworkCidr = "2001:db8::/64"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2001:db8::a/120"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth0:dhcp6"]`))
	})
	It("ip=<nic>:dhcp6 not added when machine CIDR is IPv6 and no matching interface", func() {
		cluster.MachineNetworkCidr = "2001:db8::/64"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv6_addresses":["2002:db8::a/120"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/25"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4 and multiple addresses", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/24", "192.186.10.10/25"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp added when machine CIDR is IPv4 and multiple interfaces", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
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
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp"]`))
	})
	It("ip=<nic>:dhcp not added when machine CIDR is IPv4 and no matching interface", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["10.56.20.80/25"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("ip=<nic>:dhcp and copy-network added with static config", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		cluster.ImageInfo.StaticNetworkConfig = "something"
		cluster.StaticNetworkConfigured = true
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","ip=eth1:dhcp","--copy-network"]`))
	})
	It("ip=<nic>:dhcp added with static config and copy-network set by the user", func() {
		host.InstallerArgs = `["--copy-network"]`
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		cluster.ImageInfo.StaticNetworkConfig = "something"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth0",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--copy-network","--append-karg","ip=eth0:dhcp"]`))
	})
	It("ip=<nic>:dhcp added when copy-network set by the user without static config", func() {
		host.InstallerArgs = `["--copy-network"]`
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		host.Inventory = `{
			"interfaces":[
				{
					"name": "ens3",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--copy-network","--append-karg","ip=ens3:dhcp"]`))
	})
	It("existing args updated with ip=<nic>:dhcp6 when machine CIDR is IPv6", func() {
		cluster.MachineNetworkCidr = "2001:db8::/120"
		host.InstallerArgs = `["--append-karg","rd.break=cmdline"]`
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth1",
					"ipv6_addresses":["2001:db8::b/120"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","rd.break=cmdline","--append-karg","ip=eth1:dhcp6"]`))
	})
	It("existing args updated with ip=<nic>:dhcp when machine CIDR is IPv4", func() {
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		host.InstallerArgs = `["--append-karg","rd.break=cmdline"]`
		host.Inventory = `{
			"interfaces":[
				{
					"name": "eth2",
					"ipv4_addresses":["192.186.10.10/24"]
				}
			]
		}`
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(`["--append-karg","rd.break=cmdline","--append-karg","ip=eth2:dhcp"]`))
	})
	It("don't add ip arg if ip=dhcp added by user", func() {
		kargs := `["--append-karg","ip=dhcp"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworkCidr = "2001:db8::/120"
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp6 added by user", func() {
		kargs := `["--append-karg","ip=dhcp6"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=eth0:any added by user", func() {
		kargs := `["--append-karg","ip=eth0:any"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp deleted by user", func() {
		kargs := `["--delete-karg","ip=dhcp"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworkCidr = "2001:db8::/120"
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("don't add ip arg if ip=dhcp6 deleted by user", func() {
		kargs := `["--delete-karg","ip=dhcp6"]`
		host.InstallerArgs = kargs
		cluster.MachineNetworkCidr = "192.186.10.0/24"
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(kargs))
	})
	It("error if machine CIDR not given and no hosts", func() {
		_, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).To(HaveOccurred())
	})
	It("error if machine CIDR not given and no bootsrap", func() {
		cluster.Hosts = []*models.Host{
			{
				Bootstrap: false,
				Inventory: `{"interfaces":[{"ipv4_addresses":["192.186.10.12/24"]}]}`,
			},
		}
		_, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
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
		args, err := constructHostInstallerArgs(cluster, host, log)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal(""))
	})
	It("error when inventory corrupted", func() {
		cluster.Kind = swag.String(models.ClusterKindAddHostsCluster)
		host.Inventory = ""
		_, err := constructHostInstallerArgs(cluster, host, log)
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

func verifyArgInCommand(command, key, value string, count int) {
	r := regexp.MustCompile(fmt.Sprintf(`%s ([^ ]+)`, key))
	match := r.FindAllStringSubmatch(command, -1)
	Expect(match).NotTo(BeNil())
	Expect(match).To(HaveLen(count))
	Expect(strings.TrimSpace(match[0][1])).To(Equal(quoteString(value)))
}

func verifyDiskFormatCommand(command string, value string, exists bool) {
	r := regexp.MustCompile(`dd if=\/dev\/zero of=([^\s]+) bs=512 count=1 ; `)
	matches := r.FindAllStringSubmatch(command, -1)
	matchValue := func() bool {
		if matches == nil {
			//empty format command
			return false
		}
		for _, match := range matches {
			if match[1] == value {
				//found value in command
				return true
			}
		}
		return false
	}
	Expect(matchValue()).To(Equal(exists))
}

func quoteString(value string) string {
	if strings.ContainsRune(value, '"') && !(strings.Index(value, "'") == 0) {
		return fmt.Sprintf("'%s'", value)
	}
	return value
}

func createClusterInDb(db *gorm.DB, haMode string) common.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		ID:                   &clusterId,
		OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
		HighAvailabilityMode: &haMode,
		MachineNetworkCidr:   "10.56.20.0/24",
	}}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
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
		ExpectWithOffset(1, strings.Contains(expectedstepreply.Args[1], string(expectedrole))).To(Equal(true))
	}
}

func validateInstallCommand(installCmd *installCmd, reply *models.Step, role models.HostRole, infraEnvId, clusterId, hostId strfmt.UUID,
	bootDevice string, bootableDisks []string, haMode string) {
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))
	mustGatherImage, _ := installCmd.getMustGatherArgument(defaultMustGatherVersion)
	verifyArgInCommand(reply.Args[1], "--infra-env-id", string(infraEnvId), 1)
	verifyArgInCommand(reply.Args[1], "--cluster-id", string(clusterId), 1)
	verifyArgInCommand(reply.Args[1], "--host-id", string(hostId), 1)
	verifyArgInCommand(reply.Args[1], "--high-availability-mode", haMode, 1)
	verifyArgInCommand(reply.Args[1], "--openshift-version", common.TestDefaultConfig.OpenShiftVersion, 1)
	verifyArgInCommand(reply.Args[1], "--role", string(role), 1)
	verifyArgInCommand(reply.Args[1], "--boot-device", bootDevice, 1)
	verifyArgInCommand(reply.Args[1], "--url", installCmd.instructionConfig.ServiceBaseURL, 1)
	verifyArgInCommand(reply.Args[1], "--mco-image", defaultMCOImage, 1)
	verifyArgInCommand(reply.Args[1], "--controller-image", installCmd.instructionConfig.ControllerImage, 1)
	verifyArgInCommand(reply.Args[1], "--agent-image", installCmd.instructionConfig.AgentImage, 1)
	verifyArgInCommand(reply.Args[1], "--must-gather-image", mustGatherImage, 1)
}
