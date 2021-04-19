package cnv_test

import (
	"os"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/operators/cnv"
)

var _ = Describe("CNV plugin configuration", func() {
	const (
		prefix = "test"
	)
	Context("for SR-IOV", func() {
		const supportedSriovNicsKey = "TEST_CNV_SUPPORTED_SRIOV_NICS"
		BeforeEach(func() {
			err := os.Unsetenv(supportedSriovNicsKey)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := os.Unsetenv(supportedSriovNicsKey)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should load SR-IOV defaults", func() {
			cfg := cnv.Config{}
			err := envconfig.Process(prefix, &cfg)

			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.SupportedSRIOVNetworkIC).ToNot(BeNil())
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveLen(5))
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveKeyWithValue("8086:158b", true))
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveKeyWithValue("15b3:1015", true))
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveKeyWithValue("15b3:1017", true))
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveKeyWithValue("15b3:1013", true))
			Expect(cfg.SupportedSRIOVNetworkIC).To(HaveKeyWithValue("15b3:101b", true))
		})
	})

	Context("for GPU", func() {
		const (
			prefix           = "test"
			supportedGpusKey = "TEST_CNV_SUPPORTED_GPUS"
			key1             = "0000:1111"
			key2             = "2222:3333"
			key3             = "4444:5555"
		)
		BeforeEach(func() {
			err := os.Unsetenv(supportedGpusKey)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := os.Unsetenv(supportedGpusKey)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should load GPU defaults", func() {
			cfg := cnv.Config{}
			err := envconfig.Process(prefix, &cfg)

			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.SupportedGPUs).ToNot(BeNil())
			Expect(cfg.SupportedGPUs).To(HaveLen(2))
			Expect(cfg.SupportedGPUs).To(HaveKeyWithValue("10de:1db6", true))
			Expect(cfg.SupportedGPUs).To(HaveKeyWithValue("10de:1eb8", true))
		})

		table.DescribeTable("should load supported GPUs", func(variable string, expectedKeys ...string) {
			_ = os.Setenv(supportedGpusKey, variable)

			cfg := cnv.Config{}
			err := envconfig.Process(prefix, &cfg)

			Expect(err).ToNot(HaveOccurred())
			Expect(cfg.SupportedGPUs).ToNot(BeNil())
			Expect(cfg.SupportedGPUs).To(HaveLen(len(expectedKeys)))
			for _, key := range expectedKeys {
				Expect(cfg.SupportedGPUs).To(HaveKeyWithValue(key, true))
			}

		},
			table.Entry("One key", key1, key1),
			table.Entry("Three keys", key1+","+key2+","+key3, key1, key2, key3),
			table.Entry("With a duplicate key", key1+","+key2+","+key2, key1, key2),

			table.Entry("Empty", ""),
		)
	})
})
