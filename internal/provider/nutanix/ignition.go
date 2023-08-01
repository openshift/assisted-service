package nutanix

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/openshift/assisted-service/internal/common"
)

func (p nutanixProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
func (p nutanixProvider) PostCreateManifestsHook(_ *common.Cluster, _ *[]string, workDir string) error {
	// Deleting machines and machineSets for nutanix platform after manifest generation
	// The following steps are included in the Openshift UPI vSphere installation guide. Go to step 2 in the link below:
	// https://docs.openshift.com/container-platform/4.9/installing/installing_vsphere/installing-vsphere.html#installation-user-infra-generate-k8s-manifest-ignition_installing-vsphere

	// Delete machines
	p.Log.Info("Deleting machines manifests")
	files, _ := filepath.Glob(path.Join(workDir, "openshift", "*_openshift-cluster-api_master-machines-*.yaml"))
	err := p.deleteAllFiles(files)

	if err != nil {
		return fmt.Errorf("error deleting master machine: %w", err)
	}

	// Delete machine-set
	p.Log.Info("Deleting machine set manifest")
	files, _ = filepath.Glob(path.Join(workDir, "openshift", "*_openshift-cluster-api_worker-machineset-*.yaml"))
	err = p.deleteAllFiles(files)

	if err != nil {
		return fmt.Errorf("error deleting machineset: %w", err)
	}

	// Delete control-plane-machine-set
	p.Log.Info("Deleting control-plane machine set")
	files, _ = filepath.Glob(path.Join(workDir, "openshift", "*_openshift-machine-api_master-control-plane-machine-set*.yaml"))
	err = p.deleteAllFiles(files)

	if err != nil {
		return fmt.Errorf("error deleting control-plane machineset: %w", err)
	}

	return nil
}

func (p nutanixProvider) deleteAllFiles(files []string) error {
	for _, f := range files {
		p.Log.Infof("Deleting manifest %s", f)

		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
