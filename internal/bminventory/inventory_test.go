package bminventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"testing"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/go-openapi/runtime/middleware"

	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/pkg/filemiddleware"

	awsS3Client "github.com/filanov/bm-inventory/pkg/s3Client"

	"github.com/filanov/bm-inventory/internal/cluster"
	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/job"
	"github.com/filanov/bm-inventory/restapi/operations/installer"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const ClusterStatusInstalled = "installed"

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
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		ctrl       *gomock.Controller
		mockJob    *job.MockAPI
		mockEvents *events.MockHandler
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), nil, nil, cfg, mockJob, mockEvents, nil)
	})

	registerCluster := func() *models.Cluster {
		clusterId := strfmt.UUID(uuid.New().String())
		cluster := models.Cluster{
			ID: &clusterId,
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		return &cluster
	}

	It("success", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
	})

	It("success with proxy", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{ProxyURL: "http://1.1.1.1:1234"},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
	})
	It("cluster_not_exists", func() {
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         strfmt.UUID(uuid.New().String()),
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISONotFound()))
	})

	It("failed_to_create_job", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOInternalServerError()))
	})

	It("job_failed", func() {
		clusterId := registerCluster().ID
		mockJob.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOInternalServerError()))
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
		mockJob     *job.MockAPI
		mockEvents  *events.MockHandler
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, nil, cfg, mockJob, mockEvents, nil)
	})

	It("get_next_steps_unknown_host", func() {
		clusterId := strToUUID(uuid.New().String())
		unregistered_hostID := strToUUID(uuid.New().String())

		generateReply := bm.GetNextSteps(ctx, installer.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *unregistered_hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGetNextStepsNotFound()))
	})

	It("get_next_steps_success", func() {
		clusterId := strToUUID(uuid.New().String())
		hostId := strToUUID(uuid.New().String())
		host := models.Host{
			ID:        hostId,
			ClusterID: *clusterId,
			Status:    swag.String("discovering"),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		var err error
		expectedStepsReply := models.Steps{&models.Step{StepType: models.StepTypeHardwareInfo},
			&models.Step{StepType: models.StepTypeConnectivityCheck}}
		mockHostApi.EXPECT().GetNextSteps(gomock.Any(), gomock.Any()).Return(expectedStepsReply, err)
		reply := bm.GetNextSteps(ctx, installer.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *hostId,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetNextStepsOK()))
		stepsReply := reply.(*installer.GetNextStepsOK).Payload
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

var _ = Describe("UpdateHostInstallProgress", func() {
	var (
		bm          *bareMetalInventory
		cfg         Config
		db          *gorm.DB
		ctx         = context.Background()
		ctrl        *gomock.Controller
		mockJob     *job.MockAPI
		mockHostApi *host.MockAPI
		mockEvents  *events.MockHandler
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, nil, cfg, mockJob, mockEvents, nil)
	})

	Context("host exists", func() {
		var hostID, clusterID strfmt.UUID
		BeforeEach(func() {
			hostID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&models.Host{
				ID:        &hostID,
				ClusterID: clusterID,
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("success", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), hostID.String(), gomock.Any(), gomock.Any(), clusterID.String())
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
				ClusterID:                 clusterID,
				HostInstallProgressParams: "some progress",
				HostID:                    hostID,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
		})

		It("update_failed", func() {
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))
			reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
				ClusterID:                 clusterID,
				HostInstallProgressParams: "some progress",
				HostID:                    hostID,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
		})
	})

	It("host_dont_exist", func() {
		reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
			ClusterID:                 strfmt.UUID(uuid.New().String()),
			HostInstallProgressParams: "some progress",
			HostID:                    strfmt.UUID(uuid.New().String()),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})
})

var _ = Describe("cluster", func() {
	masterHostId1 := strfmt.UUID(uuid.New().String())
	masterHostId2 := strfmt.UUID(uuid.New().String())
	masterHostId3 := strfmt.UUID(uuid.New().String())
	masterHostId4 := strfmt.UUID(uuid.New().String())

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		ctrl           *gomock.Controller
		mockHostApi    *host.MockAPI
		mockClusterApi *cluster.MockAPI
		mockJob        *job.MockAPI
		clusterID      strfmt.UUID
		mockEvents     *events.MockHandler
	)

	addHost := func(hostId strfmt.UUID, role string, state string, clusterId strfmt.UUID, inventory string, db *gorm.DB) models.Host {
		host := models.Host{
			ID:        &hostId,
			ClusterID: clusterId,
			Status:    swag.String(state),
			Role:      role,
			Inventory: inventory,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		return host
	}

	updateMachineCidr := func(clusterID strfmt.UUID, machineCidr string, db *gorm.DB) {
		Expect(db.Model(&models.Cluster{ID: &clusterID}).UpdateColumn("machine_network_cidr", machineCidr).Error).To(Not(HaveOccurred()))
	}

	getDisk := func() *models.Disk {
		disk := models.Disk{DriveType: "SSD", Name: "loop0", SizeBytes: 0}
		return &disk
	}
	setDefaultInstall := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	}
	setDefaultGetMasterNodesIds := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, nil).Times(2)
	}
	set4GetMasterNodesIds := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3, &masterHostId4}, nil)
	}
	setDefaultJobCreate := func(mockJobApi *job.MockAPI) {
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	setDefaultJobMonitor := func(mockJobApi *job.MockAPI) {
		mockJob.EXPECT().Monitor(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	setDefaultHostInstall := func(mockClusterApi *cluster.MockAPI) {
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	}
	setDefaultHostGetHostValidDisks := func(mockClusterApi *cluster.MockAPI) {
		mockHostApi.EXPECT().GetHostValidDisks(gomock.Any()).Return([]*models.Disk{getDisk()}, nil).AnyTimes()
	}
	setDefaultHostSetBootstrap := func(mockClusterApi *cluster.MockAPI) {
		mockHostApi.EXPECT().SetBootstrap(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}

	getInventoryStr := func(ipv4Addresses ...string) string {
		inventory := models.Inventory{Interfaces: []*models.Interface{
			{
				IPV4Addresses: append(make([]string, 0), ipv4Addresses...),
			},
		}}
		ret, _ := json.Marshal(&inventory)
		return string(ret)
	}

	sortedHosts := func(arr []strfmt.UUID) []strfmt.UUID {
		sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })
		return arr
	}

	sortedNetworks := func(arr []*models.HostNetwork) []*models.HostNetwork {
		sort.Slice(arr, func(i, j int) bool { return arr[i].Cidr < arr[j].Cidr })
		return arr
	}

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		mockClusterApi = cluster.NewMockAPI(ctrl)
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, mockClusterApi, cfg, mockJob, mockEvents, nil)
	})

	Context("Get", func() {
		{
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&models.Cluster{
					ID:                 &clusterID,
					APIVip:             "10.11.12.13",
					IngressVip:         "10.11.12.14",
					MachineNetworkCidr: "10.11.0.0/16",
				}).Error
				Expect(err).ShouldNot(HaveOccurred())

				addHost(masterHostId1, "master", "known", clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, "master", "known", clusterID, getInventoryStr("1.2.3.5/24", "10.11.50.80/16"), db)
				addHost(masterHostId3, "master", "known", clusterID, getInventoryStr("1.2.3.6/24", "7.8.9.10/24"), db)
			})

			It("GetCluster", func() {
				reply := bm.GetCluster(ctx, installer.GetClusterParams{
					ClusterID: clusterID,
				})
				actual, ok := reply.(*installer.GetClusterOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload.APIVip).To(BeEquivalentTo("10.11.12.13"))
				Expect(actual.Payload.IngressVip).To(BeEquivalentTo("10.11.12.14"))
				Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
				expectedNetworks := sortedNetworks([]*models.HostNetwork{
					{
						Cidr: "1.2.3.0/24",
						HostIds: sortedHosts([]strfmt.UUID{
							masterHostId1,
							masterHostId2,
							masterHostId3,
						}),
					},
					{
						Cidr: "10.11.0.0/16",
						HostIds: sortedHosts([]strfmt.UUID{
							masterHostId1,
							masterHostId2,
						}),
					},
					{
						Cidr: "7.8.9.0/24",
						HostIds: []strfmt.UUID{
							masterHostId3,
						},
					},
				})
				actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
				Expect(len(actualNetworks)).To(Equal(3))
				actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
				actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
				actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
				Expect(actualNetworks).To(Equal(expectedNetworks))
			})
		}
	})

	Context("Update", func() {
		BeforeEach(func() {
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&models.Cluster{
				ID: &clusterID,
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

			addHost(masterHostId1, "master", "known", clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, "master", "known", clusterID, getInventoryStr("1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, "master", "known", clusterID, getInventoryStr("1.2.3.6/24", "7.8.9.10/24"), db)
		})
		It("Invalid pull-secret", func() {
			pullSecret := "asdfasfda"
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					PullSecret: &pullSecret,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterBadRequest()))
		})
		It("No machine network", func() {
			apiVip := "8.8.8.8"
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip: &apiVip,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterBadRequest()))
		})
		It("Api and ingress mismatch", func() {
			apiVip := "10.11.12.15"
			ingressVip := "1.2.3.20"
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip:     &apiVip,
					IngressVip: &ingressVip,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterBadRequest()))
		})
		It("Update success", func() {
			apiVip := "10.11.12.15"
			ingressVip := "10.11.12.16"
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(3)
			mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					APIVip:     &apiVip,
					IngressVip: &ingressVip,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			actual := reply.(*installer.UpdateClusterCreated)
			Expect(actual.Payload.APIVip).To(Equal(apiVip))
			Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
			Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
			expectedNetworks := sortedNetworks([]*models.HostNetwork{
				{
					Cidr: "1.2.3.0/24",
					HostIds: sortedHosts([]strfmt.UUID{
						masterHostId1,
						masterHostId2,
						masterHostId3,
					}),
				},
				{
					Cidr: "10.11.0.0/16",
					HostIds: sortedHosts([]strfmt.UUID{
						masterHostId1,
						masterHostId2,
					}),
				},
				{
					Cidr: "7.8.9.0/24",
					HostIds: []strfmt.UUID{
						masterHostId3,
					},
				},
			})
			actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
			Expect(len(actualNetworks)).To(Equal(3))
			actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
			actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
			actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
			Expect(actualNetworks).To(Equal(expectedNetworks))
		})
	})

	Context("Install", func() {
		BeforeEach(func() {
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&models.Cluster{
				ID:                 &clusterID,
				APIVip:             "10.11.12.13",
				IngressVip:         "10.11.20.50",
				MachineNetworkCidr: "10.11.0.0/16",
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

			addHost(masterHostId1, "master", "known", clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, "master", "known", clusterID, getInventoryStr("1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, "master", "known", clusterID, getInventoryStr("10.11.200.180/16"), db)
		})

		It("success", func() {

			setDefaultInstall(mockClusterApi)
			setDefaultGetMasterNodesIds(mockClusterApi)

			setDefaultJobCreate(mockJob)
			setDefaultJobMonitor(mockJob)

			setDefaultHostInstall(mockClusterApi)
			setDefaultHostGetHostValidDisks(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
		})
		It("cidr calculate error", func() {
			updateMachineCidr(clusterID, "", db)
			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("cidr mismatch", func() {
			updateMachineCidr(clusterID, "1.1.0.0/16", db)
			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("Additional non matching master", func() {
			addHost(masterHostId4, "master", "known", clusterID, getInventoryStr("10.12.200.180/16"), db)
			set4GetMasterNodesIds(mockClusterApi)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("cluster failed to update", func() {
			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, nil)
			mockClusterApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("cluster has a error"))
			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusConflict)
		})
		It("host failed to install", func() {

			setDefaultInstall(mockClusterApi)
			setDefaultGetMasterNodesIds(mockClusterApi)

			mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.Errorf("host has a error")).AnyTimes()
			setDefaultHostGetHostValidDisks(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusConflict)
		})
		It("GetMasterNodesIds fails", func() {

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})

			verifyApiError(reply, http.StatusInternalServerError)
		})
		It("GetMasterNodesIds returns empty list", func() {

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})

			verifyApiError(reply, http.StatusInternalServerError)
		})
	})
	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})
})

var _ = Describe("KubeConfig download", func() {

	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		ctrl         *gomock.Controller
		mockS3Client *awsS3Client.MockS3Client
		clusterID    strfmt.UUID
		c            models.Cluster
		mockJob      *job.MockAPI
		clusterApi   cluster.API
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		clusterID = strfmt.UUID(uuid.New().String())
		mockS3Client = awsS3Client.NewMockS3Client(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		clusterApi = cluster.NewManager(getTestLog().WithField("pkg", "cluster-monitor"), db, nil)

		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), nil, clusterApi, cfg, mockJob, nil, mockS3Client)
		c = models.Cluster{
			ID:     &clusterID,
			APIVip: "10.11.12.13",
		}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("kubeconfig download no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: *clusterId,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewDownloadClusterKubeconfigNotFound()))
	})
	It("kubeconfig download cluster is not in installed state", func() {
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewDownloadClusterKubeconfigConflict()))

	})
	It("kubeconfig download s3download failure", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, fileName, "test").Return(nil, errors.Errorf("dummy"))
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewDownloadClusterKubeconfigConflict()))
	})
	It("kubeconfig download happy flow", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, fileName, "test").Return(r, nil)
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(r), kubeconfig)))
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
	})
})

var _ = Describe("UploadClusterIngressCert test", func() {

	var (
		bm                  *bareMetalInventory
		cfg                 Config
		db                  *gorm.DB
		ctx                 = context.Background()
		ctrl                *gomock.Controller
		mockS3Client        *awsS3Client.MockS3Client
		clusterID           strfmt.UUID
		c                   models.Cluster
		ingressCa           models.IngressCertParams
		kubeconfigFile      *os.File
		kubeconfigNoingress string
		kubeconfigObject    string
		mockJob             *job.MockAPI
		clusterApi          cluster.API
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = prepareDB()
		ingressCa = "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"MQswCQYDVQQHDAJkZDELMAkGA1UECgwCZGQxCzAJBgNVBAsMAmRkMQswCQYDVQQDDAJkZDERMA8GCSqGSIb3DQEJARYCZGQwHhcNMjAwNTI1MTYwNTAwWhcNMzA" +
			"wNTIzMTYwNTAwWjBhMQswCQYDVQQGEwJpczELMAkGA1UECAwCZGQxCzAJBgNVBAcMAmRkMQswCQYDVQQKDAJkZDELMAkGA1UECwwCZGQxCzAJBgNVBAMMAmRkMREwDwYJKoZIh" +
			"vcNAQkBFgJkZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAML63CXkBb+lvrJKfdfYBHLDYfuaC6exCSqASUAosJWWrfyDiDMUbmfs06PLKyv7N8efDhza74ov0EQJ" +
			"NRhMNaCE+A0ceq6ZXmmMswUYFdLAy8K2VMz5mroBFX8sj5PWVr6rDJ2ckBaFKWBB8NFmiK7MTWSIF9n8M107/9a0QURCvThUYu+sguzbsLODFtXUxG5rtTVKBVcPZvEfRky2Tkt4AySFS" +
			"mkO6Kf4sBd7MC4mKWZm7K8k7HrZYz2usSpbrEtYGtr6MmN9hci+/ITDPE291DFkzIcDCF493v/3T+7XsnmQajh6kuI+bjIaACfo8N+twEoJf/N1PmphAQdEiC0CAwEAAaNTMFEwHQYDVR0O" +
			"BBYEFNvmSprQQ2HUUtPxs6UOuxq9lKKpMB8GA1UdIwQYMBaAFNvmSprQQ2HUUtPxs6UOuxq9lKKpMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJEWxnxtQV5IqPVRr2SM" +
			"WNNxcJ7A/wyet39l5VhHjbrQGynk5WS80psn/riLUfIvtzYMWC0IR0pIMQuMDF5sNcKp4D8Xnrd+Bl/4/Iy/iTOoHlw+sPkKv+NL2XR3iO8bSDwjtjvd6L5NkUuzsRoSkQCG2fHASqqgFoyV9Ld" +
			"RsQa1w9ZGebtEWLuGsrJtR7gaFECqJnDbb0aPUMixmpMHID8kt154TrLhVFmMEqGGC1GvZVlQ9Of3GP9y7X4vDpHshdlWotOnYKHaeu2d5cRVFHhEbrslkISgh/TRuyl7VIpnjOYUwMBpCiVH6M" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
		clusterID = strfmt.UUID(uuid.New().String())
		mockS3Client = awsS3Client.NewMockS3Client(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		clusterApi = cluster.NewManager(getTestLog().WithField("pkg", "cluster-monitor"), db, nil)
		mockJob.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		bm = NewBareMetalInventory(db, getTestLog(), nil, clusterApi, cfg, mockJob, nil, mockS3Client)
		c = models.Cluster{
			ID:     &clusterID,
			APIVip: "10.11.12.13",
		}
		kubeconfigNoingress = fmt.Sprintf("%s/%s", clusterID, "kubeconfig-noingress")
		kubeconfigObject = fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		kubeconfigFile, err = os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())

	})

	objectExists := func() {
		mockS3Client.EXPECT().DoesObjectExists(ctx, kubeconfigObject, "test").Return(false, nil).Times(1)
	}

	It("UploadClusterIngressCert no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         *clusterId,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertNotFound()))
	})
	It("UploadClusterIngressCert cluster is not in installed state", func() {
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertBadRequest()))

	})
	It("UploadClusterIngressCert kubeconfig already exists, return ok", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExists(ctx, kubeconfigObject, "test").Return(true, nil).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertCreated()))
	})
	It("UploadClusterIngressCert DoesObjectExists fails ", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExists(ctx, kubeconfigObject, "test").Return(true, errors.Errorf("dummy")).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert s3download failure", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, kubeconfigNoingress, "test").Return(nil, errors.Errorf("dummy")).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert bad kubeconfig, mergeIngressCaIntoKubeconfig failure", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		objectExists()
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, kubeconfigNoingress, "test").Return(r, nil).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert bad ingressCa, mergeIngressCaIntoKubeconfig failure", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, kubeconfigNoingress, "test").Return(kubeconfigFile, nil)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: "bad format",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})

	It("UploadClusterIngressCert push fails", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := ioutil.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(merged).ShouldNot(Equal(kubeConfigAsBytes))
		Expect(merged).ShouldNot(Equal([]byte(ingressCa)))
		objectExists()
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, kubeconfigNoingress, "test").Return(kubeconfigFile, nil).Times(1)
		mockS3Client.EXPECT().PushDataToS3(ctx, merged, kubeconfigObject, "test").Return(errors.Errorf("Dummy"))
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})

	It("UploadClusterIngressCert download happy flow", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := ioutil.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		objectExists()
		mockS3Client.EXPECT().DownloadFileFromS3(ctx, kubeconfigNoingress, "test").Return(kubeconfigFile, nil).Times(1)
		mockS3Client.EXPECT().PushDataToS3(ctx, merged, kubeconfigObject, "test").Return(nil)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(Equal(installer.NewUploadClusterIngressCertCreated()))
	})

	AfterEach(func() {
		ctrl.Finish()
		db.Close()
		kubeconfigFile.Close()
	})
})

func verifyApiError(responder middleware.Responder, expectedHttpStatus int32) {
	ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
	conncreteError := responder.(*common.ApiErrorResponse)
	ExpectWithOffset(1, conncreteError.StatusCode()).To(Equal(expectedHttpStatus))
}
