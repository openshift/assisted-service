package network

import (
	"net"
	"strings"
)

func IsIPv4Addr(ip string) bool {
	return strings.Contains(ip, ".") && net.ParseIP(ip) != nil
}

func IsIPv6Addr(ip string) bool {
	return strings.Contains(ip, ":") && net.ParseIP(ip) != nil
}

func IsIPV4CIDR(cidr string) bool {
	_, _, e := net.ParseCIDR(cidr)
	return strings.Contains(cidr, ".") && e == nil
}

func IsIPv6CIDR(cidr string) bool {
	_, _, e := net.ParseCIDR(cidr)
	return strings.Contains(cidr, ":") && e == nil
}
