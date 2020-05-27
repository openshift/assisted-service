package cluster

import (
	context "context"

	logutil "github.com/filanov/bm-inventory/pkg/log"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/go-openapi/strfmt"

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
	case clusterStatusReady:
		log.Infof("cluster %s is starting installation", c.ID)
	case clusterStatusInsufficient:
		masterKnownHosts, err := i.GetMasterNodesIds(ctx, c, db)
		if err != nil {
			return err
		}
		return errors.Errorf("cluster %s is expected to have exactly %d known master to be installed, got %d", c.ID, minHostsNeededForInstallation, len(masterKnownHosts))
	case clusterStatusInstalling:
		return errors.Errorf("cluster %s is already installing", c.ID)
	case clusterStatusInstalled:
		return errors.Errorf("cluster %s is already installed", c.ID)
	case clusterStatusError:
		return errors.Errorf("cluster %s has a error", c.ID)
	default:
		return errors.Errorf("cluster %s state is unclear - cluster state: %s", c.ID, swag.StringValue(c.Status))
	}

	_, err := updateState(clusterStatusInstalling, statusInfoInstalling, c, db, i.log)
	if err != nil {
		return err
	}

	return nil
}

func (i *installer) GetMasterNodesIds(ctx context.Context, cluster *common.Cluster, db *gorm.DB) ([]*strfmt.UUID, error) {
	return getKnownMastersNodesIds(cluster, db)
}
