package agentbasedinstaller

import (
	"context"
	"fmt"
	"net"
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

// AgentWorkflowType defines the supported
// agent workflows.
type AgentWorkflowType string

const (
	// AgentWorkflowTypeInstall identifies the install workflow.
	AgentWorkflowTypeInstall AgentWorkflowType = "install"
	// AgentWorkflowTypeAddNodes identifies the add nodes workflow.
	AgentWorkflowTypeAddNodes AgentWorkflowType = "addnodes"
)

// loadFencingCredentials reads the fencing-credentials.yaml file from the specified path
// and returns a map of hostname→credentials for easy lookup during host application.
// Returns nil map (not error) if file doesn't exist, since fencing is optional.
func loadFencingCredentials(fencingFilePath string) (map[string]*models.FencingCredentialsParams, error) {
	fileData, err := os.ReadFile(fencingFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("No fencing credentials file found, skipping fencing configuration")
			return nil, nil // Not an error - fencing is optional
		}
		return nil, fmt.Errorf("failed to read fencing credentials file at %s: %w", fencingFilePath, err)
	}

	// Intermediate structure matching installer's YAML output
	type fencingCredentialsFile struct {
		Credentials []struct {
			Hostname                string  `yaml:"hostname"`
			Address                 *string `yaml:"address"`
			Username                *string `yaml:"username"`
			Password                *string `yaml:"password"`
			CertificateVerification *string `yaml:"certificateVerification,omitempty"`
		} `yaml:"credentials"`
	}

	fcFile := &fencingCredentialsFile{}
	if err := yaml.UnmarshalStrict(fileData, fcFile); err != nil {
		return nil, fmt.Errorf("failed to parse fencing credentials file at %s: %w", fencingFilePath, err)
	}

	credentialsMap := make(map[string]*models.FencingCredentialsParams)

	for _, cred := range fcFile.Credentials {
		// Skip entries without hostname - installer validates these
		if cred.Hostname == "" {
			continue
		}
		credentialsMap[cred.Hostname] = &models.FencingCredentialsParams{
			Address:                 cred.Address,
			Username:                cred.Username,
			Password:                cred.Password,
			CertificateVerification: cred.CertificateVerification,
		}
		log.Infof("Loaded fencing credential for hostname: %s", cred.Hostname)
	}

	log.Infof("Loaded %d fencing credentials from file", len(credentialsMap))
	return credentialsMap, nil
}

func ApplyHostConfigs(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, hostConfigs HostConfigs, infraEnvID strfmt.UUID, hostConfigDir string) ([]Failure, error) {
	hostList, err := bmInventory.Installer.V2ListHosts(ctx, installer.NewV2ListHostsParams().WithInfraEnvID(infraEnvID))
	if err != nil {
		return nil, fmt.Errorf("Failed to list hosts: %w", errorutil.GetAssistedError(err))
	}

	failures := []Failure{}

	for _, host := range hostList.Payload {
		// Apply host configuration (role, disk hints, fencing credentials)
		if err := applyHostConfig(ctx, log, bmInventory, host, hostConfigs, hostConfigDir); err != nil {
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

func applyHostConfig(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, host *models.Host, hostConfigs HostConfigs, hostConfigDir string) error {
	log.Infof("Checking configuration for host %s", *host.ID)

	if len(host.Inventory) == 0 {
		log.Info("Inventory information not yet available")
		return nil
	}

	inventory := &models.Inventory{}
	err := inventory.UnmarshalBinary([]byte(host.Inventory))
	if err != nil {
		return fmt.Errorf("failed to unmarshal host inventory: %w", err)
	}

	config := hostConfigs.findHostConfig(*host.ID, inventory)
	if config == nil {
		return nil
	}

	updateParams := &models.HostUpdateParams{}
	changed := false

	applied, err := applyRootDeviceHints(log, host, inventory, config, updateParams)
	if err != nil {
		return err
	}
	if applied {
		changed = true
	}

	role, err := config.Role()
	if err != nil {
		return err
	}
	if applyRole(log, host, inventory, role, updateParams) {
		changed = true
	}

	applied, err = applyFencingCredentials(log, host, inventory, hostConfigDir, updateParams)
	if err != nil {
		return err
	}
	if applied {
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
			}
		}
		return fmt.Errorf("failed to update Host: %w", err)
	}
	return nil
}

func applyRootDeviceHints(log *log.Logger, host *models.Host, inventory *models.Inventory, config *hostConfig, updateParams *models.HostUpdateParams) (bool, error) {
	rdh, err := config.RootDeviceHints()
	if err != nil {
		return false, err
	}
	if rdh == nil {
		return false, nil
	}

	acceptableDisks := hostutil.GetAcceptableDisksWithHints(inventory.Disks, rdh)
	if host.InstallationDiskID != "" {
		for _, disk := range acceptableDisks {
			if disk.ID == host.InstallationDiskID {
				log.Infof("Selected disk %s already matches root device hints", host.InstallationDiskID)
				return false, nil
			}
		}
	}

	diskID := "/dev/not-found-by-hints"
	if len(acceptableDisks) > 0 {
		diskID = acceptableDisks[0].ID
		log.Infof("Selecting disk %s for installation", diskID)
	} else {
		log.Info("No disk found matching root device hints")

		possibleDisks := []string{}
		for _, disk := range inventory.Disks {
			if !disk.InstallationEligibility.Eligible {
				log.Infof("Disk %s is not eligible due to %s", disk.Path, disk.InstallationEligibility.NotEligibleReasons)
				continue
			}
			diskStr := fmt.Sprintf("Disk - path: %s, by-path: %s, wwn: %s", disk.Path, disk.ByPath, disk.Wwn)
			possibleDisks = append(possibleDisks, diskStr)
		}
		log.Info("Eligible disks: ", possibleDisks)
	}

	updateParams.DisksSelectedConfig = []*models.DiskConfigParams{
		{ID: &diskID, Role: models.DiskRoleInstall},
	}
	return true, nil
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

// applyFencingCredentials looks up fencing credentials by the host's actual hostname
// from inventory. Unlike Role and RootDeviceHints which are stored in per-host
// directories (keyed by MAC), fencing credentials are stored in a shared file
// (fencing-credentials.yaml) keyed by hostname. Therefore, we look up directly
// by inventory.Hostname rather than relying on the matched hostConfig.
func applyFencingCredentials(log *log.Logger, host *models.Host, inventory *models.Inventory, hostConfigDir string, updateParams *models.HostUpdateParams) (bool, error) {
	if inventory.Hostname == "" {
		return false, nil
	}

	creds, err := loadFencingCredentials(filepath.Join(hostConfigDir, "fencing-credentials.yaml"))
	if err != nil || creds == nil {
		return false, nil
	}

	hostCreds := creds[inventory.Hostname]
	if hostCreds == nil {
		return false, nil
	}

	// Skip if already configured
	if host.FencingCredentials != "" {
		log.Info("Fencing credentials already configured for host")
		return false, nil
	}

	log.Infof("Adding fencing credentials for hostname %s", inventory.Hostname)
	updateParams.FencingCredentials = hostCreds
	return true, nil
}

func LoadHostConfigs(hostConfigDir string, workflowType AgentWorkflowType) (HostConfigs, error) {
	log.Infof("Loading host configurations from disk in %s", hostConfigDir)

	configs := HostConfigs{}

	entries, err := os.ReadDir(hostConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("No host configuration directory found %s", hostConfigDir)
			entries = []os.DirEntry{} // Continue to load fencing credentials
		} else {
			return nil, fmt.Errorf("failed to read config directory %s: %w", hostConfigDir, err)
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		hostPath := path.Join(hostConfigDir, e.Name())
		log.Infof("Reading directory %s", hostPath)

		var macs []byte
		macs, err = os.ReadFile(filepath.Join(hostPath, "mac_addresses"))
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

		if workflowType == AgentWorkflowTypeAddNodes {
			// In the addnodes workflow, the only host config we want to load is the
			// current host's. Multiple HostConfigs could exist in hostConfigDir
			// if multiple day-2 nodes are being added using the same day-2 ISO.
			// Filter out the other HostConfig entries because each day-2
			// node is added in isolation using their own internal assisted-service
			// instance.
			var addHostConfig bool
			addHostConfig, err = currentHostHasMACAddress(addresses)
			if err != nil {
				return nil, err
			}
			if !addHostConfig {
				continue
			}
		}
		configs = append(configs, &hostConfig{
			configDir:    hostPath,
			macAddresses: addresses,
		})
	}

	// Load fencing credentials and create hostname-based configs
	// Note: For AddNodes workflow, the installer doesn't generate fencing-credentials.yaml,
	// so no special filtering is needed - the file simply won't exist.
	fencingCreds, err := loadFencingCredentials(filepath.Join(hostConfigDir, "fencing-credentials.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to load fencing credentials: %w", err)
	}

	// Create hostname-based hostConfig entries for each fencing credential
	for hostname := range fencingCreds {
		configs = append(configs, &hostConfig{
			hostname: hostname,
		})
	}

	return configs, nil
}

type hostConfig struct {
	configDir    string
	macAddresses []string
	hostname     string // For hostname-based matching (fencing)
	hostID       strfmt.UUID
}

// currentHostHasMACAddress returns true if this host has a MAC address in addresses string array.
func currentHostHasMACAddress(addresses []string) (bool, error) {
	hostInterfaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("failed to get this host's interfaces: %w", err)
	}
	for _, iface := range hostInterfaces {
		if iface.HardwareAddr == nil {
			continue
		}
		for _, hostConfigMac := range addresses {
			if iface.HardwareAddr.String() == hostConfigMac {
				return true, nil
			}
		}
	}

	return false, nil
}

func (hc hostConfig) RootDeviceHints() (*bmh_v1alpha1.RootDeviceHints, error) {
	// Only MAC-based configs have root device hints
	if len(hc.macAddresses) == 0 {
		return nil, nil
	}

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
	// Only MAC-based configs have role
	if len(hc.macAddresses) == 0 {
		return nil, nil
	}

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

type HostConfigs []*hostConfig

func (configs HostConfigs) findHostConfig(hostID strfmt.UUID, inventory *models.Inventory) *hostConfig {
	log.Infof("Searching for config for host %s", hostID)

	// First: try MAC-based matching (for role + disk hints)
	for _, hc := range configs {
		if len(hc.macAddresses) > 0 {
			for _, nic := range inventory.Interfaces {
				if nic != nil {
					for _, mac := range hc.macAddresses {
						if nic.MacAddress == mac {
							log.Infof("Found host config in %s (MAC match)", hc.configDir)
							hc.hostID = hostID
							return hc
						}
					}
				}
			}
		}
	}

	// Second: try hostname-based matching (for fencing credentials)
	for _, hc := range configs {
		if hc.hostname != "" && hc.hostname == inventory.Hostname {
			log.Infof("Found fencing config for hostname %s", hc.hostname)
			hc.hostID = hostID
			return hc
		}
	}

	log.Info("No config found for host")
	return nil
}

func (configs HostConfigs) missing(log *log.Logger) []missingHost {
	missing := []missingHost{}
	for _, hc := range configs {
		if hc.hostID == "" {
			// Log appropriately based on config type
			if hc.hostname != "" {
				log.Infof("No agent found matching hostname %s", hc.hostname)
			} else {
				log.Infof("No agent found matching config at %s (%s)", hc.configDir, strings.Join(hc.macAddresses, ", "))
			}
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

type missingHost struct {
	config *hostConfig
}

func (mh missingHost) Hostname() string {
	if mh.config.hostname != "" {
		return mh.config.hostname
	}
	return path.Base(mh.config.configDir)
}

func (mh missingHost) DescribeFailure() string {
	if mh.config.hostname != "" {
		return "Fencing credentials loaded but no host with matching hostname found"
	}
	return "Host not registered"
}
