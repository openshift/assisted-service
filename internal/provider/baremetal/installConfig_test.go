package baremetal

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Add NTP sources", func() {
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
				OpenshiftVersion: "4.18",
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
		provider = NewBaremetalProvider(logger)
	})

	It("Does nothing if there are no NTP sources", func() {
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(BeEmpty())
	})

	It("Adds one NTP source from cluster", func() {
		cluster.AdditionalNtpSource = "1.1.1.1"
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
	})

	It("Adds multiple NTP sources from cluster", func() {
		cluster.AdditionalNtpSource = "1.1.1.1,2.2.2.2,3.3.3.3"
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
	})

	It("Removes extra white space in NTP sources from cluster", func() {
		cluster.AdditionalNtpSource = "  1.1.1.1,   \t  2.2.2.2 , 3.3.3.3  "
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
	})

	It("Adds one source from one infrastructure environment", func() {
		infraEnvs[0].AdditionalNtpSources = "1.1.1.1"
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
	})

	It("Adds multiple sources from one infrastructure environment", func() {
		infraEnvs[0].AdditionalNtpSources = "1.1.1.1,2.2.2.2,3.3.3.3"
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
	})

	It("Removes extra white space in NTP sources from infrastructure environment", func() {
		infraEnvs[0].AdditionalNtpSources = "  1.1.1.1,   \t  2.2.2.2 , 3.3.3.3  "
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
	})

	It("Ignores sources from infrastructure environment if there are NTP sources in the cluster", func() {
		cluster.AdditionalNtpSource = "1.1.1.1"
		infraEnvs[0].AdditionalNtpSources = "2.2.2.2"
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1"))
	})

	It("Combines NTP sources from multiple infrastructure environments", func() {
		infraEnvs := []*common.InfraEnv{
			{
				InfraEnv: models.InfraEnv{
					AdditionalNtpSources: "1.1.1.1",
				},
			},
			{
				InfraEnv: models.InfraEnv{
					AdditionalNtpSources: "2.2.2.2",
				},
			},
			{
				InfraEnv: models.InfraEnv{
					AdditionalNtpSources: "3.3.3.3",
				},
			},
		}
		err := provider.AddPlatformToInstallConfig(cfg, cluster, infraEnvs)
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Platform.Baremetal.AdditionalNTPServers).To(ConsistOf("1.1.1.1", "2.2.2.2", "3.3.3.3"))
	})
})
