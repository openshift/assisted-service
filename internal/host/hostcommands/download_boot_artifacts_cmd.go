package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/imageservice"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type downloadBootArtifactsCmd struct {
	baseCmd
	imageServiceBaseURL string
	authType            auth.AuthType
	versionsHandler     versions.Handler
	db                  *gorm.DB
	imageDuration       time.Duration
	hostFSMountDir      string
}

func NewDownloadBootArtifactsCmd(log logrus.FieldLogger, imageServiceBaseUrl string, authType auth.AuthType,
	versionsHandler versions.Handler, db *gorm.DB, imageDuration time.Duration, hostFSMountDir string) *downloadBootArtifactsCmd {
	return &downloadBootArtifactsCmd{
		baseCmd:             baseCmd{log: log},
		imageServiceBaseURL: imageServiceBaseUrl,
		authType:            authType,
		db:                  db,
		imageDuration:       imageDuration,
		versionsHandler:     versionsHandler,
		hostFSMountDir:      hostFSMountDir,
	}
}

func (c *downloadBootArtifactsCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	var infraEnv *common.InfraEnv
	infraEnv, err := common.GetInfraEnvFromDB(c.db, host.InfraEnvID)
	if err != nil {
		return nil, err
	}
	osImage, err := c.versionsHandler.GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return nil, err
	}

	if osImage.OpenshiftVersion == nil {
		return nil, errors.Errorf("OS image entry '%+v' missing OpenshiftVersion field", osImage)
	}
	bootArtifactURLs, err := imageservice.GetBootArtifactURLs(c.imageServiceBaseURL, infraEnv.ID.String(), osImage, false)
	if err != nil {
		return nil, fmt.Errorf("failed to generate urls for DownloadBootArtifactsRequest: %w", err)
	}
	// Reclaiming a host is only used in the operator scenario (not SaaS) so other auth types don't need to be considered
	if c.authType == auth.TypeLocal {
		bootArtifactURLs.InitrdURL, err = gencrypto.SignURL(bootArtifactURLs.InitrdURL, infraEnv.ID.String(), gencrypto.InfraEnvKey)
		if err != nil {
			return nil, fmt.Errorf("failed to sign initrd url for DownloadBootArtifactsRequest: %w", err)
		}
	}
	request := models.DownloadBootArtifactsRequest{
		InitrdURL:      &bootArtifactURLs.InitrdURL,
		RootfsURL:      &bootArtifactURLs.RootFSURL,
		KernelURL:      &bootArtifactURLs.KernelURL,
		HostFsMountDir: &c.hostFSMountDir,
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DownloadBootArtifactsRequest: %w", err)
	}

	step := &models.Step{
		StepType: models.StepTypeDownloadBootArtifacts,
		Args: []string{
			string(requestBytes),
		},
	}
	return []*models.Step{step}, nil
}
