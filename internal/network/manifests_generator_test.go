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
	"github.com/openshift/assisted-service/mocks"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var _ = Describe("chrony manifest", func() {
	createHost := func(clusterId strfmt.UUID, sources []*models.NtpSource) *models.Host {
		b, err := json.Marshal(&sources)
		Expect(err).ShouldNot(HaveOccurred())
		hostID := strfmt.UUID(uuid.New().String())
		return &models.Host{
			ID:         &hostID,
			NtpSources: string(b),
			ClusterID:  &clusterId,
			InfraEnvID: clusterId,
		}
	}

	Context("Create NTP Manifest", func() {
		It("no_ntp_sources", func() {
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, &models.Host{})

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster, logrus.New())
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("same_ntp_source", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			clusterId := strfmt.UUID(uuid.New().String())
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(clusterId, toMarshal))
			hosts = append(hosts, createHost(clusterId, toMarshal))

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster, logrus.New())
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			expectedContent += fmt.Sprintf("\nserver %s iburst", common.TestNTPSourceSynced.SourceName)
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("skip_disabled_hosts", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
			}

			clusterId := strfmt.UUID(uuid.New().String())
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(clusterId, toMarshal))
			hosts[0].Status = swag.String(models.HostStatusDisabled)

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster, logrus.New())
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})

		It("multiple_ntp_source", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			clusterId := strfmt.UUID(uuid.New().String())
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(clusterId, toMarshal))
			hosts = append(hosts, createHost(clusterId, []*models.NtpSource{{SourceName: "3.3.3.3", SourceState: models.SourceStateSynced}}))

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster, logrus.New())
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
			dbName       string
			clusterId    strfmt.UUID
			cluster      common.Cluster
		)

		BeforeEach(func() {
			log = logrus.New()
			ctrl = gomock.NewController(GinkgoT())
			manifestsApi = mocks.NewMockManifestsAPI(ctrl)
			ntpUtils = NewManifestsGenerator(manifestsApi, Config{})
			db, dbName = common.PrepareTestDB()
			clusterId = strfmt.UUID(uuid.New().String())

			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(clusterId, []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}))
			hosts = append(hosts, createHost(clusterId, []*models.NtpSource{{SourceName: "3.3.3.3", SourceState: models.SourceStateSynced}}))

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

var _ = Describe("dnsmasq manifest", func() {

	Context("Create dnsmasq Manifest", func() {
		It("Happy flow", func() {
			cluster := createCluster("", []string{"3.3.3.0/24"},
				createInventory(createInterface("3.3.3.3/24")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"

			var manifestParams = map[string]string{
				"CLUSTER_NAME": cluster.Cluster.Name,
				"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
				"HOST_IP":      "3.3.3.3",
			}

			log := logrus.New()

			content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
			Expect(err).To(Not(HaveOccurred()))

			forcedns, err := fillTemplate(manifestParams, forceDnsDispatcherScript, log)
			Expect(err).To(Not(HaveOccurred()))

			created, err := createDnsmasqForSingleNode(log, cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(content)))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(forcedns)))
		})

		It("Happy flow ipv6", func() {
			cluster := createCluster("", []string{"1001:db8::/120"},
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"

			var manifestParams = map[string]string{
				"CLUSTER_NAME": cluster.Cluster.Name,
				"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
				"HOST_IP":      "1001:db8::1",
			}

			log := logrus.New()

			content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
			Expect(err).To(Not(HaveOccurred()))

			forcedns, err := fillTemplate(manifestParams, forceDnsDispatcherScript, log)
			Expect(err).To(Not(HaveOccurred()))

			created, err := createDnsmasqForSingleNode(log, cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(content)))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(forcedns)))
		})

		It("Happy flow dual stack - ipv6", func() {
			cluster := createCluster("", []string{"1001:db8::/120"},
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24"), "1001:db8::1/120", "2001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]string{
				"CLUSTER_NAME": cluster.Cluster.Name,
				"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
				"HOST_IP":      "1001:db8::1",
			}

			log := logrus.New()

			content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
			Expect(err).To(Not(HaveOccurred()))

			forcedns, err := fillTemplate(manifestParams, forceDnsDispatcherScript, log)
			Expect(err).To(Not(HaveOccurred()))

			created, err := createDnsmasqForSingleNode(log, cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(content)))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(forcedns)))
		})

		It("Happy flow dual stack - ipv4", func() {
			cluster := createCluster("", []string{"3.3.3.0/24"},
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24", "1.2.3.4/24"), "1001:db8::1/120", "2001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]string{
				"CLUSTER_NAME": cluster.Cluster.Name,
				"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
				"HOST_IP":      "3.3.3.3",
			}

			log := logrus.New()

			content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
			Expect(err).To(Not(HaveOccurred()))

			forcedns, err := fillTemplate(manifestParams, forceDnsDispatcherScript, log)
			Expect(err).To(Not(HaveOccurred()))

			created, err := createDnsmasqForSingleNode(log, cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(content)))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(forcedns)))
		})

		It("Happy flow dual stack - no machine cidr", func() {
			cluster := createCluster("", []string{""},
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24"), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]string{
				"CLUSTER_NAME": cluster.Cluster.Name,
				"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
				"HOST_IP":      "3.3.3.3",
			}

			log := logrus.New()

			content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
			Expect(err).To(Not(HaveOccurred()))

			forcedns, err := fillTemplate(manifestParams, forceDnsDispatcherScript, log)
			Expect(err).To(Not(HaveOccurred()))

			created, err := createDnsmasqForSingleNode(log, cluster)
			Expect(err).To(Not(HaveOccurred()))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(content)))
			Expect(created).To(ContainSubstring(base64.StdEncoding.EncodeToString(forcedns)))
		})

		It("no bootstrap", func() {
			cluster := createCluster("", []string{"3.3.3.0/24"},
				createInventory(createInterface("3.3.3.3/24")))

			_, err := createDnsmasqForSingleNode(logrus.New(), cluster)
			Expect(err).To(HaveOccurred())
		})
	})

})

var _ = Describe("telemeter manifest", func() {

	var (
		ctx                   = context.Background()
		log                   *logrus.Logger
		ctrl                  *gomock.Controller
		mockManifestsApi      *mocks.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
		clusterId             strfmt.UUID
		cluster               common.Cluster
	)

	BeforeEach(func() {

		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		mockManifestsApi = mocks.NewMockManifestsAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())

		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	for _, test := range []struct {
		envName        string
		serviceBaseURL string
	}{
		{
			envName:        "Stage env",
			serviceBaseURL: stageServiceBaseURL,
		},
		{
			envName:        "Integration env",
			serviceBaseURL: integrationServiceBaseURL,
		},
		{
			envName: "Other envs",
		},
	} {
		test := test
		Context(test.envName, func() {

			BeforeEach(func() {
				manifestsGeneratorApi = NewManifestsGenerator(mockManifestsApi, Config{ServiceBaseURL: test.serviceBaseURL})
			})

			It("happy flow", func() {
				if test.envName == "Stage env" || test.envName == "Integration env" {
					mockManifestsApi.EXPECT().CreateClusterManifest(ctx, gomock.Any()).Return(operations.NewCreateClusterManifestCreated())
				}
				err := manifestsGeneratorApi.AddTelemeterManifest(ctx, log, &cluster)
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("AddTelemeterManifest failure", func() {
				if test.envName != "Stage env" && test.envName != "Integration env" {
					Skip("We don't create any additional manifest in prod")
				}
				mockManifestsApi.EXPECT().CreateClusterManifest(ctx, gomock.Any()).Return(common.GenerateErrorResponder(errors.Errorf("failed to upload to s3")))
				err := manifestsGeneratorApi.AddTelemeterManifest(ctx, log, &cluster)
				Expect(err).Should(HaveOccurred())
			})
		})
	}
})

var _ = Describe("schedulable masters manifest", func() {
	var (
		ctx                   = context.Background()
		log                   *logrus.Logger
		ctrl                  *gomock.Controller
		manifestsApi          *mocks.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
		clusterId             strfmt.UUID
		cluster               common.Cluster
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = mocks.NewMockManifestsAPI(ctrl)
		manifestsGeneratorApi = NewManifestsGenerator(manifestsApi, Config{})
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())

		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("CreateClusterManifest success", func() {
		It("CreateClusterManifest success", func() {
			manifestsApi.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(operations.NewCreateClusterManifestCreated()).Times(1)
			Expect(manifestsGeneratorApi.AddSchedulableMastersManifest(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})

		It("CreateClusterManifest failure", func() {
			manifestsApi.EXPECT().CreateClusterManifest(gomock.Any(), gomock.Any()).Return(common.GenerateErrorResponder(errors.Errorf("failed to upload to s3"))).Times(1)
			Expect(manifestsGeneratorApi.AddSchedulableMastersManifest(ctx, log, &cluster)).Should(HaveOccurred())
		})
	})
})
