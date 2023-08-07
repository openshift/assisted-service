package external

import (
	"github.com/openshift/assisted-service/internal/common"
)

func (p baseExternalProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}

func (p baseExternalProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
