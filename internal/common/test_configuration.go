package common

import (
	"encoding/json"
	"io/ioutil"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/gomega"
)

type TestConfiguration struct {
	OpenShiftVersion  string
	Status            string
	StatusInfo        string
	HostProgressStage models.HostStage

	Disks *models.Disk
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

func GetTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}
