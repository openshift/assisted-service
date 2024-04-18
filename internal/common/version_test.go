package common

import (
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test VersionGreaterOrEqual", func() {
	It("GA release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("pre-release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0-fc.1", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("pre-release z-stream", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.1-fc.1", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("nightly release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0-0.nightly-2022-01-23-013716", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("pre release - rc", func() {
		isGreater, _ := VersionGreaterOrEqual("4.12.0-rc.4", "4.12.0")
		Expect(isGreater).Should(BeFalse())
	})
	It("compare pre releases", func() {
		isGreater, _ := VersionGreaterOrEqual("4.12.0-ec.1", "4.12.0-rc.4")
		Expect(isGreater).Should(BeFalse())
	})
	It("pre release", func() {
		isGreater, _ := VersionGreaterOrEqual("4.12.0-ec.1", "4.12.0-0.0")
		Expect(isGreater).Should(BeTrue())
	})
	It("pre release - ec", func() {
		isGreater, _ := VersionGreaterOrEqual("4.12.0-ec.1", "4.12.0")
		Expect(isGreater).Should(BeFalse())
	})
	It("nightly smaller base release", func() {
		isGreater, _ := VersionGreaterOrEqual("4.12.0-0.nightly-2022-01-23-013716", "4.12.0")
		Expect(isGreater).Should(BeFalse())
	})
})

var _ = Describe("Test BaseVersionGreaterOrEqual", func() {
	It("nightly equals base release", func() {
		isGreater, _ := BaseVersionGreaterOrEqual("4.12.0", "4.12.0-0.nightly-2022-01-23-013716")
		Expect(isGreater).Should(BeTrue())
	})
	It("nightly greater base release", func() {
		isGreater, _ := BaseVersionGreaterOrEqual("4.12.0", "4.12.1-0.nightly-2022-01-23-013716")
		Expect(isGreater).Should(BeTrue())
	})
	It("pre release base version", func() {
		isGreater, _ := BaseVersionGreaterOrEqual("4.12.0", "4.12.0-ec.1")
		Expect(isGreater).Should(BeTrue())
	})
	It("empty base version", func() {
		_, err := BaseVersionGreaterOrEqual("4.12.0", "")
		Expect(err).Should(Not(BeNil()))
	})
	It("empty versions", func() {
		_, err := BaseVersionGreaterOrEqual("", "")
		Expect(err).Should(Not(BeNil()))
	})
})

var _ = DescribeTable("GetMajorMinorVersion", func(
	version string,
	expectedVersion *string,
	expectedError bool,
) {
	majorMinorVersion, err := GetMajorMinorVersion(version)
	if expectedError {
		Expect(err).To(HaveOccurred())
		Expect(majorMinorVersion).To(BeNil())
		return
	}

	Expect(err).ToNot(HaveOccurred())
	Expect(*majorMinorVersion).To(Equal(*expectedVersion))
},
	Entry("x.y.z", "4.10.0", swag.String("4.10"), false),
	Entry("prerelease", "4.11.0-0.alpha", swag.String("4.11"), false),
	Entry("prerelease-nightly", "4.12.0-0.nightly-2022-01-23-013716", swag.String("4.12"), false),
	Entry("x.y", "4.2", swag.String("4.2"), false),
	Entry("x", "4", nil, true),
	Entry("empty", "", nil, true),
)

var _ = DescribeTable("GetMajorVersion", func(
	version string,
	expectedVersion *string,
	expectedError bool,
) {
	majorVersion, err := GetMajorVersion(version)
	if expectedError {
		Expect(err).To(HaveOccurred())
		Expect(majorVersion).To(BeNil())
		return
	}

	Expect(err).ToNot(HaveOccurred())
	Expect(*majorVersion).To(Equal(*expectedVersion))
},
	Entry("x.y.z", "4.10.0", swag.String("4"), false),
	Entry("prerelease", "4.11.0-0.alpha", swag.String("4"), false),
	Entry("prerelease-nightly", "4.12.0-0.nightly-2022-01-23-013716", swag.String("4"), false),
	Entry("x.y", "4.2", swag.String("4"), false),
	Entry("x", "4", swag.String("4"), false),
	Entry("empty", "", nil, true),
)

var _ = DescribeTable("IsVersionPreRelease",
	func(version string, expectedIsPreRelease bool) {
		isPreRelease, err := IsVersionPreRelease(version)
		Expect(err).ToNot(HaveOccurred())
		Expect(*isPreRelease).To(Equal(expectedIsPreRelease))
	},
	Entry("with ec as prerelease version", "4.14.0-ec.2", true),
	Entry("with fc as prerelease version", "4.14.0-fc.2", true),
	Entry("with rc as prerelease version", "4.14.0-rc.2", true),
	Entry("with alpha as prerelease version", "4.14.0-alpha", true),
	Entry("with nightly as prerelease version", "4.14.0-nightly", true),
	Entry("with stable version", "4.13.17", false),
	Entry("with another stable version", "4.14.17", false),
	Entry("with yet another stable version", "4.12.17", false),
	Entry("with stable version and suffix", "4.12.17-multi", false),
)

var _ = DescribeTable("GetVersionFormat", func(input string, expectedVersionFormat VersionFormat) {
	versionFormat := GetVersionFormat(input)
	Expect(versionFormat).To(Equal(expectedVersionFormat))
},
	Entry("pre-release", "4.11.0-0.alpha", MajorMinorPatchVersion),
	Entry("major.minor.patch", "4.12.0", MajorMinorPatchVersion),
	Entry("major.minor release", "3.14", MajorMinorVersion),
	Entry("major version only", "2", MajorVersion),
	Entry("empty version", "", NoneVersion),
	Entry("non-version string", "non-version", NoneVersion),
)
