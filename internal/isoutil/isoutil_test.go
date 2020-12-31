package isoutil

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIsoUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Iso Util")
}

var _ = Describe("Isoutil", func() {
	var (
		isoDir   string
		isoFile  string
		filesDir string
		volumeID = "Assisted123"
	)
	BeforeSuite(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage(volumeID)
	})

	AfterSuite(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	Context("readFile", func() {
		It("read existing file from ISO", func() {
			h := NewHandler(isoFile, "").(*installerHandler)
			reader, err := h.ReadFile("testdir/stuff")
			Expect(err).ToNot(HaveOccurred())
			fileContent, err := ioutil.ReadAll(reader)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(fileContent)).To(Equal("morecontent\n"))
		})

		It("read non-existant file from ISO", func() {
			h := NewHandler(isoFile, "").(*installerHandler)
			reader, err := h.ReadFile("testdir/noexist")
			Expect(err).To(HaveOccurred())
			Expect(reader).To(BeNil())
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
	err = ioutil.WriteFile(filepath.Join(filesDir, "files", "test"), []byte("testcontent\n"), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "files", "testdir"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files", "testdir", "stuff"), []byte("morecontent\n"), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files", "images"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "boot_files", "images", "efiboot.img"), []byte(""), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = os.Mkdir(filepath.Join(filesDir, "boot_files", "isolinux"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "boot_files", "isolinux", "boot.cat"), []byte(""), 0664)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "boot_files", "isolinux", "isolinux.bin"), []byte(""), 0664)
	Expect(err).ToNot(HaveOccurred())
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}
