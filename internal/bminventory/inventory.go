package bminventory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi/operations/inventory"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const baseHref = "/api/bm-inventory/v1"

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

type Config struct {
	ImageBuilder    string `envconfig:"IMAGE_BUILDER" default:"quay.io/oscohen/installer-image-build"`
	ImageBuilderCmd string `envconfig:"IMAGE_BUILDER_CMD" default:"echo hello"`
	InventoryURL    string `envconfig:"INVENTORY_URL" default:"10.35.59.36"`
	InventoryPort   string `envconfig:"INVENTORY_PORT" default:"30485"`
	S3EndpointURL   string `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket        string `envconfig:"S3_BUCKET" default:"test"`
}

const ignitionConfigFormat = `{
"ignition": { "version": "3.0.0" },
  "passwd": {
    "users": [
      {
        "groups": [
          "sudo",
          "docker"
        ],
        "name": "core",
        "passwordHash": "$6$MWO4bibU8TIWG0XV$Hiuj40lWW7pHiwJmXA8MehuBhdxSswLgvGxEh8ByEzeX2D1dk87JILVUYS4JQOP45bxHRegAB9Fs/SWfszXa5."
      }
    ]
  },
"systemd": {
"units": [{
"name": "introspector.service",
"enabled": true,
"contents": "[Service]\nType=oneshot\nExecStart=docker run --rm --privileged --net=host quay.io/oamizur/introspector:latest /usr/bin/introspector --host %s --port %s\n\n[Install]\nWantedBy=multi-user.target"
}]
}
}`

type bareMetalInventory struct {
	Config
	imageBuildCmd []string
	db            *gorm.DB
	kube          client.Client
	debugCmdMap   sync.Map
}

func NewBareMetalInventory(db *gorm.DB, kclient client.Client, cfg Config) *bareMetalInventory {
	b := &bareMetalInventory{db: db, kube: kclient, Config: cfg}
	if cfg.ImageBuilderCmd != "" {
		b.imageBuildCmd = strings.Split(cfg.ImageBuilderCmd, " ")
	}
	return b
}

func strToURI(str string) *strfmt.URI {
	uri := strfmt.URI(str)
	return &uri
}

func buildHrefURI(base, id string) *strfmt.URI {
	return strToURI(fmt.Sprintf("%s/%ss/%s", baseHref, base, id))
}

func (b *bareMetalInventory) CreateImage(ctx context.Context, params inventory.CreateImageParams) middleware.Responder {
	id := strfmt.UUID(uuid.New().String())
	image := &models.Image{
		Base: models.Base{
			Href: buildHrefURI(ResourceKindImage, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindImage),
		},
		Status: swag.String(ImageStatusCreating),
	}

	if params.NewImageParams != nil {
		image.ImageCreateParams = *params.NewImageParams
	}

	logrus.Info("new image create request", image)
	if err := b.createImageJob(ctx, id.String()); err != nil {
		logrus.WithError(err).Error("failed to run job")
		return inventory.NewCreateImageInternalServerError()
	}

	if err := b.db.Create(image).Error; err != nil {
		logrus.WithError(err).Error("failed to create image")
		return inventory.NewCreateImageInternalServerError()
	}

	// TODO: should be async
	if err := b.monitorImageBuild(ctx, image); err != nil {
		return inventory.NewCreateImageInternalServerError()
	}
	return inventory.NewCreateImageCreated().WithPayload(image)
}

func (b *bareMetalInventory) monitorImageBuild(ctx context.Context, image *models.Image) error {
	var job batch.Job
	b.kube.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      fmt.Sprintf("create-image-%s", image.ID.String()),
	}, &job)

	for job.Status.Succeeded == 0 && job.Status.Failed == 0 {
		b.kube.Get(ctx, client.ObjectKey{
			Namespace: "default",
			Name:      fmt.Sprintf("create-image-%s", image.ID.String()),
		}, &job)
	}

	if job.Status.Failed > 0 {
		logrus.Error("job failed")
		return fmt.Errorf("job failed")
	}

	if err := b.kube.Delete(context.Background(), &job); err != nil {
		logrus.WithError(err).Error("failed to delete job")
	}

	// TODO: reuse target name
	// TODO: use standard path or net/url package
	image.DownloadURL = strfmt.URI(fmt.Sprintf("%s/%s/%s", b.S3EndpointURL, b.S3Bucket, fmt.Sprintf("installer-image-%s", image.ID)))
	image.Status = swag.String(ImageStatusReady)

	if err := b.db.Model(image).Updates(image).Where("id = ?", image.ID.String()).Error; err != nil {
		logrus.WithError(err).Error("failed to update image")
	}
	return nil
}

func (b *bareMetalInventory) createImageJob(ctx context.Context, id string) error {
	if err := b.kube.Create(ctx, &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      fmt.Sprintf("create-image-%s", id),
			Namespace: "default",
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      fmt.Sprintf("create-image-%s", id),
					Namespace: "default",
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Name:            "image-creator",
							Image:           b.Config.ImageBuilder,
							Command:         b.imageBuildCmd,
							ImagePullPolicy: "IfNotPresent",
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: b.S3EndpointURL,
								},
								{
									Name:  "IGNITION_CONFIG",
									Value: fmt.Sprintf(ignitionConfigFormat, b.InventoryURL, b.InventoryPort),
								},
								{
									Name:  "IMAGE_NAME",
									Value: fmt.Sprintf("installer-image-%s", id),
								},
								{
									Name:  "S3_BUCKET",
									Value: b.S3Bucket,
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}); err != nil {
		return err
	}
	return nil
}

func (b *bareMetalInventory) GetImage(ctx context.Context, params inventory.GetImageParams) middleware.Responder {
	var image models.Image
	if err := b.db.First(&image, "id = (?)", params.ImageID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to find image %s", params.ImageID)
		return inventory.NewGetImageNotFound()
	}
	return inventory.NewGetImageOK().WithPayload(&image)
}

func (b *bareMetalInventory) ListImages(ctx context.Context, params inventory.ListImagesParams) middleware.Responder {
	var images []*models.Image
	if err := b.db.Find(&images).Error; err != nil {
		return inventory.NewListImagesInternalServerError()
	}
	return inventory.NewListImagesOK().WithPayload(images)
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params inventory.RegisterClusterParams) middleware.Responder {
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

func (b *bareMetalInventory) DeregisterCluster(ctx context.Context, params inventory.DeregisterClusterParams) middleware.Responder {
	var cluster models.Cluster
	var txErr error
	tx := b.db.Begin()

	defer func() {
		if txErr != nil {
			tx.Rollback()
		}
	}()

	if err := tx.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
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

func (b *bareMetalInventory) ListClusters(ctx context.Context, params inventory.ListClustersParams) middleware.Responder {
	var clusters []*models.Cluster
	if err := b.db.Find(&clusters).Error; err != nil {
		logrus.WithError(err).Error("failed to list clusters")
		return inventory.NewListClustersInternalServerError()
	}

	return inventory.NewListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params inventory.GetClusterParams) middleware.Responder {
	var cluster models.Cluster
	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		// TODO: check for the right error
		return inventory.NewGetClusterNotFound()
	}
	return inventory.NewGetClusterOK().WithPayload(&cluster)
}

func (b *bareMetalInventory) RegisterNode(ctx context.Context, params inventory.RegisterNodeParams) middleware.Responder {
	node := &models.Node{
		Base: models.Base{
			Href: buildHrefURI(ResourceKindNode, params.NewNodeParams.NodeID.String()),
			ID:   params.NewNodeParams.NodeID,
			Kind: swag.String(ResourceKindNode),
		},
		Status: swag.String("discovering"),
	}

	node.NodeCreateParams = *params.NewNodeParams
	logrus.Infof(" register node: %+v", node)

	if err := b.db.Create(node).Error; err != nil {
		return inventory.NewRegisterClusterInternalServerError()
	}

	return inventory.NewRegisterNodeCreated().WithPayload(node)
}

func (b *bareMetalInventory) DeregisterNode(ctx context.Context, params inventory.DeregisterNodeParams) middleware.Responder {
	var node models.Node
	if err := b.db.Delete(&node, "id = ?", params.NodeID).Error; err != nil {
		// TODO: check error type
		return inventory.NewDeregisterNodeBadRequest()
	}

	// TODO: need to check that node can be deleted from the cluster
	return inventory.NewDeregisterNodeNoContent()
}

func (b *bareMetalInventory) GetNode(ctx context.Context, params inventory.GetNodeParams) middleware.Responder {
	var node models.Node

	// TODO: validate what is the error
	if err := b.db.First(&node, "id = ?", params.NodeID).Error; err != nil {
		return inventory.NewGetNodeNotFound()
	}

	return inventory.NewGetNodeOK().WithPayload(&node)
}

func (b *bareMetalInventory) ListNodes(ctx context.Context, params inventory.ListNodesParams) middleware.Responder {
	var nodes []*models.Node
	if err := b.db.Find(&nodes).Error; err != nil {
		return inventory.NewListNodesInternalServerError()
	}
	return inventory.NewListNodesOK().WithPayload(nodes)
}

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params inventory.GetNextStepsParams) middleware.Responder {
	steps := models.Steps{}
	if cmd, ok := b.debugCmdMap.Load(params.NodeID); ok {
		step := &models.Step{}
		step.StepType = models.StepTypeDebug
		step.Data = cmd.(string)
		steps = append(steps, step)
		defer func() { b.debugCmdMap.Delete(params.NodeID) }()
	}

	steps = append(steps, &models.Step{
		StepType: models.StepTypeHardawareInfo,
	})

	return inventory.NewGetNextStepsOK().WithPayload(steps)
}

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params inventory.PostStepReplyParams) middleware.Responder {
	return inventory.NewPostStepReplyNoContent()
}

func (b *bareMetalInventory) SetDebugStep(ctx context.Context, params inventory.SetDebugStepParams) middleware.Responder {
	b.debugCmdMap.Store(params.NodeID, swag.StringValue(params.Step.Command))
	return inventory.NewSetDebugStepOK()
}

func (b *bareMetalInventory) DisableNode(ctx context.Context, params inventory.DisableNodeParams) middleware.Responder {
	return inventory.NewDisableNodeNoContent()
}

func (b *bareMetalInventory) EnableNode(ctx context.Context, params inventory.EnableNodeParams) middleware.Responder {
	return inventory.NewEnableNodeNoContent()
}
