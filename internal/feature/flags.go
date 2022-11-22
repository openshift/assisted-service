package feature

// Flags contains the values of the feature flags.
type Flags struct {
	// EnableUpgradeAgent is a boolean flag to enable or disable the upgrade agent feature:
	EnableUpgradeAgent bool `envconfig:"ENABLE_UPGRADE_AGENT" default:"true"`
}
