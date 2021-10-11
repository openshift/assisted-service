package assistedserviceiso

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/imgexpirer"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/assisted_service_iso"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Config struct {
	ImageExpirationTime        time.Duration `envconfig:"IMAGE_EXPIRATION_TIME" default:"4h"`
	IgnitionConfigBaseFilename string        `envconfig:"IGNITION_CONFIG_BASE_FILENAME" default:"/data/onprem-iso-config.ign"`
	IPv6Support                bool          `envconfig:"IPV6_SUPPORT" default:"true"`
}

var _ restapi.AssistedServiceIsoAPI = &assistedServiceISOApi{}

type assistedServiceISOApi struct {
	objectHandler       s3wrapper.API
	authHandler         auth.Authenticator
	log                 logrus.FieldLogger
	pullSecretValidator validations.PullSecretValidator
	config              Config
}

func NewAssistedServiceISOApi(objectHandler s3wrapper.API, authHandler auth.Authenticator, log logrus.FieldLogger, pullSecretValidator validations.PullSecretValidator, config Config) *assistedServiceISOApi {
	return &assistedServiceISOApi{
		objectHandler:       objectHandler,
		authHandler:         authHandler,
		log:                 log,
		pullSecretValidator: pullSecretValidator,
		config:              config,
	}
}

/* CreateISOAndUploadToS3 Creates the ISO for the user and uploads it to S3. */
func (a *assistedServiceISOApi) CreateISOAndUploadToS3(ctx context.Context, params assisted_service_iso.CreateISOAndUploadToS3Params) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	sshPublicKeys := params.AssistedServiceIsoCreateParams.SSHPublicKey
	if sshPublicKeys != "" {
		if err := validations.ValidateSSHPublicKey(sshPublicKeys); err != nil {
			log.WithError(err).Errorf("Failed to validate SSH public key.")
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	pullSecret := params.AssistedServiceIsoCreateParams.PullSecret
	if pullSecret != "" {
		err := a.pullSecretValidator.ValidatePullSecret(pullSecret, ocm.UserNameFromContext(ctx), a.authHandler, a.config.IPv6Support)
		if err != nil {
			log.WithError(err).Errorf("Pull-secret for Assisted Service ISO has invalid format")
			return assisted_service_iso.NewCreateISOAndUploadToS3BadRequest().
				WithPayload(common.GenerateError(http.StatusBadRequest, err))
		}
	} else {
		log.Warn("Pull-secret for Assisted Service ISO must be provided")
		return assisted_service_iso.NewCreateISOAndUploadToS3BadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, errors.New("Pull-secret must be provided for Assisted Service ISO")))
	}

	data, err := ioutil.ReadFile(a.config.IgnitionConfigBaseFilename)
	if err != nil {
		log.WithError(err).Errorf("Error reading ignition config file")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	ignitionConfigSource := string(data)

	reIgnition := strings.NewReplacer("replace-with-your-ssh-public-key", ignition.QuoteSshPublicKeys(sshPublicKeys),
		"replace-with-your-urlencoded-pull-secret", url.PathEscape(pullSecret))

	ignitionConfig := reIgnition.Replace(ignitionConfigSource)

	username := ocm.UserNameFromContext(ctx)
	srcISOName, err := a.objectHandler.GetBaseIsoObject(params.AssistedServiceIsoCreateParams.OpenshiftVersion, common.DefaultCPUArchitecture)
	if err != nil {
		err = errors.Wrapf(err, "Failed to get source object name for ocp version %s", params.AssistedServiceIsoCreateParams.OpenshiftVersion)
		log.Error(err)
		return assisted_service_iso.NewCreateISOAndUploadToS3BadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	destISOName := fmt.Sprintf("%s%s", imgexpirer.AssistedServiceLiveISOPrefix, username)

	if err = a.objectHandler.UploadISO(ctx, ignitionConfig, srcISOName, destISOName); err != nil {
		log.WithError(err).Errorf("Failed to generate Assisted Service ISO")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return assisted_service_iso.NewCreateISOAndUploadToS3Created()
}

/* DownloadISO Downloads the ISO for the user from S3. */
func (a *assistedServiceISOApi) DownloadISO(ctx context.Context, params assisted_service_iso.DownloadISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)

	username := ocm.UserNameFromContext(ctx)
	isoName := fmt.Sprintf("%s%s.iso", imgexpirer.AssistedServiceLiveISOPrefix, username)

	reader, contentLength, err := a.objectHandler.Download(ctx, isoName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get Assisted Service ISO for user: %s", username)
		if strings.Contains(err.Error(), "NotFound") {
			return common.NewApiError(http.StatusNotFound, err)
		} else {
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	return filemiddleware.NewResponder(assisted_service_iso.NewDownloadISOOK().WithPayload(reader),
		isoName,
		contentLength)
}

func (a *assistedServiceISOApi) GetPresignedForAssistedServiceISO(ctx context.Context, params assisted_service_iso.GetPresignedForAssistedServiceISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, a.log)
	// Presigned URL only works with AWS S3 because Scality is not exposed
	if !a.objectHandler.IsAwsS3() {
		return common.NewApiError(http.StatusBadRequest, errors.New("Failed to generate presigned URL: invalid backend"))
	}

	username := ocm.UserNameFromContext(ctx)
	isoName := fmt.Sprintf("%s%s", imgexpirer.AssistedServiceLiveISOPrefix, username)
	isoNameWithExtension := fmt.Sprintf("%s%s", isoName, ".iso")

	url, err := a.objectHandler.GeneratePresignedDownloadURL(ctx, isoName, isoNameWithExtension, a.config.ImageExpirationTime)
	if err != nil {
		log.WithError(err).Errorf("failed to generate presigned URL for file: %s", isoNameWithExtension)
		if strings.Contains(err.Error(), "NotFound") {
			return common.NewApiError(http.StatusNotFound, err)
		} else {
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return assisted_service_iso.NewGetPresignedForAssistedServiceISOOK().WithPayload(&models.Presigned{URL: &url})
}
