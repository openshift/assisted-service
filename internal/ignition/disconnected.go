package ignition

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/system"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DisconnectedIgnitionGenerator struct {
	executer               executer.Executer
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder
	installerCache         installercache.InstallerCache
	versionsHandler        versions.Handler
	log                    logrus.FieldLogger
	workDir                string
}

func NewDisconnectedIgnitionGenerator(
	executer executer.Executer,
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder,
	installerCache installercache.InstallerCache,
	versionsHandler versions.Handler,
	log logrus.FieldLogger,
	workDir string,
) *DisconnectedIgnitionGenerator {
	return &DisconnectedIgnitionGenerator{
		executer:               executer,
		mirrorRegistriesConfig: mirrorRegistriesConfig,
		installerCache:         installerCache,
		versionsHandler:        versionsHandler,
		log:                    log,
		workDir:                workDir,
	}
}

// GenerateDisconnectedIgnition generates the unconfigured-ignition content for disconnected images
func (g *DisconnectedIgnitionGenerator) GenerateDisconnectedIgnition(ctx context.Context, infraEnv *common.InfraEnv, cluster *common.Cluster) (string, error) {
	log := logutil.FromContext(ctx, g.log)
	log.Infof("GenerateDisconnectedIgnition called for infraEnv %s", *infraEnv.ID)

	// For disconnected ISOs, we require the infraEnv to be bound to a cluster
	if infraEnv.ClusterID == "" {
		return "", errors.Errorf("InfraEnv %s is not bound to a cluster, which is required for disconnected ignition generation", *infraEnv.ID)
	}

	disconnectedManifestsDir, err := createDisconnectedManifestsDir(g.workDir, infraEnv)
	if err != nil {
		return "", errors.Wrap(err, "failed to create disconnected manifests directory")
	}
	defer func() {
		if removeErr := os.RemoveAll(disconnectedManifestsDir); removeErr != nil {
			log.WithError(removeErr).Warnf("Failed to clean up disconnected work directory %s", disconnectedManifestsDir)
		}
	}()

	if err = createManifests(ctx, infraEnv, cluster, disconnectedManifestsDir, log); err != nil {
		return "", errors.Wrap(err, "failed to create manifests")
	}

	if err = createMirrorConfig(disconnectedManifestsDir); err != nil {
		return "", errors.Wrap(err, "failed to create mirror config")
	}

	release, err := getInstallerRelease(ctx, infraEnv, cluster.OpenshiftVersion, infraEnv.ClusterID, g.versionsHandler, g.executer, g.mirrorRegistriesConfig, g.installerCache)
	if err != nil {
		return "", errors.Wrap(err, "failed to get installer release")
	}
	defer func() {
		if e := release.Cleanup(ctx); e != nil {
			log.WithError(e).Warnf("Failed to clean up installer release %s", release.Path)
		}
	}()

	ignitionContent, err := generateUnconfiguredIgnition(g.executer, release.Path, disconnectedManifestsDir, log)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate ignition")
	}

	log.Infof("Generated unconfigured-ignition for disconnected image (infraEnv: %s, arch: %s) using installer %s",
		*infraEnv.ID, infraEnv.CPUArchitecture, release.Path)

	return ignitionContent, nil
}

func createPullSecretManifest(infraEnv *common.InfraEnv, manifestsDir string) error {
	pullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pull-secret",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		StringData: map[string]string{
			corev1.DockerConfigJsonKey: infraEnv.PullSecret,
		},
	}

	pullSecretYAML, err := yaml.Marshal(pullSecret)
	if err != nil {
		return errors.Wrap(err, "failed to marshal pull secret YAML")
	}
	if err = os.WriteFile(filepath.Join(manifestsDir, "pull-secret.yaml"), pullSecretYAML, 0600); err != nil {
		return errors.Wrap(err, "failed to write pull-secret.yaml")
	}
	return nil
}

func createInfraEnvManifest(infraEnv *common.InfraEnv, manifestsDir string) error {
	spec := v1beta1.InfraEnvSpec{
		PullSecretRef: &corev1.LocalObjectReference{
			Name: "pull-secret",
		},
		SSHAuthorizedKey: infraEnv.SSHAuthorizedKey,
		CpuArchitecture:  infraEnv.CPUArchitecture,
	}

	// Add proxy configuration if present
	if infraEnv.Proxy != nil && (swag.StringValue(infraEnv.Proxy.HTTPProxy) != "" || swag.StringValue(infraEnv.Proxy.HTTPSProxy) != "") {
		spec.Proxy = &v1beta1.Proxy{
			HTTPProxy:  swag.StringValue(infraEnv.Proxy.HTTPProxy),
			HTTPSProxy: swag.StringValue(infraEnv.Proxy.HTTPSProxy),
			NoProxy:    swag.StringValue(infraEnv.Proxy.NoProxy),
		}
	}

	// Add additional NTP sources if present
	if infraEnv.AdditionalNtpSources != "" {
		// AdditionalNtpSources is a comma-separated string, convert to slice
		ntpSources := strings.Split(infraEnv.AdditionalNtpSources, ",")
		for i, source := range ntpSources {
			ntpSources[i] = strings.TrimSpace(source)
		}
		spec.AdditionalNTPSources = ntpSources
	}

	infraEnvManifest := &v1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name: *infraEnv.Name,
		},
		Spec: spec,
	}

	infraEnvYAML, err := yaml.Marshal(infraEnvManifest)
	if err != nil {
		return errors.Wrap(err, "failed to marshal infraEnv YAML")
	}
	if err = os.WriteFile(filepath.Join(manifestsDir, "infraenv.yaml"), infraEnvYAML, 0600); err != nil {
		return errors.Wrap(err, "failed to write infraenv.yaml")
	}
	return nil
}

func createDisconnectedManifestsDir(workDir string, infraEnv *common.InfraEnv) (string, error) {
	disconnectedWorkDir := filepath.Join(workDir, "disconnected-ignition")
	err := os.MkdirAll(disconnectedWorkDir, 0755)
	if err != nil {
		return "", errors.Wrap(err, "failed to create disconnected manifests work directory")
	}

	disconnectedManifestsDir, err := os.MkdirTemp(disconnectedWorkDir, infraEnv.ID.String())
	if err != nil {
		return "", errors.Wrap(err, "failed to create disconnected manifests temp directory")
	}
	return disconnectedManifestsDir, nil
}

func createManifests(ctx context.Context, infraEnv *common.InfraEnv, cluster *common.Cluster, disconnectedDir string, log logrus.FieldLogger) error {
	manifestsDir := filepath.Join(disconnectedDir, "cluster-manifests")
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create manifests directory")
	}

	if err := createInfraEnvManifest(infraEnv, manifestsDir); err != nil {
		return errors.Wrap(err, "failed to create infraEnv manifest")
	}

	if err := createPullSecretManifest(infraEnv, manifestsDir); err != nil {
		return errors.Wrap(err, "failed to create pull secret manifest")
	}

	if err := createNMStateConfigManifests(infraEnv, manifestsDir, log); err != nil {
		return errors.Wrap(err, "failed to create NMStateConfig manifests")
	}

	if err := createAgentConfigManifest(cluster, manifestsDir, log); err != nil {
		return errors.Wrap(err, "failed to create agent-config manifest")
	}

	return nil
}

func createMirrorConfig(disconnectedDir string) error {
	mirrorDir := filepath.Join(disconnectedDir, "mirror")
	if err := os.MkdirAll(mirrorDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create mirror directory")
	}

	if err := os.WriteFile(filepath.Join(mirrorDir, "registries.conf"), []byte(constants.DisconnectedRegistriesConf), 0600); err != nil {
		return errors.Wrap(err, "failed to write registries.conf")
	}

	return nil
}

func getInstallerRelease(
	ctx context.Context,
	infraEnv *common.InfraEnv,
	openshiftVersion string,
	clusterID strfmt.UUID,
	versionsHandler versions.Handler,
	executer executer.Executer,
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder,
	installerCache installercache.InstallerCache,
) (*installercache.Release, error) {
	releaseImage, err := versionsHandler.GetReleaseImage(ctx, openshiftVersion, infraEnv.CPUArchitecture, infraEnv.PullSecret)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get release image for version %s", openshiftVersion)
	}

	ocRelease := oc.NewRelease(
		executer,
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay},
		mirrorRegistriesConfig,
		system.NewLocalSystemInfo(),
	)

	release, err := installerCache.Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, ocRelease, openshiftVersion, clusterID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get installer from cache")
	}

	return release, nil
}

func generateUnconfiguredIgnition(executer executer.Executer, releasePath string, disconnectedDir string, log logrus.FieldLogger) (string, error) {
	stdout, stderr, exitCode := executer.Execute(releasePath, "agent", "create", "unconfigured-ignition", "--interactive", "--dir", disconnectedDir)
	if exitCode != 0 {
		log.Errorf("error running %s agent create unconfigured-ignition --interactive, stdout: %s, stderr: %s, exit code: %d", releasePath, stdout, stderr, exitCode)
		return "", errors.Errorf("failed to generate unconfigured-ignition: %s", stderr)
	}

	ignitionPath := filepath.Join(disconnectedDir, "unconfigured-agent.ign")
	ignitionContent, err := os.ReadFile(ignitionPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read generated unconfigured-ignition")
	}

	return string(ignitionContent), nil
}

func createNMStateConfigManifests(infraEnv *common.InfraEnv, manifestsDir string, log logrus.FieldLogger) error {
	if infraEnv.StaticNetworkConfig == "" {
		log.Debug("No static network configuration present, skipping NMStateConfig manifests")
		return nil
	}

	var staticNetworkConfigs []*models.HostStaticNetworkConfig
	if err := json.Unmarshal([]byte(infraEnv.StaticNetworkConfig), &staticNetworkConfigs); err != nil {
		return errors.Wrap(err, "failed to unmarshal static network config")
	}

	for i, config := range staticNetworkConfigs {
		if config == nil || config.NetworkYaml == "" {
			continue
		}

		// Convert mac_interface_map to v1beta1.Interface format
		interfaces := make([]*v1beta1.Interface, 0, len(config.MacInterfaceMap))
		for _, macInterface := range config.MacInterfaceMap {
			if macInterface.MacAddress != "" && macInterface.LogicalNicName != "" {
				interfaces = append(interfaces, &v1beta1.Interface{
					Name:       macInterface.LogicalNicName,
					MacAddress: macInterface.MacAddress,
				})
			}
		}

		if len(interfaces) == 0 {
			log.Warnf("Skipping static network config %d: no valid interfaces found", i)
			continue
		}

		nmStateConfig := &v1beta1.NMStateConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "agent-install.openshift.io/v1beta1",
				Kind:       "NMStateConfig",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("nmstate-config-%d", i),
			},
			Spec: v1beta1.NMStateConfigSpec{
				Interfaces: interfaces,
				NetConfig: v1beta1.NetConfig{
					Raw: v1beta1.RawNetConfig(config.NetworkYaml),
				},
			},
		}

		nmStateYAML, err := yaml.Marshal(nmStateConfig)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal NMStateConfig %d", i)
		}

		filename := filepath.Join(manifestsDir, fmt.Sprintf("nmstateconfig-%d.yaml", i))
		if err = os.WriteFile(filename, nmStateYAML, 0600); err != nil {
			return errors.Wrapf(err, "failed to write NMStateConfig %d", i)
		}

		log.Infof("Created NMStateConfig manifest: %s", filename)
	}

	return nil
}

type AgentConfig struct {
	APIVersion   string   `yaml:"apiVersion"`
	Kind         string   `yaml:"kind"`
	Metadata     Metadata `yaml:"metadata"`
	RendezvousIP string   `yaml:"rendezvousIP,omitempty"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

func createAgentConfigManifest(cluster *common.Cluster, manifestsDir string, log logrus.FieldLogger) error {
	bootstrapHost := common.GetBootstrapHost(cluster)
	if bootstrapHost == nil {
		log.Warn("No bootstrap host found, skipping agent-config.yaml")
		return nil
	}

	// Get the bootstrap host's IP address
	bootstrapIP, err := getBootstrapHostIP(bootstrapHost)
	if err != nil {
		return errors.Wrap(err, "failed to get bootstrap host IP")
	}

	agentConfig := AgentConfig{
		APIVersion: "v1alpha1",
		Kind:       "AgentConfig",
		Metadata: Metadata{
			Name: cluster.Name,
		},
		RendezvousIP: bootstrapIP,
	}

	agentConfigYAML, err := yaml.Marshal(agentConfig)
	if err != nil {
		return errors.Wrap(err, "failed to marshal agent-config YAML")
	}

	filename := filepath.Join(manifestsDir, "agent-config.yaml")
	if err = os.WriteFile(filename, agentConfigYAML, 0600); err != nil {
		return errors.Wrap(err, "failed to write agent-config.yaml")
	}

	log.Infof("Created agent-config manifest with rendezvous IP: %s", bootstrapIP)
	return nil
}

func getBootstrapHostIP(host *models.Host) (string, error) {
	if host.Inventory == "" {
		return "", errors.New("bootstrap host has no inventory")
	}

	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal bootstrap host inventory")
	}

	if len(inventory.Interfaces) == 0 {
		return "", errors.New("bootstrap host has no network interfaces")
	}

	// Find the first interface with an IPv4 address
	for _, iface := range inventory.Interfaces {
		if iface != nil && len(iface.IPV4Addresses) > 0 {
			// Parse the CIDR to get just the IP
			ipCIDR := iface.IPV4Addresses[0]
			if idx := strings.Index(ipCIDR, "/"); idx != -1 {
				return ipCIDR[:idx], nil
			}
			return ipCIDR, nil
		}
	}

	// If no IPv4, try IPv6
	for _, iface := range inventory.Interfaces {
		if iface != nil && len(iface.IPV6Addresses) > 0 {
			// Parse the CIDR to get just the IP
			ipCIDR := iface.IPV6Addresses[0]
			if idx := strings.Index(ipCIDR, "/"); idx != -1 {
				return ipCIDR[:idx], nil
			}
			return ipCIDR, nil
		}
	}

	return "", errors.New("bootstrap host has no IP addresses")
}
