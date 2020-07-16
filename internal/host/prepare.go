package host

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/internal/common"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

type prepare baseState

var _ StateAPI = &prepare{}

func NewPrepareState(log logrus.FieldLogger) *prepare {
	return &prepare{
		log: log,
	}
}

func (p *prepare) RefreshStatus(ctx context.Context, h *models.Host, db *gorm.DB) (*models.Host, error) {
	c := common.Cluster{}
	if err := db.Take(&c, "id = ?", h.ClusterID.String()).Error; err != nil {
		return nil, err
	}
	if swag.StringValue(c.Status) != models.ClusterStatusPreparingForInstallation {
		return updateHostStatus(logutil.FromContext(ctx, p.log), db, h.ClusterID, *h.ID, *h.Status,
			models.HostStatusError, fmt.Sprintf("Cluster is not longer is not longer %s", models.ClusterStatusPreparingForInstallation))
	}

	return h, nil
}
