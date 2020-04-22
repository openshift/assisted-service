package host

import (
	"context"
	"io/ioutil"
	"testing"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("statemachine", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		ctrl          *gomock.Controller
		mockValidator *hardware.MockValidator
		state         API
		host          models.Host
		stateReply    *UpdateReply
		stateErr      error
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockValidator = hardware.NewMockValidator(ctrl)
		state = NewState(getTestLog(), db, mockValidator)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
			Status:       swag.String("unknown invalid state"),
			HardwareInfo: defaultHwInfo,
		}
	})

	Context("unknown_host_state", func() {
		It("register_host", func() {
			stateReply, stateErr = state.RegisterHost(ctx, &host)
		})

		It("enable_host", func() {
			stateReply, stateErr = state.EnableHost(ctx, &host)
		})

		It("disable_host", func() {
			stateReply, stateErr = state.DisableHost(ctx, &host)
		})

		It("update role", func() {
			stateReply, stateErr = state.UpdateRole(ctx, &host, "master", nil)
		})

		It("install", func() {
			stateReply, stateErr = state.Install(ctx, &host, nil)
		})

		It("update_hw_info", func() {
			stateReply, stateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
		})

		It("update_hw_info", func() {
			stateReply, stateErr = state.RefreshStatus(ctx, &host)
		})

		AfterEach(func() {
			Expect(stateReply).To(BeNil())
			Expect(stateErr).Should(HaveOccurred())
		})
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})
})
var defaultHwInfo = "default hw info" // invalid hw info used only for tests

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "host state machine tests")
}

func getHost(hostId, clusterId strfmt.UUID, db *gorm.DB) *models.Host {
	var host models.Host
	Expect(db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error).ShouldNot(HaveOccurred())
	return &host
}

func prepareDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", ":memory:")
	Expect(err).ShouldNot(HaveOccurred())
	//db = db.Debug()
	db.AutoMigrate(&models.Host{})
	return db
}

type expect struct {
	expectError   bool
	expectedState string
	postCheck     func()
}

func postValidation(expectedReply *expect, firstState string, db *gorm.DB, id, clusterId strfmt.UUID,
	updateReply *UpdateReply, updateErr error) {
	if expectedReply != nil {
		h := getHost(id, clusterId, db)
		if expectedReply.expectError {
			Expect(updateReply).To(BeNil())
			Expect(updateErr).Should(HaveOccurred())
			Expect(swag.StringValue(h.Status)).Should(Equal(firstState))
		} else {
			Expect(updateErr).ShouldNot(HaveOccurred())
			Expect(updateReply).NotTo(BeNil())
			Expect(updateReply.State).Should(Equal(expectedReply.expectedState))
			if updateReply.State == firstState {
				Expect(updateReply.IsChanged).Should(BeFalse())
			} else {
				Expect(updateReply.IsChanged).Should(BeTrue())
			}
			Expect(swag.StringValue(h.Status)).Should(Equal(expectedReply.expectedState))
		}

		if expectedReply.postCheck != nil {
			expectedReply.postCheck()
		}
	}
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}
