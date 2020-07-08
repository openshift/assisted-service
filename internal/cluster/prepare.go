package cluster

import (
	"context"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/jinzhu/gorm"
)

type prepare struct {
}

var _ StateAPI = (*prepare)(nil)

func NewPrepareForInstallation() *prepare {
	return &prepare{}
}

func (p *prepare) RefreshStatus(_ context.Context, _ *common.Cluster, _ *gorm.DB) (*UpdateReply, error) {
	// this is a temporary state monitoring is not relevant for this sate.
	return &UpdateReply{
		State:     models.ClusterStatusPreparingForInstallation,
		IsChanged: false,
	}, nil
}
