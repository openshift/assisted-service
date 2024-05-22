package common

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type TestNetworking struct {
	ClusterNetworks []*models.ClusterNetwork
	ServiceNetworks []*models.ServiceNetwork
	MachineNetworks []*models.MachineNetwork
	APIVips         []*models.APIVip
	IngressVips     []*models.IngressVip
}

type TestConfiguration struct {
	OpenShiftVersion string
	ReleaseVersion   string
	ReleaseImageUrl  string
	RhcosImage       string
	RhcosVersion     string
	SupportLevel     string
	CPUArchitecture  string
	Version          *models.OpenshiftVersion
	ReleaseImage     *models.ReleaseImage
	OsImage          *models.OsImage

	Status            string
	StatusInfo        string
	HostProgressStage models.HostStage

	Disks         *models.Disk
	ImageName     string
	ClusterName   string
	BaseDNSDomain string

	MonitoredOperator models.MonitoredOperator
}

const TestDiskId = "/dev/disk/by-id/test-disk-id"
const TestDiskPath = "/dev/test-disk"

var (
	OpenShiftVersion string = "4.6"
	ReleaseVersion          = "4.6.0"
	ReleaseImageURL         = "quay.io/openshift-release-dev/ocp-release:4.6.16-x86_64"
	RhcosImage              = "rhcos_4.6.0"
	RhcosVersion            = "version-46.123-0"
	SupportLevel            = "beta"
	CPUArchitecture         = DefaultCPUArchitecture
)

// Defaults to be used by all testing modules
var TestDefaultConfig = &TestConfiguration{
	OpenShiftVersion: OpenShiftVersion,
	ReleaseVersion:   ReleaseVersion,
	ReleaseImageUrl:  ReleaseImageURL,
	CPUArchitecture:  CPUArchitecture,
	ReleaseImage: &models.ReleaseImage{
		CPUArchitecture:  &CPUArchitecture,
		OpenshiftVersion: &OpenShiftVersion,
		URL:              &ReleaseImageURL,
		Version:          &ReleaseVersion,
		CPUArchitectures: []string{CPUArchitecture},
	},
	OsImage: &models.OsImage{
		CPUArchitecture:  &CPUArchitecture,
		OpenshiftVersion: &OpenShiftVersion,
		URL:              &RhcosImage,
		Version:          &RhcosVersion,
	},
	Status:            "status",
	StatusInfo:        "statusInfo",
	HostProgressStage: models.HostStage("default progress stage"),

	Disks: &models.Disk{
		ID:     TestDiskId,
		Name:   "test-disk",
		Serial: "test-serial",
		InstallationEligibility: models.DiskInstallationEligibility{
			Eligible:           false,
			NotEligibleReasons: []string{"Bad disk"},
		},
	},

	ImageName: "image",

	ClusterName: "test-cluster",

	BaseDNSDomain: "example.com",

	MonitoredOperator: models.MonitoredOperator{
		Name:         "dummy",
		OperatorType: models.OperatorTypeBuiltin,
	},
}

var TestNTPSourceSynced = &models.NtpSource{SourceName: "clock.dummy.test", SourceState: models.SourceStateSynced}
var TestNTPSourceUnsynced = &models.NtpSource{SourceName: "2.2.2.2", SourceState: models.SourceStateUnreachable}
var TestImageStatusesSuccess = &models.ContainerImageAvailability{
	Name:         TestDefaultConfig.ImageName,
	Result:       models.ContainerImageAvailabilityResultSuccess,
	SizeBytes:    333000000.0,
	Time:         10.0,
	DownloadRate: 33.3,
}
var TestImageStatusesFailure = &models.ContainerImageAvailability{
	Name:   TestDefaultConfig.ImageName,
	Result: models.ContainerImageAvailabilityResultFailure,
}

var DomainAPI = "api.test-cluster.example.com"
var DomainAPIInternal = "api-int.test-cluster.example.com"
var DomainApps = fmt.Sprintf("%s.apps.test-cluster.example.com", constants.AppsSubDomainNameHostDNSValidation)
var UndottedWildcardDomain = fmt.Sprintf("%s.test-cluster.example.com", constants.DNSWildcardFalseDomainName)
var WildcardDomain = UndottedWildcardDomain + "."
var ReleaseDomain = "quay.io"

var DomainResolutions = []*models.DomainResolutionResponseDomain{
	{
		DomainName:    &DomainAPI,
		IPV4Addresses: []strfmt.IPv4{"1.2.3.40/24"},
		IPV6Addresses: []strfmt.IPv6{"1001:db8::20/120"},
	},
	{
		DomainName:    &DomainAPIInternal,
		IPV4Addresses: []strfmt.IPv4{"4.5.6.7/24"},
		IPV6Addresses: []strfmt.IPv6{"1002:db8::30/120"},
	},
	{
		DomainName:    &DomainApps,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::40/120"},
	},
	{
		DomainName:    &ReleaseDomain,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.11/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::41/120"},
	},
	{
		DomainName:    &WildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &UndottedWildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
}

var DomainResolutionsWithCname = []*models.DomainResolutionResponseDomain{
	{
		DomainName: &DomainAPI,
		Cnames:     []string{"api.cname.com"},
	},
	{
		DomainName: &DomainAPIInternal,
		Cnames:     []string{"api-int.cname.com"},
	},
	{
		DomainName: &DomainApps,
		Cnames:     []string{"console.apps.cname.com"},
	},
	{
		DomainName: &ReleaseDomain,
		Cnames:     []string{"release.cname.com"},
	},
	{
		DomainName: &WildcardDomain,
	},
	{
		DomainName: &UndottedWildcardDomain,
	},
}

var WildcardResolved = []*models.DomainResolutionResponseDomain{
	{
		DomainName:    &WildcardDomain,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::40/120"},
	},
	{
		DomainName:    &UndottedWildcardDomain,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::40/120"},
	},
}

var WildcardResolvedWithCname = []*models.DomainResolutionResponseDomain{
	{
		DomainName: &WildcardDomain,
		Cnames:     []string{"a.test.com"},
	},
	{
		DomainName: &UndottedWildcardDomain,
		Cnames:     []string{"a.test.com"},
	},
}

var SubDomainWildcardResolved = []*models.DomainResolutionResponseDomain{
	{
		DomainName:    &WildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &UndottedWildcardDomain,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::40/120"},
	},
}

var DomainResolutionNoAPI = []*models.DomainResolutionResponseDomain{
	{
		DomainName:    &DomainApps,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.10/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::40/120"},
	},
	{
		DomainName:    &WildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &UndottedWildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &ReleaseDomain,
		IPV4Addresses: []strfmt.IPv4{"7.8.9.11/24"},
		IPV6Addresses: []strfmt.IPv6{"1003:db8::41/120"},
	},
}

var DomainResolutionAllEmpty = []*models.DomainResolutionResponseDomain{
	{
		DomainName:    &DomainAPI,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &DomainAPIInternal,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &DomainApps,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &WildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
	{
		DomainName:    &UndottedWildcardDomain,
		IPV4Addresses: []strfmt.IPv4{},
		IPV6Addresses: []strfmt.IPv6{},
	},
}

var TestDomainNameResolutionsSuccess = &models.DomainResolutionResponse{Resolutions: DomainResolutions}
var TestDomainNameResolutionsSuccessWithCname = &models.DomainResolutionResponse{Resolutions: DomainResolutionsWithCname}
var TestDomainResolutionsNoAPI = &models.DomainResolutionResponse{Resolutions: DomainResolutionNoAPI}
var TestDomainResolutionsAllEmpty = &models.DomainResolutionResponse{Resolutions: DomainResolutionAllEmpty}
var TestDomainNameResolutionsWildcardResolved = &models.DomainResolutionResponse{Resolutions: WildcardResolved}
var TestDomainNameResolutionsWildcardResolvedWithCname = &models.DomainResolutionResponse{Resolutions: WildcardResolvedWithCname}
var TestSubDomainNameResolutionsWildcardResolved = &models.DomainResolutionResponse{Resolutions: SubDomainWildcardResolved}

var TestDefaultRouteConfiguration = []*models.Route{{Family: FamilyIPv4, Interface: "eth0", Gateway: "1.2.3.10", Destination: "0.0.0.0", Metric: 600}}

var TestIPv4Networking = TestNetworking{
	ClusterNetworks: []*models.ClusterNetwork{{Cidr: "1.3.0.0/16", HostPrefix: 24}},
	ServiceNetworks: []*models.ServiceNetwork{{Cidr: "1.2.5.0/24"}},
	MachineNetworks: []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}},
	APIVips:         []*models.APIVip{{IP: "1.2.3.5", Verification: VipVerificationPtr(models.VipVerificationSucceeded)}},
	IngressVips:     []*models.IngressVip{{IP: "1.2.3.6", Verification: VipVerificationPtr(models.VipVerificationSucceeded)}},
}

// TestIPv6Networking The values of TestIPv6Networking and TestEquivalentIPv6Networking are not equal, but are equivalent
// in terms of their values. If any of the values in TestIPv6Networking change, please change also the corresponding
// values in TestEquivalentIPv6Networking
var TestIPv6Networking = TestNetworking{
	ClusterNetworks: []*models.ClusterNetwork{{Cidr: "1003:db8::/53", HostPrefix: 64}},
	ServiceNetworks: []*models.ServiceNetwork{{Cidr: "1002:db8::/119"}},
	MachineNetworks: []*models.MachineNetwork{{Cidr: "1001:db8::/120"}},
	APIVips:         []*models.APIVip{{IP: "1001:db8::64", Verification: VipVerificationPtr(models.VipVerificationSucceeded)}},
	IngressVips:     []*models.IngressVip{{IP: "1001:db8::65", Verification: VipVerificationPtr(models.VipVerificationSucceeded)}},
}

var TestEquivalentIPv6Networking = TestNetworking{
	ClusterNetworks: []*models.ClusterNetwork{{Cidr: "1003:0db8:0::/53", HostPrefix: 64}},
	ServiceNetworks: []*models.ServiceNetwork{{Cidr: "1002:0db8:0::/119"}},
	MachineNetworks: []*models.MachineNetwork{{Cidr: "1001:0db8:0::/120"}},
	APIVips:         []*models.APIVip{{IP: "1001:db8::64"}},
	IngressVips:     []*models.IngressVip{{IP: "1001:db8::65"}},
}

var TestDualStackNetworking = TestNetworking{
	ClusterNetworks: append(TestIPv4Networking.ClusterNetworks, TestIPv6Networking.ClusterNetworks...),
	ServiceNetworks: append(TestIPv4Networking.ServiceNetworks, TestIPv6Networking.ServiceNetworks...),
	MachineNetworks: append(TestIPv4Networking.MachineNetworks, TestIPv6Networking.MachineNetworks...),
	APIVips:         TestIPv4Networking.APIVips,
	IngressVips:     TestIPv4Networking.IngressVips,
}

func IncrementCidrIP(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	IncrementIP(cidr.IP)
	return cidr.String()
}

func IncrementIPString(ipString string) string {
	ip := net.ParseIP(ipString)
	IncrementIP(ip)
	return ip.String()
}

func IncrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func IncrementCidrMask(subnet string) string {
	_, cidr, _ := net.ParseCIDR(subnet)
	ones, bits := cidr.Mask.Size()
	cidr.Mask = net.CIDRMask(ones+1, bits)
	return cidr.String()
}

func GenerateTestDefaultInventory() string {
	inventory := &models.Inventory{
		CPU: &models.CPU{
			Architecture: models.ClusterCPUArchitectureX8664,
		},
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func generateInterfaces(amount int, intfType string) []*models.Interface {
	interfaces := make([]*models.Interface, amount)
	for i := 0; i < amount; i++ {
		intf := models.Interface{
			Name: fmt.Sprintf("eth%d", i),
			IPV4Addresses: []string{
				fmt.Sprintf("192.%d.2.0/24", i),
			},
			IPV6Addresses: []string{
				fmt.Sprintf("2001:db%d::/32", i),
			},
			Type: intfType,
		}
		interfaces[i] = &intf
	}
	return interfaces
}

func GenerateTestInventoryWithVirtualInterface(physicalInterfaces, virtualInterfaces int) string {
	interfaces := generateInterfaces(physicalInterfaces, "physical")
	interfaces = append(interfaces, generateInterfaces(virtualInterfaces, "device")...)
	inventory := &models.Inventory{
		CPU: &models.CPU{
			Architecture: models.ClusterCPUArchitectureX8664,
		},
		Interfaces: interfaces,
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTest2IPv4AddressesInventory() string {
	inventory := &models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
					"7.8.9.10/24",
				},
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTestIPv6Inventory() string {
	inventory := &models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func CreateWildcardDomainNameResolutionReply(name string, baseDomain string) *models.DomainResolutionResponse {

	undottedDomain := fmt.Sprintf("%s.%s.%s", constants.DNSWildcardFalseDomainName, name, baseDomain)
	domain := undottedDomain + "."

	var domainNameWildcardConfig = []*models.DomainResolutionResponseDomain{
		{
			DomainName:    &undottedDomain,
			IPV4Addresses: []strfmt.IPv4{},
			IPV6Addresses: []strfmt.IPv6{},
		},
		{
			DomainName:    &domain,
			IPV4Addresses: []strfmt.IPv4{},
			IPV6Addresses: []strfmt.IPv6{},
		},
	}

	var testDomainNameResolutionWildcard = &models.DomainResolutionResponse{
		Resolutions: domainNameWildcardConfig}

	return testDomainNameResolutionWildcard
}

type NetAddress struct {
	IPv4Address []string
	IPv6Address []string
	Hostname    string
}

func GenerateTestInventoryWithNetwork(netAddress NetAddress) string {
	inventory := &models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name:          "eth0",
				IPV4Addresses: netAddress.IPv4Address,
				IPV6Addresses: netAddress.IPv6Address,
			},
		},
		Disks:        []*models.Disk{{SizeBytes: conversions.GibToBytes(120), DriveType: models.DriveTypeHDD}},
		CPU:          &models.CPU{Count: 16},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Hostname:     netAddress.Hostname,
		Routes:       TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTestInventoryWithMutate(mutateFn func(*models.Inventory)) string {
	inventory := &models.Inventory{
		Interfaces: []*models.Interface{
			{
				Name: "eth0",
				IPV4Addresses: []string{
					"1.2.3.4/24",
				},
				IPV6Addresses: []string{
					"1001:db8::10/120",
				},
			},
		},
		Disks:        []*models.Disk{{SizeBytes: conversions.GibToBytes(120), DriveType: models.DriveTypeHDD}},
		CPU:          &models.CPU{Count: 16},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Routes:       TestDefaultRouteConfiguration,
	}
	mutateFn(inventory)
	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTestInventory() string {
	return GenerateTestInventoryWithMutate(func(inventory *models.Inventory) {})
}

func GenerateTestInventoryWithTpmVersion(tpmVersion string) string {
	inventory := &models.Inventory{
		Disks:        []*models.Disk{{SizeBytes: conversions.GibToBytes(120), DriveType: models.DriveTypeHDD}},
		CPU:          &models.CPU{Count: 16},
		Memory:       &models.Memory{PhysicalBytes: conversions.GibToBytes(16), UsableBytes: conversions.GibToBytes(16)},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		TpmVersion:   tpmVersion,
	}
	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GetTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

type StaticNetworkConfig struct {
	DNSResolver DNSResolver  `yaml:"dns-resolver"`
	Interfaces  []Interfaces `yaml:"interfaces"`
	Routes      Routes       `yaml:"routes"`
}
type DNSResolverConfig struct {
	Server []string `yaml:"server"`
}
type DNSResolver struct {
	Config DNSResolverConfig `yaml:"config"`
}
type Address struct {
	IP           string `yaml:"ip"`
	PrefixLength int    `yaml:"prefix-length"`
}
type Ipv4 struct {
	Address []Address `yaml:"address"`
	Dhcp    bool      `yaml:"dhcp"`
	Enabled bool      `yaml:"enabled"`
}
type Interfaces struct {
	Ipv4  Ipv4   `yaml:"ipv4"`
	Name  string `yaml:"name"`
	State string `yaml:"state"`
	Type  string `yaml:"type"`
}
type RouteConfig struct {
	Destination      string `yaml:"destination"`
	NextHopAddress   string `yaml:"next-hop-address"`
	NextHopInterface string `yaml:"next-hop-interface"`
	TableID          int    `yaml:"table-id"`
}
type Routes struct {
	Config []RouteConfig `yaml:"config"`
}

func FormatStaticConfigHostYAML(nicPrimary, nicSecondary, ip4Master, ip4Secondary, dnsGW string, macInterfaceMap models.MacInterfaceMap) *models.HostStaticNetworkConfig {
	staticNetworkConfig := StaticNetworkConfig{
		DNSResolver: DNSResolver{
			Config: DNSResolverConfig{
				Server: []string{dnsGW},
			},
		},
		Interfaces: []Interfaces{
			{
				Ipv4: Ipv4{
					Address: []Address{
						{
							IP:           ip4Master,
							PrefixLength: 24,
						},
					},
					Dhcp:    false,
					Enabled: true,
				},
				Name:  nicPrimary,
				State: "up",
				Type:  "ethernet",
			},
			{
				Ipv4: Ipv4{
					Address: []Address{
						{
							IP:           ip4Secondary,
							PrefixLength: 24,
						},
					},
					Dhcp:    false,
					Enabled: true,
				},
				Name:  nicSecondary,
				State: "up",
				Type:  "ethernet",
			},
		},
		Routes: Routes{
			Config: []RouteConfig{
				{
					Destination:      "0.0.0.0/0",
					NextHopAddress:   dnsGW,
					NextHopInterface: nicPrimary,
					TableID:          254,
				},
			},
		},
	}

	output, _ := yaml.Marshal(staticNetworkConfig)
	return &models.HostStaticNetworkConfig{MacInterfaceMap: macInterfaceMap, NetworkYaml: string(output)}
}
