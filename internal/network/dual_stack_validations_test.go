package network

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("DualStack Primary IP Stack Functionality", func() {

	Describe("GetPrimaryIPStack", func() {
		It("should return nil for empty networks", func() {
			primaryStack, err := GetPrimaryIPStack(nil, nil, nil, nil, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryStack).To(BeNil())
		})
		It("should return IPv4 primary for IPv4-first dual stack", func() {
			machineNetworks := []*models.MachineNetwork{
				{Cidr: "10.0.0.0/16"},
				{Cidr: "2001:db8::/64"},
			}
			apiVips := []*models.APIVip{
				{IP: "10.0.1.1"},
				{IP: "2001:db8::1"},
			}
			ingressVips := []*models.IngressVip{
				{IP: "10.0.1.2"},
				{IP: "2001:db8::2"},
			}
			serviceNetworks := []*models.ServiceNetwork{
				{Cidr: "172.30.0.0/16"},
				{Cidr: "2001:db8:1::/64"},
			}
			clusterNetworks := []*models.ClusterNetwork{
				{Cidr: "10.128.0.0/14"},
				{Cidr: "2001:db8:2::/64"},
			}

			primaryStack, err := GetPrimaryIPStack(machineNetworks, apiVips, ingressVips, serviceNetworks, clusterNetworks)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryStack).ToNot(BeNil())
			Expect(*primaryStack).To(Equal(common.PrimaryIPStackV4))
		})

		It("should return IPv6 primary for IPv6-first dual stack", func() {
			machineNetworks := []*models.MachineNetwork{
				{Cidr: "2001:db8::/64"},
				{Cidr: "10.0.0.0/16"},
			}
			apiVips := []*models.APIVip{
				{IP: "2001:db8::1"},
				{IP: "10.0.1.1"},
			}
			ingressVips := []*models.IngressVip{
				{IP: "2001:db8::2"},
				{IP: "10.0.1.2"},
			}
			serviceNetworks := []*models.ServiceNetwork{
				{Cidr: "2001:db8:1::/64"},
				{Cidr: "172.30.0.0/16"},
			}
			clusterNetworks := []*models.ClusterNetwork{
				{Cidr: "2001:db8:2::/64"},
				{Cidr: "10.128.0.0/14"},
			}

			primaryStack, err := GetPrimaryIPStack(machineNetworks, apiVips, ingressVips, serviceNetworks, clusterNetworks)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryStack).ToNot(BeNil())
			Expect(*primaryStack).To(Equal(common.PrimaryIPStackV6))
		})

		It("should return error for inconsistent IP family order", func() {
			machineNetworks := []*models.MachineNetwork{
				{Cidr: "10.0.0.0/16"}, // IPv4 first
				{Cidr: "2001:db8::/64"},
			}
			apiVips := []*models.APIVip{
				{IP: "2001:db8::1"}, // IPv6 first - inconsistent!
				{IP: "10.0.1.1"},
			}

			primaryStack, err := GetPrimaryIPStack(machineNetworks, apiVips, nil, nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Inconsistent IP family order"))
			Expect(primaryStack).To(BeNil())
		})

		It("should handle partial network configurations", func() {
			// Only machine networks and API VIPs provided
			machineNetworks := []*models.MachineNetwork{
				{Cidr: "2001:db8::/64"},
				{Cidr: "10.0.0.0/16"},
			}
			apiVips := []*models.APIVip{
				{IP: "2001:db8::1"},
				{IP: "10.0.1.1"},
			}

			primaryStack, err := GetPrimaryIPStack(machineNetworks, apiVips, nil, nil, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(primaryStack).ToNot(BeNil())
			Expect(*primaryStack).To(Equal(common.PrimaryIPStackV6))
		})

	})

	Describe("OrderNetworksByPrimaryStack", func() {
		Context("Machine Networks", func() {
			It("should not change IPv4-first networks when primary is IPv4", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV4).([]*models.MachineNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(result[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("should swap IPv6-first networks when primary is IPv4", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "10.0.0.0/16"},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV4).([]*models.MachineNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("10.0.0.0/16"))
				Expect(string(result[1].Cidr)).To(Equal("2001:db8::/64"))
			})

			It("should not change IPv6-first networks when primary is IPv6", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "10.0.0.0/16"},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV6).([]*models.MachineNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(result[1].Cidr)).To(Equal("10.0.0.0/16"))
			})

			It("should swap IPv4-first networks when primary is IPv6", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV6).([]*models.MachineNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("2001:db8::/64"))
				Expect(string(result[1].Cidr)).To(Equal("10.0.0.0/16"))
			})
		})

		Context("Service Networks", func() {
			It("should order service networks correctly", func() {
				networks := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV6).([]*models.ServiceNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("2001:db8:1::/64"))
				Expect(string(result[1].Cidr)).To(Equal("172.30.0.0/16"))
			})
		})

		Context("Cluster Networks", func() {
			It("should order cluster networks correctly", func() {
				networks := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}

				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV6).([]*models.ClusterNetwork)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].Cidr)).To(Equal("2001:db8:2::/64"))
				Expect(result[0].HostPrefix).To(Equal(int64(64)))
				Expect(string(result[1].Cidr)).To(Equal("10.128.0.0/14"))
				Expect(result[1].HostPrefix).To(Equal(int64(23)))
			})
		})

		Context("API VIPs", func() {
			It("should order API VIPs correctly", func() {
				vips := []*models.APIVip{
					{IP: "10.0.1.1"},
					{IP: "2001:db8::1"},
				}

				result := OrderNetworksByPrimaryStack(vips, common.PrimaryIPStackV6).([]*models.APIVip)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].IP)).To(Equal("2001:db8::1"))
				Expect(string(result[1].IP)).To(Equal("10.0.1.1"))
			})
		})

		Context("Ingress VIPs", func() {
			It("should order Ingress VIPs correctly", func() {
				vips := []*models.IngressVip{
					{IP: "10.0.1.2"},
					{IP: "2001:db8::2"},
				}

				result := OrderNetworksByPrimaryStack(vips, common.PrimaryIPStackV6).([]*models.IngressVip)
				Expect(result).To(HaveLen(2))
				Expect(string(result[0].IP)).To(Equal("2001:db8::2"))
				Expect(string(result[1].IP)).To(Equal("10.0.1.2"))
			})
		})

		Context("Edge cases", func() {
			It("should return unchanged for non-slice input", func() {
				result := OrderNetworksByPrimaryStack("not a slice", common.PrimaryIPStackV4)
				Expect(result).To(Equal("not a slice"))
			})

			It("should return unchanged for single element slice", func() {
				networks := []*models.MachineNetwork{{Cidr: "10.0.0.0/16"}}
				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV4)
				Expect(result).To(Equal(networks))
			})

			It("should return unchanged for more than 2 elements", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
					{Cidr: "192.168.1.0/24"},
				}
				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV4)
				Expect(result).To(Equal(networks))
			})

			It("should return unchanged for same IP family networks", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "192.168.1.0/24"},
				}
				result := OrderNetworksByPrimaryStack(networks, common.PrimaryIPStackV4)
				Expect(result).To(Equal(networks))
			})
		})
	})

	Describe("ValidateDualStackOrder", func() {
		Context("OCP 4.12+ (IPv6-primary support)", func() {
			It("should accept IPv4-first order", func() {
				items := []string{"10.0.0.0/16", "2001:db8::/64"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.12.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should accept IPv6-first order", func() {
				items := []string{"2001:db8::/64", "10.0.0.0/16"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.12.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject two IPv4 networks", func() {
				items := []string{"10.0.0.0/16", "192.168.1.0/24"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.12.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("dual-stack machine networks must include exactly one IPv6 subnet"))
			})

			It("should reject two IPv6 networks", func() {
				items := []string{"2001:db8::/64", "2001:db8:1::/64"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.12.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("dual-stack machine networks must include exactly one IPv4 subnet"))
			})
		})

		Context("OCP < 4.12 (IPv4-first only)", func() {
			It("should accept IPv4-first order", func() {
				items := []string{"10.0.0.0/16", "2001:db8::/64"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.11.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject IPv6-first order", func() {
				items := []string{"2001:db8::/64", "10.0.0.0/16"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.11.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("First machine networks has to be IPv4 subnet (IPv6-primary dual-stack requires OpenShift 4.12+)"))
			})

			It("should reject two IPv4 networks", func() {
				items := []string{"10.0.0.0/16", "192.168.1.0/24"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.11.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Second machine networks has to be IPv6 subnet"))
			})
		})

		Context("Edge cases", func() {
			It("should reject wrong number of items", func() {
				items := []string{"10.0.0.0/16"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "4.12.0", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Expected 2 machine networks, found 1"))
			})

			It("should handle empty version string", func() {
				items := []string{"2001:db8::/64", "10.0.0.0/16"}
				err := ValidateDualStackOrder(items, "machine networks", "subnet", "", IsIPV4CIDR, IsIPv6CIDR)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("IPv6-primary dual-stack requires OpenShift 4.12+"))
			})
		})
	})

	Describe("Updated validation functions", func() {
		Context("VerifyMachineNetworksDualStack", func() {
			It("should pass for IPv4-first on OCP 4.12", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"},
					{Cidr: "2001:db8::/64"},
				}
				err := VerifyMachineNetworksDualStack(networks, true, "4.12.0")
				Expect(err).ToNot(HaveOccurred())
			})

			It("should fail for IPv6-first on OCP 4.11", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "10.0.0.0/16"},
				}
				err := VerifyMachineNetworksDualStack(networks, true, "4.11.0")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("IPv6-primary dual-stack requires OpenShift 4.12+"))
			})

			It("should pass for IPv6-first on OCP 4.12", func() {
				networks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"},
					{Cidr: "10.0.0.0/16"},
				}
				err := VerifyMachineNetworksDualStack(networks, true, "4.12.0")
				Expect(err).ToNot(HaveOccurred())
			})

			It("should skip validation for single-stack", func() {
				networks := []*models.MachineNetwork{{Cidr: "10.0.0.0/16"}}
				err := VerifyMachineNetworksDualStack(networks, false, "4.12.0")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("VerifyServiceNetworksDualStack", func() {
			It("should pass for IPv4-first on OCP 4.12", func() {
				networks := []*models.ServiceNetwork{
					{Cidr: "172.30.0.0/16"},
					{Cidr: "2001:db8:1::/64"},
				}
				err := VerifyServiceNetworksDualStack(networks, true, "4.12.0")
				Expect(err).ToNot(HaveOccurred())
			})

			It("should pass for IPv6-first on OCP 4.13", func() {
				networks := []*models.ServiceNetwork{
					{Cidr: "2001:db8:1::/64"},
					{Cidr: "172.30.0.0/16"},
				}
				err := VerifyServiceNetworksDualStack(networks, true, "4.13.0")
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("VerifyClusterNetworksDualStack", func() {
			It("should pass for IPv4-first on OCP 4.12", func() {
				networks := []*models.ClusterNetwork{
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
				}
				err := VerifyClusterNetworksDualStack(networks, true, "4.12.0")
				Expect(err).ToNot(HaveOccurred())
			})

			It("should pass for IPv6-first on OCP 4.13", func() {
				networks := []*models.ClusterNetwork{
					{Cidr: "2001:db8:2::/64", HostPrefix: 64},
					{Cidr: "10.128.0.0/14", HostPrefix: 23},
				}
				err := VerifyClusterNetworksDualStack(networks, true, "4.13.0")
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("getIPFromNetworkItem", func() {
		It("should extract CIDR from MachineNetwork", func() {
			network := &models.MachineNetwork{Cidr: "10.0.0.0/16"}
			ip := getIPFromNetworkItem(network)
			Expect(ip).To(Equal("10.0.0.0/16"))
		})

		It("should extract CIDR from ServiceNetwork", func() {
			network := &models.ServiceNetwork{Cidr: "172.30.0.0/16"}
			ip := getIPFromNetworkItem(network)
			Expect(ip).To(Equal("172.30.0.0/16"))
		})

		It("should extract CIDR from ClusterNetwork", func() {
			network := &models.ClusterNetwork{Cidr: "10.128.0.0/14"}
			ip := getIPFromNetworkItem(network)
			Expect(ip).To(Equal("10.128.0.0/14"))
		})

		It("should extract IP from APIVip", func() {
			vip := &models.APIVip{IP: "10.0.1.1"}
			ip := getIPFromNetworkItem(vip)
			Expect(ip).To(Equal("10.0.1.1"))
		})

		It("should extract IP from IngressVip", func() {
			vip := &models.IngressVip{IP: "10.0.1.2"}
			ip := getIPFromNetworkItem(vip)
			Expect(ip).To(Equal("10.0.1.2"))
		})

		It("should return empty string for nil input", func() {
			ip := getIPFromNetworkItem(nil)
			Expect(ip).To(Equal(""))
		})

		It("should return empty string for unknown type", func() {
			ip := getIPFromNetworkItem("unknown")
			Expect(ip).To(Equal(""))
		})

		It("should return empty string for nil network pointers", func() {
			var network *models.MachineNetwork = nil
			ip := getIPFromNetworkItem(network)
			Expect(ip).To(Equal(""))
		})
	})

	Describe("ValidateDualStackPartialUpdate", func() {
		Context("IPv4 primary stack", func() {
			It("should validate consistent IPv4-first networks", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "10.0.0.0/16"}, // IPv4 first
					{Cidr: "2001:db8::/64"},
				}
				apiVips := []*models.APIVip{
					{IP: "10.0.1.1"}, // IPv4 first
					{IP: "2001:db8::1"},
				}

				err := ValidateDualStackPartialUpdate(
					machineNetworks,
					apiVips,
					nil, // ingressVips
					nil, // serviceNetworks
					nil, // clusterNetworks
					common.PrimaryIPStackV4,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject inconsistent IPv6-first networks", func() {
				machineNetworks := []*models.MachineNetwork{
					{Cidr: "2001:db8::/64"}, // IPv6 first - inconsistent!
					{Cidr: "10.0.0.0/16"},
				}

				err := ValidateDualStackPartialUpdate(
					machineNetworks,
					nil, // apiVips
					nil, // ingressVips
					nil, // serviceNetworks
					nil, // clusterNetworks
					common.PrimaryIPStackV4,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Inconsistent IP family order"))
				Expect(err.Error()).To(ContainSubstring("machine_networks first IP is 2001:db8::/64 but existing primary IP stack is ipv4"))
			})

			It("should handle nil network parameters", func() {
				err := ValidateDualStackPartialUpdate(
					nil, // machineNetworks
					nil, // apiVips
					nil, // ingressVips
					nil, // serviceNetworks
					nil, // clusterNetworks
					common.PrimaryIPStackV4,
				)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("IPv6 primary stack", func() {
			It("should validate consistent IPv6-first networks", func() {
				serviceNetworks := []*models.ServiceNetwork{
					{Cidr: "2001:db8:1::/64"}, // IPv6 first
					{Cidr: "172.30.0.0/16"},
				}
				clusterNetworks := []*models.ClusterNetwork{
					{Cidr: "2001:db8:2::/64"}, // IPv6 first
					{Cidr: "10.128.0.0/14"},
				}

				err := ValidateDualStackPartialUpdate(
					nil, // machineNetworks
					nil, // apiVips
					nil, // ingressVips
					serviceNetworks,
					clusterNetworks,
					common.PrimaryIPStackV6,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject inconsistent IPv4-first networks", func() {
				apiVips := []*models.APIVip{
					{IP: "10.0.1.1"}, // IPv4 first - inconsistent!
					{IP: "2001:db8::1"},
				}

				err := ValidateDualStackPartialUpdate(
					nil, // machineNetworks
					apiVips,
					nil, // ingressVips
					nil, // serviceNetworks
					nil, // clusterNetworks
					common.PrimaryIPStackV6,
				)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Inconsistent IP family order"))
				Expect(err.Error()).To(ContainSubstring("api_vips first IP is 10.0.1.1 but existing primary IP stack is ipv6"))
			})
		})
	})

})
