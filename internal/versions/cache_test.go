package versions

import (
	"context"
	"fmt"

	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

var _ = Describe("getMustGatherImages", func() {
	var (
		log             = common.GetTestLog()
		ctrl            *gomock.Controller
		mockRelease     *oc.MockRelease
		cpuArchitecture = common.TestDefaultConfig.CPUArchitecture
		pullSecret      = "test_pull_secret"
		ocpVersion      = "4.8.0-fc.1"
		mirror          = "release-mirror"
		lso             = "lso"
		lsoURL          = "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8"

		cache MustGatherVersionCache
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		imagesKey, err := getKey(ocpVersion, cpuArchitecture, KeyFormatFull)
		Expect(err).ShouldNot(HaveOccurred())

		cache, err = NewMustGatherVersionCacheFromJSON(fmt.Sprintf(`{"%s": {
				"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
				"odf": "registry.redhat.io/ocs4/odf-must-gather-rhel8",
				"%s": "%s"
			}
		}`, imagesKey, lso, lsoURL))
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	getReleaseImageMock := func(_ context.Context, openshiftVersion, cpuArch, _ string) (*models.ReleaseImage, error) {
		if openshiftVersion == ocpVersion && cpuArch == cpuArchitecture {
			return &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.8"),
				URL:              swag.String("release_4.8"),
				Version:          swag.String("4.8-candidate"),
			}, nil
		}

		return nil, errors.New("No release image found")
	}

	verifyOcpVersion := func(images MustGatherVersion, size int) {
		Expect(len(images)).To(Equal(size))
		Expect(images["ocp"]).To(Equal("blah"))
	}

	It("happy flow", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).ShouldNot(HaveOccurred())

		verifyOcpVersion(images, 4)
		Expect(images[lso]).To(Equal(lsoURL))
	})

	It("unsupported_key", func() {
		images, err := getMustGatherImages(
			log,
			"unsupported",
			cpuArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})

	It("caching", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)

		images, err = getMustGatherImages(
			log,
			ocpVersion,
			cpuArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)
	})

	It("properly handles separate images for multiple architectures of the same version", func() {
		mockRelease.EXPECT().GetMustGatherImage(
			gomock.Any(), "release_4.12.999-multi", mirror, pullSecret).
			Return("must-gather-multi", nil).
			AnyTimes()
		mockRelease.EXPECT().GetMustGatherImage(
			gomock.Any(), "release_4.12.999-x86_64", mirror, pullSecret).
			Return("must-gather-x86", nil).
			AnyTimes()

		getReleaseImageMock = func(_ context.Context, openshiftVersion, cpuArch, _ string) (*models.ReleaseImage, error) {
			if openshiftVersion == "4.12-multi" && cpuArch == common.MultiCPUArchitecture {
				return &models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
					OpenshiftVersion: swag.String("4.12-multi"),
					URL:              swag.String("release_4.12.999-multi"),
					Version:          swag.String("4.12.999-rc.4"),
				}, nil
			}

			if openshiftVersion == "4.12" && cpuArch == common.X86CPUArchitecture {
				return &models.ReleaseImage{
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					OpenshiftVersion: swag.String("4.12"),
					URL:              swag.String("release_4.12.999-x86_64"),
					Version:          swag.String("4.12.999-rc.4"),
				}, nil
			}

			return nil, errors.New("No release image found")
		}

		images, err := getMustGatherImages(
			log,
			"4.12-multi",
			common.MultiCPUArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-multi"))

		images, err = getMustGatherImages(
			log,
			"4.12",
			common.X86CPUArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-x86"))
	})

	It("missing release image", func() {
		images, err := getMustGatherImages(
			log,
			"4.7",
			cpuArchitecture,
			pullSecret,
			mirror,
			cache,
			getReleaseImageMock,
			mockRelease,
		)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})
})

var _ = Describe("getKey", func() {
	It("KeyFormatFull returns full version and architecture", func() {
		key, err := getKey("4.14.5", common.X86CPUArchitecture, KeyFormatFull)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(key).To(Equal("4.14.5-x86_64"))
	})

	It("KeyFormatMajorMinorCPUArchitecture returns major.minor-arch", func() {
		key, err := getKey("4.14.5", common.X86CPUArchitecture, KeyFormatMajorMinorCPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(key).To(Equal("4.14-x86_64"))
	})

	It("KeyFormatMajorMinor returns major.minor only", func() {
		key, err := getKey("4.14.5", common.X86CPUArchitecture, KeyFormatMajorMinor)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(key).To(Equal("4.14"))
	})

	It("fails on invalid openshift version", func() {
		_, err := getKey("invalid", common.X86CPUArchitecture, KeyFormatMajorMinorCPUArchitecture)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("GetMustGatherVersion", func() {
	It("returns exact match when full key exists", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14.5-x86_64":{"ocp":"ocp-image"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-image"))
	})

	It("falls back to major.minor-arch when full key missing", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14-x86_64":{"ocp":"ocp-image"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-image"))
	})

	It("falls back to major.minor when major.minor-arch missing", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14":{"ocp":"ocp-image"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-image"))
	})

	It("returns errNotFound when no key matches", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.13-x86_64":{"ocp":"other"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		_, err = cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, errNotFound)).To(BeTrue())
	})
})

var _ = Describe("AddMustGatherVersion", func() {
	It("adds new entry under full key", func() {
		cache := NewMustGatherVersionCache()
		err := cache.AddMustGatherVersion("4.14.5", common.X86CPUArchitecture, MustGatherVersion{"ocp": "new-ocp"})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(cache.Size()).To(Equal(1))

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "new-ocp"))
	})

	It("merges into existing entry for same key", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14.5-x86_64":{"cnv":"cnv-url"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		err = cache.AddMustGatherVersion("4.14.5", common.X86CPUArchitecture, MustGatherVersion{"ocp": "ocp-url"})
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("cnv", "cnv-url"))
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-url"))
	})
})

var _ = Describe("NewMustGatherVersionCacheFromJSON", func() {
	It("returns error on invalid JSON", func() {
		_, err := NewMustGatherVersionCacheFromJSON(`{invalid`)
		Expect(err).Should(HaveOccurred())
	})

	It("parses valid JSON and exposes via GetMustGatherVersion", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14-x86_64":{"ocp":"parsed"}}`)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(cache.Size()).To(Equal(1))

		ret, err := cache.GetMustGatherVersion("4.14.0", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "parsed"))
	})
})

var _ = Describe("NewMustGatherVersionCacheFromMustGatherImages", func() {
	It("builds major.minor key when CpuArchitecture is empty", func() {
		cache, err := NewMustGatherVersionCacheFromMustGatherImages([]aiv1beta1.MustGatherImage{
			{
				OpenshiftVersion: "4.14",
				Name:             "ocp",
				Url:              "ocp-url",
			},
		})
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-url"))
	})

	It("builds full key when CpuArchitecture is set", func() {
		cache, err := NewMustGatherVersionCacheFromMustGatherImages([]aiv1beta1.MustGatherImage{
			{
				OpenshiftVersion: "4.14.5",
				CPUArchitecture:  common.X86CPUArchitecture,
				Name:             "ocp",
				Url:              "ocp-url",
			},
		})
		Expect(err).ShouldNot(HaveOccurred())

		ret, err := cache.GetMustGatherVersion("4.14.5", common.X86CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(ret).To(HaveKeyWithValue("ocp", "ocp-url"))
	})
})

var _ = Describe("MustGatherVersionCache Size and ToJSON", func() {
	It("Size returns number of keys", func() {
		cache := NewMustGatherVersionCache()
		Expect(cache.Size()).To(Equal(0))

		_ = cache.AddMustGatherVersion("4.14.5", common.X86CPUArchitecture, MustGatherVersion{"ocp": "x"})
		Expect(cache.Size()).To(Equal(1))

		_ = cache.AddMustGatherVersion("4.15.0", common.ARM64CPUArchitecture, MustGatherVersion{"ocp": "y"})
		Expect(cache.Size()).To(Equal(2))
	})

	It("ToJSON marshals versions map", func() {
		cache, err := NewMustGatherVersionCacheFromJSON(`{"4.14-x86_64":{"ocp":"img"}}`)
		Expect(err).ShouldNot(HaveOccurred())

		jsonStr, err := cache.ToJSON()
		Expect(err).ShouldNot(HaveOccurred())
		Expect(jsonStr).To(ContainSubstring("4.14-x86_64"))
		Expect(jsonStr).To(ContainSubstring("img"))
	})
})
