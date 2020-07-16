package cluster

import (
	"context"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(defaultTestConfig, getTestLog(), db, nil)
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
			db.Close()
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
	)

	BeforeEach(func() {
		db = prepareDB()
		id = strfmt.UUID(uuid.New().String())
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
		expectedState = ""
		shouldHaveUpdated = false
	})

	Context("from installing state", func() {

		BeforeEach(func() {
			c = common.Cluster{Cluster: models.Cluster{
				ID:     &id,
				Status: swag.String("installing"),
			}}

			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("installing -> installing", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (some hosts are installed)", func() {
			createHost(id, "installing", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)

			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing", db)

			shouldHaveUpdated = false
			expectedState = "installing"
		})
		It("installing -> finalizing", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			shouldHaveUpdated = true
			expectedState = models.ClusterStatusFinalizing
		})
		It("installing -> error", func() {
			createHost(id, "error", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("installing -> error", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			shouldHaveUpdated = true
			expectedState = "error"
		})
		It("installing -> error insufficient hosts", func() {
			createHost(id, "installing", db)
			createHost(id, "installed", db)
			shouldHaveUpdated = true
			expectedState = "error"

		})
	})

	Context("ghost hosts", func() {

		Context("from insufficient state", func() {
			BeforeEach(func() {

				c = common.Cluster{Cluster: models.Cluster{
					ID:     &id,
					Status: swag.String("insufficient"),
				}}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("insufficient -> insufficient", func() {
				createHost(id, "known", db)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> ready", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)

				shouldHaveUpdated = true
				expectedState = "ready"
				Expect(db.Model(&c).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.5"}).Error).To(Not(HaveOccurred()))
			})
			It("insufficient -> insufficient including hosts in discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
			It("insufficient -> insufficient including hosts in disabled", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "disabled", db)

				shouldHaveUpdated = false
				expectedState = "insufficient"
			})
		})
		Context("from ready state", func() {
			BeforeEach(func() {

				c = common.Cluster{Cluster: models.Cluster{
					ID:     &id,
					Status: swag.String("ready"),
				}}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
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

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient one host is discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)

				shouldHaveUpdated = true
				expectedState = "insufficient"
			})
			It("ready -> insufficient including hosts in disabled", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "disabled", db)

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
		clusterApi.ClusterMonitoring()
		after := time.Now().Truncate(10 * time.Millisecond)
		c = geCluster(id, db)
		Expect(c.Status).Should(Equal(swag.String(expectedState)))
		if shouldHaveUpdated {
			Expect(c.StatusInfo).ShouldNot(BeNil())
			updateTime := time.Time(c.StatusUpdatedAt).Truncate(10 * time.Millisecond)
			Expect(updateTime).Should(BeTemporally(">=", before))
			Expect(updateTime).Should(BeTemporally("<=", after))
		} else {
			Expect(c.StatusUpdatedAt).Should(Equal(saveUpdatedTime))
			Expect(c.StatusInfo).Should(Equal(saveStatusInfo))
		}

		db.Close()
	})

})

var _ = Describe("VerifyRegisterHost", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, host can register only in one of [insufficient ready]"
	)

	BeforeEach(func() {
		db = prepareDB()
		id = strfmt.UUID(uuid.New().String())
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
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
		checkVerifyRegisterHost(clusterStatusReady, false)
	})
	It("Register host while cluster in insufficient state", func() {
		checkVerifyRegisterHost(clusterStatusInsufficient, false)
	})
	It("Register host while cluster in installing state", func() {
		checkVerifyRegisterHost(clusterStatusInstalling, true)
	})
	It("Register host while cluster in installing state", func() {
		checkVerifyRegisterHost(models.ClusterStatusFinalizing, true)
	})
	It("Register host while cluster in error state", func() {
		checkVerifyRegisterHost(clusterStatusError, true)
	})

	It("Register host while cluster in installed state", func() {
		checkVerifyRegisterHost(clusterStatusInstalled, true)
	})
	AfterEach(func() {
		db.Close()
	})
})

var _ = Describe("VerifyClusterUpdatability", func() {
	var (
		db          *gorm.DB
		id          strfmt.UUID
		clusterApi  *Manager
		errTemplate = "Cluster %s is in %s state, cluster can be updated only in one of [insufficient ready]"
	)

	BeforeEach(func() {
		db = prepareDB()
		id = strfmt.UUID(uuid.New().String())
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
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
		checkVerifyClusterUpdatability(clusterStatusInsufficient, false)
	})
	It("Update cluster while ready", func() {
		checkVerifyClusterUpdatability(clusterStatusReady, false)
	})
	It("Update cluster while installing", func() {
		checkVerifyClusterUpdatability(clusterStatusInstalling, true)
	})
	It("Update cluster while installed", func() {
		checkVerifyClusterUpdatability(clusterStatusInstalled, true)
	})
	It("Update cluster while error", func() {
		checkVerifyClusterUpdatability(clusterStatusError, true)
	})

	AfterEach(func() {
		db.Close()
	})
})

var _ = Describe("SetGeneratorVersion", func() {
	var (
		db         *gorm.DB
		id         strfmt.UUID
		clusterApi *Manager
	)

	It("set generator version", func() {
		db = prepareDB()
		id = strfmt.UUID(uuid.New().String())
		clusterApi = NewManager(defaultTestConfig, getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
		cluster := common.Cluster{Cluster: models.Cluster{ID: &id, Status: swag.String(clusterStatusReady)}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		Expect(clusterApi.SetGeneratorVersion(&cluster, "v1", db)).ShouldNot(HaveOccurred())
		cluster = geCluster(id, db)
		Expect(cluster.IgnitionGeneratorVersion).To(Equal("v1"))
	})
})

var _ = Describe("CancelInstallation", func() {
	var (
		ctx   = context.Background()
		db    *gorm.DB
		state API
		c     common.Cluster
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(defaultTestConfig, getTestLog(), db, nil)
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusInsufficient),
		}}
	})

	Context("cancel_installation", func() {
		It("cancel_installation", func() {
			c.Status = swag.String(clusterStatusInstalling)
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
		})
		It("cancel_failed_installation", func() {
			c.Status = swag.String(clusterStatusError)
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			db.First(&c, "id = ?", c.ID)
			Expect(*c.Status).Should(Equal(clusterStatusError))
		})
	})

	Context("invalid_cancel_installation", func() {
		It("nothing_to_cancel", func() {
			Expect(state.CancelInstallation(ctx, &c, "some reason", db)).Should(HaveOccurred())
		})
	})

	AfterEach(func() {
		db.Close()
	})
})

var _ = Describe("ResetCluster", func() {
	var (
		ctx   = context.Background()
		db    *gorm.DB
		state API
		c     common.Cluster
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(defaultTestConfig, getTestLog(), db, nil)
	})

	It("reset_cluster", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusError),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		Expect(state.ResetCluster(ctx, &c, "some reason", db)).ShouldNot(HaveOccurred())
		db.First(&c, "id = ?", c.ID)
		Expect(*c.Status).Should(Equal(clusterStatusInsufficient))
	})

	It("reset cluster conflict", func() {
		id := strfmt.UUID(uuid.New().String())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &id,
			Status: swag.String(clusterStatusReady),
		}}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		reply := state.ResetCluster(ctx, &c, "some reason", db)
		Expect(int(reply.StatusCode())).Should(Equal(http.StatusConflict))
	})

	AfterEach(func() {
		db.Close()
	})
})

func createHost(clusterId strfmt.UUID, state string, db *gorm.DB) {
	hostId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:        &hostId,
		ClusterID: clusterId,
		Role:      models.HostRoleMaster,
		Status:    swag.String(state),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func prepareDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", ":memory:")
	Expect(err).ShouldNot(HaveOccurred())
	db.AutoMigrate(&common.Cluster{})
	db.AutoMigrate(&models.Host{})
	return db
}

func TestCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster state machine tests")
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
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

	}
	Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterId}}).Updates(map[string]interface{}{"api_vip": "1.2.3.5", "ingress_vip": "1.2.3.5"}).Error).To(Not(HaveOccurred()))

}

var _ = Describe("PrepareForInstallation", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		capi = NewManager(defaultTestConfig, getTestLog(), db, nil)
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
})

var _ = Describe("HandlePreInstallationError", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		capi = NewManager(defaultTestConfig, getTestLog(), db, nil)
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
})
