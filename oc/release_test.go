package oc

import (
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
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
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
	installBinary          = "openshift-install"
)

var _ = Describe("oc", func() {
	var (
		oc           Release
		tempFilePath string
		ctrl         *gomock.Controller
		mockExecuter *executer.MockExecuter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		config := Config{MaxTries: DefaultTries, RetryDelay: time.Millisecond}
		oc = NewRelease(mockExecuter, config)
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
		It("fetch cpu architecture from release image", func() {
			command := fmt.Sprintf(templateImageInfo+" --registry-config=%s", releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			imageInfoStr := fmt.Sprintf("{ \"config\": { \"architecture\": \"%s\" }}", common.TestDefaultConfig.CPUArchitecture)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(imageInfoStr, "", 0).Times(1)

			arch, err := oc.GetReleaseArchitecture(log, releaseImage, pullSecret)
			Expect(arch).Should(Equal(common.TestDefaultConfig.CPUArchitecture))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("fetch cpu architecture - no release image", func() {
			arch, err := oc.GetReleaseArchitecture(log, "", pullSecret)
			Expect(arch).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("Extract", func() {
		It("extract baremetal-install from release image", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, models.PlatformTypeBaremetal)
			filePath := filepath.Join(cacheDir+"/"+releaseImage, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install from release image mirror", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImageMirror), true, releaseImageMirror, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, releaseImageMirror, cacheDir, pullSecret, models.PlatformTypeBaremetal)
			filePath := filepath.Join(cacheDir+"/"+releaseImageMirror, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install with no release image or mirror", func() {
			path, err := oc.Extract(log, "", "", cacheDir, pullSecret, models.PlatformTypeBaremetal)
			Expect(path).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
		It("extract baremetal-install from release image with retry", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "Failed to extract the installer", 1).Times(1)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, models.PlatformTypeBaremetal)
			filePath := filepath.Join(cacheDir+"/"+releaseImage, baremetalInstallBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install from release image retry exhausted", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				baremetalInstallBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "Failed to extract the installer", 1).Times(5)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, models.PlatformTypeBaremetal)
			Expect(path).To(Equal(""))
			Expect(err).Should(HaveOccurred())
		})

		It("extract openshift-install from release image", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				installBinary, filepath.Join(cacheDir, releaseImage), false, releaseImage, tempFilePath)
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret, models.PlatformTypeNone)
			filePath := filepath.Join(cacheDir+"/"+releaseImage, installBinary)
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
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
						ret, err := oc.getImageFromRelease(log, r.imageName, r.releaseName, "pull", false)
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
