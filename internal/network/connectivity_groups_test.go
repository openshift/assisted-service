package network

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("connectivity groups", func() {

	var (
		hid1 strfmt.UUID = "11111111-1111-1111-1111-111111111111"
		hid2 strfmt.UUID = "22222222-2222-2222-2222-222222222222"
		hid3 strfmt.UUID = "33333333-3333-3333-3333-333333333333"
		hid4 strfmt.UUID = "44444444-4444-4444-4444-444444444444"
		hid5 strfmt.UUID = "55555555-5555-5555-5555-555555555555"
		hid6 strfmt.UUID = "66666666-6666-6666-6666-666666666666"
		hid7 strfmt.UUID = "77777777-7777-7777-7777-777777777777"
	)

	createL2 := func(outgoingIpAddress string, successful bool) *models.L2Connectivity {
		return &models.L2Connectivity{
			OutgoingIPAddress: outgoingIpAddress,
			Successful:        successful,
		}
	}

	createRemoteHost := func(id strfmt.UUID, l2s ...*models.L2Connectivity) *models.ConnectivityRemoteHost {
		return &models.ConnectivityRemoteHost{
			HostID:         id,
			L2Connectivity: l2s,
		}
	}

	createConnectiityReport := func(remoteHosts ...*models.ConnectivityRemoteHost) string {
		report := models.ConnectivityReport{
			RemoteHosts: remoteHosts,
		}
		b, err := json.Marshal(&report)
		Expect(err).ToNot(HaveOccurred())
		return string(b)
	}

	Context("connectivity groups", func() {
		It("Empty", func() {
			hosts := []*models.Host{
				{
					ID:           &hid1,
					Connectivity: createConnectiityReport(),
				},
				{
					ID:           &hid2,
					Connectivity: createConnectiityReport(),
				},
				{
					ID:           &hid3,
					Connectivity: createConnectiityReport(),
				},
			}
			ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
		It("Empty 2", func() {
			hosts := []*models.Host{
				{
					ID: &hid1,
				},
				{
					ID: &hid2,
				},
				{
					ID: &hid3,
				},
			}
			ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
			Expect(err).ToNot(HaveOccurred())
			Expect(ret).To(Equal([]strfmt.UUID{}))
		})
	})
	It("One with data", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID:           &hid2,
				Connectivity: createConnectiityReport(),
			},
			{
				ID:           &hid3,
				Connectivity: createConnectiityReport(),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(Equal([]strfmt.UUID{}))
	})
	It("3 with data", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true))),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.6", true)),
					createRemoteHost(hid2, createL2("1.2.3.6", true))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(hid1))
		Expect(ret).To(ContainElement(hid2))
		Expect(ret).To(ContainElement(hid3))
	})
	It("Different network", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true))),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("2.2.3.6", true)),
					createRemoteHost(hid2, createL2("2.2.3.6", true))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(Equal([]strfmt.UUID{}))
	})
	It("3 with data, additional network", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true), createL2("2.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true)),
					createRemoteHost(hid4, createL2("2.2.3.4", true)),
				),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true), createL2("2.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true)),
					createRemoteHost(hid4, createL2("2.2.3.4", true)),
				),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.6", true), createL2("2.2.3.6", true)),
					createRemoteHost(hid2, createL2("1.2.3.6", true), createL2("2.2.3.5", true))),
			},
			{
				ID: &hid4,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("2.2.3.6", true)),
					createRemoteHost(hid2, createL2("2.2.3.6", true))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(hid1))
		Expect(ret).To(ContainElement(hid2))
		Expect(ret).To(ContainElement(hid3))
		ret, err = CreateMajorityGroup("2.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(hid1))
		Expect(ret).To(ContainElement(hid2))
		Expect(ret).To(ContainElement(hid4))
	})
	It("7 - 2 groups", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true))),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.6", true)),
					createRemoteHost(hid2, createL2("1.2.3.6", true))),
			},
			{
				ID: &hid4,
				Connectivity: createConnectiityReport(createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true)),
				),
			},
			{
				ID: &hid5,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.5", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid6,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid7,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(4))
		Expect(ret).To(ContainElement(hid4))
		Expect(ret).To(ContainElement(hid5))
		Expect(ret).To(ContainElement(hid6))
		Expect(ret).To(ContainElement(hid7))
	})
	It("7 - 1 direction missing", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true))),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.6", true)),
					createRemoteHost(hid2, createL2("1.2.3.6", true))),
			},
			{
				ID: &hid4,
				Connectivity: createConnectiityReport(createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true)),
				),
			},
			{
				ID: &hid5,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.5", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid6,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid7,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", false))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(hid1))
		Expect(ret).To(ContainElement(hid2))
		Expect(ret).To(ContainElement(hid3))
	})
	It("7 - 2 directions missing", func() {
		hosts := []*models.Host{
			{
				ID: &hid1,
				Connectivity: createConnectiityReport(createRemoteHost(hid2, createL2("1.2.3.4", true)),
					createRemoteHost(hid3, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid2,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.5", true)),
					createRemoteHost(hid3, createL2("1.2.3.5", true))),
			},
			{
				ID: &hid3,
				Connectivity: createConnectiityReport(createRemoteHost(hid1, createL2("1.2.3.6", true)),
					createRemoteHost(hid2, createL2("1.2.3.6", false))),
			},
			{
				ID: &hid4,
				Connectivity: createConnectiityReport(createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true)),
				),
			},
			{
				ID: &hid5,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.5", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid6,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid7, createL2("1.2.3.4", true))),
			},
			{
				ID: &hid7,
				Connectivity: createConnectiityReport(createRemoteHost(hid4, createL2("1.2.3.6", true)),
					createRemoteHost(hid5, createL2("1.2.3.4", true)),
					createRemoteHost(hid6, createL2("1.2.3.4", false))),
			},
		}
		ret, err := CreateMajorityGroup("1.2.3.0/24", hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(ret).To(HaveLen(3))
		Expect(ret).To(ContainElement(hid4))
		Expect(ret).To(ContainElement(hid5))
		Expect(ret).To(ContainElement(hid6))
	})
})
