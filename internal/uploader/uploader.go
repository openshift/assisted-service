package uploader

import (
	"context"

	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

//go:generate mockgen -source=uploader.go -package=uploader -destination=mock_uploader.go
type Client interface {
	UploadEvents(ctx context.Context, cluster *common.Cluster, eventsHandler eventsapi.Handler) error
	IsEnabled() bool
}

type Config struct {
	Versions               versions.Versions
	DataUploadEndpoint     string `envconfig:"DATA_UPLOAD_ENDPOINT" default:"https://console.redhat.com/api/ingress/v1/upload"`
	DeploymentType         string `envconfig:"DEPLOYMENT_TYPE" default:""`
	DeploymentVersion      string `envconfig:"DEPLOYMENT_VERSION" default:""`
	AssistedServiceVersion string
	EnableDataCollection   bool `envconfig:"ENABLE_DATA_COLLECTION" default:"true"`
}

func NewClient(cfg *Config, db *gorm.DB, log logrus.FieldLogger, client k8sclient.K8SClient) Client {
	return &eventsUploader{
		db:     db,
		log:    log,
		client: client,
		Config: *cfg,
	}
}
