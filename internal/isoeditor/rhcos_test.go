package isoeditor

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	defaultTestOpenShiftVersion = "4.6"
	defaultTestServiceBaseURL   = "http://198.51.100.0:6000"
)

func TestIsoEditor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IsoEditor")
}

var _ = Context("with test files", func() {
	var (
		isoDir   string
		isoFile  string
		filesDir string
		volumeID = "Assisted123"
		log      logrus.FieldLogger
	)
	BeforeSuite(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage(volumeID)
		log = getTestLog()
	})

	AfterSuite(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := CreateEditor(isoFile, defaultTestOpenShiftVersion, log)
			_, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())
		})
		It("missing iso file", func() {
			editor := CreateEditor("invalid", defaultTestOpenShiftVersion, log)
			_, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			editor := CreateEditor(isoFile, defaultTestOpenShiftVersion, log)
			rootfsURL := fmt.Sprintf("%s/api/assisted-install/v1/boot-files?file_type=rootfs.img&openshift_version=%s",
				defaultTestServiceBaseURL, defaultTestOpenShiftVersion)
			isoHandler := editor.(*rhcosEditor).isoHandler
			err := isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			err = editor.(*rhcosEditor).fixTemplateConfigs(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())

			grubCfg := fmt.Sprintf(" linux /images/pxeboot/vmlinuz coreos.live.rootfs_url=%s", rootfsURL)
			validateFileContent(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			isolinuxCfg := fmt.Sprintf(" append initrd=/images/pxeboot/initrd.img coreos.live.rootfs_url=%s", rootfsURL)
			validateFileContent(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isolinuxCfg)
		})
	})
})

func createIsoViaGenisoimage(volumeID string) (string, string, string) {
	filesDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	isoDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())
	isoFile := filepath.Join(isoDir, "test.iso")

	err = os.Mkdir(filepath.Join(filesDir, "files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/images/pxeboot"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/EFI/redhat"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/EFI/redhat/grub.cfg"), []byte(" linux /images/pxeboot/vmlinuz coreos.liveiso=rhcos-46.82.202012051820-0"), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/isolinux"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/isolinux/isolinux.cfg"), []byte(" append coreos.liveiso=rhcos-46.82.202012051820-0 initrd=/images/pxeboot/initrd.img"), 0664)
	Expect(err).ToNot(HaveOccurred())
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func validateFileContent(filename string, content string) {
	fileContent, err := ioutil.ReadFile(filename)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(fileContent)).To(Equal(content))
}
