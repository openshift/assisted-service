package vsphere

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("AddPlatformToInstallConfig", func() {
	var (
		logger    logrus.FieldLogger
		cluster   *common.Cluster
		infraEnvs []*common.InfraEnv
		cfg       *installcfg.InstallerConfigBaremetal
		provider  provider.Provider
	)

	BeforeEach(func() {
		logger = common.GetTestLog()
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: common.MinimumVersionForUserManagedLoadBalancerFeature,
				APIVips: []*models.APIVip{
					{IP: "192.168.127.1"},
				},
				IngressVips: []*models.IngressVip{
					{IP: "192.168.127.1"},
				},
			},
		}
		infraEnvs = []*common.InfraEnv{{
			InfraEnv: models.InfraEnv{},
		}}
		cfg = &installcfg.InstallerConfigBaremetal{
			ControlPlane: struct {
				Hyperthreading string "json:\"hyperthreading,omitempty\""
				Name           string "json:\"name\""
				Replicas       int    "json:\"replicas\""
			}{
				Replicas: 1,
			},
			Compute: []struct {
				Hyperthreading string "json:\"hyperthreading,omitempty\""
				Name           string "json:\"name\""
				Replicas       int    "json:\"replicas\""
			}{{
				Replicas: 1,
			}},
		}
		provider = NewVsphereProvider(logger)
	})

	Context("addLoadBalancer", func() {
		It("Does nothing if there is no load balancer", func() {
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Vsphere.LoadBalancer).To(BeNil())
		})

		It("Does nothing if the load balancer is cluster-managed", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: models.LoadBalancerTypeClusterManaged}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Vsphere.LoadBalancer).To(BeNil())
		})

		It("Adds user-managed load balancer", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.Platform.Vsphere.LoadBalancer).ToNot(BeNil())
			Expect(cfg.Platform.Vsphere.LoadBalancer.Type).To(Equal(configv1.LoadBalancerTypeUserManaged))
		})

		It("Returns error if load balancer type is not supported", func() {
			cluster.LoadBalancer = &models.LoadBalancer{Type: "unsupported"}
			err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("load balancer type is set to unsupported value 'unsupported'"))
		})
	})
})
