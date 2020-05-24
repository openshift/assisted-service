package host

import (
	"context"
	"fmt"
	"strings"

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
		cluster           models.Cluster
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
			InstallerImage: "quay.io/ocpmetal/assisted-installer:stable",
		}
		installCmd = NewInstallCmd(getTestLog(), db, mockValidator, instructionConfig)
		cluster = createClusterInDb(db)
		clusterId = *cluster.ID
		host = createHostInDb(db, clusterId, RoleMaster, false)
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
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
		validateInstallCommand(stepReply, RoleMaster, string(clusterId), string(*host.ID))
	})

	It("get_step_three_master_success", func() {

		host2 := createHostInDb(db, clusterId, RoleMaster, false)
		host3 := createHostInDb(db, clusterId, RoleMaster, true)
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(3)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
		validateInstallCommand(stepReply, RoleMaster, string(clusterId), string(*host.ID))
		stepReply, stepErr = installCmd.GetStep(ctx, &host2)
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
		validateInstallCommand(stepReply, RoleMaster, string(clusterId), string(*host2.ID))
		stepReply, stepErr = installCmd.GetStep(ctx, &host3)
		postvalidation(false, false, stepReply, stepErr, RoleBootstrap)
		validateInstallCommand(stepReply, RoleBootstrap, string(clusterId), string(*host3.ID))
	})

	AfterEach(func() {

		// cleanup
		db.Close()
		ctrl.Finish()
		stepReply = nil
		stepErr = nil
	})
})

func createClusterInDb(db *gorm.DB) models.Cluster {
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := models.Cluster{
		ID:               &clusterId,
		OpenshiftVersion: "4.5",
	}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
}

func createHostInDb(db *gorm.DB, clusterId strfmt.UUID, role string, bootstrap bool) models.Host {
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

func postvalidation(isstepreplynil bool, issteperrnil bool, expectedstepreply *models.Step, expectedsteperr error, expectedrole string) {
	if issteperrnil {
		ExpectWithOffset(1, expectedsteperr).Should(HaveOccurred())
	} else {
		ExpectWithOffset(1, expectedsteperr).ShouldNot(HaveOccurred())
	}
	if isstepreplynil {
		ExpectWithOffset(1, expectedstepreply).Should(BeNil())
	} else {
		ExpectWithOffset(1, expectedstepreply.StepType).To(Equal(models.StepTypeExecute))
		ExpectWithOffset(1, strings.Contains(expectedstepreply.Args[1], expectedrole)).To(Equal(true))
	}
}

func validateInstallCommand(reply *models.Step, role string, clusterId string, hostId string) {
	installCommand := "sudo podman run -v /dev:/dev:rw -v /opt:/opt:rw --privileged --pid=host --net=host " +
		"--name assisted-installer quay.io/ocpmetal/assisted-installer:stable --role %s " +
		"--cluster-id %s --host 10.35.59.36 --port 30485 " +
		"--boot-device /dev/sdb --host-id %s --openshift-version 4.5"
	ExpectWithOffset(1, reply.Args[1]).Should(Equal(fmt.Sprintf(installCommand, role, clusterId, hostId)))
}
