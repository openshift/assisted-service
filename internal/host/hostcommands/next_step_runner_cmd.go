package hostcommands

import (
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	log "github.com/sirupsen/logrus"
)

type NextStepRunnerConfig struct {
	ServiceBaseURL       string
	InfraEnvID           strfmt.UUID
	HostID               strfmt.UUID
	UseCustomCACert      bool
	NextStepRunnerImage  string
	SkipCertVerification bool
}

func GetNextStepRunnerCommand(config *NextStepRunnerConfig) (string, *[]string, error) {

	request := models.NextStepCmdRequest{
		InfraEnvID:   &config.InfraEnvID,
		HostID:       &config.HostID,
		AgentVersion: swag.String(config.NextStepRunnerImage),
	}

	b, err := json.Marshal(&request)
	if err != nil {
		log.WithError(err).Warn("Json marshal")
		return "", nil, err
	}
	arguments := []string{string(b)}

	return "", &arguments, nil
}
