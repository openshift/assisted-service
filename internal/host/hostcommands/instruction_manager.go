package hostcommands

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -package=hostcommands -destination=mock_instruction_api.go . InstructionApi
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
	disabledStepsMap              map[models.StepType]bool
}

type InstructionConfig struct {
	ServiceBaseURL       string            `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath    string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs           string            `envconfig:"SERVICE_IPS" default:""`
	InstallerImage       string            `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage      string            `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	AgentImage           string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	SkipCertVerification bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	SupportL2            bool              `envconfig:"SUPPORT_L2" default:"true"`
	InstallationTimeout  uint              `envconfig:"INSTALLATION_TIMEOUT" default:"0"`
	DiskCheckTimeout     time.Duration     `envconfig:"DISK_CHECK_TIMEOUT" default:"8m"`
	SupportFreeAddresses bool              `envconfig:"SUPPORT_FREE_ADDRESSES" default:"true"`
	DisabledSteps        []models.StepType `envconfig:"DISABLED_STEPS" default:""`
	ReleaseImageMirror   string
	CheckClusterVersion  bool
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, connectivityValidator connectivity.Validator, eventsHandler events.Handler, versionHandler versions.Handler) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, connectivityValidator, instructionConfig.AgentImage)
	installCmd := NewInstallCmd(log, db, hwValidator, ocRelease, instructionConfig, eventsHandler, versionHandler)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.AgentImage)
	freeAddressesCmd := newFreeAddressesCmd(log, instructionConfig.AgentImage, instructionConfig.SupportFreeAddresses)
	resetCmd := NewResetInstallationCmd(log)
	stopCmd := NewStopInstallationCmd(log)
	logsCmd := NewLogsCmd(log, db, instructionConfig)
	dhcpAllocateCmd := NewDhcpAllocateCmd(log, instructionConfig.AgentImage, db)
	apivipConnectivityCmd := NewAPIVIPConnectivityCheckCmd(log, db, instructionConfig.AgentImage, instructionConfig.SupportL2)
	ntpSynchronizerCmd := NewNtpSyncCmd(log, instructionConfig.AgentImage, db)
	diskPerfCheckCmd := NewDiskPerfCheckCmd(log, instructionConfig.AgentImage, hwValidator, instructionConfig.DiskCheckTimeout.Seconds())
	imageAvailabilityCmd := NewImageAvailabilityCmd(log, db, ocRelease, versionHandler, instructionConfig)

	return &InstructionManager{
		log:              log,
		db:               db,
		disabledStepsMap: generateDisabledStepsMap(log, instructionConfig.DisabledSteps),
		installingClusterStateToSteps: stateToStepsMap{
			models.HostStatusKnown:                    {[]CommandGetter{connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInsufficient:             {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusDisconnected:             {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusDiscovering:              {[]CommandGetter{inventoryCmd}, defaultNextInstructionInSec},
			models.HostStatusPendingForInput:          {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInstalling:               {[]CommandGetter{installCmd, dhcpAllocateCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusInstallingInProgress:     {[]CommandGetter{inventoryCmd, dhcpAllocateCmd}, defaultNextInstructionInSec}, //TODO inventory step here is a temporary solution until format command is moved to a different state
			models.HostStatusPreparingForInstallation: {[]CommandGetter{dhcpAllocateCmd, diskPerfCheckCmd, imageAvailabilityCmd}, defaultNextInstructionInSec},
			models.HostStatusDisabled:                 {[]CommandGetter{}, defaultBackedOffInstructionInSec},
			models.HostStatusResetting:                {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusError:                    {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusCancelled:                {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec},
		},
		addHostsClusterToSteps: stateToStepsMap{
			models.HostStatusKnown:                {[]CommandGetter{connectivityCmd, apivipConnectivityCmd, inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusInsufficient:         {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
			models.HostStatusDisconnected:         {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusDiscovering:          {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec},
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

func (i *InstructionManager) isStepDisabled(stepType models.StepType) bool {
	_, ok := i.disabledStepsMap[stepType]
	return ok
}

func (i *InstructionManager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {

	log := logutil.FromContext(ctx, i.log)
	ClusterID := host.ClusterID
	hostID := host.ID
	hostStatus := swag.StringValue(host.Status)
	log.Infof("GetNextSteps cluster: ,<%s> host: <%s>, host status: <%s>", ClusterID, hostID, hostStatus)

	returnSteps := models.Steps{}
	stateToSteps := i.installingClusterStateToSteps
	if hostutil.IsDay2Host(host) {
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
				log.WithError(err).Warnf("Failed to generate steps for command %T", cmd)
				continue
			}
			if steps == nil {
				continue
			}
			// Creating StepID when needed and filtering out disabled steps
			enabledSteps := []*models.Step{}
			for _, step := range steps {
				if i.isStepDisabled(step.StepType) {
					log.WithField("disabledStepsMap", i.disabledStepsMap).Infof("Step '%v' is disabled. Will not include it in instructions", step.StepType)
					continue
				}
				if step.StepID == "" {
					step.StepID = createStepID(step.StepType)
				}
				enabledSteps = append(enabledSteps, step)
			}
			returnSteps.Instructions = append(returnSteps.Instructions, enabledSteps...)
		}
	} else {
		returnSteps.NextInstructionSeconds = defaultNextInstructionInSec
	}
	logSteps(returnSteps, ClusterID, hostID, log)
	return returnSteps, nil
}

func generateDisabledStepsMap(log logrus.FieldLogger, disabledSteps []models.StepType) map[models.StepType]bool {
	result := map[models.StepType]bool{}
	for _, step := range disabledSteps {
		if err := step.Validate(nil); err != nil {
			// invalid step type
			log.WithField("DISABLED_STEPS", disabledSteps).Warnf("InstructionManager Found an invalid StepType '%v' in DISABLED_STEPS. Ignoring...", step)
			continue
		}
		result[step] = true
	}
	return result
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
