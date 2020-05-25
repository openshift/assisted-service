package connectivity

import (
	"encoding/json"
	"testing"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/sirupsen/logrus"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Connectivity Validator tests Suite")
}

var _ = Describe("connectivity_validator", func() {
	var (
		connectivityValidator Validator
		host                  *models.Host
		inventory             *models.Inventory
		cluster               *common.Cluster
		validDiskSize         = int64(128849018880)
	)
	BeforeEach(func() {
		connectivityValidator = NewValidator(logrus.New())
		id := strfmt.UUID(uuid.New().String())
		clusterID := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id, ClusterID: clusterID}
		inventory = &models.Inventory{
			CPU:    &models.CPU{Count: 16},
			Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB)},
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
			Disks: []*models.Disk{
				{DriveType: "ODD", Name: "loop0", SizeBytes: validDiskSize},
				{DriveType: "HDD", Name: "sdb", SizeBytes: validDiskSize}},
		}
		cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			MachineNetworkCidr: "1.2.3.0/24",
		}}
	})

	It("insufficient network", func() {
		cluster.MachineNetworkCidr = "10.11.0.0/16"
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host.Inventory = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			insufficient(connectivityValidator.IsSufficient(host, cluster))
		}
	})

	It("missing network", func() {
		cluster.MachineNetworkCidr = ""
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host.Inventory = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			insufficient(connectivityValidator.IsSufficient(host, cluster))
		}
	})

	It("illegal network", func() {
		cluster.MachineNetworkCidr = "blah"
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host.Inventory = string(hw)

		roles := []string{"", "master", "worker"}
		for _, role := range roles {
			host.Role = role
			insufficient(connectivityValidator.IsSufficient(host, cluster))
		}
	})

})

//func sufficient(reply *common.IsSufficientReply, err error) {
//	ExpectWithOffset(1, err).NotTo(HaveOccurred())
//	ExpectWithOffset(1, reply.IsSufficient).To(BeTrue())
//	ExpectWithOffset(1, reply.Reason).Should(Equal(""))
//}

func insufficient(reply *common.IsSufficientReply, err error) {
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, reply.IsSufficient).To(BeFalse())
	ExpectWithOffset(1, reply.Reason).ShouldNot(Equal(""))
}
