package hostcommands

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
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
		apivipConnectivityCheckCmd = NewAPIVIPConnectivityCheckCmd(common.GetTestLog(), db, "quay.io/example/assisted-installer-agent:latest")

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
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"url\":\"http://test.com:22624/config/worker\"}"))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step_unknown_cluster_id", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		host.ClusterID = &clusterID
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).Should(HaveOccurred())
	})

	It("get_step custom pool name", func() {
		Expect(db.Model(&host).Update("MachineConfigPoolName", "testpool").Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal("{\"url\":\"http://test.com:22624/config/testpool\"}"))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step custom ignition endpoint", func() {
		customEndpoint := "https://foo.bar:33735/acme"
		expectedUrl := fmt.Sprintf("{\"url\":\"%s/worker\"}", customEndpoint)
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedUrl))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step custom ignition endpoint and CA cert", func() {
		customEndpoint := "https://foo.bar:33735/acme"
		customCACert := "somecertificatestring"
		expectedArgs := fmt.Sprintf("{\"ca_certificate\":\"%s\",\"url\":\"%s/worker\"}", customCACert, customEndpoint)
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_ca_certificate", customCACert).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedArgs))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step custom ignition endpoint and pool name", func() {
		customEndpoint := "https://foo.bar:33735/acme"
		poolName := "testpool"
		expectedUrl := fmt.Sprintf("{\"url\":\"%s/%s\"}", customEndpoint, poolName)
		Expect(db.Model(&host).Update("MachineConfigPoolName", poolName).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedUrl))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step custom token", func() {
		token := "verysecrettoken"
		expectedArgs := fmt.Sprintf("{\"ignition_endpoint_token\":\"%s\",\"request_headers\":[{\"key\":\"Authorization\",\"value\":\"Bearer %s\"}],\"url\":\"http://test.com:22624/config/worker\"}", token, token)
		Expect(db.Model(&host).Update("ignition_endpoint_token", token).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedArgs))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	It("get_step custom ignition endpoint, CA cert, token and pool name", func() {
		token := "verysecrettoken"
		poolName := "testpool"
		customEndpoint := "https://foo.bar:33735/acme"
		customCACert := "somecertificatestring"
		expectedArgs := fmt.Sprintf("{\"ca_certificate\":\"%s\",\"ignition_endpoint_token\":\"%s\",\"request_headers\":[{\"key\":\"Authorization\",\"value\":\"Bearer %s\"}],\"url\":\"%s/%s\"}",
			customCACert, token, token, customEndpoint, poolName)
		Expect(db.Model(&host).Update("MachineConfigPoolName", poolName).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&host).Update("ignition_endpoint_token", token).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_ca_certificate", customCACert).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedArgs))
		Expect(stepErr).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
