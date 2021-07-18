package hostcommands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alessio/shellescape"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

type imageAvailabilityCmd struct {
	baseCmd
	db                *gorm.DB
	ocRelease         oc.Release
	versionsHandler   versions.Handler
	instructionConfig InstructionConfig
	timeoutSeconds    float64
}

type Images struct {
	ReleaseImage    string
	MCOImage        string
	MustGatherImage string
}

func NewImageAvailabilityCmd(log logrus.FieldLogger, db *gorm.DB, ocRelease oc.Release, versionsHandler versions.Handler,
	instructionConfig InstructionConfig, timeoutSeconds float64) *imageAvailabilityCmd {
	return &imageAvailabilityCmd{
		baseCmd:           baseCmd{log: log},
		db:                db,
		instructionConfig: instructionConfig,
		ocRelease:         ocRelease,
		versionsHandler:   versionsHandler,
		timeoutSeconds:    timeoutSeconds,
	}
}

func (cmd *imageAvailabilityCmd) getImages(cluster *common.Cluster) (Images, error) {

	images := Images{}
	releaseImage, err := cmd.versionsHandler.GetReleaseImage(cluster.OpenshiftVersion)
	if err != nil {
		return images, err
	}
	images.ReleaseImage = releaseImage

	mcoImage, err := cmd.ocRelease.GetMCOImage(cmd.log, releaseImage, cmd.instructionConfig.ReleaseImageMirror, cluster.PullSecret)
	if err != nil {
		return images, err
	}
	images.MCOImage = mcoImage

	mustGatherImage, err := cmd.ocRelease.GetMustGatherImage(cmd.log, releaseImage, cmd.instructionConfig.ReleaseImageMirror, cluster.PullSecret)
	if err != nil {
		return images, err
	}
	images.MustGatherImage = mustGatherImage
	return images, nil
}

func (cmd *imageAvailabilityCmd) prepareParam(host *models.Host) (string, error) {
	var cluster common.Cluster
	if err := cmd.db.First(&cluster, "id = ?", host.ClusterID).Error; err != nil {
		cmd.log.Errorf("failed to get cluster %s", host.ClusterID)
		return "", err
	}

	images, err := cmd.getImages(&cluster)
	if err != nil {
		return "", err
	}

	imagesToCheck, err := cmd.filterImagesWithStatus(host, cmd.instructionConfig.InstallerImage, images.MCOImage, images.MustGatherImage)
	if err != nil {
		return "", err
	}

	if len(imagesToCheck) == 0 {
		return "", nil
	}

	request := models.ContainerImageAvailabilityRequest{
		Images:  imagesToCheck,
		Timeout: int64(cmd.timeoutSeconds),
	}

	b, err := json.Marshal(&request)
	if err != nil {
		cmd.log.WithError(err).Errorf("Failed to JSON marshal %+v", request)
		return "", err
	}

	return string(b), nil
}

func (cmd *imageAvailabilityCmd) filterImagesWithStatus(host *models.Host, candidates ...string) ([]string, error) {
	imagesStatus, err := common.UnmarshalImageStatuses(host.ImagesStatus)
	if err != nil {
		return nil, err
	}
	ret := make([]string, 0)
	for _, c := range candidates {
		if !common.ImageStatusExists(imagesStatus, c) {
			ret = append(ret, c)
		}
	}
	return ret, nil
}

func (cmd *imageAvailabilityCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {
	param, err := cmd.prepareParam(host)
	if err != nil {
		return nil, err
	}

	if param == "" {
		return nil, nil
	}

	const containerName = "container_image_availability"

	podmanRunCmd := shellescape.QuoteCommand([]string{
		"podman", "run", "--privileged", "--net=host", "--rm", "--quiet", "--pid=host",
		"--name", containerName,
		"-v", "/var/log:/var/log",
		"-v", "/run/systemd/journal/socket:/run/systemd/journal/socket",
		cmd.instructionConfig.AgentImage,
		"container_image_availability",
		"--request", param,
	})

	// checking if it exists and only running if it doesn't
	checkAlreadyRunningCmd := fmt.Sprintf("podman ps --format '{{.Names}}' | grep -q '^%s$'", containerName)

	step := &models.Step{
		StepType: models.StepTypeContainerImageAvailability,
		Command:  "sh",
		Args: []string{
			"-c",
			fmt.Sprintf("%s || %s", checkAlreadyRunningCmd, podmanRunCmd),
		},
	}

	return []*models.Step{step}, nil
}
