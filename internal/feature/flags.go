package feature

// Flags contains the values of the feature flags.
type Flags struct {
	// EnableUpgradeAgent is a boolean flag to enable or disable the upgrade agent feature:
	EnableUpgradeAgent bool `envconfig:"ENABLE_UPGRADE_AGENT" default:"true"`

	// EnableRejectUnknownFields is a boolean flag to enable or disable rejecting unknown fields
	// in JSON request bodies.
	EnableRejectUnknownFields bool `envconfig:"ENABLE_REJECT_UNKNOWN_FIELDS" default:"true"`

	// EnableSkipMcoReboot is a boolean flag to enable MCO reboot by assisted installer
	EnableSkipMcoReboot bool `envconfig:"ENABLE_SKIP_MCO_REBOOT" default:"true"`
}
