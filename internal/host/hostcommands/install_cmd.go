package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

type installCmd struct {
	baseCmd
	db                *gorm.DB
	hwValidator       hardware.Validator
	ocRelease         oc.Release
	instructionConfig InstructionConfig
	eventsHandler     eventsapi.Handler
	versionsHandler   versions.Handler
}

func NewInstallCmd(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator, ocRelease oc.Release,
	instructionConfig InstructionConfig, eventsHandler eventsapi.Handler, versionsHandler versions.Handler) *installCmd {
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

	db := i.db.Preload(common.HostsTable, "bootstrap = TRUE")
	cluster, err := common.GetClusterFromDB(common.LoadClusterTablesFromDB(db, common.HostsTable), *host.ClusterID, common.SkipEagerLoading)
	if err != nil {
		i.log.Errorf("failed to get cluster %s", host.ClusterID)
		return nil, err
	}

	bootdevice, err := hardware.GetBootDevice(i.hwValidator, host)
	if err != nil {
		return nil, err
	}

	var infraEnv *common.InfraEnv = nil
	if host.InfraEnvID != "" {
		infraEnv, err = common.GetInfraEnvFromDB(i.db, host.InfraEnvID)
		if err != nil {
			return nil, err
		}
	}

	inventory, err := common.UnmarshalInventory(host.Inventory)
	if err != nil {
		return nil, err
	}

	fullCmd, err := i.getFullInstallerCommand(ctx, cluster, host, inventory, infraEnv, bootdevice)
	if err != nil {
		return nil, err
	}

	step.Args = []string{fullCmd}

	if _, err := hostutil.UpdateHost(i.log, i.db, host.InfraEnvID, *host.ID, *host.Status,
		"installer_version", i.instructionConfig.InstallerImage); err != nil {
		return nil, err
	}

	return []*models.Step{step}, nil
}

func (i *installCmd) getFullInstallerCommand(ctx context.Context, cluster *common.Cluster, host *models.Host, inventory *models.Inventory, infraEnv *common.InfraEnv, bootdevice string) (string, error) {
	role := common.GetEffectiveRole(host)
	if host.Bootstrap {
		role = models.HostRoleBootstrap
	}

	haMode := models.ClusterHighAvailabilityModeFull
	if cluster.HighAvailabilityMode != nil {
		haMode = *cluster.HighAvailabilityMode
	}

	request := models.InstallCmdRequest{
		Role:                 &role,
		ClusterID:            host.ClusterID,
		HostID:               host.ID,
		InfraEnvID:           &host.InfraEnvID,
		HighAvailabilityMode: &haMode,
		ControllerImage:      swag.String(i.instructionConfig.ControllerImage),
		CheckCvo:             swag.Bool(i.instructionConfig.CheckClusterVersion),
		InstallerImage:       swag.String(i.instructionConfig.InstallerImage),
		BootDevice:           swag.String(bootdevice),
	}

	// those flags are not used on day2 installation
	if swag.StringValue(cluster.Kind) != models.ClusterKindAddHostsCluster {
		releaseImage, err := i.versionsHandler.GetReleaseImage(ctx, cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret)
		if err != nil {
			return "", err
		}

		request.McoImage, err = i.ocRelease.GetMCOImage(i.log, *releaseImage.URL, i.instructionConfig.ReleaseImageMirror, cluster.PullSecret)
		if err != nil {
			return "", err
		}
		i.log.Infof("Install command releaseImage: %s, mcoImage: %s", *releaseImage.URL, request.McoImage)

		mustGatherMap, err := i.versionsHandler.GetMustGatherImages(cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret)
		if err != nil {
			return "", err
		}
		request.MustGatherImage, err = i.getMustGatherArgument(mustGatherMap)
		if err != nil {
			return "", err
		}

		request.OpenshiftVersion = cluster.OpenshiftVersion
	}

	hostInstallerArgs, err := constructHostInstallerArgs(cluster, host, inventory, infraEnv, i.log)
	if err != nil {
		return "", err
	}

	if hostInstallerArgs != "" {
		request.InstallerArgs = hostInstallerArgs
	}

	if hostutil.SaveDiskPartitionsIsSet(hostInstallerArgs) {
		request.SkipInstallationDiskCleanup = true
		if host.SkipFormattingDisks != "" {
			host.SkipFormattingDisks += fmt.Sprintf(",%s", bootdevice)
		} else {
			host.SkipFormattingDisks = bootdevice
		}
	}

	request.DisksToFormat = i.getDisksToFormat(ctx, host, inventory)

	request.Proxy = i.getProxyArguments(cluster.Name, cluster.BaseDNSDomain, cluster.HTTPProxy, cluster.HTTPSProxy, cluster.NoProxy)

	if i.instructionConfig.ServiceIPs != "" {
		request.ServiceIps = strings.Split(i.instructionConfig.ServiceIPs, ",")
	}

	b, err := json.Marshal(&request)
	if err != nil {
		i.log.WithError(err).Warn("Json marshal")
		return "", err
	}

	return string(b), nil
}

func (i *installCmd) getProxyArguments(clusterName, baseDNSDomain, httpProxy, httpsProxy, noProxy string) *models.Proxy {
	var proxy models.Proxy
	if httpProxy == "" && httpsProxy == "" {
		return nil
	}

	if httpProxy != "" {
		proxy.HTTPProxy = swag.String(httpProxy)
	}

	if httpsProxy != "" {
		proxy.HTTPSProxy = swag.String(httpsProxy)
	}

	noProxyTrim := strings.TrimSpace(noProxy)
	if noProxyTrim == "*" {
		proxy.NoProxy = swag.String(noProxyTrim)
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
		proxy.NoProxy = swag.String(strings.Join(noProxyUpdated, ","))
	}

	return &proxy
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

func (i *installCmd) getDisksToFormat(ctx context.Context, host *models.Host, inventory *models.Inventory) []string {
	allFormattingCandidateDisks := common.GetDisksToBeFormatted(inventory)
	skippedDisksIdentifiers := common.GetSkippedFormattingDiskIdentifiers(host)

	// unskippedDisksIdentifiers will eventually contain all disk identifiers
	// that appear in allFormattingCandidateDisks but that do not also appear
	// in skippedDisksIdentifiers
	unskippedDisksIdentifiers := make([]string, 0, len(allFormattingCandidateDisks))

	for _, disk := range allFormattingCandidateDisks {
		identifier := common.GetDeviceIdentifier(disk)
		if !funk.Contains(skippedDisksIdentifiers, identifier) {
			eventgen.SendQuickDiskFormatPerformedEvent(ctx, i.eventsHandler, *host.ID, host.InfraEnvID, host.ClusterID,
				hostutil.GetHostnameForMsg(host), disk.Name, identifier)

			unskippedDisksIdentifiers = append(unskippedDisksIdentifiers, identifier)
		} else {
			eventgen.SendQuickDiskFormatSkippedEvent(ctx, i.eventsHandler, *host.ID, host.InfraEnvID, host.ClusterID,
				hostutil.GetHostnameForMsg(host), disk.Name, identifier)
		}
	}

	return unskippedDisksIdentifiers
}

/*
This function combines existing InstallerArgs ( set by user for his own reasons ) with the
--copy-network argument needed by the static ips configuration. In case user has also
set --copy-network, function will set only one such argument. It also append an arg that
controls DHCP depending on the IP stack being used.
*/
func constructHostInstallerArgs(cluster *common.Cluster, host *models.Host, inventory *models.Inventory, infraEnv *common.InfraEnv, log logrus.FieldLogger) (string, error) {

	var installerArgs []string
	var err error
	hasStaticNetwork := (infraEnv != nil && infraEnv.StaticNetworkConfig != "") || cluster.StaticNetworkConfigured

	if host.InstallerArgs != "" {
		err = json.Unmarshal([]byte(host.InstallerArgs), &installerArgs)
		if err != nil {
			return "", err
		}
	}

	if !hasStaticNetwork {
		// The set of ip=<nic>:dhcp kernel arguments should be added only if there is no static
		// network configured by the user. This is because this parameter will configure RHCOS to
		// try to obtain IP address from the DHCP server even if we provide a static addressing.
		// As in majority of cases it's not an issue because of the priorities set in the config
		// of NetworkManager, in some specific scenarios (e.g. BZ-2106110) this causes machines to
		// lose their connectivity because priorities get mixed.
		installerArgs, err = appendDHCPArgs(cluster, host, inventory, installerArgs, log)
		if err != nil {
			return "", err
		}
	}

	for _, disk := range inventory.Disks {
		if disk.DriveType == models.DriveTypeMultipath && disk.ID == host.InstallationDiskID {
			installerArgs = append(installerArgs, "--append-karg", "root=/dev/disk/by-label/dm-mpath-root", "--append-karg", "rw", "--append-karg", "rd.multipath=default")
		}
	}

	if hasStaticNetwork && !funk.Contains(installerArgs, "--copy-network") {
		// network not configured statically or
		// installer args already contain command for network configuration
		installerArgs = append(installerArgs, "--copy-network")
	}

	return toJSONString(installerArgs)
}

func appendDHCPArgs(cluster *common.Cluster, host *models.Host, inventory *models.Inventory, installerArgs []string, log logrus.FieldLogger) ([]string, error) {

	if hasUserConfiguredIP(installerArgs) {
		return installerArgs, nil
	}

	machineNetworkCIDR := network.GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
	if machineNetworkCIDR != "" {
		ipv6 := network.IsIPv6CIDR(machineNetworkCIDR)

		log.Debugf("Machine network CIDR: %s. IPv6: %t", machineNetworkCIDR, ipv6)
		_, ipNet, err := net.ParseCIDR(machineNetworkCIDR)
		if err != nil {
			return installerArgs, err
		}
		for _, nic := range inventory.Interfaces {
			dhcpArgs, err := getDHCPArgPerNIC(ipNet, nic, ipv6, network.CheckIfClusterIsDualStack(cluster), host.ID, log)
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

func getDHCPArgPerNIC(network *net.IPNet, nic *models.Interface, ipv6 bool, dualStack bool, hostID *strfmt.UUID, log logrus.FieldLogger) ([]string, error) {
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
		if dualStack {
			dhcp = "dhcp,dhcp6"
		}
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
