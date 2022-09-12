package none

import "github.com/openshift/assisted-service/internal/common"

func (p noneProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
func (p noneProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
