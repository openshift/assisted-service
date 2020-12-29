package oc

import (
	"fmt"
	"io/ioutil"
	os "os"
	"path/filepath"
	"strings"
	"testing"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/executer"
	logrus "github.com/sirupsen/logrus"
)

var (
	log                = logrus.New()
	releaseImage       = "release_image"
	releaseImageMirror = "release_image_mirror"
	cacheDir           = "/tmp"
	pullSecret         = "pull secret"
	fullVersion        = "4.6.0-0.nightly-2020-08-31-220837"
	mcoImage           = "mco_image"
)

var _ = Describe("oc", func() {
	var (
		oc                 Release
		tempPullSecretFile *os.File
		ctrl               *gomock.Controller
		mockExecuter       *executer.MockExecuter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		oc = NewRelease(mockExecuter)

		var err error
		tempPullSecretFile, err = ioutil.TempFile("", "")
		Expect(err).ShouldNot(HaveOccurred())
		mockExecuter.EXPECT().TempFile(gomock.Any(), gomock.Any()).Return(tempPullSecretFile, nil).AnyTimes()
	})

	AfterEach(func() {
		os.Remove(tempPullSecretFile.Name())
	})

	Context("GetMCOImage", func() {
		It("mco image from release image", func() {
			command := fmt.Sprintf(templateGetMCOImage+" --registry-config=%s",
				false, releaseImage, tempPullSecretFile.Name())
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mcoImage, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, "", pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("mco image from release image mirror", func() {
			command := fmt.Sprintf(templateGetMCOImage+" --registry-config=%s",
				true, releaseImageMirror, tempPullSecretFile.Name())
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(mcoImage, "", 0).Times(1)

			mco, err := oc.GetMCOImage(log, releaseImage, releaseImageMirror, pullSecret)
			Expect(mco).Should(Equal(mcoImage))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("mco image with no release image or mirror", func() {
			mco, err := oc.GetMCOImage(log, "", "", pullSecret)
			Expect(mco).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("GetOpenshiftVersion", func() {
		It("image version from release image", func() {
			command := fmt.Sprintf(templateGetVersion+" --registry-config=%s",
				false, releaseImage, tempPullSecretFile.Name())
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return(fullVersion, "", 0).Times(1)

			version, err := oc.GetOpenshiftVersion(log, releaseImage, "", pullSecret)
			Expect(version).Should(Equal(fullVersion))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("image version from release image mirror", func() {
			command := fmt.Sprintf(templateGetVersion+" --registry-config=%s",
				true, releaseImageMirror, tempPullSecretFile.Name())
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
					false, releaseImage, tempPullSecretFile.Name())
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

	Context("Extract", func() {
		It("extract baremetal-install from release image", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				filepath.Join(cacheDir, releaseImage), false, releaseImage, tempPullSecretFile.Name())
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret)
			filePath := filepath.Join(cacheDir+"/"+releaseImage, "openshift-baremetal-install")
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install from release image mirror", func() {
			command := fmt.Sprintf(templateExtract+" --registry-config=%s",
				filepath.Join(cacheDir, releaseImageMirror), true, releaseImageMirror, tempPullSecretFile.Name())
			args := splitStringToInterfacesArray(command)
			mockExecuter.EXPECT().Execute(args[0], args[1:]...).Return("", "", 0).Times(1)

			path, err := oc.Extract(log, releaseImage, releaseImageMirror, cacheDir, pullSecret)
			filePath := filepath.Join(cacheDir+"/"+releaseImageMirror, "openshift-baremetal-install")
			Expect(path).To(Equal(filePath))
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("extract baremetal-install with no release image or mirror", func() {
			path, err := oc.Extract(log, "", "", cacheDir, pullSecret)
			Expect(path).Should(BeEmpty())
			Expect(err).Should(HaveOccurred())
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
