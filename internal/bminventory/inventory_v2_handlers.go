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
