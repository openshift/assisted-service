package host

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/versions"

	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/internal/oc"

	"github.com/jinzhu/gorm"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=instructionmanager.go -package=host -destination=mock_instruction_api.go
type InstructionApi interface {
	GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error)
}

const (
	defaultNextInstructionInSec      = int64(60)
	defaultBackedOffInstructionInSec = int64(120)
)

type StepsStruct struct {
	Commands      []CommandGetter
	NextStepInSec int64
}

type stateToStepsMap map[string]StepsStruct

type InstructionManager struct {
	log                           logrus.FieldLogger
	db                            *gorm.DB
	installingClusterStateToSteps stateToStepsMap
	addHostsClusterToSteps        stateToStepsMap
}

type InstructionConfig struct {
	ServiceBaseURL               string `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath            string `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs                   string `envconfig:"SERVICE_IPS" default:""`
	InstallerImage               string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage              string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	ConnectivityCheckImage       string `envconfig:"CONNECTIVITY_CHECK_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	InventoryImage               string `envconfig:"INVENTORY_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	FreeAddressesImage           string `envconfig:"FREE_ADDRESSES_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	DhcpLeaseAllocatorImage      string `envconfig:"DHCP_LEASE_ALLOCATOR_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	APIVIPConnectivityCheckImage string `envconfig:"API_VIP_CONNECTIVITY_CHECK_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	NtpSynchronizerImage         string `envconfig:"NTP_SYNCHRONIZER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	FioPerfCheckImage            string `envconfig:"FIO_PERF_CHECK_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	SkipCertVerification         bool   `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	SupportL2                    bool   `envconfig:"SUPPORT_L2" default:"true"`
	InstallationTimeout          uint   `envconfig:"INSTALLATION_TIMEOUT" default:"0"`
	ReleaseImageMirror           string
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, connectivityValidator connectivity.Validator, eventsHandler events.Handler, versionHandler versions.Handler) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, connectivityValidator, instructionConfig.ConnectivityCheckImage)
	installCmd := NewInstallCmd(log, db, hwValidator, ocRelease, instructionConfig, eventsHandler, versionHandler)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.InventoryImage)
	freeAddressesCmd := NewFreeAddressesCmd(log, instructionConfig.FreeAddressesImage)
	resetCmd := NewResetInstallationCmd(log)
	stopCmd := NewStopInstallationCmd(log)
	logsCmd := NewLogsCmd(log, db, instructionConfig)
	dhcpAllocateCmd := NewDhcpAllocateCmd(log, instructionConfig.DhcpLeaseAllocatorImage, db)
	apivipConnectivityCmd := NewAPIVIPConnectivityCheckCmd(log, db, instructionConfig.APIVIPConnectivityCheckImage, instructionConfig.SupportL2)
	downloadInstallerCmd := NewDownloadInstallerCmd(log, instructionConfig)
	ntpSynchronizerCmd := NewNtpSyncCmd(log, instructionConfig.NtpSynchronizerImage, db)

	return &InstructionManager{
		log: log,
		db:  db,
		installingClusterStateToSteps: stateToStepsMap{
			models.HostStatusKnown:                    {[]CommandGetter{connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInsufficient:             {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusDisconnected:             {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusDiscovering:              {[]CommandGetter{inventoryCmd, downloadInstallerCmd}, defaultNextInstructionInSec},
			models.HostStatusPendingForInput:          {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInstalling:               {[]CommandGetter{installCmd, dhcpAllocateCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusInstallingInProgress:     {[]CommandGetter{inventoryCmd, dhcpAllocateCmd}, defaultNextInstructionInSec}, //TODO inventory step here is a temporary solution until format command is moved to a different state
			models.HostStatusPreparingForInstallation: {[]CommandGetter{dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusDisabled:                 {[]CommandGetter{}, defaultBackedOffInstructionInSec},
			models.HostStatusResetting:                {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusError:                    {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusCancelled:                {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec},
		},
		addHostsClusterToSteps: stateToStepsMap{
			models.HostStatusKnown:                {[]CommandGetter{connectivityCmd, apivipConnectivityCmd, inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInsufficient:         {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusDisconnected:         {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusDiscovering:          {[]CommandGetter{inventoryCmd, downloadInstallerCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusPendingForInput:      {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd}, defaultNextInstructionInSec},
			models.HostStatusInstalling:           {[]CommandGetter{installCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusInstallingInProgress: {[]CommandGetter{}, defaultNextInstructionInSec},
			models.HostStatusDisabled:             {[]CommandGetter{}, defaultBackedOffInstructionInSec},
			models.HostStatusResetting:            {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusError:                {[]CommandGetter{stopCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusCancelled:            {[]CommandGetter{stopCmd}, defaultBackedOffInstructionInSec},
		},
	}
}

func (i *InstructionManager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {

	log := logutil.FromContext(ctx, i.log)
	ClusterID := host.ClusterID
	hostID := host.ID
	hostStatus := swag.StringValue(host.Status)
	log.Infof("GetNextSteps cluster: ,<%s> host: <%s>, host status: <%s>", ClusterID, hostID, hostStatus)

	returnSteps := models.Steps{}
	stateToSteps := i.installingClusterStateToSteps
	if IsDay2Host(host) {
		stateToSteps = i.addHostsClusterToSteps
	}

	returnSteps.PostStepAction = swag.String(models.StepsPostStepActionContinue)
	if cmdsMap, ok := stateToSteps[hostStatus]; ok {
		//need to add the step id
		returnSteps.NextInstructionSeconds = cmdsMap.NextStepInSec
		for _, cmd := range cmdsMap.Commands {
			steps, err := cmd.GetSteps(ctx, host)
			if err != nil {
				// Allow to return additional steps if the current one failed
				log.WithError(err).Warn("Failed to generate steps for command")
				continue
			}
			if steps == nil {
				continue
			}
			for _, step := range steps {
				if step.StepID == "" {
					step.StepID = createStepID(step.StepType)
				}
			}

			returnSteps.Instructions = append(returnSteps.Instructions, steps...)
		}
	} else {
		returnSteps.NextInstructionSeconds = defaultNextInstructionInSec
	}
	logSteps(returnSteps, ClusterID, hostID, log)
	return returnSteps, nil
}

func createStepID(stepType models.StepType) string {
	return fmt.Sprintf("%s-%s", stepType, uuid.New().String()[:8])
}

func logSteps(steps models.Steps, clusterId strfmt.UUID, hostId *strfmt.UUID, log logrus.FieldLogger) {
	if len(steps.Instructions) == 0 {
		log.Infof("No steps required for cluster <%s> host <%s>", clusterId, hostId)
	}
	for _, step := range steps.Instructions {
		log.Infof("Submitting step <%s> id <%s> to cluster <%s> host <%s> Command: <%s> Arguments: <%+v>", step.StepType, step.StepID, clusterId, hostId,
			step.Command, step.Args)
	}
}
