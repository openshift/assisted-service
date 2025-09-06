package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("tangConnectivitycheckcmd", func() {
	ctx := context.Background()
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var tangConnectivityCheckCmd *tangConnectivityCheckCmd
	var id, clusterID, infraEnvID strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		tangConnectivityCheckCmd = NewTangConnectivityCheckCmd(common.GetTestLog(), db, "quay.io/example/assisted-installer-agent:latest")

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID}}
	})

	const hostIgnition = `{
		"ignition": {
		  "config": {},
		  "version": "3.2.0"
		},
		"storage": {
			"luks": [
				{
				  "clevis": {
					"tang": [
					  {
						"thumbprint": "nWW89qAs1hDPKiIcae-ey2cQmUk",
						"url": "http://foo.bar"
					  }
					]
				  },
				  "device": "/dev/disk/by-partlabel/root",
				  "name": "root",
				  "options": [
					"--cipher",
					"aes-cbc-essiv:sha256"
				  ],
				  "wipeVolume": true
				}
			],
		  "files": []
		}
	  }`

	const hostIgnitionWithoutLuks = `{
		"ignition": {
		  "config": {},
		  "version": "3.2.0"
		},
		"storage": {
		  "files": []
		}
	  }`

	const hostIgnitionWithoutClevis = `{
		"ignition": {
		  "config": {},
		  "version": "3.2.0"
		},
		"storage": {
			"luks": [
			],
		  "files": []
		}
	  }`

	Context("with a day2 host", func() {
		BeforeEach(func() {
			host = hostutil.GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		})

		It("skips tang check when Luks is undefined in host ignition", func() {
			apiVipConnectivity, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: true,
				Ignition:  hostIgnitionWithoutLuks,
			})
			Expect(err).ToNot(HaveOccurred())
			host.APIVipConnectivity = string(apiVipConnectivity)
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())
			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("skips tang check when Clevis is undefined in host ignition", func() {
			apiVipConnectivity, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: true,
				Ignition:  hostIgnitionWithoutClevis,
			})
			Expect(err).ToNot(HaveOccurred())
			host.APIVipConnectivity = string(apiVipConnectivity)
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())
			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("runs tang connectivity check using tang servers from host ignition", func() {
			apiVipConnectivity, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: true,
				Ignition:  hostIgnition,
			})
			Expect(err).ToNot(HaveOccurred())
			host.APIVipConnectivity = string(apiVipConnectivity)
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepReply[0]).ShouldNot(BeNil())
			Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"thumbprint\\\":\\\"nWW89qAs1hDPKiIcae-ey2cQmUk\\\",\\\"url\\\":\\\"http://foo.bar\\\"}]\"}"))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("skips tang check when APIVipConnectivity is empty for day 2 host", func() {
			host.APIVipConnectivity = ""
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(len(stepReply)).Should(Equal(0))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("returns error when host ignition contains invalid JSON", func() {
			apiVipConnectivity, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: true,
				Ignition:  "invalid json",
			})
			Expect(err).ToNot(HaveOccurred())
			host.APIVipConnectivity = string(apiVipConnectivity)
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepErr).Should(HaveOccurred())
		})
	})

	Context("with a day1 host", func() {
		BeforeEach(func() {
			host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusInsufficient)
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("runs tang connectivity check using tang servers from cluster configuration", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
			}
			Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepReply[0]).ShouldNot(BeNil())
			Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"URL\\\":\\\"http://tang.example.com:7500\\\",\\\"Thumbprint\\\":\\\"\\\"}]\"}"))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("runs tang connectivity check for worker node when EnableOnAll is configured", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
			}
			Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepReply[0]).ShouldNot(BeNil())
			Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"URL\\\":\\\"http://tang.example.com:7500\\\",\\\"Thumbprint\\\":\\\"\\\"}]\"}"))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("runs tang connectivity check for worker node when EnableOnWorkers is configured", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnWorkers),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
			}
			Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepReply[0]).ShouldNot(BeNil())
			Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"URL\\\":\\\"http://tang.example.com:7500\\\",\\\"Thumbprint\\\":\\\"\\\"}]\"}"))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("skips tang connectivity check for worker node when EnableOnMasters is configured", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
			}
			Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(len(stepReply)).Should(Equal(0)) // Host is a worker
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("skips tang connectivity check when EnableOnNone is configured", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnNone),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
			}
			Expect(db.Save(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(len(stepReply)).Should(Equal(0))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("skips tang connectivity check when TPMv2 mode is configured", func() {
			cluster.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(len(stepReply)).Should(Equal(0))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})

		It("returns error when cluster is not found in database", func() {
			unknownClusterID := strfmt.UUID(uuid.New().String())
			hostID := strfmt.UUID(uuid.New().String())
			hostWithUnknownCluster := hostutil.GenerateTestHost(hostID, infraEnvID, unknownClusterID, models.HostStatusInsufficient)
			Expect(db.Create(&hostWithUnknownCluster).Error).ShouldNot(HaveOccurred())

			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &hostWithUnknownCluster)
			Expect(stepReply).To(BeNil())
			Expect(stepErr).Should(HaveOccurred())
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
