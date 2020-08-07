package cluster

import (
	context "context"

	"github.com/openshift/assisted-service/models"

	logutil "github.com/openshift/assisted-service/pkg/log"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"

	"github.com/pkg/errors"

	"github.com/go-openapi/swag"

	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

func NewInstaller(log logrus.FieldLogger, db *gorm.DB) *installer {
	return &installer{
		log: log,
		db:  db,
	}
}

type installer struct {
	log logrus.FieldLogger
	db  *gorm.DB
}

func (i *installer) Install(ctx context.Context, c *common.Cluster, db *gorm.DB) error {
	log := logutil.FromContext(ctx, i.log)

	switch swag.StringValue(c.Status) {
	case "":
	case clusterStatusPrepareForInstallation:
		log.Infof("cluster %s is starting installation", c.ID)
	case clusterStatusInsufficient:
		masterKnownHosts, err := i.GetMasterNodesIds(ctx, c, db)
		if err != nil {
			return err
		}
		return errors.Errorf("cluster %s is expected to have exactly %d known master to be installed, got %d", c.ID, MinHostsNeededForInstallation, len(masterKnownHosts))
	case clusterStatusReady:
		return errors.Errorf("cluster %s is ready expected %s", c.ID, clusterStatusPrepareForInstallation)
	case clusterStatusInstalling:
		return errors.Errorf("cluster %s is already installing", c.ID)
	case models.ClusterStatusFinalizing:
		return errors.Errorf("cluster %s is already %s", c.ID, models.ClusterStatusFinalizing)
	case clusterStatusInstalled:
		return errors.Errorf("cluster %s is already installed", c.ID)
	case clusterStatusError:
		return errors.Errorf("cluster %s has a error", c.ID)
	default:
		return errors.Errorf("cluster %s state is unclear - cluster state: %s", c.ID, swag.StringValue(c.Status))
	}

	if _, err := updateClusterStatus(i.log, db, *c.ID, swag.StringValue(c.Status),
		clusterStatusInstalling, statusInfoInstalling); err != nil {
		return err
	}

	return nil
}

func (i *installer) GetMasterNodesIds(ctx context.Context, cluster *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return getKnownMastersNodesIds(cluster, db)
}
