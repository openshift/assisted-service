package isoutil

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	diskfs "github.com/diskfs/go-diskfs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestIsoUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Iso Util")
}

var _ = Describe("Isoutil", func() {
	var (
		isoDir           string
		isoFile          string
		filesDir         string
		filesHandler     *installerHandler
		bootFilesHandler *installerHandler
	)
	BeforeSuite(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage()
		filesHandler = NewHandler(filepath.Join(filesDir, "files")).(*installerHandler)
		bootFilesHandler = NewHandler(filepath.Join(filesDir, "boot_files")).(*installerHandler)
	})

	AfterSuite(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	Context("Extract", func() {
		It("extracts the files from an iso", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)

			h := NewHandler(dir).(*installerHandler)
			Expect(h.Extract(isoFile)).To(Succeed())

			validateFileContent(filepath.Join(dir, "test"), "testcontent\n")
			validateFileContent(filepath.Join(dir, "testdir/stuff"), "morecontent\n")
		})
	})

	Context("Create", func() {
		It("generates an iso with the content in the given directory", func() {
			dir, err := ioutil.TempDir("", "isotest")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)

			tmpIsoPath := filepath.Join(dir, "test.iso")
			Expect(filesHandler.Create(tmpIsoPath, isoFileSize(isoFile), "my-vol")).To(Succeed())

			d, err := diskfs.OpenWithMode(tmpIsoPath, diskfs.ReadOnly)
			Expect(err).ToNot(HaveOccurred())
			fs, err := d.GetFilesystem(0)
			Expect(err).ToNot(HaveOccurred())

			f, err := fs.OpenFile("/test", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err := ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("testcontent\n"))

			f, err = fs.OpenFile("/testdir/stuff", os.O_RDONLY)
			Expect(err).ToNot(HaveOccurred())
			content, err = ioutil.ReadAll(f)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("morecontent\n"))
		})
	})

	Context("fileExists", func() {
		It("returns true when file exists", func() {
			exists, err := filesHandler.fileExists("test")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())

			exists, err = filesHandler.fileExists("testdir/stuff")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("returns false when file does not exist", func() {
			exists, err := filesHandler.fileExists("asdf")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())

			exists, err = filesHandler.fileExists("missingdir/things")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})

	Context("haveBootFiles", func() {
		It("returns true when boot files are present", func() {
			haveBootFiles, err := bootFilesHandler.haveBootFiles()
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeTrue())
		})

		It("returns false when boot files are not present", func() {
			haveBootFiles, err := filesHandler.haveBootFiles()
			Expect(err).ToNot(HaveOccurred())
			Expect(haveBootFiles).To(BeFalse())
		})
	})
})

func isoFileSize(isoFile string) int64 {
	info, err := os.Stat(isoFile)
	Expect(err).NotTo(HaveOccurred())
	return info.Size()
}

func createIsoViaGenisoimage() (string, string, string) {
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
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}

func validateFileContent(filename string, content string) {
	fileContent, err := ioutil.ReadFile(filename)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(fileContent)).To(Equal(content))
}
