package oc

import (
	_ "embed"
	"fmt"
	os "os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/system"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	logrus "github.com/sirupsen/logrus"
)

var (
	log                    = logrus.New()
	releaseImage           = "release_image"
	releaseImageMirror     = "release_image_mirror"
	cacheDir               = "/tmp"
	pullSecret             = "pull secret"
	fullVersion            = "4.6.0-0.nightly-2020-08-31-220837"
	mcoImage               = "mco_image"
	mustGatherImage        = "must_gather_image"
	baremetalInstallBinary = "openshift-baremetal-install"
)

//go:embed test_skopeo_multiarch_image_output
var test_skopeo_multiarch_image_output string

var _ = Describe("oc", func() {
	var (
		oc             Release
		tempFilePath   string
		ctrl           *gomock.Controller
		mockExecuter   *executer.MockExecuter
		mockSystemInfo *system.MockSystemInfo
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		mockSystemInfo = system.NewMockSystemInfo(ctrl)
		config := Config{MaxTries: DefaultTries, RetryDelay: time.Millisecond}
		mirrorRegistriesBuilder := mirrorregistries.New()
		oc = NewRelease(mockExecuter, config, mirrorRegistriesBuilder, mockSystemInfo)
		tempFilePath = "/tmp/pull-secret"
		mockExecuter.EXPECT().TempFile(gomock.Any(), gomock.Any()).DoAndReturn(
			func(dir, pattern string) (*os.File, error) {
				tempPullSecretFile, err := os.Create(tempFilePath)
				Expect(err).ShouldNot(HaveOccurred())
				return tempPullSecretFile, nil
			},
		).AnyTimes()
	})

	AfterEach(func() {
		os.Remove(tempFilePath)
	})

	Context("GetMCOImage", func() {
		It("mco image from release image", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mcoImageName, false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mcoImage, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, "", pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("mco image from release image mirror", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mcoImageName, true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mcoImage, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("mco image exists in cache", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mcoImageName, true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mcoImage, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())

			// Fetch image again
			mco, err = oc.GetMCOImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("mco image with no release image or mirror", func() {
			mco, err := oc.GetMCOImage(log, "", "", pullSecret)
			Expect(mco).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})

		It("stdout with new lines", func() {
			stdout := fmt.Sprintf("\n%s\n", mcoImage)

			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mcoImageName, false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(stdout, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, "", pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("GetMustGatherImage", func() {
		It("must-gather image from release image", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mustGatherImageName, false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mustGatherImage, "", 0).Times(1)

			mustGather, err := oc.GetMustGatherImage(log, releaseImage, "", pullSecret)
			Expect(mustGather).Should(Equal(mustGatherImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("must-gather image from release image mirror", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mustGatherImageName, true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mustGatherImage, "", 0).Times(1)

			mustGather, err := oc.GetMustGatherImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mustGather).Should(Equal(mustGatherImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("must-gather image exists in cache", func() {
			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mustGatherImageName, true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mustGatherImage, "", 0).Times(1)

			mustGather, err := oc.GetMustGatherImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mustGather).Should(Equal(mustGatherImage))
			Expect(err).ShouldNot(HaveOccurred())

			// Fetch image again
			mustGather, err = oc.GetMustGatherImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mustGather).Should(Equal(mustGatherImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("must-gather image with no release image or mirror", func() {
			mustGather, err := oc.GetMustGatherImage(log, "", "", pullSecret)
			Expect(mustGather).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})

		It("stdout with new lines", func() {
			stdout := fmt.Sprintf("\n%s\n", mustGatherImage)

			command := fmt.Sprintf(templateGetImage+" --registry-config=%s",
				mustGatherImageName, false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(stdout, "", 0).Times(1)

			mustGather, err := oc.GetMustGatherImage(log, releaseImage, "", pullSecret)
			Expect(mustGather).Should(Equal(mustGatherImage))
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("GetOpenshiftVersion", func() {
		It("image version from release image", func() {
			command := fmt.Sprintf(templateGetVersion+" --registry-config=%s",
				false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(fullVersion, "", 0).Times(1)

			version, err := oc.GetOpenshiftVersion(log, releaseImage, "", pullSecret)
			Expect(version).Should(Equal(fullVersion))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("image version from release image mirror", func() {
			command := fmt.Sprintf(templateGetVersion+" --registry-config=%s",
				true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(fullVersion, "", 0).Times(1)

			version, err := oc.GetOpenshiftVersion(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(version).Should(Equal(fullVersion))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("image version with no release image or mirror", func() {
			version, err := oc.GetOpenshiftVersion(log, "", "", pullSecret)
			Expect(version).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("GetMajorMinorVersion", func() {
		tests := []struct {
			fullVersion  string
			shortVersion string
			isValid      bool
		}{
			{
				fullVersion:  "4.6.0",
				shortVersion: "4.6",
				isValid:      true,
			},
			{
				fullVersion:  "4.6.4",
				shortVersion: "4.6",
				isValid:      true,
			},
			{
				fullVersion:  "4.6",
				shortVersion: "4.6",
				isValid:      true,
			},
			{
				fullVersion:  "4.6.0-0.nightly-2020-08-31-220837",
				shortVersion: "4.6",
				isValid:      true,
			},
			{
				fullVersion: "-44",
				isValid:     false,
			},
		}

		for i := range tests {
			t := tests[i]
			It(t.fullVersion, func() {
				command := fmt.Sprintf(templateGetVersion+" --registry-config=%s",
					false, releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(t.fullVersion, "", 0).Times(1)

				version, err := oc.GetMajorMinorVersion(log, releaseImage, "", pullSecret)

				if t.isValid {
					Expect(err).ShouldNot(HaveOccurred())
					Expect(version).Should(Equal(t.shortVersion))
				} else {
					Expect(err).Should(HaveOccurred())
					Expect(version).Should(BeEmpty())
				}
			})
		}
	})

	Context("GetReleaseArchitecture", func() {
		Context("for single-arch release image", func() {
			It("fetch cpu architecture", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				imageInfoStr := fmt.Sprintf("{ \"config\": { \"architecture\": \"%s\" }}", common.TestDefaultConfig.CPUArchitecture)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(imageInfoStr, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(Equal([]string{common.TestDefaultConfig.CPUArchitecture}))
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("fail with malformed cpu architecture", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				imageInfoStr := fmt.Sprintf("{ \"config\": { \"not-an-architecture\": \"%s\" }}", common.TestDefaultConfig.CPUArchitecture)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(imageInfoStr, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(BeEmpty())
				Expect(err).Should(HaveOccurred())
			})
		})

		Context("for multi-arch release image", func() {
			It("fetch cpu architecture", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				command2 := fmt.Sprintf(templateSkopeoDetectMultiarch+" --authfile %s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				args2 := splitStringToInterfacesArray(command2)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "the image is a manifest list", 1).Times(1)
				mockExecuter.EXPECT().Execute(args2[0], args2[1:]...).Return(test_skopeo_multiarch_image_output, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(ConsistOf([]string{"x86_64", "ppc64le", "s390x", "arm64"}))
				Expect(err).ShouldNot(HaveOccurred())
			})

			It("fail with malformed manifests - not a list", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				command2 := fmt.Sprintf(templateSkopeoDetectMultiarch+" --authfile %s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				args2 := splitStringToInterfacesArray(command2)
				imageInfoStr := fmt.Sprintf("{ \"manifests\": { \"platform\": { \"not-an-architecture\": \"%s\" }}}", common.TestDefaultConfig.CPUArchitecture)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "the image is a manifest list", 1).Times(1)
				mockExecuter.EXPECT().Execute(args2[0], args2[1:]...).Return(imageInfoStr, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(BeEmpty())
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("failed to get image info using oc"))
			})

			It("fail with malformed manifests - no architecture", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				command2 := fmt.Sprintf(templateSkopeoDetectMultiarch+" --authfile %s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				args2 := splitStringToInterfacesArray(command2)
				imageInfoStr := fmt.Sprintf("{ \"manifests\": [{ \"platform\": { \"not-an-architecture\": \"%s\" }}]}", common.TestDefaultConfig.CPUArchitecture)
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "the image is a manifest list", 1).Times(1)
				mockExecuter.EXPECT().Execute(args2[0], args2[1:]...).Return(imageInfoStr, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(BeEmpty())
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("image manifest does not contain architecture"))
			})

			It("fail with malformed manifests - empty architecture", func() {
				command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
				command2 := fmt.Sprintf(templateSkopeoDetectMultiarch+" --authfile %s", releaseImage, tempFilePath)
				args := splitStringToInterfacesArray(command)
				args2 := splitStringToInterfacesArray(command2)
				imageInfoStr := "{ \"manifests\": [{ \"platform\": { \"architecture\": \"\" }}]}"
				mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "the image is a manifest list", 1).Times(1)
				mockExecuter.EXPECT().Execute(args2[0], args2[1:]...).Return(imageInfoStr, "", 0).Times(1)

				arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
				Expect(arch).Should(BeEmpty())
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("image manifest does not contain architecture"))
			})
		})

		It("broken release image", func() {
			command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
			command2 := fmt.Sprintf(templateSkopeoDetectMultiarch+" --authfile %s", releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			args2 := splitStringToInterfacesArray(command2)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "that's not even an image", 1).Times(1)
			mockExecuter.EXPECT().Execute(args2[0], args2[1:]...).Return("", "that's still not an image", 1).Times(1)

			arch, err := oc.GetReleaseArchitecture(log, releaseImage, "", pullSecret)
			Expect(arch).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})

		It("no release image", func() {
			arch, err := oc.GetReleaseArchitecture(log, "", "", pullSecret)
			Expect(arch).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Extract", func() {
		BeforeEach(func() {
			mockSystemInfo.EXPECT().FIPSEnabled().Return(false, nil).AnyTimes()
		})

		It("extract baremetal-install from release image", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, "4.15.0")
			filePath := filepath.Join(cacheDir+"/"+releaseImage, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install from release image mirror", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImageMirror), true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, releaseImageMirror, cacheDir, pullSecret, "4.15.0")
			filePath := filepath.Join(cacheDir+"/"+releaseImageMirror, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install with no release image or mirror", func() {
			path, err := oc.Extract(log, "", "", cacheDir, pullSecret, "4.15.0")
			Expect(path).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
		It("extract baremetal-install from release image with retry", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "Failed to extract the installer", 1).Times(1)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, "4.15.0")
			filePath := filepath.Join(cacheDir+"/"+releaseImage, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install from release image retry exhausted", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "Failed to extract the installer", 1).Times(5)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, "4.15.0")
			Expect(path).To(Equal(""))
			Expect(err).Should(HaveOccurred())
		})
	})
})

var _ = Describe("getImageFromRelease", func() {
	var (
		oc           *release
		tempFilePath string
		ctrl         *gomock.Controller
		mockExecuter *executer.MockExecuter
		log          logrus.FieldLogger
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		config := Config{MaxTries: DefaultTries, RetryDelay: time.Millisecond}
		oc = &release{executer: mockExecuter, config: config, imagesMap: common.NewExpiringCache(time.Hour, time.Hour)}
		log = logrus.New()
		tempFilePath = "/tmp/pull-secret"
		mockExecuter.EXPECT().TempFile(gomock.Any(), gomock.Any()).DoAndReturn(
			func(dir, pattern string) (*os.File, error) {
				tempPullSecretFile, err := os.Create(tempFilePath)
				Expect(err).ShouldNot(HaveOccurred())
				return tempPullSecretFile, nil
			},
		).AnyTimes()
	})
	type requester struct {
		imageName      string
		releaseName    string
		expectedResult string
		timesToRun     int
	}
	tests := []struct {
		name       string
		requesters []requester
	}{
		{
			name: "Empty",
		},
		{
			name: "Single requester",
			requesters: []requester{
				{
					imageName:      "image1",
					releaseName:    "release1",
					expectedResult: "result1",
					timesToRun:     1,
				},
			},
		},
		{
			name: "Multiple requesters",
			requesters: []requester{
				{
					imageName:      "image1",
					releaseName:    "release1",
					expectedResult: "result1",
					timesToRun:     20,
				},
			},
		},
		{
			name: "Multiple requesters - two images",
			requesters: []requester{
				{
					imageName:      "image1",
					releaseName:    "release1",
					expectedResult: "result1",
					timesToRun:     20,
				},
				{
					imageName:      "image2",
					releaseName:    "release2",
					expectedResult: "result2",
					timesToRun:     20,
				},
			},
		},
	}
	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			for _, r := range t.requesters {
				mockExecuter.EXPECT().Execute("oc", "adm", "release", "info",
					"--image-for="+r.imageName, "--insecure=false", r.releaseName,
					"--registry-config=/tmp/pull-secret").
					Return(r.expectedResult, "", 0).Times(1)
			}
			panicChan := make(chan interface{})
			doneChan := make(chan bool)
			numRequesting := 0
			for l := range t.requesters {
				r := t.requesters[l]
				for j := 0; j != r.timesToRun; j++ {
					numRequesting++
					go func() {
						defer func() {
							if panicVar := recover(); panicVar != nil {
								panicChan <- panicVar
							}
							doneChan <- true
						}()
						ret, err := oc.getImageFromRelease(log, r.imageName, r.releaseName, "pull", "", false)
						Expect(err).ToNot(HaveOccurred())
						Expect(ret).To(Equal(r.expectedResult))
					}()
				}
			}
			for numRequesting > 0 {
				select {
				case panicVar := <-panicChan:
					panic(panicVar)
				case <-doneChan:
					numRequesting--
				}
			}
		})
	}

	AfterEach(func() {
		ctrl.Finish()
	})
})

var _ = Describe("getIcspFileFromRegistriesConfig", func() {
	var (
		oc                                *release
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		ctrl                              *gomock.Controller
		mockExecuter                      *executer.MockExecuter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		config := Config{MaxTries: DefaultTries, RetryDelay: time.Millisecond}
		oc = &release{executer: mockExecuter, config: config, imagesMap: common.NewExpiringCache(time.Hour, time.Hour),
			mirrorRegistriesBuilder: mockMirrorRegistriesConfigBuilder}
		log = logrus.New()
	})

	It("valid_mirror_registries", func() {
		regData := []mirrorregistries.RegistriesConf{{Location: "registry.ci.org", Mirror: []string{"host1.example.org:5000/localimages"}}, {Location: "quay.io", Mirror: []string{"host1.example.org:5000/localimages"}}}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(1)
		mockMirrorRegistriesConfigBuilder.EXPECT().ExtractLocationMirrorDataFromRegistries().Return(regData, nil).Times(1)
		expected := "apiVersion: operator.openshift.io/v1alpha1\nkind: ImageContentSourcePolicy\nmetadata:\n  creationTimestamp: null\n  name: image-policy\nspec:\n  repositoryDigestMirrors:\n  - mirrors:\n    - host1.example.org:5000/localimages\n    source: registry.ci.org\n  - mirrors:\n    - host1.example.org:5000/localimages\n    source: quay.io\n"
		icspFile, err := oc.getIcspFileFromRegistriesConfig(log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(icspFile).ShouldNot(Equal(""))
		data, err := os.ReadFile(icspFile)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(data)).Should(Equal(expected))
	})
	It("valid_multiple_mirror_registries", func() {
		regData := []mirrorregistries.RegistriesConf{{Location: "registry.ci.org", Mirror: []string{"host1.example.org:5000/localimages", "host1.example.org:5000/openshift"}}, {Location: "quay.io", Mirror: []string{"host1.example.org:5000/localimages"}}}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(1)
		mockMirrorRegistriesConfigBuilder.EXPECT().ExtractLocationMirrorDataFromRegistries().Return(regData, nil).Times(1)
		expected := "apiVersion: operator.openshift.io/v1alpha1\nkind: ImageContentSourcePolicy\nmetadata:\n  creationTimestamp: null\n  name: image-policy\nspec:\n  repositoryDigestMirrors:\n  - mirrors:\n    - host1.example.org:5000/localimages\n    - host1.example.org:5000/openshift\n    source: registry.ci.org\n  - mirrors:\n    - host1.example.org:5000/localimages\n    source: quay.io\n"
		icspFile, err := oc.getIcspFileFromRegistriesConfig(log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(icspFile).ShouldNot(Equal(""))
		data, err := os.ReadFile(icspFile)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(data)).Should(Equal(expected))
	})

	It("no_registries", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		icspFile, err := oc.getIcspFileFromRegistriesConfig(log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(icspFile).Should(Equal(""))
	})

	It("mirror_registries_invalid", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(1)
		mockMirrorRegistriesConfigBuilder.EXPECT().ExtractLocationMirrorDataFromRegistries().Return(nil, fmt.Errorf("extract failed")).Times(1)
		icspFile, err := oc.getIcspFileFromRegistriesConfig(log)
		Expect(err).Should(HaveOccurred())
		Expect(err).Should(MatchError("extract failed"))
		Expect(icspFile).Should(Equal(""))
	})
})

var _ = Describe("GetReleaseBinaryPath", func() {
	var (
		ctrl           *gomock.Controller
		mockSystemInfo *system.MockSystemInfo
		r              Release
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockSystemInfo = system.NewMockSystemInfo(ctrl)
		r = NewRelease(nil, Config{}, nil, mockSystemInfo)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("with FIPS disabled", func() {
		BeforeEach(func() {
			mockSystemInfo.EXPECT().FIPSEnabled().Return(false, nil).AnyTimes()
		})

		It("returns the openshift-baremetal-install binary for versions earlier than 4.16", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.15.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
		})

		It("returns the openshift-install binary for 4.16.0", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.16.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
		})

		It("returns the openshift-install binary for 4.16 pre release", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.16.0-ec.6")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
			_, bin, _, err = r.GetReleaseBinaryPath("image", "dir", "4.16.0-rc.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
			_, bin, _, err = r.GetReleaseBinaryPath("image", "dir", "4.16.0-rc.3")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
		})

		It("returns the openshift-install binary for 4.16 nightlies", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.16.0-0.nightly-2024-05-30-130713")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
		})

		It("returns the openshift-install binary for versions later than 4.16.0", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.17.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
			_, bin, _, err = r.GetReleaseBinaryPath("image", "dir", "4.18.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-install"))
		})
	})

	Context("with FIPS enabled", func() {
		BeforeEach(func() {
			mockSystemInfo.EXPECT().FIPSEnabled().Return(true, nil).AnyTimes()
		})

		It("returns the openshift-baremetal-install binary for versions earlier than 4.16", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.15.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
		})

		It("returns the openshift-baremetal-install binary for 4.16.0", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.16.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
		})

		It("returns the openshift-install binary for 4.16 pre release", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.16.0-ec.6")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
			_, bin, _, err = r.GetReleaseBinaryPath("image", "dir", "4.16.0-rc.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
		})

		It("returns the openshift-install binary for versions later than 4.16.0", func() {
			_, bin, _, err := r.GetReleaseBinaryPath("image", "dir", "4.17.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
			_, bin, _, err = r.GetReleaseBinaryPath("image", "dir", "4.18.0")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(bin).To(Equal("openshift-baremetal-install"))
		})
	})
})

func splitStringToInterfacesArray(str string) []interface{} {
	argsAsString := strings.Split(str, " ")
	argsAsInterface := make([]interface{}, len(argsAsString))
	for i, v := range argsAsString {
		argsAsInterface[i] = v
	}

	return argsAsInterface
}

func TestOC(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oc tests")
}
