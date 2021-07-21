package hostcommands

import (
	"bytes"
	"context"
	"html/template"

	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type resetInstallationCmd struct {
	baseCmd
}

func NewResetInstallationCmd(log logrus.FieldLogger) *resetInstallationCmd {
	return &resetInstallationCmd{
		baseCmd: baseCmd{log: log},
	}
}

func (h *resetInstallationCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var cmdStr string
	if host.Bootstrap {
		cmdStr += "systemctl stop bootkube.service; rm -rf /etc/kubernetes/manifests/* /etc/kubernetes/static-pod-resources/* /opt/openshift/*.done; "
	}
	cmdStr += "/usr/bin/podman rm --all -f; systemctl restart agent; "
	t, err := template.New("cmd").Parse(cmdStr)
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	if err := t.Execute(buf, nil); err != nil {
		return nil, err
	}
	step := &models.Step{}
	step.StepType = models.StepTypeResetInstallation
	step.Command = "bash"
	step.Args = []string{"-c", buf.String()}
	return []*models.Step{step}, nil
}
