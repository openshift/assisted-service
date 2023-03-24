package external

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
)

// Workaround as openshift installer does not support the external platform.
// The trick is to generate the manifests with platform=none and patch
// the insfrastructure object with the external platform.
const (
	clusterInfraPatchTemplate = `---
	- op: replace
	  path: /spec/platformSpec
	  value:
		type: External
		external:
		  platformName: %s
	- op: replace
	  path: /status/platform
	  value: External
	- op: replace
	  path: /status/platformStatus
	  value:
		type: External
		external:
		  cloudControllerManager:
			state: External
	`
	defaultPlatformName = "Unknown"
)

func (p externalProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}

func (p externalProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	p.Log.Info("Patching infrastructure object")

	platformName := defaultPlatformName
	if cluster.Platform.External != nil && cluster.Platform.External.PlatformName != nil {
		platformName = *cluster.Platform.External.PlatformName
	}
	clusterInfraPatch := fmt.Sprintf(clusterInfraPatchTemplate, platformName)

	clusterInfraPatchPath := filepath.Join(workDir, "manifests", "cluster-infrastructure-02-config.yml.patch_01_configure_external_platform")
	err := os.WriteFile(clusterInfraPatchPath, []byte(clusterInfraPatch), 0600)
	if err != nil {
		p.Log.Error("Couldn't write infrastructure patch %v", clusterInfraPatchPath)
		return err
	}

	return nil
}
