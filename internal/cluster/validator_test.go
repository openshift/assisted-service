package cluster

import (
	"fmt"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/testing"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

var _ = Describe("isNetworksSameAddressFamilies", func() {

	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
	)

	BeforeEach(func() {
		validator = clusterValidator{log: logrus.New()}
		preprocessContext = &clusterPreprocessContext{}
		clusterID = strfmt.UUID(uuid.New().String())
	})

	It("Returns ValidationPending when cluster and service network are unset and required", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationPending))
		Expect(message).Should(Equal("At least one of the CIDRs (Cluster Network, Service Network) is undefined."))
	})

	It("Returns ValidationPending when machine, cluster and service network are unset and required", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(1)

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationPending))
		Expect(message).Should(Equal("At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined."))
	})

	It("Returns ValidationError when service networks contain an invalid CIDR", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "192.168.40.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "10.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal("Bad CIDR(s) appears in one of the networks"))
	})

	It("Returns ValidationError when clusterNetworks contain an invalid CIDR", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "192.168.40.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "10.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal("Bad CIDR(s) appears in one of the networks"))
	})

	It("Returns ValidationError cluster and service network address families match but there is an invalid CIDR in machine network", func() {
		userManagedNetworking := false
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "20.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal(fmt.Sprintf("Error getting machine address families for cluster %s", clusterID)))
	})

	It("Returns ValidationFailure when the address families of service and cluster networks do not match", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "192.168.10.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationFailure))
		Expect(message).Should(Equal("Address families of networks (ServiceNetworks, ClusterNetworks) are not the same."))
	})

	It("Returns ValidationFailure cluster and service network address families match but mismatch for machine network families", func() {
		userManagedNetworking := false
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "20.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationFailure))
		Expect(message).Should(Equal("Address families of networks (MachineNetworks, ServiceNetworks, ClusterNetworks) are not the same."))
	})

	It("Returns ValidationSuccess when machine, service and cluster network families match and they are all required", func() {
		userManagedNetworking := false
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationSuccess))
		Expect(message).Should(Equal("Same address families for all networks."))
	})

	It("Returns ValidationSuccess when service and cluster network families match and machine network is not required", func() {
		userManagedNetworking := true
		controlPlaneCount := int64(common.MinMasterHostsNeededForInstallationInHaMode)

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "192.168.10.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			ControlPlaneCount:     controlPlaneCount,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationSuccess))
		Expect(message).Should(Equal("Same address families for all networks."))
	})
})

var _ = Describe("areVipsValid", func() {

	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
		hosts             []*models.Host
	)

	newId := func() strfmt.UUID {
		return strfmt.UUID(uuid.New().String())
	}
	newIdPtr := func() *strfmt.UUID {
		ret := newId()
		return &ret
	}

	BeforeEach(func() {
		validator = clusterValidator{log: logrus.New()}
		preprocessContext = &clusterPreprocessContext{}
		clusterID = strfmt.UUID(uuid.New().String())
		hosts = []*models.Host{
			{
				ClusterID:  &clusterID,
				InfraEnvID: clusterID,
				ID:         newIdPtr(),
				Inventory:  common.GenerateTestDefaultInventory(),
			},
			{
				ClusterID:  &clusterID,
				InfraEnvID: clusterID,
				ID:         newIdPtr(),
				Inventory:  common.GenerateTestDefaultInventory(),
			},
			{
				ClusterID:  &clusterID,
				InfraEnvID: clusterID,
				ID:         newIdPtr(),
				Inventory:  common.GenerateTestDefaultInventory(),
			},
		}
	})

	clearApiVipsVerfication := func(vips []*models.APIVip) []*models.APIVip {
		return funk.Map(vips, func(v *models.APIVip) *models.APIVip {
			return &models.APIVip{
				ClusterID: v.ClusterID,
				IP:        v.IP,
			}
		}).([]*models.APIVip)
	}

	clearIngressVIpsVerification := func(vips []*models.IngressVip) []*models.IngressVip {
		return funk.Map(vips, func(v *models.IngressVip) *models.IngressVip {
			return &models.IngressVip{
				ClusterID: v.ClusterID,
				IP:        v.IP,
			}
		}).([]*models.IngressVip)
	}

	type loopContext struct {
		name     string
		function validationConditon
	}
	apiContext := loopContext{name: "API", function: validator.areApiVipsValid}
	ingressContext := loopContext{name: "Ingress", function: validator.areIngressVipsValid}
	for _, lc := range []loopContext{apiContext, ingressContext} {
		lcontext := lc
		Context(fmt.Sprintf("- %s vips validation:", lcontext.name), func() {
			It("user-managed networking", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:                    &clusterID,
					UserManagedNetworking: swag.Bool(true),
				}}

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(Equal(fmt.Sprintf("%s virtual IPs are not required: User Managed Networking", lcontext.name)))
			})

			It("SNO", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:                &clusterID,
					ControlPlaneCount: 1,
				}}

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(Equal(fmt.Sprintf("%s virtual IPs are not required: SNO", lcontext.name)))
			})

			It("vips undefined", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationPending))
				Expect(message).Should(Equal(fmt.Sprintf("%s virtual IPs are undefined.", lcontext.name)))
			})

			It("vips defined - no hosts", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:          &clusterID,
					APIVips:     clearApiVipsVerfication(common.TestDualStackNetworking.APIVips),
					IngressVips: clearIngressVIpsVerification(common.TestDualStackNetworking.IngressVips),
				}}

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationPending))
				Expect(message).Should(Equal("Hosts have not been discovered yet"))
			})

			It("cluster-managed load balancer - vip outside machine network", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: []*models.MachineNetwork{{Cidr: "1.2.4.0/24"}},
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(ContainSubstring("does not belong to machine-network-cidr"))
			})

			It("cluster-managed load balancer - vip is the broadcast address in machine network", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}},
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.255"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.255"}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(ContainSubstring("is the broadcast address of machine-network-cidr"))
			})

			It("cluster-managed load balancer - no free addresses", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips <1.2.3.[56]> are not verified yet", strings.ToLower(lcontext.name))))
			})

			It("cluster-managed load balancer - vips in the free addresses list", func() {
				hosts = []*models.Host{
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
				}
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips 1.2.3.[56] belongs to the Machine CIDR and is not in use.", strings.ToLower(lcontext.name))))
			})

			It("cluster-managed load balancer - verification succeeded already", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5", Verification: models.NewVipVerification(models.VipVerificationSucceeded)}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6", Verification: models.NewVipVerification(models.VipVerificationSucceeded)}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips 1.2.3.[56] belongs to the Machine CIDR and is not in use.", strings.ToLower(lcontext.name))))
			})

			It("cluster-managed load balancer - verification failed already", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5", Verification: models.NewVipVerification(models.VipVerificationFailed)}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6", Verification: models.NewVipVerification(models.VipVerificationFailed)}},
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips <1.2.3.[56]> is already in use in cidr 1.2.3.0/24", strings.ToLower(lcontext.name))))
			})

			It("user-managed load balancer - vip outside machine network", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: []*models.MachineNetwork{{Cidr: "1.2.4.0/24"}},
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(ContainSubstring("does not belong to machine-network-cidr"))
			})

			It("user-managed load balancer - vip is the broadcast address in machine network", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: []*models.MachineNetwork{{Cidr: "1.2.3.0/24"}},
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.255"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.255"}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(ContainSubstring("is the broadcast address of machine-network-cidr"))
			})

			It("user-managed load balancer - no free addresses", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vip 1.2.3.[56] is valid", strings.ToLower(lcontext.name))))
			})

			It("user-managed load balancer - vips in the free addresses list", func() {
				hosts = []*models.Host{
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
					{
						FreeAddresses: makeFreeNetworksAddressesStr(makeFreeAddresses(
							"1.2.3.0/24", "1.2.3.5", "1.2.3.6",
						)),
					},
				}
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5"}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6"}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vip 1.2.3.[56] is valid", strings.ToLower(lcontext.name))))
			})

			It("user-managed load balancer - verification succeeded already", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5", Verification: models.NewVipVerification(models.VipVerificationSucceeded)}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6", Verification: models.NewVipVerification(models.VipVerificationSucceeded)}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vip 1.2.3.[56] is valid", strings.ToLower(lcontext.name))))
			})

			It("user-managed load balancer - verification failed already", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         []*models.APIVip{{ClusterID: clusterID, IP: "1.2.3.5", Verification: models.NewVipVerification(models.VipVerificationFailed)}},
					IngressVips:     []*models.IngressVip{{ClusterID: clusterID, IP: "1.2.3.6", Verification: models.NewVipVerification(models.VipVerificationFailed)}},
					Hosts:           hosts,
					LoadBalancer:    &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vip 1.2.3.[56] is valid", strings.ToLower(lcontext.name))))
			})

			It("ipv6 vips verified", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         append(common.TestIPv4Networking.APIVips, clearApiVipsVerfication(common.TestIPv6Networking.APIVips)...),
					IngressVips:     append(common.TestIPv4Networking.IngressVips, clearIngressVIpsVerification(common.TestIPv6Networking.IngressVips)...),
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips <1001:db8::6[45]> are not verified yet", strings.ToLower(lcontext.name))))
			})

			It("all successful", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         append(common.TestIPv4Networking.APIVips, common.TestIPv6Networking.APIVips...),
					IngressVips:     append(common.TestIPv4Networking.IngressVips, common.TestIPv6Networking.IngressVips...),
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true

				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationSuccess))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips 1.2.3.[56], 1001:db8::6[45] belongs to the Machine CIDR and is not in use", strings.ToLower(lcontext.name))))
			})

			It("ipv4 verification failed", func() {
				preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
					ID:              &clusterID,
					MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
					APIVips:         append(clearApiVipsVerfication(common.TestIPv4Networking.APIVips), common.TestIPv6Networking.APIVips...),
					IngressVips:     append(clearIngressVIpsVerification(common.TestIPv4Networking.IngressVips), common.TestIPv6Networking.IngressVips...),
					Hosts:           hosts,
				}}
				preprocessContext.hasHostsWithInventories = true
				preprocessContext.cluster.APIVips[0].Verification = common.VipVerificationPtr(models.VipVerificationFailed)
				preprocessContext.cluster.IngressVips[0].Verification = common.VipVerificationPtr(models.VipVerificationFailed)
				status, message := lcontext.function(preprocessContext)
				Expect(status).Should(Equal(ValidationFailure))
				Expect(message).Should(MatchRegexp(fmt.Sprintf("%s vips <1.2.3.[56]> is already in use in cidr 1.2.3.0/24", strings.ToLower(lcontext.name))))
			})
		})
	}
})

var _ = Describe("Network type matches high control plane count", func() {
	tests := []struct {
		controlPlaneCount int64
		networkType       *string
		invalid           bool
	}{
		{
			controlPlaneCount: 1,
			networkType:       swag.String(models.ClusterNetworkTypeOVNKubernetes),
			invalid:           false,
		},
		{
			controlPlaneCount: 1,
			networkType:       swag.String("CalicoOrWhatever"),
			invalid:           false,
		},
		{
			controlPlaneCount: 1,
			networkType:       swag.String(models.ClusterNetworkTypeOpenShiftSDN),
			invalid:           true,
		},
		{
			controlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode,
			networkType:       swag.String(models.ClusterNetworkTypeOVNKubernetes),
			invalid:           false,
		},
		{
			controlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode,
			networkType:       swag.String(models.ClusterNetworkTypeOpenShiftSDN),
			invalid:           false,
		},
		{
			controlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode,
			networkType:       swag.String("CalicoOrWhatever"),
			invalid:           false,
		},
		{
			controlPlaneCount: 1,
			networkType:       nil,
			invalid:           false,
		},
		{
			controlPlaneCount: common.MinMasterHostsNeededForInstallationInHaMode,
			networkType:       nil,
			invalid:           false,
		},
	}
	for _, test := range tests {
		t := test
		It(fmt.Sprintf("Control plane count: %d, network type: %s", t.controlPlaneCount, swag.StringValue(t.networkType)),
			func() {
				cluster := common.Cluster{
					Cluster: models.Cluster{
						ControlPlaneCount: t.controlPlaneCount,
						NetworkType:       t.networkType,
					},
				}
				Expect(isControlPlaneCountUnsupportedByNetworkType(&cluster)).To(Equal(t.invalid))
			})
	}
})

var _ = Describe("Validator tests", func() {
	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
	)

	BeforeEach(func() {
		validator = clusterValidator{log: logrus.New()}
		clusterID = strfmt.UUID(uuid.New().String())
	})

	It("Should fail validation of API VIP if the supplied address is broadcast address in machine network", func() {
		preprocessContext = &clusterPreprocessContext{hasHostsWithInventories: true}
		verification := models.VipVerificationSucceeded
		// Try with an API VIP that is a broadcast address, this should fail validation.
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips: []*models.APIVip{
				{ClusterID: clusterID, IP: "1.2.3.255", Verification: &verification},
			},
		}}
		status, message := validator.areApiVipsValid(preprocessContext)
		Expect(status).To(Equal(ValidationFailure))
		Expect(message).To(Equal("api vips <1.2.3.255> is the broadcast address of machine-network-cidr <1.2.3.0/24>"))

		// Now try with an API VIP that is not a broadcast address, this should pass validation.
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			APIVips: []*models.APIVip{
				{ClusterID: clusterID, IP: "1.2.3.1", Verification: &verification},
			},
		}}
		status, _ = validator.areApiVipsValid(preprocessContext)
		Expect(status).To(Equal(ValidationSuccess))
	})

	It("Should fail validation of Ingress VIP if the supplied address is broadcast address in machine network", func() {
		preprocessContext = &clusterPreprocessContext{hasHostsWithInventories: true}
		verification := models.VipVerificationSucceeded
		// Try with an Ingress VIP that is a broadcast address, this should fail validation.
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			IngressVips: []*models.IngressVip{
				{ClusterID: clusterID, IP: "1.2.3.255", Verification: &verification},
			},
		}}
		status, message := validator.areIngressVipsValid(preprocessContext)
		Expect(status).To(Equal(ValidationFailure))
		Expect(message).To(Equal("ingress vips <1.2.3.255> is the broadcast address of machine-network-cidr <1.2.3.0/24>"))

		// Now try with an Ingress VIP that is not a broadcast address, this should pass validation.
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			MachineNetworks: common.TestIPv4Networking.MachineNetworks,
			IngressVips: []*models.IngressVip{
				{ClusterID: clusterID, IP: "1.2.3.1", Verification: &verification},
			},
		}}
		status, _ = validator.areIngressVipsValid(preprocessContext)
		Expect(status).To(Equal(ValidationSuccess))
	})

})

var _ = Describe("Platform validations", func() {
	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
	)

	BeforeEach(func() {
		validator = clusterValidator{log: logrus.New()}
		clusterID = strfmt.UUID(uuid.New().String())
	})

	It("Should fail validation OCI cluster with no custom manifests", func() {
		preprocessContext = &clusterPreprocessContext{hasHostsWithInventories: true}
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
			Platform: &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           swag.String("oci"),
					CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
				},
			},
			UserManagedNetworking: swag.Bool(true),
		}}
		status, message := validator.platformRequirementsSatisfied(preprocessContext)
		Expect(status).To(Equal(ValidationFailure))
		Expect(message).To(Equal("The custom manifest required for Oracle Cloud Infrastructure platform integration has not been added. Add a custom manifest to continue."))
	})

	It("Should pass platform validation on OCI cluster with no custom manifests", func() {
		preprocessContext = &clusterPreprocessContext{hasHostsWithInventories: true}
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
			Platform: &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           swag.String("oci"),
					CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
				},
			},
			UserManagedNetworking: swag.Bool(true),
			FeatureUsage:          "{\"Custom manifest\":{\"id\":\"CUSTOM_MANIFEST\",\"name\":\"Custom manifest\"}}",
		}}
		status, message := validator.platformRequirementsSatisfied(preprocessContext)
		Expect(status).To(Equal(ValidationSuccess))
		Expect(message).To(Equal("Platform requirements satisfied"))
	})

	It("Should pass validation if platform is baremetal", func() {
		preprocessContext = &clusterPreprocessContext{hasHostsWithInventories: true}
		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
			Platform: &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
			},
		}}
		status, message := validator.platformRequirementsSatisfied(preprocessContext)
		Expect(status).To(Equal(ValidationSuccess))
		Expect(message).To(Equal("Platform requirements satisfied"))
	})
})

var _ = Describe("skipNetworkHostPrefixCheck", func() {

	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
	)

	BeforeEach(func() {
		validator = clusterValidator{log: logrus.New()}
		preprocessContext = &clusterPreprocessContext{}
		clusterID = strfmt.UUID(uuid.New().String())
	})

	It("Returns false when hostPrefix 0 and networkType OVN", func() {
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
		}
		networkType := models.ClusterNetworkTypeOVNKubernetes

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:              &clusterID,
			ClusterNetworks: clusterNetworks,
			NetworkType:     &networkType,
		}}

		skipped := validator.skipNetworkHostPrefixCheck(preprocessContext)
		Expect(skipped).Should(Equal(false))
	})

	It("Returns true when hostPrefix 0 and networkType not OVN or SDN", func() {
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
		}
		networkType := models.ClusterNetworkTypeOVNKubernetes
		installCfgOverrides := "{\"networking\":{\"networkType\":\"Calico\"}}"

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterID,
			ClusterNetworks:        clusterNetworks,
			NetworkType:            &networkType,
			InstallConfigOverrides: installCfgOverrides,
		}}

		skipped := validator.skipNetworkHostPrefixCheck(preprocessContext)
		Expect(skipped).Should(Equal(true))
	})
})

var _ = Describe("SufficientMastersCount", func() {
	var (
		validator   clusterValidator
		clusterID   strfmt.UUID
		mockHostAPI *host.MockAPI
		ctrl        *gomock.Controller
	)

	BeforeEach(func() {
		clusterID = strfmt.UUID(uuid.New().String())
		ctrl = gomock.NewController(GinkgoT())
		mockHostAPI = host.NewMockAPI(ctrl)
		validator = clusterValidator{log: logrus.New(), hostAPI: mockHostAPI}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("pass validation", func() {
		It("with matching counts, set ControlPlaneCount", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  testing.ValidOCPVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 3,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationSuccess))
			Expect(message).To(Equal("The cluster has the exact amount of dedicated control plane nodes."))
		})

		It("with SNO cluster, set controlPlaneCount", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  testing.ValidOCPVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 1,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationSuccess))
			Expect(message).To(Equal("The cluster has the exact amount of dedicated control plane nodes."))
		})

		It("with multi-node cluster, 5 masters", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  common.MinimumVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 5,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationSuccess))
			Expect(message).To(Equal("The cluster has the exact amount of dedicated control plane nodes."))
		})
	})

	Context("fails validation", func() {
		It("with multi node cluster, 5 masters but expected 3", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  testing.ValidOCPVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 3,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationFailure))
			Expect(message).To(Equal("The cluster must have exactly 3 dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement."))
		})

		It("with SNO cluster, 2 masters 0 workers", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  testing.ValidOCPVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 1,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleMaster,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationFailure))
			Expect(message).To(Equal("Single-node clusters must have a single control plane node and no workers."))
		})

		It("with SNO cluster, 1 masters 1 workers", func() {
			mockHostAPI.EXPECT().
				IsValidMasterCandidate(
					gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				).Return(true, nil).AnyTimes()

			preprocessContext := &clusterPreprocessContext{
				clusterId: clusterID,
				cluster: &common.Cluster{
					Cluster: models.Cluster{
						ID:                &clusterID,
						OpenshiftVersion:  testing.ValidOCPVersionForNonStandardHAOCPControlPlane,
						ControlPlaneCount: 1,
						Hosts: []*models.Host{
							{
								Role: models.HostRoleMaster,
							},
							{
								Role: models.HostRoleWorker,
							},
						},
					}},
			}

			status, message := validator.SufficientMastersCount(preprocessContext)
			Expect(status).To(Equal(ValidationFailure))
			Expect(message).To(Equal("Single-node clusters must have a single control plane node and no workers."))
		})
	})
})

var _ = Describe("isMachineCidrEqualsToCalculatedCidr", func() {
	var (
		validator clusterValidator
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		clusterID = strfmt.UUID(uuid.New().String())
		validator = clusterValidator{log: logrus.New()}
	})

	It("should pass validation with user-managed load balancer cluster", func() {
		preprocessContext := &clusterPreprocessContext{
			clusterId: clusterID,
			cluster: &common.Cluster{
				Cluster: models.Cluster{
					ID:           &clusterID,
					LoadBalancer: &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged},
				},
			},
		}

		status, msg := validator.isMachineCidrEqualsToCalculatedCidr(preprocessContext)
		Expect(status).To(Equal(ValidationSuccess))
		Expect(msg).To(Equal("Calculating machine network CIDR is not enabled: User Managed Load Balancer"))
	})
})
