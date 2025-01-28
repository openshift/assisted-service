package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("disk_performance", func() {
	ctx := context.Background()
	var host models.Host
	var db *gorm.DB
	var dCmd *diskPerfCheckCmd
	var id, clusterId, infraEnvId strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string
	var ctrl *gomock.Controller
	var mockValidator *hardware.MockValidator

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		dCmd = NewDiskPerfCheckCmd(common.GetTestLog(), "quay.io/example/agent:latest", mockValidator, 600)

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvId, clusterId, models.HostStatusPreparingForInstallation)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	It("happy flow", func() {
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeInstallationDiskSpeedCheck))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("Already tested", func() {
		host.DisksInfo, stepErr = common.SetDiskSpeed("/dev/sda", 10, 0, "")
		Expect(stepErr).ToNot(HaveOccurred())
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepErr).ToNot(HaveOccurred())
		Expect(stepReply).To(BeNil())
	})

	It("returns no steps when boot device is persistent", func() {
		inventory := &models.Inventory{Boot: &models.Boot{DeviceType: models.BootDeviceTypePersistent}}
		invBytes, err := json.Marshal(inventory)
		Expect(err).NotTo(HaveOccurred())
		host.Inventory = string(invBytes)
		steps, err := dCmd.GetSteps(ctx, &host)
		Expect(steps).To(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
