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
		host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID}}

	})

	It("get_step: Tang imported cluster should use host ignition", func() {

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

		By("Set cluster to imported and inject host ignition for test", func() {
			imported := true
			cluster.Imported = &imported
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			apiVipConnectivity, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: true,
				Ignition:  hostIgnition,
			})
			Expect(err).ToNot(HaveOccurred())
			host.APIVipConnectivity = string(apiVipConnectivity)
			Expect(db.Save(&host).Error).ShouldNot(HaveOccurred())
		})

		By("Ensure that tang connectivity check uses host ignition when the cluster is imported", func() {
			stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
			Expect(stepReply[0]).ShouldNot(BeNil())
			Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"thumbprint\\\":\\\"nWW89qAs1hDPKiIcae-ey2cQmUk\\\",\\\"url\\\":\\\"http://foo.bar\\\"}]\"}"))
			Expect(stepErr).ShouldNot(HaveOccurred())
		})
	})

	It("get_step: Tang EnableOnAll", func() {
		cluster.DiskEncryption = &models.DiskEncryption{
			EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"URL\\\":\\\"http://tang.example.com:7500\\\",\\\"Thumbprint\\\":\\\"\\\"}]\"}"))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step: Tang EnableOnWorkers", func() {
		cluster.DiskEncryption = &models.DiskEncryption{
			EnableOn:    swag.String(models.DiskEncryptionEnableOnWorkers),
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"tang_servers\":\"[{\\\"URL\\\":\\\"http://tang.example.com:7500\\\",\\\"Thumbprint\\\":\\\"\\\"}]\"}"))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step: Tang EnableOnMasters", func() {
		cluster.DiskEncryption = &models.DiskEncryption{
			EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(len(stepReply)).Should(Equal(0)) // Host is a worker
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step: Tang EnableOnNone", func() {
		cluster.DiskEncryption = &models.DiskEncryption{
			EnableOn:    swag.String(models.DiskEncryptionEnableOnNone),
			Mode:        swag.String(models.DiskEncryptionModeTang),
			TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(len(stepReply)).Should(Equal(0))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step: TPMv2 EnableOnAll", func() {
		cluster.DiskEncryption = &models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(len(stepReply)).Should(Equal(0))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step: unknown cluster_id", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		host.ClusterID = &clusterID
		stepReply, stepErr = tangConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).Should(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
