package bminventory

import (
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"

	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/job"
	"github.com/filanov/bm-inventory/restapi/operations/inventory"
	"github.com/golang/mock/gomock"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "inventory_test")
}

func prepareDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", ":memory:")
	Expect(err).ShouldNot(HaveOccurred())
	//db = db.Debug()
	db.AutoMigrate(&models.Cluster{}, &models.Host{})
	return db
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

var _ = Describe("GenerateClusterISO", func() {
	var (
		bm      *bareMetalInventory
		cfg     Config
		db      *gorm.DB
		ctx     = context.Background()
		ctrl    *gomock.Controller
		mockJob *job.MockAPI
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockJob = job.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), nil, cfg, mockJob)
	})

	registerCluster := func() *models.Cluster {
		reply := bm.RegisterCluster(ctx, inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{Name: swag.String("some-cluster")},
		})
		Expect(reply).Should(BeAssignableToTypeOf(inventory.NewRegisterClusterCreated()))
		return reply.(*inventory.RegisterClusterCreated).Payload
	}

	It("success", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), defaultJobNamespace).Return(nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, inventory.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(inventory.NewGenerateClusterISOCreated()))
	})

	It("cluster_not_exists", func() {
		generateReply := bm.GenerateClusterISO(ctx, inventory.GenerateClusterISOParams{
			ClusterID:         strfmt.UUID(uuid.New().String()),
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(inventory.NewGenerateClusterISONotFound()))
	})

	It("failed_to_create_job", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, inventory.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(inventory.NewGenerateClusterISOInternalServerError()))
	})

	It("job_failed", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), defaultJobNamespace).Return(fmt.Errorf("error")).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, inventory.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(inventory.NewGenerateClusterISOInternalServerError()))
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})

})

var _ = Describe("GetNextSteps", func() {
	var (
		bm          *bareMetalInventory
		cfg         Config
		db          *gorm.DB
		ctx         = context.Background()
		ctrl        *gomock.Controller
		mockHostApi *host.MockAPI
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockHostApi = host.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, cfg, nil)
	})

	It("get_next_steps_unknown_host", func() {
		clusterId := strToUUID(uuid.New().String())
		unregistered_hostID := strToUUID(uuid.New().String())

		generateReply := bm.GetNextSteps(ctx, inventory.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *unregistered_hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(inventory.NewGetNextStepsNotFound()))
	})

	It("get_next_steps_success", func() {
		clusterId := strToUUID(uuid.New().String())
		hostId := strToUUID(uuid.New().String())
		host := models.Host{
			Base: models.Base{
				ID: hostId,
			},
			ClusterID: *clusterId,
			Status:    swag.String("discovering"),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		var err error
		expectedStepsReply := models.Steps{&models.Step{StepType: models.StepTypeHardwareInfo},
			&models.Step{StepType: models.StepTypeConnectivityCheck}}
		mockHostApi.EXPECT().GetNextSteps(gomock.Any(), gomock.Any()).Return(expectedStepsReply, err)
		reply := bm.GetNextSteps(ctx, inventory.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *hostId,
		})
		Expect(reply).Should(BeAssignableToTypeOf(inventory.NewGetNextStepsOK()))
		stepsReply := reply.(*inventory.GetNextStepsOK).Payload
		expectedStepsType := []models.StepType{models.StepTypeHardwareInfo, models.StepTypeConnectivityCheck}
		Expect(stepsReply).To(HaveLen(len(expectedStepsType)))
		for i, step := range stepsReply {
			Expect(step.StepType).Should(Equal(expectedStepsType[i]))
		}
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})

})
