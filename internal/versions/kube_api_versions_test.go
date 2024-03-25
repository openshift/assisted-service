package versions

import (
	context "context"
	"testing"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	"github.com/lib/pq"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"golang.org/x/sync/semaphore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHandler_Versions(t *testing.T) {
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
		h           *kubeAPIVersionsHandler
		ctx         = context.Background()
		pullSecret  = "mypullsecret"
		ctrl        *gomock.Controller
		mockRelease *oc.MockRelease
		client      client.Client
	)

	BeforeEach(func() {
		var err error
		Expect(err).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockRelease = oc.NewMockRelease(ctrl)
		h = &kubeAPIVersionsHandler{
			log:            common.GetTestLog(),
			releaseHandler: mockRelease,
			releaseImages:  defaultReleaseImages,
			sem:            semaphore.NewWeighted(30),
		}
		schemes := runtime.NewScheme()
		utilruntime.Must(hivev1.AddToScheme(schemes))
		client = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
		h.kubeClient = client
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

	It("get from ReleaseImages", func() {
		osImages, err := NewOSImages(defaultOsImages)
		Expect(err).ShouldNot(HaveOccurred())

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
})

var _ = Describe("GetReleaseImageByURL", func() {
	var (
		h                  *kubeAPIVersionsHandler
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
		h = &kubeAPIVersionsHandler{
			log:            common.GetTestLog(),
			releaseHandler: mockRelease,
			releaseImages:  defaultReleaseImages,
			sem:            semaphore.NewWeighted(30),
		}
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
			Expect(releaseImage.CPUArchitectures).Should(Equal(pq.StringArray{cpuArchitecture}))
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
			Expect(releaseImage.CPUArchitectures).Should(Equal(pq.StringArray{cpuArchitecture, common.ARM64CPUArchitecture}))
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
