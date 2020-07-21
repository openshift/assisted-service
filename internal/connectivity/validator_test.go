package connectivity

import (
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"

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

var _ = Describe("get valid interfaces", func() {
	var (
		connectivityValidator Validator
		host                  *models.Host
		inventory             *models.Inventory
	)
	BeforeEach(func() {
		connectivityValidator = NewValidator(logrus.New())
		id := strfmt.UUID(uuid.New().String())
		clusterID := strfmt.UUID(uuid.New().String())
		host = &models.Host{ID: &id, ClusterID: clusterID}
		inventory = &models.Inventory{
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
				},
			},
		}
	})

	It("valid interfaces", func() {
		hw, err := json.Marshal(&inventory)
		Expect(err).NotTo(HaveOccurred())
		host.Inventory = string(hw)
		interfaces, err := connectivityValidator.GetHostValidInterfaces(host)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(interfaces)).Should(Equal(1))
	})

	It("invalid interfaces", func() {

		host.Inventory = ""
		interfaces, err := connectivityValidator.GetHostValidInterfaces(host)
		Expect(err).To(HaveOccurred())
		Expect(interfaces).To(BeNil())
	})

})
