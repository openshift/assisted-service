package utils_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	operatorsClient "github.com/openshift/assisted-service/client/operators"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/wait"
)

type SubsystemTestContext struct {
	log                          *logrus.Logger
	db                           *gorm.DB
	AgentBMClient                *client.AssistedInstall
	UserBMClient                 *client.AssistedInstall
	Agent2BMClient               *client.AssistedInstall
	User2BMClient                *client.AssistedInstall
	ReadOnlyAdminUserBMClient    *client.AssistedInstall
	UnallowedUserBMClient        *client.AssistedInstall
	EditclusterUserBMClient      *client.AssistedInstall
	BadAgentBMClient             *client.AssistedInstall
	pollDefaultInterval          time.Duration
	pollDefaultTimeout           time.Duration
	vipAutoAllocOpenshiftVersion string
}

func NewSubsystemTestContext(
	log *logrus.Logger,
	db *gorm.DB,
	agentBMClient *client.AssistedInstall,
	userBMClient *client.AssistedInstall,
	agent2BMClient *client.AssistedInstall,
	user2BMClient *client.AssistedInstall,
	readOnlyAdminUserBMClient *client.AssistedInstall,
	unallowedUserBMClient *client.AssistedInstall,
	editclusterUserBMClient *client.AssistedInstall,
	badAgentBMClient *client.AssistedInstall,
	pollDefaultInterval time.Duration,
	pollDefaultTimeout time.Duration,
	vipAutoAllocOpenshiftVersion string,
) *SubsystemTestContext {
	return &SubsystemTestContext{
		log:                          log,
		db:                           db,
		AgentBMClient:                agentBMClient,
		UserBMClient:                 userBMClient,
		Agent2BMClient:               agent2BMClient,
		User2BMClient:                user2BMClient,
		ReadOnlyAdminUserBMClient:    readOnlyAdminUserBMClient,
		UnallowedUserBMClient:        unallowedUserBMClient,
		EditclusterUserBMClient:      editclusterUserBMClient,
		BadAgentBMClient:             badAgentBMClient,
		pollDefaultInterval:          pollDefaultInterval,
		pollDefaultTimeout:           pollDefaultTimeout,
		vipAutoAllocOpenshiftVersion: vipAutoAllocOpenshiftVersion,
	}
}

func (t *SubsystemTestContext) GetDB() *gorm.DB {
	return t.db
}

func (t *SubsystemTestContext) RegisterHost(infraEnvID strfmt.UUID) *models.HostRegistrationResponse {
	uuid := StrToUUID(uuid.New().String())
	return t.RegisterHostByUUID(infraEnvID, *uuid)
}

func (t *SubsystemTestContext) RegisterHostByUUID(infraEnvID, hostID strfmt.UUID) *models.HostRegistrationResponse {
	host, err := t.AgentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
		InfraEnvID: infraEnvID,
		NewHostParams: &models.HostCreateParams{
			HostID: &hostID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func (t *SubsystemTestContext) GenerateEssentialHostStepsWithInventory(ctx context.Context, h *models.Host, name string, inventory *models.Inventory) {
	t.GenerateGetNextStepsWithTimestamp(ctx, h, time.Now().Unix())
	t.GenerateHWPostStepReply(ctx, h, inventory, name)
	t.GenerateFAPostStepReply(ctx, h, ValidFreeAddresses)
	t.GenerateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
	t.GenerateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionsSuccess)
}

func (t *SubsystemTestContext) GenerateGetNextStepsWithTimestamp(ctx context.Context, h *models.Host, timestamp int64) {
	_, err := t.AgentBMClient.Installer.V2GetNextSteps(ctx, &installer.V2GetNextStepsParams{
		HostID:                *h.ID,
		InfraEnvID:            h.InfraEnvID,
		DiscoveryAgentVersion: swag.String("quay.io/edge-infrastructure/assisted-installer-agent:latest"),
		Timestamp:             &timestamp,
	})
	Expect(err).ToNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateHWPostStepReply(ctx context.Context, h *models.Host, hwInfo *models.Inventory, hostname string) {
	hwInfo.Hostname = hostname
	hw, err := json.Marshal(&hwInfo)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(hw),
			StepID:   string(models.StepTypeInventory),
			StepType: models.StepTypeInventory,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateFAPostStepReply(ctx context.Context, h *models.Host, freeAddresses models.FreeNetworksAddresses) {
	fa, err := json.Marshal(&freeAddresses)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(fa),
			StepID:   string(models.StepTypeFreeNetworkAddresses),
			StepType: models.StepTypeFreeNetworkAddresses,
		},
	})
	Expect(err).To(BeNil())
}

func (t *SubsystemTestContext) GenerateNTPPostStepReply(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource) {
	response := models.NtpSynchronizationResponse{
		NtpSources: ntpSources,
	}

	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(bytes),
			StepID:   string(models.StepTypeNtpSynchronizer),
			StepType: models.StepTypeNtpSynchronizer,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateDomainNameResolutionReply(ctx context.Context, h *models.Host, domainNameResolution models.DomainResolutionResponse) {
	dnsResolotion, err := json.Marshal(&domainNameResolution)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(dnsResolotion),
			StepID:   string(models.StepTypeDomainResolution),
			StepType: models.StepTypeDomainResolution,
		},
	})
	Expect(err).To(BeNil())
}

func (t *SubsystemTestContext) GenerateEssentialPrepareForInstallationSteps(ctx context.Context, hosts ...*models.Host) {
	t.GenerateSuccessfulDiskSpeedResponses(ctx, SdbId, hosts...)
	for _, h := range hosts {
		t.GenerateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{common.TestImageStatusesSuccess})
	}
}

func (t *SubsystemTestContext) GenerateSuccessfulDiskSpeedResponses(ctx context.Context, path string, hosts ...*models.Host) {
	for _, h := range hosts {
		t.GenerateDiskSpeedChekResponse(ctx, h, path, 0)
	}
}

func (t *SubsystemTestContext) GenerateDiskSpeedChekResponse(ctx context.Context, h *models.Host, path string, exitCode int64) {
	result := models.DiskSpeedCheckResponse{
		IoSyncDuration: 10,
		Path:           path,
	}
	b, err := json.Marshal(&result)
	Expect(err).ToNot(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: exitCode,
			Output:   string(b),
			StepID:   string(models.StepTypeInstallationDiskSpeedCheck),
			StepType: models.StepTypeInstallationDiskSpeedCheck,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateContainerImageAvailabilityPostStepReply(ctx context.Context, h *models.Host, imageStatuses []*models.ContainerImageAvailability) {
	response := models.ContainerImageAvailabilityResponse{
		Images: imageStatuses,
	}

	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(bytes),
			StepID:   string(models.StepTypeContainerImageAvailability),
			StepType: models.StepTypeContainerImageAvailability,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateConnectivityPostStepReply(ctx context.Context, h *models.Host, connectivityReport *models.ConnectivityReport) {
	fa, err := json.Marshal(connectivityReport)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(fa),
			StepID:   string(models.StepTypeConnectivityCheck),
			StepType: models.StepTypeConnectivityCheck,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateVerifyVipsPostStepReply(ctx context.Context, h *models.Host, apiVips []string, ingressVips []string, verification models.VipVerification) {
	response := models.VerifyVipsResponse{}
	for _, vip := range apiVips {
		response = append(response, &models.VerifiedVip{
			Verification: common.VipVerificationPtr(verification),
			Vip:          models.IP(vip),
			VipType:      models.VipTypeAPI,
		})
	}
	for _, vip := range ingressVips {
		response = append(response, &models.VerifiedVip{
			Verification: common.VipVerificationPtr(verification),
			Vip:          models.IP(vip),
			VipType:      models.VipTypeIngress,
		})
	}
	bytes, jsonErr := json.Marshal(&response)
	Expect(jsonErr).NotTo(HaveOccurred())
	_, err := t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			StepType: models.StepTypeVerifyVips,
			Output:   string(bytes),
			StepID:   string(models.StepTypeVerifyVips),
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateDomainResolution(ctx context.Context, h *models.Host, name string, baseDomain string) {
	reply := common.CreateWildcardDomainNameResolutionReply(name, baseDomain)
	reply.Resolutions = append(reply.Resolutions, &models.DomainResolutionResponseDomain{
		DomainName:    swag.String("quay.io"),
		IPV4Addresses: []strfmt.IPv4{"7.8.9.11/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::11/120"},
	})
	t.GenerateDomainNameResolutionReply(ctx, h, *reply)
}

func (t *SubsystemTestContext) RegisterNode(ctx context.Context, infraenvID strfmt.UUID, name, ip string) *models.Host {
	h := &t.RegisterHost(infraenvID).Host
	t.GenerateEssentialHostSteps(ctx, h, name, ip)
	t.GenerateEssentialPrepareForInstallationSteps(ctx, h)
	return h
}

func (t *SubsystemTestContext) RegisterNodeWithInventory(ctx context.Context, infraEnvID strfmt.UUID, name, ip string, inventory *models.Inventory) *models.Host {
	h := &t.RegisterHost(infraEnvID).Host
	hwInfo := inventory
	hwInfo.Interfaces[0].IPV4Addresses = []string{ip}
	t.GenerateEssentialHostStepsWithInventory(ctx, h, name, hwInfo)
	t.GenerateEssentialPrepareForInstallationSteps(ctx, h)
	return h
}

func (t *SubsystemTestContext) GenerateEssentialHostSteps(ctx context.Context, h *models.Host, name, cidr string) {
	t.GenerateEssentialHostStepsWithInventory(ctx, h, name, GetDefaultInventory(cidr))
}

func (t *SubsystemTestContext) GenerateFullMeshConnectivity(ctx context.Context, startCIDR string, hosts ...*models.Host) {

	ip, _, err := net.ParseCIDR(startCIDR)
	Expect(err).NotTo(HaveOccurred())
	hostToAddr := make(map[strfmt.UUID]string)

	for _, h := range hosts {
		hostToAddr[*h.ID] = ip.String()
		common.IncrementIP(ip)
	}

	var connectivityReport models.ConnectivityReport
	for _, h := range hosts {

		l2Connectivity := make([]*models.L2Connectivity, 0)
		l3Connectivity := make([]*models.L3Connectivity, 0)
		for id, addr := range hostToAddr {

			if id != *h.ID {
				continue
			}

			l2Connectivity = append(l2Connectivity, &models.L2Connectivity{
				RemoteIPAddress: addr,
				Successful:      true,
			})
			l3Connectivity = append(l3Connectivity, &models.L3Connectivity{
				RemoteIPAddress: addr,
				Successful:      true,
			})
		}

		connectivityReport.RemoteHosts = append(connectivityReport.RemoteHosts, &models.ConnectivityRemoteHost{
			HostID:         *h.ID,
			L2Connectivity: l2Connectivity,
			L3Connectivity: l3Connectivity,
		})
	}

	for _, h := range hosts {
		t.GenerateConnectivityPostStepReply(ctx, h, &connectivityReport)
	}
}

func (t *SubsystemTestContext) GenerateCommonDomainReply(ctx context.Context, h *models.Host, clusterName, baseDomain string) {
	fqdn := func(domainPrefix, clusterName, baseDomain string) *string {
		return swag.String(fmt.Sprintf("%s.%s.%s", domainPrefix, clusterName, baseDomain))
	}
	var domainResolutions = []*models.DomainResolutionResponseDomain{
		{
			DomainName:    fqdn(constants.APIClusterSubdomain, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"1.2.3.4/24"},
			IPV6Addresses: []strfmt.IPv6{"1001:db8::10/120"},
		},
		{
			DomainName:    fqdn(constants.InternalAPIClusterSubdomain, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"4.5.6.7/24"},
			IPV6Addresses: []strfmt.IPv6{"1002:db8::10/120"},
		},
		{
			DomainName:    fqdn(constants.AppsSubDomainNameHostDNSValidation+".apps", clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
			IPV6Addresses: []strfmt.IPv6{"1003:db8::10/120"},
		},
		{
			DomainName:    swag.String("quay.io"),
			IPV4Addresses: []strfmt.IPv4{"7.8.9.11/24"},
			IPV6Addresses: []strfmt.IPv6{"1003:db8::11/120"},
		},
		{
			DomainName:    fqdn(constants.DNSWildcardFalseDomainName, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{},
			IPV6Addresses: []strfmt.IPv6{},
		},
		{
			DomainName:    fqdn(constants.DNSWildcardFalseDomainName, clusterName, baseDomain+"."),
			IPV4Addresses: []strfmt.IPv4{},
			IPV6Addresses: []strfmt.IPv6{},
		},
	}
	var domainResolutionResponse = models.DomainResolutionResponse{
		Resolutions: domainResolutions,
	}
	t.GenerateDomainNameResolutionReply(ctx, h, domainResolutionResponse)
}

func (t *SubsystemTestContext) CompleteInstallation(clusterID strfmt.UUID) {
	ctx := context.Background()
	rep, err := t.AgentBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: clusterID})
	Expect(err).NotTo(HaveOccurred())

	status := models.OperatorStatusAvailable

	Eventually(func() error {
		_, err = t.AgentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: models.IngressCertParams(IngressCa),
		})
		return err
	}, "10s", "2s").Should(BeNil())

	for _, operator := range rep.Payload.MonitoredOperators {
		if operator.OperatorType != models.OperatorTypeBuiltin {
			continue
		}

		t.V2ReportMonitoredOperatorStatus(ctx, clusterID, operator.Name, status, "")
	}
}

func (t *SubsystemTestContext) V2ReportMonitoredOperatorStatus(ctx context.Context, clusterID strfmt.UUID, opName string, opStatus models.OperatorStatus, opVersion string) {
	_, err := t.AgentBMClient.Operators.V2ReportMonitoredOperatorStatus(ctx, &operatorsClient.V2ReportMonitoredOperatorStatusParams{
		ClusterID: clusterID,
		ReportParams: &models.OperatorMonitorReport{
			Name:       opName,
			Status:     opStatus,
			StatusInfo: string(opStatus),
			Version:    opVersion,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

func (t *SubsystemTestContext) UpdateProgress(hostID strfmt.UUID, infraEnvID strfmt.UUID, current_step models.HostStage) {
	t.UpdateHostProgressWithInfo(hostID, infraEnvID, current_step, "")
}

func (t *SubsystemTestContext) UpdateHostProgressWithInfo(hostID strfmt.UUID, infraEnvID strfmt.UUID, current_step models.HostStage, info string) {
	ctx := context.Background()

	installProgress := &models.HostProgress{
		CurrentStage: current_step,
		ProgressInfo: info,
	}
	updateReply, err := t.AgentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
		InfraEnvID:   infraEnvID,
		HostProgress: installProgress,
		HostID:       hostID,
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostInstallProgressOK()))
}

func (t *SubsystemTestContext) GenerateApiVipPostStepReply(ctx context.Context, h *models.Host, cluster *models.Cluster, success bool) {
	checkVipApiResponse := models.APIVipConnectivityResponse{
		IsSuccess: success,
	}
	if cluster != nil && swag.StringValue(cluster.Status) == models.ClusterStatusAddingHosts {
		checkVipApiResponse.Ignition = `{
			"ignition": {
			  "config": {},
			  "version": "3.2.0"
			},
			"storage": {
			  "files": []
			}
		  }`
	}
	bytes, jsonErr := json.Marshal(checkVipApiResponse)
	Expect(jsonErr).NotTo(HaveOccurred())
	_, err := t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			StepType: models.StepTypeAPIVipConnectivityCheck,
			Output:   string(bytes),
			StepID:   "apivip-connectivity-check-step",
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GetDefaultNutanixInventory(cidr string) *models.Inventory {
	nutanixInventory := *GetDefaultInventory(cidr)
	nutanixInventory.SystemVendor = &models.SystemVendor{Manufacturer: "Nutanix", ProductName: "AHV", Virtual: true, SerialNumber: "3534"}
	nutanixInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &nutanixInventory
}

func (t *SubsystemTestContext) GetDefaultExternalInventory(cidr string) *models.Inventory {
	externalInventory := *GetDefaultInventory(cidr)
	externalInventory.SystemVendor = &models.SystemVendor{Manufacturer: "OracleCloud.com", ProductName: "OCI", Virtual: true, SerialNumber: "3534"}
	externalInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &externalInventory
}

func (t *SubsystemTestContext) BindHost(infraEnvID, hostID, clusterID strfmt.UUID) *models.Host {
	host, err := t.UserBMClient.Installer.BindHost(context.Background(), &installer.BindHostParams{
		HostID:     hostID,
		InfraEnvID: infraEnvID,
		BindHostParams: &models.BindHostParams{
			ClusterID: &clusterID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func (t *SubsystemTestContext) UnbindHost(infraEnvID, hostID strfmt.UUID) *models.Host {
	host, err := t.UserBMClient.Installer.UnbindHost(context.Background(), &installer.UnbindHostParams{
		HostID:     hostID,
		InfraEnvID: infraEnvID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func (t *SubsystemTestContext) GetHostV2(infraEnvID, hostID strfmt.UUID) *models.Host {
	host, err := t.UserBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
		InfraEnvID: infraEnvID,
		HostID:     hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func (t *SubsystemTestContext) GetCluster(clusterID strfmt.UUID) *models.Cluster {
	cluster, err := t.UserBMClient.Installer.V2GetCluster(context.Background(), &installer.V2GetClusterParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	return cluster.GetPayload()
}

func (t *SubsystemTestContext) GetCommonCluster(ctx context.Context, clusterID strfmt.UUID) *common.Cluster {
	var cluster common.Cluster
	err := t.db.First(&cluster, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	return &cluster
}

func (t *SubsystemTestContext) WaitForLastInstallationCompletionStatus(clusterID strfmt.UUID, status string) {
	waitFunc := func(ctx context.Context) (bool, error) {
		c := t.GetCommonCluster(ctx, clusterID)
		return c.LastInstallationPreparation.Status == status, nil
	}
	err := wait.PollUntilContextTimeout(context.Background(), t.pollDefaultInterval, t.pollDefaultTimeout, false, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}

func (t *SubsystemTestContext) GetNextSteps(infraEnvID, hostID strfmt.UUID) models.Steps {
	steps, err := t.AgentBMClient.Installer.V2GetNextSteps(context.Background(), &installer.V2GetNextStepsParams{
		InfraEnvID:            infraEnvID,
		HostID:                hostID,
		DiscoveryAgentVersion: swag.String("quay.io/edge-infrastructure/assisted-installer-agent:latest"),
	})
	Expect(err).NotTo(HaveOccurred())
	return *steps.GetPayload()
}

func (t *SubsystemTestContext) UpdateClusterLogProgress(clusterID strfmt.UUID, progress models.LogsState) {
	ctx := context.Background()

	updateReply, err := t.AgentBMClient.Installer.V2UpdateClusterLogsProgress(ctx, &installer.V2UpdateClusterLogsProgressParams{
		ClusterID: clusterID,
		LogsProgressParams: &models.LogsProgressParams{
			LogsState: common.LogStatePtr(progress),
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterLogsProgressNoContent()))
}

func (t *SubsystemTestContext) UpdateHostLogProgress(infraEnvID strfmt.UUID, hostID strfmt.UUID, progress models.LogsState) {
	ctx := context.Background()

	updateReply, err := t.AgentBMClient.Installer.V2UpdateHostLogsProgress(ctx, &installer.V2UpdateHostLogsProgressParams{
		InfraEnvID: infraEnvID,
		HostID:     hostID,
		LogsProgressParams: &models.LogsProgressParams{
			LogsState: common.LogStatePtr(progress),
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostLogsProgressNoContent()))
}

func (t *SubsystemTestContext) GenerateConnectivityCheckPostStepReply(ctx context.Context, h *models.Host, targetCIDR string, success bool) {
	targetIP, _, err := net.ParseCIDR(targetCIDR)
	Expect(err).NotTo(HaveOccurred())
	response := models.ConnectivityReport{
		RemoteHosts: []*models.ConnectivityRemoteHost{
			{L3Connectivity: []*models.L3Connectivity{{RemoteIPAddress: targetIP.String(), Successful: success}}},
		},
	}
	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
		InfraEnvID: h.InfraEnvID,
		HostID:     *h.ID,
		Reply: &models.StepReply{
			ExitCode: 0,
			Output:   string(bytes),
			StepID:   string(models.StepTypeConnectivityCheck),
			StepType: models.StepTypeConnectivityCheck,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) GenerateTangPostStepReply(ctx context.Context, success bool, hosts ...*models.Host) {
	response := models.TangConnectivityResponse{
		IsSuccess:          false,
		TangServerResponse: nil,
	}

	if success {
		tangResponse := getTangResponse("http://tang.example.com:7500")
		response = models.TangConnectivityResponse{
			IsSuccess:          true,
			TangServerResponse: []*models.TangServerResponse{&tangResponse},
		}
	}

	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())

	for _, h := range hosts {
		_, err = t.AgentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
			InfraEnvID: h.InfraEnvID,
			HostID:     *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   string(bytes),
				StepID:   string(models.StepTypeTangConnectivityCheck),
				StepType: models.StepTypeTangConnectivityCheck,
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}
}

func (t *SubsystemTestContext) GenerateFailedDiskSpeedResponses(ctx context.Context, path string, hosts ...*models.Host) {
	for _, h := range hosts {
		t.GenerateDiskSpeedChekResponse(ctx, h, path, -1)
	}
}

func (t *SubsystemTestContext) UpdateVipParams(ctx context.Context, clusterID strfmt.UUID) {
	apiVip := "1.2.3.5"
	ingressVip := "1.2.3.6"
	_, err := t.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
			IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
		},
		ClusterID: clusterID,
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) V2UpdateVipParams(ctx context.Context, clusterID strfmt.UUID) {
	apiVip := "1.2.3.5"
	ingressVip := "1.2.3.6"
	_, err := t.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVips:           []*models.APIVip{{IP: models.IP(apiVip), ClusterID: clusterID}},
			IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip), ClusterID: clusterID}},
		},
		ClusterID: clusterID,
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *SubsystemTestContext) Register3nodes(ctx context.Context, clusterID, infraenvID strfmt.UUID, cidr string) ([]*models.Host, []string) {
	ips := hostutil.GenerateIPv4Addresses(3, cidr)
	h1 := t.RegisterNode(ctx, infraenvID, "h1", ips[0])
	h2 := t.RegisterNode(ctx, infraenvID, "h2", ips[1])
	h3 := t.RegisterNode(ctx, infraenvID, "h3", ips[2])
	t.UpdateVipParams(ctx, clusterID)
	t.GenerateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)

	return []*models.Host{h1, h2, h3}, ips
}

func (t *SubsystemTestContext) GetDefaultVmwareInventory(cidr string) *models.Inventory {
	vmwareInventory := *GetDefaultInventory(cidr)
	vmwareInventory.SystemVendor = &models.SystemVendor{Manufacturer: "VMware, Inc.", ProductName: "VMware Virtual", Virtual: true, SerialNumber: "3534"}
	vmwareInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &vmwareInventory
}

func (t *SubsystemTestContext) RegisterCluster(ctx context.Context, client *client.AssistedInstall, clusterName string, pullSecret string) (strfmt.UUID, error) {
	var cluster, err = client.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
		NewClusterParams: &models.ClusterCreateParams{
			Name:             swag.String(clusterName),
			OpenshiftVersion: swag.String(t.vipAutoAllocOpenshiftVersion),
			PullSecret:       swag.String(pullSecret),
			BaseDNSDomain:    "example.com",
		},
	})
	if err != nil {
		return "", err
	}
	return *cluster.GetPayload().ID, nil
}

type HostValidationResult struct {
	ID      models.HostValidationID `json:"id"`
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
}

type ClusterValidationResult struct {
	ID      models.ClusterValidationID `json:"id"`
	Status  string                     `json:"status"`
	Message string                     `json:"message"`
}

func (t *SubsystemTestContext) IsHostValidationInStatus(clusterID, infraEnvID, hostID strfmt.UUID, validationID models.HostValidationID, expectedStatus string) (bool, error) {
	var validationRes map[string][]HostValidationResult
	h := t.GetHostV2(infraEnvID, hostID)
	if h.ValidationsInfo == "" {
		return false, nil
	}
	err := json.Unmarshal([]byte(h.ValidationsInfo), &validationRes)
	Expect(err).ShouldNot(HaveOccurred())
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.ID != validationID {
				continue
			}
			return v.Status == expectedStatus, nil
		}
	}
	return false, nil
}

func (t *SubsystemTestContext) IsClusterValidationInStatus(clusterID strfmt.UUID, validationID models.ClusterValidationID, expectedStatus string) (bool, error) {
	var validationRes map[string][]ClusterValidationResult
	c := t.GetCluster(clusterID)
	if c.ValidationsInfo == "" {
		return false, nil
	}
	err := json.Unmarshal([]byte(c.ValidationsInfo), &validationRes)
	Expect(err).ShouldNot(HaveOccurred())
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.ID != validationID {
				continue
			}
			return v.Status == expectedStatus, nil
		}
	}
	return false, nil
}

func (t *SubsystemTestContext) WaitForHostValidationStatus(clusterID, infraEnvID, hostID strfmt.UUID, expectedStatus string, hostValidationIDs ...models.HostValidationID) {

	waitFunc := func(_ context.Context) (bool, error) {
		for _, vID := range hostValidationIDs {
			cond, _ := t.IsHostValidationInStatus(clusterID, infraEnvID, hostID, vID, expectedStatus)
			if !cond {
				return false, nil
			}
		}
		return true, nil
	}
	err := wait.PollUntilContextTimeout(context.TODO(), t.pollDefaultInterval, t.pollDefaultTimeout, false, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}

func (t *SubsystemTestContext) WaitForClusterValidationStatus(clusterID strfmt.UUID, expectedStatus string, clusterValidationIDs ...models.ClusterValidationID) {

	waitFunc := func(_ context.Context) (bool, error) {
		for _, vID := range clusterValidationIDs {
			cond, _ := t.IsClusterValidationInStatus(clusterID, vID, expectedStatus)
			if !cond {
				return false, nil
			}
		}
		return true, nil
	}
	err := wait.PollUntilContextTimeout(context.TODO(), t.pollDefaultInterval, t.pollDefaultTimeout, false, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}

func (t *SubsystemTestContext) V2RegisterDay2Cluster(ctx context.Context, openshiftVersion string, pullSecret string) strfmt.UUID {
	openshiftClusterID := strfmt.UUID(uuid.New().String())

	c, err := t.UserBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
		NewImportClusterParams: &models.ImportClusterParams{
			Name:               swag.String("test-metrics-day2-cluster"),
			OpenshiftVersion:   openshiftVersion,
			APIVipDnsname:      swag.String("api-vip.redhat.com"),
			OpenshiftClusterID: &openshiftClusterID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	clusterID := *c.GetPayload().ID

	_, err = t.UserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			PullSecret: swag.String(pullSecret),
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	return clusterID
}

func (t *SubsystemTestContext) DeregisterResources() {
	var multiErr *multierror.Error

	reply, err := t.UserBMClient.Installer.V2ListClusters(context.Background(), &installer.V2ListClustersParams{})
	if err != nil {
		t.log.WithError(err).Error("Failed to list clusters")
		return
	}

	if GinkgoT().Failed() {
		// Dump cluster info on failure
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.ClusterKindCluster, reply.Payload))
	}

	infraEnvReply, err := t.UserBMClient.Installer.ListInfraEnvs(context.Background(), &installer.ListInfraEnvsParams{})
	if err != nil {
		t.log.WithError(err).Error("Failed to list infra-envs")
	}

	if GinkgoT().Failed() {
		// Dump infar-env info on failure
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.InfraEnvKindInfraEnv, infraEnvReply.Payload))
	}

	for _, i := range infraEnvReply.GetPayload() {
		if GinkgoT().Failed() {
			hostReply, err1 := t.UserBMClient.Installer.V2ListHosts(context.Background(), &installer.V2ListHostsParams{InfraEnvID: *i.ID})
			if err1 != nil {
				t.log.WithError(err).Errorf("Failed to list infra-env %s (%s) hosts", i.ID, *i.Name)
			}
			// Dump host info on failure
			multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.HostKindHost, hostReply.Payload))
		}
		if _, err = t.UserBMClient.Installer.DeregisterInfraEnv(context.Background(), &installer.DeregisterInfraEnvParams{InfraEnvID: *i.ID}); err != nil {
			t.log.WithError(err).Debugf("InfraEnv %s couldn't be deleted via REST API", i.ID)
		}
	}

	for _, c := range reply.GetPayload() {
		if _, err = t.UserBMClient.Installer.V2DeregisterCluster(context.Background(), &installer.V2DeregisterClusterParams{ClusterID: *c.ID}); err != nil {
			t.log.WithError(err).Debugf("Cluster %s couldn't be deleted via REST API", *c.ID)
		}
	}

	if multiErr.ErrorOrNil() != nil {
		t.log.WithError(err).Error("At-least one error occured during deregister cleanup")
	}
}

func (t *SubsystemTestContext) ClearDB() {
	// Clean the DB to make sure we start tests from scratch
	for _, model := range []interface{}{
		&models.Host{},
		&models.Cluster{},
		&models.InfraEnv{},
		&models.Event{},
		&models.MonitoredOperator{},
		&models.ClusterNetwork{},
		&models.ServiceNetwork{},
		&models.MachineNetwork{},
	} {
		t.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(model)
	}
}
