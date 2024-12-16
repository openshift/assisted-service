package testing

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	// This is the name of the tool used to prepare 'envtest'.
	envtestSetupToolName = "setup-envtest"

	// Name of the Go package that contains the tool:
	envtestSetupToolsPkg = "sigs.k8s.io/controller-runtime/tools"

	// Branch `release-0.17` is the newest version that can be installed with Go 1.21. This should be updated when
	// we update the version of Go.
	envtestSetupToolVersion = "release-0.17"

	// This is the version of the Kubernetes binaries that will be installed by the 'setup-envtest' tool.
	envtestAssetsVersion = "1.30.0"

	// Default location where the library looks for binaries.
	envtestAssetsDir = "/usr/local/kubebuilder/bin"

	// Environment variable that overrides the default location of binaries.
	envtestAssetsEnv = "KUBEBUILDER_ASSETS"
)

var (
	// Names of asset files:
	envtestAssetFiles = []string{
		"etcd",
		"kube-apiserver",
		"kubectl",
	}
)

// SetupEnvtest prepares the machine for use of the 'envtest` package. It installs the 'setup-envtest' tool if needed,
// uses it to download the assets and prepares an envtest.Environment to use them. If the passed environment is nil a
// new one will be created. The returned environment is the passed one, or a new one if that is nil. The rest of the
// preparation, like adding CRDs, starting and stopping the environment, are responsibility of the caller.
func SetupEnvtest(env *envtest.Environment) *envtest.Environment {
	var err error

	// Create a new empty environment if needed:
	if env == nil {
		env = &envtest.Environment{}
	}

	// If the binaries are already available then we don't need to do anything else, the library will pick and
	// use them automatically.
	assetsDir, ok := os.LookupEnv(envtestAssetsEnv)
	if !ok || assetsDir == "" {
		assetsDir = envtestAssetsDir
	}
	assetsMissing := 0
	for _, assetFile := range envtestAssetFiles {
		assetPath := filepath.Join(assetsDir, assetFile)
		_, err = os.Stat(assetPath)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "Asset file '%s' doesn't exist: %v\n", assetPath, err)
			assetsMissing++
		}
	}
	if assetsMissing == 0 {
		return env
	}

	// Install the setup tool if needed:
	setupToolPath, err := exec.LookPath(envtestSetupToolName)
	if errors.Is(err, exec.ErrNotFound) {
		fmt.Fprintf(GinkgoWriter, "Tool '%s' isn't available, will try to install it\n", envtestSetupToolName)
		// #nosec:G204
		goInstallCmd := exec.Command(
			"go", "install",
			fmt.Sprintf("%s/%s@%s", envtestSetupToolsPkg, envtestSetupToolName, envtestSetupToolVersion),
		)
		goInstallCmd.Stdout = GinkgoWriter
		goInstallCmd.Stderr = GinkgoWriter
		err = goInstallCmd.Run()
		Expect(err).ToNot(HaveOccurred())
		setupToolPath, err = exec.LookPath(envtestSetupToolName)
	}
	Expect(err).ToNot(HaveOccurred())

	// Run the setup tool to ensure install the assets and get their location:
	setupToolOut := &bytes.Buffer{}
	setupToolCmd := exec.Command(setupToolPath, "use", "--print", "path", envtestAssetsVersion)
	setupToolCmd.Stdout = setupToolOut
	setupToolCmd.Stderr = GinkgoWriter
	err = setupToolCmd.Run()
	Expect(err).ToNot(HaveOccurred())
	assetsDir = strings.TrimSpace(setupToolOut.String())

	// Prepare the environment:
	env.BinaryAssetsDirectory = assetsDir
	return env
}
