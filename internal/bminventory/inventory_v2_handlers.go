package bminventory

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

func (b *bareMetalInventory) V2UpdateHost(ctx context.Context, params installer.V2UpdateHostParams) middleware.Responder {
	host, err := b.V2UpdateHostInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostCreated().WithPayload(&host.Host)
}

func (b *bareMetalInventory) V2RegisterCluster(ctx context.Context, params installer.V2RegisterClusterParams) middleware.Responder {
	c, err := b.RegisterClusterInternal(ctx, nil, params, false)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2RegisterClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2ListClusters(ctx context.Context, params installer.V2ListClustersParams) middleware.Responder {
	clusters, err := b.listClustersInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2ListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) V2GetCluster(ctx context.Context, params installer.V2GetClusterParams) middleware.Responder {
	c, err := b.GetClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2GetClusterOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2DeregisterCluster(ctx context.Context, params installer.V2DeregisterClusterParams) middleware.Responder {
	if err := b.DeregisterClusterInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2DeregisterClusterNoContent()
}

func (b *bareMetalInventory) V2GetClusterInstallConfig(ctx context.Context, params installer.V2GetClusterInstallConfigParams) middleware.Responder {
	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	cfg, err := b.installConfigBuilder.GetInstallConfig(c, false, "")
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewV2GetClusterInstallConfigOK().WithPayload(string(cfg))
}

func (b *bareMetalInventory) V2UpdateClusterInstallConfig(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) middleware.Responder {
	_, err := b.UpdateClusterInstallConfigInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateClusterInstallConfigCreated()
}
