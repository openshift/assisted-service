package cluster

import (
	context "context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
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

func (i *installer) Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	log := logutil.FromContext(ctx, i.log)

	switch swag.StringValue(c.Status) {
	case "":
	case models.ClusterStatusPreparingForInstallation:
		log.Infof("cluster %s is starting installation", c.ID)
	case models.ClusterStatusInsufficient:
		masterKnownHosts, err := i.GetMasterNodesIds(ctx, c, db)
		if err != nil {
			return err
		}
		return errors.Errorf("cluster %s is expected to have exactly %d known master to be installed, got %d",
			c.ID, common.MinMasterHostsNeededForInstallation, len(masterKnownHosts))
	case models.ClusterStatusReady:
		return errors.Errorf("cluster %s is ready expected %s", c.ID, models.ClusterStatusPreparingForInstallation)
	case models.ClusterStatusInstalling:
		return errors.Errorf("cluster %s is already installing", c.ID)
	case models.ClusterStatusFinalizing:
		return errors.Errorf("cluster %s is already %s", c.ID, models.ClusterStatusFinalizing)
	case models.ClusterStatusInstalled:
		return errors.Errorf("cluster %s is already installed", c.ID)
	case models.ClusterStatusError:
		return errors.Errorf("cluster %s has a error", c.ID)
	default:
		return errors.Errorf("cluster %s state is unclear - cluster state: %s", c.ID, swag.StringValue(c.Status))
	}

	if _, err := updateClusterStatus(ctx, i.log, db, *c.ID, swag.StringValue(c.Status),
		models.ClusterStatusInstalling, statusInfoInstalling, i.eventsHandler); err != nil {
		return err
	}

	return nil
}

func (i *installer) GetMasterNodesIds(ctx context.Context, cluster *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return getKnownMastersNodesIds(cluster, db)
}
