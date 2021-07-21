package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/sirupsen/logrus"
)

type ntpSynchronizerCmd struct {
	baseCmd
	ntpSynchronizerImage string
	db                   *gorm.DB
}

func NewNtpSyncCmd(log logrus.FieldLogger, ntpSynchronizerImage string, db *gorm.DB) *ntpSynchronizerCmd {
	return &ntpSynchronizerCmd{
		baseCmd:              baseCmd{log: log},
		ntpSynchronizerImage: ntpSynchronizerImage,
		db:                   db,
	}
}

func (f *ntpSynchronizerCmd) prepareParam(host *models.Host, cluster *common.Cluster) (string, error) {
	request := models.NtpSynchronizationRequest{
		NtpSource: &cluster.AdditionalNtpSource,
	}
	b, err := json.Marshal(&request)
	if err != nil {
		f.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (f *ntpSynchronizerCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var cluster common.Cluster
	if err := f.db.Take(&cluster, "id = ?", host.ClusterID.String()).Error; err != nil {
		return nil, err
	}

	param, err := f.prepareParam(host, &cluster)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeNtpSynchronizer,
		Command:  "podman",
		Args: []string{
			"run", "--privileged", "--net=host", "--rm",
			"-v", "/var/log:/var/log",
			"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
			"-v", "/var/run/chrony:/var/run/chrony",
			f.ntpSynchronizerImage,
			"ntp_synchronizer",
			param,
		},
	}
	return []*models.Step{step}, nil
}
