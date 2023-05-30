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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

		It("multiple_ntp_source", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			clusterId := strfmt.UUID(uuid.New().String())
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(clusterId, toMarshal))

			sources := []*models.NtpSource{
				{SourceName: "3.3.3.3", SourceState: models.SourceStateSynced},
				{SourceName: "0.rhel.pool.ntp.org", SourceState: models.SourceStateCombined},
				{SourceName: "1.rhel.pool.ntp.org", SourceState: models.SourceStateNotCombined},
				{SourceName: "2.rhel.pool.ntp.org", SourceState: models.SourceStateError},
				{SourceName: "3.rhel.pool.ntp.org", SourceState: models.SourceStateVariable},
				{SourceName: "4.rhel.pool.ntp.org", SourceState: models.SourceStateUnreachable},
			}
			hosts = append(hosts, createHost(clusterId, sources))

			response, err := createChronyManifestContent(&common.Cluster{Cluster: models.Cluster{
				Hosts: hosts,
			}}, models.HostRoleMaster, logrus.New())
			Expect(err).ShouldNot(HaveOccurred())

			expectedContent := defaultChronyConf
			expectedContent += fmt.Sprintf("\nserver %s iburst", common.TestNTPSourceSynced.SourceName)
			expectedContent += fmt.Sprintf("\nserver %s iburst", common.TestNTPSourceUnsynced.SourceName)
			for _, s := range sources {
				expectedContent += fmt.Sprintf("\nserver %s iburst", s.SourceName)
			}
			Expect(response).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte(expectedContent))))
		})
	})

	Context("Add NTP Manifest", func() {
		var (
			ctx          = context.Background()
			log          *logrus.Logger
			ctrl         *gomock.Controller
			manifestsApi *manifestsapi.MockManifestsAPI
			ntpUtils     ManifestsGeneratorAPI
			db           *gorm.DB
			dbName       string
			clusterId    strfmt.UUID
			cluster      common.Cluster
		)

		BeforeEach(func() {
			log = logrus.New()
			ctrl = gomock.NewController(GinkgoT())
			manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
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
			manifestsApi.EXPECT().V2CreateClusterManifest(gomock.Any(), gomock.Any()).Times(0)
		})

		AfterEach(func() {
			ctrl.Finish()
			common.DeleteTestDB(db, dbName)
		})

		It("CreateClusterManifest success", func() {
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: "50-masters-chrony-configuration.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(1)
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: "50-workers-chrony-configuration.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(1)
			Expect(ntpUtils.AddChronyManifest(ctx, log, &cluster)).ShouldNot(HaveOccurred())

		})

		It("CreateClusterManifest failure", func() {
			fileName := "50-masters-chrony-configuration.yaml"
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(nil, errors.Errorf("Failed to create manifest %s", fileName)).Times(1)
			Expect(ntpUtils.AddChronyManifest(ctx, log, &cluster)).Should(HaveOccurred())
		})
	})
})

var _ = Describe("dnsmasq manifest", func() {

	Context("Create dnsmasq Manifest", func() {
		It("Happy flow", func() {
			cluster := createCluster("", "3.3.3.0/24",
				createInventory(createInterface("3.3.3.3/24")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"

			var manifestParams = map[string]interface{}{
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
			cluster := createCluster("", "1001:db8::/120",
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"

			var manifestParams = map[string]interface{}{
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
			cluster := createCluster("", "1001:db8::/120",
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24"), "1001:db8::1/120", "2001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]interface{}{
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
			cluster := createCluster("", "3.3.3.0/24",
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24", "1.2.3.4/24"), "1001:db8::1/120", "2001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]interface{}{
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
			cluster := createCluster("", "",
				createInventory(addIPv6Addresses(createInterface("3.3.3.3/24"), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			var manifestParams = map[string]interface{}{
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
			cluster := createCluster("", "3.3.3.0/24",
				createInventory(createInterface("3.3.3.3/24")))

			_, err := createDnsmasqForSingleNode(logrus.New(), cluster)
			Expect(err).To(HaveOccurred())
		})

		It("don't inject DNSMasq SNO manifest", func() {
			cluster := createCluster("", "1001:db8::/120",
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			clusterId := strfmt.UUID(uuid.New().String())
			cluster.ID = &clusterId

			ctrl := gomock.NewController(GinkgoT())
			mockManifestsApi := manifestsapi.NewMockManifestsAPI(ctrl)
			mockManifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: "dnsmasq-bootstrap-in-place.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(0)
			manifestsGenerator := NewManifestsGenerator(mockManifestsApi,
				Config{ServiceBaseURL: stageServiceBaseURL, EnableSingleNodeDnsmasq: false})
			err := manifestsGenerator.AddDnsmasqForSingleNode(context.TODO(), logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("inject DNSMasq SNO manifest", func() {
			cluster := createCluster("", "1001:db8::/120",
				createInventory(addIPv6Addresses(createInterface(), "1001:db8::1/120")))
			cluster.Hosts[0].Bootstrap = true
			cluster.Cluster.BaseDNSDomain = "test.com"
			cluster.Cluster.Name = "test"
			clusterId := strfmt.UUID(uuid.New().String())
			cluster.ID = &clusterId

			ctrl := gomock.NewController(GinkgoT())
			mockManifestsApi := manifestsapi.NewMockManifestsAPI(ctrl)
			mockManifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: "dnsmasq-bootstrap-in-place.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(1)
			manifestsGenerator := NewManifestsGenerator(mockManifestsApi,
				Config{ServiceBaseURL: stageServiceBaseURL, EnableSingleNodeDnsmasq: true})
			err := manifestsGenerator.AddDnsmasqForSingleNode(context.TODO(), logrus.New(), cluster)
			Expect(err).To(Not(HaveOccurred()))
		})
	})

})

var _ = Describe("telemeter manifest", func() {

	var (
		ctx                   = context.Background()
		log                   *logrus.Logger
		ctrl                  *gomock.Controller
		mockManifestsApi      *manifestsapi.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
		clusterId             strfmt.UUID
		cluster               common.Cluster
	)

	BeforeEach(func() {

		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		mockManifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())

		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
		mockManifestsApi.EXPECT().V2CreateClusterManifest(ctx, gomock.Any()).Times(0)
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

			fileName := "redirect-telemeter.yaml"
			It("happy flow", func() {
				if test.envName == "Stage env" || test.envName == "Integration env" {
					mockManifestsApi.EXPECT().CreateClusterManifestInternal(ctx, gomock.Any(), false).Return(&models.Manifest{
						FileName: fileName,
						Folder:   models.ManifestFolderOpenshift,
					},
						nil)
				}
				err := manifestsGeneratorApi.AddTelemeterManifest(ctx, log, &cluster)
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("AddTelemeterManifest failure", func() {
				if test.envName != "Stage env" && test.envName != "Integration env" {
					Skip("We don't create any additional manifest in prod")
				}
				mockManifestsApi.EXPECT().CreateClusterManifestInternal(ctx, gomock.Any(), false).Return(nil, errors.Errorf("Failed to create manifest %s", fileName))
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
		manifestsApi          *manifestsapi.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
		clusterId             strfmt.UUID
		cluster               common.Cluster
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		manifestsGeneratorApi = NewManifestsGenerator(manifestsApi, Config{})
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())

		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
		manifestsApi.EXPECT().V2CreateClusterManifest(gomock.Any(), gomock.Any()).Times(0)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("CreateClusterManifest success", func() {
		fileName := "cluster-scheduler-02-config.yml.patch_ai_set_masters_schedulable"
		It("CreateClusterManifest success", func() {
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: fileName,
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(1)
			Expect(manifestsGeneratorApi.AddSchedulableMastersManifest(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})

		It("CreateClusterManifest failure", func() {
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(nil, errors.Errorf("Failed to create manifest %s", fileName)).Times(1)
			Expect(manifestsGeneratorApi.AddSchedulableMastersManifest(ctx, log, &cluster)).Should(HaveOccurred())
		})
	})
})

var _ = Describe("disk encryption manifest", func() {

	var (
		ctx                   = context.Background()
		log                   *logrus.Logger
		ctrl                  *gomock.Controller
		mockManifestsApi      *manifestsapi.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
		clusterId             strfmt.UUID
		c                     common.Cluster
	)

	BeforeEach(func() {

		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		mockManifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		manifestsGeneratorApi = NewManifestsGenerator(mockManifestsApi, Config{})
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())
		c = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		mockManifestsApi.EXPECT().V2CreateClusterManifest(gomock.Any(), gomock.Any()).Times(0)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	for _, t := range []struct {
		name           string
		diskEncryption *models.DiskEncryption
		numOfManifests int
	}{
		{
			name: "masters and workers, tpmv2",
			diskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			numOfManifests: 2,
		},
		{
			name: "masters and workers, tang",
			diskEncryption: &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"url":"http://tang.invalid","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
			},
			numOfManifests: 2,
		},
		{
			name: "masters only, tpmv2",
			diskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			numOfManifests: 1,
		},
		{
			name: "masters only, tang",
			diskEncryption: &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"url":"http://tang.invalid","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
			},
			numOfManifests: 1,
		},
		{
			name: "workers only, tpmv2",
			diskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnWorkers),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			numOfManifests: 1,
		},
		{
			name: "workers only, tang",
			diskEncryption: &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnWorkers),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"url":"http://tang.invalid","thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
			},
			numOfManifests: 1,
		},
		{
			name: "disks encryption not set",
			// This is the default values for disk_encryption
			diskEncryption: &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			},
			numOfManifests: 0,
		},
	} {
		t := t

		It(t.name, func() {
			c.DiskEncryption = t.diskEncryption
			Expect(db.Create(&c).Error).NotTo(HaveOccurred())
			mockManifestsApi.EXPECT().CreateClusterManifestInternal(ctx, gomock.Any(), false).Times(t.numOfManifests)
			err := manifestsGeneratorApi.AddDiskEncryptionManifest(ctx, log, &c)
			Expect(err).ToNot(HaveOccurred())
		})
	}
})

var _ = Describe("node ip hint", func() {

	clusterCreate := func(machineCidr string) *common.Cluster {
		clusterId := strfmt.UUID(uuid.New().String())
		cluster := createCluster("", machineCidr,
			createInventory(&models.Interface{
				IPV4Addresses: append([]string{}, "3.3.3.3/24"),
				Name:          "test1",
			}, &models.Interface{
				IPV4Addresses: append([]string{}, "4.4.4.4/24"),
				Name:          "test2"}))
		cluster.ID = &clusterId
		cluster.Hosts[0].Bootstrap = true
		cluster.Cluster.BaseDNSDomain = "test.com"
		cluster.Cluster.Name = "test"
		cluster.OpenshiftVersion = "4.11.0"
		cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		return cluster
	}
	var (
		ctx                   = context.Background()
		log                   *logrus.Logger
		ctrl                  *gomock.Controller
		manifestsApi          *manifestsapi.MockManifestsAPI
		manifestsGeneratorApi ManifestsGeneratorAPI
		db                    *gorm.DB
		dbName                string
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		manifestsGeneratorApi = NewManifestsGenerator(manifestsApi, Config{})
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("CreateClusterManifest - node ip hint", func() {
		fileName := "node-ip-hint.yaml"
		It("CreateClusterManifest success", func() {
			cluster := clusterCreate("3.3.3.0/24")
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(&models.Manifest{
				FileName: fileName,
				Folder:   models.ManifestFolderOpenshift,
			}, nil).Times(2)

			Expect(manifestsGeneratorApi.AddNodeIpHint(ctx, log, cluster)).ShouldNot(HaveOccurred())
		})

		It("CreateClusterManifest failure no machine cidr", func() {
			cluster := clusterCreate("")
			Expect(manifestsGeneratorApi.AddNodeIpHint(ctx, log, cluster)).Should(HaveOccurred())
		})

		It("CreateClusterManifest failure bad machine cidr", func() {
			cluster := clusterCreate("bad_cidr")
			Expect(manifestsGeneratorApi.AddNodeIpHint(ctx, log, cluster)).Should(HaveOccurred())
		})

		It("Non sno cluster should do nothing", func() {
			cluster := clusterCreate("3.3.3.0/24")
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
			Expect(manifestsGeneratorApi.AddNodeIpHint(ctx, log, cluster)).ShouldNot(HaveOccurred())
		})

		It("No need to create manifest if openshift version is lower then supported", func() {
			cluster := clusterCreate("3.3.3.0/24")
			cluster.OpenshiftVersion = "4.10.14"
			Expect(manifestsGeneratorApi.AddNodeIpHint(ctx, log, cluster)).ShouldNot(HaveOccurred())
		})

		It("validate expected machine cidr is set", func() {
			cluster := clusterCreate("4.4.4.0/24")
			log := logrus.New()
			manifest, err := createNodeIpHintContent(log, cluster, "master")
			Expect(err).To(Not(HaveOccurred()))

			Expect(string(manifest[:])).To(ContainSubstring(base64.StdEncoding.EncodeToString([]byte("KUBELET_NODEIP_HINT=4.4.4.0"))))
		})
	})
})
