package host

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	ignition_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gomega_format "github.com/onsi/gomega/format"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/vincent-petithory/dataurl"
	"gorm.io/gorm"
)

//go:embed test_hypershift_kubeconfig
var test_hypershift_kubeconfig []byte

//go:embed test_regular_kubeconfig
var test_regular_kubeconfig []byte

var _ = Describe("Validations test", func() {

	gomega_format.CharactersAroundMismatchToInclude = 80

	var (
		ctrl            *gomock.Controller
		ctx             = context.Background()
		db              *gorm.DB
		dbName          string
		mockEvents      *eventsapi.MockHandler
		mockHwValidator *hardware.MockValidator
		mockMetric      *metrics.MockAPI
		mockOperators   *operators.MockAPI
		m               *Manager

		clusterID, hostID, infraEnvID strfmt.UUID
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHwValidator = hardware.NewMockValidator(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		mockOperators = operators.NewMockAPI(ctrl)
		pr := registry.NewMockProviderRegistry(ctrl)
		pr.EXPECT().IsHostSupported(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil, mockOperators, pr, false, nil)

		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	mockAndRefreshStatusWithoutEvents := func(h *models.Host) {
		mockDefaultClusterHostRequirements(mockHwValidator)
		mockHwValidator.EXPECT().IsValidStorageDeviceType(gomock.Any()).Return(true).AnyTimes()
		mockHwValidator.EXPECT().ListEligibleDisks(gomock.Any()).Return([]*models.Disk{}).AnyTimes()
		mockHwValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/sda").AnyTimes()
		mockOperators.EXPECT().ValidateHost(gomock.Any(), gomock.Any(), gomock.Any()).Return([]api.ValidationResult{
			{Status: api.Success, ValidationId: string(models.HostValidationIDOdfRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLsoRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDCnvRequirementsSatisfied)},
			{Status: api.Success, ValidationId: string(models.HostValidationIDLvmRequirementsSatisfied)},
		}, nil).AnyTimes()

		err := m.RefreshStatus(ctx, h, db)
		Expect(err).ToNot(HaveOccurred())
	}

	mockAndRefreshStatus := func(h *models.Host) {
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
			eventstest.WithHostIdMatcher(h.ID.String()),
			eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
			eventstest.WithClusterIdMatcher(h.ClusterID.String()),
		))
		mockAndRefreshStatusWithoutEvents(h)
	}

	getDay2Host := func() *models.Host {
		h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleWorker)
		h.Kind = swag.String(models.HostKindAddToExistingClusterHost)
		return &h
	}

	generateDay2Cluster := func() *common.Cluster {
		c := hostutil.GenerateTestCluster(clusterID)
		c.Kind = swag.String(models.ClusterKindAddHostsCluster)
		c.DiskEncryption = &models.DiskEncryption{}
		c.DiskEncryption = &models.DiskEncryption{}
		return &c
	}

	createDay2Cluster := func() {
		Expect(db.Create(generateDay2Cluster()).Error).ToNot(HaveOccurred())
	}

	generateDay2ImportedCluster := func() *common.Cluster {
		c := generateDay2Cluster()
		c.Imported = swag.Bool(true)

		// Imported clusters always have their UserManagedNetworking set to false because it's unknown
		c.UserManagedNetworking = swag.Bool(false)
		return c
	}

	getValidationResult := func(validationsInfo string, validation validationID) (ValidationStatus, string, bool) {

		var validationsRes ValidationsStatus
		err := json.Unmarshal([]byte(validationsInfo), &validationsRes)
		Expect(err).ToNot(HaveOccurred())

		for _, vl := range validationsRes {
			for _, v := range vl {
				if v.ID == validation {
					return v.Status, v.Message, true
				}
			}
		}
		return "", "", false
	}

	getIgnitionConfig := func() *ignition_types.Config {
		return &ignition_types.Config{
			Ignition: ignition_types.Ignition{Version: "3.2.0"},
			Storage: ignition_types.Storage{
				Luks: []ignition_types.Luks{
					{
						Device: swag.String("/dev/disk"),
					},
				},
			},
		}
	}

	addEmptyIgnitionFile := func(config *ignition_types.Config, path string) {
		config.Storage.Files = append(config.Storage.Files, ignition_types.File{
			Node: ignition_types.Node{
				Path: path,
			},
		})
	}

	addIgnitionFile := func(config *ignition_types.Config, path string, contents []byte) {
		dataURL := dataurl.EncodeBytes(contents)
		config.Storage.Files = append(config.Storage.Files, ignition_types.File{
			Node: ignition_types.Node{
				Path: path,
			},
			FileEmbedded1: ignition_types.FileEmbedded1{
				Contents: ignition_types.Resource{
					Source: &dataURL,
				},
			},
		})
	}

	getIgnitionConfigManagedNetworking := func() *ignition_types.Config {
		config := getIgnitionConfig()
		addEmptyIgnitionFile(config, "/etc/kubernetes/manifests/keepalived.yaml")
		addEmptyIgnitionFile(config, "/etc/kubernetes/manifests/coredns.yaml")
		return config
	}

	getIgnitionConfigManagedNetworkingKeepalived := func() *ignition_types.Config {
		config := getIgnitionConfig()
		addEmptyIgnitionFile(config, "/etc/kubernetes/manifests/keepalived.yaml")
		return config
	}

	getIgnitionConfigManagedNetworkingCoreDNS := func() *ignition_types.Config {
		config := getIgnitionConfig()
		addEmptyIgnitionFile(config, "/etc/kubernetes/manifests/coredns.yaml")
		return config
	}

	getIgnitionConfigManagedNetworkingKubeletKubeconfigServerIsIP := func() *ignition_types.Config {
		config := getIgnitionConfig()
		addIgnitionFile(config, "/etc/kubernetes/kubeconfig", test_hypershift_kubeconfig)
		return config
	}

	getIgnitionConfigManagedNetworkingKubeletKubeconfigServerIsDomain := func() *ignition_types.Config {
		config := getIgnitionConfig()
		addIgnitionFile(config, "/etc/kubernetes/kubeconfig", test_regular_kubeconfig)
		return config
	}

	getIgnitionConfigUserManagedNetworking := func() *ignition_types.Config {
		// A config without coredns / keepalived files is detected as "user managed networking"
		return getIgnitionConfig()
	}

	getIgnitionConfigTPM2Disabled := func() *ignition_types.Config {
		config := getIgnitionConfig()
		enabled := false
		config.Storage.Luks[0].Clevis = &ignition_types.Clevis{Tpm2: &enabled}
		return config
	}

	getIgnitionConfigTPM2Enabled := func() *ignition_types.Config {
		config := getIgnitionConfig()
		enabled := true
		config.Storage.Luks[0].Clevis = &ignition_types.Clevis{Tpm2: &enabled}
		return config
	}

	setHostDomainResolutions := func(host *models.Host, domainNameResolutions *models.DomainResolutionResponse) {
		bytes, err := json.Marshal(domainNameResolutions)
		Expect(err).ShouldNot(HaveOccurred())
		host.DomainNameResolutions = string(bytes)
	}

	setHostCorruptDomainResolutions := func(host *models.Host) {
		host.DomainNameResolutions = string("{")
	}

	Context("Ignition downloadable validation", func() {
		ignitionDownloadableID := validationID(models.HostValidationIDIgnitionDownloadable)
		It("day 1 host with infraenv - successful validation", func() {
			c := hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusDiscovering)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeFalse())
		})
		It("day 1 host with infraenv - successful validation", func() {
			c := hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			h := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusDiscovering)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeFalse())
		})
		It("day2 host with no API Connectivity - pending validation", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.APIVipConnectivity = ""
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationPending))
			Expect(validationMessage).To(Equal("Ignition is not yet available, pending API connectivity"))
		})
		It("day2 host with valid API Connectivity - successful validation", func() {
			createDay2Cluster()

			h := getDay2Host()
			configBytes, err := json.Marshal(getIgnitionConfigTPM2Enabled())
			Expect(err).ShouldNot(HaveOccurred())
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationSuccess))
			Expect(validationMessage).To(Equal("Ignition is downloadable"))
		})
		It("day2 host with corrupt API Connectivity - error validation", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.APIVipConnectivity = string("{")
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationError), validationMessage)
			Expect(validationMessage).To(Equal("Internal error - failed to parse agent API connectivity response"))
		})
		It("day2 host with non-successfull API Connectivity - fails validation - old agent", func() {
			createDay2Cluster()

			h := getDay2Host()
			apivip, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess: false,
				Ignition:  "ignition",
			})
			Expect(err).ShouldNot(HaveOccurred())
			h.APIVipConnectivity = string(apivip)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("This host has failed to download the ignition file from the cluster, please ensure the host can reach the cluster"))
		})
		It("day2 host with invalid API Connectivity - fails validation", func() {
			createDay2Cluster()

			h := getDay2Host()
			apivip, err := json.Marshal(models.APIVipConnectivityResponse{
				IsSuccess:     false,
				Ignition:      "ignition",
				URL:           "http://cluster.example.com:22624/config/worker",
				DownloadError: "connection refused",
			})
			Expect(err).ShouldNot(HaveOccurred())
			h.APIVipConnectivity = string(apivip)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, ignitionDownloadableID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("This host has failed to download the ignition file from http://cluster.example.com:22624/config/worker with the following error: connection refused. Please ensure the host can reach this URL"))
		})
	})

	Context("DNS validation", func() {
		type APIConnectivityType string

		var apiConnectivityTypeEmpty APIConnectivityType = "empty"
		var apiConnectivityTypeBroken APIConnectivityType = "broken"
		var apiConnectivityTypeManagedNetworking APIConnectivityType = "managed"
		var apiConnectivityTypeManagedNetworkingJustCoreDNS APIConnectivityType = "managed-coredns"
		var apiConnectivityTypeManagedNetworkingJustKeepalived APIConnectivityType = "managed-keepalived"
		var apiConnectivityTypeUserManagedNetworking APIConnectivityType = "user-managed"
		var apiConnectivityTypeKubeletKubeconfigServerIsIP APIConnectivityType = "kubelet-kubeconfig-server-is-ip"
		var apiConnectivityTypeKubeletKubeconfigServerIsDomain APIConnectivityType = "kubelet-kubeconfig-server-is-domain"

		getIgnitionConfigBytes := func(connectivityType APIConnectivityType) []byte {
			config := (*ignition_types.Config)(nil)

			switch connectivityType {
			case apiConnectivityTypeEmpty:
				return []byte{}
			case apiConnectivityTypeBroken:
				return []byte("{")
			case apiConnectivityTypeManagedNetworking:
				config = getIgnitionConfigManagedNetworking()
			case apiConnectivityTypeUserManagedNetworking:
				config = getIgnitionConfigUserManagedNetworking()
			case apiConnectivityTypeManagedNetworkingJustCoreDNS:
				config = getIgnitionConfigManagedNetworkingCoreDNS()
			case apiConnectivityTypeManagedNetworkingJustKeepalived:
				config = getIgnitionConfigManagedNetworkingKeepalived()
			case apiConnectivityTypeKubeletKubeconfigServerIsIP:
				config = getIgnitionConfigManagedNetworkingKubeletKubeconfigServerIsIP()
			case apiConnectivityTypeKubeletKubeconfigServerIsDomain:
				config = getIgnitionConfigManagedNetworkingKubeletKubeconfigServerIsDomain()
			default:
				Fail("This should not happen")
			}

			configBytes, err := json.Marshal(config)
			Expect(err).ShouldNot(HaveOccurred())

			return configBytes
		}

		setHostConnectivity := func(host *models.Host, connectivityType APIConnectivityType) {
			switch connectivityType {
			case apiConnectivityTypeEmpty, apiConnectivityTypeBroken:
				host.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseFailureString(
					string(getIgnitionConfigBytes(connectivityType)))
			case apiConnectivityTypeManagedNetworking,
				apiConnectivityTypeUserManagedNetworking,
				apiConnectivityTypeManagedNetworkingJustCoreDNS,
				apiConnectivityTypeManagedNetworkingJustKeepalived,
				apiConnectivityTypeKubeletKubeconfigServerIsIP,
				apiConnectivityTypeKubeletKubeconfigServerIsDomain:
				host.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(
					string(getIgnitionConfigBytes(connectivityType)))
			default:
				Fail("This should not happen")
			}
		}

		Describe("Wildcard connectivity check is performed", func() {
			successMessage := "DNS wildcard check was successful"
			successMessageDay2 := "DNS wildcard check is not required for day2"
			failureMessage := "DNS wildcard configuration was detected for domain *.test-cluster.example.com The installation will not be able to complete while the entry exists. Please remove it to proceed."
			errorMessage := "Error while parsing DNS resolution response"
			pendingMessage := "DNS wildcard check cannot be performed yet because the host has not yet performed DNS resolution"

			// All the possible states for the DNS response
			type DNSResponseState string
			var DNSResponseStateNoResponse DNSResponseState = "no-response"
			var DNSResponseStateCorruptResponse DNSResponseState = "corrupt-response"
			var DNSResponseStateDidntResolveIllegalWildcard DNSResponseState = "didnt-resolve-illegal-wildcard"
			var DNSResponseStateResolvedIllegalWildcard DNSResponseState = "resolved-illegal-wildcard"

			for _, dnsWildcardTestCase := range []struct {
				// Inputs
				testCaseName string
				isDay2       bool
				dnsResponse  DNSResponseState

				// Expectations
				expectedValidationStatus ValidationStatus
				expectedMessage          string
			}{
				{
					testCaseName: "day 1 cluster - wildcard not resolved",
					isDay2:       false,

					dnsResponse: DNSResponseStateDidntResolveIllegalWildcard,

					expectedValidationStatus: ValidationSuccess,
					expectedMessage:          successMessage,
				},
				{
					testCaseName: "day 1 cluster - wildcard resolved",
					isDay2:       false,

					dnsResponse: DNSResponseStateResolvedIllegalWildcard,

					expectedValidationStatus: ValidationFailure,
					expectedMessage:          failureMessage,
				},
				{
					testCaseName: "day 1 cluster - no resolutions",
					isDay2:       false,

					dnsResponse: DNSResponseStateNoResponse,

					expectedValidationStatus: ValidationPending,
					expectedMessage:          pendingMessage,
				},
				{
					testCaseName: "day 1 cluster - corrupt resolutions",
					isDay2:       false,

					dnsResponse: DNSResponseStateCorruptResponse,

					expectedValidationStatus: ValidationError,
					expectedMessage:          errorMessage,
				},
				{
					testCaseName: "day 2 cluster - no resolutions",
					isDay2:       true,

					dnsResponse: DNSResponseStateNoResponse,

					expectedValidationStatus: ValidationSuccess,
					expectedMessage:          successMessageDay2,
				},
				{
					testCaseName: "day 2 cluster - wildcard not resolved",
					isDay2:       true,

					dnsResponse: DNSResponseStateDidntResolveIllegalWildcard,

					expectedValidationStatus: ValidationSuccess,
					expectedMessage:          successMessageDay2,
				},
				{
					testCaseName: "day 2 cluster - wildcard resolved",
					isDay2:       true,

					dnsResponse: DNSResponseStateResolvedIllegalWildcard,

					expectedValidationStatus: ValidationSuccess,
					expectedMessage:          successMessageDay2,
				},
				{
					testCaseName: "day 1 cluster - corrupt resolutions",
					isDay2:       true,

					dnsResponse: DNSResponseStateCorruptResponse,

					expectedValidationStatus: ValidationSuccess,
					expectedMessage:          successMessageDay2,
				},
			} {
				dnsWildcardTestCase := dnsWildcardTestCase

				createTestCluster := func() {
					var testCluster *common.Cluster
					if dnsWildcardTestCase.isDay2 {
						testCluster = generateDay2Cluster()
					} else {
						day1Cluster := hostutil.GenerateTestCluster(clusterID)
						testCluster = &day1Cluster
					}
					Expect(db.Create(&testCluster).Error).ShouldNot(HaveOccurred())
				}

				createTestHost := func() *models.Host {
					var testHost *models.Host
					if !dnsWildcardTestCase.isDay2 {
						day1Host := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusDiscovering)
						testHost = &day1Host
					} else {
						testHost = getDay2Host()
					}

					// Apply the host DNS resolutions depending on test input
					switch dnsWildcardTestCase.dnsResponse {
					case DNSResponseStateDidntResolveIllegalWildcard:
						setHostDomainResolutions(testHost, common.TestDomainNameResolutionsSuccess)
					case DNSResponseStateResolvedIllegalWildcard:
						setHostDomainResolutions(testHost, common.TestDomainNameResolutionsWildcardResolved)
					case DNSResponseStateCorruptResponse:
						setHostCorruptDomainResolutions(testHost)
					case DNSResponseStateNoResponse:
						testHost.DomainNameResolutions = ""
					}

					Expect(db.Create(testHost).Error).ShouldNot(HaveOccurred())

					return testHost
				}

				It(dnsWildcardTestCase.testCaseName, func() {
					createTestCluster()

					testHost := createTestHost()

					// Process validations
					mockAndRefreshStatus(testHost)

					// Get processed host from database
					hostFromDatabase := hostutil.GetHostFromDB(*testHost.ID, testHost.InfraEnvID, db).Host

					validationStatus, validationMessage, found := getValidationResult(hostFromDatabase.ValidationsInfo, IsDNSWildcardNotConfigured)

					// Verify IsDNSWildcardNotConfigured host validation exists and has the expected status/message
					Expect(found).To(BeTrue())
					Expect(validationStatus).To(Equal(dnsWildcardTestCase.expectedValidationStatus),
						fmt.Sprintf("Validation status was not as expected, message: %s", validationMessage))
					Expect(validationMessage).To(Equal(dnsWildcardTestCase.expectedMessage))
				})
			}
		})

		type day2TestInfo struct {
			imported            bool
			apiConnectivityType APIConnectivityType
		}
		for _, domainType := range []struct {
			validationID validationID
			destination  string
			domainName   string
		}{
			{IsAPIDomainNameResolvedCorrectly, "API load balancer", "api.test-cluster.example.com"},
			{IsAPIInternalDomainNameResolvedCorrectly, "internal API load balancer", "api-int.test-cluster.example.com"},
			{IsAppsDomainNameResolvedCorrectly, "application ingress", "*.apps.test-cluster.example.com"},
		} {
			successMessage := fmt.Sprintf("Domain name resolution for the %s domain was successful or not required", domainType.domainName)
			failureMessage := fmt.Sprintf(
				"Couldn't resolve domain name %s on the host. To continue installation, create the necessary DNS entries to resolve this domain name to your cluster's %s IP address",
				domainType.domainName,
				domainType.destination,
			)
			pendingMessage := fmt.Sprintf("DNS validation for the %s domain cannot be completed at the moment. This could be due to other validations", domainType.domainName)
			errorBaseDomainMessage := fmt.Sprintf("DNS validation for the %s domain cannot be completed because the cluster does not have base_dns_domain set. Please update the cluster with the correct base_dns_domain", domainType.destination)
			for _, dnsTestCase := range []struct {
				testCaseName string
				// nil signifies day-1 cluster
				testClusterDay2Parameters       *day2TestInfo
				testClusterHasManagedNetworking bool
				testHostResolvedDNS             bool
				// whether the scenario requires the cluster to have a base
				// domain set. This should be set to true for scenarios that
				// actually try to validate DNS and don't give a fake "success"
				// result just because DNS is unnecessary
				requiresBaseDomain       bool
				expectedValidationStatus ValidationStatus
				expectedMessage          string
			}{
				{
					testCaseName:                    "day 1 cluster - managed networking - valid DNS",
					testClusterDay2Parameters:       nil,
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName:                    "day 1 cluster - managed networking - invalid DNS",
					testClusterDay2Parameters:       nil,
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName:                    "day 1 cluster - user managed networking - valid DNS",
					testClusterDay2Parameters:       nil,
					testClusterHasManagedNetworking: false,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName:                    "day 1 cluster - user managed networking - invalid DNS",
					testClusterDay2Parameters:       nil,
					testClusterHasManagedNetworking: false,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationFailure,
					expectedMessage:                 failureMessage,
				},
				{
					testCaseName: "day 2 cluster - not imported - user managed networking - invalid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            false,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: false,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationFailure,
					expectedMessage:                 failureMessage,
				},
				{
					testCaseName: "day 2 cluster - not imported - user managed networking - valid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            false,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: false,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - not imported - managed networking - invalid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            false,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - not imported - managed networking - valid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            false,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationPending,
					expectedMessage:                 pendingMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - valid DNS - no connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeEmpty,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationPending,
					expectedMessage:                 pendingMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - unmanaged connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeUserManagedNetworking,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationFailure,
					expectedMessage:                 failureMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - valid DNS - unmanaged connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeUserManagedNetworking,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - managed connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeManagedNetworking,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - coredns managed connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeManagedNetworkingJustCoreDNS,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - keepalived managed connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeManagedNetworkingJustKeepalived,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - kubelet kubeconfig server is IP address managed connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeKubeletKubeconfigServerIsIP,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - invalid DNS - kubelet kubeconfig server is domain unmanaged connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeKubeletKubeconfigServerIsDomain,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             false,
					requiresBaseDomain:              true,
					expectedValidationStatus:        ValidationFailure,
					expectedMessage:                 failureMessage,
				},
				{
					testCaseName: "day 2 cluster - imported - valid DNS - managed connectivity",
					testClusterDay2Parameters: &day2TestInfo{
						imported:            true,
						apiConnectivityType: apiConnectivityTypeManagedNetworking,
					},
					testClusterHasManagedNetworking: true,
					testHostResolvedDNS:             true,
					requiresBaseDomain:              false,
					expectedValidationStatus:        ValidationSuccess,
					expectedMessage:                 successMessage,
				},
			} {
				if dnsTestCase.testClusterDay2Parameters != nil && dnsTestCase.testClusterDay2Parameters.imported && !dnsTestCase.testClusterHasManagedNetworking {
					Fail("Imported clusters always have user managed networking set to false, impossible test case")
				}

				for _, withBaseDomain := range []bool{true, false} {
					withBaseDomainTestName := "with base domain"
					if !withBaseDomain {
						withBaseDomainTestName = "without base domain"
					}

					if !withBaseDomain {
						if dnsTestCase.requiresBaseDomain {
							dnsTestCase.expectedValidationStatus = ValidationError
							dnsTestCase.expectedMessage = errorBaseDomainMessage
						} else {
							if dnsTestCase.expectedValidationStatus == ValidationSuccess {
								dnsTestCase.expectedMessage = fmt.Sprintf(
									"Domain name resolution for the %s domain was successful or not required", domainType.destination)
							}
						}
					}

					if !withBaseDomain && dnsTestCase.expectedValidationStatus == ValidationPending {
						dnsTestCase.expectedMessage = fmt.Sprintf(
							"DNS validation for the %s domain cannot be completed at the moment. This could be due to other validations",
							domainType.destination)
					}

					domainType := domainType
					dnsTestCase := dnsTestCase
					withBaseDomain := withBaseDomain
					It(fmt.Sprintf("%s - %s - %s", domainType.destination, dnsTestCase.testCaseName, withBaseDomainTestName), func() {
						resolutions := common.TestDomainNameResolutionsSuccess
						if !dnsTestCase.testHostResolvedDNS {
							resolutions = common.TestDomainResolutionsAllEmpty
						}

						// Create test cluster
						var cluster *common.Cluster
						if dnsTestCase.testClusterDay2Parameters == nil {
							day1Cluster := hostutil.GenerateTestCluster(clusterID)
							cluster = &day1Cluster
						} else {
							if !dnsTestCase.testClusterDay2Parameters.imported {
								cluster = generateDay2Cluster()
							} else {
								cluster = generateDay2ImportedCluster()
							}
						}
						cluster.UserManagedNetworking = swag.Bool(!dnsTestCase.testClusterHasManagedNetworking)

						if !withBaseDomain {
							cluster.BaseDNSDomain = ""
						}

						Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

						// Create test host
						var host *models.Host
						if dnsTestCase.testClusterDay2Parameters == nil {
							day1Host := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusDiscovering)
							host = &day1Host
						} else {
							host = getDay2Host()
							setHostConnectivity(host, dnsTestCase.testClusterDay2Parameters.apiConnectivityType)
						}
						setHostDomainResolutions(host, resolutions)
						Expect(db.Create(host).Error).ShouldNot(HaveOccurred())

						// Process validations
						mockAndRefreshStatus(host)

						// Get processed host from database
						hostFromDatabase := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host

						// Verify host validations
						validationStatus, validationMessage, found := getValidationResult(hostFromDatabase.ValidationsInfo, domainType.validationID)
						Expect(found).To(BeTrue())
						Expect(validationStatus).To(Equal(dnsTestCase.expectedValidationStatus),
							fmt.Sprintf("Validation status was not as expected, message: %s", validationMessage))
						Expect(validationMessage).To(Equal(dnsTestCase.expectedMessage))
					})
				}
			}
		}
	})

	Context("Disk encryption validation", func() {
		diskEncryptionID := validationID(models.HostValidationIDDiskEncryptionRequirementsSatisfied)
		It("disk-encryption not set", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeFalse())
		})

		It("un-affected roles", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleWorker)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeFalse())
		})

		It("auto-assigned role", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleAutoAssign)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())
			mockEvents.EXPECT().NotifyInternalEvent(ctx, h.ClusterID, h.ID, &h.InfraEnvID, gomock.Any())

			checkValidation := func(expectedStatus ValidationStatus, expectedMsg string) {
				h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
				validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, validationID(models.HostValidationIDDiskEncryptionRequirementsSatisfied))
				ExpectWithOffset(1, found).To(BeTrue())
				ExpectWithOffset(1, validationStatus).To(Equal(expectedStatus))
				ExpectWithOffset(1, validationMessage).To(Equal(expectedMsg))
			}

			By("effective role is auto-assign (no suggestion yet)")
			mockAndRefreshStatus(&h)
			checkValidation(ValidationPending, "Missing role assignment")

			By("auto-assign node is effectively assigned to master")
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostRoleUpdatedEventName),
				eventstest.WithHostIdMatcher(h.ID.String()),
				eventstest.WithInfraEnvIdMatcher(h.InfraEnvID.String()),
			))
			err := m.RefreshRole(ctx, &h, db)
			Expect(err).ToNot(HaveOccurred())

			mockAndRefreshStatusWithoutEvents(&h)
			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			checkValidation(ValidationSuccess, "Installation disk can be encrypted using tpmv2")
		})

		It("missing inventory", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			h.Inventory = ""
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatusWithoutEvents(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationPending))
			Expect(validationMessage).To(Equal("Missing host inventory"))
		})

		It("tang mode - validation success", func() {
			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			h.TangConnectivity = hostutil.GenerateTestTangConnectivity(true)
			h.Inventory = common.GenerateTestInventory()
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationSuccess))
			Expect(validationMessage).To(Equal(fmt.Sprintf("Installation disk can be encrypted using %s", models.DiskEncryptionModeTang)))
		})

		It("tang mode - validation failure", func() {
			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn:    swag.String(models.DiskEncryptionEnableOnMasters),
				Mode:        swag.String(models.DiskEncryptionModeTang),
				TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":"PLjNyRdGw03zlRoGjQYMahSZGu9"}]`,
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			h.TangConnectivity = hostutil.GenerateTestTangConnectivity(false)
			h.Inventory = common.GenerateTestInventory()
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).Should(ContainSubstring("Could not validate that all Tang servers are reachable and working"))
		})

		It("TPM is disabled in host's BIOS", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnWorkers),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleWorker)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("TPM version could not be found, make sure TPM is enabled in host's BIOS"))
		})

		It("host's TPM version is unsupported", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr12)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal(fmt.Sprintf("The host's TPM version is not supported, expected-version: %s, actual-version: %s",
				models.InventoryTpmVersionNr20, models.InventoryTpmVersionNr12)))
		})

		It("happy flow - explicit role", func() {

			c := hostutil.GenerateTestCluster(clusterID)
			c.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}
			Expect(db.Create(&c).Error).ToNot(HaveOccurred())

			h := hostutil.GenerateTestHostByKind(hostID, infraEnvID, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleWorker)
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(&h)

			h = hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationSuccess))
			Expect(validationMessage).To(Equal(fmt.Sprintf("Installation disk can be encrypted using %s", models.DiskEncryptionModeTpmv2)))
		})

		It("day2 host - TPM2 for worker enabled on cluster, TPM2 available on host", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			configBytes, err := json.Marshal(getIgnitionConfigTPM2Enabled())
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationSuccess))
			Expect(validationMessage).To(Equal(fmt.Sprintf("Installation disk can be encrypted using %s", models.DiskEncryptionModeTpmv2)))
		})

		It("day2 host - only Tang is enabled for worker", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			config := getIgnitionConfigTPM2Disabled()
			config.Storage.Luks[0].Clevis = &ignition_types.Clevis{
				Tang: []ignition_types.Tang{{URL: "http://test", Thumbprint: swag.String("test")}}}
			configBytes, err := json.Marshal(config)
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			h.TangConnectivity = hostutil.GenerateTestTangConnectivity(true)
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
		})

		It("day2 host - TPM2 and Tang are not available in ignition LUKS", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNr20)
			configBytes, err := json.Marshal(getIgnitionConfigTPM2Disabled())
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("Invalid LUKS object in ignition - both TPM2 and Tang are not available"))
		})

		It("day2 host - TPM2 for worker enabled on cluster, TPM2 not available on host", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			configBytes, err := json.Marshal(getIgnitionConfigTPM2Enabled())
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))

			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("TPM version could not be found, make sure TPM is enabled in host's BIOS"))
		})

		It("day2 host - disk encryption disabled on cluster", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			configBytes, err := json.Marshal(getIgnitionConfigTPM2Enabled())
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))

			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, validationMessage, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationFailure))
			Expect(validationMessage).To(Equal("TPM version could not be found, make sure TPM is enabled in host's BIOS"))
		})

		It("day2 host - disk encryption is not available", func() {
			createDay2Cluster()

			h := getDay2Host() //explicit set the role to worker
			h.Inventory = common.GenerateTestInventoryWithTpmVersion("")
			configBytes, err := json.Marshal(ignition_types.Config{})
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeFalse())
		})

		It("day2 host - pending on APIVipConnectivity response", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			h.APIVipConnectivity = ""
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			validationStatus, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeTrue())
			Expect(validationStatus).To(Equal(ValidationPending))
		})

		It("day2 host - empty ignition in APIVipConnectivity response", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString("")
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeFalse())
		})

		It("day2 host - no LUKS in cluster ignition", func() {
			createDay2Cluster()

			h := getDay2Host()
			h.Inventory = common.GenerateTestInventoryWithTpmVersion(models.InventoryTpmVersionNone)
			ignitionConfig := ignition_types.Config{Ignition: ignition_types.Ignition{Version: "3.2.0"}}
			configBytes, err := json.Marshal(ignitionConfig)
			Expect(err).To(Not(HaveOccurred()))
			h.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(string(configBytes))
			Expect(db.Create(&h).Error).ShouldNot(HaveOccurred())

			mockAndRefreshStatus(h)

			h = &hostutil.GetHostFromDB(*h.ID, h.InfraEnvID, db).Host
			_, _, found := getValidationResult(h.ValidationsInfo, diskEncryptionID)
			Expect(found).To(BeFalse())
		})
	})

	Context("VSphere host UUID enable validation", func() {

		var (
			hostUUIDValidation = validationID(models.HostValidationIDVsphereDiskUUIDEnabled)
			host               models.Host
			cluster            common.Cluster
		)

		BeforeEach(func() {
			cluster = hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			hostId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHostByKind(hostId, infraEnvId, &clusterID, models.HostStatusKnown, models.HostKindHost, models.HostRoleMaster)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
		})

		updateHostInventory := func(updateFunc func(*models.Inventory)) {
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			inventory, err := common.UnmarshalInventory(host.Inventory)
			Expect(err).ToNot(HaveOccurred())
			updateFunc(inventory)
			inventoryByte, err := json.Marshal(inventory)
			Expect(err).ToNot(HaveOccurred())
			updates := map[string]interface{}{}
			updates["inventory"] = string(inventoryByte)
			Expect(db.Model(host).Updates(updates).Error).ShouldNot(HaveOccurred())
		}

		updateClusterPlatform := func() {
			cluster.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
			}

			updates := map[string]interface{}{}
			updates["platform_type"] = "vsphere"
			Expect(db.Model(cluster).Updates(updates).Error).ShouldNot(HaveOccurred())
		}

		It("Baremetal platform with no disk UUID", func() {
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, _, _ := getValidationResult(host.ValidationsInfo, hostUUIDValidation)
			Expect(status).To(BeEquivalentTo(ValidationSuccess))
		})

		It("Vsphere platform with disk UUID", func() {
			updateHostInventory(func(inventory *models.Inventory) {
				for _, disk := range inventory.Disks {
					disk.HasUUID = true
				}
			})

			updateClusterPlatform()
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, _, _ := getValidationResult(host.ValidationsInfo, hostUUIDValidation)
			Expect(status).To(BeEquivalentTo(ValidationSuccess))
		})

		It("Vsphere platform with no disk UUID", func() {
			updateHostInventory(func(inventory *models.Inventory) {
				for _, disk := range inventory.Disks {
					disk.HasUUID = false
				}
			})

			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			updateClusterPlatform()
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, _, _ := getValidationResult(host.ValidationsInfo, hostUUIDValidation)
			Expect(status).To(BeEquivalentTo(ValidationFailure))
		})

		It("Vsphere platform with CDROM disk with no UUID", func() {
			CDROM := &models.Disk{
				SizeBytes: conversions.GibToBytes(120),
				DriveType: "CDROM",
				HasUUID:   false,
			}

			HDD := &models.Disk{
				SizeBytes: conversions.GibToBytes(120),
				DriveType: "HDD",
				HasUUID:   true,
			}

			updateHostInventory(func(inventory *models.Inventory) {
				inventory.Disks = []*models.Disk{HDD, CDROM}
			})

			updateClusterPlatform()
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host

			mockHwValidator.EXPECT().IsValidStorageDeviceType(CDROM).Return(false).Times(1)
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, _, _ := getValidationResult(host.ValidationsInfo, hostUUIDValidation)
			Expect(status).To(BeEquivalentTo(ValidationSuccess))
		})
	})

	Context("Agent compatibility validation", func() {
		var (
			host    models.Host
			cluster common.Cluster
		)

		BeforeEach(func() {
			cluster = hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			hostId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHostByKind(hostId, infraEnvId, &clusterID, models.HostStatusKnown, models.HostKindHost, models.HostRoleMaster)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
		})

		It("Passes if the agent and the service use the same image", func() {
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, message, ok := getValidationResult(host.ValidationsInfo, CompatibleAgent)
			Expect(ok).To(BeTrue())
			Expect(status).To(Equal(ValidationSuccess))
			Expect(message).To(Equal("Host agent is compatible with the service"))
		})

		It("Fails if the agent and the service use different images", func() {
			host.DiscoveryAgentVersion = "quay.io/edge-infrastructure/assisted-installer-agent:wrong"
			mockAndRefreshStatus(&host)
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			status, message, ok := getValidationResult(host.ValidationsInfo, CompatibleAgent)
			Expect(ok).To(BeTrue())
			Expect(status).To(Equal(ValidationFailure))
			Expect(message).To(Equal(
				"This host's agent is in the process of being upgraded to a " +
					"compatible version. This might take a few minutes",
			))
		})
	})

	Context("No skip installation disk validation", func() {
		var (
			host    models.Host
			cluster common.Cluster
		)

		const (
			successMessage string = "No request to skip formatting of the installation disk"
			failureMessage string = "Requesting to skip the formatting of the installation disk is not allowed. The installation disk must be formatted. Please either change this host's installation disk or do not skip the formatting of the installation disk"
		)

		BeforeEach(func() {
			// Create a test cluster
			cluster = hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			// Create a test host
			hostId, infraEnvId := strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHostByKind(hostId, infraEnvId, &clusterID, models.HostStatusKnown, models.HostKindHost, models.HostRoleMaster)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
		})

		for _, test := range []struct {
			name                      string
			installationDisk          string
			skipFormattingDisks       string
			expectedValidationStatus  ValidationStatus
			expectedValidationMessage string
		}{
			{
				name:                      "No skip disks",
				installationDisk:          "/dev/sda",
				skipFormattingDisks:       "",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "One skip disk - not installation disk",
				installationDisk:          "/dev/sda",
				skipFormattingDisks:       "/dev/sdb",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "Multiple skip disks - none are installation disk",
				installationDisk:          "/dev/sda",
				skipFormattingDisks:       "/dev/sdb,/dev/sdc",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "One skip disk - is installation disk",
				installationDisk:          "/dev/sda",
				skipFormattingDisks:       "/dev/sda",
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "Multiple skip disks - one is installation disk",
				installationDisk:          "/dev/sda",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "No skip disk - no installation disk",
				installationDisk:          "",
				skipFormattingDisks:       "",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "One skip disk - no installation disk",
				installationDisk:          "",
				skipFormattingDisks:       "/dev/sda",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "Multiple skip disks - no installation disk",
				installationDisk:          "",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
		} {
			test := test
			It(test.name, func() {
				// Apply test inputs
				host.InstallationDiskID = test.installationDisk
				host.SkipFormattingDisks = test.skipFormattingDisks

				// Trigger validations
				mockAndRefreshStatus(&host)

				// Retrieve results
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				status, message, ok := getValidationResult(host.ValidationsInfo, NoSkipInstallationDisk)

				// Validate expectations
				Expect(ok).To(BeTrue())
				Expect(message).To(Equal(test.expectedValidationMessage))
				Expect(status).To(Equal(test.expectedValidationStatus))
			})
		}
	})
	Context("No skip missing disk validation", func() {
		var (
			host    models.Host
			cluster common.Cluster
		)

		const (
			pendingMessage string = "Host inventory not available yet"
			successMessage string = "All disks that have skipped formatting are present in the host inventory"
			failureMessage string = "One or more of the disks that you have requested to skip the formatting of are no longer present on this host. To ensure they haven't just changed their identity, please remove your request to skip their formatting and then if needed add them back using the new ID"
		)

		BeforeEach(func() {
			// Create a test cluster
			cluster = hostutil.GenerateTestCluster(clusterID)
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

			// Create a test host
			hostId, infraEnvId := strfmt.UUID(uuid.New().String()), strfmt.UUID(uuid.New().String())
			host = hostutil.GenerateTestHostByKind(hostId, infraEnvId, &clusterID, models.HostStatusDiscovering, models.HostKindHost, models.HostRoleMaster)
			host.Inventory = hostutil.GenerateMasterInventory()
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
		})

		for _, test := range []struct {
			name                      string
			skipFormattingDisks       string
			inventoryDisksIDs         []string
			noInventory               bool
			expectedValidationStatus  ValidationStatus
			expectedValidationMessage string
		}{
			{
				name:                      "No skip disks, no inventory",
				skipFormattingDisks:       "",
				noInventory:               true,
				expectedValidationStatus:  ValidationPending,
				expectedValidationMessage: pendingMessage,
			},
			{
				name:                      "One skip disk, no inventory",
				skipFormattingDisks:       "/dev/sda",
				noInventory:               true,
				expectedValidationStatus:  ValidationPending,
				expectedValidationMessage: pendingMessage,
			},
			{
				name:                      "Multiple skip disks, no inventory",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				noInventory:               true,
				expectedValidationStatus:  ValidationPending,
				expectedValidationMessage: pendingMessage,
			},
			{
				name:                      "No skip disks, one inventory disk",
				skipFormattingDisks:       "",
				inventoryDisksIDs:         []string{"/dev/sda"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "No skip disks, multiple inventory disks",
				skipFormattingDisks:       "",
				inventoryDisksIDs:         []string{"/dev/sda", "/dev/sdb"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "One skip disk, one different inventory disk",
				skipFormattingDisks:       "/dev/sda",
				inventoryDisksIDs:         []string{"/dev/sdb"},
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "One skip disk, multiple different inventory disks",
				skipFormattingDisks:       "/dev/sda",
				inventoryDisksIDs:         []string{"/dev/sdb", "/dev/sdc"},
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "One skip disk, one same inventory disk",
				skipFormattingDisks:       "/dev/sda",
				inventoryDisksIDs:         []string{"/dev/sda"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "One skip disk, multiple inventory disks, one same",
				skipFormattingDisks:       "/dev/sda",
				inventoryDisksIDs:         []string{"/dev/sdb", "/dev/sda"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "Multiple skip disks, one same inventory disk",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				inventoryDisksIDs:         []string{"/dev/sda"},
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "Multiple skip disks, multiple inventory disks, one same",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				inventoryDisksIDs:         []string{"/dev/sdb", "/dev/sdc"},
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "Multiple skip disks, multiple inventory disks, all different",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				inventoryDisksIDs:         []string{"/dev/sdc", "/dev/sdd"},
				expectedValidationStatus:  ValidationFailure,
				expectedValidationMessage: failureMessage,
			},
			{
				name:                      "Multiple skip disks, multiple inventory disks, all same",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				inventoryDisksIDs:         []string{"/dev/sda", "/dev/sdb"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
			{
				name:                      "Multiple skip disks, multiple inventory disks, all same, plus another inventory disk",
				skipFormattingDisks:       "/dev/sda,/dev/sdb",
				inventoryDisksIDs:         []string{"/dev/sda", "/dev/sdb", "/dev/sdc"},
				expectedValidationStatus:  ValidationSuccess,
				expectedValidationMessage: successMessage,
			},
		} {
			test := test
			It(test.name, func() {
				// Apply test inputs
				host.SkipFormattingDisks = test.skipFormattingDisks
				var inventory models.Inventory
				Expect(json.Unmarshal([]byte(host.Inventory), &inventory)).To(Succeed())
				inventory.Disks = []*models.Disk{}
				for _, inventoryDiskID := range test.inventoryDisksIDs {
					inventory.Disks = append(inventory.Disks, &models.Disk{
						ID: inventoryDiskID,
					})
				}
				inventoryBytes, err := json.Marshal(inventory)
				Expect(err).ShouldNot(HaveOccurred())
				if test.noInventory {
					inventoryBytes = []byte("")
				}

				host.Inventory = string(inventoryBytes)

				// Trigger validations
				if test.noInventory {
					mockAndRefreshStatusWithoutEvents(&host)
				} else {
					mockAndRefreshStatus(&host)
				}

				// Retrieve results
				host = hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
				status, message, ok := getValidationResult(host.ValidationsInfo, NoSkipMissingDisk)

				// Validate expectations
				Expect(ok).To(BeTrue())
				Expect(message).To(Equal(test.expectedValidationMessage))
				Expect(status).To(Equal(test.expectedValidationStatus))
			})
		}
	})
})
