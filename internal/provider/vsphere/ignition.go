package vsphere

import "github.com/openshift/assisted-service/internal/common"

func (p vsphereProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
func (p vsphereProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
