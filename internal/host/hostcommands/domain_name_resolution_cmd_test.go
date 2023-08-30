package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("domainNameResolution", func() {
	ctx := context.Background()
	var host models.Host
	var cluster common.Cluster
	var db *gorm.DB
	var dCmd *domainNameResolutionCmd
	var id, clusterID, infraEnvID strfmt.UUID
	var stepReply []*models.Step
	var stepErr error
	var dbName string
	var name string
	var baseDNSDomain string
	var ctrl *gomock.Controller
	var mockVersions *versions.MockHandler

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockVersions = versions.NewMockHandler(ctrl)
		dCmd = NewDomainNameResolutionCmd(common.GetTestLog(), "quay.io/example/assisted-installer-agent:latest", mockVersions, db)
		id = strfmt.UUID(uuid.New().String())
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(id, infraEnvID, clusterID, models.HostStatusPreparingForInstallation)
		host.Inventory = hostutil.GenerateMasterInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		name = "example"
		baseDNSDomain = "test.com"
	})

	It("happy flow", func() {
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, Name: name, BaseDNSDomain: baseDNSDomain}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{URL: swag.String("quay.io/release")}, nil)
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).ToNot(BeNil())
		Expect(stepReply[0].StepType).To(Equal(models.StepTypeDomainResolution))
		Expect(stepErr).ShouldNot(HaveOccurred())
		Expect(stepReply[0].Args).To(HaveLen(1))
		var request models.DomainResolutionRequest
		Expect(json.Unmarshal([]byte(stepReply[0].Args[0]), &request)).ToNot(HaveOccurred())
		req := func(s string) models.DomainResolutionRequestDomain {
			return models.DomainResolutionRequestDomain{
				DomainName: swag.String(s),
			}
		}
		clusterDomain := func(prefix string) string {
			return fmt.Sprintf("%s.%s.%s", prefix, name, baseDNSDomain)
		}
		Expect(request.Domains).To(ContainElements(
			req(clusterDomain("api")),
			req(clusterDomain("api-int")),
			req(clusterDomain(constants.AppsSubDomainNameHostDNSValidation+".apps")),
			req(clusterDomain(constants.DNSWildcardFalseDomainName)),
			req(clusterDomain(constants.DNSWildcardFalseDomainName)+"."),
			req("quay.io"),
		))
	})

	It("Missing cluster name", func() {
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, BaseDNSDomain: baseDNSDomain}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{URL: swag.String("quay.io/release")}, nil)
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	It("Missing domain base name", func() {
		cluster = common.Cluster{Cluster: models.Cluster{ID: &clusterID, Name: name}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{URL: swag.String("quay.io/release")}, nil)
		stepReply, stepErr = dCmd.GetSteps(ctx, &host)
		Expect(stepReply).To(BeNil())
		Expect(stepErr).To(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		stepReply = nil
		stepErr = nil
	})
})
