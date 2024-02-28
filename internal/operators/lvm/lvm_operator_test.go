package lvm_test

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/common/testing"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/api"
	. "github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"gorm.io/gorm"
)

var _ = Describe("Lvm Operator", func() {
	var (
		ctx                           = context.Background() //TODO()
		db                            *gorm.DB
		dbName                        string
		mockCtrl                      *gomock.Controller
		mockEvents                    *eventsapi.MockHandler
		mockMetric                    *metrics.MockAPI
		hostManager                   *host.Manager
		mockOperators                 *operators.Manager
		mockVersions                  *versions.MockHandler
		mockProviderRegistry          *registry.MockProviderRegistry
		defaultConfig                 *host.Config
		hostID, clusterID, infraEnvID strfmt.UUID

		diskID1 = "/dev/disk/by-id/test-disk-1"
		diskID2 = "/dev/disk/by-id/test-disk-2"

		operator         = NewLvmOperator(common.GetTestLog(), nil)
		lvmMemMB         = conversions.MibToBytes(operator.Config.LvmMemoryPerHostMiB)
		lvmMemMB_pre4_13 = conversions.MibToBytes(operator.Config.LvmMemoryPerHostMiBBefore4_13)

		masterNode = &models.Host{
			Role:               models.HostRoleMaster,
			InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{
				Cpus:  4,
				Ram:   16384,
				Disks: []*models.Disk{},
			}),
		}
		workerNode = &models.Host{
			Role:               models.HostRoleWorker,
			InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{
				Cpus:  3,
				Ram:   8192 + operator.Config.LvmMemoryPerHostMiB,
				Disks: []*models.Disk{},
			}),
		}
		masterCompactNode = &models.Host{
			Role:               models.HostRoleMaster,
			InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{
				Cpus:  6,
				Ram:   24576,
				Disks: []*models.Disk{},
			}),
		}
		MaxHostDisconnectionTime = 3 * time.Minute

		hostWithSufficientResources = &models.Host{
			InstallationDiskID: diskID1,
			Inventory: Inventory(&InventoryResources{
				Cpus: 12,
				Ram:  32 * conversions.GiB,
				Disks: []*models.Disk{
					{
						SizeBytes: 20 * conversions.GB,
						DriveType: models.DriveTypeHDD,
						ID:        diskID1,
					},
					{
						SizeBytes: 40 * conversions.GB,
						DriveType: models.DriveTypeSSD,
						ID:        diskID2,
					},
				},
			}),
		}
	)

	Context("ValidateCluster", func() {
		fullHaMode := models.ClusterHighAvailabilityModeFull
		noneHaMode := models.ClusterHighAvailabilityModeNone

		table.DescribeTable("validate cluster when ", func(cluster *common.Cluster, expectedResult api.ValidationResult) {
			res, _ := operator.ValidateCluster(ctx, cluster)
			Expect(res).Should(Equal(expectedResult))
		},
			table.Entry("High Availability Mode Full",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &fullHaMode, Hosts: []*models.Host{hostWithSufficientResources, hostWithSufficientResources}, OpenshiftVersion: LvmMinMultiNodeSupportVersion}},
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
			table.Entry("High Availability Mode Full with pre-release version",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &fullHaMode, Hosts: []*models.Host{hostWithSufficientResources, hostWithSufficientResources}, OpenshiftVersion: "4.15.0-rc0"}},
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
			table.Entry("High Availability Mode Full with higher than LvmMinMultiNodeSupportVersion",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &fullHaMode, Hosts: []*models.Host{hostWithSufficientResources, hostWithSufficientResources}, OpenshiftVersion: "4.15.7"}},
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
			table.Entry("High Availability Mode Full and Openshift version less than LvmMinMultiNodeSupportVersion",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &fullHaMode, Hosts: []*models.Host{hostWithSufficientResources}, OpenshiftVersion: "4.14.0"}},
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Logical Volume Manager is only supported for highly available openshift with version 4.15.0 or above"}},
			),
			table.Entry("High Availability Mode None and Openshift version less than minimal",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &noneHaMode, Hosts: []*models.Host{hostWithSufficientResources}, OpenshiftVersion: "4.10.0"}},
				api.ValidationResult{Status: api.Failure, ValidationId: operator.GetHostValidationID(), Reasons: []string{"Logical Volume Manager is only supported for openshift versions 4.11.0 and above"}},
			),
			table.Entry("High Availability Mode None and Openshift version more than minimal",
				&common.Cluster{Cluster: models.Cluster{HighAvailabilityMode: &noneHaMode, Hosts: []*models.Host{hostWithSufficientResources}, OpenshiftVersion: LvmoMinOpenshiftVersion}},
				api.ValidationResult{Status: api.Success, ValidationId: operator.GetHostValidationID()},
			),
		)
	})
	Context("GetHostRequirements", func() {
		BeforeEach(func() {
			db, dbName = common.PrepareTestDB()
			mockCtrl = gomock.NewController(GinkgoT())
			hostID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			infraEnvID = strfmt.UUID(uuid.New().String())
			mockEvents = eventsapi.NewMockHandler(mockCtrl)
			mockMetric = metrics.NewMockAPI(mockCtrl)
			mockOperators = operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
			mockProviderRegistry = registry.NewMockProviderRegistry(mockCtrl)
			mockProviderRegistry.EXPECT().IsHostSupported(testing.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any()).Return(true, nil).AnyTimes()
			mockVersions = versions.NewMockHandler(mockCtrl)

			defaultConfig = &host.Config{
				ResetTimeout:             MaxHostDisconnectionTime,
				EnableAutoAssign:         true,
				MonitorBatchSize:         100,
				DisabledHostvalidations:  host.DisabledHostValidations{},
				MaxHostDisconnectionTime: MaxHostDisconnectionTime,
			}

			hostManager = host.NewManager(common.GetTestLog(), db, testing.GetDummyNotificationStream(mockCtrl),
				mockEvents, nil, nil, nil, mockMetric, defaultConfig, nil, mockOperators, mockProviderRegistry, false, nil, mockVersions, false)

		})
		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
			mockCtrl.Finish()

		})

		successResult := models.ClusterHostRequirementsDetails{
			CPUCores: operator.Config.LvmCPUPerHost,
			RAMMib:   operator.Config.LvmMemoryPerHostMiB,
		}
		successResultBefore4_13 := models.ClusterHostRequirementsDetails{
			CPUCores: operator.Config.LvmCPUPerHost,
			RAMMib:   operator.Config.LvmMemoryPerHostMiBBefore4_13,
		}
		successResultMasterfull := models.ClusterHostRequirementsDetails{
			CPUCores: 0,
			RAMMib:   0,
		}

		requirementTests := []struct {
			name                 string
			ocpVersion           string
			result               *models.ClusterHostRequirementsDetails
			hostRole             models.HostRole
			HighAvailabilityMode string
			clusterhosts         []*models.Host
			hostCPU              int64
			hostMem              int64
		}{

			{
				name:                 "SNO version 4.13.0",
				ocpVersion:           "4.13.0",
				result:               &successResult,
				clusterhosts:         []*models.Host{},
				hostCPU:              8 + operator.Config.LvmCPUPerHost,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeNone,
				hostRole:             models.HostRoleAutoAssign,
				hostMem:              16*conversions.GiB + operator.Config.LvmMemoryPerHostMiB,
			},
			{
				name:                 "SNO version 4.11.0",
				ocpVersion:           "4.11.0",
				result:               &successResultBefore4_13,
				clusterhosts:         []*models.Host{},
				hostCPU:              12,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeNone,
				hostRole:             models.HostRoleAutoAssign,
				hostMem:              32 * conversions.GiB,
			},
			{
				name:                 "Compact 4.15, role Master",
				ocpVersion:           "4.15.0",
				result:               &successResult,
				clusterhosts:         []*models.Host{masterNode, masterNode},
				hostCPU:              7,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeFull,
				hostRole:             models.HostRoleMaster,
				hostMem:              25 * conversions.GiB,
			},
			{
				name:                 "Compact cluster version 4.15.0, role auto-assign",
				ocpVersion:           "4.15.0",
				result:               &successResult,
				clusterhosts:         []*models.Host{masterCompactNode, masterCompactNode},
				hostCPU:              12,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeFull,
				hostRole:             models.HostRoleAutoAssign,
				hostMem:              32 * conversions.GiB,
			},
			{
				name:                 "Full cluster 4.15, Master",
				ocpVersion:           "4.15.0",
				result:               &successResultMasterfull,
				clusterhosts:         []*models.Host{masterNode, masterNode, workerNode, workerNode},
				hostCPU:              4,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeFull,
				hostRole:             models.HostRoleMaster,
				hostMem:              16 * conversions.GiB,
			},
			{
				name:                 "Full cluster 4.15, Worker",
				ocpVersion:           "4.15.0",
				result:               &successResult,
				clusterhosts:         []*models.Host{masterNode, masterNode, masterNode, workerNode},
				hostCPU:              12,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeFull,
				hostRole:             models.HostRoleWorker,
				hostMem:              32 * conversions.GiB,
			},
			{
				name:                 "Full cluster 4.15, Role Auto Assign as worker",
				ocpVersion:           "4.15.0",
				result:               &successResult,
				clusterhosts:         []*models.Host{masterNode, masterNode, masterNode, workerNode, workerNode},
				hostCPU:              3,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeFull,
				hostRole:             models.HostRoleAutoAssign,
				hostMem:              8*conversions.GiB + operator.Config.LvmMemoryPerHostMiB,
			},
			{
				name:                 "SNO 4.11.0, not supported",
				ocpVersion:           "4.10.0",
				result:               &successResultBefore4_13,
				clusterhosts:         []*models.Host{},
				hostCPU:              12,
				HighAvailabilityMode: models.ClusterHighAvailabilityModeNone,
				hostRole:             models.HostRoleMaster,
				hostMem:              32 * conversions.GiB,
			},
		}
		for i := range requirementTests {
			test := requirementTests[i]
			It(fmt.Sprintf("GetHostRequirements , %s", test.name), func() {

				infraEnv := hostutil.GenerateTestInfraEnv(infraEnvID)
				infraEnv.ClusterID = clusterID

				Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())

				testHost := hostutil.GenerateTestHost(hostID, infraEnvID, clusterID, models.HostStatusKnown)

				testHost.Role = test.hostRole
				testHost.Inventory = Inventory(&InventoryResources{
					Cpus: test.hostCPU,
					Ram:  test.hostMem,
				})

				var schedulableMasters bool = false
				if len(test.clusterhosts) <= 3 {
					schedulableMasters = true
				}
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID:                   &clusterID,
					MonitoredOperators:   []*models.MonitoredOperator{&Operator},
					OpenshiftVersion:     test.ocpVersion,
					HighAvailabilityMode: &test.HighAvailabilityMode,
					SchedulableMasters:   &schedulableMasters,
				}}
				for i := range test.clusterhosts {
					genrateHostID := strfmt.UUID(uuid.New().String())
					test.clusterhosts[i].ID = &genrateHostID
					test.clusterhosts[i].InfraEnvID = infraEnvID
					test.clusterhosts[i].ClusterID = &clusterID

					mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.HostRegistrationSucceededEventName),
						eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
						eventstest.WithClusterIdMatcher(clusterID.String()),
						eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
					err := hostManager.RegisterHost(ctx, test.clusterhosts[i], db)
					Expect(err).ShouldNot(HaveOccurred())

				}

				test.clusterhosts = append(test.clusterhosts, &testHost)
				cluster.Hosts = test.clusterhosts
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
				host := hostutil.GetHostFromDB(hostID, infraEnvID, db)
				testHost.SuggestedRole = host.SuggestedRole

				res, _ := operator.GetHostRequirements(ctx, cluster, &testHost)
				Expect(test.result).Should(Equal(res))

			})
		}
	})

	Context("ValidateHost", func() {
		var (
			HostID       = strfmt.UUID(uuid.New().String())
			templateHost *models.Host
		)
		BeforeEach(func() {

			templateHost = &models.Host{
				ID:                 &HostID,
				InstallationDiskID: diskID1,
				Inventory: Inventory(&InventoryResources{
					Cpus:  0,
					Ram:   0,
					Disks: []*models.Disk{},
				}),
			}
		})
		hostValidationTests := []struct {
			name                        string
			ocpVersion                  string
			hosts                       []*models.Host
			resultMessage               []string
			apiStatus                   api.ValidationStatus
			hostRole                    models.HostRole
			HighAvailabilityMode        string
			diskCount                   int
			hostCPU                     int64
			hostMem                     float32
			additionalDiskRequirementGB int64
		}{
			{
				name:                        "SNO with sufficient resources, 4.12",
				ocpVersion:                  "4.12.0",
				hosts:                       []*models.Host{},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     9,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeNone,
				hostRole:                    models.HostRoleAutoAssign,
				hostMem:                     16*conversions.GiB + float32(lvmMemMB_pre4_13),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "SNO with sufficient resources",
				ocpVersion:                  "4.13.0",
				hosts:                       []*models.Host{},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     9,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeNone,
				hostRole:                    models.HostRoleAutoAssign,
				hostMem:                     16*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "SNO with insufficient disks",
				ocpVersion:                  "4.13.0",
				hosts:                       []*models.Host{},
				resultMessage:               []string{"Logical Volume Manager requires at least one non-installation HDD/SSD disk on the host"},
				apiStatus:                   api.Failure,
				diskCount:                   1,
				hostCPU:                     9,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeNone,
				hostRole:                    models.HostRoleAutoAssign,
				hostMem:                     16*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "Compact version 4.15, HostRoleAutoAssign",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterCompactNode, masterCompactNode},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     7,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleAutoAssign,
				hostMem:                     24*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "Compact version 4.15, HostRoleMaster",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterCompactNode, masterCompactNode},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     7,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleMaster,
				hostMem:                     24*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "Compact version 4.15, Worker",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterCompactNode, masterCompactNode},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     7,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleWorker,
				hostMem:                     24*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "full version 4.15, Master",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterNode, masterNode, workerNode, workerNode, workerNode},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   1,
				hostCPU:                     4,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleMaster,
				hostMem:                     16 * conversions.GiB,
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "full version 4.15, Worker",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterNode, masterNode, masterNode, workerNode, workerNode},
				resultMessage:               nil,
				apiStatus:                   api.Success,
				diskCount:                   2,
				hostCPU:                     3,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleWorker,
				hostMem:                     8*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "full version 4.15, Worker  HostRoleAutoAssign",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterNode, masterNode, masterNode, workerNode, workerNode},
				resultMessage:               []string{"For Logical Volume Manager Standard Mode, host role must be assigned to master or worker."},
				apiStatus:                   api.Failure,
				diskCount:                   2,
				hostCPU:                     3,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleAutoAssign,
				hostMem:                     8*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "full version 4.15, Worker, insufficient Disk",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterNode, masterNode, masterNode, workerNode, workerNode},
				resultMessage:               []string{"Logical Volume Manager requires at least one non-installation HDD/SSD disk on the host"},
				apiStatus:                   api.Failure,
				diskCount:                   1,
				hostCPU:                     3,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleWorker,
				hostMem:                     8*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(0),
			},
			{
				name:                        "full version 4.15, Worker, insufficient Disk size",
				ocpVersion:                  "4.15.0",
				hosts:                       []*models.Host{masterNode, masterNode, masterNode, workerNode, workerNode},
				resultMessage:               []string{"Logical Volume Manager requires at least one non-installation HDD/SSD disk of 50GB minimum on the host"},
				apiStatus:                   api.Failure,
				diskCount:                   1,
				hostCPU:                     3,
				HighAvailabilityMode:        models.ClusterHighAvailabilityModeFull,
				hostRole:                    models.HostRoleWorker,
				hostMem:                     8*conversions.GiB + float32(lvmMemMB),
				additionalDiskRequirementGB: int64(50),
			},
		}

		for i := range hostValidationTests {
			test := hostValidationTests[i]
			It(fmt.Sprintf("ValidateHost , %s", test.name), func() {
				disks := make([]*models.Disk, test.diskCount)
				for x := range disks {
					disks[x] = &models.Disk{
						ID:        fmt.Sprintf("/dev/disk/by-id/test-disk-%d", x+1),
						DriveType: models.DriveTypeHDD,
						SizeBytes: 20 * conversions.GB,
					}
				}

				testHost := templateHost
				testHost.Role = test.hostRole
				testHost.SuggestedRole = models.HostRoleAutoAssign
				testHost.Inventory = Inventory(&InventoryResources{
					Cpus:  test.hostCPU,
					Ram:   int64(test.hostMem),
					Disks: disks,
				})
				test.hosts = append(test.hosts, testHost)

				var schedulableMasters bool = false
				if len(test.hosts) <= 3 {
					schedulableMasters = true
				}
				cluster := &common.Cluster{Cluster: models.Cluster{
					OpenshiftVersion: test.ocpVersion,
					Hosts:            test.hosts, HighAvailabilityMode: &test.HighAvailabilityMode,
					SchedulableMasters:           swag.Bool(false),
					SchedulableMastersForcedTrue: &schedulableMasters,
				}}

				operator.SetAdditionalDiskRequirements(test.additionalDiskRequirementGB)
				res, _ := operator.ValidateHost(ctx, cluster, testHost)

				Expect(test.resultMessage).Should(Equal(res.Reasons))
				Expect(test.apiStatus).Should(Equal(res.Status))

			})
		}

	})
})
