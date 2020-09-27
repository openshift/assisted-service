package host

import (
	"bytes"
	"context"
	"fmt"
	"html/template"

	"github.com/openshift/assisted-service/internal/connectivity"

	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/models"
)

type resetInstallationCmd struct {
	baseCmd
	connectivityValidator connectivity.Validator
}

func NewResetInstallationCmd(log logrus.FieldLogger, connectivityValidator connectivity.Validator) *resetInstallationCmd {
	return &resetInstallationCmd{
		baseCmd:               baseCmd{log: log},
		connectivityValidator: connectivityValidator,
	}
}

func (ri *resetInstallationCmd) GetStep(_ context.Context, host *models.Host) (*models.Step, error) {
	var cmd string
	if host.Bootstrap {
		cmd += "systemctl stop bootkube.service; " +
			"rm -rf /etc/kubernetes/manifests/* " +
			"/etc/kubernetes/static-pod-resources/* " +
			"/opt/openshift/*.done; "
		resetNetIfaceCmd, err := ri.getResetNetworkInterfacesCmd(*host)
		if err != nil {
			return nil, err
		}
		cmd += *resetNetIfaceCmd
	}
	cmd += "/usr/bin/podman rm --all -f; systemctl restart agent; "
	t, err := template.New("cmd").Parse(cmd)
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
	return step, nil
}

func (ri resetInstallationCmd) getResetNetworkInterfacesCmd(host models.Host) (*string, error) {
	interfaces, err := ri.connectivityValidator.GetHostValidInterfaces(&host)
	if err != nil {
		return nil, err
	}
	var cmd string
	for _, iface := range interfaces {
		cmd += fmt.Sprintf("ip address flush %s; ", iface.Name)
	}
	if cmd != "" {
		cmd += "systemctl restart NetworkManager.service; "
	}
	return &cmd, nil
}
