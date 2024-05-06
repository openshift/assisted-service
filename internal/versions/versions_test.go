package versions

import (
	context "context"
	"fmt"
	"testing"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandler_ListComponentVersions(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "versions")
}

var defaultOsImages = models.OsImages{
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.11.1"),
		URL:              swag.String("rhcos_4.11"),
		Version:          swag.String("version-411.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("rhcos_4.10.1"),
		Version:          swag.String("version-4101.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.10.2"),
		URL:              swag.String("rhcos_4.10.2"),
		Version:          swag.String("version-4102.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("rhcos_4.9"),
		Version:          swag.String("version-49.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("rhcos_4.9_arm64"),
		Version:          swag.String("version-49.123-0_arm64"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("rhcos_4.91"),
		Version:          swag.String("version-491.123-0"),
	},
	&models.OsImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("rhcos_4.8"),
		Version:          swag.String("version-48.123-0"),
	},
}

var defaultReleaseImages = models.ReleaseImages{
	// Two images below represent a scenario when the same OpenShift Version (as reported by the
	// CVO) is provided by more than a single release image. This is a scenario when for the same
	// OCP version we have single-arch and multi-arch image. This happens because starting from
	// OCP 4.12 CSV returns the same value no matter the architecture-ness of the release image.
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.12"),
		URL:              swag.String("release_4.12.999-x86_64"),
		Version:          swag.String("4.12.999-rc.4"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
		OpenshiftVersion: swag.String("4.12-multi"),
		URL:              swag.String("release_4.12.999-multi"),
		Version:          swag.String("4.12.999-rc.4"),
	},
	&models.ReleaseImage{
		// This image uses a syntax with missing "cpu_architectures". It is crafted
		// in order to make sure the change in MGMT-11494 is backwards-compatible.
		CPUArchitecture:  swag.String("fake-architecture-chocobomb"),
		CPUArchitectures: []string{},
		OpenshiftVersion: swag.String("4.11.2"),
		URL:              swag.String("release_4.11.2"),
		Version:          swag.String("4.11.2-fake-chocobomb"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
		OpenshiftVersion: swag.String("4.11.1"),
		URL:              swag.String("release_4.11.1"),
		Version:          swag.String("4.11.1-multi"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.10.1"),
		URL:              swag.String("release_4.10.1"),
		Version:          swag.String("4.10.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.10.2"),
		URL:              swag.String("release_4.10.2"),
		Version:          swag.String("4.10.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9"),
		Version:          swag.String("4.9-candidate"),
		Default:          true,
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
		CPUArchitectures: []string{common.ARM64CPUArchitecture},
		OpenshiftVersion: swag.String("4.9"),
		URL:              swag.String("release_4.9_arm64"),
		Version:          swag.String("4.9-candidate_arm64"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.9.1"),
		URL:              swag.String("release_4.9.1"),
		Version:          swag.String("4.9.1-candidate"),
	},
	&models.ReleaseImage{
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		OpenshiftVersion: swag.String("4.8"),
		URL:              swag.String("release_4.8"),
		Version:          swag.String("4.8-candidate"),
	},
}

var _ = Describe("GetReleaseImage", func() {
	var (
		h           *handler
		osImages    OSImages
		ctx         = context.Background()
		pullSecret  = "mypullsecret"
		ctrl        *gomock.Controller
		mockRelease *oc.MockRelease
	)

	BeforeEach(func() {
		var err error
		osImages, err = NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		h, err = NewHandler(common.GetTestLog(), mockRelease, defaultReleaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("unsupported openshiftVersion", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "unsupported", common.TestDefaultConfig.CPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("unsupported cpuArchitecture", func() {
		releaseImage, err := h.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "unsupported", pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
		Expect(err.Error()).To(ContainSubstring("isn't specified in release images list"))
	})

	It("empty openshiftVersion", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "", common.TestDefaultConfig.CPUArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("empty cpuArchitecture", func() {
		releaseImage, err := h.GetReleaseImage(ctx, common.TestDefaultConfig.OpenShiftVersion, "", pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(releaseImage).Should(BeNil())
	})

	It("fetch release image by major.minor", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.9", common.DefaultCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
		Expect(*releaseImage.Version).Should(Equal("4.9-candidate"))
	})

	It("fetch release image by major.minor if given X.Y.Z version", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.9.2", common.DefaultCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
		Expect(*releaseImage.Version).Should(Equal("4.9-candidate"))
	})

	It("get from ReleaseImages", func() {
		for _, key := range osImages.GetOpenshiftVersions() {
			for _, architecture := range osImages.GetCPUArchitectures(key) {
				releaseImage, err := h.GetReleaseImage(ctx, key, architecture, pullSecret)
				if err != nil {
					releaseImage, err = h.GetReleaseImage(ctx, key, common.MultiCPUArchitecture, pullSecret)
					Expect(err).ShouldNot(HaveOccurred())
				}

				for _, release := range defaultReleaseImages {
					if *release.OpenshiftVersion == key && *release.CPUArchitecture == architecture {
						Expect(releaseImage).Should(Equal(release))
					}
				}
			}
		}
	})

	It("gets successfuly image with old syntax", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.11.2", "fake-architecture-chocobomb", pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.2"))
		Expect(*releaseImage.Version).Should(Equal("4.11.2-fake-chocobomb"))
	})

	It("gets successfuly image with new syntax", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.10.1", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.10.1"))
		Expect(*releaseImage.Version).Should(Equal("4.10.1-candidate"))
	})

	It("gets successfuly image using generic multiarch query", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.11.1", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.1"))
		Expect(*releaseImage.Version).Should(Equal("4.11.1-multi"))
	})

	It("gets successfuly image using sub-architecture", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.11.1", common.PowerCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.11.1"))
		Expect(*releaseImage.Version).Should(Equal("4.11.1-multi"))
	})

	It("gets successfuly multi-arch image for multiple images with the same version", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.12.999-rc.4", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.12-multi"))
		Expect(*releaseImage.Version).Should(Equal("4.12.999-rc.4"))
	})

	It("gets successfuly single-arch image for multiple images with the same version", func() {
		releaseImage, err := h.GetReleaseImage(ctx, "4.12.999-rc.4", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.12"))
		Expect(*releaseImage.Version).Should(Equal("4.12.999-rc.4"))
	})

	It("returns the matching CPU architecture over multi-arch if it is present", func() {
		h.releaseImages = models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
				OpenshiftVersion: swag.String("4.12-multi"),
				URL:              swag.String("release_4.12.999-multi"),
				Version:          swag.String("4.12.999"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("release_4.12.999-x86_64"),
				Version:          swag.String("4.12.999"),
			},
		}

		releaseImage, err := h.GetReleaseImage(ctx, "4.12.999", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.12"))
		Expect(*releaseImage.Version).Should(Equal("4.12.999"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.X86CPUArchitecture))
	})

	It("returns the multi-arch image if matching CPU architecture is not present", func() {
		h.releaseImages = models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
				OpenshiftVersion: swag.String("4.12-multi"),
				URL:              swag.String("release_4.12.999-multi"),
				Version:          swag.String("4.12.999"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("release_4.13.999-x86_64"),
				Version:          swag.String("4.13.999"),
			},
		}

		releaseImage, err := h.GetReleaseImage(ctx, "4.12.999", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.12-multi"))
		Expect(*releaseImage.Version).Should(Equal("4.12.999"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
	})

	Context("with a kube client", func() {
		var (
			client client.Client
		)
		BeforeEach(func() {
			schemes := runtime.NewScheme()
			utilruntime.Must(hivev1.AddToScheme(schemes))
			client = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
			h.kubeClient = client
		})

		It("returns no image when no clusterimageset matches", func() {
			image, err := h.GetReleaseImage(ctx, "4.20.0", common.X86CPUArchitecture, pullSecret)
			Expect(err).To(HaveOccurred())
			Expect(image).To(BeNil())
		})

		It("returns an existing image from the cache", func() {
			image, err := h.GetReleaseImage(ctx, "4.11.1", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal("release_4.11.1")))
		})

		It("adds a release to the cache from a clusterimageset when no image in the cache matches", func() {
			releaseImageURL := "example.com/openshift-release-dev/ocp-release:4.11.999"
			cis := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: "new-release"},
				Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: releaseImageURL},
			}
			Expect(client.Create(ctx, cis)).To(Succeed())

			mockRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), releaseImageURL, "", pullSecret).Return("4.11.999", nil).Times(1)
			mockRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), releaseImageURL, "", pullSecret).Return([]string{common.X86CPUArchitecture}, nil).Times(1)

			image, err := h.GetReleaseImage(ctx, "4.11.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
			image, err = h.GetReleaseImage(ctx, "4.11.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
		})

		It("doesn't re-add existing releases", func() {
			for _, rel := range defaultReleaseImages {
				cis := &hivev1.ClusterImageSet{
					ObjectMeta: metav1.ObjectMeta{Name: *rel.URL},
					Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: *rel.URL},
				}
				Expect(client.Create(ctx, cis)).To(Succeed())
			}
			releaseImageURL := "example.com/openshift-release-dev/ocp-release:4.11.999"
			cis := &hivev1.ClusterImageSet{
				ObjectMeta: metav1.ObjectMeta{Name: "new-release"},
				Spec:       hivev1.ClusterImageSetSpec{ReleaseImage: releaseImageURL},
			}
			Expect(client.Create(ctx, cis)).To(Succeed())

			mockRelease.EXPECT().GetOpenshiftVersion(gomock.Any(), releaseImageURL, "", pullSecret).Return("4.11.999", nil).Times(1)
			mockRelease.EXPECT().GetReleaseArchitecture(gomock.Any(), releaseImageURL, "", pullSecret).Return([]string{common.X86CPUArchitecture}, nil).Times(1)

			image, err := h.GetReleaseImage(ctx, "4.11.999", common.X86CPUArchitecture, pullSecret)
			Expect(err).ToNot(HaveOccurred())
			Expect(image.URL).To(HaveValue(Equal(releaseImageURL)))
		})
	})
})

var _ = Describe("ValidateReleaseImageForRHCOS", func() {
	var h *handler

	BeforeEach(func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.11.1"),
				URL:              swag.String("release_4.11.1"),
				Default:          false,
				Version:          swag.String("4.11.1-chocobomb-for-test"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
				OpenshiftVersion: swag.String("4.12"),
				URL:              swag.String("release_4.12"),
				Default:          false,
				Version:          swag.String("4.12"),
			},
		}
		var err error
		h, err = NewHandler(common.GetTestLog(), nil, releaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("validates successfuly using exact match", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11.1", common.X86CPUArchitecture)).To(Succeed())
	})
	It("validates successfuly using major.minor", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11", common.X86CPUArchitecture)).To(Succeed())
	})
	It("validates successfuly using major.minor using default architecture", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.11", "")).To(Succeed())
	})
	It("validates successfuly using major.minor.patch-something", func() {
		Expect(h.ValidateReleaseImageForRHCOS("4.12.2-chocobomb", common.X86CPUArchitecture)).To(Succeed())
	})
	It("fails validation using non-existing major.minor.patch-something", func() {
		Expect(h.ValidateReleaseImageForRHCOS("9.9.9-chocobomb", common.X86CPUArchitecture)).NotTo(Succeed())
	})
	It("fails validation using multiarch", func() {
		// This test is supposed to fail because there exists no RHCOS image that supports
		// multiple architectures.
		Expect(h.ValidateReleaseImageForRHCOS("4.11", common.MultiCPUArchitecture)).NotTo(Succeed())
	})
	It("fails validation using invalid version", func() {
		Expect(h.ValidateReleaseImageForRHCOS("invalid", common.X86CPUArchitecture)).NotTo(Succeed())
	})
})

var _ = Describe("GetDefaultReleaseImage", func() {
	It("Default release image exists", func() {
		h, err := NewHandler(common.GetTestLog(), nil, defaultReleaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())

		releaseImage, err := h.GetDefaultReleaseImage(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(releaseImage.Default).Should(Equal(true))
		Expect(*releaseImage.OpenshiftVersion).Should(Equal("4.9"))
		Expect(*releaseImage.CPUArchitecture).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("Missing default release image", func() {
		h, err := NewHandler(common.GetTestLog(), nil, models.ReleaseImages{}, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())

		_, err = h.GetDefaultReleaseImage(common.TestDefaultConfig.CPUArchitecture)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).Should(Equal("Default release image is not available"))
	})
})

var _ = Describe("GetMustGatherImages", func() {
	var (
		h                *handler
		ctrl             *gomock.Controller
		mockRelease      *oc.MockRelease
		cpuArchitecture  = common.TestDefaultConfig.CPUArchitecture
		pullSecret       = "test_pull_secret"
		ocpVersion       = "4.8.0-fc.1"
		mirror           = "release-mirror"
		imagesKey        = fmt.Sprintf("4.8-%s", cpuArchitecture)
		mustgatherImages = MustGatherVersions{
			imagesKey: MustGatherVersion{
				"cnv": "registry.redhat.io/container-native-virtualization/cnv-must-gather-rhel8:v2.6.5",
				"odf": "registry.redhat.io/ocs4/odf-must-gather-rhel8",
				"lso": "registry.redhat.io/openshift4/ose-local-storage-mustgather-rhel8",
			},
		}
	)

	BeforeEach(func() {
		var err error
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		h, err = NewHandler(common.GetTestLog(), mockRelease, defaultReleaseImages, mustgatherImages, mirror, nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	verifyOcpVersion := func(images MustGatherVersion, size int) {
		Expect(len(images)).To(Equal(size))
		Expect(images["ocp"]).To(Equal("blah"))
	}

	It("happy flow", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.8", mirror, pullSecret).Return("blah", nil).Times(1)
		images, err := h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())

		verifyOcpVersion(images, 4)
		Expect(images["lso"]).To(Equal(mustgatherImages[imagesKey]["lso"]))
	})

	It("unsupported_key", func() {
		images, err := h.GetMustGatherImages("unsupported", cpuArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(images).Should(BeEmpty())
	})

	It("caching", func() {
		images, err := h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)

		images, err = h.GetMustGatherImages(ocpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		verifyOcpVersion(images, 4)
	})

	It("properly handles separate images for multiple architectures of the same version", func() {
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.12.999-multi", mirror, pullSecret).Return("must-gather-multi", nil).AnyTimes()
		mockRelease.EXPECT().GetMustGatherImage(gomock.Any(), "release_4.12.999-x86_64", mirror, pullSecret).Return("must-gather-x86", nil).AnyTimes()

		images, err := h.GetMustGatherImages("4.12", common.MultiCPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-multi"))

		images, err = h.GetMustGatherImages("4.12", common.X86CPUArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(images["ocp"]).To(Equal("must-gather-x86"))
	})

	It("missing release image", func() {
		images, err := h.GetMustGatherImages("4.7", cpuArchitecture, pullSecret)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("isn't specified in release images list"))
		Expect(images).Should(BeEmpty())
	})
})

var _ = Describe("GetReleaseImageByURL", func() {
	var (
		h                  *handler
		ctrl               *gomock.Controller
		mockRelease        *oc.MockRelease
		cpuArchitecture    = common.TestDefaultConfig.CPUArchitecture
		pullSecret         = "test_pull_secret"
		releaseImageUrl    = "releaseImage"
		customOcpVersion   = "4.8.0"
		existingOcpVersion = "4.9.1"
		ctx                = context.Background()
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)

		var err error
		h, err = NewHandler(common.GetTestLog(), mockRelease, defaultReleaseImages, nil, "", nil)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("for single-arch release image", func() {
		It("added successfully", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			releaseImage, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(cpuArchitecture))
			Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture}))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("when release image already exists", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(existingOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
				return *releaseImage.OpenshiftVersion == existingOcpVersion && *releaseImage.CPUArchitecture == cpuArchitecture
			})
			Expect(releaseImageFromCache).ShouldNot(BeNil())

			_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			releaseImage, err := h.GetReleaseImage(ctx, existingOcpVersion, cpuArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
		})

		It("succeeds for missing OS image", func() {
			ocpVersion := "4.7"
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

			_, err := h.GetReleaseImageByURL(ctx, "invalidRelease", pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("for multi-arch release image", func() {
		It("added successfully", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

			releaseImage, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(*releaseImage.CPUArchitecture).Should(Equal(common.MultiCPUArchitecture))
			Expect(releaseImage.CPUArchitectures).Should(Equal([]string{cpuArchitecture, common.ARM64CPUArchitecture}))
			Expect(*releaseImage.OpenshiftVersion).Should(Equal(customOcpVersion))
			Expect(*releaseImage.URL).Should(Equal(releaseImageUrl))
			Expect(*releaseImage.Version).Should(Equal(customOcpVersion))
		})

		It("when release image already exists", func() {
			mockRelease.EXPECT().GetOpenshiftVersion(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("4.11.1", nil).AnyTimes()
			mockRelease.EXPECT().GetReleaseArchitecture(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture, common.ARM64CPUArchitecture}, nil).AnyTimes()

			releaseImageFromCache := funk.Find(h.releaseImages, func(releaseImage *models.ReleaseImage) bool {
				return *releaseImage.OpenshiftVersion == "4.11.1" && *releaseImage.CPUArchitecture == common.MultiCPUArchitecture
			})
			Expect(releaseImageFromCache).ShouldNot(BeNil())

			_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())

			// Query for multi-arch release image using generic multiarch
			releaseImage, err := h.GetReleaseImage(ctx, "4.11.1", common.MultiCPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))

			// Query for multi-arch release image using specific arch
			releaseImage, err = h.GetReleaseImage(ctx, "4.11.1", common.X86CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))
			releaseImage, err = h.GetReleaseImage(ctx, "4.11.1", common.ARM64CPUArchitecture, pullSecret)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(releaseImage.Version).Should(Equal(releaseImageFromCache.(*models.ReleaseImage).Version))

			// Query for non-existing architecture
			_, err = h.GetReleaseImage(ctx, "4.11.1", "architecture-chocobomb", pullSecret)
			Expect(err.Error()).Should(Equal("The requested CPU architecture (architecture-chocobomb) isn't specified in release images list"))
		})
	})

	It("fails when the version extraction fails", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("invalid")).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).Should(HaveOccurred())
	})

	It("fails when the arch extraction fails", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error when getting architecture")).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).Should(HaveOccurred())
	})

	It("keep support level from cache", func() {
		mockRelease.EXPECT().GetOpenshiftVersion(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(customOcpVersion, nil).AnyTimes()
		mockRelease.EXPECT().GetReleaseArchitecture(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]string{cpuArchitecture}, nil).AnyTimes()

		_, err := h.GetReleaseImageByURL(ctx, releaseImageUrl, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		_, err = h.GetReleaseImage(ctx, customOcpVersion, cpuArchitecture, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("NewHandler", func() {
	validateNewHandler := func(releaseImages models.ReleaseImages) error {
		_, err := NewHandler(common.GetTestLog(), nil, releaseImages, nil, "", nil)
		return err
	}

	It("succeeds if no release images are specified", func() {
		releaseImages := models.ReleaseImages{}
		Expect(validateNewHandler(releaseImages)).To(Succeed())
	})

	It("succeeds with valid release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
				Version:          swag.String("4.9-candidate"),
			},
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.ARM64CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9_arm64"),
				Version:          swag.String("4.9-candidate_arm64"),
			},
		}
		Expect(validateNewHandler(releaseImages)).To(Succeed())
	})

	It("fails when missing CPUArchitecture in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
				Version:          swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cpu_architecture"))
	})

	It("fails when missing URL in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				Version:          swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("url"))
	})

	It("fails when missing Version in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.9"),
				URL:              swag.String("release_4.9"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("version"))
	})

	It("fails when missing OpenshiftVersion in Release images", func() {
		releaseImages := models.ReleaseImages{
			&models.ReleaseImage{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				URL:             swag.String("release_4.9"),
				Version:         swag.String("4.9-candidate"),
			},
		}
		err := validateNewHandler(releaseImages)
		Expect(err).Should(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("version"))
	})
})

var _ = Describe("toMajorMinor", func() {
	It("works for x.y", func() {
		res, err := toMajorMinor("4.6")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("works for x.y.z", func() {
		res, err := toMajorMinor("4.6.9")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("works for x.y.z-thing", func() {
		res, err := toMajorMinor("4.6.9-beta")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res).Should(Equal("4.6"))
	})

	It("fails when the version cannot parse", func() {
		res, err := toMajorMinor("ere.654.45")
		Expect(err).Should(HaveOccurred())
		Expect(res).Should(Equal(""))
	})
})
