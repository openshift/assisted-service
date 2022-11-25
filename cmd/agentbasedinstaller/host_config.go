package agentbasedinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-openapi/strfmt"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	errorutil "github.com/openshift/assisted-service/pkg/error"
	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

func ApplyHostConfigs(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, hostConfigs HostConfigs, infraEnvID strfmt.UUID) ([]Failure, error) {
	hostList, err := bmInventory.Installer.V2ListHosts(ctx, installer.NewV2ListHostsParams().WithInfraEnvID(infraEnvID))
	if err != nil {
		return nil, fmt.Errorf("Failed to list hosts: %w", errorutil.GetAssistedError(err))
	}

	failures := []Failure{}

	for _, host := range hostList.Payload {
		hostConfig, inventory, err := getHostConfigAndInventory(ctx, log, host, hostConfigs)
		if err != nil {
			if fail, ok := err.(Failure); ok {
				failures = append(failures, fail)
				log.Error(err.Error())
				continue
			} else {
				return failures, err
			}
		}

		if hostConfig == nil {
			continue
		}

		if err := applyHostConfig(ctx, log, bmInventory, host, inventory, hostConfig); err != nil {
			if fail, ok := err.(Failure); ok {
				failures = append(failures, fail)
				log.Error(err.Error())
			} else {
				return failures, err
			}
		}

		if err := applyInstallerArgOverrides(ctx, log, bmInventory, host, inventory, hostConfig); err != nil {
			if fail, ok := err.(Failure); ok {
				failures = append(failures, fail)
				log.Error(err.Error())
			} else {
				return failures, err
			}
		}
	}

	missing := hostConfigs.missing(log)
	if len(missing) > 0 {
		log.Info("Not all hosts present yet")
		for _, mh := range missing {
			failures = append(failures, mh)
		}
	} else {
		log.Info("All expected hosts found")
	}
	return failures, nil
}

func getHostConfigAndInventory(ctx context.Context, log *log.Logger, host *models.Host, hostConfigs HostConfigs) (*hostConfig, *models.Inventory, error) {
	log.Infof("Getting configuration for host %s", *host.ID)

	if len(host.Inventory) == 0 {
		log.Info("Inventory information not yet available")
		return nil, nil, nil
	}

	inventory := &models.Inventory{}
	err := inventory.UnmarshalBinary([]byte(host.Inventory))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal host inventory: %w", err)
	}

	config := hostConfigs.findHostConfig(*host.ID, inventory)
	if config == nil {
		return nil, nil, nil
	}

	return config, inventory, nil
}

func applyHostConfig(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, host *models.Host, inventory *models.Inventory, config *hostConfig) error {
	log.Infof("Checking configuration for host %s", *host.ID)

	updateParams := &models.HostUpdateParams{}
	changed := false

	rdh, err := config.RootDeviceHints()
	if err != nil {
		return err
	}
	if applyRootDeviceHints(log, host, inventory, rdh, updateParams) {
		changed = true
	}

	role, err := config.Role()
	if err != nil {
		return err
	}
	if applyRole(log, host, inventory, role, updateParams) {
		changed = true
	}

	if !changed {
		log.Info("No configuration changes needed")
		return nil
	}

	log.Info("Updating host")
	params := installer.NewV2UpdateHostParams().
		WithHostID(*host.ID).
		WithInfraEnvID(host.InfraEnvID).
		WithHostUpdateParams(updateParams)
	_, err = bmInventory.Installer.V2UpdateHost(ctx, params)
	if err != nil {
		if errorResponse, ok := err.(errorutil.AssistedServiceErrorAPI); ok {
			return &UpdateFailure{
				response:  errorResponse,
				params:    updateParams,
				host:      host,
				inventory: inventory,
				config:    config,
			}
		}
		return fmt.Errorf("failed to update Host: %w", err)
	}
	return nil
}

func applyInstallerArgOverrides(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, host *models.Host, inventory *models.Inventory, config *hostConfig) error {
	log.Infof("Checking installer overrides for host %s", *host.ID)

	installerArgs, err := config.InstallerArgs()
	if err != nil {
		return err
	}

	if len(installerArgs) == 0 {
		log.Info("No installerArgs configured")
		return nil
	}

	var existingInstallerArgs []string
	if len(host.InstallerArgs) > 0 {
		if err = json.Unmarshal([]byte(host.InstallerArgs), &existingInstallerArgs); err != nil {
			return fmt.Errorf("failed to parse installerArgs from host inventory")
		}
	}

	needUpdate := false
	if len(installerArgs) == len(existingInstallerArgs) {
		for i := range installerArgs {
			if installerArgs[i] != existingInstallerArgs[i] {
				needUpdate = true
			}
		}
	} else {
		needUpdate = true
	}

	if !needUpdate {
		log.Info("installerArgs already configured")
		return nil
	}

	log.Info("Updating installerArgs for host")
	updateParams := &models.InstallerArgsParams{
		Args: installerArgs,
	}
	params := installer.NewV2UpdateHostInstallerArgsParams().
		WithHostID(*host.ID).
		WithInfraEnvID(host.InfraEnvID).
		WithInstallerArgsParams(updateParams)

	_, err = bmInventory.Installer.V2UpdateHostInstallerArgs(ctx, params)
	if err != nil {
		if errorResponse, ok := err.(errorutil.AssistedServiceErrorAPI); ok {
			return &UpdateInstallerArgsFailure{
				response:  errorResponse,
				params:    updateParams,
				host:      host,
				inventory: inventory,
				config:    config,
			}
		}
		return fmt.Errorf("failed to update Host: %w", err)
	}
	return nil
}

func applyRootDeviceHints(log *log.Logger, host *models.Host, inventory *models.Inventory, rdh *bmh_v1alpha1.RootDeviceHints, updateParams *models.HostUpdateParams) bool {
	acceptableDisks := hostutil.GetAcceptableDisksWithHints(inventory.Disks, rdh)
	if host.InstallationDiskID != "" {
		for _, disk := range acceptableDisks {
			if disk.ID == host.InstallationDiskID {
				log.Infof("Selected disk %s already matches root device hints", host.InstallationDiskID)
				return false
			}
		}
	}

	diskID := "/dev/not-found-by-hints"
	if len(acceptableDisks) > 0 {
		diskID = acceptableDisks[0].ID
		log.Infof("Selecting disk %s for installation", diskID)
	} else {
		log.Info("No disk found matching root device hints")
	}

	updateParams.DisksSelectedConfig = []*models.DiskConfigParams{
		{ID: &diskID, Role: models.DiskRoleInstall},
	}
	return true
}

func applyRole(log *log.Logger, host *models.Host, inventory *models.Inventory, role *string, updateParams *models.HostUpdateParams) bool {
	if role == nil {
		log.Info("No role configured")
		return false
	}

	if host.SuggestedRole == models.HostRole(*role) {
		log.Infof("Host role %s already configured", *role)
		return false
	}

	updateParams.HostRole = role
	return true
}

func LoadHostConfigs(hostConfigDir string) (HostConfigs, error) {
	log.Infof("Loading host configurations from disk in %s", hostConfigDir)

	configs := HostConfigs{}

	entries, err := os.ReadDir(hostConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("No host configuration directory found %s", hostConfigDir)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config directory %s: %w", hostConfigDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		hostPath := path.Join(hostConfigDir, e.Name())
		log.Infof("Reading directory %s", hostPath)

		macs, err := os.ReadFile(filepath.Join(hostPath, "mac_addresses"))
		if os.IsNotExist(err) {
			log.Info("No MAC Addresses file found")
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read MAC Addresses file: %w", err)
		}

		lines := strings.Split(string(macs), "\n")
		addresses := []string{}
		for _, l := range lines {
			mac := strings.TrimSpace(l)
			if len(mac) > 0 {
				addresses = append(addresses, mac)
			}
		}

		configs = append(configs, &hostConfig{
			configDir:    hostPath,
			macAddresses: addresses,
		})
	}
	return configs, nil
}

type hostConfig struct {
	configDir    string
	macAddresses []string
	hostID       strfmt.UUID
}

func (hc hostConfig) RootDeviceHints() (*bmh_v1alpha1.RootDeviceHints, error) {
	hintData, err := os.ReadFile(path.Join(hc.configDir, "root-device-hints.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("No root device hints file found for host")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read Root Device Hints file: %w", err)
	}

	rdh := &bmh_v1alpha1.RootDeviceHints{}
	if err := yaml.UnmarshalStrict(hintData, rdh); err != nil {
		return nil, fmt.Errorf("failed to parse Root Device Hints file: %w", err)
	}
	log.Info("Read root device hints file")
	return rdh, nil
}

func (hc hostConfig) Role() (*string, error) {
	roleData, err := os.ReadFile(path.Join(hc.configDir, "role"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("No role file found for host")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read role file: %w", err)
	}

	role := strings.TrimSpace(string(roleData))
	if len(role) == 0 {
		log.Info("Empty role")
		return nil, nil
	}

	log.Infof("Found role %s", role)
	return &role, nil
}

func (hc hostConfig) InstallerArgs() ([]string, error) {
	var installerArgs []string
	installerArgsData, err := os.ReadFile(path.Join(hc.configDir, "installerArgs"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("No installerArgs file found for host")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read installerArgs file: %w", err)
	}

	if err := json.Unmarshal(installerArgsData, &installerArgs); err != nil {
		return nil, fmt.Errorf("failed to parse installerArgs file: %w", err)
	}

	log.Infof("Found installerArgs %s", installerArgs)
	return installerArgs, nil
}

type HostConfigs []*hostConfig

func (configs HostConfigs) findHostConfig(hostID strfmt.UUID, inventory *models.Inventory) *hostConfig {
	log.Infof("Searching for config for host %s", hostID)

	for _, hc := range configs {
		for _, nic := range inventory.Interfaces {
			if nic != nil {
				for _, mac := range hc.macAddresses {
					if nic.MacAddress == mac {
						log.Infof("Found host config in %s", hc.configDir)
						hc.hostID = hostID
						return hc
					}
				}
			}
		}
	}
	log.Info("No config found for host")
	return nil
}

func (configs HostConfigs) missing(log *log.Logger) []missingHost {
	missing := []missingHost{}
	for _, hc := range configs {
		if hc.hostID == "" {
			log.Infof("No agent found matching config at %s (%s)", hc.configDir, strings.Join(hc.macAddresses, ", "))

			missing = append(missing, missingHost{config: hc})
		}
	}
	return missing
}

type Failure interface {
	Hostname() string
	DescribeFailure() string
}

type UpdateFailure struct {
	response  errorutil.AssistedServiceErrorAPI
	params    *models.HostUpdateParams
	config    *hostConfig
	host      *models.Host
	inventory *models.Inventory
}

func (uf *UpdateFailure) Error() string {
	return fmt.Sprintf("Host %s update refused: %s", uf.Hostname(), errorutil.GetAssistedError(uf.response).Error())
}

func (uf *UpdateFailure) Unwrap() error {
	return uf.response
}

func (uf *UpdateFailure) Hostname() string {
	if uf.inventory != nil {
		return uf.inventory.Hostname
	}
	return path.Base(uf.config.configDir)
}

func (uf *UpdateFailure) DescribeFailure() string {
	changes := []string{}
	if len(uf.params.DisksSelectedConfig) > 0 {
		changes = append(changes, fmt.Sprintf(
			"installation disk to %s (from %s)",
			*uf.params.DisksSelectedConfig[0].ID,
			uf.host.InstallationDiskID))
	}
	if uf.params.HostRole != nil {
		changes = append(changes, fmt.Sprintf(
			"role to %s (from %s)",
			*uf.params.HostRole,
			uf.host.SuggestedRole))
	}

	reason := "unknown reason"
	if payload := uf.response.GetPayload(); payload != nil && payload.Reason != nil {
		reason = *payload.Reason
	}

	return fmt.Sprintf("Failed to update host %s: %s",
		strings.Join(changes, " and "),
		reason)
}

type UpdateInstallerArgsFailure struct {
	response  errorutil.AssistedServiceErrorAPI
	params    *models.InstallerArgsParams
	config    *hostConfig
	host      *models.Host
	inventory *models.Inventory
}

func (uf *UpdateInstallerArgsFailure) Error() string {
	return fmt.Sprintf("Host %s installer args update refused: %s", uf.Hostname(), errorutil.GetAssistedError(uf.response).Error())
}

func (uf *UpdateInstallerArgsFailure) Unwrap() error {
	return uf.response
}

func (uf *UpdateInstallerArgsFailure) Hostname() string {
	if uf.inventory != nil {
		return uf.inventory.Hostname
	}
	return path.Base(uf.config.configDir)
}

func (uf *UpdateInstallerArgsFailure) DescribeFailure() string {
	reason := "unknown reason"
	if payload := uf.response.GetPayload(); payload != nil && payload.Reason != nil {
		reason = *payload.Reason
	}

	return fmt.Sprintf("Failed to update host installer args: %s", reason)
}

type missingHost struct {
	config *hostConfig
}

func (mh missingHost) Hostname() string {
	return path.Base(mh.config.configDir)
}

func (mh missingHost) DescribeFailure() string {
	return "Host not registered"
}
