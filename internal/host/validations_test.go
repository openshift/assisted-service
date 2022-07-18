package host

import (
	"context"
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
	"gorm.io/gorm"
)

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
		m = NewManager(common.GetTestLog(), db, mockEvents, mockHwValidator, nil, createValidatorCfg(), mockMetric, defaultConfig, nil, mockOperators, pr)

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
		It("day2 host with invalid API Connectivity - fails validation", func() {
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
			Expect(validationMessage).To(Equal("Ignition is not downloadable. Please ensure host connectivity to the cluster's API"))
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
				apiConnectivityTypeManagedNetworkingJustKeepalived:
				host.APIVipConnectivity = hostutil.GenerateTestAPIConnectivityResponseSuccessString(
					string(getIgnitionConfigBytes(connectivityType)))
			default:
				Fail("This should not happen")
			}
		}

		type day2TestInfo struct {
			imported            bool
			apiConnectivityType APIConnectivityType
		}
		for _, domainType := range []struct {
			validationID validationID
			destination  string
			domainName   string
		}{
			{IsAPIDomainNameResolvedCorrectly, "API", "api.test-cluster.example.com"},
			{IsAPIInternalDomainNameResolvedCorrectly, "internal API", "api-int.test-cluster.example.com"},
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
})
