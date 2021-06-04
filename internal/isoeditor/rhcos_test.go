package isoeditor

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/cavaliercoder/go-cpio"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/sirupsen/logrus"
)

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

var _ = Context("with test files", func() {
	var (
		workDir                 string
		ctrl                    *gomock.Controller
		mockStaticNetworkConfig *staticnetworkconfig.MockStaticNetworkConfig
	)

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
			file, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())

			// Creating the template should remove the working directory
			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})

		It("missing iso file", func() {
			editor := editorForFile("invalid", workDir, mockStaticNetworkConfig)
			_, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CreateClusterMinimalISO", func() {
		It("cluster ISO created successfully", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
			proxyInfo := &ClusterProxyInfo{}
			file, err := editor.CreateClusterMinimalISO("ignition", nil, proxyInfo)
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(workDir)
			Expect(os.IsNotExist(err)).To(BeTrue())

			os.Remove(file)
		})
	})

	Describe("fixTemplateConfigs", func() {
		It("alters the kernel parameters correctly", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)
			isoHandler := editor.(*rhcosEditor).isoHandler

			err := isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			err = editor.(*rhcosEditor).fixTemplateConfigs(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())

			newLine := "	linux /images/pxeboot/vmlinuz random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal 'coreos.live.rootfs_url=%s'"
			grubCfg := fmt.Sprintf(newLine, testRootFSURL)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "	initrd /images/pxeboot/initrd.img /images/ignition.img %s"
			grubCfg = fmt.Sprintf(newLine, ramDiskImagePath)
			validateFileContainsLine(isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), grubCfg)

			newLine = "  append initrd=/images/pxeboot/initrd.img,/images/ignition.img,%s random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal coreos.live.rootfs_url=%s"
			isolinuxCfg := fmt.Sprintf(newLine, ramDiskImagePath, testRootFSURL)
			validateFileContainsLine(isoHandler.ExtractedPath("isolinux/isolinux.cfg"), isolinuxCfg)
		})
	})

	Describe("embedOffsetsInSystemArea", func() {
		It("embeds offsets in system area correctly", func() {
			editor := editorForFile(isoFile, workDir, mockStaticNetworkConfig)

			// Create template
			isoPath, err := editor.CreateMinimalISOTemplate(testRootFSURL)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(isoPath)

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

			ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoFile)
			Expect(err).ToNot(HaveOccurred())

			ramDiskSize, err := isoutil.GetFileSize(ramDiskImagePath, isoFile)
			Expect(err).ToNot(HaveOccurred())

			err = addCustomRAMDisk(isoFile, staticnetworkConfigOutput, &clusterProxyInfo,
				&OffsetInfo{
					Offset: ramDiskOffset,
					Length: ramDiskSize,
				})
			Expect(err).ToNot(HaveOccurred())

			err = isoHandler.Extract()
			Expect(err).ToNot(HaveOccurred())

			By("checking that the files are present in the archive")
			f, err := os.Open(isoHandler.ExtractedPath("images/assisted_installer_custom.img"))
			Expect(err).ToNot(HaveOccurred())

			gzipReader, err := gzip.NewReader(f)
			Expect(err).ToNot(HaveOccurred())

			var scriptContent, rootfsServiceConfigContent string
			r := cpio.NewReader(gzipReader)
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
	It("custom RAM disk is larger than placeholder", func() {
		staticNetworkConfigOutput := []staticnetworkconfig.StaticNetworkConfigData{
			{
				FilePath:     "1.nmconnection",
				FileContents: "1.nmconnection contents",
			},
		}

		ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoFile)
		Expect(err).ToNot(HaveOccurred())

		err = addCustomRAMDisk(isoFile, staticNetworkConfigOutput, &ClusterProxyInfo{},
			&OffsetInfo{
				Offset: ramDiskOffset,
				Length: 10, // Set a tiny value as the archive is compressed
			})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).Should(ContainSubstring("Custom RAM disk is larger than the placeholder in ISO"))
	})
})

func editorForFile(iso string, workDir string, staticNetworkConfig staticnetworkconfig.StaticNetworkConfig) Editor {
	return &rhcosEditor{
		isoHandler: isoutil.NewHandler(iso, workDir),
		log:        getTestLog(),
	}
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
