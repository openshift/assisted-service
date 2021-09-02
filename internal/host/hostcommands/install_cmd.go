package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
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

	cluster, err := common.GetClusterFromDBWithoutDisabledHosts(i.db, *host.ClusterID)
	if err != nil {
		i.log.Errorf("failed to get cluster %s", host.ClusterID)
		return nil, err
	}

	bootdevice, err := hardware.GetBootDevice(i.hwValidator, host)
	if err != nil {
		return nil, err
	}

	fullCmd, err := i.getFullInstallerCommand(cluster, host, bootdevice)
	if err != nil {
		return nil, err
	}

	unbootableCmd, err := i.getDiskUnbootableCmd(ctx, *host)
	if err != nil {
		return nil, err
	}

	step.Args = []string{"-c", unbootableCmd + fullCmd}

	if _, err := hostutil.UpdateHost(i.log, i.db, host.InfraEnvID, *host.ID, *host.Status,
		"installer_version", i.instructionConfig.InstallerImage); err != nil {
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

	ocpVersion, err := i.versionsHandler.GetOpenshiftVersion(cluster.OpenshiftVersion, cluster.CPUArchitecture)
	if err != nil {
		return "", err
	}

	mcoImage, err := i.ocRelease.GetMCOImage(i.log, *ocpVersion.ReleaseImage, i.instructionConfig.ReleaseImageMirror, cluster.PullSecret)
	if err != nil {
		return "", err
	}

	mustGatherMap, err := i.versionsHandler.GetMustGatherImages(cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret)
	if err != nil {
		return "", err
	}
	mustGatherImages, err := i.getMustGatherArgument(mustGatherMap)
	if err != nil {
		return "", err
	}

	i.log.Infof("Install command releaseImage: %s, mcoImage: %s", *ocpVersion.ReleaseImage, mcoImage)

	podmanCmd := podmanBaseCmd[:]
	installerCmd := []string{
		"--role", string(role),
		"--infra-env-id", host.InfraEnvID.String(),
		"--cluster-id", host.ClusterID.String(),
		"--host-id", string(*host.ID),
		"--boot-device", bootdevice,
		"--url", i.instructionConfig.ServiceBaseURL,
		"--openshift-version", cluster.OpenshiftVersion,
		"--high-availability-mode", haMode,
		"--mco-image", mcoImage,
		"--controller-image", i.instructionConfig.ControllerImage,
		"--agent-image", i.instructionConfig.AgentImage,
		"--must-gather-image", mustGatherImages,
	}

	/*
		boolean flag must be used either without value (flag present means True) or in the format of <flag>=True|False.
		format <boolean flag> <value> is not supported by golang flag package and will cause the flags processing to finish
		before processing the rest of the input flags
	*/
	if i.instructionConfig.SkipCertVerification {
		installerCmd = append(installerCmd, "--insecure")
	}

	if i.instructionConfig.CheckClusterVersion {
		installerCmd = append(installerCmd, "--check-cluster-version")
	}

	if i.hasCACert() {
		podmanCmd = append(podmanCmd, "--volume", fmt.Sprintf("%s:%s:rw", common.HostCACertPath, common.HostCACertPath))
		installerCmd = append(installerCmd, "--cacert", common.HostCACertPath)
	}

	hostInstallerArgs, err := constructHostInstallerArgs(cluster, host, i.log)
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

func (i *installCmd) getMustGatherArgument(mustGatherMap versions.MustGatherVersion) (string, error) {
	//for backward compatability, if must gather images map contains only the ocp must gather
	//we shall send a single image. otherwise, we shall send a json structure holding all the
	//relevant images
	if len(mustGatherMap) == 1 && mustGatherMap["ocp"] != "" {
		return mustGatherMap["ocp"], nil
	}

	arg, err := json.Marshal(mustGatherMap)
	if err != nil {
		i.log.WithError(err).Errorf("can not encode must-gather image map")
		return "", err
	}
	return string(arg), nil
}

func (i *installCmd) getProxyArguments(clusterName, baseDNSDomain, httpProxy, httpsProxy, noProxy string) []string {
	cmd := make([]string, 0)
	if httpProxy == "" && httpsProxy == "" {
		return cmd
	}

	if httpProxy != "" {
		cmd = append(cmd, "--http-proxy", httpProxy)
	}

	if httpsProxy != "" {
		cmd = append(cmd, "--https-proxy", httpsProxy)
	}

	noProxyTrim := strings.TrimSpace(noProxy)
	if noProxyTrim == "*" {
		cmd = append(cmd, "--no-proxy", noProxyTrim)
	} else {

		noProxyUpdated := []string{}
		if noProxyTrim != "" {
			noProxyUpdated = append(noProxyUpdated, noProxyTrim)
		}
		// if we set proxy we need to update assisted installer no proxy with no proxy params as installer.
		// it must be able to connect to api int. Added this way for not to pass name and base domain
		noProxyUpdated = append(noProxyUpdated,
			"127.0.0.1",
			"localhost",
			".svc",
			".cluster.local",
			fmt.Sprintf("api-int.%s.%s", clusterName, baseDNSDomain))
		cmd = append(cmd, "--no-proxy", strings.Join(noProxyUpdated, ","))
	}

	return cmd
}

func (i *installCmd) hasCACert() bool {
	return i.instructionConfig.ServiceCACertPath != ""
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
		isMmcblk := strings.Contains(disk.ByPath, "mmcblk") //mmc devices should be treated as removable
		if disk.Bootable && !disk.Removable && !isMmcblk && !isFcIscsi && !disk.IsInstallationMedia {
			formatCmds += fmt.Sprintf("dd if=/dev/zero of=%s bs=512 count=1 ; ", hostutil.GetDeviceIdentifier(disk))
			i.eventsHandler.AddEvent(
				ctx,
				*host.ClusterID,
				host.ID,
				models.EventSeverityInfo,
				fmt.Sprintf("%s: Performing quick format of disk %s(%s)", hostutil.GetHostnameForMsg(&host), disk.Name, hostutil.GetDeviceIdentifier(disk)),
				time.Now())
		}
	}
	return formatCmds, nil
}

/*
	This function combines existing InstallerArgs ( set by user for his own reasons ) with the
	--copy-network argument needed by the static ips configuration. In case user has also
	set --copy-network, function will set only one such argument. It also append an arg that
	controls DHCP depending on the IP stack being used.
*/
func constructHostInstallerArgs(cluster *common.Cluster, host *models.Host, log logrus.FieldLogger) (string, error) {

	var installerArgs []string
	if host.InstallerArgs != "" {
		err := json.Unmarshal([]byte(host.InstallerArgs), &installerArgs)
		if err != nil {
			return "", err
		}
	}

	installerArgs, err := appendDHCPArgs(cluster, host, installerArgs, log)
	if err != nil {
		return "", err
	}

	if cluster.StaticNetworkConfigured && !funk.Contains(installerArgs, "--copy-network") {
		// network not configured statically or
		// installer args already contain command for network configuration
		installerArgs = append(installerArgs, "--copy-network")
	}

	return toJSONString(installerArgs)
}

func appendDHCPArgs(cluster *common.Cluster, host *models.Host, installerArgs []string, log logrus.FieldLogger) ([]string, error) {

	if hasUserConfiguredIP(installerArgs) {
		return installerArgs, nil
	}

	machineNetworkCIDR := network.GetMachineCidrForUserManagedNetwork(cluster, log)
	if machineNetworkCIDR != "" {
		ipv6 := network.IsIPv6CIDR(machineNetworkCIDR)
		log.Debugf("Machine network CIDR: %s. IPv6: %t", machineNetworkCIDR, ipv6)
		inventory, err := common.UnmarshalInventory(host.Inventory)
		if err != nil {
			return nil, err
		}
		_, network, err := net.ParseCIDR(machineNetworkCIDR)
		if err != nil {
			return installerArgs, err
		}
		for _, nic := range inventory.Interfaces {
			dhcpArgs, err := getDHCPArgPerNIC(network, nic, ipv6, host.ID, log)
			if err != nil {
				return installerArgs, err
			}
			installerArgs = append(installerArgs, dhcpArgs...)
		}
		return installerArgs, nil
	}

	if swag.StringValue(cluster.Kind) != models.ClusterKindAddHostsCluster {
		return installerArgs, errors.Errorf("cannot determine machine network address family")
	}
	if v4, v6, err := network.GetHostAddressFamilies(host); err != nil {
		return installerArgs, err
	} else if v4 && v6 {
		log.Warnf("Cannot set DHCP kernel argument for host %s of day-2 cluster %s with dual IP stack. Not doing so may result in failing to download ignition or ISO", host.ID, *cluster.ID)
	}
	return installerArgs, nil
}

func getDHCPArgPerNIC(network *net.IPNet, nic *models.Interface, ipv6 bool, hostID *strfmt.UUID, log logrus.FieldLogger) ([]string, error) {
	args := make([]string, 0)
	var addresses []string
	var dhcp string
	if ipv6 {
		addresses = nic.IPV6Addresses
		dhcp = "dhcp6"
	} else {
		addresses = nic.IPV4Addresses
		dhcp = "dhcp"
	}
	found, err := findAnyInCIDR(network, addresses)
	if err != nil {
		return nil, err
	}
	if found {
		log.Debugf("Host %s: Added kernel argument ip=%s:%s", hostID, nic.Name, dhcp)
		return append(args, "--append-karg", fmt.Sprintf("ip=%s:%s", nic.Name, dhcp)), nil
	}
	return args, nil
}

func findAnyInCIDR(network *net.IPNet, addresses []string) (bool, error) {
	for _, a := range addresses {
		ip, _, err := net.ParseCIDR(a)
		if err != nil {
			return false, err
		}
		if network.Contains(ip) {
			return true, nil
		}
	}
	return false, nil
}

func hasUserConfiguredIP(args []string) bool {
	// check if the user has configured any ip arguments manually
	// https://man7.org/linux/man-pages/man7/dracut.cmdline.7.html
	_, result := funk.FindString(args, func(s string) bool {
		return strings.HasPrefix(s, "ip=")
	})
	return result
}

func toJSONString(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	argsBytes, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(argsBytes), nil
}
