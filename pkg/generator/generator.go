package generator

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
)

type ISOGenerator interface {
	UploadBaseISO() error
}

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, releaseImage string) error
	AbortInstallConfig(ctx context.Context, cluster common.Cluster) error
}

//go:generate mockgen -package generator -destination mock_install_config.go . ISOInstallConfigGenerator
type ISOInstallConfigGenerator interface {
	ISOGenerator
	InstallConfigGenerator
}
