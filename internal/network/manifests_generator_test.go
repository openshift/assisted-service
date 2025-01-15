package network

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"

	configv31 "github.com/coreos/ignition/v2/config/v3_1"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/models"
	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	"gorm.io/gorm"
	"sigs.k8s.io/yaml"
)

var _ = Describe("chrony manifest", func() {
	var (
		ctx          = context.Background()
		log          *logrus.Logger
		ctrl         *gomock.Controller
		manifestsApi *manifestsapi.MockManifestsAPI
		ntpUtils     *ManifestsGenerator
		db           *gorm.DB
		dbName       string
		clusterId    strfmt.UUID
		cluster      *common.Cluster
		infraEnvId   strfmt.UUID
		infraEnv     *common.InfraEnv
	)

	createHost := func(sources []*models.NtpSource) *models.Host {
		var sourcesText string
		if sources != nil {
			sourcesBytes, err := json.Marshal(&sources)
			Expect(err).ToNot(HaveOccurred())
			sourcesText = string(sourcesBytes)
		}
		hostID := strfmt.UUID(uuid.New().String())
		host := &models.Host{
			ID:         &hostID,
			NtpSources: sourcesText,
			ClusterID:  &clusterId,
			InfraEnvID: infraEnvId,
		}
		db.Create(host)
		Expect(db.Error).ToNot(HaveOccurred())
		return host
	}

	chronyConfServerRE := regexp.MustCompile(`(?m)^server\s+([^\s]+)\s+.*$`)

	extractChronyConf := func(machineConfigBytes []byte) string {
		var machineConfig *mcfgv1.MachineConfig
		err := yaml.Unmarshal(machineConfigBytes, &machineConfig)
		Expect(err).ToNot(HaveOccurred())
		config, _, err := configv31.Parse(machineConfig.Spec.Config.Raw)
		Expect(err).ToNot(HaveOccurred())
		var source string
		for _, file := range config.Storage.Files {
			if file.Path == "/etc/chrony.conf" {
				Expect(file.Contents.Source).ToNot(BeNil())
				source = *file.Contents.Source
				break
			}
		}
		Expect(source).ToNot(BeEmpty())
		data, err := dataurl.DecodeString(source)
		Expect(err).ToNot(HaveOccurred())
		return string(data.Data)
	}

	extractChronyConfServers := func(machineConfigBytes []byte) []string {
		chronyConf := extractChronyConf(machineConfigBytes)
		var serverList []string
		serverMatches := chronyConfServerRE.FindAllStringSubmatch(chronyConf, -1)
		for _, serverMatch := range serverMatches {
			serverList = append(serverList, serverMatch[1])
		}
		return serverList
	}

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		db, dbName = common.PrepareTestDB()
		ntpUtils = NewManifestsGenerator(manifestsApi, Config{}, db)

		clusterId = strfmt.UUID(uuid.NewString())
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		db.Create(cluster)
		Expect(db.Error).ToNot(HaveOccurred())

		infraEnvId = strfmt.UUID(uuid.NewString())
		infraEnv = &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:        &infraEnvId,
				ClusterID: clusterId,
			},
		}
		db.Create(infraEnv)
		Expect(db.Error).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Create NTP Manifest", func() {
		It("Adds nothing if there aren't sources in the host, cluster or infra-env", func() {
			cluster.Hosts = []*models.Host{
				createHost(nil),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(BeEmpty())
		})

		It("Eliminates duplicated sources from two hosts", func() {
			cluster.Hosts = []*models.Host{
				createHost([]*models.NtpSource{
					common.TestNTPSourceSynced,
					common.TestNTPSourceUnsynced,
				}),
				createHost([]*models.NtpSource{
					common.TestNTPSourceSynced,
					common.TestNTPSourceUnsynced,
				}),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(ConsistOf(
				common.TestNTPSourceSynced.SourceName,
				common.TestNTPSourceUnsynced.SourceName,
			))
		})

		It("Merges different sources from two hosts", func() {
			cluster.Hosts = []*models.Host{
				createHost([]*models.NtpSource{
					common.TestNTPSourceSynced,
					common.TestNTPSourceUnsynced,
				}),
				createHost([]*models.NtpSource{
					{
						SourceName:  "3.3.3.3",
						SourceState: models.SourceStateSynced,
					},
					{
						SourceName:  "0.rhel.pool.ntp.org",
						SourceState: models.SourceStateCombined,
					},
					{
						SourceName:  "1.rhel.pool.ntp.org",
						SourceState: models.SourceStateNotCombined,
					},
					{
						SourceName:  "2.rhel.pool.ntp.org",
						SourceState: models.SourceStateError,
					},
					{
						SourceName:  "3.rhel.pool.ntp.org",
						SourceState: models.SourceStateVariable,
					},
					{
						SourceName:  "4.rhel.pool.ntp.org",
						SourceState: models.SourceStateUnreachable,
					},
				}),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			actualChronyConfServers := extractChronyConfServers(response)
			var expectedChronyConfServers []string
			for _, host := range cluster.Hosts {
				var sources []*models.NtpSource
				if host.NtpSources != "" {
					err = json.Unmarshal([]byte(host.NtpSources), &sources)
					Expect(err).ToNot(HaveOccurred())
				}
				for _, source := range sources {
					expectedChronyConfServers = append(expectedChronyConfServers, source.SourceName)
				}
			}
			Expect(actualChronyConfServers).To(ConsistOf(expectedChronyConfServers))
		})

		It("Adds cluster sources if there are no sources in the host", func() {
			cluster.AdditionalNtpSource = "from.cluster.1, from.cluster.2"
			db.Save(cluster)
			Expect(db.Error).ToNot(HaveOccurred())

			cluster.Hosts = []*models.Host{
				createHost(nil),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(ConsistOf("from.cluster.1", "from.cluster.2"))
		})

		It("Adds infra-env sources if there are no sources in the host or in the cluster", func() {
			infraEnv.AdditionalNtpSources = "from.infraenv.1, from.infraenv.2"
			db.Save(infraEnv)
			Expect(db.Error).ToNot(HaveOccurred())

			cluster.Hosts = []*models.Host{
				createHost(nil),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(ConsistOf("from.infraenv.1", "from.infraenv.2"))
		})

		It("Ignores infra-env sources if there are sources in the cluster", func() {
			cluster.AdditionalNtpSource = "from.cluster"
			db.Save(cluster)
			Expect(db.Error).ToNot(HaveOccurred())

			infraEnv.AdditionalNtpSources = "from.infraenv"
			db.Save(infraEnv)
			Expect(db.Error).ToNot(HaveOccurred())

			cluster.Hosts = []*models.Host{
				createHost(nil),
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(ConsistOf("from.cluster"))
		})

		It("Adds sources from infra-env if hosts don't have reference to cluster", func() {
			host := createHost(nil)
			host.ClusterID = nil
			db.Save(host)
			Expect(db.Error).ToNot(HaveOccurred())

			cluster.AdditionalNtpSource = "from.cluster"
			db.Save(cluster)
			Expect(db.Error).ToNot(HaveOccurred())

			infraEnv.ClusterID = ""
			infraEnv.AdditionalNtpSources = "from.infraenv"
			db.Save(infraEnv)
			Expect(db.Error).ToNot(HaveOccurred())

			cluster.Hosts = []*models.Host{
				host,
			}

			response, err := ntpUtils.createChronyManifestContent(cluster, models.HostRoleMaster, log)
			Expect(err).ToNot(HaveOccurred())

			chronyConfServers := extractChronyConfServers(response)
			Expect(chronyConfServers).To(ConsistOf("from.infraenv"))
		})
	})

	Context("Add NTP Manifest", func() {

		BeforeEach(func() {
			cluster.Hosts = []*models.Host{
				createHost([]*models.NtpSource{
					common.TestNTPSourceSynced,
					common.TestNTPSourceUnsynced,
				}),
				createHost([]*models.NtpSource{{
					SourceName:  "3.3.3.3",
					SourceState: models.SourceStateSynced,
				}}),
			}

			manifestsApi.EXPECT().V2CreateClusterManifest(gomock.Any(), gomock.Any()).Times(0)
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
			Expect(ntpUtils.AddChronyManifest(ctx, log, cluster)).ShouldNot(HaveOccurred())

		})

		It("CreateClusterManifest failure", func() {
			fileName := "50-masters-chrony-configuration.yaml"
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(nil, errors.Errorf("Failed to create manifest %s", fileName)).Times(1)
			Expect(ntpUtils.AddChronyManifest(ctx, log, cluster)).Should(HaveOccurred())
		})
	})
})

var _ = Describe("dnsmasq manifest", func() {

	var (
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

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
				Config{ServiceBaseURL: stageServiceBaseURL, EnableSingleNodeDnsmasq: false}, db)
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
				Config{ServiceBaseURL: stageServiceBaseURL, EnableSingleNodeDnsmasq: true}, db)
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
				manifestsGeneratorApi = NewManifestsGenerator(mockManifestsApi, Config{ServiceBaseURL: test.serviceBaseURL}, db)
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
		manifestsGeneratorApi = NewManifestsGenerator(manifestsApi, Config{}, db)
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
		manifestsGeneratorApi = NewManifestsGenerator(mockManifestsApi, Config{}, db)
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

var _ = Describe("nic reaaply manifest", func() {
	var (
		ctx                    = context.Background()
		log                    *logrus.Logger
		ctrl                   *gomock.Controller
		manifestsApi           *manifestsapi.MockManifestsAPI
		manifestsGeneratorApi  ManifestsGeneratorAPI
		db                     *gorm.DB
		dbName                 string
		clusterId              strfmt.UUID
		cluster                common.Cluster
		hostWithiSCSI          models.Host
		hostWithMultipathiSCSI models.Host
		hostWithSSD            models.Host
	)

	BeforeEach(func() {
		log = logrus.New()
		ctrl = gomock.NewController(GinkgoT())
		manifestsApi = manifestsapi.NewMockManifestsAPI(ctrl)
		manifestsGeneratorApi = NewManifestsGenerator(manifestsApi, Config{}, db)
		db, dbName = common.PrepareTestDB()
		clusterId = strfmt.UUID(uuid.New().String())

		cluster = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterId,
			},
		}
		diskInventoryTemplate := `{
						"disks":[
							{
								"id": "install-id",
								"drive_type": "%s"
							}
						]
					}`
		hostWithiSCSI = models.Host{
			Inventory:          fmt.Sprintf(diskInventoryTemplate, models.DriveTypeISCSI),
			InstallationDiskID: "install-id",
		}
		hostWithSSD = models.Host{
			Inventory:          fmt.Sprintf(diskInventoryTemplate, models.DriveTypeSSD),
			InstallationDiskID: "install-id",
		}

		Expect(db.Create(&cluster).Error).NotTo(HaveOccurred())
		manifestsApi.EXPECT().V2CreateClusterManifest(gomock.Any(), gomock.Any()).Times(0)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("CreateClusterManifest", func() {
		It("added when one of the host installs on an iSCSI drive", func() {
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Times(2).Return(&models.Manifest{
				FileName: "manifest.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil)
			cluster.Cluster.Hosts = []*models.Host{&hostWithiSCSI, &hostWithSSD}
			Expect(manifestsGeneratorApi.AddNicReapply(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})
		It("added when one of the host installs on an Multipath iSCSI drive", func() {
			inventory := fmt.Sprintf(`{
			"disks":[
				{
					"id": "install-id",
					"drive_type": "%s",
					"name": "dm-0"
				},
				{
					"id": "other-id",
					"drive_type": "%s",
					"holders": "dm-0"
				}
			]
		}`, models.DriveTypeMultipath, models.DriveTypeISCSI)

			hostWithMultipathiSCSI = models.Host{
				Inventory:          inventory,
				InstallationDiskID: "install-id",
			}
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Times(2).Return(&models.Manifest{
				FileName: "manifest.yaml",
				Folder:   models.ManifestFolderOpenshift,
			}, nil)
			cluster.Cluster.Hosts = []*models.Host{&hostWithMultipathiSCSI, &hostWithSSD}
			Expect(manifestsGeneratorApi.AddNicReapply(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})
		It("not added when one of the host installs on an Multipath FC drive", func() {
			inventory := fmt.Sprintf(`{
			"disks":[
				{
					"id": "install-id",
					"drive_type": "%s",
					"name": "dm-0"
				},
				{
					"id": "other-id",
					"drive_type": "%s",
					"holders": "dm-0"
				}
			]
		}`, models.DriveTypeMultipath, models.DriveTypeFC)

			hostWithMultipathFC := models.Host{
				Inventory:          inventory,
				InstallationDiskID: "install-id",
			}
			cluster.Cluster.Hosts = []*models.Host{&hostWithMultipathFC, &hostWithSSD}
			Expect(manifestsGeneratorApi.AddNicReapply(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})
		It("not added when no hosts installs on an iSCSI/ Multipath iSCSI drive", func() {
			cluster.Cluster.Hosts = []*models.Host{&hostWithSSD, &hostWithSSD}
			Expect(manifestsGeneratorApi.AddNicReapply(ctx, log, &cluster)).ShouldNot(HaveOccurred())
		})
		It("failure", func() {
			manifestsApi.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).Return(nil, errors.Errorf("Failed to create manifest")).Times(1)
			cluster.Cluster.Hosts = []*models.Host{&hostWithiSCSI}
			err := manifestsGeneratorApi.AddNicReapply(ctx, log, &cluster)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(Equal("Failed to create manifest 50-masters-iscsi-nic-reapply.yaml: Failed to create manifest"))
		})
	})
})
