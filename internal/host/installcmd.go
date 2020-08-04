package host

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/models"
)

type installCmd struct {
	baseCmd
	db                *gorm.DB
	hwValidator       hardware.Validator
	instructionConfig InstructionConfig
}

func NewInstallCmd(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, instructionConfig InstructionConfig) *installCmd {
	return &installCmd{
		baseCmd:           baseCmd{log: log},
		db:                db,
		hwValidator:       hwValidator,
		instructionConfig: instructionConfig,
	}
}

func (i *installCmd) GetStep(ctx context.Context, host *models.Host) (*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeInstall
	step.Command = "bash"

	//get openshift version
	var cluster common.Cluster
	if err := i.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		i.log.Errorf("failed to get cluster %s", host.ClusterID)
		return nil, err
	}

	var role = host.Role
	if host.Bootstrap {
		role = models.HostRoleBootstrap
	}

	cmdArgsTmpl := "podman run -v /dev:/dev:rw -v /opt:/opt:rw -v /run/systemd/journal/socket:/run/systemd/journal/socket --privileged --pid=host --net=host " +
		"-v /var/log:/var/log:rw --env PULL_SECRET_TOKEN --name assisted-installer {{.INSTALLER}} --role {{.ROLE}} --cluster-id {{.CLUSTER_ID}} --host {{.HOST}} " +
		"--port {{.PORT}} --boot-device {{.BOOT_DEVICE}} --host-id {{.HOST_ID}} --openshift-version {{.OPENSHIFT_VERSION}} " +
		"--controller-image {{.CONTROLLER_IMAGE}}"

	data := map[string]string{
		"HOST":              strings.TrimSpace(i.instructionConfig.ServiceURL),
		"PORT":              strings.TrimSpace(i.instructionConfig.ServicePort),
		"CLUSTER_ID":        string(host.ClusterID),
		"HOST_ID":           string(*host.ID),
		"ROLE":              string(role),
		"INSTALLER":         i.instructionConfig.InstallerImage,
		"CONTROLLER_IMAGE":  i.instructionConfig.ControllerImage,
		"BOOT_DEVICE":       "",
		"OPENSHIFT_VERSION": cluster.OpenshiftVersion,
	}

	hostname, _ := common.GetCurrentHostName(host)
	if hostname != "" {
		cmdArgsTmpl = cmdArgsTmpl + " --host-name {{.HOST_NAME}}"
		data["HOST_NAME"] = hostname
	}

	bootdevice, err := getBootDevice(i.log, i.hwValidator, *host)
	if err != nil {
		return nil, err
	}
	data["BOOT_DEVICE"] = bootdevice

	t, err := template.New("cmd").Parse(cmdArgsTmpl)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	if err := t.Execute(buf, data); err != nil {
		return nil, err
	}
	step.Args = []string{"-c", buf.String()}

	if err := i.db.Model(&models.Host{}).Where("id = ?", host.ID.String()).
		Update("installer_version", i.instructionConfig.InstallerImage).Error; err != nil {
		return nil, err
	}

	return step, nil
}

func getBootDevice(log logrus.FieldLogger, hwValidator hardware.Validator, host models.Host) (string, error) {
	disks, err := hwValidator.GetHostValidDisks(&host)
	if err != nil || len(disks) == 0 {
		err := fmt.Errorf("Failed to get valid disks on host with id %s", host.ID)
		log.Errorf("Failed to get valid disks on host with id %s", host.ID)
		return "", err
	}
	return fmt.Sprintf("/dev/%s", disks[0].Name), nil
}
