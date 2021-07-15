package cluster

import (
	context "context"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/sirupsen/logrus"
)

func NewInstaller(log logrus.FieldLogger, db *gorm.DB, eventsHandler events.Handler) *installer {
	return &installer{
		log:           log,
		db:            db,
		eventsHandler: eventsHandler,
	}
}

type installer struct {
	log           logrus.FieldLogger
	db            *gorm.DB
	eventsHandler events.Handler
}

func (i *installer) GetMasterNodesIds(ctx context.Context, cluster *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return getKnownMastersNodesIds(cluster, db)
}
