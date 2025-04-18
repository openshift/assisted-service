package featuresupport

import (
	"fmt"
	"sync"

	"github.com/kelseyhightower/envconfig"
)

// Config contains configuration for the feature support logic.
type Config struct {
	// AmdGpuSupportedOpenShiftversions is a comma separated list of versions of OpenShift that where the AMD GPU
	// operator // is supported.
	//
	// This is needed because when the operator was initially released it was supported only 4.17 and not in 4.18
	// and we wanted to be able to enable/disable versions in the production environment without having to deploy
	// a new version of the service.
	//
	// If the value is '*' then all versions will be supported.
	AmdGpuSupportedOpenShiftversions []string `envconfig:"AMD_GPU_SUPPORTED_OPENSHIFT_VERSIONS" default:"*"`
}

// The configuration is handled as a singleton that is lazily initialized. Don't use these variables directly, use the
// GetConfig function instead.
var (
	config     *Config
	configLock *sync.Mutex = &sync.Mutex{}
)

// GetConfig returns the configuration, reading it from the environment variables first if this is the first time it is
// used.
func GetConfig() (result *Config, err error) {
	configLock.Lock()
	defer configLock.Unlock()
	if config == nil {
		config = &Config{}
		err = envconfig.Process("", config)
		if err != nil {
			err = fmt.Errorf("failed to load feature support configuration: %w", err)
			return
		}
	}
	result = config
	return
}

// SetConfig replaces the configuration. This is intended for unit tests, where it is convenient to set the
// configuration without changing the environment variables.
func SetConfig(replacement *Config) {
	configLock.Lock()
	defer configLock.Unlock()
	config = replacement
}
