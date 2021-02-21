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
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
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
		isoDir                  string
		isoFile                 string
		filesDir                string
		workDir                 string
		volumeID                = "Assisted123"
		ctrl                    *gomock.Controller
		mockStaticNetworkConfig *staticnetworkconfig.MockStaticNetworkConfig
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
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		workDir, err = ioutil.TempDir("", "testisoeditor")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		err := os.RemoveAll(workDir)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("CreateMinimalISOTemplate", func() {
		It("iso created successfully", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
			err := editor.(*rhcosEditor).embedOffsetsInSystemArea(isoFile)
			Expect(err).ToNot(HaveOccurred())
			file, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())

			// Creating the template should remove the working directory
			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})

		It("missing iso file", func() {
			editor := editorForFile("invalid", workDir, mockStaticNetworkConfig)
			_, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateClusterMinimalISO", func() {
		It("cluster ISO created successfully", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
			proxyInfo := &ClusterProxyInfo{}
			file, err := editor.CreateClusterMinimalISO("ignition", "", proxyInfo)
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
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

			newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
			grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, rootfsURL)
			validateFileContainsLine(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isolinuxCfg)
		})
	})

	Describe("embedOffsetsInSystemArea", func() {
		It("embeds offsets in system area correctly", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)

			// Create template
			isoPath, err := editor.CreateMinimalISOTemplate(defaultTestServiceBaseURL)
			Expect(err).ToNot(HaveOccurred())

			// Read offsets
			ignitionOffsetInfo, ramDiskOffsetInfo, err := readHeader(isoPath)
			Expect(err).ToNot(HaveOccurred())

			// Validate ignitionOffsetInfo
			ignitionOffset, err := isoutil.GetFileLocation(ignitionImagePath, isoPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ignitionOffsetInfo.Key[:])).To(Equal(ignitionHeaderKey))
			Expect(ignitionOffsetInfo.Offset).To(Equal(ignitionOffset))
			Expect(ignitionOffsetInfo.Length).To(Equal(IgnitionPaddingLength))

			// Validate ramDiskOffsetInfo
			ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(ramDiskOffsetInfo.Key[:])).To(Equal(ramdiskHeaderKey))
			Expect(ramDiskOffsetInfo.Offset).To(Equal(ramDiskOffset))
			Expect(ramDiskOffsetInfo.Length).To(Equal(RamDiskPaddingLength))
		})
	})

	Describe("addCustomRAMDisk", func() {
		It("adds a new archive correctly", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
			isoHandler := editor.(*rhcosEditor).isoHandler
			clusterProxyInfo := ClusterProxyInfo{
				HTTPProxy:  "http://10.10.1.1:3128",
				HTTPSProxy: "https://10.10.1.1:3128",
				NoProxy:    "quay.io",
			}

			staticnetworkConfigOutput := []staticnetworkconfig.StaticNetworkConfigData{
				{
					FilePath:     "1.nmconnection",
					FileContents: "1.nmconnection contents",
				},
				{
					FilePath:     "2.nmconnection",
					FileContents: "2.nmconnection contents",
				},
			}
			mockStaticNetworkConfig.EXPECT().GenerateStaticNetworkConfigData("staticnetworkconfig").Return(staticnetworkConfigOutput, nil).Times(1)

			ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoFile)
			Expect(err).ToNot(HaveOccurred())

			err = editor.(*rhcosEditor).addCustomRAMDisk(isoFile, "staticnetworkconfig", &clusterProxyInfo, ramDiskOffset)
			Expect(err).ToNot(HaveOccurred())

			err = isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			By("checking that the files are present in the archive")
			f, err := os.Open(isoHandler.ExtractedPath("images/assisted_installer_custom.img"))
			Expect(err).ToNot(HaveOccurred())

			var scriptContent, rootfsServiceConfigContent string
			r := cpio.NewReader(f)
			for {
				hdr, err := r.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				switch hdr.Name {
				case "/etc/1.nmconnection":
					configBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					Expect(string(configBytes)).To(Equal("1.nmconnection contents"))
				case "/etc/2.nmconnection":
					configBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					Expect(string(configBytes)).To(Equal("2.nmconnection contents"))
				case "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-pre-static-network-config.sh":
					scriptBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					scriptContent = string(scriptBytes)
				case "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf":
					rootfsServiceConfigBytes, err := ioutil.ReadAll(r)
					Expect(err).ToNot(HaveOccurred())
					rootfsServiceConfigContent = string(rootfsServiceConfigBytes)
				}
			}

			Expect(scriptContent).To(Equal(constants.PreNetworkConfigScript))

			rootfsServiceConfig := fmt.Sprintf("[Service]\n"+
				"Environment=http_proxy=%s\nEnvironment=https_proxy=%s\nEnvironment=no_proxy=%s\n"+
				"Environment=HTTP_PROXY=%s\nEnvironment=HTTPS_PROXY=%s\nEnvironment=NO_PROXY=%s",
				clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy,
				clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy)
			Expect(rootfsServiceConfigContent).To(Equal(rootfsServiceConfig))
		})
	})
})

func editorForFile(iso string, workDir string, staticNetworkConfig staticnetworkconfig.StaticNetworkConfig) Editor {
	return &rhcosEditor{
		isoHandler:          isoutil.NewHandler(iso, workDir),
		openshiftVersion:    defaultTestOpenShiftVersion,
		log:                 getTestLog(),
		staticNetworkConfig: staticNetworkConfig,
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
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/assisted_installer_custom.img"), make([]byte, RamDiskPaddingLength), 0600)
	Expect(err).ToNot(HaveOccurred())
	err = ioutil.WriteFile(filepath.Join(filesDir, "files/images/ignition.img"), make([]byte, IgnitionPaddingLength), 0600)
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
