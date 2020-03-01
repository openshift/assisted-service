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

const (
	ImageStatusCreating = "creating"
	ImageStatusReady    = "ready"
	ImageStatusError    = "error"
)

const (
	ClusterStatusCreating = "creating"
	ClusterStatusReady    = "ready"
	ClusterStatusError    = "error"
)

const (
	ResourceKindImage   = "image"
	ResourceKindNode    = "node"
	ResourceKindCluster = "cluster"
)

type bareMetalInventory struct {
	db *gorm.DB
}

func NewBareMetalInventory(db *gorm.DB) *bareMetalInventory {
	return &bareMetalInventory{db: db}
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
			Href: buildHrefURI(ResourceKindImage, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindCluster),
		},
		Status: swag.String(ImageStatusCreating),
	}

	if params.NewImageParams != nil {
		image.ImageCreateParams = *params.NewImageParams
	}

	logrus.Info("new image create request", image)

	if err := b.db.Create(image).Error; err != nil {
		logrus.WithError(err).Error("failed to create image")
		return inventory.NewCreateImageInternalServerError()
	}

	return inventory.NewCreateImageCreated().WithPayload(image)
}

func (b bareMetalInventory) GetImage(ctx context.Context, params inventory.GetImageParams) middleware.Responder {
	var image models.Image
	if err := b.db.First(&image, "id = (?)", params.ImageID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to find image %s", params.ImageID)
		return inventory.NewGetImageNotFound()
	}
	return inventory.NewGetImageOK().WithPayload(&image)
}

func (b bareMetalInventory) ListImages(ctx context.Context, params inventory.ListImagesParams) middleware.Responder {
	var images []*models.Image
	if err := b.db.Find(&images).Error; err != nil {
		return inventory.NewListImagesInternalServerError()
	}
	return inventory.NewListImagesOK().WithPayload(images)
}

func (b bareMetalInventory) RegisterCluster(ctx context.Context, params inventory.RegisterClusterParams) middleware.Responder {
	logrus.Info("Register cluster:", params.NewClusterParams)
	id := strfmt.UUID(uuid.New().String())
	cluster := models.Cluster{
		Base: models.Base{
			Href: buildHrefURI(ResourceKindCluster, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindCluster),
		},
		Namespace: nil, // TODO: get namespace from the nodes
		Status:    swag.String(ClusterStatusReady),
	}
	// TODO: validate that we 3 master nodes and that they are from the same namespace
	if params.NewClusterParams != nil {
		cluster.ClusterCreateParams = *params.NewClusterParams
	}

	if err := b.db.Create(&cluster).Error; err != nil {
		return inventory.NewRegisterClusterInternalServerError()
	}
	return inventory.NewRegisterClusterCreated().WithPayload(&cluster)
}

func (b bareMetalInventory) DeregisterCluster(ctx context.Context, params inventory.DeregisterClusterParams) middleware.Responder {
	var cluster models.Cluster
	var txErr error
	tx := b.db.Begin()

	defer func() {
		if txErr != nil {
			tx.Rollback()
		}
	}()

	if err := tx.First(&cluster, "id = ?", params.ClusterID); err != nil {
		return inventory.NewDeregisterClusterNotFound()
	}

	for i := range cluster.Nodes {
		if txErr = tx.Delete(&models.Node{}, "id = ?", cluster.Nodes[i].ID).Error; txErr != nil {
			logrus.WithError(txErr).Errorf("failed to delete node %s", cluster.Nodes[i].ID)
			// TODO: fix error code
			return inventory.NewDeregisterClusterNotFound()
		}
	}
	if txErr = tx.Delete(cluster).Error; txErr != nil {
		logrus.WithError(txErr).Errorf("failed to delete cluster %s", cluster.ID)
		// TODO: fix error code
		return inventory.NewDeregisterClusterNotFound()
	}

	if txErr = tx.Commit().Error; txErr != nil {
		logrus.WithError(txErr).Errorf("failed to delete cluster %s, commit tx", cluster.ID)
		// TODO: fix error code
		return inventory.NewDeregisterClusterNotFound()
	}

	return inventory.NewDeregisterClusterNoContent()
}

func (b bareMetalInventory) ListClusters(ctx context.Context, params inventory.ListClustersParams) middleware.Responder {
	var clusters []*models.Cluster
	if err := b.db.Find(&clusters).Error; err != nil {
		logrus.WithError(err).Error("failed to list clusters")
		return inventory.NewListClustersInternalServerError()
	}

	return inventory.NewListClustersOK().WithPayload(clusters)
}

func (b bareMetalInventory) GetCluster(ctx context.Context, params inventory.GetClusterParams) middleware.Responder {
	var cluster *models.Cluster
	if err := b.db.First(cluster, "id = ?", params.ClusterID); err != nil {
		// TODO: check for the right error
		return inventory.NewGetClusterNotFound()
	}
	return inventory.NewGetClusterOK().WithPayload(cluster)
}

func (b bareMetalInventory) RegisterNode(ctx context.Context, params inventory.RegisterNodeParams) middleware.Responder {
	id := strfmt.UUID(uuid.New().String())
	node := &models.Node{
		Base: models.Base{
			Href: buildHrefURI(ResourceKindNode, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindNode),
		},
		Status: nil, // TODO: TBD
	}

	node.NodeCreateParams = *params.NewNodeParams
	logrus.Infof(" register node: %+v", node)

	if err := b.db.Create(node).Error; err != nil {
		return inventory.NewRegisterClusterInternalServerError()
	}

	return inventory.NewRegisterNodeCreated().WithPayload(node)
}

func (b bareMetalInventory) DeregisterNode(ctx context.Context, params inventory.DeregisterNodeParams) middleware.Responder {
	var node models.Node
	if err := b.db.Delete(&node, "id = ?", params.NodeID); err != nil {
		// TODO: check error type
		return inventory.NewDeregisterNodeBadRequest()
	}

	// TODO: need to check that node can be deleted from the cluster
	return inventory.NewDeregisterNodeNoContent()
}

func (b bareMetalInventory) GetNode(ctx context.Context, params inventory.GetNodeParams) middleware.Responder {
	var node *models.Node

	// TODO: validate what is the error
	if err := b.db.First(node, "id = ?", params.NodeID); err != nil {
		return inventory.NewGetNodeNotFound()
	}

	return inventory.NewGetNodeOK().WithPayload(node)
}

func (b bareMetalInventory) ListNodes(ctx context.Context, params inventory.ListNodesParams) middleware.Responder {
	var nodes []*models.Node
	if err := b.db.Find(&nodes).Error; err != nil {
		return inventory.NewListNodesInternalServerError()
	}
	return inventory.NewListNodesOK().WithPayload(nodes)
}
