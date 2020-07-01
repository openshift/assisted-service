package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/filanov/bm-inventory/internal/validators"

	"github.com/filanov/bm-inventory/internal/connectivity"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/pkg/errors"

	"github.com/filanov/bm-inventory/internal/common"
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

var defaultHwInfo = "default hw info"                                 // invalid hw info used only for tests
var defaultInventoryS = "default inventory"                           // invalid inventory info used only for tests
var defaultProgressStage = models.HostStage("default progress stage") // invalid progress stage used only for tests

var _ = Describe("statemachine", func() {
	var (
		ctx                       = context.Background()
		db                        *gorm.DB
		ctrl                      *gomock.Controller
		mockHwValidator           *hardware.MockValidator
		mockConnectivityValidator *connectivity.MockValidator
		state                     API
		host                      models.Host
		stateReply                *UpdateReply
		stateErr                  error
		mockEvents                *events.MockHandler
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockConnectivityValidator = connectivity.NewMockValidator(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		state = NewManager(getTestLog(), db, mockEvents, mockHwValidator, nil, mockConnectivityValidator)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, "unknown invalid state")
	})

	Context("unknown_host_state", func() {

		It("update_hw_info", func() {
			stateReply, stateErr = state.RefreshStatus(ctx, &host, nil)
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

var _ = Describe("update_role", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		id, clusterID strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(getTestLog(), db, nil, nil, nil, nil)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
	})

	Context("update role by src state", func() {
		success := func(srcState string) {
			host = getTestHost(id, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).ShouldNot(HaveOccurred())
			h := getHost(id, clusterID, db)
			Expect(h.Role).To(Equal(models.HostRoleMaster))
		}

		failure := func(srcState string) {
			host = getTestHost(id, clusterID, srcState)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).To(HaveOccurred())
			h := getHost(id, clusterID, db)
			Expect(h.Role).To(Equal(models.HostRoleWorker))
		}

		tests := []struct {
			name     string
			srcState string
			testFunc func(srcState string)
		}{
			{
				name:     "discovering",
				srcState: HostStatusDiscovering,
				testFunc: success,
			},
			{
				name:     "known",
				srcState: HostStatusKnown,
				testFunc: success,
			},
			{
				name:     "disconnected",
				srcState: HostStatusDisconnected,
				testFunc: success,
			},
			{
				name:     "insufficient",
				srcState: HostStatusInsufficient,
				testFunc: success,
			},
			{
				name:     "disabled",
				srcState: HostStatusDisabled,
				testFunc: failure,
			},
			{
				name:     "error",
				srcState: HostStatusError,
				testFunc: failure,
			},
			{
				name:     "installing",
				srcState: HostStatusInstalling,
				testFunc: failure,
			},
			{
				name:     "installed",
				srcState: HostStatusInstalled,
				testFunc: failure,
			},
			{
				name:     "installing-in-progress",
				srcState: HostStatusInstallingInProgress,
				testFunc: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				t.testFunc(t.srcState)
			})
		}
	})

	It("update role with transaction", func() {
		host = getTestHost(id, clusterID, HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		By("rollback transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Rollback().Error).ShouldNot(HaveOccurred())
			h := getHost(id, clusterID, db)
			Expect(h.Role).Should(Equal(models.HostRoleWorker))
		})
		By("commit transaction", func() {
			tx := db.Begin()
			Expect(tx.Error).ShouldNot(HaveOccurred())
			Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, tx)).NotTo(HaveOccurred())
			Expect(tx.Commit().Error).ShouldNot(HaveOccurred())
			h := getHost(id, clusterID, db)
			Expect(h.Role).Should(Equal(models.HostRoleMaster))
		})
	})

	It("update role master to worker", func() {
		host = getTestHost(id, clusterID, HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(state.UpdateRole(ctx, &host, models.HostRoleMaster, nil)).NotTo(HaveOccurred())
		h := getHost(id, clusterID, db)
		Expect(h.Role).To(Equal(models.HostRoleMaster))
		Expect(state.UpdateRole(ctx, &host, models.HostRoleWorker, nil)).NotTo(HaveOccurred())
		h = getHost(id, clusterID, db)
		Expect(h.Role).To(Equal(models.HostRoleWorker))
	})
})

var _ = Describe("update_progress", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		host       models.Host
		ctrl       *gomock.Controller
		mockEvents *events.MockHandler
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		state = NewManager(getTestLog(), db, mockEvents, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, "")
	})

	Context("installaing host", func() {
		var (
			stage        models.HostStage
			hostProgress models.HostProgress
			progress     *models.HostInstallProgressParams
		)

		BeforeEach(func() {
			progress = &models.HostInstallProgressParams{}
			host.Status = swag.String(HostStatusInstalling)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("some_progress", func() {
			progress.CurrentStage = defaultProgressStage
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusInstallingInProgress))
			Expect(*h.StatusInfo).Should(Equal(string(defaultProgressStage)))

			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).ToNot(HaveOccurred())
			Expect(hostProgress.CurrentStage).Should(Equal(defaultProgressStage))
		})

		It("writing to disk", func() {
			stage = models.HostStageWritingImageToDisk
			progress.CurrentStage = stage
			progress.ProgressInfo = "20%"
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusInstallingInProgress))
			Expect(*h.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", stage, progress.ProgressInfo)))

			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).ToNot(HaveOccurred())
			Expect(hostProgress.CurrentStage).Should(Equal(stage))
		})

		It("done", func() {
			stage = models.HostStageDone
			progress.CurrentStage = stage
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusInstalled))
			Expect(*h.StatusInfo).Should(Equal(string(stage)))

			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).ToNot(HaveOccurred())
			Expect(hostProgress.CurrentStage).Should(Equal(stage))
		})

		It("progress_failed", func() {
			stage = models.HostStageFailed
			progress.CurrentStage = stage
			progress.ProgressInfo = "reason"
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusError))
			Expect(*h.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", stage, progress.ProgressInfo)))
			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).To(HaveOccurred())
		})

		It("progress_failed_empty_reason", func() {
			stage = models.HostStageFailed
			progress.CurrentStage = stage
			progress.ProgressInfo = ""
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusError))
			Expect(*h.StatusInfo).Should(Equal(string(stage)))
			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).To(HaveOccurred())
		})

		It("progress_failed_after_a_stage", func() {
			// Some stage
			stage = models.HostStageWritingImageToDisk
			progress.CurrentStage = stage
			progress.ProgressInfo = "20%"
			Expect(state.UpdateInstallProgress(ctx, &host, progress)).ShouldNot(HaveOccurred())
			h := getHost(*host.ID, host.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusInstallingInProgress))
			Expect(*h.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", stage, progress.ProgressInfo)))

			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).ToNot(HaveOccurred())
			Expect(hostProgress.CurrentStage).Should(Equal(stage))

			// Failed
			stage = models.HostStageFailed
			progress.CurrentStage = stage
			progress.ProgressInfo = "reason"
			Expect(state.UpdateInstallProgress(ctx, h, progress)).ShouldNot(HaveOccurred())
			h = getHost(*h.ID, h.ClusterID, db)
			Expect(*h.Status).Should(Equal(HostStatusError))
			Expect(*h.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", stage, progress.ProgressInfo)))

			Expect(json.Unmarshal([]byte(h.Progress), &hostProgress)).ToNot(HaveOccurred())
			Expect(hostProgress.CurrentStage).ShouldNot(Equal(defaultProgressStage))
		})
	})

	It("invalid state", func() {
		Expect(state.UpdateInstallProgress(ctx, &host,
			&models.HostInstallProgressParams{CurrentStage: defaultProgressStage})).Should(HaveOccurred())
	})
})

var _ = Describe("monitor_disconnection", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		host       models.Host
		ctrl       *gomock.Controller
		mockEvents *events.MockHandler
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		state = NewManager(getTestLog(), db, mockEvents, nil, nil, nil)
		host = getTestHost(strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String()), HostStatusDiscovering)
		err := state.RegisterHost(ctx, &host)
		Expect(err).ShouldNot(HaveOccurred())
		db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
	})

	Context("host_disconnecting", func() {
		It("known_host_disconnects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(HostStatusKnown)
			db.Save(&host)
		})

		It("discovering_host_disconnects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(HostStatusDiscovering)
			db.Save(&host)
		})

		It("known_host_insufficient", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now().Add(-4 * time.Minute))
			host.Status = swag.String(HostStatusInsufficient)
			db.Save(&host)
		})

		AfterEach(func() {
			state.HostMonitoring()
			db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
			Expect(*host.Status).Should(Equal(HostStatusDisconnected))
		})
	})

	Context("host_reconnecting", func() {
		It("host_connects", func() {
			host.CheckedInAt = strfmt.DateTime(time.Now())
			host.Status = swag.String(HostStatusDisconnected)
			db.Save(&host)
		})

		AfterEach(func() {
			state.HostMonitoring()
			db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
			Expect(*host.Status).Should(Equal(HostStatusDiscovering))
		})
	})

	AfterEach(func() {
		db.Close()
	})
})

var _ = Describe("cancel_installation", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		h          models.Host
		ctrl       *gomock.Controller
		mockEvents *events.MockHandler
	)

	BeforeEach(func() {
		db = prepareDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		state = NewManager(getTestLog(), db, mockEvents, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		h = getTestHost(id, clusterId, HostStatusDiscovering)
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			h.Status = swag.String(HostStatusInstalling)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
		})

		It("cancel_failed_installation", func() {
			h.Status = swag.String(HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(HostStatusError))
		})
	})

	Context("invalid_cancel_installation", func() {
		It("nothing_to_cancel", func() {
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).Should(HaveOccurred())
		})
	})

	AfterEach(func() {
		db.Close()
	})
})

var _ = Describe("reset_host", func() {
	var (
		ctx   = context.Background()
		db    *gorm.DB
		state API
		h     models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(getTestLog(), db, nil, nil, nil, nil)
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = getTestHost(id, clusterId, HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.ResetHost(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(HostStatusResetting))
		})

		It("register resetting host", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = getTestHost(id, clusterId, HostStatusResetting)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.RegisterHost(ctx, &h)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(HostStatusDiscovering))
		})
	})

	Context("invalid_reset_installation", func() {
		It("nothing_to_reset", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = getTestHost(id, clusterId, HostStatusDiscovering)
			reply := state.ResetHost(ctx, &h, "some reason", db)
			Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
		})
	})

	AfterEach(func() {
		db.Close()
	})
})

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
	// db = db.Debug()
	db.AutoMigrate(&models.Host{}, &common.Cluster{})
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
				Expect(h.StatusInfo).ShouldNot(BeNil())
				Expect(h.StatusUpdatedAt).ShouldNot(BeNil())
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

func getTestHost(hostID, clusterID strfmt.UUID, state string) models.Host {
	return models.Host{
		ID:           &hostID,
		ClusterID:    clusterID,
		Status:       swag.String(state),
		HardwareInfo: defaultHwInfo,
		Inventory:    defaultInventory(),
		Role:         models.HostRoleWorker,
		CheckedInAt:  strfmt.DateTime(time.Now()),
	}
}

func mockConnectivityAndHwValidators(h *models.Host, mockHWValidator *hardware.MockValidator, mockConnectivityValidator *connectivity.MockValidator, hwError, sufficientHw, sufficientConnectivity bool) string {
	var statusInfoDetails = make(map[string]string)
	roleReply := isSufficientRole(h)
	statusInfoDetails[roleReply.Type] = roleReply.Reason
	if hwError {
		mockHWValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("error")).AnyTimes()
		statusInfoDetails["hardware"] = "parsing error"
	} else if sufficientHw {
		mockHWValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
			Return(&validators.IsSufficientReply{Type: "hardware", IsSufficient: true}, nil).AnyTimes()
		statusInfoDetails["hardware"] = ""
	} else {
		//insufficient hw
		mockHWValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
			Return(&validators.IsSufficientReply{Type: "hardware", IsSufficient: false, Reason: "failed reason"}, nil).AnyTimes()
		statusInfoDetails["hardware"] = "failed reason"
	}
	if sufficientConnectivity {
		mockConnectivityValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
			Return(&validators.IsSufficientReply{Type: "connectivity", IsSufficient: true}, nil).AnyTimes()
		statusInfoDetails["connectivity"] = ""
	} else {
		//insufficient connectivity
		mockConnectivityValidator.EXPECT().IsSufficient(gomock.Any(), gomock.Any()).
			Return(&validators.IsSufficientReply{Type: "connectivity", IsSufficient: false, Reason: "failed reason"}, nil).AnyTimes()
		statusInfoDetails["connectivity"] = "failed reason"
	}

	if !hwError && sufficientHw && sufficientConnectivity && roleReply.IsSufficient {
		return ""
	}
	statusInfo, err := json.Marshal(statusInfoDetails)
	if err != nil {
		return ""
	}
	return string(statusInfo)
}

func defaultInventory() string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func addTestCluster(clusterID strfmt.UUID, apiVip, ingressVip string, machineCidr string, db *gorm.DB) {
	cluster := models.Cluster{
		ID:                 &clusterID,
		APIVip:             apiVip,
		IngressVip:         ingressVip,
		MachineNetworkCidr: machineCidr,
	}
	Expect(db.Create(&cluster).Error).To(Not(HaveOccurred()))
}

var _ = Describe("UpdateInventory", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})
	Context("enable host", func() {
		newInventory := "new inventory stuff"
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(h.Inventory).To(Equal(newInventory))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(h.Inventory).To(Equal(defaultInventoryS))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: success,
			},
			{
				name:       models.HostStatusDisabled,
				srcState:   models.HostStatusDisabled,
				validation: failure,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: success,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: success,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				validation: success,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, t.srcState)
				host.Inventory = defaultInventoryS

				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateInventory(ctx, &host, newInventory))
			})
		}
	})
	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("UpdateHwInfo", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("enable host", func() {
		newHWInfo := "new hardware info"
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(h.HardwareInfo).To(Equal(newHWInfo))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(h.HardwareInfo).To(Equal(defaultHwInfo))
		}

		tests := []struct {
			name       string
			srcState   string
			validation func(error)
		}{
			{
				name:       models.HostStatusKnown,
				srcState:   models.HostStatusKnown,
				validation: success,
			},
			{
				name:       models.HostStatusDisabled,
				srcState:   models.HostStatusDisabled,
				validation: failure,
			},
			{
				name:       models.HostStatusDisconnected,
				srcState:   models.HostStatusDisconnected,
				validation: success,
			},
			{
				name:       models.HostStatusDiscovering,
				srcState:   models.HostStatusDiscovering,
				validation: success,
			},
			{
				name:       models.HostStatusError,
				srcState:   models.HostStatusError,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalled,
				srcState:   models.HostStatusInstalled,
				validation: failure,
			},
			{
				name:       models.HostStatusInstalling,
				srcState:   models.HostStatusInstalling,
				validation: failure,
			},
			{
				name:       models.HostStatusInstallingInProgress,
				srcState:   models.HostStatusInstallingInProgress,
				validation: failure,
			},
			{
				name:       models.HostStatusInsufficient,
				srcState:   models.HostStatusInsufficient,
				validation: success,
			},
			{
				name:       models.HostStatusResetting,
				srcState:   models.HostStatusResetting,
				validation: failure,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, t.srcState)
				host.HardwareInfo = defaultHwInfo
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateHwInfo(ctx, &host, newHWInfo))
			})
		}
	})

	AfterEach(func() {
		_ = db.Close()
	})
})

var _ = Describe("SetBootstrap", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
	)

	BeforeEach(func() {
		db = prepareDB()
		hapi = NewManager(getTestLog(), db, nil, nil, nil, nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())

		host = getTestHost(hostId, clusterId, HostStatusResetting)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		h := getHost(*host.ID, host.ClusterID, db)
		Expect(h.Bootstrap).Should(Equal(false))
	})

	tests := []struct {
		IsBootstrap bool
	}{
		{
			IsBootstrap: true,
		},
		{
			IsBootstrap: false,
		},
	}

	for i := range tests {
		t := tests[i]
		It(fmt.Sprintf("Boostrap %s", strconv.FormatBool(t.IsBootstrap)), func() {
			Expect(hapi.SetBootstrap(ctx, &host, t.IsBootstrap, db)).ShouldNot(HaveOccurred())

			h := getHost(*host.ID, host.ClusterID, db)
			Expect(h.Bootstrap).Should(Equal(t.IsBootstrap))
		})
	}

	AfterEach(func() {
		_ = db.Close()
	})
})
