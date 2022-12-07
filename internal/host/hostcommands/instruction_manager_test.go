package hostcommands

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/connectivity"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const UNBOUND_SOURCE = "my-unbound-source"

var _ = Describe("instruction_manager", func() {
	var (
		ctx                           = context.Background()
		host                          models.Host
		db                            *gorm.DB
		mockEvents                    *eventsapi.MockHandler
		mockVersions                  *versions.MockHandler
		mockOSImages                  *versions.MockOSImages
		stepsReply                    models.Steps
		hostId, clusterId, infraEnvId strfmt.UUID
		stepsErr                      error
		instMng                       *InstructionManager
		ctrl                          *gomock.Controller
		hwValidator                   *hardware.MockValidator
		mockRelease                   *oc.MockRelease
		cnValidator                   *connectivity.MockValidator
		instructionConfig             InstructionConfig
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockOSImages = versions.NewMockOSImages(ctrl)
		hwValidator = hardware.NewMockValidator(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		cnValidator = connectivity.NewMockValidator(ctrl)
		instMng = NewInstructionManager(common.GetTestLog(), db, hwValidator, mockRelease, instructionConfig, cnValidator, mockEvents, mockVersions, mockOSImages)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, "unknown invalid state")
		host.Role = models.HostRoleMaster
		host.Inventory = hostutil.GenerateMasterInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		anotherHost := hostutil.GenerateTestHost(strfmt.UUID(uuid.New().String()), infraEnvId, clusterId, "insufficient")
		Expect(db.Create(&anotherHost).Error).ShouldNot(HaveOccurred())
	})

	checkStep := func(state string, expectedStepTypes []models.StepType) {
		checkStepsByState(state, &host, db, mockEvents, instMng, hwValidator, mockRelease, mockVersions, cnValidator, ctx, expectedStepTypes)
	}

	Context("No DHCP", func() {
		BeforeEach(func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                &clusterId,
					VipDhcpAllocation: swag.Bool(false),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Name:              "example",
					BaseDNSDomain:     "test.com",
					OpenshiftVersion:  "4.9",
				}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			infraEnv := common.InfraEnv{
				InfraEnv: models.InfraEnv{
					ID:        &infraEnvId,
					ClusterID: clusterId,
				},
			}
			Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
		})
		Context("get_next_steps", func() {
			It("invalid_host_state", func() {
				stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
				Expect(stepsReply.Instructions).To(HaveLen(0))
				Expect(stepsErr).Should(BeNil())
			})
			It("discovering", func() {
				checkStep(models.HostStatusDiscovering, []models.StepType{
					models.StepTypeInventory,
				})
			})
			It("known", func() {
				checkStep(models.HostStatusKnown, []models.StepType{
					models.StepTypeConnectivityCheck, models.StepTypeFreeNetworkAddresses,
					models.StepTypeInventory, models.StepTypeNtpSynchronizer,
					models.StepTypeDomainResolution,
				})
			})
			It("disconnected", func() {
				checkStep(models.HostStatusDisconnected, []models.StepType{
					models.StepTypeInventory,
				})
			})
			It("insufficient", func() {
				checkStep(models.HostStatusInsufficient, []models.StepType{
					models.StepTypeInventory, models.StepTypeConnectivityCheck,
					models.StepTypeFreeNetworkAddresses, models.StepTypeNtpSynchronizer,
					models.StepTypeDomainResolution,
				})
			})
			It("pending-for-input", func() {
				checkStep(models.HostStatusPendingForInput, []models.StepType{
					models.StepTypeInventory, models.StepTypeConnectivityCheck,
					models.StepTypeFreeNetworkAddresses, models.StepTypeNtpSynchronizer,
					models.StepTypeDomainResolution,
				})
			})
			It("error", func() {
				checkStep(models.HostStatusError, []models.StepType{
					models.StepTypeLogsGather, models.StepTypeStopInstallation,
				})
			})
			It("error with already uploades logs", func() {
				host.LogsCollectedAt = strfmt.DateTime(time.Now())
				db.Save(&host)
				checkStep(models.HostStatusError, []models.StepType{
					models.StepTypeStopInstallation,
				})
			})
			It("cancelled", func() {
				checkStep(models.HostStatusCancelled, []models.StepType{
					models.StepTypeLogsGather, models.StepTypeStopInstallation,
				})
			})
			It("installing", func() {
				checkStep(models.HostStatusInstalling, []models.StepType{
					models.StepTypeInstall,
				})
			})
			It("reset", func() {
				checkStep(models.HostStatusResetting, []models.StepType{})
			})
			It("binding", func() {
				checkStep(models.HostStatusBinding, nil)
			})
		})
	})

	Context("With DHCP", func() {
		BeforeEach(func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:                &clusterId,
				VipDhcpAllocation: swag.Bool(true),
				MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
				Name:              "example",
				BaseDNSDomain:     "test.com",
				OpenshiftVersion:  "4.9",
			}}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			infraEnv := common.InfraEnv{
				InfraEnv: models.InfraEnv{
					ID:        &infraEnvId,
					ClusterID: clusterId,
				},
			}
			Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
		})
		Context("get_next_steps", func() {
			It("invalid_host_state", func() {
				stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
				Expect(stepsReply.Instructions).To(HaveLen(0))
				Expect(stepsErr).Should(BeNil())
			})
			It("discovering", func() {
				checkStep(models.HostStatusDiscovering, []models.StepType{
					models.StepTypeInventory,
				})
			})
			It("known", func() {
				checkStep(models.HostStatusKnown, []models.StepType{
					models.StepTypeConnectivityCheck, models.StepTypeFreeNetworkAddresses,
					models.StepTypeDhcpLeaseAllocate, models.StepTypeInventory,
					models.StepTypeNtpSynchronizer, models.StepTypeDomainResolution,
				})
			})
			It("binding", func() {
				checkStep(models.HostStatusBinding, nil)
			})
			It("disconnected", func() {
				checkStep(models.HostStatusDisconnected, []models.StepType{
					models.StepTypeInventory,
				})
			})
			It("insufficient", func() {
				checkStep(models.HostStatusInsufficient, []models.StepType{
					models.StepTypeInventory, models.StepTypeConnectivityCheck,
					models.StepTypeFreeNetworkAddresses, models.StepTypeDhcpLeaseAllocate,
					models.StepTypeNtpSynchronizer, models.StepTypeDomainResolution,
				})
			})
			It("pending-for-input", func() {
				checkStep(models.HostStatusPendingForInput, []models.StepType{
					models.StepTypeInventory, models.StepTypeConnectivityCheck,
					models.StepTypeFreeNetworkAddresses, models.StepTypeDhcpLeaseAllocate,
					models.StepTypeNtpSynchronizer, models.StepTypeDomainResolution,
				})
			})
			It("error", func() {
				checkStep(models.HostStatusError, []models.StepType{
					models.StepTypeLogsGather, models.StepTypeStopInstallation,
				})
			})
			It("cancelled", func() {
				checkStep(models.HostStatusCancelled, []models.StepType{
					models.StepTypeLogsGather, models.StepTypeStopInstallation,
				})
			})
			It("installing", func() {
				checkStep(models.HostStatusInstalling, []models.StepType{
					models.StepTypeInstall, models.StepTypeDhcpLeaseAllocate,
				})
			})
			It("installing-in-progress", func() {
				checkStep(models.HostStatusInstallingInProgress, []models.StepType{
					models.StepTypeDhcpLeaseAllocate,
				})
			})
			It("reset", func() {
				checkStep(models.HostStatusResetting, []models.StepType{})
			})
		})
	})

	Context("Unbound host steps", func() {
		BeforeEach(func() {
			Expect(db.Session(&gorm.Session{AllowGlobalUpdate: true}).Model(&common.Host{}).Select("cluster_id").Updates(map[string]interface{}{"cluster_id": nil}).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&common.InfraEnv{
				InfraEnv: models.InfraEnv{
					ID:                   &infraEnvId,
					AdditionalNtpSources: UNBOUND_SOURCE,
				},
			}).Error).ToNot(HaveOccurred())
		})

		It("discovering-unbound", func() {
			checkStep(models.HostStatusDiscoveringUnbound, []models.StepType{
				models.StepTypeInventory, models.StepTypeNtpSynchronizer,
			})
		})

		It("disconnected-unbound", func() {
			checkStep(models.HostStatusDisconnectedUnbound, []models.StepType{
				models.StepTypeInventory,
			})
		})

		It("insufficient-unbound", func() {
			checkStep(models.HostStatusInsufficientUnbound, []models.StepType{
				models.StepTypeInventory, models.StepTypeNtpSynchronizer,
			})
		})

		It("known-unbound", func() {
			checkStep(models.HostStatusKnownUnbound, []models.StepType{
				models.StepTypeInventory, models.StepTypeNtpSynchronizer,
			})
		})

		It("unbinding", func() {
			checkStep(models.HostStatusUnbinding, nil)
		})

		It("unbinding-pending-user-action", func() {
			checkStep(models.HostStatusUnbindingPendingUserAction, nil)
		})
		It("reclaiming", func() {
			mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			checkStep(models.HostStatusReclaiming, []models.StepType{
				models.StepTypeDownloadBootArtifacts,
			})
		})
		It("reclaiming-rebooting", func() {
			checkStep(models.HostStatusReclaimingRebooting, []models.StepType{
				models.StepTypeRebootForReclaim,
			})
		})
	})

	Context("Disable Steps verification", func() {
		createInstMngWithDisabledSteps := func(steps []models.StepType) *InstructionManager {
			instructionConfig.DisabledSteps = steps
			return NewInstructionManager(common.GetTestLog(), db, hwValidator, mockRelease, instructionConfig, cnValidator, mockEvents, mockVersions, mockOSImages)
		}
		Context("disabledStepsMap in InstructionManager", func() {
			It("Should except empty DISABLED_STEPS", func() {
				Expect(instMng.disabledStepsMap).Should(BeEmpty())
			})
			It("Should filter out all DISABLED_STEPS when all are invalid", func() {
				instMng = createInstMngWithDisabledSteps([]models.StepType{"invalid step", "Invalid step 2"})
				Expect(instMng.disabledStepsMap).Should(BeEmpty())
			})
			It("Should filter out any invalid StepType from disabled steps in InstructionManager", func() {
				disabledSteps := []models.StepType{
					"invalid-step",
					models.StepTypeConnectivityCheck,
					models.StepTypeAPIVipConnectivityCheck,
					models.StepTypeDhcpLeaseAllocate,
				}
				instMng = createInstMngWithDisabledSteps(disabledSteps)
				Expect(len(instMng.disabledStepsMap)).Should(Equal(len(disabledSteps) - 1))
			})
		})
		Context("InstructionManager.GetNextSteps should be filtered according to disabled steps", func() {
			BeforeEach(func() {
				cluster := common.Cluster{Cluster: models.Cluster{
					ID:                &clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					OpenshiftVersion:  "4.9",
				}}
				Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

				infraEnv := common.InfraEnv{
					InfraEnv: models.InfraEnv{
						ID:        &infraEnvId,
						ClusterID: clusterId,
					},
				}
				Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
			})
			It("Should not filter out any step when: HostState=installing DisabledSteps=execute.", func() {
				instMng = createInstMngWithDisabledSteps([]models.StepType{models.StepTypeExecute})
				checkStep(models.HostStatusInstalling, []models.StepType{
					models.StepTypeInstall, models.StepTypeDhcpLeaseAllocate,
				})
			})
			It("Should filter out StepTypeDhcpLeaseAllocate when: HostState=installing DisabledSteps=execute,dhcp-lease-allocate.", func() {
				instMng = createInstMngWithDisabledSteps([]models.StepType{
					models.StepTypeDhcpLeaseAllocate,
				})
				checkStep(models.HostStatusInstalling, []models.StepType{
					models.StepTypeInstall,
				})
			})
			It("Should filter out StepTypeExecute (No steps) when: HostState=error DisabledSteps=execute.", func() {
				instMng = createInstMngWithDisabledSteps([]models.StepType{
					models.StepTypeLogsGather,
					models.StepTypeStopInstallation,
					models.StepTypeDhcpLeaseAllocate,
				})
				checkStep(models.HostStatusError, []models.StepType{})
			})
			It("Should skip 'StepTypeFreeNetworkAddresses' when: HostState=insufficient DisabledSteps=StepTypeFreeNetworkAddresses", func() {
				instMng = createInstMngWithDisabledSteps([]models.StepType{
					models.StepTypeFreeNetworkAddresses,
				})
				checkStep(models.HostStatusInsufficient, []models.StepType{
					models.StepTypeInventory,
					models.StepTypeConnectivityCheck,
					models.StepTypeDhcpLeaseAllocate,
					models.StepTypeNtpSynchronizer,
				})
			})
		})
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
		stepsReply = models.Steps{}
		stepsErr = nil
	})

})

var _ = Describe("agent_upgrade", func() {
	var (
		ctx                           = context.Background()
		host                          models.Host
		db                            *gorm.DB
		mockEvents                    *eventsapi.MockHandler
		mockVersions                  *versions.MockHandler
		mockOSImages                  *versions.MockOSImages
		stepsReply                    models.Steps
		hostId, clusterId, infraEnvId strfmt.UUID
		stepsErr                      error
		instMng                       *InstructionManager
		ctrl                          *gomock.Controller
		hwValidator                   *hardware.MockValidator
		mockRelease                   *oc.MockRelease
		cnValidator                   *connectivity.MockValidator
		instructionConfig             InstructionConfig
		dbName                        string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockVersions = versions.NewMockHandler(ctrl)
		mockOSImages = versions.NewMockOSImages(ctrl)
		hwValidator = hardware.NewMockValidator(ctrl)
		mockRelease = oc.NewMockRelease(ctrl)
		cnValidator = connectivity.NewMockValidator(ctrl)
		instructionConfig = InstructionConfig{AgentImage: "quay.io/my/image:v1.2.3"}
		instructionConfig.EnableUpgradeAgent = true
		instMng = NewInstructionManager(common.GetTestLog(), db, hwValidator, mockRelease, instructionConfig, cnValidator, mockEvents, mockVersions, mockOSImages)
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusInsufficient)
		host.Role = models.HostRoleMaster
		host.Inventory = hostutil.GenerateMasterInventory()
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	})

	Context("When the host status allows upgrade", func() {
		upgradeAllowdStatuses := []string{models.HostStatusBinding,
			models.HostStatusDiscovering,
			models.HostStatusDiscoveringUnbound,
			models.HostStatusInsufficient,
			models.HostStatusInsufficientUnbound,
			models.HostStatusKnown,
			models.HostStatusPendingForInput,
			models.HostStatusKnownUnbound}
		for _, hostStatus := range upgradeAllowdStatuses {
			hostStatus := hostStatus
			It(fmt.Sprintf("Creates a single upgrade agent step, hosts stauts: %s", hostStatus), func() {
				Expect(db.Model(&host).Update("Status", hostStatus).Error).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.UpgradeAgentStartedEventName),
					eventstest.WithHostIdMatcher(host.ID.String()),
					eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String())))
				stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
				Expect(stepsErr).Should(BeNil())
				Expect(stepsErr).ToNot(HaveOccurred())
				Expect(stepsReply).ToNot(BeNil())
				Expect(stepsReply.Instructions).To(HaveLen(1))
				Expect(stepsReply.Instructions[0].StepType).To(Equal(models.StepTypeUpgradeAgent))
			})
		}
	})

	Context("When the host status doesn't allows upgrade", func() {
		upgradeAllowdStatuses := []string{models.HostStatusInstalled,
			models.HostStatusInstalling,
			models.HostStatusDisabled,
			models.HostStatusDisabledUnbound,
			models.HostStatusError,
			models.HostStatusUnbindingPendingUserAction,
			models.HostStatusInstallingPendingUserAction,
			models.HostStatusResetting,
			models.HostStatusReclaiming}
		for _, hostStatus := range upgradeAllowdStatuses {
			hostStatus := hostStatus
			It(fmt.Sprintf("Don't creates upgrade agent step, hosts stauts: %s", hostStatus), func() {
				Expect(db.Model(&host).Update("Status", hostStatus).Error).ShouldNot(HaveOccurred())
				stepsReply, stepsErr = instMng.GetNextSteps(ctx, &host)
				Expect(stepsErr).Should(BeNil())
				Expect(stepsErr).ToNot(HaveOccurred())
				Expect(stepsReply).ToNot(BeNil())
				if len(stepsReply.Instructions) > 0 {
					Expect(stepsReply.Instructions[0].StepType).ToNot(Equal(models.StepTypeUpgradeAgent))
				}
			})
		}
	})

	AfterEach(func() {
		// cleanup
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
		stepsReply = models.Steps{}
		stepsErr = nil
	})

})

func checkStepsByState(state string, host *models.Host, db *gorm.DB, mockEvents *eventsapi.MockHandler,
	instMng *InstructionManager, mockValidator *hardware.MockValidator, mockRelease *oc.MockRelease, mockVersions *versions.MockHandler,
	mockConnectivity *connectivity.MockValidator, ctx context.Context, expectedStepTypes []models.StepType) {

	mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
		eventstest.WithNameMatcher(eventgen.HostStatusUpdatedEventName),
		eventstest.WithHostIdMatcher(host.ID.String()),
		eventstest.WithInfraEnvIdMatcher(host.InfraEnvID.String())))
	updateReply, updateErr := hostutil.UpdateHostStatus(ctx, common.GetTestLog(), db, mockEvents, host.InfraEnvID, *host.ID, *host.Status, state, "")
	ExpectWithOffset(1, updateErr).ShouldNot(HaveOccurred())
	ExpectWithOffset(1, updateReply).ShouldNot(BeNil())
	h := hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db)
	ExpectWithOffset(1, swag.StringValue(h.Status)).Should(Equal(state))
	if funk.Contains(expectedStepTypes, models.StepTypeInstall) {
		mockValidator.EXPECT().GetHostInstallationPath(gomock.Any()).Return("/dev/disk/by-id/wwn-sda").Times(1)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockRelease.EXPECT().GetMCOImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMCOImage, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).Times(1)
	}
	if funk.Contains(expectedStepTypes, models.StepTypeConnectivityCheck) {
		mockConnectivity.EXPECT().GetHostValidInterfaces(gomock.Any()).Return([]*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.10/24",
				},
				MacAddress: "52:54:00:09:de:93",
			},
		}, nil).Times(1)
	}

	if funk.Contains(expectedStepTypes, models.StepTypeContainerImageAvailability) {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockVersions.EXPECT().GetMustGatherImages(gomock.Any(), gomock.Any(), gomock.Any()).Return(defaultMustGatherVersion, nil).Times(1)
	}
	stepsReply, stepsErr := instMng.GetNextSteps(ctx, &h.Host)
	ExpectWithOffset(1, stepsReply.Instructions).To(HaveLen(len(expectedStepTypes)))
	if stateValues, ok := instMng.installingClusterStateToSteps[state]; ok {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(stateValues.NextStepInSec))
		ExpectWithOffset(1, *stepsReply.PostStepAction).Should(Equal(stateValues.PostStepAction))
	} else if stateValues, ok = instMng.addHostsClusterToSteps[state]; ok {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(stateValues.NextStepInSec))
		ExpectWithOffset(1, *stepsReply.PostStepAction).Should(Equal(stateValues.PostStepAction))
	} else if stateValues, ok = instMng.poolHostToSteps[state]; ok {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(stateValues.NextStepInSec))
		ExpectWithOffset(1, *stepsReply.PostStepAction).Should(Equal(stateValues.PostStepAction))
	} else {
		Expect(stepsReply.NextInstructionSeconds).Should(Equal(defaultNextInstructionInSec))
		ExpectWithOffset(1, *stepsReply.PostStepAction).Should(Equal(models.StepsPostStepActionContinue))
	}

	for i, step := range stepsReply.Instructions {
		ExpectWithOffset(1, step.StepType).Should(Equal(expectedStepTypes[i]))
		if expectedStepTypes[i] == models.StepTypeNtpSynchronizer && funk.ContainsString([]string{models.HostStatusKnownUnbound,
			models.HostStatusInsufficientUnbound, models.HostStatusDiscoveringUnbound}, state) {
			Expect(strings.Join(step.Args, ",")).To(ContainSubstring(UNBOUND_SOURCE))
		}
	}

	ExpectWithOffset(1, stepsErr).ShouldNot(HaveOccurred())
}

func TestHostCommands(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "Host commands test Suite")
}
