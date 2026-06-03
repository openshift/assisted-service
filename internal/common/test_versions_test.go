package common

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

func withVersions(versions map[string][]string, fn func()) {
	original := testVersionsByArch
	testVersionsByArch = versions
	defer func() { testVersionsByArch = original }()
	fn()
}

var _ = Describe("TestVersionBuilder", func() {
	Describe("Version selection with defaults", func() {
		It("returns the latest x86_64 version by default", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14", "4.15", "4.16"},
			}, func() {
				Expect(TestVersion().Version()).To(Equal("4.16"))
			})
		})

		It("returns the oldest version when Oldest is called", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14", "4.15", "4.16"},
			}, func() {
				Expect(TestVersion().Oldest().Version()).To(Equal("4.14"))
			})
		})

		It("returns the latest version when Latest is called after Oldest", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14", "4.15", "4.16"},
			}, func() {
				Expect(TestVersion().Oldest().Latest().Version()).To(Equal("4.16"))
			})
		})
	})

	Describe("Architecture selection", func() {
		It("selects versions for the requested architecture", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture:   {"4.14", "4.15"},
				ARM64CPUArchitecture: {"4.16", "4.17"},
			}, func() {
				Expect(TestVersion().ForArch(ARM64CPUArchitecture).Version()).To(Equal("4.17"))
			})
		})

		It("panics when the architecture has no versions", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14"},
			}, func() {
				Expect(func() {
					TestVersion().ForArch("s390x").Version()
				}).To(Panic())
			})
		})
	})

	Describe("TryVersion", func() {
		It("returns false when no versions exist for the architecture", func() {
			withVersions(map[string][]string{}, func() {
				v, ok := TestVersion().TryVersion()
				Expect(ok).To(BeFalse())
				Expect(v).To(BeEmpty())
			})
		})

		It("returns false when constraint filters out all versions", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14"},
			}, func() {
				v, ok := TestVersion().GreaterThan("5.0").TryVersion()
				Expect(ok).To(BeFalse())
				Expect(v).To(BeEmpty())
			})
		})

		It("returns the matching version on success", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.14", "4.15"},
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
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					Expect(TestVersion().LessThan("4.16").Version()).To(Equal("4.15"))
					Expect(TestVersion().LessThan("4.16").Oldest().Version()).To(Equal("4.14"))
				})
			})

			It("returns nothing when all versions are at or above the threshold", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.16", "4.17"},
				}, func() {
					_, ok := TestVersion().LessThan("4.16").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("GreaterThan", func() {
			It("filters to versions above the threshold", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					Expect(TestVersion().GreaterThan("4.15").Version()).To(Equal("4.17"))
					Expect(TestVersion().GreaterThan("4.15").Oldest().Version()).To(Equal("4.16"))
				})
			})

			It("returns nothing when all versions are at or below the threshold", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15"},
				}, func() {
					_, ok := TestVersion().GreaterThan("4.15").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("Exact", func() {
			It("matches the exact version", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16"},
				}, func() {
					Expect(TestVersion().Exact("4.15").Version()).To(Equal("4.15"))
				})
			})

			It("returns nothing when the version is not present", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.16"},
				}, func() {
					_, ok := TestVersion().Exact("4.15").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("constraint with single version", func() {
			It("returns the only version when it matches", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.15"},
				}, func() {
					Expect(TestVersion().LessThan("5.0").Version()).To(Equal("4.15"))
				})
			})

			It("returns nothing when the single version does not match", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.15"},
				}, func() {
					_, ok := TestVersion().GreaterThan("5.0").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("constraints compose with AND semantics", func() {
			It("applies all constraints together", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					v := TestVersion().LessThan("4.17").GreaterThan("4.14").Version()
					Expect(v).To(Equal("4.16"))
				})
			})

			It("returns nothing when constraints are mutually exclusive", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					_, ok := TestVersion().LessThan("4.15").GreaterThan("4.16").TryVersion()
					Expect(ok).To(BeFalse())
				})
			})
		})

		Describe("LessThanOrEqual", func() {
			It("includes the threshold version", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					Expect(TestVersion().LessThanOrEqual("4.16").Version()).To(Equal("4.16"))
					Expect(TestVersion().LessThanOrEqual("4.16").Oldest().Version()).To(Equal("4.14"))
				})
			})

			It("excludes versions above the threshold", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.16", "4.17"},
				}, func() {
					Expect(TestVersion().LessThanOrEqual("4.16").Version()).To(Equal("4.16"))
				})
			})
		})

		Describe("GreaterThanOrEqual", func() {
			It("includes the threshold version", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17"},
				}, func() {
					Expect(TestVersion().GreaterThanOrEqual("4.16").Oldest().Version()).To(Equal("4.16"))
					Expect(TestVersion().GreaterThanOrEqual("4.16").Version()).To(Equal("4.17"))
				})
			})

			It("excludes versions below the threshold", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15"},
				}, func() {
					Expect(TestVersion().GreaterThanOrEqual("4.15").Version()).To(Equal("4.15"))
				})
			})
		})

		Describe("range query with composed constraints", func() {
			It("GreaterThanOrEqual + LessThan gives half-open range [low, high)", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16", "4.17", "4.18"},
				}, func() {
					b := TestVersion().GreaterThanOrEqual("4.15").LessThan("4.18")
					Expect(b.Oldest().Version()).To(Equal("4.15"))
					Expect(b.Latest().Version()).To(Equal("4.17"))
				})
			})
		})

		Describe("no constraint returns all versions", func() {
			It("returns latest without any constraint", func() {
				withVersions(map[string][]string{
					X86CPUArchitecture: {"4.14", "4.15", "4.16"},
				}, func() {
					Expect(TestVersion().Version()).To(Equal("4.16"))
					Expect(TestVersion().Oldest().Version()).To(Equal("4.14"))
				})
			})
		})
	})

	Describe("Constraint with cross-major versions", func() {
		It("compares across major version boundaries", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16", "4.17", "5.0", "5.1"},
			}, func() {
				Expect(TestVersion().GreaterThan("4.17").Oldest().Version()).To(Equal("5.0"))
				Expect(TestVersion().LessThan("5.0").Version()).To(Equal("4.17"))
			})
		})
	})

	Describe("ReleaseVersion", func() {
		It("appends .0 to the selected version", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16"},
			}, func() {
				Expect(TestVersion().ReleaseVersion()).To(Equal("4.16.0"))
			})
		})
	})

	Describe("ReleaseImageURL", func() {
		It("builds the correct URL for x86_64", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16"},
			}, func() {
				url := TestVersion().ReleaseImageURL()
				Expect(url).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"))
			})
		})

		It("uses aarch64 suffix for arm64 architecture", func() {
			withVersions(map[string][]string{
				ARM64CPUArchitecture: {"4.16"},
			}, func() {
				url := TestVersion().ForArch(ARM64CPUArchitecture).ReleaseImageURL()
				Expect(url).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.0-aarch64"))
			})
		})

		It("uses the arch name directly for multi", func() {
			withVersions(map[string][]string{
				MultiCPUArchitecture: {"4.16"},
			}, func() {
				url := TestVersion().ForArch(MultiCPUArchitecture).ReleaseImageURL()
				Expect(url).To(Equal("quay.io/openshift-release-dev/ocp-release:4.16.0-multi"))
			})
		})
	})

	Describe("parseVersion panics on invalid input", func() {
		It("panics for an unparseable version string", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16"},
			}, func() {
				Expect(func() {
					TestVersion().LessThan("not.a" + ".version.$$")
				}).To(Panic())
			})
		})

		It("panics when Version is called and constraint uses invalid version", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16"},
			}, func() {
				Expect(func() {
					TestVersion().Exact("%%%").Version()
				}).To(Panic())
			})
		})
	})

	Describe("Edge cases", func() {
		It("handles empty version list for a known architecture", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {},
			}, func() {
				_, ok := TestVersion().TryVersion()
				Expect(ok).To(BeFalse())
			})
		})

		It("constraint on empty list returns nothing", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {},
			}, func() {
				_, ok := TestVersion().LessThan("5.0").TryVersion()
				Expect(ok).To(BeFalse())
			})
		})

		It("Oldest and Latest on a single-element list return the same version", func() {
			withVersions(map[string][]string{
				X86CPUArchitecture: {"4.16"},
			}, func() {
				Expect(TestVersion().Oldest().Version()).To(Equal("4.16"))
				Expect(TestVersion().Latest().Version()).To(Equal("4.16"))
			})
		})
	})

	DescribeTable("constraint + arch combinations",
		func(arch string, constraint func(*TestVersionBuilder) *TestVersionBuilder, expected string) {
			withVersions(map[string][]string{
				X86CPUArchitecture:   {"4.14", "4.15", "4.16"},
				ARM64CPUArchitecture: {"4.15", "4.16", "4.17"},
				MultiCPUArchitecture: {"4.16", "4.17", "4.18"},
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
		Entry("multi exact 4.17",
			MultiCPUArchitecture,
			func(b *TestVersionBuilder) *TestVersionBuilder { return b.Exact("4.17") },
			"4.17",
		),
		Entry("arm64 oldest greater than 4.15",
			ARM64CPUArchitecture,
			func(b *TestVersionBuilder) *TestVersionBuilder { return b.GreaterThan("4.15").Oldest() },
			"4.16",
		),
	)
})
