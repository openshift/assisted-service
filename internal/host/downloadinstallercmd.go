package host

import (
	"bytes"
	"context"
	"text/template"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type downloadInstallerCmd struct {
	baseCmd
	instructionConfig InstructionConfig
}

func NewDownloadInstallerCmd(log logrus.FieldLogger, cfg InstructionConfig) *downloadInstallerCmd {
	return &downloadInstallerCmd{
		baseCmd:           baseCmd{log: log},
		instructionConfig: cfg,
	}
}

func (d *downloadInstallerCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeExecute
	step.Command = "timeout"

	cmdArgsTmpl := "until podman pull {{.INSTALLER}}; do sleep 1; done"

	data := map[string]string{
		"INSTALLER": d.instructionConfig.InstallerImage,
	}

	t, err := template.New("cmd").Parse(cmdArgsTmpl)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		return nil, err
	}
	step.Args = []string{"15m", "bash", "-c", buf.String()}

	return step, nil
}
