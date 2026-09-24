package controllers

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("parsePostgresMajorVersion", func() {
	It("parses sclorg image tags", func() {
		v, ok := parsePostgresMajorVersion("quay.io/sclorg/postgresql-16-c9s:latest")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal(16))
	})

	It("parses mirrored OLM image names", func() {
		v, ok := parsePostgresMajorVersion("registry.example/olm/sclorg-postgresql-15-c9s:latest")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal(15))
	})

	It("parses registry.redhat.io rhel9 images", func() {
		v, ok := parsePostgresMajorVersion("registry.redhat.io/rhel9/postgresql-13:1-1")
		Expect(ok).To(BeTrue())
		Expect(v).To(Equal(13))
	})

	It("returns false when the image has no postgresql-N token", func() {
		_, ok := parsePostgresMajorVersion("registry.example/sha256:abc")
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("postgresHopsBeforeTarget", func() {
	AfterEach(func() {
		Expect(os.Unsetenv("DATABASE_IMAGE")).To(Succeed())
		Expect(os.Unsetenv(databaseImageVersionEnv)).To(Succeed())
		Expect(os.Unsetenv(databaseImagePG13Env)).To(Succeed())
		Expect(os.Unsetenv(databaseImagePG15Env)).To(Succeed())
	})

	It("includes PG13 and PG15 hops when the target is PG16", func() {
		hops := postgresHopsBeforeTarget()
		Expect(hops).To(HaveLen(2))
		Expect(hops[0].version).To(Equal("13"))
		Expect(hops[1].version).To(Equal("15"))
	})

	It("includes only the PG13 hop when the target is PG15", func() {
		Expect(os.Setenv("DATABASE_IMAGE", "quay.io/sclorg/postgresql-15-c9s:latest")).To(Succeed())
		hops := postgresHopsBeforeTarget()
		Expect(hops).To(HaveLen(1))
		Expect(hops[0].version).To(Equal("13"))
	})

	It("includes no hops when the target is PG13", func() {
		Expect(os.Setenv("DATABASE_IMAGE", "quay.io/sclorg/postgresql-13-c9s:latest")).To(Succeed())
		Expect(postgresHopsBeforeTarget()).To(BeEmpty())
	})

	It("uses DATABASE_IMAGE_VERSION when the image name has no version token", func() {
		Expect(os.Setenv("DATABASE_IMAGE", "registry.example/postgres@sha256:deadbeef")).To(Succeed())
		Expect(os.Setenv(databaseImageVersionEnv, "16")).To(Succeed())
		hops := postgresHopsBeforeTarget()
		Expect(hops).To(HaveLen(2))
		Expect(hops[0].version).To(Equal("13"))
		Expect(hops[1].version).To(Equal("15"))
	})

	It("prefers the postgresql-N token over DATABASE_IMAGE_VERSION", func() {
		Expect(os.Setenv("DATABASE_IMAGE", "quay.io/sclorg/postgresql-15-c9s:latest")).To(Succeed())
		Expect(os.Setenv(databaseImageVersionEnv, "16")).To(Succeed())
		hops := postgresHopsBeforeTarget()
		Expect(hops).To(HaveLen(1))
		Expect(hops[0].version).To(Equal("13"))
	})

	It("skips hops when the target version cannot be determined", func() {
		Expect(os.Setenv("DATABASE_IMAGE", "registry.example/postgres@sha256:deadbeef")).To(Succeed())
		Expect(postgresHopsBeforeTarget()).To(BeEmpty())
	})
})

var _ = Describe("postgresUpgradeInitContainers", func() {
	AfterEach(func() {
		Expect(os.Unsetenv("DATABASE_IMAGE")).To(Succeed())
		Expect(os.Unsetenv(databaseImagePG13Env)).To(Succeed())
		Expect(os.Unsetenv(databaseImagePG15Env)).To(Succeed())
	})

	It("builds init containers that share env, mounts, and the hop script", func() {
		env := []corev1.EnvVar{{Name: "POSTGRESQL_USER", Value: "admin"}}
		mounts := []corev1.VolumeMount{{Name: "postgresdb", MountPath: "/var/lib/pgsql/data"}}
		resources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			},
		}

		init := postgresUpgradeInitContainers(env, mounts, resources)
		Expect(init).To(HaveLen(2))
		Expect(init[0].Name).To(Equal("postgres-upgrade-to-13"))
		Expect(init[0].Image).To(Equal(defaultDatabaseImagePG13))
		Expect(init[1].Name).To(Equal("postgres-upgrade-to-15"))
		Expect(init[1].Image).To(Equal(defaultDatabaseImagePG15))
		for _, c := range init {
			Expect(c.Command).To(Equal([]string{"/bin/bash", "-c"}))
			Expect(c.Args).To(Equal([]string{PostgresHopScript}))
			Expect(c.Env).To(Equal(env))
			Expect(c.VolumeMounts).To(Equal(mounts))
			Expect(c.Resources).To(Equal(resources))
		}
	})

	It("uses operator env overrides for hop images", func() {
		Expect(os.Setenv(databaseImagePG13Env, "registry.example/postgresql-13:custom")).To(Succeed())
		init := postgresUpgradeInitContainers(nil, nil, corev1.ResourceRequirements{})
		Expect(init[0].Image).To(Equal("registry.example/postgresql-13:custom"))
	})
})

var _ = Describe("DatabaseImage hop getters", func() {
	AfterEach(func() {
		Expect(os.Unsetenv(databaseImagePG13Env)).To(Succeed())
		Expect(os.Unsetenv(databaseImagePG15Env)).To(Succeed())
	})

	It("returns default hop images", func() {
		Expect(DatabaseImagePG13()).To(Equal(defaultDatabaseImagePG13))
		Expect(DatabaseImagePG15()).To(Equal(defaultDatabaseImagePG15))
	})

	It("returns configured hop images", func() {
		Expect(os.Setenv(databaseImagePG13Env, "registry.example/pg13")).To(Succeed())
		Expect(os.Setenv(databaseImagePG15Env, "registry.example/pg15")).To(Succeed())
		Expect(DatabaseImagePG13()).To(Equal("registry.example/pg13"))
		Expect(DatabaseImagePG15()).To(Equal("registry.example/pg15"))
	})
})
