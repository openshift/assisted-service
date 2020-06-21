package host

import (
	"context"
	"fmt"

	"github.com/jinzhu/gorm"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=instructionmanager.go -package=host -destination=mock_instruction_api.go
type InstructionApi interface {
	GetNextSteps(ctx context.Context, host *models.Host) (models.Steps, error)
}

type stateToStepsMap map[string][]CommandGetter

type InstructionManager struct {
	log          logrus.FieldLogger
	db           *gorm.DB
	stateToSteps stateToStepsMap
}
type InstructionConfig struct {
	InventoryURL           string `envconfig:"INVENTORY_URL" default:"10.35.59.36"`
	InventoryPort          string `envconfig:"INVENTORY_PORT" default:"30485"`
	InstallerImage         string `envconfig:"INSTALLER_IMAGE" default:"quay.io/ocpmetal/assisted-installer:latest"`
	ConnectivityCheckImage string `envconfig:"CONNECTIVITY_CHECK_IMAGE" default:"quay.io/ocpmetal/connectivity_check:latest"`
	InventoryImage         string `envconfig:"INVENTORY_IMAGE" default:"quay.io/ocpmetal/inventory:latest"`
	HardwareInfoImage      string `envconfig:"HARDWARE_INFO_IMAGE" default:"quay.io/ocpmetal/hardware_info:latest"`
	FreeAddressesImage     string `envconfig:"FREE_ADDRESSES_IMAGE" default:"quay.io/ocpmetal/free_addresses:latest"`
}

func NewInstructionManager(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, instructionConfig InstructionConfig) *InstructionManager {
	connectivityCmd := NewConnectivityCheckCmd(log, db, hwValidator, instructionConfig.ConnectivityCheckImage)
	installCmd := NewInstallCmd(log, db, hwValidator, instructionConfig)
	hwCmd := NewHwInfoCmd(log, instructionConfig.HardwareInfoImage)
	inventoryCmd := NewInventoryCmd(log, instructionConfig.InventoryImage)
	freeAddressesCmd := NewFreeAddressesCmd(log, instructionConfig.FreeAddressesImage)

	return &InstructionManager{
		log: log,
		db:  db,
		stateToSteps: stateToStepsMap{
			HostStatusKnown:        {connectivityCmd, freeAddressesCmd},
			HostStatusInsufficient: {hwCmd, inventoryCmd, connectivityCmd, freeAddressesCmd},
			HostStatusDisconnected: {hwCmd, inventoryCmd, connectivityCmd},
			HostStatusDiscovering:  {hwCmd, inventoryCmd, connectivityCmd},
			HostStatusInstalling:   {installCmd},
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
		for _, cmd := range cmdsMap {
			step, err := cmd.GetStep(ctx, host)
			if err != nil {
				return returnSteps, err
			}
			if step.StepID == "" {
				step.StepID = createStepID(step.StepType)
			}
			returnSteps = append(returnSteps, step)
		}
	}
	logSteps(returnSteps, ClusterID, HostID, log)
	return returnSteps, nil
}

func createStepID(stepType models.StepType) string {
	return fmt.Sprintf("%s-%s", stepType, uuid.New().String()[:8])
}

func logSteps(steps models.Steps, clusterId strfmt.UUID, hostId *strfmt.UUID, log logrus.FieldLogger) {
	if len(steps) == 0 {
		log.Infof("No steps required for cluster <%s> host <%s>", clusterId, hostId)
	}
	for _, step := range steps {
		log.Infof("Submitting step <%s> id <%s> to cluster <%s> host <%s> Command: <%s> Arguments: <%+v>", step.StepType, step.StepID, clusterId, hostId,
			step.Command, step.Args)
	}
}
