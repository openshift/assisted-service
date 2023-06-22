package external

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"
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
        state: External`
)

func (p externalProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}

func (p externalProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {

	platformName := string(p.platformType)
	clusterInfraPatch := fmt.Sprintf(clusterInfraPatchTemplate, platformName)

	p.Log.Infof("Patching Infrastructure CR with external platform: platformName=%s", platformName)

	infraManifest := filepath.Join(workDir, "manifests", "cluster-infrastructure-02-config.yml")
	data, err := os.ReadFile(infraManifest)
	if err != nil {
		return errors.Wrapf(err, "failed to read Infrastructure Manifest \"%s\"", infraManifest)
	}
	p.Log.Infof("read the infrastructure manifest at %s", infraManifest)

	data, err = common.ApplyYamlPatch(data, []byte(clusterInfraPatch))
	if err != nil {
		return errors.Wrapf(err, "failed to patch Infrastructure Manifest \"%s\"", infraManifest)
	}
	p.Log.Infof("applied the yaml patch to the infrastructure manifest at %s: \n %s", infraManifest, string(data[:]))

	err = os.WriteFile(infraManifest, data, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to write Infrastructure Manifest \"%s\"", infraManifest)
	}
	p.Log.Infof("wrote the resulting infrastructure manifest at %s", infraManifest)

	return nil
}
