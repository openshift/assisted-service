package common

import (
	"encoding/json"
	"io/ioutil"

	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type TestConfiguration struct {
	OpenShiftVersion string
	ReleaseVersion   string
	ReleaseImage     string
	RhcosImage       string
	RhcosVersion     string
	SupportLevel     string
	Version          *models.OpenshiftVersion

	Status            string
	StatusInfo        string
	HostProgressStage models.HostStage

	Disks     *models.Disk
	ImageName string

	MonitoredOperator models.MonitoredOperator
}

const TestDiskId = "/dev/disk/by-id/test-disk-id"
const TestDiskPath = "/dev/test-disk"

var (
	OpenShiftVersion string = "4.6"
	ReleaseVersion          = "4.6.0"
	ReleaseImage            = "quay.io/openshift-release-dev/ocp-release:4.6.16-x86_64"
	RhcosImage              = "rhcos_4.6.0"
	RhcosVersion            = "version-46.123-0"
	SupportLevel            = "beta"
)

// Defaults to be used by all testing modules
var TestDefaultConfig = &TestConfiguration{
	OpenShiftVersion: OpenShiftVersion,
	ReleaseVersion:   ReleaseVersion,
	ReleaseImage:     ReleaseImage,
	Version: &models.OpenshiftVersion{
		DisplayName:    &OpenShiftVersion,
		ReleaseImage:   &ReleaseImage,
		ReleaseVersion: &ReleaseVersion,
		RhcosImage:     &RhcosImage,
		RhcosVersion:   &RhcosVersion,
		SupportLevel:   &SupportLevel,
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

var TestDefaultRouteConfiguration = []*models.Route{{Family: FamilyIPv4, Interface: "eth0", Gateway: "192.168.1.1", Destination: "0.0.0.0"}}

func GenerateTestDefaultInventory() string {
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
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTestDefaultVmwareInventory() string {
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
		Disks: []*models.Disk{
			TestDefaultConfig.Disks,
		},
		SystemVendor: &models.SystemVendor{
			Manufacturer: "vmware",
		},
		Routes: TestDefaultRouteConfiguration,
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
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
		Disks:        []*models.Disk{{SizeBytes: conversions.GibToBytes(120), DriveType: "HDD"}},
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

func GetTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
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
