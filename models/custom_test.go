package models

import (
	"testing"
)

func TestIPNormalize(t *testing.T) {
	tests := []struct {
		name      string
		input     IP
		expected  IP
		expectErr bool
	}{
		{"IPv6 expanded to canonical", IP("2620:0052:0009:164d:0000:0000:0000:0046"), IP("2620:52:9:164d::46"), false},
		{"IPv6 leading zeros", IP("2001:0db8:0000:0000:0000:0000:0000:0001"), IP("2001:db8::1"), false},
		{"IPv6 mixed case", IP("2001:0DB8::ABCD"), IP("2001:db8::abcd"), false},
		{"IPv6 already canonical", IP("2001:db8::1"), IP("2001:db8::1"), false},
		{"IPv4 unchanged", IP("192.168.1.1"), IP("192.168.1.1"), false},
		{"invalid returns error", IP("invalid"), IP("invalid"), true},
		{"empty returns empty", IP(""), IP(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Normalize()
			if tt.expectErr && err == nil {
				t.Errorf("IP(%q).Normalize() expected error, got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("IP(%q).Normalize() unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("IP(%q).Normalize() = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIPEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     IP
		expected bool
	}{
		{"same canonical", IP("2001:db8::1"), IP("2001:db8::1"), true},
		{"expanded vs canonical", IP("2620:0052:0009:164d:0000:0000:0000:0046"), IP("2620:52:9:164d::46"), true},
		{"mixed case", IP("2001:0DB8::ABCD"), IP("2001:db8::abcd"), true},
		{"different IPs", IP("2001:db8::1"), IP("2001:db8::2"), false},
		{"IPv4 equal", IP("192.168.1.1"), IP("192.168.1.1"), true},
		{"IPv4 different", IP("192.168.1.1"), IP("192.168.1.2"), false},
		{"invalid falls back to string", IP("invalid"), IP("invalid"), true},
		{"invalid different", IP("invalid1"), IP("invalid2"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Equal(tt.b)
			if got != tt.expected {
				t.Errorf("IP(%q).Equal(IP(%q)) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestSubnetNormalize(t *testing.T) {
	tests := []struct {
		name      string
		input     Subnet
		expected  Subnet
		expectErr bool
	}{
		{"IPv6 non-canonical", Subnet("fcff:0069:0001:0000::/64"), Subnet("fcff:69:1::/64"), false},
		{"IPv6 mixed case", Subnet("2A00:8A00:4000:0D80::/64"), Subnet("2a00:8a00:4000:d80::/64"), false},
		{"IPv6 already canonical", Subnet("fd01::/48"), Subnet("fd01::/48"), false},
		{"IPv4 unchanged", Subnet("192.168.1.0/24"), Subnet("192.168.1.0/24"), false},
		{"IPv4 host bits masked", Subnet("192.168.1.5/24"), Subnet("192.168.1.0/24"), false},
		{"invalid returns error", Subnet("invalid"), Subnet("invalid"), true},
		{"empty returns empty", Subnet(""), Subnet(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.input.Normalize()
			if tt.expectErr && err == nil {
				t.Errorf("Subnet(%q).Normalize() expected error, got nil", tt.input)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Subnet(%q).Normalize() unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("Subnet(%q).Normalize() = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSubnetEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     Subnet
		expected bool
	}{
		{"same canonical", Subnet("fd01::/48"), Subnet("fd01::/48"), true},
		{"non-canonical vs canonical", Subnet("fcff:0069:0001:0000::/64"), Subnet("fcff:69:1::/64"), true},
		{"mixed case", Subnet("2A00:8A00:4000:0D80::/64"), Subnet("2a00:8a00:4000:d80::/64"), true},
		{"different subnets", Subnet("fd01::/48"), Subnet("fd02::/48"), false},
		{"different prefix length", Subnet("fd01::/48"), Subnet("fd01::/64"), false},
		{"IPv4 equal", Subnet("192.168.1.0/24"), Subnet("192.168.1.0/24"), true},
		{"IPv4 different", Subnet("192.168.1.0/24"), Subnet("192.168.2.0/24"), false},
		{"invalid falls back to string", Subnet("invalid"), Subnet("invalid"), true},
		{"invalid different", Subnet("invalid1"), Subnet("invalid2"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Equal(tt.b)
			if got != tt.expected {
				t.Errorf("Subnet(%q).Equal(Subnet(%q)) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}
