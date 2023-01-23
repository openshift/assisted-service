package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type verifyVipsCmd struct {
	baseCmd
	db *gorm.DB
}

func newVerifyVipsCmd(log logrus.FieldLogger, db *gorm.DB) CommandGetter {
	return &verifyVipsCmd{
		baseCmd: baseCmd{log: log},
		db:      db,
	}
}

func (f *verifyVipsCmd) prepareParam(host *models.Host) (string, error) {
	var inventory models.Inventory
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		f.log.WithError(err).Warn("Inventory parse")
		return "", err
	}
	if host.ClusterID == nil || *host.ClusterID == "" {
		f.log.Warnf("Missing cluster id for host %s infra-env %s", host.ID.String(), host.InfraEnvID.String())
		return "", nil
	}
	cluster, err := common.GetClusterFromDBWithVips(f.db, *host.ClusterID)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get cluster %s", host.ClusterID.String())
	}
	if len(cluster.APIVips) == 0 && len(cluster.IngressVips) == 0 {
		return "", nil
	}
	request := models.VerifyVipsRequest{}
	for _, vip := range cluster.APIVips {
		request = append(request, &models.VerifyVip{
			Vip:     vip.IP,
			VipType: models.VipTypeAPI,
		})
	}

	for _, vip := range cluster.IngressVips {
		request = append(request, &models.VerifyVip{
			Vip:     vip.IP,
			VipType: models.VipTypeIngress,
		})
	}

	b, err := json.Marshal(&request)
	if err != nil {
		f.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (f *verifyVipsCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	param, err := f.prepareParam(host)
	if param == "" || err != nil {
		return nil, err
	}

	step := &models.Step{
		StepType: models.StepTypeVerifyVips,
		Args: []string{
			param,
		},
	}

	return []*models.Step{step}, nil
}
