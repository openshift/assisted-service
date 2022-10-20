package host

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type Config struct {
	LogTimeoutConfig
	EnableAutoReset          bool                    `envconfig:"ENABLE_AUTO_RESET" default:"false"`
	EnableAutoAssign         bool                    `envconfig:"ENABLE_AUTO_ASSIGN" default:"true"`
	ResetTimeout             time.Duration           `envconfig:"RESET_CLUSTER_TIMEOUT" default:"3m"`
	MonitorBatchSize         int                     `envconfig:"HOST_MONITOR_BATCH_SIZE" default:"100"`
	DisabledHostvalidations  DisabledHostValidations `envconfig:"DISABLED_HOST_VALIDATIONS" default:""` // Which host validations to disable (should not run in preprocess)
	BootstrapHostMAC         string                  `envconfig:"BOOTSTRAP_HOST_MAC" default:""`        // For ephemeral installer to ensure the bootstrap for the (single) cluster lands on the same host as assisted-service
	MaxHostDisconnectionTime time.Duration           `envconfig:"HOST_MAX_DISCONNECTION_TIME" default:"3m"`
	EnableVirtualInterfaces  bool                    `envconfig:"ENABLE_VIRTUAL_INTERFACES" default:"false"`

	// hostStageTimeouts contains the values of the host stage timeouts. Don't use this
	// directly, use the HostStageTimeout method instead.
	hostStageTimeouts map[models.HostStage]time.Duration `ignored:"true"`
}

// Complete performs additional processing of the configuration after it has been loaded by the
// envconfig library. For example, it loads the values of the host stage timeouts from the
// environment.
func (c *Config) Complete() error {
	values := map[models.HostStage]time.Duration{}
	for stage, value := range hostStageTimeoutDefaults {
		stageName := strings.ToUpper(strings.ReplaceAll(string(stage), " ", "_"))
		envName := fmt.Sprintf("HOST_STAGE_%s_TIMEOUT", stageName)
		envValue, ok := os.LookupEnv(envName)
		if ok {
			var err error
			value, err = time.ParseDuration(envValue)
			if err != nil {
				return errors.Wrapf(
					err,
					"failed to parse timeout of host stage '%s' from value of "+
						"environment variable '%s'",
					stage, envName,
				)
			}
		}
		values[stage] = value
	}
	c.hostStageTimeouts = values
	return nil
}

// hostStageTimeoutDefaults contains the built-in default values for the host stage timeouts.
var hostStageTimeoutDefaults = map[models.HostStage]time.Duration{
	models.HostStageStartingInstallation:   30 * time.Minute,
	models.HostStageWaitingForControlPlane: 60 * time.Minute,
	models.HostStageWaitingForController:   60 * time.Minute,
	models.HostStageWaitingForBootkube:     60 * time.Minute,
	models.HostStageInstalling:             60 * time.Minute,
	models.HostStageJoined:                 60 * time.Minute,
	models.HostStageWritingImageToDisk:     30 * time.Minute,
	models.HostStageRebooting:              40 * time.Minute,
	models.HostStageConfiguring:            60 * time.Minute,
	models.HostStageWaitingForIgnition:     24 * time.Hour,
}

// hostStageTimeoutDefault is the default timeout for stages that aren't explicitly enumerated in
// the map above.
const hostStageTimeoutDefault = 60 * time.Minute

// HostStageTimeout returns the timeout for the given host stage.
//
// If the Complete method of the configuration has been called then then the timeout for a stage
// will be taken from an environment variable. If that environment variable doesn't exist then it
// will return a built-in default.
//
// The values of the environment variables should use the format used by the time.ParseDuration
// method.
//
// The names of the environment variables will be calculated converting the stage name to upper
// case, replacing spaces with underscores and adding the `HOST_STAGE_` prefix and the `_TIMEOUT`
// suffix. For example, for the `Starting installation` stage the name of the environment variable
// will be `HOST_STAGE_STARTING_INSTALLATION_TIMEOUT`.
func (c *Config) HostStageTimeout(stage models.HostStage) time.Duration {
	if c.hostStageTimeouts != nil {
		value, ok := c.hostStageTimeouts[stage]
		if ok {
			return value
		}
	}
	return hostStageTimeoutDefault
}
