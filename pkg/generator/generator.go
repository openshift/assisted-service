package generator

import (
	"context"

	"github.com/openshift/assisted-service/internal/dbc"
)

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster dbc.Cluster, cfg []byte, releaseImage string) error
	AbortInstallConfig(ctx context.Context, cluster dbc.Cluster) error
}

//go:generate mockgen -package generator -destination mock_install_config.go . ISOInstallConfigGenerator
type ISOInstallConfigGenerator interface {
	InstallConfigGenerator
}
