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
	const validCACert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURFRENDQWZpZ0F3SUJBZ0lJUk90aUgvOC82ckF3RFFZSktvWklodmNOQVFFTEJRQXdKakVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1SQXdEZ1lEVlFRREV3ZHliMjkwTFdOaE1CNFhEVEl3TURreE9ERTVORFV3TVZvWApEVE13TURreE5qRTVORFV3TVZvd0pqRVNNQkFHQTFVRUN4TUpiM0JsYm5Ob2FXWjBNUkF3RGdZRFZRUURFd2R5CmIyOTBMV05oTUlJQklqQU5CZ2txaGtpRzl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUE1c1orVWtaaGsxUWQKeFU3cWI3YXArNFczaS9ZWTFzZktURC8ybDVJTjFJeVhPajlSL1N2VG5SOGYvajNJa1JHMWN5ZXR4bnNlNm1aZwpaOW1IRDJMV0srSEFlTTJSYXpuRkEwVmFwOWxVbVRrd3Vza2Z3QzhnMWJUZUVHUlEyQmFId09KekpvdjF4a0ZICmU2TUZCMlcxek1rTWxLTkwycnlzMzRTeVYwczJpNTFmTTJvTEM2SXRvWU91RVVVa2o0dnVUbThPYm5rV0t4ZnAKR1VGMThmNzVYeHJId0tVUEd0U0lYMGxpVGJNM0tiTDY2V2lzWkFIeStoN1g1dnVaaFYzYXhwTVFMdlczQ2xvcQpTaG9zSXY4SWNZbUJxc210d2t1QkN3cWxibEo2T2gzblFrelorVHhQdGhkdWsrZytzaVBUNi9va0JKU2M2cURjClBaNUNyN3FrR3dJREFRQUJvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvQkFVd0F3RUIKL3pBZEJnTlZIUTRFRmdRVWNSbHFHT1g3MWZUUnNmQ0tXSGFuV3NwMFdmRXdEUVlKS29aSWh2Y05BUUVMQlFBRApnZ0VCQU5Xc0pZMDY2RnNYdzFOdXluMEkwNUtuVVdOMFY4NVJVV2drQk9Wd0J5bHluTVRneGYyM3RaY1FsS0U4CjVHMlp4Vzl5NmpBNkwzMHdSNWhOcnBzM2ZFcUhobjg3UEM3L2tWQWlBOWx6NjBwV2ovTE5GU1hobDkyejBGMEIKcGNUQllFc1JNYU0zTFZOK0tZb3Q2cnJiamlXdmxFMU9hS0Q4dnNBdkk5YXVJREtOdTM0R2pTaUJGWXMrelRjSwphUUlTK3UzRHVYMGpVY001aUgrMmwzNGxNR0hlY2tjS1hnUWNXMGJiT28xNXY1Q2ExenJtQ2hIUHUwQ2NhMU1MCjJaM2MxMHVXZnR2OVZnbC9LcEpzSjM3b0phbTN1Mmp6MXN0K3hHby9iTmVSdHpOMjdXQSttaDZ6bXFwRldYKzUKdWFjZUY1SFRWc0FkbmtJWHpwWXBuek5qb0lFPQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg=="
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
		customCACert := validCACert
		expectedArgs := fmt.Sprintf("{\"ca_certificate\":\"%s\",\"url\":\"%s/worker\"}", customCACert, customEndpoint)
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_ca_certificate", customCACert).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply).ShouldNot(BeNil())
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedArgs))
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
		customCACert := validCACert
		expectedArgs := fmt.Sprintf("{\"ca_certificate\":\"%s\",\"ignition_endpoint_token\":\"%s\",\"request_headers\":[{\"key\":\"Authorization\",\"value\":\"Bearer %s\"}],\"url\":\"%s/%s\"}",
			customCACert, token, token, customEndpoint, poolName)
		Expect(db.Model(&host).Update("MachineConfigPoolName", poolName).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&host).Update("ignition_endpoint_token", token).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_url", customEndpoint).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&cluster).Update("ignition_endpoint_ca_certificate", customCACert).Error).ShouldNot(HaveOccurred())
		stepReply, stepErr = apivipConnectivityCheckCmd.GetSteps(ctx, &host)
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply).ShouldNot(BeNil())
		Expect(stepReply[0]).ShouldNot(BeNil())
		Expect(stepReply[0].Args[len(stepReply[0].Args)-1]).Should(Equal(expectedArgs))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
