package bootfiles

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/bootfiles"
	"github.com/sirupsen/logrus"
)

var _ restapi.BootfilesAPI = &BootFiles{}

// NewManifestsAPI returns manifests API
func NewBootFilesAPI(log logrus.FieldLogger, objectHandler s3wrapper.API) *BootFiles {
	return &BootFiles{
		log:           log,
		objectHandler: objectHandler,
	}
}

// Manifests represents manifests handler implementation
type BootFiles struct {
	log           logrus.FieldLogger
	objectHandler s3wrapper.API
}

func (b *BootFiles) DownloadBootFiles(ctx context.Context, params operations.DownloadBootFilesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	srcObjectName := b.objectHandler.GetBaseIsoObject(params.OpenshiftVersion)

	// If we're working with AWS, redirect to download directly from there
	if b.objectHandler.IsAwsS3() {
		return operations.NewDownloadBootFilesTemporaryRedirect().WithLocation(b.objectHandler.GetS3BootFileURL(srcObjectName, params.FileType))
	}

	reader, objectName, contentLength, err := b.objectHandler.DownloadBootFile(ctx, srcObjectName, params.FileType)
	if err != nil {
		log.WithError(err).Errorf("Failed to get %s PXE artifact from object %s", params.FileType, srcObjectName)
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(operations.NewDownloadBootFilesOK().WithPayload(reader),
		objectName, contentLength)
}
