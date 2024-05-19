package hostcommands

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/connectivity"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/feature"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

//go:generate mockgen --build_flags=--mod=mod -package=hostcommands -destination=mock_instruction_api.go . InstructionApi
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
	config                        InstructionConfig
	installingClusterStateToSteps stateToStepsMap
	addHostsClusterToSteps        stateToStepsMap
	poolHostToSteps               stateToStepsMap
	disabledStepsMap              map[models.StepType]bool
	upgradeAgentCmd               CommandGetter
	eventsHandler                 eventsapi.Sender
}

type InstructionConfig struct {
	feature.Flags

	AuthType                 auth.AuthType     `envconfig:"AUTH_TYPE" default:""`
	ServiceBaseURL           string            `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath        string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs               string            `envconfig:"SERVICE_IPS" default:""`
	ImageServiceBaseURL      string            `envconfig:"IMAGE_SERVICE_BASE_URL"`
	ImageExpirationTime      time.Duration     `envconfig:"IMAGE_EXPIRATION_TIME" default:"4h"`
	InstallerImage           string            `envconfig:"INSTALLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer:latest"`
	ControllerImage          string            `envconfig:"CONTROLLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-controller:latest"`
	AgentImage               string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	SkipCertVerification     bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	DiskCheckTimeout         time.Duration     `envconfig:"DISK_CHECK_TIMEOUT" default:"8m"`
	ImageAvailabilityTimeout time.Duration     `envconfig:"IMAGE_AVAILABILITY_TIMEOUT" default:"16m"`
	DisabledSteps            []models.StepType `envconfig:"DISABLED_STEPS" default:""`
	ReleaseImageMirror       string
	CheckClusterVersion      bool
	HostFSMountDir           string
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, connectivityValidator connectivity.Validator, eventsHandler eventsapi.Handler,
	versionHandler versions.Handler, osImages versions.OSImages, kubeApiEnabled bool) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, connectivityValidator, instructionConfig.AgentImage)
	installCmd := NewInstallCmd(log, db, hwValidator, ocRelease, instructionConfig, eventsHandler, versionHandler, instructionConfig.EnableSkipMcoReboot, !kubeApiEnabled)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.AgentImage)
	freeAddressesCmd := newFreeAddressesCmd(log, kubeApiEnabled)
	stopCmd := NewStopInstallationCmd(log)
	logsCmd := NewLogsCmd(log, db, instructionConfig)
	dhcpAllocateCmd := NewDhcpAllocateCmd(log, instructionConfig.AgentImage, db)
	apivipConnectivityCmd := NewAPIVIPConnectivityCheckCmd(log, db, instructionConfig.AgentImage)
	tangConnectivityCmd := NewTangConnectivityCheckCmd(log, db, instructionConfig.AgentImage)
	ntpSynchronizerCmd := NewNtpSyncCmd(log, instructionConfig.AgentImage, db)
	diskPerfCheckCmd := NewDiskPerfCheckCmd(log, instructionConfig.AgentImage, hwValidator, instructionConfig.DiskCheckTimeout.Seconds())
	imageAvailabilityCmd := NewImageAvailabilityCmd(log, db, ocRelease, versionHandler, instructionConfig, instructionConfig.ImageAvailabilityTimeout.Seconds())
	domainNameResolutionCmd := NewDomainNameResolutionCmd(log, instructionConfig.AgentImage, versionHandler, db)
	noopCmd := NewNoopCmd()
	upgradeAgentCmd := NewUpgradeAgentCmd(instructionConfig.AgentImage)
	downloadBootArtifactsCmd := NewDownloadBootArtifactsCmd(log, instructionConfig.ImageServiceBaseURL, instructionConfig.AuthType, osImages, db, instructionConfig.ImageExpirationTime, instructionConfig.HostFSMountDir)
	rebootForReclaimCmd := NewRebootForReclaimCmd(log, instructionConfig.HostFSMountDir)
	verifyVipsCmd := newVerifyVipsCmd(log, db)

	return &InstructionManager{
		log:              log,
		db:               db,
		config:           instructionConfig,
		disabledStepsMap: generateDisabledStepsMap(log, instructionConfig.DisabledSteps),
		installingClusterStateToSteps: stateToStepsMap{
			models.HostStatusKnown:                    {[]CommandGetter{connectivityCmd, tangConnectivityCmd, freeAddressesCmd, dhcpAllocateCmd, inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd, verifyVipsCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficient:             {[]CommandGetter{inventoryCmd, connectivityCmd, tangConnectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd, domainNameResolutionCmd, verifyVipsCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnected:             {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDiscovering:              {[]CommandGetter{inventoryCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusPendingForInput:          {[]CommandGetter{inventoryCmd, connectivityCmd, tangConnectivityCmd, freeAddressesCmd, dhcpAllocateCmd, ntpSynchronizerCmd, domainNameResolutionCmd, verifyVipsCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstalling:               {[]CommandGetter{installCmd, dhcpAllocateCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstallingInProgress:     {[]CommandGetter{dhcpAllocateCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue}, //TODO inventory step here is a temporary solution until format command is moved to a different state
			models.HostStatusPreparingForInstallation: {[]CommandGetter{dhcpAllocateCmd, diskPerfCheckCmd, imageAvailabilityCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabled:                 {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusResetting:                {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusError:                    {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusCancelled:                {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusBinding:                  {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
		},
		addHostsClusterToSteps: stateToStepsMap{
			models.HostStatusKnown:                {[]CommandGetter{connectivityCmd, apivipConnectivityCmd, tangConnectivityCmd, inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficient:         {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd, tangConnectivityCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnected:         {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDiscovering:          {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd, domainNameResolutionCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusPendingForInput:      {[]CommandGetter{inventoryCmd, connectivityCmd, apivipConnectivityCmd, tangConnectivityCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstalling:           {[]CommandGetter{installCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInstallingInProgress: {[]CommandGetter{}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabled:             {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusResetting:            {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusError:                {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusCancelled:            {[]CommandGetter{logsCmd, stopCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
		},
		poolHostToSteps: stateToStepsMap{
			models.HostStatusDiscoveringUnbound:         {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisconnectedUnbound:        {[]CommandGetter{inventoryCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusDisabledUnbound:            {[]CommandGetter{}, defaultBackedOffInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusInsufficientUnbound:        {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusKnownUnbound:               {[]CommandGetter{inventoryCmd, ntpSynchronizerCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusUnbinding:                  {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
			models.HostStatusUnbindingPendingUserAction: {[]CommandGetter{noopCmd}, 0, models.StepsPostStepActionExit},
			models.HostStatusReclaiming:                 {[]CommandGetter{downloadBootArtifactsCmd}, defaultNextInstructionInSec, models.StepsPostStepActionContinue},
			models.HostStatusReclaimingRebooting:        {[]CommandGetter{rebootForReclaimCmd}, defaultBackedOffInstructionInSec, models.StepsPostStepActionExit},
		},
		upgradeAgentCmd: upgradeAgentCmd,
		eventsHandler:   eventsHandler,
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

	// If the agent isn't compatible with the service and the host is in a state that allows to
	// upgrade the agent, then replace all the calculated steps with a single step to upgrade
	// the agent:
	if i.isAgentUpgradeAllowed(host) &&
		!common.IsAgentCompatible(i.config.AgentImage, host.DiscoveryAgentVersion) {
		log.WithFields(logrus.Fields{
			"expected_image": i.config.AgentImage,
			"actual_image":   host.DiscoveryAgentVersion,
		}).Info(
			"Agent image ins't compatible with the service, and it can be upgraded, " +
				"will replace all the calculated steps with a single step to " +
				"upgrade the image",
		)
		var err error
		returnSteps.Instructions, err = i.upgradeAgentCmd.GetSteps(ctx, host)
		if err != nil {
			return models.Steps{}, err
		}
		returnSteps.PostStepAction = swag.String(models.StepsPostStepActionContinue)
		returnSteps.NextInstructionSeconds = defaultNextInstructionInSec
		events.SendUpgradeAgentStartedEvent(
			ctx,
			i.eventsHandler,
			*host.ID,
			hostutil.GetHostnameForMsg(host),
			host.InfraEnvID,
			host.ClusterID,
			i.config.AgentImage,
		)
	}

	logSteps(returnSteps, InfraEnvID, hostID, log)
	return returnSteps, nil
}

// isAgentUpgradeAllowed checks if the current state of the host allows the agent to be upgraded.
// For example, it is not allowed to upgrade the agent when the installation of the cluster is in
// progress.
func (i *InstructionManager) isAgentUpgradeAllowed(host *models.Host) bool {
	if !i.config.EnableUpgradeAgent {
		return false
	}
	if i.isStepDisabled(models.StepTypeUpgradeAgent) {
		return false
	}
	switch *host.Status {
	case models.HostStatusBinding,
		models.HostStatusDiscovering,
		models.HostStatusDiscoveringUnbound,
		models.HostStatusInsufficient,
		models.HostStatusInsufficientUnbound,
		models.HostStatusKnown,
		models.HostStatusPendingForInput,
		models.HostStatusKnownUnbound:
		return true
	default:
		return false
	}
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
		log.Infof("Submitting step <%s> id <%s> to infra_env <%s> host <%s>  Arguments: <%+v>", step.StepType, step.StepID, infraEnvId, hostId, step.Args)
	}
}
