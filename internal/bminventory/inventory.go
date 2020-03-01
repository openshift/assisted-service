package bminventory

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/google/uuid"

	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi/operations/inventory"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
)

const baseHref = "/api/bm.inventory/v1"
const image = "image"
const bareMetalType = "BareMetalNode"

const (
	ImageStatusCreating = "creating"
	ImageStatusReady    = "ready"
	ImageStatusError    = "error"
)

const (
	ResourceKindImage   = "image"
	ResourceKindNode    = "node"
	ResourceKindCluster = "cluster"
)

type bareMetalInventory struct {
	db *gorm.DB
}

func NewBareMetalInventory() *bareMetalInventory {
	return &bareMetalInventory{}
}

func strToURI(str string) *strfmt.URI {
	uri := strfmt.URI(str)
	return &uri
}

func buildHrefURI(base, id string) *strfmt.URI {
	return strToURI(fmt.Sprintf("%s/%s/%s", baseHref, base, id))
}

func (b bareMetalInventory) CreateImage(ctx context.Context, params inventory.CreateImageParams) middleware.Responder {
	id := strfmt.UUID(uuid.New().String())
	image := &models.Image{
		Base: models.Base{
			Href: buildHrefURI(image, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindCluster),
		},
		Status: swag.String(ImageStatusCreating),
	}

	if params.NewImageParams != nil {
		image.ImageCreateParams = *params.NewImageParams
	}

	if err := b.db.Create(image).Error; err != nil {
		logrus.WithError(err).Error("failed to create image")
		return inventory.NewCreateImageInternalServerError()
	}

	return inventory.NewCreateImageCreated().WithPayload(image)
}

func (b bareMetalInventory) GetImage(ctx context.Context, params inventory.GetImageParams) middleware.Responder {
	var image *models.Image
	if err := b.db.First(image, "id = ? ", params.ImageID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to find image %s", params.ImageID)
		return inventory.NewGetImageNotFound()
	}
	return inventory.NewGetImageOK().WithPayload(image)
}

func (b bareMetalInventory) ListImages(ctx context.Context, params inventory.ListImagesParams) middleware.Responder {
	var images []*models.Image
	if err := b.db.Find(images).Error; err != nil {
		return inventory.NewListImagesInternalServerError()
	}
	return inventory.NewListImagesOK().WithPayload(images)
}

func (b bareMetalInventory) DeregisterCluster(ctx context.Context, params inventory.DeregisterClusterParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) DeregisterNode(ctx context.Context, params inventory.DeregisterNodeParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) GetCluster(ctx context.Context, params inventory.GetClusterParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) GetNode(ctx context.Context, params inventory.GetNodeParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) ListClusters(ctx context.Context, params inventory.ListClustersParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) ListNodes(ctx context.Context, params inventory.ListNodesParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) RegisterCluster(ctx context.Context, params inventory.RegisterClusterParams) middleware.Responder {
	panic("implement me")
}

func (b bareMetalInventory) RegisterNode(ctx context.Context, params inventory.RegisterNodeParams) middleware.Responder {
	panic("implement me")
}
