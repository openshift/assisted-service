package common

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func osVersions(versions ...string) map[string]testOsImage {
	m := make(map[string]testOsImage, len(versions))
	for _, v := range versions {
		m[v] = testOsImage{}
	}
	return m
}

func withOsImages(images map[string]map[string]testOsImage, fn func()) {
	original := testOsImagesByArch
	testOsImagesByArch = images
	defer func() { testOsImagesByArch = original }()
	fn()
}

func withReleaseImages(images map[string]map[string]testReleaseImage, fn func()) {
	original := testReleaseImagesByArch
	testReleaseImagesByArch = images
	defer func() { testReleaseImagesByArch = original }()
	fn()
}

var _ = Describe("TestVersionBuilder", func() {
	Describe("Version selection with defaults", func() {
		It("returns the latest x86_64 version by default", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14", "4.15", "4.16"),
			}, func() {
				Expect(TestVersion().Version()).To(Equal("4.16"))
			})
		})

		It("returns the oldest version when Oldest is called", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14", "4.15", "4.16"),
			}, func() {
				Expect(TestVersion().Oldest().Version()).To(Equal("4.14"))
			})
		})

		It("returns the latest version when Latest is called after Oldest", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14", "4.15", "4.16"),
			}, func() {
				Expect(TestVersion().Oldest().Latest().Version()).To(Equal("4.16"))
			})
		})
	})

	Describe("Architecture selection", func() {
		It("selects versions for the requested architecture", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture:   osVersions("4.14", "4.15"),
				ARM64CPUArchitecture: osVersions("4.16", "4.17"),
			}, func() {
				Expect(TestVersion().ForArch(ARM64CPUArchitecture).Version()).To(Equal("4.17"))
			})
		})

		It("panics when the architecture has no versions", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14"),
			}, func() {
				Expect(func() {
					TestVersion().ForArch("s390x").Version()
				}).To(Panic())
			})
		})
	})

	Describe("TryVersion", func() {
		It("returns false when no versions exist for the architecture", func() {
			withOsImages(map[string]map[string]testOsImage{}, func() {
				v, ok := TestVersion().TryVersion()
				Expect(ok).To(BeFalse())
				Expect(v).To(BeEmpty())
			})
		})

		It("returns false when constraint filters out all versions", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14"),
			}, func() {
				v, ok := TestVersion().GreaterThan("5.0").TryVersion()
				Expect(ok).To(BeFalse())
				Expect(v).To(BeEmpty())
			})
		})

		It("returns the matching version on success", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.14", "4.15"),
			}, func() {
				v, ok := TestVersion().TryVersion()
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("4.15"))
			})
		})
	})

	Describe("Constraints", func() {
		BeforeEach(func() {})

		Describe("LessThan", func() {
			It("filters to versions below the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					Expect(TestVersion().LessThan("4.16").Version()).To(Equal("4.15"))
					Expect(TestVersion().LessThan("4.16").Oldest().Version()).To(Equal("4.14"))
				})
			})

			It("returns nothing when all versions are at or above the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.16", "4.17"),
				}, func() {
					_, ok := TestVersion().LessThan("4.16").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("GreaterThan", func() {
			It("filters to versions above the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					Expect(TestVersion().GreaterThan("4.15").Version()).To(Equal("4.17"))
					Expect(TestVersion().GreaterThan("4.15").Oldest().Version()).To(Equal("4.16"))
				})
			})

			It("returns nothing when all versions are at or below the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15"),
				}, func() {
					_, ok := TestVersion().GreaterThan("4.15").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("Exact", func() {
			It("matches the exact version", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16"),
				}, func() {
					Expect(TestVersion().Exact("4.15").Version()).To(Equal("4.15"))
				})
			})

			It("returns nothing when the version is not present", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.16"),
				}, func() {
					_, ok := TestVersion().Exact("4.15").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("constraint with single version", func() {
			It("returns the only version when it matches", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.15"),
				}, func() {
					Expect(TestVersion().LessThan("5.0").Version()).To(Equal("4.15"))
				})
			})

			It("returns nothing when the single version does not match", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.15"),
				}, func() {
					_, ok := TestVersion().GreaterThan("5.0").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("constraints compose with AND semantics", func() {
			It("applies all constraints together", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					v := TestVersion().LessThan("4.17").GreaterThan("4.14").Version()
					Expect(v).To(Equal("4.16"))
				})
			})

			It("returns nothing when constraints are mutually exclusive", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					_, ok := TestVersion().LessThan("4.15").GreaterThan("4.16").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("LessThanOrEqual", func() {
			It("includes the threshold version", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					Expect(TestVersion().LessThanOrEqual("4.16").Version()).To(Equal("4.16"))
					Expect(TestVersion().LessThanOrEqual("4.16").Oldest().Version()).To(Equal("4.14"))
				})
			})

			It("excludes versions above the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.16", "4.17"),
				}, func() {
					Expect(TestVersion().LessThanOrEqual("4.16").Version()).To(Equal("4.16"))
				})
			})
		})

		Describe("GreaterThanOrEqual", func() {
			It("includes the threshold version", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17"),
				}, func() {
					Expect(TestVersion().GreaterThanOrEqual("4.16").Oldest().Version()).To(Equal("4.16"))
					Expect(TestVersion().GreaterThanOrEqual("4.16").Version()).To(Equal("4.17"))
				})
			})

			It("excludes versions below the threshold", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15"),
				}, func() {
					Expect(TestVersion().GreaterThanOrEqual("4.15").Version()).To(Equal("4.15"))
				})
			})
		})

		Describe("range query with composed constraints", func() {
			It("GreaterThanOrEqual + LessThan gives half-open range [low, high)", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16", "4.17", "4.18"),
				}, func() {
					b := TestVersion().GreaterThanOrEqual("4.15").LessThan("4.18")
					Expect(b.Oldest().Version()).To(Equal("4.15"))
					Expect(b.Latest().Version()).To(Equal("4.17"))
				})
			})
		})

		Describe("no constraint returns all versions", func() {
			It("returns latest without any constraint", func() {
				withOsImages(map[string]map[string]testOsImage{
					X86CPUArchitecture: osVersions("4.14", "4.15", "4.16"),
				}, func() {
					Expect(TestVersion().Version()).To(Equal("4.16"))
					Expect(TestVersion().Oldest().Version()).To(Equal("4.14"))
				})
			})
		})
	})

	Describe("Constraint with cross-major versions", func() {
		It("compares across major version boundaries", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16", "4.17", "5.0", "5.1"),
			}, func() {
				Expect(TestVersion().GreaterThan("4.17").Oldest().Version()).To(Equal("5.0"))
				Expect(TestVersion().LessThan("5.0").Version()).To(Equal("4.17"))
			})
		})
	})

	Describe("ReleaseVersion", func() {
		It("returns the release version from generated data", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{
					X86CPUArchitecture: {"4.16": {Version: "4.16.3", URL: "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"}},
				}, func() {
					Expect(TestVersion().ReleaseVersion()).To(Equal("4.16.3"))
				})
			})
		})

		It("falls back to multi release version when arch has no entry", func() {
			withOsImages(map[string]map[string]testOsImage{
				S390xCPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{
					MultiCPUArchitecture: {"4.16": {Version: "4.16.3-multi", URL: "quay.io/openshift-release-dev/ocp-release:4.16.3-multi"}},
				}, func() {
					Expect(TestVersion().ForArch(S390xCPUArchitecture).ReleaseVersion()).To(Equal("4.16.3"))
				})
			})
		})

		It("falls back to version + .0 when no release data exists", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{}, func() {
					Expect(TestVersion().ReleaseVersion()).To(Equal("4.16.0"))
				})
			})
		})
	})

	Describe("ReleaseImageURL", func() {
		It("returns the URL from generated data for x86_64", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{
					X86CPUArchitecture: {"4.16": {Version: "4.16.3", URL: "quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"}},
				}, func() {
					Expect(TestVersion().ReleaseImageURL()).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.3-x86_64"))
				})
			})
		})

		It("returns the URL from generated data for arm64", func() {
			withOsImages(map[string]map[string]testOsImage{
				ARM64CPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{
					ARM64CPUArchitecture: {"4.16": {Version: "4.16.3", URL: "quay.io/openshift-release-dev/ocp-release:4.16.3-aarch64"}},
				}, func() {
					Expect(TestVersion().ForArch(ARM64CPUArchitecture).ReleaseImageURL()).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.3-aarch64"))
				})
			})
		})

		It("falls back to multi URL when arch has no entry", func() {
			withOsImages(map[string]map[string]testOsImage{
				S390xCPUArchitecture: osVersions("4.16"),
			}, func() {
				withReleaseImages(map[string]map[string]testReleaseImage{
					MultiCPUArchitecture: {"4.16": {Version: "4.16.3-multi", URL: "quay.io/openshift-release-dev/ocp-release:4.16.3-multi"}},
				}, func() {
					Expect(TestVersion().ForArch(S390xCPUArchitecture).ReleaseImageURL()).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.3-multi"))
				})
			})
		})
	})

	Describe("parseVersion panics on invalid input", func() {
		It("panics for an unparseable version string", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				Expect(func() {
					TestVersion().LessThan("not.a" + ".version.$$")
				}).To(Panic())
			})
		})

		It("panics when Version is called and constraint uses invalid version", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				Expect(func() {
					TestVersion().Exact("%%%").Version()
				}).To(Panic())
			})
		})
	})

	Describe("Edge cases", func() {
		It("handles empty version list for a known architecture", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions(),
			}, func() {
				_, ok := TestVersion().TryVersion()
				Expect(ok).To(BeFalse())
			})
		})

		It("constraint on empty list returns nothing", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions(),
			}, func() {
				_, ok := TestVersion().LessThan("5.0").TryVersion()
				Expect(ok).To(BeFalse())
			})
		})

		It("Oldest and Latest on a single-element list return the same version", func() {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture: osVersions("4.16"),
			}, func() {
				Expect(TestVersion().Oldest().Version()).To(Equal("4.16"))
				Expect(TestVersion().Latest().Version()).To(Equal("4.16"))
			})
		})
	})

	DescribeTable("constraint + arch combinations",
		func(arch string, constraint func(*TestVersionBuilder) *TestVersionBuilder, expected string) {
			withOsImages(map[string]map[string]testOsImage{
				X86CPUArchitecture:   osVersions("4.14", "4.15", "4.16"),
				ARM64CPUArchitecture: osVersions("4.15", "4.16", "4.17"),
			}, func() {
				v := constraint(TestVersion().ForArch(arch)).Version()
				Expect(v).To(Equal(expected))
			})
		},
		Entry("x86_64 less than 4.16",
			X86CPUArchitecture,
			func(b *TestVersionBuilder) *TestVersionBuilder { return b.LessThan("4.16") },
			"4.15",
		),
		Entry("arm64 greater than 4.15",
			ARM64CPUArchitecture,
			func(b *TestVersionBuilder) *TestVersionBuilder { return b.GreaterThan("4.15") },
			"4.17",
		),
		Entry("arm64 oldest greater than 4.15",
			ARM64CPUArchitecture,
			func(b *TestVersionBuilder) *TestVersionBuilder { return b.GreaterThan("4.15").Oldest() },
			"4.16",
		),
	)
})
