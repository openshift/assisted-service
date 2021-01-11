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
	"github.com/thoas/go-funk"
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

	config := FioPerfCheckConfig{
		ServiceBaseURL:       i.instructionConfig.ServiceBaseURL,
		ClusterID:            string(host.ClusterID),
		HostID:               string(*host.ID),
		UseCustomCACert:      i.hasCACert(),
		FioPerfCheckImage:    i.instructionConfig.FioPerfCheckImage,
		SkipCertVerification: i.instructionConfig.SkipCertVerification,
		Path:                 bootdevice,
		DurationThresholdMs:  FioDurationThresholdMs,
	}

	fioPerfCheckCmd := NewFioPerfCheckCmd(i.log, config)
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
	}

	/*
		boolean flag must be used either without value (flag present means True) or in the format of <flag>=True|False.
		format <boolean flag> <value> is not supported by golang flag package and will cause the flags processing to finish
		before processing the rest of the input flags
	*/
	if i.instructionConfig.SkipCertVerification {
		installerCmd = append(installerCmd, "--insecure")
	}

	if i.hasCACert() {
		podmanCmd = append(podmanCmd, "--volume", fmt.Sprintf("%s:%s:rw", common.HostCACertPath, common.HostCACertPath))
		installerCmd = append(installerCmd, "--cacert", common.HostCACertPath)
	}

	if i.instructionConfig.InstallationTimeout != 0 {
		installerCmd = append(installerCmd, "--installation-timeout", strconv.Itoa(int(i.instructionConfig.InstallationTimeout)))
	}

	hostInstallerArgs, err := constructHostInstallerArgs(cluster, host)
	if err != nil {
		return "", err
	}

	if hostInstallerArgs != "" {
		installerCmd = append(installerCmd, "--installer-args", hostInstallerArgs)
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

	// TODO: We generally shouldn't reach this point on any version containing this comment.
	//  It might happen a few times while this version is first rolled out. Remove the call to GetHostValidDisks when
	//  that new version has been running for a sufficiently long time.
	//  Note that instead of a call to GetHostValidDisks, an error should occur. That's because if the installation disk
	//  path is empty, it means there are no valid disks to install on.
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
		if disk.Bootable && !isFcIscsi && !disk.IsInstallationMedia {
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

/*
	This function combines existing InstallerArgs ( set by user for his own reasons ) with the
	--copy-nework argument needed by the static ips configuration. In case user has also
	set --copy-nework, function will set only one such argument
*/
func constructHostInstallerArgs(cluster *common.Cluster, host *models.Host) (string, error) {
	if cluster.ImageInfo.StaticIpsConfig == "" {
		return host.InstallerArgs, nil
	}

	if host.InstallerArgs == "" {
		newArgs := []string{"--copy-network"}
		argsBytes, err := json.Marshal(newArgs)
		if err != nil {
			return "", err
		}
		return string(argsBytes), nil
	}

	var currentInstallerArgs []string
	err := json.Unmarshal([]byte(host.InstallerArgs), &currentInstallerArgs)
	if err != nil {
		return "", err
	}

	// installer args already contain  command for network configuration
	if funk.Contains(currentInstallerArgs, "--copy-network") {
		return host.InstallerArgs, nil
	}

	currentInstallerArgs = append(currentInstallerArgs, "--copy-network")
	argsBytes, err := json.Marshal(currentInstallerArgs)
	if err != nil {
		return "", err
	}
	return string(argsBytes), nil
}
