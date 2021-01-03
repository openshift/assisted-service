package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/versions"

	"github.com/sirupsen/logrus"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
)

type installCmd struct {
	baseCmd
	db                *gorm.DB
	hwValidator       hardware.Validator
	ocRelease         oc.Release
	instructionConfig InstructionConfig
	eventsHandler     events.Handler
	versionsHandler   versions.Handler
}

var podmanBaseCmd = [...]string{
	"podman", "run", "--privileged", "--pid=host", "--net=host", "--name=assisted-installer",
	"--volume", "/dev:/dev:rw",
	"--volume", "/opt:/opt:rw",
	"--volume", "/var/log:/var/log:rw",
	"--volume", "/run/systemd/journal/socket:/run/systemd/journal/socket",
	"--env=PULL_SECRET_TOKEN",
}

func NewInstallCmd(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, eventsHandler events.Handler, versionsHandler versions.Handler) *installCmd {
	return &installCmd{
		baseCmd:           baseCmd{log: log},
		db:                db,
		hwValidator:       hwValidator,
		ocRelease:         ocRelease,
		instructionConfig: instructionConfig,
		eventsHandler:     eventsHandler,
		versionsHandler:   versionsHandler,
	}
}

func (i *installCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	step := &models.Step{}
	step.StepType = models.StepTypeInstall
	step.Command = "bash"

	//get openshift version and HighAvailabilityMode
	var cluster common.Cluster
	if err := i.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		i.log.Errorf("failed to get cluster %s", host.ClusterID)
		return nil, err
	}

	bootdevice, err := getBootDevice(i.log, i.hwValidator, *host)
	if err != nil {
		return nil, err
	}

	fullCmd, err := i.getFullInstallerCommand(&cluster, host, bootdevice)
	if err != nil {
		return nil, err
	}

	unbootableCmd, err := i.getDiskUnbootableCmd(ctx, *host)
	if err != nil {
		return nil, err
	}

	fioPerfCheckCmd := NewFioPerfCheckCmd(i.log, i.instructionConfig.FioPerfCheckImage, bootdevice, FioDurationThreshold)
	step.Args = []string{"-c", unbootableCmd + fioPerfCheckCmd.GetCommandString() + fullCmd}

	if _, err := UpdateHost(i.log, i.db, host.ClusterID, *host.ID, *host.Status,
		"installer_version", i.instructionConfig.InstallerImage, "installation_disk_path", bootdevice); err != nil {
		return nil, err
	}

	return []*models.Step{step}, nil
}

func (i *installCmd) getFullInstallerCommand(cluster *common.Cluster, host *models.Host, bootdevice string) (string, error) {

	role := host.Role
	if host.Bootstrap {
		role = models.HostRoleBootstrap
	}

	haMode := models.ClusterHighAvailabilityModeFull
	if cluster.HighAvailabilityMode != nil {
		haMode = *cluster.HighAvailabilityMode
	}

	releaseImage, err := i.versionsHandler.GetReleaseImage(cluster.OpenshiftVersion)
	if err != nil {
		return "", err
	}

	mcoImage, err := i.ocRelease.GetMCOImage(i.log, releaseImage, i.instructionConfig.ReleaseImageMirror, cluster.PullSecret)
	if err != nil {
		return "", err
	}

	i.log.Infof("Install command releaseImage: %s, mcoImage: %s", releaseImage, mcoImage)

	podmanCmd := podmanBaseCmd[:]
	installerCmd := []string{
		"--role", string(role),
		"--cluster-id", string(host.ClusterID),
		"--host-id", string(*host.ID),
		"--boot-device", bootdevice,
		"--url", i.instructionConfig.ServiceBaseURL,
		"--openshift-version", cluster.OpenshiftVersion,
		"--high-availability-mode", haMode,
		"--mco-image", mcoImage,
		"--controller-image", i.instructionConfig.ControllerImage,
		"--agent-image", i.instructionConfig.InventoryImage,
		"--insecure", strconv.FormatBool(i.instructionConfig.SkipCertVerification),
	}

	if i.hasCACert() {
		podmanCmd = append(podmanCmd, "--volume", fmt.Sprintf("%s:%s:rw", common.HostCACertPath, common.HostCACertPath))
		installerCmd = append(installerCmd, "--cacert", common.HostCACertPath)
	}

	if i.instructionConfig.InstallationTimeout != 0 {
		installerCmd = append(installerCmd, "--installation-timeout", strconv.Itoa(int(i.instructionConfig.InstallationTimeout)))
	}

	if host.InstallerArgs != "" {
		installerCmd = append(installerCmd, "--installer-args", host.InstallerArgs)
	}

	noProxyArgs := i.getProxyArguments(cluster.Name, cluster.BaseDNSDomain, cluster.HTTPProxy, cluster.HTTPSProxy, cluster.NoProxy)
	if len(noProxyArgs) > 0 {
		installerCmd = append(installerCmd, noProxyArgs...)
	}

	if i.instructionConfig.ServiceIPs != "" {
		installerCmd = append(installerCmd, "--service-ips", i.instructionConfig.ServiceIPs)
	}

	return fmt.Sprintf("%s %s %s", shellescape.QuoteCommand(podmanCmd), i.instructionConfig.InstallerImage,
		shellescape.QuoteCommand(installerCmd)), nil
}

func (i *installCmd) getProxyArguments(clusterName, baseDNSDomain, httpProxy, httpsProxy, noProxy string) []string {
	cmd := make([]string, 0)

	if httpProxy != "" || httpsProxy != "" {
		if httpProxy != "" {
			cmd = append(cmd, "--http-proxy", httpProxy)
		}
		if httpsProxy != "" {
			cmd = append(cmd, "--https-proxy", httpsProxy)
		}

		// if we set proxy we need to update assisted installer no proxy with no proxy params as installer.
		// it must be able to connect to api int. Added this way for not to pass name and base domain
		noProxyUpdated := []string{noProxy, "127.0.0.1",
			"localhost",
			".svc",
			".cluster.local",
			fmt.Sprintf("api-int.%s.%s", clusterName, baseDNSDomain)}
		cmd = append(cmd, "--no-proxy", strings.Join(noProxyUpdated, ","))
	}

	return cmd
}

func (i *installCmd) hasCACert() bool {
	return i.instructionConfig.ServiceCACertPath != ""
}

func getBootDevice(log logrus.FieldLogger, hwValidator hardware.Validator, host models.Host) (string, error) {
	if host.InstallationDiskPath != "" {
		return host.InstallationDiskPath, nil
	}

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

func (i *installCmd) getDiskUnbootableCmd(ctx context.Context, host models.Host) (string, error) {
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		i.log.Errorf("Failed to get inventory from host with id %s", host.ID)
		return "", err
	}
	formatCmds := ""
	for _, disk := range inventory.Disks {
		isFcIscsi := strings.Contains(disk.ByPath, "-fc-") || strings.Contains(disk.ByPath, "-iscsi-")
		if disk.Bootable && !isFcIscsi {
			dev := GetDeviceFullName(disk.Name)
			formatCmds += fmt.Sprintf("dd if=/dev/zero of=%s bs=512 count=1 ; ", dev)
			i.eventsHandler.AddEvent(
				ctx,
				host.ClusterID,
				host.ID,
				models.EventSeverityInfo,
				fmt.Sprintf("Performing quick format of disk %s", dev),
				time.Now())
		}
	}
	return formatCmds, nil
}
