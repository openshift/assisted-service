package agentbasedinstaller

import (
	"context"
	"fmt"
	"io/ioutil"
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
	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

func ApplyHostConfigs(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, hostConfigs HostConfigs, infraEnvID strfmt.UUID) (bool, error) {
	hostList, err := bmInventory.Installer.V2ListHosts(ctx, installer.NewV2ListHostsParams().WithInfraEnvID(infraEnvID))
	if err != nil {
		return false, fmt.Errorf("Failed to list hosts: %w", err)
	}

	for _, host := range hostList.Payload {
		if err := applyHostConfig(ctx, log, bmInventory, host, hostConfigs); err != nil {
			return false, err
		}
	}

	if !hostConfigs.allFound(log) {
		log.Info("Not all hosts present yet")
		return false, nil
	}
	log.Info("All hosts configured")
	return true, nil
}

func applyHostConfig(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, host *models.Host, hostConfigs HostConfigs) error {
	log.Infof("Checking configuration for host %s", *host.ID)

	inventory := &models.Inventory{}
	err := inventory.UnmarshalBinary([]byte(host.Inventory))
	if err != nil {
		return fmt.Errorf("Failed to unmarshal host inventory: %w", err)
	}

	config := hostConfigs.findHostConfig(*host.ID, inventory)
	if config == nil {
		return nil
	}

	updateParams := &models.HostUpdateParams{}
	changed := false

	rdh, err := config.RootDeviceHints()
	if err != nil {
		return err
	}
	if applyRootDeviceHints(log, host, inventory, rdh, updateParams) {
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
		return fmt.Errorf("Failed to update Host: %w", err)
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

func LoadHostConfigs(hostConfigDir string) (HostConfigs, error) {
	log.Infof("Loading host configurations from disk in %s", hostConfigDir)

	configs := HostConfigs{}

	entries, err := os.ReadDir(hostConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("No host configuration directory found %s", hostConfigDir)
			return nil, nil
		}
		return nil, fmt.Errorf("Failed to read config directory %s: %w", hostConfigDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		hostPath := path.Join(hostConfigDir, e.Name())
		log.Infof("Reading directory %s", hostPath)

		macs, err := ioutil.ReadFile(filepath.Join(hostPath, "mac_addresses"))
		if os.IsNotExist(err) {
			log.Info("No MAC Addresses file found")
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("Failed to read MAC Addresses file: %w", err)
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
	hintData, err := ioutil.ReadFile(path.Join(hc.configDir, "root-device-hints.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("No root device hints file found for host")
			return nil, nil
		}
		return nil, fmt.Errorf("Failed to read Root Device Hints file: %w", err)
	}

	rdh := &bmh_v1alpha1.RootDeviceHints{}
	if err := yaml.UnmarshalStrict(hintData, rdh); err != nil {
		return nil, fmt.Errorf("Failed to parse Root Device Hints file: %w", err)
	}
	log.Info("Read root device hints file")
	return rdh, nil
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

func (configs HostConfigs) allFound(log *log.Logger) bool {
	found := true
	for _, hc := range configs {
		if hc.hostID == "" {
			found = false
			log.Infof("No agent found matching config at %s (%s)", hc.configDir, strings.Join(hc.macAddresses, ", "))
		}
	}
	return found
}
