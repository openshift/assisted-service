package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
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
)

var _ = Describe("Host tests", func() {
	ctx := context.Background()
	var cluster *installer.V2RegisterClusterCreated
	var clusterID strfmt.UUID
	var infraEnvID *strfmt.UUID

	BeforeEach(func() {
		var err error
		cluster, err = userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:              swag.String("test-cluster"),
				OpenshiftVersion:  swag.String(openshiftVersion),
				PullSecret:        swag.String(pullSecret),
				VipDhcpAllocation: swag.Bool(true),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
	})

	It("Should reject hostname if it is forbidden", func() {
		host := &registerHost(*infraEnvID).Host
		host = getHostV2(*infraEnvID, *host.ID)
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
			_, hostnameUpdateError := userBMClient.Installer.V2UpdateHost(ctx, hostnameChangeRequest)
			Expect(hostnameUpdateError).To(HaveOccurred())
		}
	})

	It("Should accept hostname if it is permitted", func() {
		host := &registerHost(*infraEnvID).Host
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host).NotTo(BeNil())

		hostname := "arbitrary.hostname"
		hostnameChangeRequest := &installer.V2UpdateHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
			HostUpdateParams: &models.HostUpdateParams{
				HostName: &hostname,
			},
		}
		_, hostnameUpdateError := userBMClient.Installer.V2UpdateHost(ctx, hostnameChangeRequest)
		Expect(hostnameUpdateError).NotTo(HaveOccurred())
	})

	It("host CRUD", func() {
		host := &registerHost(*infraEnvID).Host
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(*host.Status).Should(Equal("discovering"))
		Expect(host.StatusUpdatedAt).ShouldNot(Equal(strfmt.DateTime(time.Time{})))

		list, err := userBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		list, err = agentBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		list, err = userBMClient.Installer.V2ListHosts(ctx, &installer.V2ListHostsParams{InfraEnvID: *infraEnvID})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = userBMClient.Installer.V2GetHost(ctx, &installer.V2GetHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *host.ID,
		})
		Expect(err).Should(HaveOccurred())
	})

	It("should update host installation disk id successfully", func() {
		host := &registerHost(*infraEnvID).Host
		host = getHostV2(*infraEnvID, *host.ID)
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

		updatedHost, updateError := userBMClient.Installer.V2UpdateHost(ctx, diskSelectionRequest)
		Expect(updateError).NotTo(HaveOccurred())

		host = updatedHost.Payload
		Expect(host.InstallationDiskID).To(Equal(inventory.Disks[1].ID))
		Expect(host.InstallationDiskPath).To(Equal(common.GetDeviceFullName(inventory.Disks[1])))
	})

	It("next step", func() {
		_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterID: clusterID,
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
				VipDhcpAllocation: swag.Bool(false),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		host := &registerHost(*infraEnvID).Host
		host2 := &registerHost(*infraEnvID).Host
		Expect(db.Model(host2).UpdateColumns(&models.Host{Inventory: defaultInventory(),
			Status:             swag.String(models.HostStatusInsufficient),
			InstallationDiskID: "wwn-0x1111111111111111111111"}).Error).NotTo(HaveOccurred())
		steps := getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeFalse())
		Expect(db.Save(&models.APIVip{IP: "1.2.3.4", ClusterID: clusterID}).Error).ToNot(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "known").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "disabled").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(steps.NextInstructionSeconds).Should(Equal(int64(120)))
		Expect(*steps.PostStepAction).Should(Equal(models.StepsPostStepActionContinue))
		Expect(len(steps.Instructions)).Should(Equal(0))
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(db.Model(host).Update("status", "error").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeStopInstallation)).Should(BeTrue())
		Expect(db.Model(host).Update("status", models.HostStatusResetting).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(len(steps.Instructions)).Should(Equal(0))
		Expect(db.Model(cluster.GetPayload()).Update("status", models.ClusterStatusPreparingForInstallation).Error).NotTo(HaveOccurred())
		Expect(db.Model(host2).Update("status", models.HostStatusPreparingForInstallation).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host2.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInstallationDiskSpeedCheck)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeContainerImageAvailability)).Should(BeTrue())
	})

	It("next step - DHCP", func() {
		By("Creating cluster")
		Expect(db.Save(&models.MachineNetwork{ClusterID: clusterID, Cidr: "1.2.3.0/24"}).Error).ToNot(HaveOccurred())
		By("Creating hosts")
		host := &registerHost(*infraEnvID).Host
		host2 := &registerHost(*infraEnvID).Host
		Expect(db.Model(host2).UpdateColumns(&models.Host{Inventory: defaultInventory(),
			Status: swag.String(models.HostStatusInsufficient)}).Error).NotTo(HaveOccurred())
		By("Get steps in discovering ...")
		steps := getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		host = getHostV2(*infraEnvID, *host.ID)
		By("Get steps in insufficient ...")
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeFalse())
		Expect(db.Save(&models.APIVip{IP: "1.2.3.4", ClusterID: clusterID}).Error).ToNot(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeInventory)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		By("Get steps in known ...")
		Expect(db.Model(host).Update("status", "known").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeFreeNetworkAddresses)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeVerifyVips)).Should(BeTrue())
		By("Get steps in disabled ...")
		Expect(db.Model(host).Update("status", "disabled").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(steps.NextInstructionSeconds).Should(Equal(int64(120)))
		Expect(len(steps.Instructions)).Should(Equal(0))
		By("Get steps in insufficient ...")
		Expect(db.Model(host).Update("status", "insufficient").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeConnectivityCheck)).Should(BeTrue())
		Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
		By("Get steps in error ...")
		Expect(db.Model(host).Update("status", "error").Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeStopInstallation)).Should(BeTrue())
		By("Get steps in resetting ...")
		Expect(db.Model(host).Update("status", models.HostStatusResetting).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(len(steps.Instructions)).Should(Equal(0))
		for _, st := range []string{models.HostStatusInstalling, models.HostStatusPreparingForInstallation} {
			By(fmt.Sprintf("Get steps in %s ...", st))
			Expect(db.Model(host).Update("status", st).Error).NotTo(HaveOccurred())
			steps = getNextSteps(*infraEnvID, *host.ID)
			Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
		}
		By(fmt.Sprintf("Get steps in %s ...", models.HostStatusInstallingInProgress))
		Expect(db.Model(host).Updates(map[string]interface{}{"status": models.HostStatusInstallingInProgress, "progress_stage_updated_at": strfmt.DateTime(time.Now())}).Error).NotTo(HaveOccurred())
		steps = getNextSteps(*infraEnvID, *host.ID)
		Expect(isStepTypeInList(steps, models.StepTypeDhcpLeaseAllocate)).Should(BeTrue())
	})

	It("host_disconnection", func() {
		host := &registerHost(*infraEnvID).Host
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("CheckedInAt", strfmt.DateTime(time.Time{})).Error).NotTo(HaveOccurred())

		host = getHostV2(*infraEnvID, *host.ID)
		time.Sleep(time.Second * 3)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("error"))
		Expect(swag.StringValue(host.StatusInfo)).Should(Equal("Host failed to install due to timeout while connecting to host during the installation phase."))
	})

	It("host installation progress", func() {
		host := &registerHost(*infraEnvID).Host
		bootstrapStages := serviceHost.BootstrapStages[:]
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "master").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("bootstrap", "true").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())

		updateProgress(*host.ID, host.InfraEnvID, models.HostStageStartingInstallation)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageStartingInstallation))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageInstalling)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageInstalling))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageWritingImageToDisk)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageWritingImageToDisk))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageRebooting)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageRebooting))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageConfiguring)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageConfiguring))
		time.Sleep(time.Second * 3)
		updateProgress(*host.ID, host.InfraEnvID, models.HostStageDone)
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.ProgressStages).Should(Equal(bootstrapStages))
		Expect(host.Progress.CurrentStage).Should(Equal(models.HostStageDone))
		time.Sleep(time.Second * 3)
	})

	It("installation_error_reply", func() {
		host := &registerHost(*infraEnvID).Host
		Expect(db.Model(host).Update("status", "installing").Error).NotTo(HaveOccurred())
		Expect(db.Model(host).UpdateColumn("inventory", defaultInventory()).Error).NotTo(HaveOccurred())
		Expect(db.Model(host).Update("role", "worker").Error).NotTo(HaveOccurred())

		_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(swag.StringValue(host.Status)).Should(Equal("error"))
		Expect(swag.StringValue(host.StatusInfo)).Should(Equal("installation command failed"))

	})

	It("connectivity_report_store_only_relevant_reply", func() {
		host := &registerHost(*infraEnvID).Host

		connectivity := "{\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null}]}"
		extraConnectivity := "{\"extra\":\"data\",\"remote_hosts\":[{\"host_id\":\"b8a1228d-1091-4e79-be66-738a160f9ff7\",\"l2_connectivity\":null,\"l3_connectivity\":null}]}"

		_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

		//exit code is not 0
		_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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
		host = getHostV2(*infraEnvID, *host.ID)
		Expect(host.Connectivity).Should(Equal(connectivity))

	})

	Context("image availability", func() {

		var (
			h           *models.Host
			imageStatus *models.ContainerImageAvailability
		)

		BeforeEach(func() {
			h = &registerHost(*infraEnvID).Host
		})

		getHostImageStatus := func(hostID strfmt.UUID, imageName string) *models.ContainerImageAvailability {
			hostInDb := getHostV2(*infraEnvID, hostID)

			var hostImageStatuses map[string]*models.ContainerImageAvailability
			Expect(json.Unmarshal([]byte(hostInDb.ImagesStatus), &hostImageStatuses)).ShouldNot(HaveOccurred())

			return hostImageStatuses[imageName]
		}

		It("First success good bandwidth", func() {
			By("pull success", func() {
				imageStatus = common.TestImageStatusesSuccess

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
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

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})

			By("network fixed", func() {
				newImageStatus := &models.ContainerImageAvailability{
					Name:   imageStatus.Name,
					Result: models.ContainerImageAvailabilityResultSuccess,
				}

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
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

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
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

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
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

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
			})
		})

		It("First failure", func() {
			By("pull failed", func() {
				imageStatus = common.TestImageStatusesFailure

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{imageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(imageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationFailure), models.HostValidationIDContainerImagesAvailable)
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

				generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{newImageStatus})
				Expect(getHostImageStatus(*h.ID, imageStatus.Name)).Should(Equal(expectedImageStatus))
				waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, string(serviceHost.ValidationSuccess), models.HostValidationIDContainerImagesAvailable)
			})
		})
	})

	It("register_same_host_id", func() {
		hostID := strToUUID(uuid.New().String())
		// register to cluster1
		_, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		cluster2, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("another-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		infraEnvID2 := registerInfraEnv(cluster2.GetPayload().ID, models.ImageTypeMinimalIso).ID

		// register to cluster2
		_, err = agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID2,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// successfully get from both clusters
		_ = getHostV2(*infraEnvID, *hostID)
		h2 := getHostV2(*infraEnvID2, *hostID)
		h2initialRegistrationTimestamp := h2.RegisteredAt

		_, err = userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
			InfraEnvID: *infraEnvID,
			HostID:     *hostID,
		})
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(time.Second * 2)
		_, err = agentBMClient.Installer.V2RegisterHost(ctx, &installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvID2,
			NewHostParams: &models.HostCreateParams{
				HostID: hostID,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// confirm if new registration updated the timestamp
		h2 = getHostV2(*infraEnvID2, *hostID)
		h2newRegistrationTimestamp := h2.RegisteredAt
		Expect(h2newRegistrationTimestamp.Equal(h2initialRegistrationTimestamp)).Should(BeFalse())

		Eventually(func() string {
			h := getHostV2(*infraEnvID2, *hostID)
			return swag.StringValue(h.Status)
		}, "30s", "1s").Should(Equal(models.HostStatusDiscovering))
	})

	It("register_wrong_pull_secret", func() {
		if Options.AuthType == auth.TypeNone {
			Skip("auth is disabled")
		}

		wrongTokenStubID, err := wiremock.createWrongStubTokenAuth(WrongPullSecret)
		Expect(err).ToNot(HaveOccurred())

		hostID := strToUUID(uuid.New().String())
		_, err = badAgentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
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
		registration := registerHost(*infraEnvID)
		Expect(registration.NextStepRunnerCommand).ShouldNot(BeNil())
		Expect(registration.NextStepRunnerCommand.Command).Should(BeEmpty())
		Expect(registration.NextStepRunnerCommand.Args).ShouldNot(BeEmpty())
		Expect(registration.NextStepRunnerCommand.RetrySeconds).Should(Equal(int64(0))) //default, just to have in the API
	})
})

func updateInventory(ctx context.Context, infraEnvId strfmt.UUID, hostId strfmt.UUID, inventory string) *models.Host {
	_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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
	host := getHostV2(infraEnvId, hostId)
	Expect(host).NotTo(BeNil())
	Expect(host.Inventory).NotTo(BeEmpty())
	return host
}
