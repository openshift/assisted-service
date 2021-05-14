package generator

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
)

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error
}

//go:generate mockgen -package generator -destination mock_install_config.go . ISOInstallConfigGenerator
type ISOInstallConfigGenerator interface {
	InstallConfigGenerator
}
