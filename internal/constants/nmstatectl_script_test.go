package constants

import (
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pre Network Config With Nmstatectl Script", func() {
	var (
		root                  string
		scriptPath            string
		commonNetworkFilePath string
		err                   error
		systemConnections     string
	)
	BeforeSuite(func() {
		var commonNetworkFile *os.File
		var n int
		commonNetworkFile, err = os.CreateTemp("", "common_network_script.sh")
		Expect(err).ToNot(HaveOccurred())
		n, err = commonNetworkFile.WriteString(CommonNetworkScript)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(len(CommonNetworkScript)))
		commonNetworkFilePath = commonNetworkFile.Name()
		Expect(commonNetworkFile.Chmod(0o755)).ToNot(HaveOccurred())
		os.Setenv("COMMON_SCRIPT_PATH", commonNetworkFile.Name())
		commonNetworkFile.Close()

		var f *os.File
		f, err = os.CreateTemp("", "nmstatectl_script.sh")
		Expect(err).ToNot(HaveOccurred())
		n, err = f.WriteString(PreNetworkConfigScriptWithNmstatectl)
		Expect(err).ToNot(HaveOccurred())
		Expect(n).To(Equal(len(PreNetworkConfigScriptWithNmstatectl)))
		scriptPath = f.Name()
		Expect(f.Chmod(0o755)).ToNot(HaveOccurred())
		f.Close()

	})
	AfterSuite(func() {
		Expect(os.RemoveAll(commonNetworkFilePath)).ToNot(HaveOccurred())
		Expect(os.RemoveAll(scriptPath)).ToNot(HaveOccurred())
	})
	BeforeEach(func() {
		root, err = os.MkdirTemp("", "test_root_with_nmstate")
		Expect(err).ToNot(HaveOccurred())
		systemConnections = filepath.Join(root, "etc/NetworkManager/system-connections")
	})
	AfterEach(func() {
		Expect(os.RemoveAll(root)).ToNot(HaveOccurred())
	})
	copySys := func() {
		err := CopyDir("test_root_with_nmstate/sys", root)
		Expect(err).ToNot(HaveOccurred())
	}
	copyFiles := func() {
		err := CopyDir("test_root_with_nmstate/", root)
		Expect(err).ToNot(HaveOccurred())
	}
	copyUnmatchingDir := func() {
		dir := filepath.Join(root, "etc/assisted/network/host0")
		Expect(os.MkdirAll(dir, 0o777)).ToNot(HaveOccurred())
		err := CopyDir("test_root_with_nmstate/etc/assisted/network/host0", dir)
		Expect(err).ToNot(HaveOccurred())
	}
	It("script runs successfully when there are no host directories", func() {
		copySys()

		err := os.Setenv("PATH_PREFIX", path.Join(root, scriptPath))
		Expect(err).ToNot(HaveOccurred())
		cmd := exec.Command(scriptPath)
		Expect(cmd.Run()).To(Succeed())

		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
	It("doesn't copy any files when there is no matching host directory", func() {
		copyFiles()
		copySys()
		copyUnmatchingDir()

		err := os.Setenv("PATH_PREFIX", path.Join(root, scriptPath))
		Expect(err).ToNot(HaveOccurred())
		cmd := exec.Command(scriptPath)
		Expect(cmd.Run()).To(Succeed())

		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(systemConnections)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})

// CopyDir recursively copies a directory and its contents.
func CopyDir(src, dest string) error {
	return filepath.Walk(src, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Construct the destination path
		relPath := srcPath[len(src):]
		destPath := filepath.Join(dest, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return CopyFile(srcPath, destPath)
	})
}

// CopyFile copies a single file from src to dest.
func CopyFile(srcFile, destFile string) error {
	src, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer src.Close()

	dest, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err = io.Copy(dest, src); err != nil {
		return err
	}

	srcInfo, err := src.Stat()
	if err != nil {
		return err
	}

	return os.Chmod(destFile, srcInfo.Mode())
}
