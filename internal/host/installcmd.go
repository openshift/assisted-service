package host

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/internal/hostutil"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"

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

	cmdArgsTmpl := "podman run -v /dev:/dev:rw -v /opt:/opt:rw {{if .HOST_CA_CERT_PATH}}-v {{.HOST_CA_CERT_PATH}}:{{.HOST_CA_CERT_PATH}}:rw {{end}}-v /run/systemd/journal/socket:/run/systemd/journal/socket --privileged --pid=host --net=host " +
		"-v /var/log:/var/log:rw --env PULL_SECRET_TOKEN --name assisted-installer {{.INSTALLER}} --role {{.ROLE}} --cluster-id {{.CLUSTER_ID}} " +
		"--boot-device {{.BOOT_DEVICE}} --host-id {{.HOST_ID}} --openshift-version {{.OPENSHIFT_VERSION}} " +
		"--controller-image {{.CONTROLLER_IMAGE}} --url {{.BASE_URL}} --insecure={{.SKIP_CERT_VERIFICATION}} --agent-image {{.AGENT_IMAGE}}"
	data := map[string]string{
		"BASE_URL":               strings.TrimSpace(i.instructionConfig.ServiceBaseURL),
		"CLUSTER_ID":             string(host.ClusterID),
		"HOST_ID":                string(*host.ID),
		"ROLE":                   string(role),
		"INSTALLER":              i.instructionConfig.InstallerImage,
		"CONTROLLER_IMAGE":       i.instructionConfig.ControllerImage,
		"BOOT_DEVICE":            "",
		"OPENSHIFT_VERSION":      cluster.OpenshiftVersion,
		"SKIP_CERT_VERIFICATION": strconv.FormatBool(i.instructionConfig.SkipCertVerification),
		"AGENT_IMAGE":            i.instructionConfig.InventoryImage,
	}

	if i.instructionConfig.InstallationTimeout != 0 {
		cmdArgsTmpl = cmdArgsTmpl + " --installation-timeout {{.INSTALLATION_TIMEOUT}}"
		data["INSTALLATION_TIMEOUT"] = strconv.Itoa(int(i.instructionConfig.InstallationTimeout))
	}

	hostname, _ := hostutil.GetCurrentHostName(host)
	if hostname != "" {
		cmdArgsTmpl = cmdArgsTmpl + " --host-name {{.HOST_NAME}}"
		data["HOST_NAME"] = hostname
	}

	if cluster.HTTPProxy != "" || cluster.HTTPSProxy != "" {
		if cluster.HTTPProxy != "" {
			cmdArgsTmpl = cmdArgsTmpl + " --http-proxy {{.HTTP_PROXY}}"
			data["HTTP_PROXY"] = cluster.HTTPProxy
		}
		if cluster.HTTPSProxy != "" {
			cmdArgsTmpl = cmdArgsTmpl + " --https-proxy {{.HTTPS_PROXY}}"
			data["HTTPS_PROXY"] = cluster.HTTPSProxy
		}
		cmdArgsTmpl = cmdArgsTmpl + " --no-proxy {{.NO_PROXY}}"
		// if we set proxy we need to update assisted installer no proxy with no proxy params as installer.
		// it must be able to connect to api int. Added this way for not to pass name and base domain
		noProxy := []string{cluster.NoProxy, "127.0.0.1",
			"localhost",
			".svc",
			".cluster.local",
			fmt.Sprintf("api-int.%s.%s", cluster.Name, cluster.BaseDNSDomain)}
		data["NO_PROXY"] = strings.Join(noProxy, ",")
	}

	if i.hasCACert() {
		cmdArgsTmpl = cmdArgsTmpl + " --cacert {{.HOST_CA_CERT_PATH}}"
		data["HOST_CA_CERT_PATH"] = common.HostCACertPath
	}

	if i.instructionConfig.ServiceIPs != "" {
		cmdArgsTmpl = cmdArgsTmpl + " --service-ips '{{.SERVICE_IPS}}'"
		data["SERVICE_IPS"] = strings.TrimSpace(i.instructionConfig.ServiceIPs)
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

	if _, err := UpdateHost(i.log, i.db, host.ClusterID, *host.ID, *host.Status,
		"installer_version", i.instructionConfig.InstallerImage, "installation_disk_path", bootdevice); err != nil {
		return nil, err
	}

	return step, nil
}

func (i *installCmd) hasCACert() bool {
	return i.instructionConfig.ServiceCACertPath != ""
}

func getBootDevice(log logrus.FieldLogger, hwValidator hardware.Validator, host models.Host) (string, error) {
	disks, err := hwValidator.GetHostValidDisks(&host)
	if err != nil || len(disks) == 0 {
		err := errors.Errorf("Failed to get valid disks on host with id %s", host.ID)
		log.Errorf("Failed to get valid disks on host with id %s", host.ID)
		return "", err
	}
	return GetDeviceFullName(disks[0].Name), nil
}

func GetDeviceFullName(name string) string {
	return fmt.Sprintf("/dev/%s", name)
}
