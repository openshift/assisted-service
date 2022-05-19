package hostcommands

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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

func (f *ntpSynchronizerCmd) prepareParam(host *models.Host) (string, error) {
	ntpSources, err := common.GetHostNTPSources(f.db, host)
	if err != nil {
		return "", err
	}
	request := models.NtpSynchronizationRequest{
		NtpSource: swag.String(ntpSources),
	}
	b, err := json.Marshal(&request)
	if err != nil {
		f.log.WithError(err).Warn("Json marshal")
		return "", err
	}
	return string(b), nil
}

func (f *ntpSynchronizerCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	param, err := f.prepareParam(host)
	if err != nil {
		return nil, err
	}
	step := &models.Step{
		StepType: models.StepTypeNtpSynchronizer,
		Args: []string{
			param,
		},
	}
	return []*models.Step{step}, nil
}
