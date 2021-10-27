package hostcommands

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/internal/connectivity"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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
	Commands       []CommandGetter
	NextStepInSec  int64
	PostStepAction string
}

type stateToStepsMap map[string]StepsStruct

type InstructionManager struct {
	log                           logrus.FieldLogger
	db                            *gorm.DB
	installingClusterStateToSteps stateToStepsMap
	addHostsClusterToSteps        stateToStepsMap
	poolHostToSteps               stateToStepsMap
	disabledStepsMap              map[models.StepType]bool
}

type InstructionConfig struct {
	ServiceBaseURL           string            `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath        string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs               string            `envconfig:"SERVICE_IPS" default:""`
	InstallerImage           string            `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage          string            `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	AgentImage               string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	SkipCertVerification     bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	DiskCheckTimeout         time.Duration     `envconfig:"DISK_CHECK_TIMEOUT" default:"8m"`
	ImageAvailabilityTimeout time.Duration     `envconfig:"IMAGE_AVAILABILITY_TIMEOUT" default:"16m"`
	DisabledSteps            []models.StepType `envconfig:"DISABLED_STEPS" default:""`
	ReleaseImageMirror       string
	CheckClusterVersion      bool
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, connectivityValidator connectivity.Validator, eventsHandler eventsapi.Handler, versionHandler versions.Handler) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, connectivityValidator, instructionConfig.AgentImage)
	installCmd := NewInstallCmd(log, db, hwValidator, ocRelease, instructionConfig, eventsHandler, versionHandler)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.AgentImage)
	freeAddressesCmd := newFreeAddressesCmd(log, instructionConfig.AgentImage)
	resetCmd := NewResetInstallationCmd(log)
	stopCmd := NewStopInstallationCmd(log)
	logsCmd := NewLogsCmd(log, db, instructionConfig)
	dhcpAllocateCmd := NewDhcpAllocateCmd(log, instructionConfig.AgentImage, db)
	apivipConnectivityCmd := NewAPIVIPConnectivityCheckCmd(log, db, instructionConfig.AgentImage)
	ntpSynchronizerCmd := NewNtpSyncCmd(log, instructionConfig.AgentImage, db)
	diskPerfCheckCmd := NewDiskPerfCheckCmd(log, instructionConfig.AgentImage, hwValidator, instructionConfig.DiskCheckTimeout.Seconds())
	imageAvailabilityCmd := NewImageAvailabilityCmd(log, db, ocRelease, versionHandler, instructionConfig, instructionConfig.ImageAvailabilityTimeout.Seconds())
	domainNameResolutionCmd := NewDomainNameResolutionCmd(log, instructionConfig.AgentImage, db)
	noopCmd := NewNoopCmd()

	return &InstructionManager{
		log:              log,
		db:               db,
		disabledStepsMap: generateDisabledStepsMap(log, instructionConfig.DisabledSteps),
		installingClusterStateToSteps: stateToStepsMap{
			models.HostStatusKnown:                    {[]CommandGetter{connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficient:             {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnected:             {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDiscovering:              {[]CommandGetter{inventoryCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusPendingForInput:          {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstalling:               {[]CommandGetter{installCmd, dhcpAllocateCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstallingInProgress:     {[]CommandGetter{inventoryCmd, dhcpAllocateCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue}, //TODO inventory step here is a temporary solution until format command is moved to a different state
			models.HostStatusPreparingForInstallation: {[]CommandGetter{dhcpAllocateCmd, diskPerfCheckCmd, imageAvailabilityCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabled:                 {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusResetting:                {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusError:                    {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusCancelled:                {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusBinding:                  {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
		},
		addHostsClusterToSteps: stateToStepsMap{
			models.HostStatusKnown:                {[]CommandGetter{connectivityCmd, apivipConnectivityCmd, inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficient:         {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnected:         {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDiscovering:          {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusPendingForInput:      {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstalling:           {[]CommandGetter{installCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstallingInProgress: {[]CommandGetter{}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabled:             {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusResetting:            {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusError:                {[]CommandGetter{stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusCancelled:            {[]CommandGetter{stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
		},
		poolHostToSteps: stateToStepsMap{
			models.HostStatusDiscoveringUnbound:         {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnectedUnbound:        {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabledUnbound:            {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficientUnbound:        {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusKnownUnbound:               {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusUnbinding:                  {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
			models.HostStatusUnbindingPendingUserAction: {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
		},
	}
}

func (i *InstructionManager) isStepDisabled(stepType models.StepType) bool {
	_, ok := i.disabledStepsMap[stepType]
	return ok
}

func (i *InstructionManager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {

	log := logutil.FromContext(ctx, i.log)
	InfraEnvID := host.InfraEnvID
	hostID := host.ID
	hostStatus := swag.StringValue(host.Status)
	log.Infof("GetNextSteps infra_env: <%s>, host: <%s>, host status: <%s>", InfraEnvID, hostID, hostStatus)

	returnSteps := models.Steps{}
	stateToSteps := i.installingClusterStateToSteps
	if hostutil.IsDay2Host(host) {
		stateToSteps = i.addHostsClusterToSteps
	}
	if hostutil.IsUnboundHost(host) {
		stateToSteps = i.poolHostToSteps
	}

	// default value for states with not step defined
	returnSteps.PostStepAction = swag.String(models.StepsPostStepActionContinue)
	if cmdsMap, ok := stateToSteps[hostStatus]; ok {
		//need to add the step id
		returnSteps.NextInstructionSeconds = cmdsMap.NextStepInSec
		returnSteps.PostStepAction = swag.String(cmdsMap.PostStepAction)
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
					log.Infof("Step '%v' is disabled. Will not include it in instructions", step.StepType)
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
	logSteps(returnSteps, InfraEnvID, hostID, log)
	return returnSteps, nil
}

func generateDisabledStepsMap(log logrus.FieldLogger, disabledSteps []models.StepType) map[models.StepType]bool {
	result := make(map[models.StepType]bool)
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

func logSteps(steps models.Steps, infraEnvId strfmt.UUID, hostId *strfmt.UUID, log logrus.FieldLogger) {
	if len(steps.Instructions) == 0 {
		log.Infof("No steps required for infraEnv <%s> host <%s>", infraEnvId, hostId)
	}
	for _, step := range steps.Instructions {
		log.Infof("Submitting step <%s> id <%s> to infra_env <%s> host <%s> Command: <%s> Arguments: <%+v>", step.StepType, step.StepID, infraEnvId, hostId,
			step.Command, step.Args)
	}
}
