package host

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("installcmd", func() {
	var (
		ctx               = context.Background()
		host              models.Host
		cluster           common.Cluster
		db                *gorm.DB
		installCmd        *installCmd
		clusterId         strfmt.UUID
		stepReply         *models.Step
		stepErr           error
		ctrl              *gomock.Controller
		mockValidator     *hardware.MockValidator
		instructionConfig InstructionConfig
		disks             []*models.Disk
	)
	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		instructionConfig = InstructionConfig{
			InventoryURL:   "10.35.59.36",
			InventoryPort:  "30485",
			InstallerImage: "quay.io/ocpmetal/assisted-installer:latest",
		}
		installCmd = NewInstallCmd(getTestLog(), db, mockValidator, instructionConfig)
		cluster = createClusterInDb(db)
		clusterId = *cluster.ID
		host = createHostInDb(db, clusterId, models.HostRoleMaster, false)
		validDiskSize := int64(128849018880)
		disks = []*models.Disk{
			{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sda", SizeBytes: validDiskSize},
			{DriveType: "HDD", Name: "sdh", SizeBytes: validDiskSize},
		}

	})

	It("get_step_one_master", func() {
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, errors.New("error")).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(true, true, stepReply, stepErr, "")
	})

	It("get_step_one_master_no_disks", func() {
		var emptydisks []*models.Disk
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(emptydisks, nil).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(true, true, stepReply, stepErr, "")
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(false, false, stepReply, stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply, models.HostRoleMaster, string(clusterId), string(*host.ID))
		Expect(getHost(*host.ID, clusterId, db).InstallerVersion).
			To(Equal("quay.io/ocpmetal/assisted-installer:latest"))
	})

	It("get_step_three_master_success", func() {

		host2 := createHostInDb(db, clusterId, models.HostRoleMaster, false)
		host3 := createHostInDb(db, clusterId, models.HostRoleMaster, true)
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(3)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(false, false, stepReply, stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply, models.HostRoleMaster, string(clusterId), string(*host.ID))
		stepReply, stepErr = installCmd.GetStep(ctx, &host2)
		postvalidation(false, false, stepReply, stepErr, models.HostRoleMaster)
		validateInstallCommand(stepReply, models.HostRoleMaster, string(clusterId), string(*host2.ID))
		stepReply, stepErr = installCmd.GetStep(ctx, &host3)
		postvalidation(false, false, stepReply, stepErr, models.HostRoleBootstrap)
		validateInstallCommand(stepReply, models.HostRoleBootstrap, string(clusterId), string(*host3.ID))
	})

	AfterEach(func() {

		// cleanup
		db.Close()
		ctrl.Finish()
		stepReply = nil
		stepErr = nil
	})
})

func createClusterInDb(db *gorm.DB) common.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		ID:               &clusterId,
		OpenshiftVersion: "4.5",
	}}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
}

func createHostInDb(db *gorm.DB, clusterId strfmt.UUID, role models.HostRole, bootstrap bool) models.Host {
	id := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:           &id,
		ClusterID:    clusterId,
		Status:       swag.String(HostStatusDiscovering),
		Role:         role,
		Bootstrap:    bootstrap,
		HardwareInfo: defaultHwInfo,
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

func validateInstallCommand(reply *models.Step, role models.HostRole, clusterId string, hostId string) {
	installCommand := "sudo podman run -v /dev:/dev:rw -v /opt:/opt:rw --privileged --pid=host " +
		"--net=host -v /var/log:/var/log:rw " +
		"--name assisted-installer quay.io/ocpmetal/assisted-installer:latest --role %s " +
		"--cluster-id %s --host 10.35.59.36 --port 30485 " +
		"--boot-device /dev/sdb --host-id %s --openshift-version 4.5"
	ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId, hostId)))
	ExpectWithOffset(1, reply.StepType).To(Equal(models.StepTypeInstall))
}

func TestEvents(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "installcmd test Suite")
}
