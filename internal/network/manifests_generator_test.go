package network

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/mocks"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var _ = Describe("chrony manifest", func() {
	createHost := func(sources []*models.NtpSource) *models.Host {
		b, err := json.Marshal(&sources)
		Expect(err).ShouldNot(HaveOccurred())
		hostID := strfmt.UUID(uuid.New().String())
		return &models.Host{
			ID:         &hostID,
			NtpSources: string(b),
		}
	}

	Context("Create NTP Manifest", func() {
		It("no_ntp_sources", func() {
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, &models.Host{})

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster)
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("same_ntp_source", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(toMarshal))
			hosts = append(hosts, createHost(toMarshal))

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster)
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			expectedContent += fmt.Sprintf("\nserver %s iburst", common.TestNTPSourceSynced.SourceName)
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("skip_disabled_hosts", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
			}

			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(toMarshal))
			hosts[0].Status = swag.String(models.HostStatusDisabled)

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster)
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("multiple_ntp_source", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(toMarshal))
			hosts = append(hosts, createHost([]*models.NtpSource{{SourceName: "3.3.3.3", SourceState: models.SourceStateSynced}}))

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster)
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			expectedContent += fmt.Sprintf("\nserver %s iburst", common.TestNTPSourceSynced.SourceName)
			expectedContent += "\nserver 3.3.3.3 iburst"
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})
	})

	Context("Add NTP Manifest", func() {
		var (
			ctx          = context.Background()
			log          *logrus.Logger
			ctrl         *gomock.Controller
			manifestsApi *mocks.MockManifestsAPI
			ntpUtils     ManifestsGeneratorAPI
			db           *gorm.DB
			dbName       = "ntp_utils"
			clusterId    strfmt.UUID
			cluster      common.Cluster
		)

		BeforeEach(func() {
			log = logrus.New()
			ctrl = gomock.NewController(GinkgoT())
			manifestsApi = mocks.NewMockManifestsAPI(ctrl)
			ntpUtils = NewManifestsGenerator(manifestsApi)
			db = common.PrepareTestDB(dbName, &events.Event{})
			clusterId = strfmt.UUID(uuid.New().String())

			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost([]*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}))
			hosts = append(hosts, createHost([]*models.NtpSource{{SourceName: "3.3.3.3", SourceState: models.SourceStateSynced}}))

			cluster = common.Cluster{
				Cluster: models.Cluster{
					ID:    &clusterId,
					Hosts: hosts,
				},
			}
			Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			ctrl.Finish()
			common.DeleteTestDB(db, dbName)
		})

		It("CreateClusterManifest success", func() {
			manifestsApi.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(operations.NewCreateClusterManifestCreated()).Times(2)
			Expect(ntpUtils.AddChronyManifest(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})

		It("CreateClusterManifest failure", func() {
			manifestsApi.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(common.GenerateErrorResponder(errors.Errorf("failed to upload to s3"))).Times(1)
			Expect(ntpUtils.AddChronyManifest(ctx, log, &cluster)).Should(HaveOccurred())
		})
	})
})
