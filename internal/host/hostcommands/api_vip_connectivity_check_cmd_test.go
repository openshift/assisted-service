package hostcommands

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("apivipconnectivitycheckcmd", func() {
	ctx := context.Background()
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var apivipConnectivityCheckCmd *apivipConnectivityCheckCmd
	var id, clusterID, infraEnvID strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		apivipConnectivityCheckCmd = NewAPIVIPConnectivityCheckCmd(common.GetTestLog(), db, "quay.io/ocpmetal/assisted-installer-agent:latest", true)

		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHostAddedToCluster(id, infraEnvID, clusterID, models.HostStatusInsufficient)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		apiVipDNSName := "test.com"
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, APIVipDNSName: &apiVipDNSName}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	It("get_step", func() {
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"url\":\"http://test.com:22624/config/worker\",\"verify_cidr\":true}"))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step_unknown_cluster_id", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		host.ClusterID = &clusterID
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).Should(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
