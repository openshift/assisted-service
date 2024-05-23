package ignition

import (
	"encoding/json"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_32 "github.com/coreos/ignition/v2/config/v3_2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetServiceIPHostnames", func() {
	content := GetServiceIPHostnames("")
	Expect(content).To(Equal(""))

	content = GetServiceIPHostnames("10.10.10.10")
	Expect(content).To(Equal("10.10.10.10 assisted-api.local.openshift.io\n"))

	content = GetServiceIPHostnames("10.10.10.1,10.10.10.2")
	Expect(content).To(Equal("10.10.10.1 assisted-api.local.openshift.io\n10.10.10.2 assisted-api.local.openshift.io\n"))
})

var _ = Context("with test ignitions", func() {
	const v30ignition = `{"ignition": {"version": "3.0.0"},"storage": {"files": []}}`
	const v31ignition = `{"ignition": {"version": "3.1.0"},"storage": {"files": [{"path": "/tmp/chocobomb", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v32ignition = `{"ignition": {"version": "3.2.0"},"storage": {"files": [{"path": "/tmp/chocobomb", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v33ignition = `{"ignition": {"version": "3.3.0"},"storage": {"files": []}}`
	const v99ignition = `{"ignition": {"version": "9.9.0"},"storage": {"files": []}}`

	const v31override = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v32override = `{"ignition": {"version": "3.2.0"}, "storage": {"disks":[{"device":"/dev/sdb","partitions":[{"label":"root","number":4,"resize":true,"sizeMiB":204800}],"wipeTable":false}],"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`

	Describe("ParseToLatest", func() {
		It("parses a v32 config as 3.2.0", func() {
			config, err := ParseToLatest([]byte(v32ignition))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.2.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v32Config, _, err := config_32.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))
		})

		It("parses a v31 config as 3.1.0", func() {
			config, err := ParseToLatest([]byte(v31ignition))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})

		It("does not parse v99 config", func() {
			_, err := ParseToLatest([]byte(v99ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})

		It("does not parse v30 config", func() {
			_, err := ParseToLatest([]byte(v30ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})

		It("does not parse v33 config", func() {
			_, err := ParseToLatest([]byte(v33ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})
	})

	Describe("MergeIgnitionConfig", func() {
		It("parses a v31 config with v31 override as 3.1.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v31ignition), []byte(v31override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})

		It("parses a v31 config with v32 override as 3.2.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v31ignition), []byte(v32override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.2.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v32Config, _, err := config_32.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))
		})

		// Be aware, this combination is counterintuitive and comes from the fact that MergeStructTranscribe()
		// is not order-agnostic and prefers the field coming from the override rather from the base.
		It("parses a v32 config with v31 override as 3.1.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v32ignition), []byte(v31override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})
	})
})
