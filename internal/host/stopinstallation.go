package host

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"

	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/models"
)

type stopInstallationCmd struct {
	baseCmd
	instructionConfig InstructionConfig
}

func NewStopInstallationCmd(log logrus.FieldLogger, instructionConfig InstructionConfig) *stopInstallationCmd {
	return &stopInstallationCmd{
		baseCmd:           baseCmd{log: log},
		instructionConfig: instructionConfig,
	}
}

func (h *stopInstallationCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeExecute

	const stopAllCommand = "podman stop `podman ps --format '\"'{{.ID}} {{.Names}}'\"' | grep -v next-step-runner | awk '\"'{print \\$1}'\"'`"

	// added to run upload logs if we are in error or cancelled state. Stop all and gather logs
	// will return same exit code as stop command command
	if host.LogsCollectedAt == strfmt.DateTime(time.Time{}) {
		logsCommand, err := CreateUploadLogsCmd(host, h.instructionConfig.ServiceBaseURL,
			h.instructionConfig.InventoryImage, h.instructionConfig.SkipCertVerification, false)
		if err != nil {
			h.log.WithError(err).Error("Failed to create logs upload command")
		}
		step.Command = "bash"
		step.Args = []string{"-c", stopAllCommand + "; " + logsCommand}
	} else {
		step.Command = "bash"
		step.Args = []string{"-c", stopAllCommand}
	}

	return step, nil
}
