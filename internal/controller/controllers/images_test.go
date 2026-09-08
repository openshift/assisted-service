package controllers

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("getEnvVar", func() {
	const key = "TEST_GET_ENV_VAR"

	AfterEach(func() {
		Expect(os.Unsetenv(key)).To(Succeed())
	})

	It("returns the default when the variable is unset", func() {
		Expect(getEnvVar(key, "default")).To(Equal("default"))
	})

	It("returns the default when the variable is empty", func() {
		Expect(os.Setenv(key, "")).To(Succeed())
		Expect(getEnvVar(key, "default")).To(Equal("default"))
	})

	It("returns the configured value when the variable is set", func() {
		Expect(os.Setenv(key, "custom")).To(Succeed())
		Expect(getEnvVar(key, "default")).To(Equal("custom"))
	})
})

var _ = Describe("DatabaseImage", func() {
	const key = "DATABASE_IMAGE"

	AfterEach(func() {
		Expect(os.Unsetenv(key)).To(Succeed())
	})

	It("returns the default postgres image when DATABASE_IMAGE is empty", func() {
		Expect(os.Setenv(key, "")).To(Succeed())
		Expect(DatabaseImage()).To(Equal("quay.io/sclorg/postgresql-12-c8s:latest"))
	})

	It("returns DATABASE_IMAGE when it is set", func() {
		Expect(os.Setenv(key, "registry.example/olm/sclorg-postgresql-13-c9s:latest")).To(Succeed())
		Expect(DatabaseImage()).To(Equal("registry.example/olm/sclorg-postgresql-13-c9s:latest"))
	})
})
