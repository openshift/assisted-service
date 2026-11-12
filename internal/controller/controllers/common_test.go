package controllers

import (
	"testing"

	"github.com/openshift/assisted-service/models"
)

func TestMachineNetworkSetsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []*models.MachineNetwork
		expected bool
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "both empty",
			a:        []*models.MachineNetwork{},
			b:        []*models.MachineNetwork{},
			expected: true,
		},
		{
			name:     "same single element",
			a:        []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}},
			b:        []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}},
			expected: true,
		},
		{
			name: "same elements different order",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.0/25"},
				{Cidr: "192.168.20.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.20.0/25"},
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.0/25"},
			},
			expected: true,
		},
		{
			name: "same elements same order",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.0/25"},
			},
			expected: true,
		},
		{
			name: "different CIDRs",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "10.0.0.0/8"},
			},
			expected: false,
		},
		{
			name: "different lengths",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
			},
			expected: false,
		},
		{
			name:     "one nil one empty",
			a:        nil,
			b:        []*models.MachineNetwork{},
			expected: true,
		},
		{
			name: "four elements reversed order",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.10.11/25"},
				{Cidr: "192.168.20.21/25"},
				{Cidr: "192.168.30.31/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.30.31/25"},
				{Cidr: "192.168.20.21/25"},
				{Cidr: "192.168.10.11/25"},
				{Cidr: "192.168.0.0/25"},
			},
			expected: true,
		},
		{
			name: "duplicate desired matches distinct backend - not equal",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.0.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "10.0.0.0/8"},
			},
			expected: false,
		},
		{
			name: "duplicate backend matches distinct desired - not equal",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "10.0.0.0/8"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.0.0/25"},
			},
			expected: false,
		},
		{
			name: "identical duplicates on both sides - equal",
			a: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.0.0/25"},
			},
			b: []*models.MachineNetwork{
				{Cidr: "192.168.0.0/25"},
				{Cidr: "192.168.0.0/25"},
			},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := machineNetworkSetsEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("machineNetworkSetsEqual() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestServiceNetworkSetsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []*models.ServiceNetwork
		expected bool
	}{
		{
			name:     "both empty",
			a:        []*models.ServiceNetwork{},
			b:        []*models.ServiceNetwork{},
			expected: true,
		},
		{
			name: "same elements different order",
			a: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
				{Cidr: "1002:db8::/119"},
			},
			b: []*models.ServiceNetwork{
				{Cidr: "1002:db8::/119"},
				{Cidr: "1.2.5.0/24"},
			},
			expected: true,
		},
		{
			name: "different CIDRs",
			a: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
			},
			b: []*models.ServiceNetwork{
				{Cidr: "10.96.0.0/12"},
			},
			expected: false,
		},
		{
			name: "duplicate desired matches distinct backend - not equal",
			a: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
				{Cidr: "1.2.5.0/24"},
			},
			b: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
				{Cidr: "10.96.0.0/12"},
			},
			expected: false,
		},
		{
			name: "identical duplicates on both sides - equal",
			a: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
				{Cidr: "1.2.5.0/24"},
			},
			b: []*models.ServiceNetwork{
				{Cidr: "1.2.5.0/24"},
				{Cidr: "1.2.5.0/24"},
			},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceNetworkSetsEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("serviceNetworkSetsEqual() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClusterNetworkSetsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []*models.ClusterNetwork
		expected bool
	}{
		{
			name:     "both empty",
			a:        []*models.ClusterNetwork{},
			b:        []*models.ClusterNetwork{},
			expected: true,
		},
		{
			name: "same elements different order",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "1003:db8::/53", HostPrefix: 64},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "1003:db8::/53", HostPrefix: 64},
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			expected: true,
		},
		{
			name: "same CIDR different HostPrefix",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 23},
			},
			expected: false,
		},
		{
			name: "different CIDRs same HostPrefix",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "10.128.0.0/14", HostPrefix: 24},
			},
			expected: false,
		},
		{
			name: "different lengths",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "1003:db8::/53", HostPrefix: 64},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			expected: false,
		},
		{
			name: "duplicate desired matches distinct backend - not equal",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "10.128.0.0/14", HostPrefix: 24},
			},
			expected: false,
		},
		{
			name: "identical duplicates on both sides - equal",
			a: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			b: []*models.ClusterNetwork{
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
				{Cidr: "1.3.0.0/16", HostPrefix: 24},
			},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterNetworkSetsEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("clusterNetworkSetsEqual() = %v, want %v", got, tt.expected)
			}
		})
	}
}
