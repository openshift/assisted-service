package ovirt

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// OvirtConfig contains the required information to perform
// an HTTPS connection to an oVirt engine
type OvirtConfig struct {
	URL      string `yaml:"ovirt_url,omitempty"`
	Username string `yaml:"ovirt_username,omitempty"`
	Password string `yaml:"ovirt_password,omitempty"`
	Insecure bool   `yaml:"ovirt_insecure,omitempty"`
	CaBundle string `yaml:"ovirt_ca_bundle,omitempty"`
}

// PreCreateManifestHook creates the 'ovirt-config.yaml' file required by the installer
// for the oVirt platform and append the OVIRT_CONFIG to the environment variables
func (p ovirtProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}

// PostCreateManifestsHook modifies master's Machine manifests with the actual VM and Template names
func (p ovirtProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil {
		return errors.New("no cluster provided")
	}
	if cluster.Platform == nil || common.PlatformTypeValue(cluster.Platform.Type) != models.PlatformTypeOvirt {
		return errors.New("platform type is not ovirt")
	}
	return errors.New("ovirt platform connection params not set")
}
