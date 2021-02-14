package common

import (
	"encoding/json"
	"io/ioutil"

	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type TestConfiguration struct {
	OpenShiftVersion  string
	Status            string
	StatusInfo        string
	HostProgressStage models.HostStage

	Disks     *models.Disk
	ImageName string
}

// Defaults to be used by all testing modules
var TestDefaultConfig = &TestConfiguration{
	OpenShiftVersion:  "4.6",
	Status:            "status",
	StatusInfo:        "statusInfo",
	HostProgressStage: models.HostStage("default progress stage"),

	Disks: &models.Disk{
		Name:   "test-disk",
		Serial: "test-serial",
		InstallationEligibility: models.DiskInstallationEligibility{
			Eligible:           false,
			NotEligibleReasons: []string{"Bad disk"},
		},
	},

	ImageName: "image",
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
