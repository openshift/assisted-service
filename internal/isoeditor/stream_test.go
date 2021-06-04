package isoeditor

import (
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/cavaliercoder/go-cpio"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
)

var _ = Describe("Read", func() {
	validateISOIgnitionContent := func(isoPath, content string) {
		workDir, err := ioutil.TempDir("", "isoStreamEditorWorkDir")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(workDir)

		h := isoutil.NewHandler(isoPath, workDir)
		ignitionArchiveReader, err := h.ReadFile("/images/ignition.img")
		Expect(err).NotTo(HaveOccurred())

		gzipReader, err := gzip.NewReader(ignitionArchiveReader)
		Expect(err).ToNot(HaveOccurred())

		r := cpio.NewReader(gzipReader)
		header, err := r.Next()
		Expect(err).NotTo(HaveOccurred())
		Expect(header.Name).To(Equal("config.ign"))
		ignBytes, err := ioutil.ReadAll(r)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(ignBytes)).To(Equal(content))
	}

	validateISOInitrdContent := func(isoPath, filePath, content string) {
		workDir, err := ioutil.TempDir("", "isoStreamEditorWorkDir")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(workDir)

		h := isoutil.NewHandler(isoPath, workDir)
		initrdArchiveReader, err := h.ReadFile("/images/assisted_installer_custom.img")
		Expect(err).NotTo(HaveOccurred())

		gzipReader, err := gzip.NewReader(initrdArchiveReader)
		Expect(err).ToNot(HaveOccurred())

		r := cpio.NewReader(gzipReader)
		for {
			header, err := r.Next()
			if err == io.EOF {
				break
			}
			Expect(err).ToNot(HaveOccurred())
			if header.Name == filePath {
				fileBytes, err := ioutil.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(fileBytes)).To(Equal(content))
				return
			}
		}
		Fail(fmt.Sprintf("Could not find file %s in initrd archive", filePath))
	}

	It("creates a correct customized minimal iso", func() {
		isoReader, err := os.Open(isoFile)
		Expect(err).NotTo(HaveOccurred())
		stat, err := isoReader.Stat()
		Expect(err).NotTo(HaveOccurred())

		netFiles := []staticnetworkconfig.StaticNetworkConfigData{
			{
				FilePath:     "1.nmconnection",
				FileContents: "1.nmconnection contents",
			},
			{
				FilePath:     "2.nmconnection",
				FileContents: "2.nmconnection contents",
			},
		}
		clusterProxyInfo := &ClusterProxyInfo{
			HTTPProxy:  "http://192.0.2.12:3128",
			HTTPSProxy: "https://192.0.2.12:3128",
			NoProxy:    "quay.io",
		}

		streamEditor, err := NewClusterISOReader(isoReader, "ignitionContentHere", netFiles, clusterProxyInfo, models.ImageTypeMinimalIso)
		Expect(err).NotTo(HaveOccurred())

		newFile, err := ioutil.TempFile("", "isoStreamEditor")
		Expect(err).NotTo(HaveOccurred())
		newISOName := newFile.Name()
		defer os.Remove(newISOName)

		written, err := io.Copy(newFile, streamEditor)
		Expect(err).NotTo(HaveOccurred())
		Expect(written).To(Equal(stat.Size()))

		validateISOIgnitionContent(newISOName, "ignitionContentHere")
		validateISOInitrdContent(newISOName, "/etc/assisted/network/1.nmconnection", "1.nmconnection contents")
		validateISOInitrdContent(newISOName, "/etc/assisted/network/2.nmconnection", "2.nmconnection contents")
		rootfsServiceConfig := `[Service]
Environment=http_proxy=http://192.0.2.12:3128
Environment=https_proxy=https://192.0.2.12:3128
Environment=no_proxy=quay.io
Environment=HTTP_PROXY=http://192.0.2.12:3128
Environment=HTTPS_PROXY=https://192.0.2.12:3128
Environment=NO_PROXY=quay.io`
		validateISOInitrdContent(newISOName, "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf", rootfsServiceConfig)
		validateISOInitrdContent(newISOName, "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-pre-static-network-config.sh", constants.PreNetworkConfigScript)
	})

	It("creates a minimal iso with no extra ramdisk info", func() {
		isoReader, err := os.Open(isoFile)
		Expect(err).NotTo(HaveOccurred())
		stat, err := isoReader.Stat()
		Expect(err).NotTo(HaveOccurred())

		streamEditor, err := NewClusterISOReader(isoReader, "ignitionContentHere", nil, nil, models.ImageTypeMinimalIso)
		Expect(err).NotTo(HaveOccurred())

		newFile, err := ioutil.TempFile("", "isoStreamEditor")
		Expect(err).NotTo(HaveOccurred())
		newISOName := newFile.Name()
		defer os.Remove(newISOName)

		written, err := io.Copy(newFile, streamEditor)
		Expect(err).NotTo(HaveOccurred())
		Expect(written).To(Equal(stat.Size()))

		validateISOIgnitionContent(newISOName, "ignitionContentHere")
	})

	It("creates a correct customized full iso", func() {
		isoReader, err := os.Open(isoFile)
		Expect(err).NotTo(HaveOccurred())
		stat, err := isoReader.Stat()
		Expect(err).NotTo(HaveOccurred())

		streamEditor, err := NewClusterISOReader(isoReader, "ignitionContentHere", nil, nil, models.ImageTypeFullIso)
		Expect(err).NotTo(HaveOccurred())

		newFile, err := ioutil.TempFile("", "isoStreamEditor")
		Expect(err).NotTo(HaveOccurred())
		newISOName := newFile.Name()
		defer os.Remove(newISOName)

		written, err := io.Copy(newFile, streamEditor)
		Expect(err).NotTo(HaveOccurred())
		Expect(written).To(Equal(stat.Size()))

		validateISOIgnitionContent(newISOName, "ignitionContentHere")
	})

	It("fails with an ignition that is larger than the reserved area", func() {
		isoReader, err := os.Open(isoFile)
		Expect(err).NotTo(HaveOccurred())

		// use a much larger random slice to ensure compression doesn't bring it under the limit
		data := make([]byte, IgnitionPaddingLength*2)
		count, err := rand.Reader.Read(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(uint64(count)).To(Equal(IgnitionPaddingLength * 2))

		streamEditor, err := NewClusterISOReader(isoReader, string(data), nil, nil, models.ImageTypeFullIso)
		Expect(err).NotTo(HaveOccurred())

		newFile, err := ioutil.TempFile("", "isoStreamEditor")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(newFile.Name())

		_, err = io.Copy(newFile, streamEditor)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds embed area size"))
	})

	It("fails with a ramdisk that is larger than the reserved area", func() {
		isoReader, err := os.Open(isoFile)
		Expect(err).NotTo(HaveOccurred())

		// use a much larger random slice to ensure compression doesn't bring it under the limit
		data := make([]byte, RamDiskPaddingLength*2)
		count, err := rand.Reader.Read(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(uint64(count)).To(Equal(RamDiskPaddingLength * 2))

		netFiles := []staticnetworkconfig.StaticNetworkConfigData{
			{
				FilePath:     "1.nmconnection",
				FileContents: string(data),
			},
		}

		streamEditor, err := NewClusterISOReader(isoReader, "ignition", netFiles, nil, models.ImageTypeMinimalIso)
		Expect(err).NotTo(HaveOccurred())

		newFile, err := ioutil.TempFile("", "isoStreamEditor")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(newFile.Name())

		_, err = io.Copy(newFile, streamEditor)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds embed area size"))
	})
})
