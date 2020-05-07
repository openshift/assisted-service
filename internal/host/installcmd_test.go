package host

import (
	"context"

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
	funk "github.com/thoas/go-funk"
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
		disks             []*models.BlockDevice
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
		disks = []*models.BlockDevice{
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdb", Size: validDiskSize},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sda", Size: validDiskSize},
			{DeviceType: "disk", Fstype: "iso9660", MajorDeviceNumber: 11, Mountpoint: "/test", Name: "sdh", Size: validDiskSize},
		}

	})

	It("get_step_one_master", func() {
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(nil, errors.New("error")).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(true, true, stepReply, stepErr, "")
	})

	It("get_step_one_master_no_disks", func() {
		var emptydisks []*models.BlockDevice
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(emptydisks, nil).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(true, true, stepReply, stepErr, "")
	})

	It("get_step_one_master_success", func() {
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(1)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
	})

	It("get_step_three_master_success", func() {

		host2 := createHostInDb(db, clusterId, RoleMaster, false)
		host3 := createHostInDb(db, clusterId, RoleMaster, true)
		mockValidator.EXPECT().GetHostValidDisks(gomock.Any()).Return(disks, nil).Times(3)
		stepReply, stepErr = installCmd.GetStep(ctx, &host)
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
		stepReply, stepErr = installCmd.GetStep(ctx, &host2)
		postvalidation(false, false, stepReply, stepErr, RoleMaster)
		stepReply, stepErr = installCmd.GetStep(ctx, &host3)
		postvalidation(false, false, stepReply, stepErr, RoleBootstrap)
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
		Base: models.Base{
			ID: &clusterId,
		},
	}
	Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	return cluster
}

func createHostInDb(db *gorm.DB, clusterId strfmt.UUID, role string, bootstrap bool) models.Host {
	id := strfmt.UUID(uuid.New().String())
	host := models.Host{
		Base: models.Base{
			ID: &id,
		},
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
		Expect(expectedsteperr).Should(HaveOccurred())
	} else {
		Expect(expectedsteperr).ShouldNot(HaveOccurred())
	}
	if isstepreplynil {
		Expect(expectedstepreply).Should(BeNil())
	} else {
		Expect(expectedstepreply.StepType).To(Equal(models.StepTypeExecute))
		Expect(funk.Contains(expectedstepreply.Args, expectedrole)).To(Equal(true))
	}
}
