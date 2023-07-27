package external

import (
	"github.com/openshift/assisted-service/internal/common"
)

func (p externalProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}

func (p externalProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	return nil
}
