package host

import (
	"context"
	"fmt"

	"github.com/openshift/assisted-service/internal/connectivity"

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
	log          logrus.FieldLogger
	db           *gorm.DB
	stateToSteps stateToStepsMap
}
type InstructionConfig struct {
	ServiceBaseURL          string `envconfig:"SERVICE_BASE_URL"`
	InstallerImage          string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ControllerImage         string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-controller:latest"`
	ConnectivityCheckImage  string `envconfig:"CONNECTIVITY_CHECK_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	InventoryImage          string `envconfig:"INVENTORY_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	FreeAddressesImage      string `envconfig:"FREE_ADDRESSES_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	DhcpLeaseAllocatorImage string `envconfig:"DHCP_LEASE_ALLOCATOR_IMAGE" default:"quay.io/assisted-installer-agent:latest"`
	SkipCertVerification    bool   `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, instructionConfig InstructionConfig, connectivityValidator connectivity.Validator) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, connectivityValidator, instructionConfig.ConnectivityCheckImage)
	installCmd := NewInstallCmd(log, db, hwValidator, instructionConfig)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.InventoryImage)
	freeAddressesCmd := NewFreeAddressesCmd(log, instructionConfig.FreeAddressesImage)
	resetCmd := NewResetInstallationCmd(log)
	stopCmd := NewStopInstallationCmd(log)
	dhcpAllocateCmd := NewDhcpAllocateCmd(log, instructionConfig.DhcpLeaseAllocatorImage, db)

	return &InstructionManager{
		log: log,
		db:  db,
		stateToSteps: stateToStepsMap{
			models.HostStatusKnown:                    {[]CommandGetter{connectivityCmd, freeAddressesCmd, dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusInsufficient:             {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusDisconnected:             {[]CommandGetter{inventoryCmd, connectivityCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusDiscovering:              {[]CommandGetter{inventoryCmd, connectivityCmd}, defaultNextInstructionInSec},
			models.HostStatusPendingForInput:          {[]CommandGetter{inventoryCmd, connectivityCmd, freeAddressesCmd, dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusInstalling:               {[]CommandGetter{installCmd, dhcpAllocateCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusInstallingInProgress:     {[]CommandGetter{dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusPreparingForInstallation: {[]CommandGetter{dhcpAllocateCmd}, defaultNextInstructionInSec},
			models.HostStatusDisabled:                 {[]CommandGetter{}, defaultBackedOffInstructionInSec},
			models.HostStatusResetting:                {[]CommandGetter{resetCmd}, defaultBackedOffInstructionInSec},
			models.HostStatusError:                    {[]CommandGetter{stopCmd}, defaultBackedOffInstructionInSec},
		},
	}
}

func (i *InstructionManager) GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error) {

	log := logutil.FromContext(ctx, i.log)
	ClusterID := host.ClusterID
	HostID := host.ID
	HostStatus := swag.StringValue(host.Status)
	log.Infof("GetNextSteps cluster: ,<%s> host: <%s>, host status: <%s>", ClusterID, HostID, HostStatus)

	returnSteps := models.Steps{}

	if cmdsMap, ok := i.stateToSteps[HostStatus]; ok {
		//need to add the step id
		returnSteps.NextInstructionSeconds = cmdsMap.NextStepInSec
		for _, cmd := range cmdsMap.Commands {
			step, err := cmd.GetStep(ctx, host)
			if err != nil {
				return returnSteps, err
			}
			if step == nil {
				continue
			}
			if step.StepID == "" {
				step.StepID = createStepID(step.StepType)
			}
			returnSteps.Instructions = append(returnSteps.Instructions, step)
		}
	} else {
		returnSteps.NextInstructionSeconds = defaultNextInstructionInSec
	}
	logSteps(returnSteps, ClusterID, HostID, log)
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
