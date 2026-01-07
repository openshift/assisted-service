package networkobservability

import (
	"encoding/json"
	"fmt"
)

const (
	Name             = "network-observability"
	FullName         = "Network Observability Operator"
	Namespace        = "openshift-netobserv-operator"
	SubscriptionName = "network-observability-operator"
	Source           = "redhat-operators"
	SourceName       = "netobserv-operator"
	GroupName        = "netobserv-operatorgroup"
)

// Config holds the configuration for Network Observability Operator
type Config struct {
	// CreateFlowCollector indicates whether to create a FlowCollector resource
	CreateFlowCollector bool `json:"createFlowCollector,omitempty"`
	// Sampling rate for eBPF agent (default: 50)
	Sampling int `json:"sampling,omitempty"`
}

// ParseProperties parses the properties JSON string into a Config struct
func ParseProperties(properties string) (*Config, error) {
	config := &Config{
		CreateFlowCollector: false, // Default: don't create FlowCollector
		Sampling:            50,    // Default sampling rate
	}

	if properties == "" {
		return config, nil
	}

	if err := json.Unmarshal([]byte(properties), config); err != nil {
		return nil, fmt.Errorf("failed to parse network-observability properties: %w", err)
	}

	// Validate sampling rate
	if config.Sampling <= 0 {
		config.Sampling = 50
	}

	return config, nil
}
