package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	operatorsClient "github.com/openshift/assisted-service/client/operators"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	usageMgr "github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/util/wait"
)

// #nosec
const (
	sshPublicKey                            = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain"
	pullSecretName                          = "pull-secret"
	pullSecret                              = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}"
	defaultWaitForHostStateTimeout          = 20 * time.Second
	defaultWaitForClusterStateTimeout       = 40 * time.Second
	defaultWaitForMachineNetworkCIDRTimeout = 40 * time.Second
)

func subsystemAfterEach() {
	if Options.EnableKubeAPI {
		printCRs(context.Background(), kubeClient)
		cleanUpCRs(context.Background(), kubeClient)
		verifyCleanUP(context.Background(), kubeClient)
	} else {
		deregisterResources()
	}
	clearDB()
}

func deregisterResources() {
	var multiErr *multierror.Error

	// Delete cluster should use the REST API in order to delete any
	// clusters' resources managed by the service
	reply, err := userBMClient.Installer.ListClusters(context.Background(), &installer.ListClustersParams{})
	Expect(err).To(BeNil())
	if GinkgoT().Failed() {
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.ClusterKindCluster, reply.Payload))
	}
	for _, c := range reply.GetPayload() {
		// DeregisterCluster API isn't necessarily available (e.g. cluster is being installed)
		if _, err = userBMClient.Installer.DeregisterCluster(context.Background(), &installer.DeregisterClusterParams{ClusterID: *c.ID}); err != nil {
			log.WithError(err).Debugf("Cluster %s couldn't be deleted via REST API", *c.ID)
		}
	}
	// Delete infra env
	infraEnvReply, err := userBMClient.Installer.ListInfraEnvs(context.Background(), &installer.ListInfraEnvsParams{})
	Expect(err).To(BeNil())
	if GinkgoT().Failed() {
		multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.InfraEnvKindInfraEnv, infraEnvReply.Payload))
	}
	for _, i := range infraEnvReply.GetPayload() {
		if GinkgoT().Failed() {
			hostReply, err1 := userBMClient.Installer.V2ListHosts(context.Background(), &installer.V2ListHostsParams{InfraEnvID: *i.ID})
			Expect(err1).To(BeNil())
			multiErr = multierror.Append(multiErr, GinkgoResourceLogger(models.HostKindHost, hostReply.Payload))
		}
		if _, err = userBMClient.Installer.DeregisterInfraEnv(context.Background(), &installer.DeregisterInfraEnvParams{InfraEnvID: *i.ID}); err != nil {
			log.WithError(err).Debugf("InfraEnv %s couldn't be deleted via REST API", i.ID)
		}
		// Delete host env
		hostReply, err := userBMClient.Installer.ListHosts(context.Background(), &installer.ListHostsParams{})
		Expect(err).ShouldNot(HaveOccurred())
		for _, h := range hostReply.GetPayload() {
			if _, err = userBMClient.Installer.DeregisterHost(context.Background(), &installer.DeregisterHostParams{HostID: *h.ID}); err != nil {
				log.WithError(err).Debugf("Host %s couldn't be deleted via REST API", h.ID)
			}
		}

	}
	Expect(multiErr.ErrorOrNil()).To(BeNil())
}
func clearDB() {
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
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(model)
	}
}

func GinkgoResourceLogger(kind string, resources interface{}) error {
	resList, err := json.MarshalIndent(resources, "", "  ")
	if err != nil {
		return err
	}
	GinkgoLogger(fmt.Sprintf("The failed test '%s' created the following %s resources:", GinkgoT().Name(), kind))
	GinkgoLogger(string(resList))
	return nil
}

func GinkgoLogger(s string) {
	_, _ = GinkgoWriter.Write([]byte(fmt.Sprintln(s)))
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func registerHost(infraEnvID strfmt.UUID) *models.HostRegistrationResponse {
	uuid := strToUUID(uuid.New().String())
	return registerHostByUUID(infraEnvID, *uuid)
}

func registerHostByUUID(infraEnvID, hostID strfmt.UUID) *models.HostRegistrationResponse {
	host, err := agentBMClient.Installer.V2RegisterHost(context.Background(), &installer.V2RegisterHostParams{
		InfraEnvID: infraEnvID,
		NewHostParams: &models.HostCreateParams{
			HostID: &hostID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func bindHost(infraEnvID, hostID, clusterID strfmt.UUID) *models.Host {
	host, err := userBMClient.Installer.BindHost(context.Background(), &installer.BindHostParams{
		HostID:     hostID,
		InfraEnvID: infraEnvID,
		BindHostParams: &models.BindHostParams{
			ClusterID: &clusterID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func unbindHost(infraEnvID, hostID strfmt.UUID) *models.Host {
	host, err := userBMClient.Installer.UnbindHost(context.Background(), &installer.UnbindHostParams{
		HostID:     hostID,
		InfraEnvID: infraEnvID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func getHost(clusterID, hostID strfmt.UUID) *models.Host {
	host, err := userBMClient.Installer.GetHost(context.Background(), &installer.GetHostParams{
		ClusterID: clusterID,
		HostID:    hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func getHostV2(infraEnvID, hostID strfmt.UUID) *models.Host {
	host, err := userBMClient.Installer.V2GetHost(context.Background(), &installer.V2GetHostParams{
		InfraEnvID: infraEnvID,
		HostID:     hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return host.GetPayload()
}

func generateClusterISO(clusterID strfmt.UUID, imageType models.ImageType) {
	_, err := userBMClient.Installer.GenerateClusterISO(context.Background(), &installer.GenerateClusterISOParams{
		ClusterID: clusterID,
		ImageCreateParams: &models.ImageCreateParams{
			ImageType: imageType,
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

func registerCluster(ctx context.Context, client *client.AssistedInstall, clusterName string, pullSecret string) (strfmt.UUID, error) {
	var cluster, err = client.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
		NewClusterParams: &models.ClusterCreateParams{
			Name:             swag.String(clusterName),
			OpenshiftVersion: swag.String(openshiftVersion),
			PullSecret:       swag.String(pullSecret),
			BaseDNSDomain:    "example.com",
		},
	})
	if err != nil {
		return "", err
	}
	return *cluster.GetPayload().ID, nil
}

func getCluster(clusterID strfmt.UUID) *models.Cluster {
	cluster, err := userBMClient.Installer.GetCluster(context.Background(), &installer.GetClusterParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	return cluster.GetPayload()
}

func getCommonCluster(ctx context.Context, clusterID strfmt.UUID) *common.Cluster {
	var cluster common.Cluster
	err := db.First(&cluster, "id = ?", clusterID).Error
	Expect(err).ShouldNot(HaveOccurred())
	return &cluster
}

func checkStepsInList(steps models.Steps, stepTypes []models.StepType, numSteps int) {
	Expect(len(steps.Instructions)).Should(BeNumerically(">=", numSteps))
	for _, stepType := range stepTypes {
		_, res := getStepInList(steps, stepType)
		Expect(res).Should(Equal(true))
	}
}

func getStepInList(steps models.Steps, sType models.StepType) (*models.Step, bool) {
	for _, step := range steps.Instructions {
		if step.StepType == sType {
			return step, true
		}
	}
	return nil, false
}

func getNextSteps(infraEnvID, hostID strfmt.UUID) models.Steps {
	steps, err := agentBMClient.Installer.V2GetNextSteps(context.Background(), &installer.V2GetNextStepsParams{
		InfraEnvID: infraEnvID,
		HostID:     hostID,
	})
	Expect(err).NotTo(HaveOccurred())
	return *steps.GetPayload()
}

func updateHostLogProgress(clusterID strfmt.UUID, hostID strfmt.UUID, progress models.LogsState) {
	ctx := context.Background()

	updateReply, err := agentBMClient.Installer.UpdateHostLogsProgress(ctx, &installer.UpdateHostLogsProgressParams{
		ClusterID: clusterID,
		HostID:    hostID,
		LogsProgressParams: &models.LogsProgressParams{
			LogsState: common.LogStatePtr(progress),
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewUpdateHostLogsProgressNoContent()))
}

func updateClusterLogProgress(clusterID strfmt.UUID, progress models.LogsState) {
	ctx := context.Background()

	updateReply, err := agentBMClient.Installer.UpdateClusterLogsProgress(ctx, &installer.UpdateClusterLogsProgressParams{
		ClusterID: clusterID,
		LogsProgressParams: &models.LogsProgressParams{
			LogsState: common.LogStatePtr(progress),
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewUpdateClusterLogsProgressNoContent()))
}

func updateProgress(hostID strfmt.UUID, infraEnvID strfmt.UUID, current_step models.HostStage) {
	updateHostProgressWithInfo(hostID, infraEnvID, current_step, "")
}

func updateHostProgressWithInfo(hostID strfmt.UUID, infraEnvID strfmt.UUID, current_step models.HostStage, info string) {
	ctx := context.Background()

	installProgress := &models.HostProgress{
		CurrentStage: current_step,
		ProgressInfo: info,
	}
	updateReply, err := agentBMClient.Installer.V2UpdateHostInstallProgress(ctx, &installer.V2UpdateHostInstallProgressParams{
		InfraEnvID:   infraEnvID,
		HostProgress: installProgress,
		HostID:       hostID,
	})
	Expect(err).ShouldNot(HaveOccurred())
	Expect(updateReply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostInstallProgressOK()))
}

func generateHWPostStepReply(ctx context.Context, h *models.Host, hwInfo *models.Inventory, hostname string) {
	hwInfo.Hostname = hostname
	hw, err := json.Marshal(&hwInfo)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateConnectivityCheckPostStepReply(ctx context.Context, h *models.Host, targetCIDR string, success bool) {
	targetIP, _, err := net.ParseCIDR(targetCIDR)
	Expect(err).NotTo(HaveOccurred())
	response := models.ConnectivityReport{
		RemoteHosts: []*models.ConnectivityRemoteHost{
			{L3Connectivity: []*models.L3Connectivity{{RemoteIPAddress: targetIP.String(), Successful: success}}},
		},
	}
	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateNTPPostStepReply(ctx context.Context, h *models.Host, ntpSources []*models.NtpSource) {
	response := models.NtpSynchronizationResponse{
		NtpSources: ntpSources,
	}

	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateApiVipPostStepReply(ctx context.Context, h *models.Host, success bool) {
	checkVipApiResponse := models.APIVipConnectivityResponse{
		IsSuccess: success,
	}
	bytes, jsonErr := json.Marshal(checkVipApiResponse)
	Expect(jsonErr).NotTo(HaveOccurred())
	_, err := agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateContainerImageAvailabilityPostStepReply(ctx context.Context, h *models.Host, imageStatuses []*models.ContainerImageAvailability) {
	response := models.ContainerImageAvailabilityResponse{
		Images: imageStatuses,
	}

	bytes, err := json.Marshal(&response)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func getDefaultInventory(cidr string) *models.Inventory {
	hwInfo := validHwInfo
	hwInfo.Interfaces[0].IPV4Addresses = []string{cidr}
	return hwInfo
}

func generateEssentialHostSteps(ctx context.Context, h *models.Host, name, cidr string) {
	generateEssentialHostStepsWithInventory(ctx, h, name, getDefaultInventory(cidr))
}

func generateEssentialHostStepsWithInventory(ctx context.Context, h *models.Host, name string, inventory *models.Inventory) {
	generateHWPostStepReply(ctx, h, inventory, name)
	generateFAPostStepReply(ctx, h, validFreeAddresses)
	generateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
	generateDomainNameResolutionReply(ctx, h, *common.TestDomainNameResolutionSuccess)
}

func generateDomainResolution(ctx context.Context, h *models.Host, name string, baseDomain string) {
	generateDomainNameResolutionReply(ctx, h, *common.CreateWildcardDomainNameResolutionReply(name, baseDomain))
}

func generateCommonDomainReply(ctx context.Context, h *models.Host, clusterName, baseDomain string) {
	fqdn := func(domainPrefix, clusterName, baseDomain string) *string {
		return swag.String(fmt.Sprintf("%s.%s.%s", domainPrefix, clusterName, baseDomain))
	}
	var domainResolutions = []*models.DomainResolutionResponseDomain{
		{
			DomainName:    fqdn(constants.APIName, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"1.2.3.4/24"},
			IPV6Addresses: []strfmt.IPv6{"1001:db8::10/120"},
		},
		{
			DomainName:    fqdn(constants.APIInternalName, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"4.5.6.7/24"},
			IPV6Addresses: []strfmt.IPv6{"1002:db8::10/120"},
		},
		{
			DomainName:    fqdn(constants.AppsSubDomainNameHostDNSValidation+".apps", clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
			IPV6Addresses: []strfmt.IPv6{"1003:db8::10/120"},
		},
		{
			DomainName:    fqdn(constants.DNSWildcardFalseDomainName, clusterName, baseDomain),
			IPV4Addresses: []strfmt.IPv4{},
			IPV6Addresses: []strfmt.IPv6{},
		},
	}
	var domainResolutionResponse = models.DomainResolutionResponse{
		Resolutions: domainResolutions,
	}
	generateDomainNameResolutionReply(ctx, h, domainResolutionResponse)
}

func generateEssentialPrepareForInstallationSteps(ctx context.Context, hosts ...*models.Host) {
	generateSuccessfulDiskSpeedResponses(ctx, sdbId, hosts...)
	for _, h := range hosts {
		generateContainerImageAvailabilityPostStepReply(ctx, h, []*models.ContainerImageAvailability{common.TestImageStatusesSuccess})
	}
}

func registerNode(ctx context.Context, clusterID strfmt.UUID, name, ip string) *models.Host {
	h := &registerHost(clusterID).Host
	generateEssentialHostSteps(ctx, h, name, ip)
	generateEssentialPrepareForInstallationSteps(ctx, h)
	return h
}

func registerNodeWithUUID(ctx context.Context, clusterID strfmt.UUID, name, ip, newUUID string) *models.Host {
	h := &registerHostByUUID(clusterID, strfmt.UUID(newUUID)).Host
	generateEssentialHostSteps(ctx, h, name, ip)
	generateEssentialPrepareForInstallationSteps(ctx, h)
	return h
}

func isJSON(s []byte) bool {
	var js map[string]interface{}
	return json.Unmarshal(s, &js) == nil

}

func generateFAPostStepReply(ctx context.Context, h *models.Host, freeAddresses models.FreeNetworksAddresses) {
	fa, err := json.Marshal(&freeAddresses)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateDiskSpeedChekResponse(ctx context.Context, h *models.Host, path string, exitCode int64) {
	result := models.DiskSpeedCheckResponse{
		IoSyncDuration: 10,
		Path:           path,
	}
	b, err := json.Marshal(&result)
	Expect(err).ToNot(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func generateSuccessfulDiskSpeedResponses(ctx context.Context, path string, hosts ...*models.Host) {
	for _, h := range hosts {
		generateDiskSpeedChekResponse(ctx, h, path, 0)
	}
}

func generateFailedDiskSpeedResponses(ctx context.Context, path string, hosts ...*models.Host) {
	for _, h := range hosts {
		generateDiskSpeedChekResponse(ctx, h, path, -1)
	}
}

func generateDomainNameResolutionReply(ctx context.Context, h *models.Host, domainNameResolution models.DomainResolutionResponse) {
	dnsResolotion, err := json.Marshal(&domainNameResolution)
	Expect(err).NotTo(HaveOccurred())
	_, err = agentBMClient.Installer.V2PostStepReply(ctx, &installer.V2PostStepReplyParams{
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

func updateVipParams(ctx context.Context, clusterID strfmt.UUID) {
	apiVip := "1.2.3.5"
	ingressVip := "1.2.3.6"
	_, err := userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVip:            &apiVip,
			IngressVip:        &ingressVip,
		},
		ClusterID: clusterID,
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func v2UpdateVipParams(ctx context.Context, clusterID strfmt.UUID) {
	apiVip := "1.2.3.5"
	ingressVip := "1.2.3.6"
	_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			VipDhcpAllocation: swag.Bool(false),
			APIVip:            &apiVip,
			IngressVip:        &ingressVip,
		},
		ClusterID: clusterID,
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func register3nodes(ctx context.Context, clusterID strfmt.UUID, cidr string) ([]*models.Host, []string) {
	ips := hostutil.GenerateIPv4Addresses(3, cidr)
	h1 := registerNode(ctx, clusterID, "h1", ips[0])
	h2 := registerNode(ctx, clusterID, "h2", ips[1])
	h3 := registerNode(ctx, clusterID, "h3", ips[2])
	updateVipParams(ctx, clusterID)
	generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3)

	return []*models.Host{h1, h2, h3}, ips
}

func reportMonitoredOperatorStatus(ctx context.Context, client *client.AssistedInstall, clusterID strfmt.UUID, opName string, opStatus models.OperatorStatus) {
	_, err := client.Operators.ReportMonitoredOperatorStatus(ctx, &operatorsClient.ReportMonitoredOperatorStatusParams{
		ClusterID: clusterID,
		ReportParams: &models.OperatorMonitorReport{
			Name:       opName,
			Status:     opStatus,
			StatusInfo: string(opStatus),
		},
	})
	Expect(err).NotTo(HaveOccurred())
}

func verifyUsageSet(featureUsage string, candidates ...models.Usage) {
	usages := make(map[string]models.Usage)
	err := json.Unmarshal([]byte(featureUsage), &usages)
	Expect(err).NotTo(HaveOccurred())
	for _, usage := range candidates {
		usage.ID = usageMgr.UsageNameToID(usage.Name)
		Expect(usages[usage.Name]).To(Equal(usage))
	}
}

func verifyUsageNotSet(featureUsage string, features ...string) {
	usages := make(map[string]*models.Usage)
	err := json.Unmarshal([]byte(featureUsage), &usages)
	Expect(err).NotTo(HaveOccurred())
	for _, name := range features {
		Expect(usages[name]).To(BeNil())
	}
}

func waitForInstallationPreparationCompletionStatus(clusterID strfmt.UUID, status string) {

	waitFunc := func() (bool, error) {
		c := getCommonCluster(context.Background(), clusterID)
		return c.InstallationPreparationCompletionStatus == status, nil
	}
	err := wait.Poll(pollDefaultInterval, pollDefaultTimeout, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}
