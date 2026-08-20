package versions

import (
	context "context"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen --build_flags=--mod=mod -package versions -destination mock_os_image_resolver.go -self_package github.com/openshift/assisted-service/internal/versions . OsImageResolver
type OsImageResolver interface {
	GetOsImageForRelease(releaseImage *models.ReleaseImage, cpuArchitecture, pullSecret string) (*models.OsImage, error)
	GetOsImageForVersion(ctx context.Context, version, cpuArchitecture, pullSecret string) (*models.OsImage, error)
	GetOsImageForInfraEnv(ctx context.Context, infraEnv *common.InfraEnv) (*models.OsImage, error)
}

type osImageResolver struct {
	log                logrus.FieldLogger
	release            oc.Release
	versionsHandler    Handler
	osImages           OSImages
	releaseImageMirror string
}

func NewOsImageResolver(
	log logrus.FieldLogger,
	release oc.Release,
	versionsHandler Handler,
	osImages OSImages,
	releaseImageMirror string,
) OsImageResolver {
	return &osImageResolver{
		log:                log,
		release:            release,
		versionsHandler:    versionsHandler,
		osImages:           osImages,
		releaseImageMirror: releaseImageMirror,
	}
}

func (r *osImageResolver) GetOsImageForRelease(releaseImage *models.ReleaseImage, cpuArchitecture, pullSecret string) (*models.OsImage, error) {
	if releaseImage == nil {
		return nil, errors.New("release image is nil")
	}

	cpuArchitecture = common.NormalizeCPUArchitecture(cpuArchitecture)
	if cpuArchitecture == "" {
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	openshiftVersion := swag.StringValue(releaseImage.OpenshiftVersion)
	releaseImageURL := swag.StringValue(releaseImage.URL)

	rhcosVersion, err := r.release.GetDefaultRhcosVersion(r.log, releaseImageURL, r.releaseImageMirror, pullSecret, cpuArchitecture)
	if err != nil {
		r.log.WithError(err).Debugf(
			"failed to get default RHCOS version from release image %s, falling back to OpenShift version %s",
			releaseImageURL, openshiftVersion,
		)
		return r.osImages.GetOsImageByOpenshiftVersion(openshiftVersion, cpuArchitecture)
	}

	osImage, err := r.osImages.GetOsImageByRhcosVersion(rhcosVersion, cpuArchitecture)
	if err != nil {
		r.log.WithError(err).Debugf(
			"failed to get OS image for RHCOS version %s and architecture %s, falling back to OpenShift version %s",
			rhcosVersion, cpuArchitecture, openshiftVersion,
		)
		return r.osImages.GetOsImageByOpenshiftVersion(openshiftVersion, cpuArchitecture)
	}

	return osImage, nil
}

func (r *osImageResolver) GetOsImageForVersion(ctx context.Context, version, cpuArchitecture, pullSecret string) (*models.OsImage, error) {
	cpuArchitecture = common.NormalizeCPUArchitecture(cpuArchitecture)
	if cpuArchitecture == "" {
		cpuArchitecture = common.DefaultCPUArchitecture
	}

	osImage, err := r.osImages.GetOsImageByRhcosVersion(version, cpuArchitecture)
	if err == nil {
		return osImage, nil
	}

	releaseImage, err := r.versionsHandler.GetReleaseImage(ctx, version, cpuArchitecture, pullSecret)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get release image for OpenShift version %s and architecture %s", version, cpuArchitecture)
	}

	return r.GetOsImageForRelease(releaseImage, cpuArchitecture, pullSecret)
}

func (r *osImageResolver) GetOsImageForInfraEnv(ctx context.Context, infraEnv *common.InfraEnv) (*models.OsImage, error) {
	return r.GetOsImageForVersion(ctx, infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture, infraEnv.PullSecret)
}
