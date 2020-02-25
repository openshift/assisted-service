package bminventory

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/models"

	"github.com/go-openapi/runtime/middleware"

	"github.com/filanov/bm-inventory/restapi/operations/inventory"
)

const baseHref = "/api/bm.inventory/v1/node/"
const bareMetalType = "BareMetalNode"

type bareMetalInventory struct {
}

func NewBareMetalInventory() *bareMetalInventory {
	return &bareMetalInventory{}
}

func (b *bareMetalInventory) ListNodes(ctx context.Context, params inventory.ListNodesParams) middleware.Responder {
	return inventory.NewListNodesOK().WithPayload(models.Nodes{
		&models.RegisteredNode{

			Node: models.Node{
				Name: "example",
				URL:  "example.com",
			},
			Base: models.Base{
				Href: fmt.Sprintf("%s:%s", baseHref, "123"),
				ID:   "123",
				Kind: bareMetalType,
			},
		},
		&models.RegisteredNode{

			Node: models.Node{
				Name: "example",
				URL:  "example.com",
			},
			Base: models.Base{
				Href: fmt.Sprintf("%s:%s", baseHref, "456"),
				ID:   "456",
				Kind: bareMetalType,
			},
		},
	})
}

func (b *bareMetalInventory) RegisterNode(ctx context.Context, params inventory.RegisterNodeParams) middleware.Responder {
	return inventory.NewRegisterNodeCreated().WithPayload(&models.RegisteredNode{
		Node: models.Node{
			Name: "example",
			URL:  "example.com",
		},
		Base: models.Base{
			Href: fmt.Sprintf("%s:%s", baseHref, "123"),
			ID:   "123",
			Kind: bareMetalType,
		},
	})
}
