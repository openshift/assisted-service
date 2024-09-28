//nolint:gosec
package constants

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

var _ = Describe("Pre Network Config Script", func() {
	format.TruncatedDiff = false
	type replace struct {
		from string
		to   string
	}
	const assistedNetwork = "test_root/etc/assisted/network"
	var (
		root              string
		scriptPath        string
		err               error
		systemConnections string
	)
	BeforeEach(func() {
		root, err = os.MkdirTemp("", "test_root")
		Expect(err).ToNot(HaveOccurred())
		var f *os.File
		f, err = os.CreateTemp("", "script.sh")
		Expect(err).ToNot(HaveOccurred())
		var n int
		n, err = f.WriteString(PreNetworkConfigScript)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(len(PreNetworkConfigScript)))
		scriptPath = f.Name()
		Expect(f.Chmod(0o755)).ToNot(HaveOccurred())
		f.Close()
		systemConnections = filepath.Join(root, "etc/NetworkManager/system-connections")
	})
	AfterEach(func() {
		Expect(os.RemoveAll(root)).ToNot(HaveOccurred())
		Expect(os.RemoveAll(scriptPath)).ToNot(HaveOccurred())
	})
	copyFiles := func() {
		_, err = exec.Command("bash", "-c", "cp -r test_root/* "+root).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	}
	copySys := func() {
		_, err = exec.Command("bash", "-c", "cp -r test_root/sys "+root).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	}
	copyUnmatchingDir := func() {
		dir := filepath.Join(root, "etc/assisted/network")
		Expect(os.MkdirAll(dir, 0o777)).ToNot(HaveOccurred())
		_, err = exec.Command("bash", "-c", "cp -r test_root/etc/assisted/network/host1 "+dir).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
	}
	verifyFile := func(sourcePath, destPath string, replaces []replace) {
		src, err := os.ReadFile(sourcePath)
		Expect(err).ToNot(HaveOccurred())
		dst, err := os.ReadFile(destPath)
		Expect(err).ToNot(HaveOccurred())

		source := string(src)
		destination := string(dst)

		if len(replaces) > 0 {
			Expect(source).ToNot(Equal(destination))
		}
		for _, r := range replaces {
			source = strings.ReplaceAll(source, r.from, r.to)
		}
		Expect(source).To(Equal(destination))
	}

	It("script runs successfully when there are no host directories", func() {
		copySys()
		_, err := exec.Command("bash", "-c", fmt.Sprintf("PATH_PREFIX=%s %s", root, scriptPath)).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	Context("with all files", func() {
		BeforeEach(func() {
			copyFiles()
			_, err := exec.Command("bash", "-c", fmt.Sprintf("PATH_PREFIX=%s %s", root, scriptPath)).CombinedOutput()
			Expect(err).ToNot(HaveOccurred())
			info, err := os.Stat(systemConnections)
			Expect(os.IsNotExist(err)).To(BeFalse())
			Expect(info.IsDir()).To(BeTrue())
		})

		It("correctly transfers a vlan", func() {
			replaces := []replace{
				{
					from: "=eth0",
					to:   "=enp0s31f6",
				},
			}
			verifyFile(assistedNetwork+"/host0/eth0.nmconnection", systemConnections+"/enp0s31f6.nmconnection", replaces)
			verifyFile(assistedNetwork+"/host0/eth0.101.nmconnection", systemConnections+"/enp0s31f6.101.nmconnection", replaces)
		})

		It("correctly transfers bonded interfaces", func() {
			replaces := []replace{
				{
					from: "=eth0",
					to:   "=ens1",
				},
				{
					from: "=eth1",
					to:   "=ens2",
				},
			}
			verifyFile(assistedNetwork+"/host2/eth0.nmconnection", systemConnections+"/ens1.nmconnection", replaces)
			verifyFile(assistedNetwork+"/host2/eth1.nmconnection", systemConnections+"/ens2.nmconnection", replaces)
			verifyFile(assistedNetwork+"/host2/bond0.nmconnection", systemConnections+"/bond0.nmconnection", nil)
		})

		It("correctly transfers a regular interface", func() {
			replaces := []replace{
				{
					from: "=eth0",
					to:   "=wlp0s20f3",
				},
			}
			verifyFile(assistedNetwork+"/host3/eth0.nmconnection", systemConnections+"/wlp0s20f3.nmconnection", replaces)
		})

		It("truncates vlan interface names", func() {
			replaces := []replace{
				{
					from: "=eth0.2507",
					to:   "=enp94s0f0n.2507",
				},
				{
					from: "=eth0",
					to:   "=enp94s0f0np0",
				},
			}
			verifyFile(assistedNetwork+"/host4/eth0.2507.nmconnection", systemConnections+"/enp94s0f0n.2507.nmconnection", replaces)
			verifyFile(assistedNetwork+"/host4/eth0.nmconnection", systemConnections+"/enp94s0f0np0.nmconnection", replaces)
		})
	})

	It("doesn't copy any files when there is no matching host directory", func() {
		copySys()
		copyUnmatchingDir()
		_, err := exec.Command("bash", "-c", fmt.Sprintf("PATH_PREFIX=%s %s", root, scriptPath)).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})

func TestScripts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scripts Tests")
}
