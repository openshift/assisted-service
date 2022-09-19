package isoeditor

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"testing"

	"github.com/cavaliercoder/go-cpio"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
)

func TestIsoEditor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IsoEditor")
}

var _ = Describe("RamdiskImageArchive", func() {
	It("adds a new archive correctly", func() {
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

		archive, err := RamdiskImageArchive(staticnetworkConfigOutput, &clusterProxyInfo)
		Expect(err).ToNot(HaveOccurred())

		By("checking that the files are present in the archive")
		gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
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
				configBytes, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(configBytes)).To(Equal("1.nmconnection contents"))
			case "/etc/2.nmconnection":
				configBytes, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(configBytes)).To(Equal("2.nmconnection contents"))
			case "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-pre-static-network-config.sh":
				scriptBytes, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				scriptContent = string(scriptBytes)
			case "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf":
				rootfsServiceConfigBytes, err := io.ReadAll(r)
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
	It("returns nothing when given nothing", func() {
		archive, err := RamdiskImageArchive(
			[]staticnetworkconfig.StaticNetworkConfigData{},
			&ClusterProxyInfo{},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(archive).To(BeNil())
	})
})
