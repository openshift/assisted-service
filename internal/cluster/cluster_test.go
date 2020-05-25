package cluster

import (
	"context"
	"io/ioutil"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("stateMachine", func() {
	var (
		ctx        = context.Background()
		db         *gorm.DB
		state      API
		cluster    models.Cluster
		stateReply *UpdateReply
		stateErr   error
	)

	BeforeEach(func() {
		db = prepareDB()
		state = NewManager(getTestLog(), db, nil)
		id := strfmt.UUID(uuid.New().String())
		cluster = models.Cluster{
			ID:     &id,
			Status: swag.String("not a known state"),
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	Context("unknown_cluster_state", func() {
		It("update_cluster", func() {
			stateReply, stateErr = state.RefreshStatus(ctx, &cluster, db)
		})

		It("install_cluster", func() {
			stateErr = state.Install(ctx, &cluster, db)
		})

		AfterEach(func() {
			db.Close()
			Expect(stateReply).To(BeNil())
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
		db         *gorm.DB
		c          models.Cluster
		id         strfmt.UUID
		err        error
		clusterApi *Manager
	)

	BeforeEach(func() {
		db = prepareDB()
		id = strfmt.UUID(uuid.New().String())
		clusterApi = NewManager(getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
	})

	Context("from installing state", func() {

		BeforeEach(func() {
			c = models.Cluster{
				ID:     &id,
				Status: swag.String("installing"),
			}

			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("installing -> installing", func() {
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			createHost(id, "installing", db)
			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("installing")))
		})
		It("installing -> installing (some hosts are installed)", func() {
			createHost(id, "installing", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("installing")))
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("installing")))
		})
		It("installing -> installing (including installing-in-progress)", func() {
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing-in-progress", db)
			createHost(id, "installing", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("installing")))
		})
		It("installing -> installed", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("installed")))
		})
		It("installing -> error", func() {
			createHost(id, "error", db)
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("error")))
		})
		It("installing -> error", func() {
			createHost(id, "installed", db)
			createHost(id, "installed", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("error")))
		})
		It("installing -> error insufficient hosts", func() {
			createHost(id, "installing", db)
			createHost(id, "installed", db)

			clusterApi.ClusterMonitoring()
			c = geCluster(id, db)
			Expect(c.Status).Should(Equal(swag.String("error")))
		})

	})

	Context("ghost hosts", func() {

		Context("from insufficient state", func() {
			BeforeEach(func() {

				c = models.Cluster{
					ID:     &id,
					Status: swag.String("insufficient"),
				}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("insufficient -> insufficient", func() {
				createHost(id, "known", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
			It("insufficient -> ready", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("ready")))
			})
			It("insufficient -> insufficient including hosts in discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
			It("insufficient -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
		})
		Context("from ready state", func() {
			BeforeEach(func() {

				c = models.Cluster{
					ID:     &id,
					Status: swag.String("ready"),
				}

				Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("ready -> ready", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "known", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("ready")))
			})
			It("ready -> insufficient", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
			It("ready -> insufficient one host is discovering", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "discovering", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
			It("ready -> insufficient including hosts in error", func() {
				createHost(id, "known", db)
				createHost(id, "known", db)
				createHost(id, "error", db)
				clusterApi.ClusterMonitoring()
				c = geCluster(id, db)
				Expect(c.Status).Should(Equal(swag.String("insufficient")))
			})
		})

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
		Role:      "master",
		Status:    swag.String(state),
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
}

func prepareDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", ":memory:")
	Expect(err).ShouldNot(HaveOccurred())
	db.AutoMigrate(&models.Cluster{})
	db.AutoMigrate(&models.Host{})
	return db
}

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "cluster state machine tests")
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func geCluster(clusterId strfmt.UUID, db *gorm.DB) models.Cluster {
	var cluster models.Cluster
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
			Role:      "master",
			Status:    swag.String("known"),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

	}
}
