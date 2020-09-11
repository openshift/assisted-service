package generator

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
)

type ISOGenerator interface {
	GenerateISO(ctx context.Context, cluster common.Cluster, jobName string, imageName string, ignitionConfig string, eventsHandler events.Handler) error
}

type InstallConfigGenerator interface {
	GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte, objectHandler s3wrapper.API) error
	AbortInstallConfig(ctx context.Context, cluster common.Cluster) error
}

type ISOInstallConfigGenerator interface {
	ISOGenerator
	InstallConfigGenerator
}
