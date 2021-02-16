package isoeditor

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cavaliercoder/go-cpio"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/sirupsen/logrus"
)

const (
	defaultTestOpenShiftVersion = "4.6"
	defaultTestServiceBaseURL   = "http://198.51.100.0:6000"
)

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func TestIsoEditor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IsoEditor")
}

var _ = Context("with test files", func() {
	var (
		isoDir   string
		isoFile  string
		filesDir string
		workDir  string
		volumeID = "Assisted123"
	)

	BeforeSuite(func() {
		filesDir, isoDir, isoFile = createIsoViaGenisoimage(volumeID)
	})

	AfterSuite(func() {
		os.RemoveAll(filesDir)
		os.RemoveAll(isoDir)
	})

	BeforeEach(func() {
		var err error
		workDir, err = ioutil.TempDir("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		err := os.RemoveAll(workDir)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := editorForFile(isoFile, workDir)
			file, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())

			// Creating the template should remove the working directory
			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})

		It("missing iso file", func() {
			editor := editorForFile("invalid", workDir)
			_, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateClusterMinimalISO", func() {
		It("removes the workspace", func() {
			editor := editorForFile(isoFile, workDir)
			proxyInfo := &ClusterProxyInfo{}
			file, err := editor.CreateClusterMinimalISO("ignition", "", proxyInfo)
			Expect(err).ToNot(HaveOccurred())

			// Creating the template should remove the working directory
			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			editor := editorForFile(isoFile, workDir)
			rootfsURL := fmt.Sprintf("%s/api/assisted-install/v1/boot-files?file_type=rootfs.img&openshift_version=%s",
				defaultTestServiceBaseURL, defaultTestOpenShiftVersion)
			isoHandler := editor.(*rhcosEditor).isoHandler

			err := isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			err = editor.(*rhcosEditor).fixTemplateConfigs(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())

			newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			grubCfg := fmt.Sprintf(newLine, rootfsURL)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			isolinuxCfg := fmt.Sprintf(newLine, rootfsURL)
			validateFileContainsLine(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isolinuxCfg)
		})
	})

	Describe("addCustomRAMDisk", func() {
		It("adds a new archive correctly", func() {
			editor := editorForFile(isoFile, workDir)

			isoHandler := editor.(*rhcosEditor).isoHandler
			err := isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			clusterProxyInfo := ClusterProxyInfo{
				HTTPProxy:  "http://10.10.1.1:3128",
				HTTPSProxy: "https://10.10.1.1:3128",
				NoProxy:    "quay.io",
			}

			err = editor.(*rhcosEditor).addCustomRAMDisk("staticipconfig", &clusterProxyInfo)
			Expect(err).ToNot(HaveOccurred())

			By("checking that the files are present in the archive")
			f, err := os.Open(isoHandler.ExtractedPath("images/assisted_installer_custom.img"))
			Expect(err).ToNot(HaveOccurred())

			var configContent, scriptContent, rootfsServiceConfigContent string
			r := cpio.NewReader(f)
			for {
				hdr, err := r.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				switch hdr.Name {
				case "/etc/static_ips_config.csv":
					configBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					configContent = string(configBytes)
				case "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-static-ip-config.sh":
					scriptBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					scriptContent = string(scriptBytes)
				case "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf":
					rootfsServiceConfigBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					rootfsServiceConfigContent = string(rootfsServiceConfigBytes)
				}
			}

			Expect(configContent).To(Equal("staticipconfig"))
			Expect(scriptContent).To(Equal(constants.ConfigStaticIpsScript))

			rootfsServiceConfig := fmt.Sprintf("[Service]\n"+
				"Environment=http_proxy=%s\nEnvironment=https_proxy=%s\nEnvironment=no_proxy=%s\n"+
				"Environment=HTTP_PROXY=%s\nEnvironment=HTTPS_PROXY=%s\nEnvironment=NO_PROXY=%s",
				clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy,
				clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy)
			Expect(rootfsServiceConfigContent).To(Equal(rootfsServiceConfig))

			By("checking that the config files were edited correctly")
			grubLine := "	initrd /images/pxeboot/initrd.img /images/ignition.img /images/assisted_installer_custom.img"
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubLine)

			isoLine := "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,/images/assisted_installer_custom.img random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal"
			validateFileContainsLine(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isoLine)
		})
	})
})

func editorForFile(iso string, workDir string) Editor {
	return &rhcosEditor{
		isoHandler:       isoutil.NewHandler(iso, workDir),
		openshiftVersion: defaultTestOpenShiftVersion,
		log:              getTestLog(),
	}
}

func createIsoViaGenisoimage(volumeID string) (string, string, string) {
	grubConfig := `
menuentry 'RHEL CoreOS (Live)' --class fedora --class gnu-linux --class gnu --class os {
	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
	initrd /images/pxeboot/initrd.img /images/ignition.img
}
`
	isoLinuxConfig := `
label linux
  menu label ^RHEL CoreOS (Live)
  menu default
  kernel /images/pxeboot/vmlinuz
  append initrd=/images/pxeboot/initrd.img,/images/ignition.img random.trust_cpu=on rd.luks.options=discard coreos.liveiso=rhcos-46.82.202010091720-0 ignition.firstboot ignition.platform.id=metal
`

	filesDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())

	isoDir, err := ioutil.TempDir("", "isotest")
	Expect(err).ToNot(HaveOccurred())
	isoFile := filepath.Join(isoDir, "test.iso")

	err = os.Mkdir(filepath.Join(filesDir, "files"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/images/pxeboot"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/pxeboot/rootfs.img"), []byte("this is rootfs"), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/EFI/redhat"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/EFI/redhat/grub.cfg"), []byte(grubConfig), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = os.MkdirAll(filepath.Join(filesDir, "files/isolinux"), 0755)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/isolinux/isolinux.cfg"), []byte(isoLinuxConfig), 0600)
	Expect(err).ToNot(HaveOccurred())
	cmd := exec.Command("genisoimage", "-rational-rock", "-J", "-joliet-long", "-V", volumeID, "-o", isoFile, filepath.Join(filesDir, "files"))
	err = cmd.Run()
	Expect(err).ToNot(HaveOccurred())

	return filesDir, isoDir, isoFile
}

func validateFileContainsLine(filename string, content string) {
	fileContent, err := ioutil.ReadFile(filename)
	Expect(err).NotTo(HaveOccurred())

	found := false
	for _, line := range strings.Split(string(fileContent), "\n") {
		if line == content {
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "Failed to find required string `%s` in file `%s`", content, filename)
}
