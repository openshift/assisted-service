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
