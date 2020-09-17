package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/thoas/go-funk"

	"github.com/openshift/assisted-service/pkg/leader"

	"github.com/openshift/assisted-service/pkg/s3wrapper"

	"github.com/kelseyhightower/envconfig"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var defaultTestConfig = Config{
	PrepareConfig: PrepareConfig{
		InstallationTimeout: 10 * time.Minute,
	},
}

var _ = Describe("stateMachine", func() {
	var (
		ctx              = context.Background()
		db               *gorm.DB
		state            API
		cluster          *common.Cluster
		refreshedCluster *common.Cluster
		stateErr         error
		dbName           = "state_machine"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		dummy := &leader.DummyElector{}
		state = NewManager(defaultTestConfig, getTestLog(), db, nil, nil, nil, dummy)
		id := strfmt.UUID(uuid.New().String())
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String("not a known state"),
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	Context("unknown_cluster_state", func() {
		It("update_cluster", func() {
			refreshedCluster, stateErr = state.RefreshStatus(ctx, cluster, db)
		})

		It("install_cluster", func() {
			stateErr = state.Install(ctx, cluster, db)
		})

		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
			Expect(refreshedCluster).To(BeNil())
			Expect(stateErr).Should(HaveOccurred())
		})
	})

})

/*
All supported case options:
installing -> installing
installing -> installed
installing -> error

known -> insufficient
insufficient -> known
*/

var _ = Describe("cluster monitor", func() {
	var (
		//ctx        = context.Background()
		db                *gorm.DB
		c                 common.Cluster
		id                strfmt.UUID
		err               error
		clusterApi        *Manager
		shouldHaveUpdated bool
		expectedState     string
		ctrl              *gomock.Controller
		mockHostAPI       *host.MockAPI
		mockMetric        *metrics.MockAPI
		dbName            = "cluster_monitor"
		mockEvents        *events.MockHandler
	)

	mockHostAPIIsValidMasterCandidateTrue := func(times int) {
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).
			Times(times)
	}

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, dummy)
		expectedState = ""
		shouldHaveUpdated = false
	})

	Context("from installing state", func() {

		BeforeEach(func() {
			c = common.Cluster{Cluster: models.Cluster{
				ID:                 &id,
				Status:             swag.String("installing"),
				StatusInfo:         swag.String(statusInfoInstalling),
				MachineNetworkCidr: "1.1.0.0/16",
				BaseDNSDomain:      "test.com",
				PullSecretSet:      true,
			}}

			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(err).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		})

		It("installing -> installing", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			mockHostAPIIsValidMasterCandidateTrue(3)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("with workers 1 in error, installing -> installing", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createWorkerHost(id, "installing", db)
			createWorkerHost(id, "error", db)
			mockHostAPIIsValidMasterCandidateTrue(5)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("with workers 2 in installing, installing -> installing", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createWorkerHost(id, "installing", db)
			createWorkerHost(id, "installing", db)
			mockHostAPIIsValidMasterCandidateTrue(5)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (some hosts are installed)", func() {
			createHost(id, "installing", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(3)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			mockHostAPIIsValidMasterCandidateTrue(3)

			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing", db)
			mockHostAPIIsValidMasterCandidateTrue(3)

			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("with worker installing -> installing", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(3)

			shouldHaveUpdated = true
			expectedState = models.ClusterStatusFinalizing
		})
		It("installing -> finalizing", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(3)

			shouldHaveUpdated = true
			expectedState = models.ClusterStatusFinalizing
		})
		It("with workers installing -> finalizing", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createWorkerHost(id, "installing", db)
			createWorkerHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(5)

			shouldHaveUpdated = true
			expectedState = models.ClusterStatusFinalizing
		})

		It("installing -> error", func() {
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", gomock.Any(), gomock.Any()).AnyTimes()
			createHost(id, "error", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(3)

			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("installing -> error", func() {
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", gomock.Any(), gomock.Any()).AnyTimes()
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(2)

			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("installing -> error insufficient hosts", func() {
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "error", gomock.Any(), gomock.Any()).AnyTimes()
			createHost(id, "installing", db)
			createHost(id, "installed", db)
			createWorkerHost(id, "installed", db)
			mockHostAPIIsValidMasterCandidateTrue(3)
			shouldHaveUpdated = true
			expectedState = "error"

		})
		It("with workers in error, installing -> error", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createWorkerHost(id, "error", db)
			createWorkerHost(id, "error", db)
			mockHostAPIIsValidMasterCandidateTrue(5)
			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("with single worker in error, installing -> error", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createWorkerHost(id, "error", db)
			mockHostAPIIsValidMasterCandidateTrue(4)
			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("with single worker in error, installing -> error", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createWorkerHost(id, "error", db)
			mockHostAPIIsValidMasterCandidateTrue(4)
			shouldHaveUpdated = true
			expectedState = "error"
		})
	})

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	mockHostAPIIsValidMasterCandidateFalseNoErrors := func(times int) {
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil).
			Times(times)
	}

	Context("host hosts", func() {

		Context("from insufficient state", func() {
			BeforeEach(func() {

				c = common.Cluster{Cluster: models.Cluster{
					ID:                       &id,
					Status:                   swag.String("insufficient"),
					MachineNetworkCidr:       "1.2.3.0/24",
					APIVip:                   "1.2.3.5",
					IngressVip:               "1.2.3.6",
					BaseDNSDomain:            "test.com",
					PullSecretSet:            true,
					StatusInfo:               swag.String(statusInfoInsufficient),
					ClusterNetworkCidr:       "1.3.0.0/16",
					ServiceNetworkCidr:       "1.2.5.0/24",
					ClusterNetworkHostPrefix: 24,
				}}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("insufficient -> insufficient", func() {
				createHost(id, "known", db)
				mockHostAPIIsRequireUserActionResetFalse()
				mockHostAPIIsValidMasterCandidateTrue(1)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient", func() {
				createHost(id, "insufficient", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				mockHostAPIIsRequireUserActionResetFalse()
				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> ready", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				mockHostAPIIsRequireUserActionResetFalse()

				shouldHaveUpdated = true
				expectedState = "ready"
				Expect(db.Model(&c).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.6"}).Error).To(Not(HaveOccurred()))
			})
			It("insufficient -> insufficient including hosts in discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)
				mockHostAPIIsRequireUserActionResetFalse()
				mockHostAPIIsValidMasterCandidateTrue(2)
				mockHostAPIIsValidMasterCandidateFalseNoErrors(1)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)
				mockHostAPIIsRequireUserActionResetFalse()
				mockHostAPIIsValidMasterCandidateTrue(2)
				mockHostAPIIsValidMasterCandidateFalseNoErrors(1)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient including hosts in disabled", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "disabled", db)
				mockHostAPIIsRequireUserActionResetFalse()
				mockHostAPIIsValidMasterCandidateTrue(2)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
		})
		Context("from ready state", func() {
			BeforeEach(func() {
				c = common.Cluster{Cluster: models.Cluster{
					ID:                       &id,
					Status:                   swag.String(models.ClusterStatusReady),
					StatusInfo:               swag.String(statusInfoReady),
					MachineNetworkCidr:       "1.2.3.0/24",
					APIVip:                   "1.2.3.5",
					IngressVip:               "1.2.3.6",
					BaseDNSDomain:            "test.com",
					PullSecretSet:            true,
					ClusterNetworkCidr:       "1.3.0.0/16",
					ServiceNetworkCidr:       "1.2.5.0/24",
					ClusterNetworkHostPrefix: 24,
				}}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			})

			It("ready -> ready", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)

				shouldHaveUpdated = false
				expectedState = "ready"
			})
			It("ready -> insufficient", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				mockHostAPIIsValidMasterCandidateTrue(2)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient one host is discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)
				mockHostAPIIsValidMasterCandidateTrue(2)
				mockHostAPIIsValidMasterCandidateFalseNoErrors(1)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)
				mockHostAPIIsValidMasterCandidateFalseNoErrors(1)
				mockHostAPIIsValidMasterCandidateTrue(2)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient including hosts in disabled", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "disabled", db)
				mockHostAPIIsValidMasterCandidateTrue(2)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
		})

	})

	AfterEach(func() {
		before := time.Now().Truncate(10 * time.Millisecond)
		c = geCluster(id, db)
		saveUpdatedTime := c.StatusUpdatedAt
		saveStatusInfo := c.StatusInfo
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockHostAPIIsRequireUserActionResetFalse()
		clusterApi.ClusterMonitoring()
		after := time.Now().Truncate(10 * time.Millisecond)
		c = geCluster(id, db)
		Expect(swag.StringValue(c.Status)).Should(Equal(expectedState))
		if shouldHaveUpdated {
			Expect(c.StatusInfo).ShouldNot(BeNil())
			updateTime := time.Time(c.StatusUpdatedAt).Truncate(10 * time.Millisecond)
			Expect(updateTime).Should(BeTemporally(">=", before))
			Expect(updateTime).Should(BeTemporally("<=", after))
		} else {
			Expect(c.StatusUpdatedAt).Should(Equal(saveUpdatedTime))
			Expect(c.StatusInfo).Should(Equal(saveStatusInfo))
		}

		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("lease timeout event", func() {
	var (
		db          *gorm.DB
		c           common.Cluster
		id          strfmt.UUID
		clusterApi  *Manager
		ctrl        *gomock.Controller
		mockHostAPI *host.MockAPI
		mockMetric  *metrics.MockAPI
		dbName      = "cluster_monitor"
		mockEvents  *events.MockHandler
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		id = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, dummy)
	})
	tests := []struct {
		name                string
		srcState            string
		apiVip              string
		ingressVip          string
		shouldTimeout       bool
		eventCalllsExpected int
	}{
		{
			name:                "VIPs exist",
			srcState:            models.ClusterStatusReady,
			apiVip:              "1.2.3.4",
			ingressVip:          "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 1,
		},
		{
			name:                "API Vip missing with timeout",
			srcState:            models.ClusterStatusReady,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 2,
		},
		{
			name:                "API Vip missing without timeout",
			srcState:            models.ClusterStatusReady,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       false,
			eventCalllsExpected: 1,
		},
		{
			name:                "API Vip missing without timeout from insufficient",
			srcState:            models.ClusterStatusInsufficient,
			ingressVip:          "1.2.3.5",
			shouldTimeout:       false,
			eventCalllsExpected: 0,
		},
		{
			name:                "Ingress Vip missing with timeout from insufficient",
			srcState:            models.ClusterStatusInsufficient,
			apiVip:              "1.2.3.5",
			shouldTimeout:       true,
			eventCalllsExpected: 1,
		},
	}
	for _, t := range tests {
		It(t.name, func() {
			c = common.Cluster{Cluster: models.Cluster{
				ID:                       &id,
				Status:                   swag.String(t.srcState),
				APIVip:                   t.apiVip,
				IngressVip:               t.ingressVip,
				MachineNetworkCidr:       "1.2.3.0/24",
				BaseDNSDomain:            "test.com",
				PullSecretSet:            true,
				ClusterNetworkCidr:       "1.3.0.0/16",
				ServiceNetworkCidr:       "1.2.5.0/24",
				ClusterNetworkHostPrefix: 24,
				VipDhcpAllocation:        swag.Bool(true),
			}}
			if t.shouldTimeout {
				c.MachineNetworkCidrUpdatedAt = time.Now().Add(-2 * time.Minute)
			} else {
				c.MachineNetworkCidrUpdatedAt = time.Now().Add(-1 * time.Minute)
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			if t.eventCalllsExpected > 0 {
				mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(t.eventCalllsExpected)
			}
			clusterApi.ClusterMonitoring()
			ctrl.Finish()
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("VerifyRegisterHost", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, host can register only in one of [insufficient ready pending-for-input]"
		dbName      = "verify_register_host"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		id = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			nil, nil, nil, dummy)
	})

	checkVerifyRegisterHost := func(clusterStatus string, expectErr bool) {
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(clusterStatus)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		err := clusterApi.AcceptRegistration(&cluster)
		if expectErr {
			Expect(err.Error()).Should(Equal(errors.Errorf(errTemplate, id, clusterStatus).Error()))
		} else {
			Expect(err).Should(BeNil())
		}
	}
	It("Register host while cluster in ready state", func() {
		checkVerifyRegisterHost(models.ClusterStatusReady, false)
	})
	It("Register host while cluster in insufficient state", func() {
		checkVerifyRegisterHost(models.ClusterStatusInsufficient, false)
	})
	It("Register host while cluster in installing state", func() {
		checkVerifyRegisterHost(models.ClusterStatusInstalling, true)
	})
	It("Register host while cluster in installing state", func() {
		checkVerifyRegisterHost(models.ClusterStatusFinalizing, true)
	})
	It("Register host while cluster in error state", func() {
		checkVerifyRegisterHost(models.ClusterStatusError, true)
	})

	It("Register host while cluster in installed state", func() {
		checkVerifyRegisterHost(models.ClusterStatusInstalled, true)
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("VerifyClusterUpdatability", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, cluster can be updated only in one of [insufficient ready pending-for-input]"
		dbName      = "verify_cluster_updatability"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		id = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			nil, nil, nil, dummy)
	})

	checkVerifyClusterUpdatability := func(clusterStatus string, expectErr bool) {
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(clusterStatus)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		err := clusterApi.VerifyClusterUpdatability(&cluster)
		if expectErr {
			Expect(err.Error()).Should(Equal(errors.Errorf(errTemplate, id, clusterStatus).Error()))
		} else {
			Expect(err).Should(BeNil())
		}
	}
	It("Update cluster while insufficient", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInsufficient, false)
	})
	It("Update cluster while ready", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusReady, false)
	})
	It("Update cluster while installing", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInstalling, true)
	})
	It("Update cluster while installed", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusInstalled, true)
	})
	It("Update cluster while error", func() {
		checkVerifyClusterUpdatability(models.ClusterStatusError, true)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("SetGeneratorVersion", func() {
	var (
		db         *gorm.DB
		id         strfmt.UUID
		clusterApi *Manager
		dbName     = "set_generator_version"
	)

	It("set generator version", func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		id = strfmt.UUID(uuid.New().String())
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			nil, nil, nil, dummy)
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(models.ClusterStatusReady)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		Expect(clusterApi.SetGeneratorVersion(&cluster, "v1", db)).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		Expect(cluster.IgnitionGeneratorVersion).To(Equal("v1"))
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("CancelInstallation", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler events.Handler
		ctrl          *gomock.Controller
		mockMetric    *metrics.MockAPI
		dbName        = "cluster_cancel_installation"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		dummy := &leader.DummyElector{}
		state = NewManager(defaultTestConfig, getTestLog(), db, eventsHandler, nil, mockMetric, dummy)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:         &id,
			Status:     swag.String(models.ClusterStatusInsufficient),
			StatusInfo: swag.String(statusInfoInsufficient)}}
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			c.Status = swag.String(models.ClusterStatusInstalling)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, c.InstallStartedAt)
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(*c.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			Expect(*cancelEvent.Message).Should(Equal("Canceled cluster installation"))
		})
		It("cancel_failed_installation", func() {
			c.Status = swag.String(models.ClusterStatusError)
			c.InstallStartedAt = strfmt.DateTime(time.Now().Add(-time.Minute))
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), "canceled", c.OpenshiftVersion, c.InstallStartedAt)
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
			events, err := eventsHandler.GetEvents(*c.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityInfo))
			Expect(*cancelEvent.Message).Should(Equal("Canceled cluster installation"))
		})

		AfterEach(func() {
			db.First(&c, "id = ?", c.ID)
			Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusError))
		})
	})

	Context("invalid_cancel_installation", func() {
		It("nothing_to_cancel", func() {
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).Should(HaveOccurred())
			events, err := eventsHandler.GetEvents(*c.ID, nil)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(events)).ShouldNot(Equal(0))
			cancelEvent := events[len(events)-1]
			Expect(*cancelEvent.Severity).Should(Equal(models.EventSeverityError))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("ResetCluster", func() {
	var (
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler events.Handler
		dbName        = "reset_cluster"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(defaultTestConfig, getTestLog(), db, eventsHandler, nil, nil, dummy)
	})

	It("reset_cluster", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusError),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		Expect(state.ResetCluster(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
		db.First(&c, "id = ?", c.ID)
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
		events, err := eventsHandler.GetEvents(*c.ID, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
		Expect(*resetEvent.Message).Should(Equal("Reset cluster installation"))
	})

	It("reset cluster conflict", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusReady),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		reply := state.ResetCluster(ctx, &c, "some reason", db)
		Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
		events, err := eventsHandler.GetEvents(*c.ID, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityError))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

func createHost(clusterId strfmt.UUID, state string, db *gorm.DB) {
	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:        &hostId,
		ClusterID: clusterId,
		Role:      models.HostRoleMaster,
		Status:    swag.String(state),
		Inventory: defaultInventory(),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func createWorkerHost(clusterId strfmt.UUID, state string, db *gorm.DB) {
	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:        &hostId,
		ClusterID: clusterId,
		Role:      models.HostRoleWorker,
		Status:    swag.String(state),
		Inventory: defaultInventory(),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func geCluster(clusterId strfmt.UUID, db *gorm.DB) common.Cluster {
	var cluster common.Cluster
	Expect(db.Preload("Hosts").First(&cluster, "id = ?", clusterId).Error).ShouldNot(HaveOccurred())
	return cluster
}
func addInstallationRequirements(clusterId strfmt.UUID, db *gorm.DB) {
	var hostId strfmt.UUID
	var host models.Host
	for i := 0; i < 3; i++ {
		hostId = strfmt.UUID(uuid.New().String())
		host = models.Host{
			ID:        &hostId,
			ClusterID: clusterId,
			Role:      models.HostRoleMaster,
			Status:    swag.String("known"),
			Inventory: defaultInventory(),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

	}
	Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.6"}).Error).To(Not(HaveOccurred()))
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

var _ = Describe("PrepareForInstallation", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
		dbName    = "cluster_prepare_for_installation"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		dummy := &leader.DummyElector{}
		capi = NewManager(defaultTestConfig, getTestLog(), db, nil, nil, nil, dummy)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	// state changes to preparing-for-installation
	success := func(cluster *common.Cluster) {
		Expect(capi.PrepareForInstallation(ctx, cluster, db)).NotTo(HaveOccurred())
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).To(Equal(models.ClusterStatusPreparingForInstallation))
	}

	// status should not change
	failure := func(cluster *common.Cluster) {
		src := swag.StringValue(cluster.Status)
		Expect(capi.PrepareForInstallation(ctx, cluster, db)).To(HaveOccurred())
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(src))
	}

	tests := []struct {
		name       string
		srcState   string
		validation func(cluster *common.Cluster)
	}{
		{
			name:       "success from ready",
			srcState:   models.ClusterStatusReady,
			validation: success,
		},
		{
			name:       "already prepared for installation - should fail",
			srcState:   models.ClusterStatusPreparingForInstallation,
			validation: failure,
		},
		{
			name:       "insufficient - should fail",
			srcState:   models.ClusterStatusInsufficient,
			validation: failure,
		},
		{
			name:       "installing - should fail",
			srcState:   models.ClusterStatusInstalling,
			validation: failure,
		},
		{
			name:       "error - should fail",
			srcState:   models.ClusterStatusError,
			validation: failure,
		},
		{
			name:       "installed - should fail",
			srcState:   models.ClusterStatusInstalled,
			validation: failure,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.srcState)}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Take(&cluster, "id = ?", clusterId).Error).ShouldNot(HaveOccurred())
			t.validation(&cluster)
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("HandlePreInstallationError", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
		dbName    = "handle_preInstallation_error"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		dummy := &leader.DummyElector{}
		capi = NewManager(defaultTestConfig, getTestLog(), db, nil, nil, nil, dummy)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	// state changes to error
	success := func(cluster *common.Cluster) {
		capi.HandlePreInstallError(ctx, cluster, errors.Errorf("pre-install error"))
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).To(Equal(models.ClusterStatusError))
	}

	// status should not change
	failure := func(cluster *common.Cluster) {
		src := swag.StringValue(cluster.Status)
		capi.HandlePreInstallError(ctx, cluster, errors.Errorf("pre-install error"))
		Expect(db.Take(cluster, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(src))
	}

	tests := []struct {
		name       string
		srcState   string
		validation func(cluster *common.Cluster)
	}{
		{
			name:       "success",
			srcState:   models.ClusterStatusPreparingForInstallation,
			validation: success,
		},
		{
			name:       "ready - should fail",
			srcState:   models.ClusterStatusReady,
			validation: failure,
		},
		{
			name:       "insufficient - should fail",
			srcState:   models.ClusterStatusInsufficient,
			validation: failure,
		},
		{
			name:       "installing - should fail",
			srcState:   models.ClusterStatusInstalling,
			validation: failure,
		},
		{
			name:       "error - success",
			srcState:   models.ClusterStatusError,
			validation: success,
		},
		{
			name:       "installed - should fail",
			srcState:   models.ClusterStatusInstalled,
			validation: failure,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.srcState)}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Take(&cluster, "id = ?", clusterId).Error).ShouldNot(HaveOccurred())
			t.validation(&cluster)
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("SetVips", func() {
	var (
		ctx        = context.Background()
		capi       API
		mockEvents *events.MockHandler
		ctrl       *gomock.Controller
		db         *gorm.DB
		clusterId  strfmt.UUID
		dbName     = "set_vips"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		capi = NewManager(defaultTestConfig, getTestLog(), db, mockEvents, nil, nil, dummy)
		clusterId = strfmt.UUID(uuid.New().String())
	})
	AfterEach(func() {
		ctrl.Finish()
	})

	tests := []struct {
		name               string
		srcState           string
		clusterApiVip      string
		clusterIngressVip  string
		apiVip             string
		ingressVip         string
		expectedApiVip     string
		expectedIngressVip string
		expectedState      string
		errorExpected      bool
		eventExpected      bool
	}{
		{
			name:               "success-empty",
			srcState:           models.ClusterStatusInsufficient,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInsufficient,
			eventExpected:      true,
		},
		{
			name:               "success-empty, from pending-from-input",
			srcState:           models.ClusterStatusPendingForInput,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusPendingForInput,
			eventExpected:      true,
		},
		{
			name:               "success-empty from ready",
			srcState:           models.ClusterStatusReady,
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusReady,
			eventExpected:      true,
		},
		{
			name:               "success- insufficient",
			srcState:           models.ClusterStatusInsufficient,
			clusterApiVip:      "1.1.1.1",
			clusterIngressVip:  "2.2.2.2",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInsufficient,
			eventExpected:      true,
		},
		{
			name:               "success- ready same",
			srcState:           models.ClusterStatusReady,
			clusterApiVip:      "1.2.3.4",
			clusterIngressVip:  "1.2.3.5",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusReady,
		},
		{
			name:               "failure- installing",
			srcState:           models.ClusterStatusInstalling,
			clusterApiVip:      "1.1.1.1",
			clusterIngressVip:  "2.2.2.2",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.1.1.1",
			expectedIngressVip: "2.2.2.2",
			errorExpected:      true,
			expectedState:      models.ClusterStatusInstalling,
		},
		{
			name:               "success- ready same",
			srcState:           models.ClusterStatusInstalling,
			clusterApiVip:      "1.2.3.4",
			clusterIngressVip:  "1.2.3.5",
			apiVip:             "1.2.3.4",
			ingressVip:         "1.2.3.5",
			expectedApiVip:     "1.2.3.4",
			expectedIngressVip: "1.2.3.5",
			errorExpected:      false,
			expectedState:      models.ClusterStatusInstalling,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId, Status: swag.String(t.srcState)}}
			cluster.APIVip = t.clusterApiVip
			cluster.IngressVip = t.clusterIngressVip
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			if t.eventExpected {
				mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).Times(1)
			}
			err := capi.SetVips(ctx, &cluster, t.apiVip, t.ingressVip, db)
			Expect(err != nil).To(Equal(t.errorExpected))
			var c common.Cluster
			Expect(db.Take(&c, "id = ?", clusterId.String()).Error).ToNot(HaveOccurred())
			Expect(c.APIVip).To(Equal(t.expectedApiVip))
			Expect(c.IngressVip).To(Equal(t.expectedIngressVip))
			Expect(swag.StringValue(c.Status)).To(Equal(t.expectedState))
		})
	}
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("ready_state", func() {
	var (
		ctx        = context.Background()
		clusterApi *Manager
		db         *gorm.DB
		id         strfmt.UUID
		cluster    common.Cluster
		dbName     = "cluster_ready_state"
		ctrl       *gomock.Controller
		mockEvents *events.MockHandler
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, nil, nil, dummy)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:                       &id,
			Status:                   swag.String(models.ClusterStatusReady),
			MachineNetworkCidr:       "1.2.3.0/24",
			BaseDNSDomain:            "test.com",
			PullSecretSet:            true,
			ServiceNetworkCidr:       "1.2.4.0/24",
			ClusterNetworkCidr:       "1.3.0.0/16",
			ClusterNetworkHostPrefix: 24,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		addInstallationRequirements(id, db)

		cluster = geCluster(*cluster.ID, db)
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusReady))
		Expect(len(cluster.Hosts)).Should(Equal(3))
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	})

	Context("refresh_state", func() {
		It("cluster is satisfying the install requirements", func() {
			clusterAfterRefresh, updateErr := clusterApi.RefreshStatus(ctx, &cluster, db)

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(models.ClusterStatusReady))
		})

		It("cluster is not satisfying the install requirements", func() {
			Expect(db.Where("cluster_id = ?", cluster.ID).Delete(&models.Host{}).Error).NotTo(HaveOccurred())

			cluster = geCluster(*cluster.ID, db)
			clusterAfterRefresh, updateErr := clusterApi.RefreshStatus(ctx, &cluster, db)

			Expect(updateErr).Should(BeNil())
			Expect(*clusterAfterRefresh.Status).Should(Equal(models.ClusterStatusInsufficient))
		})
	})
	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("insufficient_state", func() {
	var (
		ctx          = context.Background()
		clusterApi   *Manager
		db           *gorm.DB
		currentState = models.ClusterStatusInsufficient
		id           strfmt.UUID
		cluster      common.Cluster
		ctrl         *gomock.Controller
		mockHostAPI  *host.MockAPI
		dbName       = "cluster_insufficient_state"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := events.NewMockHandler(ctrl)
		db = common.PrepareTestDB(dbName, &events.Event{})
		dummy := &leader.DummyElector{}
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, nil, dummy)

		id = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{
			ID:                 &id,
			Status:             swag.String(currentState),
			MachineNetworkCidr: "1.2.3.0/24",
			APIVip:             "1.2.3.5",
			IngressVip:         "1.2.3.6",
			BaseDNSDomain:      "test.com",
			PullSecretSet:      true,
		}}

		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		replyErr := clusterApi.RegisterCluster(ctx, &cluster)
		Expect(replyErr).Should(BeNil())
		Expect(swag.StringValue(cluster.Status)).Should(Equal(models.ClusterStatusInsufficient))
		c := geCluster(*cluster.ID, db)
		Expect(swag.StringValue(c.Status)).Should(Equal(models.ClusterStatusInsufficient))
	})
})

var _ = Describe("prepare-for-installation refresh status", func() {
	var (
		ctx         = context.Background()
		capi        API
		db          *gorm.DB
		clusterId   strfmt.UUID
		cl          common.Cluster
		dbName      = "cluster_prepare_for_installation"
		ctrl        *gomock.Controller
		mockHostAPI *host.MockAPI
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		cfg := Config{}
		Expect(envconfig.Process("myapp", &cfg)).NotTo(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		capi = NewManager(cfg, getTestLog(), db, mockEvents, mockHostAPI, nil, dummy)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	})

	It("no change", func() {
		Expect(db.Take(&cl, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(*refreshedCluster.Status).To(Equal(models.ClusterStatusPreparingForInstallation))
	})

	It("timeout", func() {
		Expect(db.Model(&cl).Update("status_updated_at", strfmt.DateTime(time.Now().Add(-15*time.Minute))).Error).
			NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(refreshedCluster.Status)).To(Equal(models.ClusterStatusError))
	})

	AfterEach(func() {
		db.Close()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Cluster tarred files", func() {
	var (
		ctx          = context.Background()
		capi         API
		db           *gorm.DB
		clusterId    strfmt.UUID
		cl           common.Cluster
		dbName       = "cluster_tar"
		ctrl         *gomock.Controller
		mockHostAPI  *host.MockAPI
		mockS3Client *s3wrapper.MockAPI
		prefix       string
		files        []string
		tarFile      string
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		cfg := Config{}
		files = []string{"test", "test2"}
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEvents := events.NewMockHandler(ctrl)
		dummy := &leader.DummyElector{}
		capi = NewManager(cfg, getTestLog(), db, mockEvents, mockHostAPI, nil, dummy)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		tarFile = fmt.Sprintf("%s/logs/cluster_logs.tar", clusterId)
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
		prefix = fmt.Sprintf("%s/logs/", cl.ID)
		mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("list objects failed", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(nil, errors.Errorf("dummy"))
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects no files", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{tarFile}, nil)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("list objects only all logs file", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return([]string{}, nil)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("download failed", func() {
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, files[0]).Return(nil, int64(0), errors.Errorf("Dummy")).Times(1)
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})

	It("upload failed", func() {
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, prefix).Return(files, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, gomock.Any()).Return(r, int64(4), nil).AnyTimes()
		mockS3Client.EXPECT().UploadStream(ctx, gomock.Any(), tarFile).Return(errors.Errorf("Dummy")).Times(1)
		_, err := capi.CreateTarredClusterLogs(ctx, &cl, mockS3Client)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("CompleteInstallation", func() {
	var (
		ctrl          *gomock.Controller
		ctx           = context.Background()
		db            *gorm.DB
		state         API
		c             common.Cluster
		eventsHandler events.Handler
		mockMetric    *metrics.MockAPI
		dbName        = "complete_installation"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMetric = metrics.NewMockAPI(ctrl)
		db = common.PrepareTestDB(dbName, &events.Event{})
		eventsHandler = events.New(db, logrus.New())
		dummy := &leader.DummyElector{}
		state = NewManager(defaultTestConfig, getTestLog(), db, eventsHandler, nil, mockMetric, dummy)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(models.ClusterStatusFinalizing),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
	})

	It("complete installation successfully", func() {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusInstalled, gomock.Any(), gomock.Any()).Times(1)
		apiErr := state.CompleteInstallation(ctx, &c, true, "")
		Expect(apiErr).ShouldNot(HaveOccurred())
		events, err := eventsHandler.GetEvents(*c.ID, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityInfo))
	})
	It("complete installation failure", func() {
		mockMetric.EXPECT().ClusterInstallationFinished(gomock.Any(), models.ClusterStatusError, gomock.Any(), gomock.Any()).Times(1)
		apiErr := state.CompleteInstallation(ctx, &c, false, "dummy error")
		Expect(apiErr).ShouldNot(HaveOccurred())
		events, err := eventsHandler.GetEvents(*c.ID, nil)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(events)).ShouldNot(Equal(0))
		resetEvent := events[len(events)-1]
		Expect(*resetEvent.Severity).Should(Equal(models.EventSeverityCritical))
		Expect(funk.Contains(*resetEvent.Message, "dummy error")).Should(Equal(true))
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})
