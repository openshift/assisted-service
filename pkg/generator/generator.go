package generator

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
)

type ISOGenerator interface {
	UploadBaseISO() error
}

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte) error
	AbortInstallConfig(ctx context.Context, cluster common.Cluster) error
}

type ISOInstallConfigGenerator interface {
	ISOGenerator
	InstallConfigGenerator
}
