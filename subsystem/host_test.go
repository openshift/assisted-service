package subsystem

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	serviceHost "github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/subsystem/utils_test"
)

var _ = Describe("Host tests", func() {
	ctx := context.Background()
	var cluster *installer.V2RegisterClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID *strfmt.UUID

	BeforeEach(func() {
		var err error
		cluster, err = utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String(defaultOpenshiftVersion),
				PullSecret:        swag.String(pullSecret),
				VipDhcpAllocation: swag.Bool(false),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
		infraEnvID = registerInfraEnvSpecificVersion(&clusterID, models.ImageTypeMinimalIso, cluster.Payload.OpenshiftVersion).ID
	})

	It("Should reject hostname if it is forbidden", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())

		hostnames := []string{
			"localhost",
			"localhost.localdomain",
			"localhost4",
			"localhost4.localdomain4",
			"localhost6",
			"localhost6.localdomain6",
		}

		for i := range hostnames {
			hostnameChangeRequest := &installer.V2UpdateHostParams{
				InfraEnvID: *infraEnvID,
				HostID:     *host.ID,
				HostUpdateParams: &models.HostUpdateParams{
					HostName: &hostnames[i],
				},
			}
			_, hostnameUpdateError := utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, hostnameChangeRequest)
			Expect(hostnameUpdateError).To(HaveOccurred())
		}
	})

	It("Should accept hostname if it is permitted", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())

		hostname := "arbitrary.hostname"
		hostnameChangeRequest := &installer.V2UpdateHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &hostname,
			},
		}
		_, hostnameUpdateError := utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, hostnameChangeRequest)
		Expect(hostnameUpdateError).NotTo(HaveOccurred())
	})

	It("host CRUD", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering"))
		Expect(host.StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		list, err := utils_test.TestContext.UserBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		list, err = utils_test.TestContext.AgentBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		list, err = utils_test.TestContext.UserBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = utils_test.TestContext.UserBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).Should(HaveOccurred())
	})

	It("should update host installation disk id successfully", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		inventory, error := common.UnmarshalInventory(defaultInventory())
		Expect(error).ToNot(HaveOccurred())
		inventory.Disks = []*models.Disk{
			{
				ID:        "wwn-0x1111111111111111111111",
				ByID:      "wwn-0x1111111111111111111111",
				DriveType: "HDD",
				Name:      "sda",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
			{
				ID:        "wwn-0x2222222222222222222222",
				ByID:      "wwn-0x2222222222222222222222",
				DriveType: "HDD",
				Name:      "sdb",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
		}

		inventoryStr, err := common.MarshalInventory(inventory)
		Expect(err).ToNot(HaveOccurred())
		host = updateInventory(ctx, *infraEnvID, *host.ID, inventoryStr)

		Expect(host.InstallationDiskID).To(Equal(inventory.Disks[0].ID))
		Expect(host.InstallationDiskPath).To(Equal(common.GetDeviceFullName(inventory.Disks[0])))

		diskSelectionRequest := &installer.V2UpdateHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				DisksSelectedConfig: []*models.DiskConfigParams{
					{ID: &inventory.Disks[1].ID, Role: models.DiskRoleInstall},
					{ID: &inventory.Disks[0].ID, Role: models.DiskRoleNone},
				},
			},
		}

		updatedHost, updateError := utils_test.TestContext.UserBMClient.Installer.V2UpdateHost(ctx, diskSelectionRequest)
		Expect(updateError).NotTo(HaveOccurred())

		host = updatedHost.Payload
		Expect(host.InstallationDiskID).To(Equal(inventory.Disks[1].ID))
		Expect(host.InstallationDiskPath).To(Equal(common.GetDeviceFullName(inventory.Disks[1])))
	})

	It("should select bootable disk as default installation disk", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())
		inventory, error := common.UnmarshalInventory(defaultInventory())
		Expect(error).ToNot(HaveOccurred())
		inventory.Disks = []*models.Disk{
			{
				ID:        "wwn-0x1111111111111111111111",
				ByID:      "wwn-0x1111111111111111111111",
				DriveType: "SSD",
				Name:      "nvme0",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  false,
			},
			{
				ID:        "wwn-0x2222222222222222222222",
				ByID:      "wwn-0x2222222222222222222222",
				DriveType: "SSD",
				Name:      "nvme1",
				SizeBytes: int64(120) * (int64(1) << 30),
				Bootable:  true,
			},
		}

		inventoryStr, err := common.MarshalInventory(inventory)
		Expect(err).ToNot(HaveOccurred())
		host = updateInventory(ctx, *infraEnvID, *host.ID, inventoryStr)

		Expect(host.InstallationDiskID).To(Equal(inventory.Disks[1].ID))
		Expect(host.InstallationDiskPath).To(Equal(common.GetDeviceFullName(inventory.Disks[1])))
	})

	It("next step", func() {
		_, err := utils_test.TestContext.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterID: clusterID,
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		host2 := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		Expect(db.Model(host2).UpdateColumns(&models.Host{Inventory: defaultInventory(),
			Status:             swag.String(models.HostStatusInsufficient),
			InstallationDiskID: "wwn-0x1111111111111111111111"}).Error).NotTo(HaveOccurred())
		steps := utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeFalse())
		Expect(db.Save(&models.APIVip{IP: "1.2.3.4", ClusterID: clusterID}).Error).ToNot(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "known").Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "disabled").Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(steps.NextInstructionSeconds).Should(Equal(int64(120)))
		Expect(*steps.PostStepAction).Should(Equal(models.StepsPostStepActionContinue))
		Expect(len(steps.Instructions)).Should(Equal(0))
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "error").Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeStopInstallation)).Should(BeTrue())
		Expect(db.Model(host).Update("status", models.HostStatusResetting).Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host.ID)
		Expect(len(steps.Instructions)).Should(Equal(0))
		Expect(db.Model(cluster.GetPayload()).Update("status", models.ClusterStatusPreparingForInstallation).Error).NotTo(HaveOccurred())
		Expect(db.Model(host2).Update("status", models.HostStatusPreparingForInstallation).Error).NotTo(HaveOccurred())
		steps = utils_test.TestContext.GetNextSteps(*infraEnvID, *host2.ID)
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeInstallationDiskSpeedCheck)).Should(BeTrue())
		Expect(utils_test.IsStepTypeInList(steps, models.StepTypeContainerImageAvailability)).Should(BeTrue())
	})

	It("host_disconnection", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("CheckedInAt", strfmt.DateTime(time.Time{})).Error).NotTo(HaveOccurred())

		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		time.Sleep(time.Second * 3)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("error"))
		Expect(swag.StringValue(host.StatusInfo)).Should(Equal("Host failed to install due to timeout while connecting to host during the installation phase."))
	})

	It("host installation progress", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		bootstrapStages := serviceHost.BootstrapStages[:]
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())

		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageStartingInstallation)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageStartingInstallation))
		time.Sleep(time.Second * 3)
		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageInstalling)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageInstalling))
		time.Sleep(time.Second * 3)
		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageWritingImageToDisk)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk))
		time.Sleep(time.Second * 3)
		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageRebooting)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageRebooting))
		time.Sleep(time.Second * 3)
		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageConfiguring)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageConfiguring))
		time.Sleep(time.Second * 3)
		utils_test.TestContext.UpdateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageDone))
		time.Sleep(time.Second * 3)
	})

	It("installation_error_reply", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "worker").Error).NotTo(HaveOccurred())

		_, err := utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: 137,
				Output:   "Failed to install",
				StepType: models.StepTypeInstall,
				StepID:   "installCmd-" + string(models.StepTypeExecute),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("error"))
		Expect(swag.StringValue(host.StatusInfo)).Should(Equal("installation command failed"))

	})

	It("connectivity_report_store_only_relevant_reply", func() {
		host := &utils_test.TestContext.RegisterHost(*infraEnvID).Host

		connectivity := "{\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null,\"mtu_report\":null}]}"
		extraConnectivity := "{\"extra\":\"data\",\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null}]}"

		_, err := utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   extraConnectivity,
				StepID:   string(models.StepTypeConnectivityCheck),
				StepType: models.StepTypeConnectivityCheck,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		_, err = utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   "not a json",
				StepID:   string(models.StepTypeConnectivityCheck),
				StepType: models.StepTypeConnectivityCheck,
			},
		})
		Expect(err).To(HaveOccurred())
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		//exit code is not 0
		_, err = utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			Reply: &models.StepReply{
				ExitCode: -1,
				Error:    "some error",
				Output:   "not a json",
				StepID:   string(models.StepTypeConnectivityCheck),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		host = utils_test.TestContext.GetHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

	})

	Context("image availability", func() {

		var (
			h           *models.Host
			imageStatus *models.ContainerImageAvailability
		)

		BeforeEach(func() {
			h = &utils_test.TestContext.RegisterHost(*infraEnvID).Host
		})

		getHostImageStatus := func(hostID strfmt.UUID, imageName string) *models.ContainerImageAvailability {
			hostInDb := utils_test.TestContext.GetHostV2(*infraEnvID, hostID)

			var hostImageStatuses map[string]*models.ContainerImageAvailability
			Expect(json.Unmarshal([]byte(hostInDb.ImagesStatus), &hostImageStatuses)).ShouldNot(HaveOccurred())

			return hostImageStatuses[imageName]
		}

		It("First success good bandwidth", func() {
			By("pull success", func() {
				imageStatus = common.TestImageStatusesSuccess

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
			})

			By("network failure", func() {
				newImageStatus := common.TestImageStatusesFailure
				expectedImageStatus := &models.ContainerImageAvailability{
					Name:         newImageStatus.Name,
					Result:       newImageStatus.Result,
					DownloadRate: imageStatus.DownloadRate,
					SizeBytes:    imageStatus.SizeBytes,
					Time:         imageStatus.Time,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})

			By("network fixed", func() {
				newImageStatus := &models.ContainerImageAvailability{
					Name:   imageStatus.Name,
					Result: models.ContainerImageAvailabilityResultSuccess,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
			})
		})

		It("First success bad bandwidth", func() {
			By("pull success", func() {
				imageStatus = &models.ContainerImageAvailability{
					Name:         common.TestDefaultConfig.ImageName,
					Result:       models.ContainerImageAvailabilityResultSuccess,
					DownloadRate: 0.000333,
					SizeBytes:    333000000.0,
					Time:         1000000.0,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})

			By("network failure", func() {
				newImageStatus := common.TestImageStatusesFailure
				expectedImageStatus := &models.ContainerImageAvailability{
					Name:         newImageStatus.Name,
					Result:       newImageStatus.Result,
					DownloadRate: imageStatus.DownloadRate,
					SizeBytes:    imageStatus.SizeBytes,
					Time:         imageStatus.Time,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})

			By("network fixed", func() {
				newImageStatus := &models.ContainerImageAvailability{
					Name:   imageStatus.Name,
					Result: models.ContainerImageAvailabilityResultSuccess,
				}
				expectedImageStatus := &models.ContainerImageAvailability{
					Name:         newImageStatus.Name,
					Result:       newImageStatus.Result,
					DownloadRate: imageStatus.DownloadRate,
					SizeBytes:    imageStatus.SizeBytes,
					Time:         imageStatus.Time,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})
		})

		It("First failure", func() {
			By("pull failed", func() {
				imageStatus = common.TestImageStatusesFailure

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})
			By("network fixed", func() {
				newImageStatus := common.TestImageStatusesSuccess
				expectedImageStatus := &models.ContainerImageAvailability{
					Name:         newImageStatus.Name,
					Result:       newImageStatus.Result,
					DownloadRate: imageStatus.DownloadRate,
					SizeBytes:    imageStatus.SizeBytes,
					Time:         imageStatus.Time,
				}

				utils_test.TestContext.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				utils_test.TestContext.WaitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
			})
		})
	})

	It("register_same_host_id", func() {
		hostID := utils_test.StrToUUID(uuid.New().String())
		// register to cluster1
		_, err := utils_test.TestContext.AgentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cluster2, err := utils_test.TestContext.UserBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("another-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		infraEnvID2 := registerInfraEnv(cluster2.GetPayload().ID, models.ImageTypeMinimalIso).ID

		// register to cluster2
		_, err = utils_test.TestContext.AgentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID2,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// successfully get from both clusters
		_ = utils_test.TestContext.GetHostV2(*infraEnvID, *hostID)
		h2 := utils_test.TestContext.GetHostV2(*infraEnvID2, *hostID)
		h2initialRegistrationTimestamp := h2.RegisteredAt

		_, err = utils_test.TestContext.UserBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *hostID,
		})
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(time.Second * 2)
		_, err = utils_test.TestContext.AgentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID2,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// confirm if new registration updated the timestamp
		h2 = utils_test.TestContext.GetHostV2(*infraEnvID2, *hostID)
		h2newRegistrationTimestamp := h2.RegisteredAt
		Expect(h2newRegistrationTimestamp.Equal(h2initialRegistrationTimestamp)).Should(BeFalse())

		Eventually(func() string {
			h := utils_test.TestContext.GetHostV2(*infraEnvID2, *hostID)
			return swag.StringValue(h.Status)
		}, "30s", "1s").Should(Equal(models.HostStatusDiscovering))
	})

	It("register_wrong_pull_secret", func() {
		if Options.AuthType == auth.TypeNone {
			Skip("auth is disabled")
		}

		wrongTokenStubID, err := wiremock.CreateWrongStubTokenAuth(utils_test.WrongPullSecret)
		Expect(err).ToNot(HaveOccurred())

		hostID := utils_test.StrToUUID(uuid.New().String())
		_, err = utils_test.TestContext.BadAgentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).To(HaveOccurred())

		err = wiremock.DeleteStub(wrongTokenStubID)
		Expect(err).ToNot(HaveOccurred())
	})

	It("next_step_runner_command", func() {
		registration := utils_test.TestContext.RegisterHost(*infraEnvID)
		Expect(registration.NextStepRunnerCommand).ShouldNot(BeNil())
		Expect(registration.NextStepRunnerCommand.Command).Should(BeEmpty())
		Expect(registration.NextStepRunnerCommand.Args).ShouldNot(BeEmpty())
		Expect(registration.NextStepRunnerCommand.RetrySeconds).Should(Equal(int64(0))) //default, just to have in the API
	})
})

func updateInventory(ctx context.Context, infraEnvId strfmt.UUID, hostId strfmt.UUID, inventory string) *models.Host {
	_, err := utils_test.TestContext.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: infraEnvId,
		HostID:     hostId,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   inventory,
			StepID:   uuid.New().String(),
			StepType: models.StepTypeInventory,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	host := utils_test.TestContext.GetHostV2(infraEnvId, hostId)
	Expect(host).NotTo(BeNil())
	Expect(host.Inventory).NotTo(BeEmpty())
	return host
}
