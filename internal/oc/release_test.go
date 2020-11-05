package oc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logrus "github.com/sirupsen/logrus"
)

var (
	log                = logrus.New()
	releaseImage       = "release_image"
	releaseImageMirror = "release_image_mirror"
	cacheDir           = "/tmp"
	pullSecret         = "pull secret"
)

var _ = Describe("oc", func() {
	var (
		oc Release
	)

	BeforeEach(func() {
		oc = NewRelease()
		execCommand = fakeExecCommand
	})

	AfterEach(func() {
		execCommand = exec.Command
	})

	It("mco image from release image", func() {
		mco, err := oc.GetMCOImage(log, releaseImage, "", pullSecret)
		Expect(mco).ShouldNot(BeEmpty())
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("mco image from release image mirror", func() {
		mco, err := oc.GetMCOImage(log, releaseImage, releaseImageMirror, pullSecret)
		Expect(mco).ShouldNot(BeEmpty())
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("mco image with no release image or mirror", func() {
		mco, err := oc.GetMCOImage(log, "", "", pullSecret)
		Expect(mco).Should(BeEmpty())
		Expect(err).Should(HaveOccurred())
	})

	It("extract baremetal-install from release image", func() {
		path, err := oc.Extract(log, releaseImage, "", cacheDir, pullSecret)
		filePath := filepath.Join(cacheDir+"/"+releaseImage, "openshift-baremetal-install")
		Expect(path).To(Equal(filePath))
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("extract baremetal-install from release image mirror", func() {
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

func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...) //nolint:gosec
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "oc tests")
}
