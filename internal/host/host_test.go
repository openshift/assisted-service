package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/metrics"

	"github.com/filanov/bm-inventory/internal/events"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var defaultHwInfo = "default hw info"                                 // invalid hw info used only for tests
var defaultInventoryS = "default inventory"                           // invalid inventory info used only for tests
var defaultProgressStage = models.HostStage("default progress stage") // invalid progress stage used only for tests

var _ = Describe("update_role", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		host          models.Host
		id, clusterID strfmt.UUID
		dbName        = "update_role"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		state = NewManager(getTestLog(), db, nil, nil, nil, createValidatorCfg(), nil)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
		mockMetric *metrics.MockAPI
		dbName     = "host_update_progress"
	)

	setDefaultReportHostInstallationMetrics := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	}

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		state = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), mockMetric)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, "")
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("installaing host", func() {
		var (
			progress   models.HostProgress
			hostFromDB *models.Host
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockMetric = metrics.NewMockAPI(ctrl)
			setDefaultReportHostInstallationMetrics(mockMetric)
			host.Status = swag.String(HostStatusInstalling)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		})

		Context("positive stages", func() {
			It("some_progress", func() {
				progress.CurrentStage = defaultProgressStage
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = getHost(*host.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(HostStatusInstallingInProgress))
			})

			It("writing to disk", func() {
				progress.CurrentStage = models.HostStageWritingImageToDisk
				progress.ProgressInfo = "20%"
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = getHost(*host.ID, host.ClusterID, db)

				Expect(*hostFromDB.Status).Should(Equal(HostStatusInstallingInProgress))
			})

			It("done", func() {
				progress.CurrentStage = models.HostStageDone
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = getHost(*host.ID, host.ClusterID, db)

				Expect(*hostFromDB.Status).Should(Equal(HostStatusInstalled))
			})

			AfterEach(func() {
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))
				Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
				Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
			})
		})

		Context("Negative stages", func() {
			It("progress_failed", func() {
				progress.CurrentStage = models.HostStageFailed
				progress.ProgressInfo = "reason"
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = getHost(*host.ID, host.ClusterID, db)

				Expect(*hostFromDB.Status).Should(Equal(HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", progress.CurrentStage, progress.ProgressInfo)))
			})

			It("progress_failed_empty_reason", func() {
				progress.CurrentStage = models.HostStageFailed
				progress.ProgressInfo = ""
				Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
				hostFromDB = getHost(*host.ID, host.ClusterID, db)
				Expect(*hostFromDB.Status).Should(Equal(HostStatusError))
				Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))
			})

			It("progress_failed_after_a_stage", func() {
				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					hostFromDB = getHost(*host.ID, host.ClusterID, db)
					Expect(*hostFromDB.Status).Should(Equal(HostStatusInstallingInProgress))
					Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				})

				By("Failed", func() {
					newProgress := models.HostProgress{
						CurrentStage: models.HostStageFailed,
						ProgressInfo: "reason",
					}
					Expect(state.UpdateInstallProgress(ctx, hostFromDB, &newProgress)).ShouldNot(HaveOccurred())
					hostFromDB = getHost(*host.ID, host.ClusterID, db)
					Expect(*hostFromDB.Status).Should(Equal(HostStatusError))
					Expect(*hostFromDB.StatusInfo).Should(Equal(fmt.Sprintf("%s - %s", newProgress.CurrentStage, newProgress.ProgressInfo)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				})
			})

			It("lower_stage", func() {
				verifyDb := func() {
					hostFromDB = getHost(*host.ID, host.ClusterID, db)
					Expect(*hostFromDB.Status).Should(Equal(HostStatusInstallingInProgress))
					Expect(*hostFromDB.StatusInfo).Should(Equal(string(progress.CurrentStage)))

					Expect(hostFromDB.Progress.CurrentStage).Should(Equal(progress.CurrentStage))
					Expect(hostFromDB.Progress.ProgressInfo).Should(Equal(progress.ProgressInfo))
				}

				By("Some stage", func() {
					progress.CurrentStage = models.HostStageWritingImageToDisk
					progress.ProgressInfo = "20%"
					mockMetric.EXPECT().ReportHostInstallationMetrics(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
					Expect(state.UpdateInstallProgress(ctx, &host, &progress)).ShouldNot(HaveOccurred())
					verifyDb()
				})

				By("Lower stage", func() {
					newProgress := models.HostProgress{
						CurrentStage: models.HostStageInstalling,
					}
					Expect(state.UpdateInstallProgress(ctx, hostFromDB, &newProgress)).Should(HaveOccurred())
					verifyDb()
				})
			})
		})
	})

	It("invalid stage", func() {
		Expect(state.UpdateInstallProgress(ctx, &host,
			&models.HostProgress{CurrentStage: defaultProgressStage})).Should(HaveOccurred())
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
		dbName     = "monitor_disconnection"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return().AnyTimes()
		state = NewManager(getTestLog(), db, mockEvents, nil, nil, createValidatorCfg(), nil)
		clusterID := strfmt.UUID(uuid.New().String())
		host = getTestHost(strfmt.UUID(uuid.New().String()), clusterID, HostStatusDiscovering)
		cluster := getTestCluster(clusterID, "1.1.0.0/16")
		Expect(db.Save(&cluster).Error).ToNot(HaveOccurred())
		host.Inventory = workerInventory()
		err := state.RegisterHost(ctx, &host)
		Expect(err).ShouldNot(HaveOccurred())
		db.First(&host, "id = ? and cluster_id = ?", host.ID, host.ClusterID)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
			host.Inventory = ""
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
		ctrl.Finish()
		db.Close()
	})
})

var _ = Describe("cancel_installation", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		h             models.Host
		eventsHandler events.Handler
		dbName        = "cancel_installation"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		state = NewManager(getTestLog(), db, eventsHandler, nil, nil, nil, nil)
		id := strfmt.UUID(uuid.New().String())
		clusterId := strfmt.UUID(uuid.New().String())
		h = getTestHost(id, clusterId, HostStatusDiscovering)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			h.Status = swag.String(HostStatusInstalling)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ID.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation canceled for host %s", state.GetHostname(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		It("cancel_failed_installation", func() {
			h.Status = swag.String(HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ID.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation canceled for host %s", state.GetHostname(&h))
			Expect(*cancelEvent.Message).Should(Equal(eventMessage))
		})

		AfterEach(func() {
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(HostStatusError))
		})
	})

	Context("invalid_cancel_installation", func() {
		It("nothing_to_cancel", func() {
			Expect(state.CancelInstallation(ctx, &h, "some reason", db)).Should(HaveOccurred())
			events, err := eventsHandler.GetEvents(h.ID.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})
})

var _ = Describe("reset_host", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		h             models.Host
		eventsHandler events.Handler
		dbName        = "reset_host"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		state = NewManager(getTestLog(), db, eventsHandler, nil, nil, nil, nil)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("reset_installation", func() {
		It("reset_installation", func() {
			id := strfmt.UUID(uuid.New().String())
			clusterId := strfmt.UUID(uuid.New().String())
			h = getTestHost(id, clusterId, HostStatusError)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			Expect(state.ResetHost(ctx, &h, "some reason", db)).ShouldNot(HaveOccurred())
			db.First(&h, "id = ? and cluster_id = ?", h.ID, h.ClusterID)
			Expect(*h.Status).Should(Equal(HostStatusResetting))
			events, err := eventsHandler.GetEvents(h.ID.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
			eventMessage := fmt.Sprintf("Installation reset for host %s", state.GetHostname(&h))
			Expect(*resetEvent.Message).Should(Equal(eventMessage))
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
			events, err := eventsHandler.GetEvents(h.ID.String())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			resetEvent := events[len(events)-1]
			Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})

})

func getHost(hostId, clusterId strfmt.UUID, db *gorm.DB) *models.Host {
	var host models.Host
	Expect(db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error).ShouldNot(HaveOccurred())
	return &host
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func getTestHost(hostID, clusterID strfmt.UUID, state string) models.Host {
	return models.Host{
		ID:          &hostID,
		ClusterID:   clusterID,
		Status:      swag.String(state),
		Inventory:   defaultInventory(),
		Role:        models.HostRoleWorker,
		CheckedInAt: strfmt.DateTime(time.Now()),
	}
}

func getTestCluster(clusterID strfmt.UUID, machineNetworkCidr string) common.Cluster {
	return common.Cluster{
		Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: machineNetworkCidr,
		},
	}
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

func insufficientHWInventory() string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 2},
		Disks: []*models.Disk{
			{
				SizeBytes: 130,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory: &models.Memory{PhysicalBytes: 130},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func workerInventory() string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 2},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory: &models.Memory{PhysicalBytes: gibToBytes(8)},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func masterInventory() string {
	return masterInventoryWithHostname("master-hostname")
}

func masterInventoryWithHostname(hostname string) string {
	inventory := models.Inventory{
		CPU: &models.CPU{Count: 8},
		Disks: []*models.Disk{
			{
				SizeBytes: 128849018880,
				DriveType: "HDD",
			},
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
			},
		},
		Memory:   &models.Memory{PhysicalBytes: gibToBytes(16)},
		Hostname: hostname,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("UpdateInventory", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "update_inventory"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		hapi = NewManager(getTestLog(), db, nil, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
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
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
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
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: success,
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
})

var _ = Describe("Update hostname", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "update_inventory"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		hapi = NewManager(getTestLog(), db, nil, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("set hostname", func() {
		success := func(reply error) {
			Expect(reply).To(BeNil())
			h := getHost(hostId, clusterId, db)
			Expect(h.RequestedHostname).To(Equal("my-hostname"))
		}

		failure := func(reply error) {
			Expect(reply).To(HaveOccurred())
			h := getHost(hostId, clusterId, db)
			Expect(h.RequestedHostname).To(Equal(""))
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
				name:       models.HostStatusResettingPendingUserAction,
				srcState:   models.HostStatusResettingPendingUserAction,
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
			{
				name:       models.HostStatusPendingForInput,
				srcState:   models.HostStatusPendingForInput,
				validation: success,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				host = getTestHost(hostId, clusterId, t.srcState)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				t.validation(hapi.UpdateHostname(ctx, &host, "my-hostname", db))
			})
		}
	})
})

var _ = Describe("SetBootstrap", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "SetBootstrap"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		hapi = NewManager(getTestLog(), db, nil, nil, nil, createValidatorCfg(), nil)
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
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("PrepareForInstallation", func() {
	var (
		ctx               = context.Background()
		hapi              API
		db                *gorm.DB
		hostId, clusterId strfmt.UUID
		host              models.Host
		dbName            = "prepare_for_installation"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		hapi = NewManager(getTestLog(), db, nil, nil, nil, createValidatorCfg(), nil)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	It("success", func() {
		host = getTestHost(hostId, clusterId, models.HostStatusKnown)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(hapi.PrepareForInstallation(ctx, &host, db)).NotTo(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).To(Equal(models.HostStatusPreparingForInstallation))
		Expect(swag.StringValue(h.StatusInfo)).To(Equal(statusInfoPreparingForInstallation))
	})

	It("failure - no role set", func() {
		host = getTestHost(hostId, clusterId, models.HostStatusKnown)
		host.Role = ""
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		Expect(hapi.PrepareForInstallation(ctx, &host, db)).To(HaveOccurred())
		h := getHost(hostId, clusterId, db)
		Expect(swag.StringValue(h.Status)).To(Equal(models.HostStatusKnown))
	})

	Context("forbidden", func() {

		forbiddenStates := []string{
			models.HostStatusDisabled,
			models.HostStatusDisconnected,
			models.HostStatusError,
			models.HostStatusInstalling,
			models.HostStatusInstallingInProgress,
			models.HostStatusDiscovering,
			models.HostStatusPreparingForInstallation,
			models.HostStatusResetting,
		}

		for _, state := range forbiddenStates {
			It(fmt.Sprintf("forbidden state %s", state), func() {
				host = getTestHost(hostId, clusterId, state)
				Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
				Expect(hapi.PrepareForInstallation(ctx, &host, db)).To(HaveOccurred())
				h := getHost(hostId, clusterId, db)
				Expect(swag.StringValue(h.Status)).To(Equal(state))
			})
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})
