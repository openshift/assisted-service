package host

import (
	"strings"

	"github.com/filanov/bm-inventory/internal/connectivity"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("connectivitycheckconvertor", func() {
	var (
		ctrl                                       *gomock.Controller
		mockValidator                              *connectivity.MockValidator
		currentHostId, hostId2, hostId3, clusterId strfmt.UUID
		hosts                                      []*models.Host
		interfaces                                 []*models.Interface
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		mockValidator = connectivity.NewMockValidator(ctrl)

		clusterId = strfmt.UUID(uuid.New().String())
		currentHostId = strfmt.UUID(uuid.New().String())
		hostId2 = strfmt.UUID(uuid.New().String())
		hostId3 = strfmt.UUID(uuid.New().String())
		currentHostId = strfmt.UUID(uuid.New().String())
		hosts = []*models.Host{
			{ID: &currentHostId, ClusterID: clusterId},
			{ID: &hostId2, ClusterID: clusterId},
			{ID: &hostId3, ClusterID: clusterId},
		}

		interfaces = []*models.Interface{
			{Name: "eth0", MacAddress: "44:85:00:80:12:a4", IPV4Addresses: []string{"10.0.0.1/24", "10.0.0.2", "10.0.0.3/24"}},
			{Name: "eth1", MacAddress: "45:85:00:80:12:a4", IPV4Addresses: []string{"10.0.0.4", "10.0.0.5/24", "10.0.0.6", "10.0.0.7/24"}},
		}
	})

	It("convertNicsToConnectivityParamsHost_success", func() {
		connectivityParamsHost := convertInterfacesToConnectivityCheckHost(&currentHostId, interfaces)
		Expect(connectivityParamsHost.HostID.String()).To(Equal(currentHostId.String()))
		Expect(connectivityParamsHost.Nics).To(HaveLen(2))
		Expect(connectivityParamsHost.Nics[0].IPAddresses).To(HaveLen(3))
		Expect(connectivityParamsHost.Nics[1].IPAddresses).To(HaveLen(4))
	})

	It("convertHostsToConnectivityParamsHosts_success", func() {
		mockValidator.EXPECT().GetHostValidInterfaces(gomock.Any()).Return(interfaces, nil).AnyTimes()
		jsonData, err := convertHostsToConnectivityCheckParams(&currentHostId, hosts, mockValidator)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(jsonData).ShouldNot(Equal(""))
		Expect(strings.Contains(jsonData, currentHostId.String())).To(Equal(false))
		Expect(strings.Contains(jsonData, hostId2.String())).To(Equal(true))
		Expect(strings.Contains(jsonData, hostId3.String())).To(Equal(true))
	})

	It("convertHostsToConnectivityParamsHosts_no_hosts", func() {
		mockValidator.EXPECT().GetHostValidInterfaces(gomock.Any()).Return(interfaces, nil).AnyTimes()
		var no_hosts []*models.Host
		jsonData, err := convertHostsToConnectivityCheckParams(&currentHostId, no_hosts, mockValidator)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(jsonData).Should(Equal(""))
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
