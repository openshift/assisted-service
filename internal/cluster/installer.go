package cluster

import (
	context "context"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func NewInstaller(log logrus.FieldLogger, db *gorm.DB, eventsHandler eventsapi.Handler) *installer {
	return &installer{
		log:           log,
		db:            db,
		eventsHandler: eventsHandler,
	}
}

type installer struct {
	log           logrus.FieldLogger
	db            *gorm.DB
	eventsHandler eventsapi.Handler
}

func (i *installer) GetMasterNodesIds(ctx context.Context, cluster *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return getKnownMastersNodesIds(cluster, db)
}
