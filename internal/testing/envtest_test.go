package testing

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Envtest support", func() {
	It("Creates a working environment", func() {
		env := SetupEnvtest(nil)
		config, err := env.Start()
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := env.Stop()
			Expect(err).ToNot(HaveOccurred())
		}()
		Expect(config).ToNot(BeNil())
	})
})
