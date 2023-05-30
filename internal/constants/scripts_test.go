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
)

var _ = Describe("Pre Network Config Script", func() {
	type replace struct {
		from string
		to   string
	}
	type expectedFile struct {
		source      string
		destination string
		replaces    []replace
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
	verifyExpectedFiles := func(expectedFiles []expectedFile) {
		for _, e := range expectedFiles {
			src, err := os.ReadFile(e.source)
			Expect(err).ToNot(HaveOccurred())
			dst, err := os.ReadFile(e.destination)
			Expect(err).ToNot(HaveOccurred())
			source := string(src)
			destination := string(dst)
			if len(e.replaces) > 0 {
				Expect(source).ToNot(Equal(destination))
			}
			for _, r := range e.replaces {
				source = strings.ReplaceAll(source, r.from, r.to)
			}
			Expect(source).To(Equal(destination))
		}
	}
	It("script runs successfully when there are no host directories", func() {
		copySys()
		_, err := exec.Command("bash", "-c", fmt.Sprintf("PATH_PREFIX=%s %s", root, scriptPath)).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
	It("copies all the network config from matching host directories", func() {
		copyFiles()
		_, err := exec.Command("bash", "-c", fmt.Sprintf("PATH_PREFIX=%s %s", root, scriptPath)).CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		info, err := os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeFalse())
		Expect(info.IsDir()).To(BeTrue())
		expectedFiles := []expectedFile{
			{
				source:      assistedNetwork + "/host0/eth0.nmconnection",
				destination: systemConnections + "/enp0s31f6.nmconnection",
				replaces: []replace{
					{
						from: "=eth0",
						to:   "=enp0s31f6",
					},
				},
			},
			{
				source:      assistedNetwork + "/host0/eth0.101.nmconnection",
				destination: systemConnections + "/enp0s31f6.101.nmconnection",
				replaces: []replace{
					{
						from: "=eth0",
						to:   "=enp0s31f6",
					},
				},
			},
			{
				source:      assistedNetwork + "/host2/eth0.nmconnection",
				destination: systemConnections + "/ens1.nmconnection",
				replaces: []replace{
					{
						from: "=eth0",
						to:   "=ens1",
					},
				},
			},
			{
				source:      assistedNetwork + "/host2/eth1.nmconnection",
				destination: systemConnections + "/ens2.nmconnection",
				replaces: []replace{
					{
						from: "=eth1",
						to:   "=ens2",
					},
				},
			},
			{
				source:      assistedNetwork + "/host2/bond0.nmconnection",
				destination: systemConnections + "/bond0.nmconnection",
			},
			{
				source:      assistedNetwork + "/host3/eth0.nmconnection",
				destination: systemConnections + "/wlp0s20f3.nmconnection",
				replaces: []replace{
					{
						from: "=eth0",
						to:   "=wlp0s20f3",
					},
				},
			},
		}
		entries, err := os.ReadDir(systemConnections)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(entries)).To(Equal(len(expectedFiles)))
		verifyExpectedFiles(expectedFiles)
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
