package common

import (
	"encoding/json"
	"io/ioutil"

	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type TestConfiguration struct {
	OpenShiftVersion  string
	Status            string
	StatusInfo        string
	HostProgressStage models.HostStage

	Disks     *models.Disk
	ImageName string

	MonitoredOperator models.MonitoredOperator
}

const TestDiskId = "/dev/disk/by-id/test-disk-id"
const TestDiskPath = "/dev/test-disk"

// Defaults to be used by all testing modules
var TestDefaultConfig = &TestConfiguration{
	OpenShiftVersion:  "4.6",
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

var TestNTPSourceSynced = &models.NtpSource{SourceName: "clock.dummy.com", SourceState: models.SourceStateSynced}
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
	}

	b, err := json.Marshal(inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

func GenerateTestDefaultInventoryIPv4Only() string {
	defaultInventory := GenerateTestDefaultInventory()
	var inventory models.Inventory
	Expect(json.Unmarshal([]byte(defaultInventory), &inventory)).ToNot(HaveOccurred())
	inventory.Interfaces[0].IPV6Addresses = nil

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
