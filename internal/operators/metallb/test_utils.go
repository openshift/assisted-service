package metallb

import (
	"encoding/json"

	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

type InventoryResources struct {
	Cpus  int64
	Ram   int64
	Disks []*models.Disk
}

func Inventory(r *InventoryResources) string {
	inventory := models.Inventory{
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
		CPU: &models.CPU{
			Count: r.Cpus,
		},
		Memory: &models.Memory{
			UsableBytes: r.Ram,
		},
		Disks: r.Disks,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}
