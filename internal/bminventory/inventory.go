package bminventory

import (
	"bytes"
	"context"

	// #nosec
	"crypto/md5"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/hashicorp/go-version"
	"github.com/kennygrant/sanitize"
	clusterPkg "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/identity"
	"github.com/openshift/assisted-service/internal/ignition"
	installcfg "github.com/openshift/assisted-service/internal/installcfg/builder"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	ctxparams "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/pkg/transaction"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
)

const DefaultUser = "kubeadmin"

const WindowBetweenRequestsInSeconds = 10 * time.Second
const mediaDisconnectionMessage = "Cannot read from the media (ISO) - media was likely disconnected"

const (
	MediaDisconnected int64 = 256
	// 125 is the generic exit code for cases the error is in podman / docker and not the container we tried to run
	ContainerAlreadyRunningExitCode = 125
)

type Config struct {
	ignition.IgnitionConfig
	AgentDockerImg                  string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	ServiceBaseURL                  string            `envconfig:"SERVICE_BASE_URL"`
	ImageServiceBaseURL             string            `envconfig:"IMAGE_SERVICE_BASE_URL"`
	ServiceCACertPath               string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	S3EndpointURL                   string            `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket                        string            `envconfig:"S3_BUCKET" default:"test"`
	ImageExpirationTime             time.Duration     `envconfig:"IMAGE_EXPIRATION_TIME" default:"4h"`
	AwsAccessKeyID                  string            `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey              string            `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	BaseDNSDomains                  map[string]string `envconfig:"BASE_DNS_DOMAINS" default:""`
	SkipCertVerification            bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	InstallRHCa                     bool              `envconfig:"INSTALL_RH_CA" default:"false"`
	RhQaRegCred                     string            `envconfig:"REGISTRY_CREDS" default:""`
	AgentTimeoutStart               time.Duration     `envconfig:"AGENT_TIMEOUT_START" default:"3m"`
	ServiceIPs                      string            `envconfig:"SERVICE_IPS" default:""`
	DefaultNTPSource                string            `envconfig:"NTP_DEFAULT_SERVER"`
	ISOCacheDir                     string            `envconfig:"ISO_CACHE_DIR" default:"/tmp/isocache"`
	DefaultClusterNetworkCidr       string            `envconfig:"CLUSTER_NETWORK_CIDR" default:"10.128.0.0/14"`
	DefaultClusterNetworkHostPrefix int64             `envconfig:"CLUSTER_NETWORK_HOST_PREFIX" default:"23"`
	DefaultServiceNetworkCidr       string            `envconfig:"SERVICE_NETWORK_CIDR" default:"172.30.0.0/16"`
	ISOImageType                    string            `envconfig:"ISO_IMAGE_TYPE" default:"full-iso"`
	IPv6Support                     bool              `envconfig:"IPV6_SUPPORT" default:"true"`
	DiskEncryptionSupport           bool              `envconfig:"DISK_ENCRYPTION_SUPPORT" default:"true"`
}

const minimalOpenShiftVersionForSingleNode = "4.8.0-0.0"

type Interactivity bool

const (
	Interactive    Interactivity = true
	NonInteractive Interactivity = false
)

type OCPClusterAPI interface {
	RegisterOCPCluster(ctx context.Context) error
}

//go:generate mockgen -package bminventory -destination mock_installer_internal.go . InstallerInternals
type InstallerInternals interface {
	RegisterClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.V2RegisterClusterParams, v1Flag common.InfraEnvCreateFlag) (*common.Cluster, error)
	GetClusterInternal(ctx context.Context, params installer.V2GetClusterParams) (*common.Cluster, error)
	UpdateClusterNonInteractive(ctx context.Context, params installer.V2UpdateClusterParams) (*common.Cluster, error)
	GenerateClusterISOInternal(ctx context.Context, params installer.GenerateClusterISOParams) (*common.Cluster, error)
	UpdateDiscoveryIgnitionInternal(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) error
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	GetHostByKubeKey(key types.NamespacedName) (*common.Host, error)
	InstallClusterInternal(ctx context.Context, params installer.V2InstallClusterParams) (*common.Cluster, error)
	DeregisterClusterInternal(ctx context.Context, params installer.V2DeregisterClusterParams) error
	DeregisterHostInternal(ctx context.Context, params installer.DeregisterHostParams) error
	V2DeregisterHostInternal(ctx context.Context, params installer.V2DeregisterHostParams) error
	GetCommonHostInternal(ctx context.Context, infraEnvId string, hostId string) (*common.Host, error)
	UpdateHostApprovedInternal(ctx context.Context, infraEnvId string, hostId string, approved bool) error
	UpdateHostInstallerArgsInternal(ctx context.Context, params installer.UpdateHostInstallerArgsParams) (*models.Host, error)
	V2UpdateHostInstallerArgsInternal(ctx context.Context, params installer.V2UpdateHostInstallerArgsParams) (*models.Host, error)
	UpdateHostIgnitionInternal(ctx context.Context, params installer.UpdateHostIgnitionParams) (*models.Host, error)
	V2UpdateHostIgnitionInternal(ctx context.Context, params installer.V2UpdateHostIgnitionParams) (*models.Host, error)
	GetCredentialsInternal(ctx context.Context, params installer.V2GetCredentialsParams) (*models.Credentials, error)
	DownloadClusterFilesInternal(ctx context.Context, params installer.DownloadClusterFilesParams) (io.ReadCloser, int64, error)
	V2DownloadClusterFilesInternal(ctx context.Context, params installer.V2DownloadClusterFilesParams) (io.ReadCloser, int64, error)
	V2DownloadClusterCredentialsInternal(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) (io.ReadCloser, int64, error)
	V2ImportClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, id *strfmt.UUID, params installer.V2ImportClusterParams, v1Flag common.InfraEnvCreateFlag) (*common.Cluster, error)
	InstallSingleDay2HostInternal(ctx context.Context, clusterId strfmt.UUID, infraEnvId strfmt.UUID, hostId strfmt.UUID) error
	UpdateClusterInstallConfigInternal(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) (*common.Cluster, error)
	CancelInstallationInternal(ctx context.Context, params installer.V2CancelInstallationParams) (*common.Cluster, error)
	TransformClusterToDay2Internal(ctx context.Context, clusterID strfmt.UUID) (*common.Cluster, error)
	AddReleaseImage(ctx context.Context, releaseImageUrl, pullSecret string) (*models.ReleaseImage, error)
	GetClusterSupportedPlatformsInternal(ctx context.Context, params installer.GetClusterSupportedPlatformsParams) (*[]models.PlatformType, error)
	V2UpdateHostInternal(ctx context.Context, params installer.V2UpdateHostParams) (*common.Host, error)
	GetInfraEnvByKubeKey(key types.NamespacedName) (*common.InfraEnv, error)
	UpdateInfraEnvInternal(ctx context.Context, params installer.UpdateInfraEnvParams) (*common.InfraEnv, error)
	RegisterInfraEnvInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterInfraEnvParams) (*common.InfraEnv, error)
	DeregisterInfraEnvInternal(ctx context.Context, params installer.DeregisterInfraEnvParams) error
	UnbindHostInternal(ctx context.Context, params installer.UnbindHostParams) (*common.Host, error)
	BindHostInternal(ctx context.Context, params installer.BindHostParams) (*common.Host, error)
	GetInfraEnvHostsInternal(ctx context.Context, infraEnvId strfmt.UUID) ([]*common.Host, error)
}

//go:generate mockgen -package bminventory -destination mock_crd_utils.go . CRDUtils
type CRDUtils interface {
	CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId string, infraenv *common.InfraEnv, cluster *common.Cluster) error
}
type bareMetalInventory struct {
	Config
	db                   *gorm.DB
	log                  logrus.FieldLogger
	hostApi              host.API
	clusterApi           clusterPkg.API
	dnsApi               dns.DNSApi
	eventsHandler        eventsapi.Handler
	objectHandler        s3wrapper.API
	metricApi            metrics.API
	usageApi             usage.API
	operatorManagerApi   operators.API
	generator            generator.ISOInstallConfigGenerator
	authHandler          auth.Authenticator
	k8sClient            k8sclient.K8SClient
	ocmClient            *ocm.Client
	leaderElector        leader.Leader
	secretValidator      validations.PullSecretValidator
	versionsHandler      versions.Handler
	isoEditorFactory     isoeditor.Factory
	crdUtils             CRDUtils
	IgnitionBuilder      ignition.IgnitionBuilder
	hwValidator          hardware.Validator
	installConfigBuilder installcfg.InstallConfigBuilder
	staticNetworkConfig  staticnetworkconfig.StaticNetworkConfig
	gcConfig             garbagecollector.Config
	providerRegistry     registry.ProviderRegistry
}

func NewBareMetalInventory(
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi clusterPkg.API,
	cfg Config,
	generator generator.ISOInstallConfigGenerator,
	eventsHandler eventsapi.Handler,
	objectHandler s3wrapper.API,
	metricApi metrics.API,
	usageApi usage.API,
	operatorManagerApi operators.API,
	authHandler auth.Authenticator,
	k8sClient k8sclient.K8SClient,
	ocmClient *ocm.Client,
	leaderElector leader.Leader,
	pullSecretValidator validations.PullSecretValidator,
	versionsHandler versions.Handler,
	isoEditorFactory isoeditor.Factory,
	crdUtils CRDUtils,
	IgnitionBuilder ignition.IgnitionBuilder,
	hwValidator hardware.Validator,
	dnsApi dns.DNSApi,
	installConfigBuilder installcfg.InstallConfigBuilder,
	staticNetworkConfig staticnetworkconfig.StaticNetworkConfig,
	gcConfig garbagecollector.Config,
	providerRegistry registry.ProviderRegistry,
) *bareMetalInventory {
	return &bareMetalInventory{
		db:                   db,
		log:                  log,
		Config:               cfg,
		hostApi:              hostApi,
		clusterApi:           clusterApi,
		dnsApi:               dnsApi,
		generator:            generator,
		eventsHandler:        eventsHandler,
		objectHandler:        objectHandler,
		metricApi:            metricApi,
		usageApi:             usageApi,
		operatorManagerApi:   operatorManagerApi,
		authHandler:          authHandler,
		k8sClient:            k8sClient,
		ocmClient:            ocmClient,
		leaderElector:        leaderElector,
		secretValidator:      pullSecretValidator,
		versionsHandler:      versionsHandler,
		isoEditorFactory:     isoEditorFactory,
		crdUtils:             crdUtils,
		IgnitionBuilder:      IgnitionBuilder,
		hwValidator:          hwValidator,
		installConfigBuilder: installConfigBuilder,
		staticNetworkConfig:  staticNetworkConfig,
		gcConfig:             gcConfig,
		providerRegistry:     providerRegistry,
	}
}

func (b *bareMetalInventory) updatePullSecret(pullSecret string, log logrus.FieldLogger) (string, error) {
	if b.Config.RhQaRegCred != "" {
		ps, err := validations.AddRHRegPullSecret(pullSecret, b.Config.RhQaRegCred)
		if err != nil {
			log.Errorf("Failed to add RH QA Credentials to Pull Secret: %s", err.Error())
			return "", errors.Errorf("Failed to add RH QA Credentials to Pull Secret: %s", err.Error())
		}
		return ps, nil
	}
	return pullSecret, nil
}

func (b *bareMetalInventory) GetDiscoveryIgnition(ctx context.Context, params installer.GetDiscoveryIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	cluster, err := common.GetClusterFromDB(b.db, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return common.GenerateErrorResponder(err)
	}

	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.ClusterID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra-env: %s", params.ClusterID)
		return common.GenerateErrorResponder(err)
	}

	// update pull secret and proxy, since they could have been set during cluster update
	proxy := models.Proxy{
		HTTPProxy:  swag.String(cluster.HTTPProxy),
		HTTPSProxy: swag.String(cluster.HTTPSProxy),
		NoProxy:    swag.String(cluster.NoProxy),
	}
	infraEnv.PullSecret = cluster.PullSecret
	infraEnv.Proxy = &proxy
	infraEnv.IgnitionConfigOverride = cluster.IgnitionConfigOverrides

	cfg, err := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, false, b.authHandler.AuthType())
	if err != nil {
		log.WithError(err).Error("Failed to format ignition config")
		return common.GenerateErrorResponder(err)
	}

	configParams := models.DiscoveryIgnitionParams{Config: cfg}
	return installer.NewGetDiscoveryIgnitionOK().WithPayload(&configParams)
}

func (b *bareMetalInventory) UpdateDiscoveryIgnition(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) middleware.Responder {
	err := b.UpdateDiscoveryIgnitionInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateDiscoveryIgnitionCreated()
}

func (b *bareMetalInventory) UpdateDiscoveryIgnitionInternal(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) error {
	log := logutil.FromContext(ctx, b.log)

	//[TODO] - change the code to use InfraEnv once we move the code and test to use InfraEnv CRUD
	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		log.WithError(err).Errorf("Failed to get cluster %s", params.ClusterID)
		return err
	}

	_, err = ignition.ParseToLatest([]byte(params.DiscoveryIgnitionParams.Config))
	if err != nil {
		log.WithError(err).Errorf("Failed to parse ignition config patch %s", params.DiscoveryIgnitionParams)
		return common.NewApiError(http.StatusBadRequest, err)
	}

	err = b.db.Model(&common.Cluster{}).Where(identity.AddUserFilter(ctx, "id = ?"), params.ClusterID).Update("ignition_config_overrides", params.DiscoveryIgnitionParams.Config).Error
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendDiscoveryIgnitionConfigAppliedEvent(ctx, b.eventsHandler, params.ClusterID)

	log.Infof("Custom discovery ignition config was applied to cluster %s", params.ClusterID)

	existed, err := b.objectHandler.DeleteObject(ctx, getImageName(c.ID))
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if existed {
		eventgen.SendIgnitionUpdatedThereforeImageDeletedEvent(ctx, b.eventsHandler, *c.ID)
	}

	return nil
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	v2Params := installer.V2RegisterClusterParams{
		NewClusterParams: params.NewClusterParams,
	}
	c, err := b.RegisterClusterInternal(ctx, nil, v2Params, true)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) setDefaultRegisterClusterParams(_ context.Context, params installer.V2RegisterClusterParams) installer.V2RegisterClusterParams {
	if params.NewClusterParams.ClusterNetworks == nil {
		params.NewClusterParams.ClusterNetworks = []*models.ClusterNetwork{
			{Cidr: models.Subnet(b.Config.DefaultClusterNetworkCidr), HostPrefix: b.Config.DefaultClusterNetworkHostPrefix},
		}
	}
	if params.NewClusterParams.ServiceNetworks == nil {
		params.NewClusterParams.ServiceNetworks = []*models.ServiceNetwork{
			{Cidr: models.Subnet(b.Config.DefaultServiceNetworkCidr)},
		}
	}
	if params.NewClusterParams.MachineNetworks == nil {
		params.NewClusterParams.MachineNetworks = []*models.MachineNetwork{}
	}

	if params.NewClusterParams.VipDhcpAllocation == nil {
		params.NewClusterParams.VipDhcpAllocation = swag.Bool(true)
	}
	if params.NewClusterParams.UserManagedNetworking == nil {
		params.NewClusterParams.UserManagedNetworking = swag.Bool(false)
	}
	if params.NewClusterParams.Hyperthreading == nil {
		params.NewClusterParams.Hyperthreading = swag.String(models.ClusterHyperthreadingAll)
	}
	if params.NewClusterParams.SchedulableMasters == nil {
		params.NewClusterParams.SchedulableMasters = swag.Bool(false)
	}
	if params.NewClusterParams.Platform == nil {
		params.NewClusterParams.Platform = &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
		}
	}
	if params.NewClusterParams.AdditionalNtpSource == nil {
		params.NewClusterParams.AdditionalNtpSource = &b.Config.DefaultNTPSource
	}
	if params.NewClusterParams.HTTPProxy != nil &&
		(params.NewClusterParams.HTTPSProxy == nil || *params.NewClusterParams.HTTPSProxy == "") {
		params.NewClusterParams.HTTPSProxy = params.NewClusterParams.HTTPProxy
	}

	if params.NewClusterParams.DiskEncryption == nil {
		params.NewClusterParams.DiskEncryption = &models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		}
	}

	return params
}

func (b *bareMetalInventory) validateRegisterClusterInternalParams(params *installer.V2RegisterClusterParams, log logrus.FieldLogger) error {
	var err error

	if err = validateProxySettings(params.NewClusterParams.HTTPProxy,
		params.NewClusterParams.HTTPSProxy,
		params.NewClusterParams.NoProxy, params.NewClusterParams.OpenshiftVersion); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if err = validations.ValidateDiskEncryptionParams(params.NewClusterParams.DiskEncryption, b.DiskEncryptionSupport); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if swag.StringValue(params.NewClusterParams.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		// verify minimal OCP version
		err = verifyMinimalOpenShiftVersionForSingleNode(swag.StringValue(params.NewClusterParams.OpenshiftVersion))
		if err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}

		err = validateAndUpdateSingleNodeParams(params.NewClusterParams, log)
		if err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if err = b.validateIgnitionEndpointURL(params.NewClusterParams.IgnitionEndpoint, log); err != nil {
		return err
	}

	if swag.BoolValue(params.NewClusterParams.UserManagedNetworking) {
		if swag.BoolValue(params.NewClusterParams.VipDhcpAllocation) {
			err = errors.Errorf("VIP DHCP Allocation cannot be enabled with User Managed Networking")
			return common.NewApiError(http.StatusBadRequest, err)
		}
		if params.NewClusterParams.IngressVip != "" {
			err = errors.Errorf("Ingress VIP cannot be set with User Managed Networking")
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.NewClusterParams.AdditionalNtpSource != nil {
		ntpSource := swag.StringValue(params.NewClusterParams.AdditionalNtpSource)

		if ntpSource != "" && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err = errors.Errorf("Invalid NTP source: %s", ntpSource)
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	return nil
}

func (b *bareMetalInventory) RegisterClusterInternal(
	ctx context.Context,
	kubeKey *types.NamespacedName,
	params installer.V2RegisterClusterParams,
	v1Flag common.InfraEnvCreateFlag) (*common.Cluster, error) {

	id := strfmt.UUID(uuid.New().String())
	url := installer.GetClusterURL{ClusterID: id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	log.Infof("Register cluster: %s with id %s", swag.StringValue(params.NewClusterParams.Name), id)
	success := false
	var err error
	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
			log.Info(msg)
			eventgen.SendClusterRegistrationSucceededEvent(ctx, b.eventsHandler, id)
		} else {
			errWrapperLog := log
			if err != nil {
				errWrapperLog = log.WithError(err)
			}
			errWrapperLog.Errorf("Failed to registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
		}
	}()

	if err = validations.ValidateIPAddresses(b.IPv6Support, params.NewClusterParams); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	if err = validations.ValidateDualStackNetworks(params.NewClusterParams, false); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	if err = b.validateRegisterClusterInternalParams(&params, log); err != nil {
		return nil, err
	}

	params = b.setDefaultRegisterClusterParams(ctx, params)

	cpuArchitecture, err := b.getNewClusterCPUArchitecture(params.NewClusterParams)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	releaseImage, err := b.versionsHandler.GetReleaseImage(
		swag.StringValue(params.NewClusterParams.OpenshiftVersion), cpuArchitecture)
	if err != nil {
		err = errors.Wrapf(err, "Openshift version %s for CPU architecture %s is not supported",
			swag.StringValue(params.NewClusterParams.OpenshiftVersion), cpuArchitecture)
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	if models.OpenshiftVersionSupportLevelMaintenance == releaseImage.SupportLevel {
		return nil, common.NewApiError(http.StatusBadRequest, errors.Errorf(
			"Openshift version %s support level is: %s, and can't be used for creating a new cluster",
			swag.StringValue(params.NewClusterParams.OpenshiftVersion), releaseImage.SupportLevel))
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	monitoredOperators := b.operatorManagerApi.GetSupportedOperatorsByType(models.OperatorTypeBuiltin)

	if params.NewClusterParams.OlmOperators != nil {
		var newOLMOperators []*models.MonitoredOperator
		newOLMOperators, err = b.getOLMOperators(params.NewClusterParams.OlmOperators)
		if err != nil {
			return nil, err
		}

		monitoredOperators = append(monitoredOperators, newOLMOperators...)
	}

	cluster := common.Cluster{
		Cluster: models.Cluster{
			ID:                    &id,
			Href:                  swag.String(url.String()),
			Kind:                  swag.String(models.ClusterKindCluster),
			BaseDNSDomain:         params.NewClusterParams.BaseDNSDomain,
			IngressVip:            params.NewClusterParams.IngressVip,
			Name:                  swag.StringValue(params.NewClusterParams.Name),
			OpenshiftVersion:      *releaseImage.Version,
			OcpReleaseImage:       *releaseImage.URL,
			SSHPublicKey:          params.NewClusterParams.SSHPublicKey,
			UserName:              ocm.UserNameFromContext(ctx),
			OrgID:                 ocm.OrgIDFromContext(ctx),
			EmailDomain:           ocm.EmailDomainFromContext(ctx),
			HTTPProxy:             swag.StringValue(params.NewClusterParams.HTTPProxy),
			HTTPSProxy:            swag.StringValue(params.NewClusterParams.HTTPSProxy),
			NoProxy:               swag.StringValue(params.NewClusterParams.NoProxy),
			VipDhcpAllocation:     params.NewClusterParams.VipDhcpAllocation,
			NetworkType:           params.NewClusterParams.NetworkType,
			UserManagedNetworking: params.NewClusterParams.UserManagedNetworking,
			AdditionalNtpSource:   swag.StringValue(params.NewClusterParams.AdditionalNtpSource),
			MonitoredOperators:    monitoredOperators,
			HighAvailabilityMode:  params.NewClusterParams.HighAvailabilityMode,
			Hyperthreading:        swag.StringValue(params.NewClusterParams.Hyperthreading),
			SchedulableMasters:    params.NewClusterParams.SchedulableMasters,
			Platform:              params.NewClusterParams.Platform,
			ClusterNetworks:       params.NewClusterParams.ClusterNetworks,
			ServiceNetworks:       params.NewClusterParams.ServiceNetworks,
			MachineNetworks:       params.NewClusterParams.MachineNetworks,
			CPUArchitecture:       cpuArchitecture,
			IgnitionEndpoint:      params.NewClusterParams.IgnitionEndpoint,
		},
		KubeKeyName:             kubeKey.Name,
		KubeKeyNamespace:        kubeKey.Namespace,
		TriggerMonitorTimestamp: time.Now(),
	}

	createNetworkParamsCompatibilityPropagation(params)

	// TODO MGMT-7365: Deprecate single network
	if common.IsSliceNonEmpty(params.NewClusterParams.ClusterNetworks) {
		cluster.ClusterNetworkCidr = string(params.NewClusterParams.ClusterNetworks[0].Cidr)
		cluster.ClusterNetworkHostPrefix = params.NewClusterParams.ClusterNetworks[0].HostPrefix
	}
	if common.IsSliceNonEmpty(params.NewClusterParams.ServiceNetworks) {
		cluster.ServiceNetworkCidr = string(params.NewClusterParams.ServiceNetworks[0].Cidr)
	}
	if common.IsSliceNonEmpty(params.NewClusterParams.MachineNetworks) {
		cluster.MachineNetworkCidr = string(params.NewClusterParams.MachineNetworks[0].Cidr)
	}

	pullSecret := swag.StringValue(params.NewClusterParams.PullSecret)
	err = b.secretValidator.ValidatePullSecret(pullSecret, ocm.UserNameFromContext(ctx), b.authHandler)
	if err != nil {
		err = errors.Wrap(secretValidationToUserError(err), "pull secret for new cluster is invalid")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	ps, err := b.updatePullSecret(pullSecret, log)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest,
			errors.New("Failed to update Pull-secret with additional credentials"))
	}
	setPullSecret(&cluster, ps)

	if err = validations.ValidateClusterNameFormat(swag.StringValue(params.NewClusterParams.Name)); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateDNSName(cluster); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = updateSSHPublicKey(&cluster); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	setDiskEncryptionWithDefaultValues(&cluster.Cluster, params.NewClusterParams.DiskEncryption)

	if err = b.setDefaultUsage(&cluster.Cluster); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	err = b.clusterApi.RegisterCluster(ctx, &cluster, v1Flag, models.ImageType(b.Config.ISOImageType))
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if b.ocmClient != nil {
		if err = b.integrateWithAMSClusterRegistration(ctx, &cluster); err != nil {
			err = errors.Wrapf(err, "cluster %s failed to integrate with AMS on cluster registration", id)
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	success = true
	b.metricApi.ClusterRegistered(cluster.OpenshiftVersion, *cluster.ID, cluster.EmailDomain)
	return b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: *cluster.ID})
}

func setDiskEncryptionWithDefaultValues(c *models.Cluster, config *models.DiskEncryption) {
	// When enabling the encryption we set the mode to TPMv2 unless the request contains an
	// explicit value.
	if config == nil {
		return
	}

	c.DiskEncryption = config

	if c.DiskEncryption.EnableOn == nil {
		c.DiskEncryption.EnableOn = swag.String(models.DiskEncryptionEnableOnNone)
	}

	if config.Mode == nil {
		c.DiskEncryption.Mode = swag.String(models.DiskEncryptionModeTpmv2)
	}
}

func updateSSHPublicKey(cluster *common.Cluster) error {
	sshPublicKey := swag.StringValue(&cluster.SSHPublicKey)
	if sshPublicKey == "" {
		return nil
	}
	sshPublicKey = strings.TrimSpace(cluster.SSHPublicKey)
	if err := validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
		return err
	}
	cluster.SSHPublicKey = sshPublicKey
	return nil
}

func updateSSHAuthorizedKey(infraEnv *common.InfraEnv) error {
	sshPublicKey := swag.StringValue(&infraEnv.SSHAuthorizedKey)
	if sshPublicKey == "" {
		return nil
	}
	sshPublicKey = strings.TrimSpace(infraEnv.SSHAuthorizedKey)
	if err := validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
		return err
	}
	infraEnv.SSHAuthorizedKey = sshPublicKey
	return nil
}

func (b *bareMetalInventory) validateDNSName(cluster common.Cluster) error {
	if cluster.Name == "" || cluster.BaseDNSDomain == "" {
		return nil
	}
	return b.dnsApi.ValidateDNSName(cluster.Name, cluster.BaseDNSDomain)
}

func (b *bareMetalInventory) integrateWithAMSClusterRegistration(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Creating AMS subscription for cluster %s", *cluster.ID)
	sub, err := b.ocmClient.AccountsMgmt.CreateSubscription(ctx, *cluster.ID, cluster.Name)
	if err != nil {
		log.WithError(err).Errorf("Failed to create AMS subscription for cluster %s, rolling back cluster registration", *cluster.ID)
		if deregisterErr := b.clusterApi.DeregisterCluster(ctx, cluster); deregisterErr != nil {
			log.WithError(deregisterErr).Errorf("Failed to rollback cluster %s registration", *cluster.ID)
		}
		return err
	}
	log.Infof("AMS subscription %s was created for cluster %s", sub.ID(), *cluster.ID)
	if err := b.clusterApi.UpdateAmsSubscriptionID(ctx, *cluster.ID, strfmt.UUID(sub.ID())); err != nil {
		log.WithError(err).Errorf("Failed to update ams_subscription_id in cluster %v, rolling back AMS subscription and cluster registration", *cluster.ID)
		if deleteSubErr := b.ocmClient.AccountsMgmt.DeleteSubscription(ctx, strfmt.UUID(sub.ID())); deleteSubErr != nil {
			log.WithError(deleteSubErr).Errorf("Failed to rollback AMS subscription %s in cluster %s", sub.ID(), *cluster.ID)
		}
		if deregisterErr := b.clusterApi.DeregisterCluster(ctx, cluster); deregisterErr != nil {
			log.WithError(deregisterErr).Errorf("Failed to rollback cluster %s registration", *cluster.ID)
		}
		return err
	}
	return nil
}

func verifyMinimalOpenShiftVersionForSingleNode(requestedOpenshiftVersion string) error {
	ocpVersion, err := version.NewVersion(requestedOpenshiftVersion)
	if err != nil {
		return errors.Errorf("Failed to parse OCP version %s", requestedOpenshiftVersion)
	}
	minimalVersionForSno, err := version.NewVersion(minimalOpenShiftVersionForSingleNode)
	if err != nil {
		return errors.Errorf("Failed to parse minimal OCP version %s", minimalOpenShiftVersionForSingleNode)
	}
	if ocpVersion.LessThan(minimalVersionForSno) {
		return errors.Errorf("Invalid OCP version (%s) for Single node, Single node OpenShift is supported for version 4.8 and above", requestedOpenshiftVersion)
	}
	return nil
}

func validateAndUpdateSingleNodeParams(newClusterParams *models.ClusterCreateParams, log logrus.FieldLogger) error {
	if newClusterParams.UserManagedNetworking == nil {
		log.Infof("HA mode is None, setting UserManagedNetworking to true")
		// in case of single node UserManagedNetworking should be true
		newClusterParams.UserManagedNetworking = swag.Bool(true)
	}
	if newClusterParams.VipDhcpAllocation == nil {
		log.Infof("HA mode is None, setting VipDhcpAllocation to false")
		// in case of single node VipDhcpAllocation should be false
		newClusterParams.VipDhcpAllocation = swag.Bool(false)
	}

	if !swag.BoolValue(newClusterParams.UserManagedNetworking) {
		return errors.Errorf("User Managed Networking must be enabled on single node OpenShift")
	}
	if swag.BoolValue(newClusterParams.VipDhcpAllocation) {
		return errors.Errorf("VIP DHCP Allocation cannot be enabled on single node OpenShift")
	}

	return nil
}

func (b *bareMetalInventory) getNewClusterCPUArchitecture(newClusterParams *models.ClusterCreateParams) (string, error) {
	if newClusterParams.CPUArchitecture == "" || newClusterParams.CPUArchitecture == common.DefaultCPUArchitecture {
		// Empty value implies x86_64 (default architecture),
		// which is supported for now regardless of the release images list.
		// TODO: remove once release images list is exclusively used.
		return common.DefaultCPUArchitecture, nil
	}

	if !swag.BoolValue(newClusterParams.UserManagedNetworking) {
		return "", errors.Errorf("Non x86_64 CPU architectures are supported only with User Managed Networking")
	}

	cpuArchitectures, err := b.versionsHandler.GetCPUArchitectures(*newClusterParams.OpenshiftVersion)
	if err != nil {
		return "", err
	}
	for _, cpuArchitecture := range cpuArchitectures {
		if cpuArchitecture == newClusterParams.CPUArchitecture {
			return cpuArchitecture, nil
		}
	}

	// Didn't find requested architecture in the release images list
	return "", errors.Errorf("Requested CPU architecture %s is not available", newClusterParams.CPUArchitecture)
}

func (b *bareMetalInventory) RegisterAddHostsCluster(ctx context.Context, params installer.RegisterAddHostsClusterParams) middleware.Responder {
	v2Params := installer.V2ImportClusterParams{
		NewImportClusterParams: &models.ImportClusterParams{
			APIVipDnsname:      params.NewAddHostsClusterParams.APIVipDnsname,
			Name:               params.NewAddHostsClusterParams.Name,
			OpenshiftVersion:   params.NewAddHostsClusterParams.OpenshiftVersion,
			OpenshiftClusterID: params.NewAddHostsClusterParams.ID,
		},
	}
	c, err := b.V2ImportClusterInternal(ctx, nil, params.NewAddHostsClusterParams.ID, v2Params, true)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterAddHostsClusterCreated().WithPayload(&c.Cluster)

}
func (b *bareMetalInventory) V2ImportClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, id *strfmt.UUID,
	params installer.V2ImportClusterParams, v1Flag common.InfraEnvCreateFlag) (*common.Cluster, error) {
	url := installer.GetClusterURL{ClusterID: *id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	apivipDnsname := swag.StringValue(params.NewImportClusterParams.APIVipDnsname)
	clusterName := swag.StringValue(params.NewImportClusterParams.Name)
	inputOpenshiftVersion := swag.StringValue(params.NewImportClusterParams.OpenshiftVersion)

	log.Infof("Import add-hosts-cluster: %s, id %s, version %s, openshift cluster id %s", clusterName, id.String(), inputOpenshiftVersion, params.NewImportClusterParams.OpenshiftClusterID)

	if clusterPkg.ClusterExists(b.db, *id) {
		return nil, common.NewApiError(http.StatusBadRequest, fmt.Errorf("AddHostsCluster for AI cluster %s already exists", id))
	}

	// Day2 supports only x86_64 for now
	releaseImage, err := b.versionsHandler.GetReleaseImage(inputOpenshiftVersion, common.DefaultCPUArchitecture)
	if err != nil {
		log.WithError(err).Errorf("Failed to get opnshift version supported by versions map from version %s", inputOpenshiftVersion)
		return nil, common.NewApiError(http.StatusBadRequest, fmt.Errorf("failed to get opnshift version supported by versions map from version %s", inputOpenshiftVersion))
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	newCluster := common.Cluster{Cluster: models.Cluster{
		ID:                 id,
		Href:               swag.String(url.String()),
		Kind:               swag.String(models.ClusterKindAddHostsCluster),
		Name:               clusterName,
		OpenshiftVersion:   *releaseImage.Version,
		OpenshiftClusterID: common.StrFmtUUIDVal(params.NewImportClusterParams.OpenshiftClusterID),
		OcpReleaseImage:    *releaseImage.URL,
		UserName:           ocm.UserNameFromContext(ctx),
		OrgID:              ocm.OrgIDFromContext(ctx),
		EmailDomain:        ocm.EmailDomainFromContext(ctx),
		APIVipDNSName:      swag.String(apivipDnsname),
		HostNetworks:       []*models.HostNetwork{},
		Hosts:              []*models.Host{},
		CPUArchitecture:    common.DefaultCPUArchitecture,
		Platform:           &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
	},
		KubeKeyName:      kubeKey.Name,
		KubeKeyNamespace: kubeKey.Namespace,
	}

	err = validations.ValidateClusterNameFormat(clusterName)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	// After registering the cluster, its status should be 'ClusterStatusAddingHosts'
	err = b.clusterApi.RegisterAddHostsCluster(ctx, &newCluster, v1Flag, models.ImageType(b.Config.ISOImageType))
	if err != nil {
		log.Errorf("failed to register cluster %s ", clusterName)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	b.metricApi.ClusterRegistered(newCluster.OpenshiftVersion, *newCluster.ID, newCluster.EmailDomain)
	return &newCluster, nil
}

func (b *bareMetalInventory) createAndUploadNodeIgnition(ctx context.Context, cluster *common.Cluster, host *models.Host) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Starting createAndUploadNodeIgnition for cluster %s, host %s", cluster.ID, host.ID)

	// Specify ignition endpoint based on cluster configuration:
	address := cluster.APIVip
	if address == "" {
		address = swag.StringValue(cluster.APIVipDNSName)
	}

	ignitionEndpointUrl := fmt.Sprintf("http://%s:22624/config/%s", address, host.MachineConfigPoolName)
	if cluster.IgnitionEndpoint != nil && cluster.IgnitionEndpoint.URL != nil {
		url, err := url.Parse(*cluster.IgnitionEndpoint.URL)
		if err != nil {
			return err
		}
		url.Path = path.Join(url.Path, host.MachineConfigPoolName)
		ignitionEndpointUrl = url.String()
	}

	var caCert *string = nil
	if cluster.IgnitionEndpoint != nil {
		caCert = cluster.IgnitionEndpoint.CaCertificate
	}

	ignitionBytes, err := b.IgnitionBuilder.FormatSecondDayWorkerIgnitionFile(ignitionEndpointUrl, caCert, host.IgnitionEndpointToken)
	if err != nil {
		return errors.Errorf("Failed to create ignition string for cluster %s", cluster.ID)
	}

	// Update host ignition hostname:
	fullIgnition, err := ignition.SetHostnameForNodeIgnition(ignitionBytes, host)
	if err != nil {
		return errors.Errorf("Failed to create ignition string for cluster %s, host %s", cluster.ID, host.ID)
	}
	fileName := fmt.Sprintf("%s/worker-%s.ign", cluster.ID, host.ID)
	log.Infof("Uploading ignition file <%s>", fileName)
	err = b.objectHandler.Upload(ctx, fullIgnition, fileName)
	if err != nil {
		return errors.Errorf("Failed to upload worker ignition for cluster %s", cluster.ID)
	}
	return nil
}

func (b *bareMetalInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	v2Params := installer.V2DeregisterClusterParams{ClusterID: params.ClusterID}
	if err := b.DeregisterClusterInternal(ctx, v2Params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewDeregisterClusterNoContent()
}

func (b *bareMetalInventory) integrateWithAMSClusterDeregistration(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)
	// AMS subscription is created only for day1 clusters
	if *cluster.Kind == models.ClusterKindCluster {
		log.Infof("Deleting AMS subscription %s for non-active cluster %s", cluster.AmsSubscriptionID, *cluster.ID)
		sub, err := b.ocmClient.AccountsMgmt.GetSubscription(ctx, cluster.AmsSubscriptionID)
		if err != nil {
			log.WithError(err).Errorf("Failed to get AMS subscription %s for cluster %s", cluster.AmsSubscriptionID, *cluster.ID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if sub.Status() == ocm.SubscriptionStatusReserved {
			if err = b.ocmClient.AccountsMgmt.DeleteSubscription(ctx, cluster.AmsSubscriptionID); err != nil {
				log.WithError(err).Errorf("Failed to delete AMS subscription %s for cluster %s", cluster.AmsSubscriptionID, *cluster.ID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) DeregisterClusterInternal(ctx context.Context, params installer.V2DeregisterClusterParams) error {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var err error
	log.Infof("Deregister cluster id %s", params.ClusterID)

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	if b.ocmClient != nil {
		if err = b.integrateWithAMSClusterDeregistration(ctx, cluster); err != nil {
			log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster deregistration", params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err = b.deleteDNSRecordSets(ctx, *cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	if err = b.deleteOrUnbindHosts(ctx, cluster); err != nil {
		log.WithError(err).Errorf("failed delete or unbind hosts when deregistering cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.clusterApi.DeregisterCluster(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister cluster %s", params.ClusterID)
		return common.NewApiError(http.StatusNotFound, err)
	}
	return nil
}

func (b *bareMetalInventory) deleteOrUnbindHosts(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)
	for _, h := range cluster.Hosts {
		infraEnv, err := common.GetInfraEnvFromDB(b.db, h.InfraEnvID)
		if err != nil {
			log.WithError(err).Errorf("failed to get infra env: %s", h.InfraEnvID)
			return err
		}
		if infraEnv.ClusterID == *cluster.ID {
			if err = b.hostApi.UnRegisterHost(ctx, h.ID.String(), h.InfraEnvID.String()); err != nil {
				log.WithError(err).Errorf("failed to delete host: %s", h.ID.String())
				return err
			}
			eventgen.SendHostDeregisteredEvent(ctx, b.eventsHandler, *h.ID, h.InfraEnvID, cluster.ID,
				hostutil.GetHostnameForMsg(h))
		} else if h.ClusterID != nil {
			if err = b.hostApi.UnbindHost(ctx, h, b.db); err != nil {
				log.WithError(err).Errorf("Failed to unbind host <%s>", h.ID.String())
				return err
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	return b.DownloadISOInternal(ctx, params.ClusterID)
}

func (b *bareMetalInventory) DownloadISOInternal(ctx context.Context, infraEnvID strfmt.UUID) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	infraEnv, err := common.GetInfraEnvFromDB(b.db, infraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env %s", infraEnvID)
		return common.GenerateErrorResponder(err)
	}

	imgName := getImageName(infraEnv.ID)
	exists, err := b.objectHandler.DoesObjectExist(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for cluster %s", infraEnv.ID.String())
		eventgen.SendDownloadImageFetchFailedEvent(ctx, b.eventsHandler, infraEnvID)

		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	if !exists {
		eventgen.SendDownloadImageFindFailedEvent(ctx, b.eventsHandler, infraEnvID)

		return installer.NewDownloadClusterISONotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, errors.New("The image was not found "+
				"(perhaps it expired) - please generate the image and try again")))
	}
	reader, contentLength, err := b.objectHandler.Download(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for cluster %s", infraEnv.ID.String())
		eventgen.SendDownloadImageFetchFailedEvent(ctx, b.eventsHandler, infraEnvID)
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	eventgen.SendDownloadImageStartedEvent(ctx, b.eventsHandler, infraEnvID, string(common.ImageTypeValue(infraEnv.Type)))

	return filemiddleware.NewResponder(installer.NewDownloadClusterISOOK().WithPayload(reader),
		fmt.Sprintf("cluster-%s-discovery.iso", infraEnvID),
		contentLength)
}

func (b *bareMetalInventory) DownloadClusterISOHeaders(ctx context.Context, params installer.DownloadClusterISOHeadersParams) middleware.Responder {
	return b.DownloadISOHeadersInternal(ctx, params.ClusterID)
}

func (b *bareMetalInventory) DownloadISOHeadersInternal(ctx context.Context, infraEnvID strfmt.UUID) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	infraEnv, err := common.GetInfraEnvFromDB(b.db, infraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env %s", infraEnvID)
		return common.GenerateErrorResponder(err)
	}

	imgName := getImageName(infraEnv.ID)
	exists, err := b.objectHandler.DoesObjectExist(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for infra env %s", infraEnv.ID.String())
		eventgen.SendDownloadImageFetchFailedEvent(ctx, b.eventsHandler, infraEnvID)
		return installer.NewDownloadClusterISOHeadersInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	if !exists {
		return installer.NewDownloadClusterISOHeadersNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, errors.New("The image was not found")))
	}
	imgSize, err := b.objectHandler.GetObjectSizeBytes(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO size for cluster %s", infraEnv.ID.String())
		return common.NewApiError(http.StatusBadRequest, err)
	}
	return installer.NewDownloadClusterISOHeadersOK().WithContentLength(imgSize)
}

type uriBuilder interface {
	Build() (*url.URL, error)
}

func (b *bareMetalInventory) updateImageInfoPostUpload(ctx context.Context, infraEnv *common.InfraEnv, infraEnvProxyHash string, imageType models.ImageType, generated bool, v2 bool) error {
	updates := map[string]interface{}{}
	imgName := getImageName(infraEnv.ID)
	imgSize, err := b.objectHandler.GetObjectSizeBytes(ctx, imgName)
	if err != nil {
		return errors.New("Failed to generate image: error fetching size")
	}
	updates["size_bytes"] = imgSize
	infraEnv.SizeBytes = &imgSize

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if generated {
		downloadURL := ""
		if b.objectHandler.IsAwsS3() {
			downloadURL, err = b.objectHandler.GeneratePresignedDownloadURL(ctx, imgName, imgName, b.Config.ImageExpirationTime)
			if err != nil {
				return errors.New("Failed to generate image: error generating URL")
			}
		} else {
			var builder uriBuilder
			if v2 {
				builder = &installer.DownloadInfraEnvDiscoveryImageURL{InfraEnvID: *infraEnv.ID}
			} else {
				builder = &installer.DownloadClusterISOURL{ClusterID: *infraEnv.ID}
			}
			clusterISOURL, err := builder.Build()
			if err != nil {
				return errors.New("Failed to generate image: error generating cluster ISO URL")
			}
			downloadURL = fmt.Sprintf("%s%s", b.Config.ServiceBaseURL, clusterISOURL.RequestURI())
			if b.authHandler.AuthType() == auth.TypeLocal {
				downloadURL, err = gencrypto.SignURL(downloadURL, infraEnv.ID.String(), gencrypto.InfraEnvKey)
				if err != nil {
					return errors.Wrap(err, "Failed to sign cluster ISO URL")
				}
			}
		}
		updates["download_url"] = downloadURL
		infraEnv.DownloadURL = downloadURL
		updates["generated"] = true
		infraEnv.Generated = true
	}

	if infraEnv.ProxyHash != infraEnvProxyHash {
		updates["proxy_hash"] = infraEnvProxyHash
		infraEnv.ProxyHash = infraEnvProxyHash
	}

	updates["type"] = imageType
	infraEnv.Type = common.ImageTypePtr(imageType)

	dbReply := b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		return errors.New("Failed to generate image: error updating image record")
	}

	return nil
}

func (b *bareMetalInventory) updateExternalImageInfo(infraEnv *common.InfraEnv, infraEnvProxyHash string, imageType models.ImageType) error {
	updates := map[string]interface{}{}

	// this is updated before now for the v2 (infraEnv) case, but not in the cluster ISO case so we need to check if we should save it here
	if infraEnv.ProxyHash != infraEnvProxyHash {
		updates["proxy_hash"] = infraEnvProxyHash
		infraEnv.ProxyHash = infraEnvProxyHash
	}

	var (
		prevType    string
		prevVersion string
		prevArch    string
	)
	if infraEnv.DownloadURL != "" {
		currentURL, err := url.Parse(infraEnv.DownloadURL)
		if err != nil {
			return errors.Wrap(err, "failed to parse current download URL")
		}
		vals := currentURL.Query()
		prevType = vals.Get("type")
		prevVersion = vals.Get("version")
		prevArch = vals.Get("arch")
	}

	updates["type"] = imageType
	infraEnv.Type = common.ImageTypePtr(imageType)

	osImage, err := b.getOsImageOrLatest(&infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return err
	}

	var version string
	if osImage.OpenshiftVersion != nil {
		version = *osImage.OpenshiftVersion
	} else {
		return errors.Errorf("OS image entry '%+v' missing OpenshiftVersion field", osImage)
	}

	var arch string
	if osImage.CPUArchitecture != nil {
		arch = *osImage.CPUArchitecture
	}

	if string(imageType) != prevType || version != prevVersion || arch != prevArch || !infraEnv.Generated {
		var expiresAt *strfmt.DateTime
		infraEnv.DownloadURL, expiresAt, err = b.generateImageDownloadURL(infraEnv.ID.String(), string(imageType), version, arch, infraEnv.ImageTokenKey)
		if err != nil {
			return errors.Wrap(err, "failed to create download URL")
		}

		updates["download_url"] = infraEnv.DownloadURL
		updates["generated"] = true
		infraEnv.Generated = true
		updates["expires_at"] = *expiresAt
		infraEnv.ExpiresAt = *expiresAt
	}

	err = b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates).Error
	if err != nil {
		return errors.Wrap(err, "failed to update infraenv")
	}

	return nil
}

func (b *bareMetalInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	c, err := b.GenerateClusterISOInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGenerateClusterISOCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) GenerateClusterISOInternal(ctx context.Context, params installer.GenerateClusterISOParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("prepare image for cluster %s", params.ClusterID)

	if params.ImageCreateParams.SSHPublicKey != "" {
		if err := validations.ValidateSSHPublicKey(params.ImageCreateParams.SSHPublicKey); err != nil {
			log.Error(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.ImageCreateParams.StaticNetworkConfig != nil {
		if err := b.staticNetworkConfig.ValidateStaticConfigParams(ctx, params.ImageCreateParams.StaticNetworkConfig); err != nil {
			log.Error(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	// set the default value for REST API case, in case it was not provided in the request
	if params.ImageCreateParams.ImageType == "" {
		params.ImageCreateParams.ImageType = models.ImageType(b.Config.ISOImageType)
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("generate cluster ISO failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("generate cluster ISO failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		eventgen.SendGenerateImageStartFailedEvent(ctx, b.eventsHandler, params.ClusterID)
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction"))
	}

	cluster, err := common.GetClusterFromDB(tx, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, err
	}

	if !cluster.PullSecretSet {
		errMsg := "Can't generate cluster ISO without pull secret"
		log.Error(errMsg)
		return nil, common.NewApiError(http.StatusBadRequest, errors.New(errMsg))
	}

	infraEnv, err := common.GetInfraEnvFromDB(tx, params.ClusterID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env for cluster: %s", params.ClusterID)
		return nil, err
	}

	// [TODO] - set the proxy here in order to get the proxy changes done set during cluster update.
	// we don't update the infra env in the ClusterUpdate, since it might not existance at that time
	// once we move to v2 ( updating InfraEnv directly), this code should be removed
	infraEnv.Proxy = &models.Proxy{
		HTTPProxy:  swag.String(cluster.HTTPProxy),
		HTTPSProxy: swag.String(cluster.HTTPSProxy),
		NoProxy:    swag.String(cluster.NoProxy),
	}

	/* We need to ensure that the metadata in the DB matches the image that will be uploaded to S3,
	so we check that at least 10 seconds have past since the previous request to reduce the chance
	of a race between two consecutive requests.

	This is not relevant if we're using the image service as the image is never uploaded to s3, so skip the check
	if the image service URL is set
	*/
	now := time.Now()
	previousCreatedAt := time.Time(infraEnv.GeneratedAt)
	if b.ImageServiceBaseURL == "" && previousCreatedAt.Add(WindowBetweenRequestsInSeconds).After(now) {
		log.Error("request came too soon after previous request")
		return nil, common.NewApiError(
			http.StatusConflict,
			errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
	}

	/* If the request has the same parameters as the previous request and the image is still in S3,
	just refresh the timestamp.
	*/
	infraEnvProxyHash, err := computeProxyHash(infraEnv.Proxy)
	if err != nil {
		msg := "Failed to compute infraEnv proxy hash"
		log.Error(msg, err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	staticNetworkConfig := b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.ImageCreateParams.StaticNetworkConfig)

	var imageExists bool
	if infraEnv.SSHAuthorizedKey == params.ImageCreateParams.SSHPublicKey &&
		infraEnv.ProxyHash == infraEnvProxyHash &&
		infraEnv.StaticNetworkConfig == staticNetworkConfig &&
		infraEnv.Generated &&
		common.ImageTypeValue(infraEnv.Type) == params.ImageCreateParams.ImageType &&
		b.ImageServiceBaseURL == "" {
		imgName := getImageName(&params.ClusterID)
		imageExists, err = b.objectHandler.UpdateObjectTimestamp(ctx, imgName)
		if err != nil {
			log.WithError(err).Errorf("failed to contact storage backend")
			eventgen.SendGenerateImageContactStorageBackendFailedEvent(ctx, b.eventsHandler, params.ClusterID)
			return nil, common.NewApiError(http.StatusInternalServerError, errors.New("failed to contact storage backend"))
		}
	}

	updates := map[string]interface{}{}
	updates["pull_secret"] = cluster.PullSecret
	updates["ssh_authorized_key"] = params.ImageCreateParams.SSHPublicKey
	updates["generated_at"] = strfmt.DateTime(now)
	updates["image_expires_at"] = strfmt.DateTime(now.Add(b.Config.ImageExpirationTime))
	updates["static_network_config"] = staticNetworkConfig
	updates["proxy_http_proxy"] = cluster.HTTPProxy
	updates["proxy_https_proxy"] = cluster.HTTPSProxy
	updates["proxy_no_proxy"] = cluster.NoProxy
	//[TODO] - remove this code once we update ignition config override in InfraEnv via UpdateDiscoveryIgnitionInternal
	updates["ignition_config_override"] = cluster.IgnitionConfigOverrides
	if b.ImageServiceBaseURL == "" && !imageExists {
		// set image-generated indicator to false before the attempt to genearate the image in order to have an explicit
		// state of the image creation based on the cluster parameters which will be committed to the DB
		updates["generated"] = false
		updates["download_url"] = ""
	}
	dbReply := tx.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update infra env: %s", params.ClusterID)
		msg := "Failed to generate image: error updating metadata"
		eventgen.SendGenerateImageUpdateMetadataFailedEvent(ctx, b.eventsHandler, params.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	cluster_updates := map[string]interface{}{}
	cluster_updates["static_network_configured"] = (staticNetworkConfig != "")

	dbReply = tx.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(cluster_updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update cluster: %s", params.ClusterID)
		msg := "Failed to generate image: error updating metadata"
		eventgen.SendGenerateImageUpdateMetadataFailedEvent(ctx, b.eventsHandler, params.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		msg := "Failed to generate image: error committing the transaction"
		eventgen.SendGenerateImageCommitTransactionFailedEvent(ctx, b.eventsHandler, params.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}
	txSuccess = true

	err = b.createAndUploadNewImage(ctx, log, infraEnvProxyHash, params.ClusterID, params.ImageCreateParams.ImageType, false, imageExists)
	if err != nil {
		return nil, err
	}

	return b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: params.ClusterID})
}

func (b *bareMetalInventory) GenerateInfraEnvISOInternal(ctx context.Context, infraEnv *common.InfraEnv) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("prepare image for infraEnv %s", infraEnv.ID)

	if !infraEnv.PullSecretSet {
		errMsg := "Can't generate infraEnv ISO without pull secret"
		log.Error(errMsg)
		return common.NewApiError(http.StatusBadRequest, errors.New(errMsg))
	}

	/* We need to ensure that the metadata in the DB matches the image that will be uploaded to S3,
	so we check that at least 10 seconds have past since the previous request to reduce the chance
	of a race between two consecutive requests.

	This is not relevant if we're using the image service as the image is never uploaded to s3, so skip the check
	if the image service URL is set
	*/
	now := time.Now()
	previousCreatedAt := time.Time(infraEnv.GeneratedAt)
	if b.ImageServiceBaseURL == "" && previousCreatedAt.Add(WindowBetweenRequestsInSeconds).After(now) {
		log.Error("request came too soon after previous request")
		return common.NewApiError(
			http.StatusConflict,
			errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
	}

	/* If the request has the same parameters as the previous request and the image is still in S3,
	just refresh the timestamp.
	*/
	var imageExists bool
	var err error
	if infraEnv.Generated && b.ImageServiceBaseURL == "" {
		imgName := getImageName(infraEnv.ID)
		imageExists, err = b.objectHandler.UpdateObjectTimestamp(ctx, imgName)
		if err != nil {
			log.WithError(err).Errorf("failed to contact storage backend")
			return common.NewApiError(http.StatusInternalServerError, errors.New("failed to contact storage backend"))
		}
	}

	updates := map[string]interface{}{}
	updates["generated_at"] = strfmt.DateTime(now)
	updates["image_expires_at"] = strfmt.DateTime(now.Add(b.Config.ImageExpirationTime))
	if b.ImageServiceBaseURL == "" && !imageExists {
		// set image-generated indicator to false before the attempt to genearate the image in order to have an explicit
		// state of the image creation based on the cluster parameters which will be committed to the DB
		updates["generated"] = false
		updates["download_url"] = ""
	}
	dbReply := b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update infra env: %s", infraEnv.ID)
		msg := "Failed to generate image: error updating metadata"
		return common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	err = b.createAndUploadNewImage(ctx, log, infraEnv.ProxyHash, *infraEnv.ID, common.ImageTypeValue(infraEnv.Type), true, imageExists)
	if err != nil {
		return err
	}

	return nil
}

func (b *bareMetalInventory) createAndUploadNewImage(ctx context.Context, log logrus.FieldLogger, infraEnvProxyHash string,
	infraEnvID strfmt.UUID, imageType models.ImageType, v2 bool, imageExists bool) error {

	infraEnv, err := common.GetInfraEnvFromDB(b.db, infraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env %s after update", infraEnv.ID)
		eventgen.SendGenerateImageFetchFailedEvent(ctx, b.eventsHandler, infraEnvID)
		return err
	}

	// return without generating the image if we're using the image service or if the image has already been generated
	if b.ImageServiceBaseURL != "" {
		if err = b.updateExternalImageInfo(infraEnv, infraEnvProxyHash, imageType); err != nil {
			return err
		}

		return nil
	} else if imageExists {
		if err = b.updateImageInfoPostUpload(ctx, infraEnv, infraEnvProxyHash, imageType, false, v2); err != nil {
			return err
		}

		log.Infof("Re-used existing image <%s>", infraEnv.ID)
		eventgen.SendExistingImageReusedEvent(ctx, b.eventsHandler, infraEnvID, string(imageType))
		return nil
	}

	// Setting ImageInfo.Type at this point in order to pass it to FormatDiscoveryIgnitionFile without saving it to the DB.
	// Saving it to the DB will be done after a successful image generation by updateImageInfoPostUpload
	infraEnv.Type = common.ImageTypePtr(imageType)
	ignitionConfig, err := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, false, b.authHandler.AuthType())
	if err != nil {
		log.WithError(err).Errorf("failed to format ignition config file for cluster %s", infraEnv.ID)
		eventgen.SendGenerateImageFormatFailedEvent(ctx, b.eventsHandler, *infraEnv.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	objectPrefix := fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, infraEnv.ID.String())

	if imageType == models.ImageTypeMinimalIso {
		if err := b.generateClusterMinimalISO(ctx, log, infraEnv, ignitionConfig, objectPrefix); err != nil {
			log.WithError(err).Errorf("Failed to generate minimal ISO for cluster %s", infraEnv.ID)
			eventgen.SendGenerateMinimalIsoFailedEvent(ctx, b.eventsHandler, *infraEnv.ID)

			return common.NewApiError(http.StatusInternalServerError, err)
		}
	} else {
		baseISOName, err := b.objectHandler.GetBaseIsoObject(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
		if err != nil {
			log.WithError(err).Errorf("Failed to get source object name for cluster %s with ocp version %s", infraEnv.ID, infraEnv.OpenshiftVersion)
			return common.NewApiError(http.StatusInternalServerError, err)
		}

		if err := b.objectHandler.UploadISO(ctx, ignitionConfig, baseISOName, objectPrefix); err != nil {
			log.WithError(err).Errorf("Upload ISO failed for cluster %s", infraEnv.ID)
			eventgen.SendUploadImageFailedEvent(ctx, b.eventsHandler, *infraEnv.ID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err := b.updateImageInfoPostUpload(ctx, infraEnv, infraEnvProxyHash, imageType, true, v2); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	details := b.getIgnitionConfigForLogging(ctx, infraEnv, log, imageType)
	eventgen.SendIgnitionConfigImageGeneratedEvent(ctx, b.eventsHandler, *infraEnv.ID, details)
	log.Infof("Generated image %s", details)

	return nil
}

func (b *bareMetalInventory) getIgnitionConfigForLogging(ctx context.Context, infraEnv *common.InfraEnv, log logrus.FieldLogger, imageType models.ImageType) string {
	ignitionConfigForLogging, _ := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, true, b.authHandler.AuthType())
	log.Infof("Generated infra env <%s> image with ignition config %s", infraEnv.ID, ignitionConfigForLogging)
	var msgDetails []string

	httpProxy, _, _ := common.GetProxyConfigs(infraEnv.Proxy)
	if httpProxy != "" {
		msgDetails = append(msgDetails, fmt.Sprintf(`proxy URL is "%s"`, httpProxy))
	}

	msgDetails = append(msgDetails, fmt.Sprintf(`Image type is "%s"`, string(imageType)))

	sshExtra := "SSH public key is not set"
	if infraEnv.SSHAuthorizedKey != "" {
		sshExtra = "SSH public key is set"
	}

	msgDetails = append(msgDetails, sshExtra)

	return strings.Join(msgDetails, ", ")
}

func (b *bareMetalInventory) generateClusterMinimalISO(ctx context.Context, log logrus.FieldLogger,
	infraEnv *common.InfraEnv, ignitionConfig, objectPrefix string) error {

	baseISOName, err := b.objectHandler.GetMinimalIsoObjectName(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		log.WithError(err).Errorf("Failed to get source object name for infraEnv %s with ocp version %s", infraEnv.ID.String(), infraEnv.OpenshiftVersion)
		return err
	}

	isoPath, err := s3wrapper.GetFile(ctx, b.objectHandler, baseISOName, b.ISOCacheDir, true)
	if err != nil {
		log.WithError(err).Errorf("Failed to download minimal ISO template %s", baseISOName)
		return err
	}

	var netFiles []staticnetworkconfig.StaticNetworkConfigData
	if infraEnv.StaticNetworkConfig != "" {
		netFiles, err = b.staticNetworkConfig.GenerateStaticNetworkConfigData(ctx, infraEnv.StaticNetworkConfig)
		if err != nil {
			log.WithError(err).Errorf("Failed to create static network config data")
			return err
		}
	}

	var clusterISOPath string
	err = b.isoEditorFactory.WithEditor(ctx, isoPath, log, func(editor isoeditor.Editor) error {
		log.Infof("Creating minimal ISO for cluster %s", infraEnv.ID)
		var createError error
		httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
		infraEnvProxyInfo := isoeditor.ClusterProxyInfo{
			HTTPProxy:  httpProxy,
			HTTPSProxy: httpsProxy,
			NoProxy:    noProxy,
		}
		clusterISOPath, createError = editor.CreateClusterMinimalISO(ignitionConfig, netFiles, &infraEnvProxyInfo)
		return createError
	})

	if err != nil {
		log.WithError(err).Errorf("Failed to create minimal discovery ISO infra env %s with iso file %s", infraEnv.ID, isoPath)
		return err
	}

	log.Infof("Uploading minimal ISO for cluster %s", infraEnv.ID)
	if err := b.objectHandler.UploadFile(ctx, clusterISOPath, fmt.Sprintf("%s.iso", objectPrefix)); err != nil {
		os.Remove(clusterISOPath)
		log.WithError(err).Errorf("Failed to upload minimal discovery ISO for infra env %s", infraEnv.ID)
		return err
	}
	return os.Remove(clusterISOPath)
}

func getImageName(infraEnvID *strfmt.UUID) string {
	return fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, infraEnvID.String()))
}

func (b *bareMetalInventory) refreshAllHosts(ctx context.Context, cluster *common.Cluster) error {
	err := b.setMajorityGroupForCluster(cluster.ID, b.db)
	if err != nil {
		return err
	}
	for _, chost := range cluster.Hosts {
		if swag.StringValue(chost.Status) != models.HostStatusKnown && swag.StringValue(chost.Kind) == models.HostKindHost {
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("Host %s is in status %s and not ready for install",
				hostutil.GetHostnameForMsg(chost), swag.StringValue(chost.Status)))
		}

		err := b.hostApi.RefreshStatus(ctx, chost, b.db)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *bareMetalInventory) storeOpenshiftClusterID(ctx context.Context, clusterID string) (string, error) {
	log := logutil.FromContext(ctx, b.log)
	log.Debug("Downloading bootstrap ignition file")
	reader, _, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterID, "bootstrap.ign"))
	if err != nil {
		log.WithError(err).Error("Failed downloading bootstrap ignition file")
		return "", err
	}

	var openshiftClusterID string
	log.Debug("Extracting Openshift cluster ID from ignition file")
	openshiftClusterID, err = ignition.ExtractClusterID(reader)
	if err != nil {
		log.WithError(err).Error("Failed extracting Openshift cluster ID from ignition file")
		return "", err
	}
	log.Debugf("Got OpenShift cluster ID of %s", openshiftClusterID)

	log.Debugf("Storing Openshift cluster ID of cluster %s to DB", clusterID)
	if err = b.db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update(
		"openshift_cluster_id", openshiftClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed storing Openshift cluster ID of cluster %s to DB", clusterID)
		return "", err
	}

	return openshiftClusterID, nil
}

func (b *bareMetalInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	return b.V2InstallCluster(ctx, installer.V2InstallClusterParams(params))
}

func (b *bareMetalInventory) integrateWithAMSClusterPreInstallation(ctx context.Context, amsSubscriptionID, openshiftClusterID strfmt.UUID) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Updating AMS subscription %s with openshift cluster ID %s", amsSubscriptionID, openshiftClusterID)
	if err := b.ocmClient.AccountsMgmt.UpdateSubscriptionOpenshiftClusterID(ctx, amsSubscriptionID, openshiftClusterID); err != nil {
		log.WithError(err).Errorf("Failed to update AMS subscription with openshift cluster ID %s", openshiftClusterID)
		return err
	}
	return nil
}

func (b *bareMetalInventory) InstallClusterInternal(ctx context.Context, params installer.V2InstallClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	cluster := &common.Cluster{}
	var err error

	log.Infof("preparing for cluster %s installation", params.ClusterID)
	if cluster, err = common.GetClusterFromDBWithoutDisabledHosts(b.db, params.ClusterID); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	// auto select hosts roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		var autoAssigned bool
		var selected bool
		for i := range cluster.Hosts {
			if selected, err = b.hostApi.AutoAssignRole(ctx, cluster.Hosts[i], tx); err != nil {
				return err
			} else {
				autoAssigned = autoAssigned || selected
			}
		}
		//usage for auto role selection is measured only for day1 clusters with more than
		//3 hosts (which would automatically be assigned as masters if the hw is sufficient)
		if usages, uerr := usage.Unmarshal(cluster.Cluster.FeatureUsage); uerr == nil {
			report := cluster.Cluster.EnabledHostCount > common.MinMasterHostsNeededForInstallation && selected
			b.setUsage(report, usage.AutoAssignRoleUsage, nil, usages)
			b.usageApi.Save(tx, *cluster.ID, usages)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err = b.refreshAllHosts(ctx, cluster); err != nil {
		return nil, err
	}
	if _, err = b.clusterApi.RefreshStatus(ctx, cluster, b.db); err != nil {
		return nil, err
	}

	// Reload again after refresh
	if cluster, err = common.GetClusterFromDBWithoutDisabledHosts(b.db, params.ClusterID); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	// Verify cluster is ready to install
	if ok, reason := b.clusterApi.IsReadyForInstallation(cluster); !ok {
		return nil, common.NewApiError(http.StatusConflict,
			errors.Errorf("Cluster is not ready for installation, %s", reason))
	}

	// prepare cluster and hosts for installation
	err = b.db.Transaction(func(tx *gorm.DB) error {
		if err = b.clusterApi.PrepareForInstallation(ctx, cluster, tx); err != nil {
			return err
		}

		if err = b.setBootstrapHost(ctx, *cluster, tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return nil, err
	}

	if err = b.clusterApi.GenerateAdditionalManifests(ctx, cluster); err != nil {
		b.log.WithError(err).Errorf("Failed to generated additional cluster manifest")
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("Failed to generated additional cluster manifest"))
	}

	// Delete previews installation log files from object storage (if exist).
	if err := b.clusterApi.DeleteClusterLogs(ctx, cluster, b.objectHandler); err != nil {
		log.WithError(err).Warnf("Failed deleting s3 logs of cluster %s", cluster.ID.String())
	}

	go func() {
		var err error
		asyncCtx := ctxparams.Copy(ctx)

		defer func() {
			if err != nil {
				log.WithError(err).Warn("Cluster installation initialization failed")
				b.clusterApi.HandlePreInstallError(asyncCtx, cluster, err)
			} else {
				b.clusterApi.HandlePreInstallSuccess(asyncCtx, cluster)
			}
		}()

		if err = b.generateClusterInstallConfig(asyncCtx, *cluster); err != nil {
			return
		}
		log.Infof("generated ignition for cluster %s", cluster.ID.String())

		log.Infof("Storing OpenShift cluster ID of cluster %s to DB", cluster.ID.String())
		var openshiftClusterID string
		if openshiftClusterID, err = b.storeOpenshiftClusterID(ctx, cluster.ID.String()); err != nil {
			return
		}

		if b.ocmClient != nil {
			if err = b.integrateWithAMSClusterPreInstallation(asyncCtx, cluster.AmsSubscriptionID, strfmt.UUID(openshiftClusterID)); err != nil {
				log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster pre installation", params.ClusterID)
				return
			}
		}
	}()

	log.Infof("Successfully prepared cluster <%s> for installation", params.ClusterID.String())
	return cluster, nil
}

func (b *bareMetalInventory) InstallSingleDay2HostInternal(ctx context.Context, clusterId strfmt.UUID, infraEnvId strfmt.UUID, hostId strfmt.UUID) error {

	log := logutil.FromContext(ctx, b.log)
	var err error
	var cluster *common.Cluster
	var h *common.Host

	if h, err = b.getHost(ctx, infraEnvId.String(), hostId.String()); err != nil {
		return err
	}
	// auto select host roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		if _, err = b.hostApi.AutoAssignRole(ctx, &h.Host, tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err = b.hostApi.RefreshStatus(ctx, &h.Host, b.db); err != nil {
		return err
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("InstallSingleDay2HostInternal failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("InstallSingleDay2HostInternal failed")
			tx.Rollback()
		}
	}()

	// in case host monitor already updated the state we need to use FOR UPDATE option
	if cluster, err = common.GetClusterFromDBForUpdate(tx, clusterId, common.UseEagerLoading); err != nil {
		return err
	}

	// move host to installing
	err = b.createAndUploadNodeIgnition(ctx, cluster, &h.Host)
	if err != nil {
		log.Errorf("Failed to upload ignition for host %s", h.RequestedHostname)
		return err
	}
	if installErr := b.hostApi.Install(ctx, &h.Host, tx); installErr != nil {
		log.WithError(installErr).Errorf("Failed to move host %s to installing", h.RequestedHostname)
		return installErr
	}

	err = tx.Commit().Error
	if err != nil {
		log.Error(err)
		return err
	}
	txSuccess = true
	eventgen.SendHostInstallationStartedEvent(ctx, b.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID, hostutil.GetHostnameForMsg(&h.Host))

	return nil
}

func (b *bareMetalInventory) V2InstallHost(ctx context.Context, params installer.V2InstallHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var h *models.Host
	var cluster *common.Cluster
	var err error

	log.Info("Install single day2 host: ", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	h = &host.Host
	if h == nil {
		log.WithError(err).Errorf("host %s not found", params.HostID)
		return common.NewApiError(http.StatusNotFound, err)
	}

	if h.ClusterID == nil {
		log.WithError(err).Errorf("host %s is not bound to any cluster", params.HostID)
		return common.NewApiError(http.StatusNotFound, err)
	}

	if !hostutil.IsDay2Host(h) {
		log.Errorf("InstallHost for host %s is forbidden: not a Day2 hosts", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when adding hosts to an existing cluster"))
	}

	if swag.StringValue(h.Status) != models.HostStatusKnown {
		log.Errorf("Install host for host %s, state %s is forbidden: host not in Known state", params.HostID.String(), swag.StringValue(h.Status))
		return common.NewApiError(http.StatusConflict, fmt.Errorf("cannot install host in state %s", swag.StringValue(h.Status)))
	}

	_, err = b.hostApi.AutoAssignRole(ctx, h, b.db)
	if err != nil {
		log.Errorf("Failed to update role for host %s", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	err = b.hostApi.RefreshStatus(ctx, h, b.db)
	if err != nil {
		log.Errorf("Failed to refresh host %s", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	if swag.StringValue(h.Status) != models.HostStatusKnown {
		return common.NewApiError(http.StatusConflict, fmt.Errorf("cannot install host in state %s after refresh", swag.StringValue(h.Status)))
	}
	if cluster, err = common.GetClusterFromDB(b.db, *h.ClusterID, common.SkipEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}
	err = b.createAndUploadNodeIgnition(ctx, cluster, h)
	if err != nil {
		log.Errorf("Failed to upload ignition for host %s", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}
	err = b.hostApi.Install(ctx, h, b.db)
	if err != nil {
		// we just logs the error, each host install is independent
		log.Errorf("Failed to move host %s to installing", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}

	return installer.NewInstallHostAccepted().WithPayload(h)
}

func (b *bareMetalInventory) InstallHosts(ctx context.Context, params installer.InstallHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	cluster := &common.Cluster{}
	var err error

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// auto select hosts roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		for i := range cluster.Hosts {
			if swag.StringValue(cluster.Hosts[i].Status) != models.HostStatusKnown {
				continue
			}
			if _, err = b.hostApi.AutoAssignRole(ctx, cluster.Hosts[i], tx); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = b.refreshAllHosts(ctx, cluster); err != nil {
		return common.GenerateErrorResponder(err)
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("InstallHosts failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("InstallHosts failed")
			tx.Rollback()
		}
	}()

	// in case host monitor already updated the state we need to use FOR UPDATE option
	if cluster, err = common.GetClusterFromDBForUpdate(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// move hosts to installing
	for i := range cluster.Hosts {
		if swag.StringValue(cluster.Hosts[i].Status) != models.HostStatusKnown {
			continue
		}
		err = b.createAndUploadNodeIgnition(ctx, cluster, cluster.Hosts[i])
		if err != nil {
			log.Errorf("Failed to upload ignition for host %s", cluster.Hosts[i].RequestedHostname)
			continue
		}
		if installErr := b.hostApi.Install(ctx, cluster.Hosts[i], tx); installErr != nil {
			// we just logs the error, each host install is independent
			log.Errorf("Failed to move host %s to installing", cluster.Hosts[i].RequestedHostname)
		}
	}

	err = tx.Commit().Error
	if err != nil {
		log.Error(err)
		return common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	return installer.NewInstallHostsAccepted().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) setBootstrapHost(ctx context.Context, cluster common.Cluster, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)

	// check if cluster already has bootstrap
	for _, h := range cluster.Hosts {
		if h.Bootstrap {
			log.Infof("Bootstrap ID is %s", h.ID)
			return nil
		}
	}

	masterNodesIds, err := b.clusterApi.GetMasterNodesIds(ctx, &cluster, transaction.AddForUpdateQueryOption(db))
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster %s master node id's", cluster.ID)
		return errors.Wrapf(err, "Failed to get cluster %s master node id's", cluster.ID)
	}
	if len(masterNodesIds) == 0 {
		return errors.Errorf("Cluster have no master hosts that can operate as bootstrap")
	}
	bootstrapId := masterNodesIds[len(masterNodesIds)-1]
	log.Infof("Bootstrap ID is %s", bootstrapId)
	for i := range cluster.Hosts {
		if cluster.Hosts[i].ID.String() == bootstrapId.String() {
			err = b.hostApi.SetBootstrap(ctx, cluster.Hosts[i], true, db)
			if err != nil {
				log.WithError(err).Errorf("failed to update bootstrap host for cluster %s", cluster.ID)
				return errors.Wrapf(err, "Failed to update bootstrap host for cluster %s", cluster.ID)
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) GetClusterInstallConfig(ctx context.Context, params installer.GetClusterInstallConfigParams) middleware.Responder {
	return b.V2GetClusterInstallConfig(ctx, installer.V2GetClusterInstallConfigParams(params))
}

func (b *bareMetalInventory) GetClusterDefaultConfig(_ context.Context, _ installer.GetClusterDefaultConfigParams) middleware.Responder {
	return installer.NewGetClusterDefaultConfigOK().WithPayload(b.getClusterDefaultConfig())
}

func (b *bareMetalInventory) getClusterDefaultConfig() *models.ClusterDefaultConfig {
	body := &models.ClusterDefaultConfig{}

	body.NtpSource = b.Config.DefaultNTPSource
	body.ClusterNetworkCidr = b.Config.DefaultClusterNetworkCidr
	body.ServiceNetworkCidr = b.Config.DefaultServiceNetworkCidr
	body.ClusterNetworkHostPrefix = b.Config.DefaultClusterNetworkHostPrefix
	body.InactiveDeletionHours = int64(b.gcConfig.DeregisterInactiveAfter.Hours())

	return body
}

func (b *bareMetalInventory) TransformClusterToDay2Internal(ctx context.Context, clusterID strfmt.UUID) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("transforming day1 cluster %s into day2 cluster", clusterID)

	var cluster *common.Cluster
	var err error

	if cluster, err = common.GetClusterFromDB(b.db, clusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", clusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewApiError(http.StatusNotFound, err)
		}
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.clusterApi.TransformClusterToDay2(ctx, cluster, b.db)
	if err != nil {
		return nil, err
	}

	return b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: clusterID})
}

func (b *bareMetalInventory) GetClusterSupportedPlatformsInternal(
	ctx context.Context, params installer.GetClusterSupportedPlatformsParams) (*[]models.PlatformType, error) {
	cluster, err := b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: params.ClusterID})
	if err != nil {
		return nil, fmt.Errorf("error getting cluster, error: %w", err)
	}
	// no hosts or SNO
	if len(cluster.Hosts) == 0 || swag.StringValue(cluster.HighAvailabilityMode) != models.ClusterHighAvailabilityModeFull {
		return &[]models.PlatformType{models.PlatformTypeBaremetal}, nil
	}
	hostSupportedPlatforms, err := b.providerRegistry.GetSupportedProvidersByHosts(cluster.Hosts)
	if err != nil {
		return nil, fmt.Errorf(
			"error while checking supported platforms, error: %w", err)
	}
	return &hostSupportedPlatforms, nil
}

func (b *bareMetalInventory) GetClusterSupportedPlatforms(ctx context.Context, params installer.GetClusterSupportedPlatformsParams) middleware.Responder {
	supportedPlatforms, err := b.GetClusterSupportedPlatformsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetClusterSupportedPlatformsOK().WithPayload(*supportedPlatforms)
}

func (b *bareMetalInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	return b.V2UpdateClusterInstallConfig(ctx, installer.V2UpdateClusterInstallConfigParams(params))
}

func (b *bareMetalInventory) UpdateClusterInstallConfigInternal(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	query := "id = ?"

	err := b.db.First(&cluster, query, params.ClusterID).Error
	if err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		return nil, err
	}

	if err = b.installConfigBuilder.ValidateInstallConfigPatch(&cluster, params.InstallConfigParams); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	err = b.db.Model(&common.Cluster{}).Where(query, params.ClusterID).Update("install_config_overrides", params.InstallConfigParams).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendInstallConfigAppliedEvent(ctx, b.eventsHandler, params.ClusterID)
	log.Infof("Custom install config was applied to cluster %s", params.ClusterID)
	return &cluster, nil
}

func (b *bareMetalInventory) generateClusterInstallConfig(ctx context.Context, cluster common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)

	cfg, err := b.installConfigBuilder.GetInstallConfig(&cluster, b.Config.InstallRHCa, ignition.RedhatRootCA)
	if err != nil {
		log.WithError(err).Errorf("failed to get install config for cluster %s", cluster.ID)
		return errors.Wrapf(err, "failed to get install config for cluster %s", cluster.ID)
	}

	releaseImage, err := b.versionsHandler.GetReleaseImage(cluster.OpenshiftVersion, cluster.CPUArchitecture)
	if err != nil {
		msg := fmt.Sprintf("failed to get OpenshiftVersion for cluster %s with openshift version %s", cluster.ID, cluster.OpenshiftVersion)
		log.WithError(err).Errorf(msg)
		return errors.Wrapf(err, msg)
	}

	if err := b.generator.GenerateInstallConfig(ctx, cluster, cfg, *releaseImage.URL); err != nil {
		msg := fmt.Sprintf("failed generating install config for cluster %s", cluster.ID)
		log.WithError(err).Error(msg)
		return errors.Wrap(err, msg)
	}

	return nil
}

func (b *bareMetalInventory) refreshClusterHosts(ctx context.Context, cluster *common.Cluster, tx *gorm.DB, log logrus.FieldLogger) error {
	err := b.setMajorityGroupForCluster(cluster.ID, tx)
	if err != nil {
		log.WithError(err).Errorf("Failed to set cluster %s majority groups", cluster.ID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	// Cluster is retrieved from DB to make sure we operate on the most recent information regarding monitored operators
	// enabled for the cluster.
	dbCluster, err := common.GetClusterFromDB(tx, *cluster.ID, common.UseEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("not refreshing cluster hosts - failed to find cluster %s", *cluster.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	for _, dbHost := range dbCluster.Hosts {
		var err error

		// Refresh inventory - especially disk eligibility. The host requirements might have changed.
		// dbHost object might be updated with the latest disk eligibility information.
		err = b.refreshInventory(ctx, dbCluster, dbHost, tx)
		if err != nil {
			return err
		}

		if err = b.hostApi.RefreshStatus(ctx, dbHost, tx); err != nil {
			log.WithError(err).Errorf("failed to refresh state of host %s cluster %s", *dbHost.ID, cluster.ID.String())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) refreshInventory(ctx context.Context, cluster *common.Cluster, host *models.Host, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if host.Inventory != "" {
		err := b.hostApi.RefreshInventory(ctx, cluster, host, db)
		if err != nil {
			log.WithError(err).Errorf("failed to update inventory of host %s cluster %s", host.ID, cluster.ID.String())
			switch err := err.(type) {
			case *common.ApiErrorResponse:
				if err.StatusCode() != http.StatusConflict {
					return err
				}
				// In RefreshInventory there is a precondition on host's status that on failure returns StatusConflict.
				// In case of cluster update we don't want to fail the whole update if host can't be, according to
				// business rules, updated. An example of such case is disabled host.
				log.Infof("ignoring wrong status error (%v) for host %s in cluster %s", err, host.ID, cluster.ID.String())
			default:
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) noneHaModeClusterUpdateValidations(cluster *common.Cluster, params installer.UpdateClusterParams) error {
	if swag.StringValue(cluster.HighAvailabilityMode) != models.ClusterHighAvailabilityModeNone {
		return nil
	}

	if len(params.ClusterUpdateParams.HostsRoles) > 0 {
		return errors.Errorf("setting host role is not allowed in single node mode")
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && !swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) {
		return errors.Errorf("disabling UserManagedNetworking is not allowed in single node mode")
	}

	return nil
}

func (b *bareMetalInventory) v2NoneHaModeClusterUpdateValidations(cluster *common.Cluster, params installer.V2UpdateClusterParams) error {
	if swag.StringValue(cluster.HighAvailabilityMode) != models.ClusterHighAvailabilityModeNone {
		return nil
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && !swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) {
		return errors.Errorf("disabling UserManagedNetworking is not allowed in single node mode")
	}

	return nil
}

func (b *bareMetalInventory) validateAndUpdateClusterParams(ctx context.Context, params *installer.V2UpdateClusterParams) (installer.V2UpdateClusterParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if swag.StringValue(params.ClusterUpdateParams.PullSecret) != "" {
		if err := b.secretValidator.ValidatePullSecret(*params.ClusterUpdateParams.PullSecret, ocm.UserNameFromContext(ctx), b.authHandler); err != nil {
			log.WithError(err).Errorf("Pull secret for cluster %s is invalid", params.ClusterID)
			return installer.V2UpdateClusterParams{}, err
		}
		ps, errUpdate := b.updatePullSecret(*params.ClusterUpdateParams.PullSecret, log)
		if errUpdate != nil {
			return installer.V2UpdateClusterParams{}, errors.New("Failed to update Pull-secret with additional credentials")
		}
		params.ClusterUpdateParams.PullSecret = &ps
	}

	if swag.StringValue(params.ClusterUpdateParams.Name) != "" {
		if err := validations.ValidateClusterNameFormat(*params.ClusterUpdateParams.Name); err != nil {
			return installer.V2UpdateClusterParams{}, err
		}
	}

	if err := validations.ValidateIPAddresses(b.IPv6Support, params.ClusterUpdateParams); err != nil {
		return installer.V2UpdateClusterParams{}, common.NewApiError(http.StatusBadRequest, err)
	}

	if sshPublicKey := swag.StringValue(params.ClusterUpdateParams.SSHPublicKey); sshPublicKey != "" {
		sshPublicKey = strings.TrimSpace(sshPublicKey)
		if err := validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
			return installer.V2UpdateClusterParams{}, err
		}
		*params.ClusterUpdateParams.SSHPublicKey = sshPublicKey
	}

	if err := b.validateIgnitionEndpointURL(params.ClusterUpdateParams.IgnitionEndpoint, log); err != nil {
		return installer.V2UpdateClusterParams{}, err
	}

	return *params, nil
}

func (b *bareMetalInventory) validateAndUpdateProxyParams(ctx context.Context, params *installer.V2UpdateClusterParams, ocpVersion *string) (installer.V2UpdateClusterParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if params.ClusterUpdateParams.HTTPProxy != nil &&
		(params.ClusterUpdateParams.HTTPSProxy == nil || *params.ClusterUpdateParams.HTTPSProxy == "") {
		params.ClusterUpdateParams.HTTPSProxy = params.ClusterUpdateParams.HTTPProxy
	}

	if err := validateProxySettings(params.ClusterUpdateParams.HTTPProxy,
		params.ClusterUpdateParams.HTTPSProxy,
		params.ClusterUpdateParams.NoProxy, ocpVersion); err != nil {
		log.WithError(err).Errorf("Failed to validate Proxy settings")
		return installer.V2UpdateClusterParams{}, err
	}

	return *params, nil
}

func (b *bareMetalInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	c, err := b.updateClusterInternal(ctx, params, Interactive)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) V2UpdateCluster(ctx context.Context, params installer.V2UpdateClusterParams) middleware.Responder {
	c, err := b.v2UpdateClusterInternal(ctx, params, Interactive)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) UpdateClusterNonInteractive(ctx context.Context, params installer.V2UpdateClusterParams) (*common.Cluster, error) {
	return b.v2UpdateClusterInternal(ctx, params, NonInteractive)
}

func (b *bareMetalInventory) updateClusterInternal(ctx context.Context, v1Params installer.UpdateClusterParams, interactivity Interactivity) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var err error
	log.Infof("update cluster %s with v2Params: %+v", v1Params.ClusterID, v1Params.ClusterUpdateParams)

	v2Params := installer.V2UpdateClusterParams{
		HTTPRequest: v1Params.HTTPRequest,
		ClusterID:   v1Params.ClusterID,
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			AdditionalNtpSource:      v1Params.ClusterUpdateParams.AdditionalNtpSource,
			APIVip:                   v1Params.ClusterUpdateParams.APIVip,
			APIVipDNSName:            v1Params.ClusterUpdateParams.APIVipDNSName,
			BaseDNSDomain:            v1Params.ClusterUpdateParams.BaseDNSDomain,
			ClusterNetworkCidr:       v1Params.ClusterUpdateParams.ClusterNetworkCidr,
			ClusterNetworkHostPrefix: v1Params.ClusterUpdateParams.ClusterNetworkHostPrefix,
			ClusterNetworks:          v1Params.ClusterUpdateParams.ClusterNetworks,
			DiskEncryption:           v1Params.ClusterUpdateParams.DiskEncryption,
			HTTPProxy:                v1Params.ClusterUpdateParams.HTTPProxy,
			HTTPSProxy:               v1Params.ClusterUpdateParams.HTTPSProxy,
			Hyperthreading:           v1Params.ClusterUpdateParams.Hyperthreading,
			IngressVip:               v1Params.ClusterUpdateParams.IngressVip,
			MachineNetworkCidr:       v1Params.ClusterUpdateParams.MachineNetworkCidr,
			MachineNetworks:          v1Params.ClusterUpdateParams.MachineNetworks,
			Name:                     v1Params.ClusterUpdateParams.Name,
			NetworkType:              v1Params.ClusterUpdateParams.NetworkType,
			NoProxy:                  v1Params.ClusterUpdateParams.NoProxy,
			OlmOperators:             v1Params.ClusterUpdateParams.OlmOperators,
			Platform:                 v1Params.ClusterUpdateParams.Platform,
			PullSecret:               v1Params.ClusterUpdateParams.PullSecret,
			SchedulableMasters:       v1Params.ClusterUpdateParams.SchedulableMasters,
			ServiceNetworkCidr:       v1Params.ClusterUpdateParams.ServiceNetworkCidr,
			ServiceNetworks:          v1Params.ClusterUpdateParams.ServiceNetworks,
			SSHPublicKey:             v1Params.ClusterUpdateParams.SSHPublicKey,
			UserManagedNetworking:    v1Params.ClusterUpdateParams.UserManagedNetworking,
			VipDhcpAllocation:        v1Params.ClusterUpdateParams.VipDhcpAllocation,
			IgnitionEndpoint:         v1Params.ClusterUpdateParams.IgnitionEndpoint,
		},
	}

	if v2Params, err = b.validateAndUpdateClusterParams(ctx, &v2Params); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return nil, common.NewApiError(http.StatusInternalServerError,
			errors.New("DB error, failed to start transaction"))
	}

	// in case host monitor already updated the state we need to use FOR UPDATE option
	if cluster, err = common.GetClusterFromDBForUpdate(tx, v2Params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", v2Params.ClusterID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	alreadyDualStack := network.CheckIfClusterIsDualStack(cluster)
	if err = validations.ValidateDualStackNetworks(v2Params.ClusterUpdateParams, alreadyDualStack); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if v2Params, err = b.validateAndUpdateProxyParams(ctx, &v2Params, &cluster.OpenshiftVersion); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.clusterApi.VerifyClusterUpdatability(cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", v2Params.ClusterID)
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	if err = b.noneHaModeClusterUpdateValidations(cluster, v1Params); err != nil {
		log.WithError(err).Warnf("Unsupported update v2Params in none ha mode")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateDNSDomain(*cluster, v2Params, log); err != nil {
		return nil, err
	}

	if err = b.validateIgnitionEndpointURL(v2Params.ClusterUpdateParams.IgnitionEndpoint, log); err != nil {
		return nil, err
	}

	if err = validations.ValidateDiskEncryptionParams(v2Params.ClusterUpdateParams.DiskEncryption, b.DiskEncryptionSupport); err != nil {
		log.WithError(err).Errorf("failed to validate disk-encryption params: %v", *v2Params.ClusterUpdateParams.DiskEncryption)
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	usages, err := usage.Unmarshal(cluster.Cluster.FeatureUsage)
	if err != nil {
		log.WithError(err).Errorf("failed to read feature usage from cluster %s", v2Params.ClusterID)
		return nil, err
	}

	err = b.updateClusterData(ctx, cluster, v2Params, usages, tx, log, interactivity)
	if err != nil {
		log.WithError(err).Error("updateClusterData")
		return nil, err
	}

	err = b.updateHostsData(ctx, v1Params, usages, tx, log)
	if err != nil {
		return nil, err
	}

	err = b.updateOperatorsData(ctx, cluster, v2Params, usages, tx, log)
	if err != nil {
		return nil, err
	}

	err = b.updateHostsAndClusterStatus(ctx, cluster, tx, log)
	if err != nil {
		return nil, err
	}

	b.updateClusterNetworkVMUsage(cluster, v2Params.ClusterUpdateParams, usages, log)

	b.updateClusterCPUFeatureUsage(cluster, usages)

	b.usageApi.Save(tx, v2Params.ClusterID, usages)

	newClusterName := swag.StringValue(v2Params.ClusterUpdateParams.Name)
	if b.ocmClient != nil && newClusterName != "" && newClusterName != cluster.Name {
		if err = b.integrateWithAMSClusterUpdateName(ctx, cluster, *v2Params.ClusterUpdateParams.Name); err != nil {
			log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster update with new name %s", v2Params.ClusterID, newClusterName)
			return nil, err
		}
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("DB error, failed to commit"))
	}
	txSuccess = true

	if proxySettingsChanged(v2Params.ClusterUpdateParams, cluster) {
		eventgen.SendProxySettingsChangedEvent(ctx, b.eventsHandler, v2Params.ClusterID)
	}

	if cluster, err = common.GetClusterFromDB(b.db, v2Params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", v2Params.ClusterID)
		return nil, err
	}

	cluster.HostNetworks = b.calculateHostNetworks(log, cluster)
	for _, host := range cluster.Hosts {
		if err = b.customizeHost(&cluster.Cluster, host); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

	imageInfo, err := b.getImageInfo(cluster.ID)
	if err != nil {
		return nil, err
	}
	cluster.ImageInfo = imageInfo

	return cluster, nil
}

func (b *bareMetalInventory) v2UpdateClusterInternal(ctx context.Context, params installer.V2UpdateClusterParams, interactivity Interactivity) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var err error
	log.Infof("update cluster %s with params: %+v", params.ClusterID, params.ClusterUpdateParams)

	if params, err = b.validateAndUpdateClusterParams(ctx, &params); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return nil, common.NewApiError(http.StatusInternalServerError,
			errors.New("DB error, failed to start transaction"))
	}

	// in case host monitor already updated the state we need to use FOR UPDATE option
	if cluster, err = common.GetClusterFromDBForUpdate(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	alreadyDualStack := network.CheckIfClusterIsDualStack(cluster)
	if err = validations.ValidateDualStackNetworks(params.ClusterUpdateParams, alreadyDualStack); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if params, err = b.validateAndUpdateProxyParams(ctx, &params, &cluster.OpenshiftVersion); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.clusterApi.VerifyClusterUpdatability(cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", params.ClusterID)
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	if err = b.v2NoneHaModeClusterUpdateValidations(cluster, params); err != nil {
		log.WithError(err).Warnf("Unsupported update params in none ha mode")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateDNSDomain(*cluster, params, log); err != nil {
		return nil, err
	}

	if err = validations.ValidateDiskEncryptionParams(params.ClusterUpdateParams.DiskEncryption, b.DiskEncryptionSupport); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	usages, err := usage.Unmarshal(cluster.Cluster.FeatureUsage)
	if err != nil {
		log.WithError(err).Errorf("failed to read feature usage from cluster %s", params.ClusterID)
		return nil, err
	}

	err = b.updateClusterData(ctx, cluster, params, usages, tx, log, interactivity)
	if err != nil {
		log.WithError(err).Error("updateClusterData")
		return nil, err
	}

	err = b.updateOperatorsData(ctx, cluster, params, usages, tx, log)
	if err != nil {
		return nil, err
	}

	if _, err = b.clusterApi.RefreshStatus(ctx, cluster, tx); err != nil {
		log.WithError(err).Errorf("failed to validate or update cluster %s state", cluster.ID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	b.updateClusterNetworkVMUsage(cluster, params.ClusterUpdateParams, usages, log)

	b.updateClusterCPUFeatureUsage(cluster, usages)

	b.usageApi.Save(tx, params.ClusterID, usages)

	newClusterName := swag.StringValue(params.ClusterUpdateParams.Name)
	if b.ocmClient != nil && newClusterName != "" && newClusterName != cluster.Name {
		if err = b.integrateWithAMSClusterUpdateName(ctx, cluster, *params.ClusterUpdateParams.Name); err != nil {
			log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster update with new name %s", params.ClusterID, newClusterName)
			return nil, err
		}
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("DB error, failed to commit"))
	}
	txSuccess = true

	if proxySettingsChanged(params.ClusterUpdateParams, cluster) {
		eventgen.SendProxySettingsChangedEvent(ctx, b.eventsHandler, params.ClusterID)
	}

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return nil, err
	}

	cluster.HostNetworks = b.calculateHostNetworks(log, cluster)

	return cluster, nil
}

func (b *bareMetalInventory) integrateWithAMSClusterUpdateName(ctx context.Context, cluster *common.Cluster, newClusterName string) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Updating AMS subscription for cluster %s with new name %s", *cluster.ID, newClusterName)
	if err := b.ocmClient.AccountsMgmt.UpdateSubscriptionDisplayName(ctx, cluster.AmsSubscriptionID, newClusterName); err != nil {
		log.WithError(err).Errorf("Failed to update AMS subscription with the new cluster name %s for cluster %s", newClusterName, *cluster.ID)
		return err
	}
	return nil
}

func (b *bareMetalInventory) updateNonDhcpNetworkParams(updates map[string]interface{}, cluster *common.Cluster, params installer.V2UpdateClusterParams, log logrus.FieldLogger, interactivity Interactivity) error {
	apiVip := cluster.APIVip
	ingressVip := cluster.IngressVip

	// We are checking if the cluster is requested to be a dual-stack cluster based on the Cluster
	// Networks provided.
	//
	// TODO(mko) As updateNonDhcpNetworkParams is called before updateNetworks, this check looks
	//           at the already configured networks and not the ones inside V2UpdateClusterParams.
	//           An extension would be to check if V2UpdateClusterParams configures any of those
	//           and if that's the case, instead of GetConfiguredAddressFamilies(cluster) we
	//           should call something like GetRequestedAddressFamilies(cluster).
	reqV4, reqV6, _ := network.GetConfiguredAddressFamilies(cluster)
	reqDualStack := reqV4 && reqV6

	if params.ClusterUpdateParams.APIVip != nil {
		updates["api_vip"] = *params.ClusterUpdateParams.APIVip
		apiVip = *params.ClusterUpdateParams.APIVip
	}
	if params.ClusterUpdateParams.IngressVip != nil {
		updates["ingress_vip"] = *params.ClusterUpdateParams.IngressVip
		ingressVip = *params.ClusterUpdateParams.IngressVip
	}
	if params.ClusterUpdateParams.MachineNetworks != nil &&
		common.IsSliceNonEmpty(params.ClusterUpdateParams.MachineNetworks) &&
		!reqDualStack {
		err := errors.New("Setting Machine network CIDR is forbidden when cluster is not in vip-dhcp-allocation mode")
		log.WithError(err).Warnf("Set Machine Network CIDR")
		return common.NewApiError(http.StatusBadRequest, err)
	}
	var err error
	err = verifyParsableVIPs(apiVip, ingressVip)
	if err != nil {
		log.WithError(err).Errorf("Failed validating VIPs of cluster id=%s", params.ClusterID)
		return err
	}

	err = network.VerifyDifferentVipAddresses(apiVip, ingressVip)
	if err != nil {
		log.WithError(err).Errorf("VIP verification failed for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if interactivity == Interactive && (params.ClusterUpdateParams.APIVip != nil || params.ClusterUpdateParams.IngressVip != nil) {
		var primaryMachineNetworkCidr string
		matchRequired := apiVip != "" || ingressVip != ""
		primaryMachineNetworkCidr, err = network.CalculateMachineNetworkCIDR(apiVip, ingressVip, cluster.Hosts, matchRequired)
		if err != nil {
			return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Calculate machine network CIDR"))
		}
		if primaryMachineNetworkCidr != "" {
			if network.IsMachineCidrAvailable(cluster) {
				cluster.MachineNetworks[0].Cidr = models.Subnet(primaryMachineNetworkCidr)
			} else {
				cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: models.Subnet(primaryMachineNetworkCidr)}}
			}
			updates["machine_network_cidr"] = primaryMachineNetworkCidr
		}
		err = network.VerifyVips(cluster.Hosts, primaryMachineNetworkCidr, apiVip, ingressVip, false, log)
		if err != nil {
			log.WithError(err).Warnf("Verify VIPs")
			return common.NewApiError(http.StatusBadRequest, err)
		}

		if params.ClusterUpdateParams.MachineNetworks != nil {
			err = network.VerifyMachineNetworksDualStack(params.ClusterUpdateParams.MachineNetworks, reqDualStack)
			if err != nil {
				log.WithError(err).Warnf("Verify dual-stack machine networks")
				return common.NewApiError(http.StatusBadRequest, err)
			}
		} else {
			err = network.VerifyMachineNetworksDualStack(cluster.MachineNetworks, reqDualStack)
			if err != nil {
				log.WithError(err).Warnf("Verify dual-stack machine networks")
				return common.NewApiError(http.StatusBadRequest, err)
			}
		}
	}

	return nil
}

func verifyParsableVIPs(apiVip string, ingressVip string) error {
	if apiVip != "" && net.ParseIP(apiVip) == nil {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Could not parse VIP ip %s", apiVip))
	}
	if ingressVip != "" && net.ParseIP(ingressVip) == nil {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Could not parse VIP ip %s", ingressVip))
	}
	return nil
}

func (b *bareMetalInventory) updateDhcpNetworkParams(updates map[string]interface{}, params installer.V2UpdateClusterParams, primaryMachineCIDR string, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.APIVip != nil {
		err := errors.New("Setting API VIP is forbidden when cluster is in vip-dhcp-allocation mode")
		log.WithError(err).Warnf("Set API VIP")
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if params.ClusterUpdateParams.IngressVip != nil {
		err := errors.New("Setting Ingress VIP is forbidden when cluster is in vip-dhcp-allocation mode")
		log.WithError(err).Warnf("Set Ingress VIP")
		return common.NewApiError(http.StatusBadRequest, err)
	}
	// VIPs are always allocated from the first provided machine network. We want to trigger
	// their reallocation only when this network has changed, but not in other cases.
	//
	// TODO(mko) Use `common.IsSliceNonEmpty` instead of double `!= nil` check. Deferring it for
	// later so that this commit can be cherry-picked if needed as a hotfix without dependency on
	// the one implementing `common.IsSliceNonEmpty`.
	// Ref.: https://bugzilla.redhat.com/show_bug.cgi?id=1999297
	// Ref.: https://github.com/openshift/assisted-service/pull/2512
	if params.ClusterUpdateParams.MachineNetworks != nil && params.ClusterUpdateParams.MachineNetworks[0] != nil && string(params.ClusterUpdateParams.MachineNetworks[0].Cidr) != primaryMachineCIDR {
		updates["api_vip"] = ""
		updates["ingress_vip"] = ""
	}
	return nil
}

func (b *bareMetalInventory) setDiskEncryptionUsage(c *models.Cluster, diskEncryption *models.DiskEncryption, usages map[string]models.Usage) {

	if c.DiskEncryption == nil || swag.StringValue(c.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone {
		return
	}

	props := map[string]interface{}{}
	if diskEncryption.EnableOn != nil {
		props["enable_on"] = swag.StringValue(diskEncryption.EnableOn)
	}
	if diskEncryption.Mode != nil {
		props["mode"] = swag.StringValue(diskEncryption.Mode)
		props["tang_servers"] = diskEncryption.TangServers
	}
	b.setUsage(swag.StringValue(c.DiskEncryption.EnableOn) != models.DiskEncryptionEnableOnNone, usage.DiskEncryption, &props, usages)
}

func (b *bareMetalInventory) updateClusterData(_ context.Context, cluster *common.Cluster, params installer.V2UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger, interactivity Interactivity) error {
	var err error
	updates := map[string]interface{}{}
	optionalParam(params.ClusterUpdateParams.Name, "name", updates)
	optionalParam(params.ClusterUpdateParams.BaseDNSDomain, "base_dns_domain", updates)
	optionalParam(params.ClusterUpdateParams.HTTPProxy, "http_proxy", updates)
	optionalParam(params.ClusterUpdateParams.HTTPSProxy, "https_proxy", updates)
	optionalParam(params.ClusterUpdateParams.NoProxy, "no_proxy", updates)
	optionalParam(params.ClusterUpdateParams.SSHPublicKey, "ssh_public_key", updates)
	optionalParam(params.ClusterUpdateParams.Hyperthreading, "hyperthreading", updates)

	b.setProxyUsage(params.ClusterUpdateParams.HTTPProxy, params.ClusterUpdateParams.HTTPSProxy, params.ClusterUpdateParams.NoProxy, usages)

	if err = b.updateProviderParams(params, updates, usages); err != nil {
		return err
	}

	if err = updateNetworkParamsCompatiblityPropagation(params, cluster); err != nil {
		return err
	}

	if err = b.updateNetworkParams(params, cluster, updates, usages, db, log, interactivity); err != nil {
		return err
	}

	if err = b.updateNtpSources(params, updates, usages, log); err != nil {
		return err
	}

	if params.ClusterUpdateParams.PullSecret != nil {
		cluster.PullSecret = *params.ClusterUpdateParams.PullSecret
		updates["pull_secret"] = *params.ClusterUpdateParams.PullSecret
		if cluster.PullSecret != "" {
			updates["pull_secret_set"] = true
		} else {
			updates["pull_secret_set"] = false
		}
	}

	if params.ClusterUpdateParams.APIVipDNSName != nil {
		if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
			log.Infof("Updating api vip to %s for day2 cluster %s", *params.ClusterUpdateParams.APIVipDNSName, cluster.ID)
			updates["api_vip_dns_name"] = *params.ClusterUpdateParams.APIVipDNSName
		} else {
			msg := fmt.Sprintf("Can't update api vip to %s for day1 cluster %s", *params.ClusterUpdateParams.APIVipDNSName, cluster.ID)
			log.Error(msg)
			return common.NewApiError(http.StatusBadRequest, errors.Errorf(msg))
		}
	}
	if params.ClusterUpdateParams.SchedulableMasters != nil {
		value := swag.BoolValue(params.ClusterUpdateParams.SchedulableMasters)
		updates["schedulable_masters"] = value
		b.setUsage(value, usage.SchedulableMasters, nil, usages)
	}

	if params.ClusterUpdateParams.DiskEncryption != nil {
		if params.ClusterUpdateParams.DiskEncryption.EnableOn != nil {
			updates["disk_encryption_enable_on"] = params.ClusterUpdateParams.DiskEncryption.EnableOn
		}
		if params.ClusterUpdateParams.DiskEncryption.Mode != nil {
			updates["disk_encryption_mode"] = params.ClusterUpdateParams.DiskEncryption.Mode
		}
		if params.ClusterUpdateParams.DiskEncryption.TangServers != "" {
			updates["disk_encryption_tang_servers"] = params.ClusterUpdateParams.DiskEncryption.TangServers
		}
		b.setDiskEncryptionUsage(&cluster.Cluster, params.ClusterUpdateParams.DiskEncryption, usages)
	}

	if params.ClusterUpdateParams.IgnitionEndpoint != nil {
		if params.ClusterUpdateParams.IgnitionEndpoint.URL != nil {
			optionalParam(params.ClusterUpdateParams.IgnitionEndpoint.URL, "ignition_endpoint_url", updates)
		}
		if params.ClusterUpdateParams.IgnitionEndpoint.CaCertificate != nil {
			optionalParam(params.ClusterUpdateParams.IgnitionEndpoint.CaCertificate, "ignition_endpoint_ca_certificate", updates)
		}
	}

	if len(updates) > 0 {
		updates["trigger_monitor_timestamp"] = time.Now()
		dbReply := db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
		if dbReply.Error != nil {
			return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "failed to update cluster: %s", params.ClusterID))
		}
	}

	return nil
}

func (b *bareMetalInventory) updateNetworks(db *gorm.DB, params installer.V2UpdateClusterParams, updates map[string]interface{},
	cluster *common.Cluster, userManagedNetworking, vipDhcpAllocation bool) error {
	var err error

	if params.ClusterUpdateParams.ClusterNetworks != nil {
		for _, clusterNetwork := range params.ClusterUpdateParams.ClusterNetworks {
			if err = network.VerifyClusterOrServiceCIDR(string(clusterNetwork.Cidr)); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Cluster network CIDR %s", string(clusterNetwork.Cidr)))
			}

			if err = network.VerifyNetworkHostPrefix(clusterNetwork.HostPrefix); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Cluster network host prefix %d", clusterNetwork.HostPrefix))
			}

			err = network.VerifyClusterCidrSize(int(clusterNetwork.HostPrefix), string(clusterNetwork.Cidr), len(cluster.Hosts))
			if err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Cluster CIDR size"))
			}
		}
		cluster.ClusterNetworks = params.ClusterUpdateParams.ClusterNetworks

		// TODO MGMT-7365: Deprecate single network
		if common.IsSliceNonEmpty(params.ClusterUpdateParams.ClusterNetworks) {
			updates["cluster_network_cidr"] = string(params.ClusterUpdateParams.ClusterNetworks[0].Cidr)
			updates["cluster_network_host_prefix"] = params.ClusterUpdateParams.ClusterNetworks[0].HostPrefix
		} else {
			updates["cluster_network_cidr"] = ""
			updates["cluster_network_host_prefix"] = 0
		}
	}

	if params.ClusterUpdateParams.ServiceNetworks != nil {
		for _, serviceNetwork := range params.ClusterUpdateParams.ServiceNetworks {
			if err = network.VerifyClusterOrServiceCIDR(string(serviceNetwork.Cidr)); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Service network CIDR %s", string(serviceNetwork.Cidr)))
			}
		}
		cluster.ServiceNetworks = params.ClusterUpdateParams.ServiceNetworks

		// TODO MGMT-7365: Deprecate single network
		if common.IsSliceNonEmpty(params.ClusterUpdateParams.ServiceNetworks) {
			updates["service_network_cidr"] = string(params.ClusterUpdateParams.ServiceNetworks[0].Cidr)
		} else {
			updates["service_network_cidr"] = ""
		}
	}

	if params.ClusterUpdateParams.MachineNetworks != nil {
		for _, machineNetwork := range params.ClusterUpdateParams.MachineNetworks {
			if err = network.VerifyMachineCIDR(string(machineNetwork.Cidr)); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Machine network CIDR %s", string(machineNetwork.Cidr)))
			}
		}
		cluster.MachineNetworks = params.ClusterUpdateParams.MachineNetworks

		// TODO MGMT-7365: Deprecate single network
		if common.IsSliceNonEmpty(params.ClusterUpdateParams.MachineNetworks) {
			updates["machine_network_cidr"] = string(params.ClusterUpdateParams.MachineNetworks[0].Cidr)
		} else {
			updates["machine_network_cidr"] = ""
		}
	}

	if common.IsSliceNonEmpty(params.ClusterUpdateParams.MachineNetworks) {
		if err = validations.ValidateVipDHCPAllocationWithIPv6(vipDhcpAllocation, string(cluster.MachineNetworks[0].Cidr)); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.ClusterUpdateParams.ClusterNetworks != nil || params.ClusterUpdateParams.ServiceNetworks != nil ||
		params.ClusterUpdateParams.MachineNetworks != nil {
		// TODO MGMT-7587: Support any number of subnets
		// Assumes that the number of cluster networks equal to the number of service networks
		for index := range cluster.ClusterNetworks {
			machineNetworkCidr := ""
			if len(cluster.MachineNetworks) > index {
				machineNetworkCidr = string(cluster.MachineNetworks[index].Cidr)
			}

			serviceNetworkCidr := ""
			if len(cluster.ServiceNetworks) > index {
				serviceNetworkCidr = string(cluster.ServiceNetworks[index].Cidr)
			}

			if err = network.VerifyClusterCIDRsNotOverlap(machineNetworkCidr,
				string(cluster.ClusterNetworks[index].Cidr),
				serviceNetworkCidr,
				userManagedNetworking); err != nil {
				return common.NewApiError(http.StatusBadRequest, err)
			}
		}
	}

	return b.updateNetworkTables(db, cluster, params)
}

func (b *bareMetalInventory) updateNetworkTables(db *gorm.DB, cluster *common.Cluster, params installer.V2UpdateClusterParams) error {
	var err error

	if params.ClusterUpdateParams.ClusterNetworks != nil {
		if err = db.Where("cluster_id = ?", *cluster.ID).Delete(&models.ClusterNetwork{}).Error; err != nil {
			err = errors.Wrapf(err, "failed to delete cluster networks of cluster %s", *cluster.ID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		for _, clusterNetwork := range cluster.ClusterNetworks {
			clusterNetwork.ClusterID = *cluster.ID
			if err = db.Save(clusterNetwork).Error; err != nil {
				err = errors.Wrapf(err, "failed to update cluster network %v of cluster %s", *clusterNetwork, *cluster.ID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}
	if params.ClusterUpdateParams.ServiceNetworks != nil {
		if err = db.Where("cluster_id = ?", *cluster.ID).Delete(&models.ServiceNetwork{}).Error; err != nil {
			err = errors.Wrapf(err, "failed to delete service networks of cluster %s", *cluster.ID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		for _, serviceNetwork := range cluster.ServiceNetworks {
			serviceNetwork.ClusterID = *cluster.ID
			if err = db.Save(serviceNetwork).Error; err != nil {
				err = errors.Wrapf(err, "failed to update service network %v of cluster %s", *serviceNetwork, params.ClusterID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}

	// TODO: Update machine CIDR only if necessary
	// The machine cidr can be resetted, calculated and provided by the user
	if err = db.Where("cluster_id = ?", *cluster.ID).Delete(&models.MachineNetwork{}).Error; err != nil {
		err = errors.Wrapf(err, "failed to delete machine networks of cluster %s", *cluster.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	for _, machineNetwork := range cluster.MachineNetworks {
		machineNetwork.ClusterID = *cluster.ID
		if err = db.Save(machineNetwork).Error; err != nil {
			err = errors.Wrapf(err, "failed to update machine network %v of cluster %s", *machineNetwork, params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	return nil
}

func (b *bareMetalInventory) updateProviderParams(params installer.V2UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage) error {
	if params.ClusterUpdateParams.Platform != nil && common.PlatformTypeValue(params.ClusterUpdateParams.Platform.Type) != "" {
		err := b.providerRegistry.SetPlatformValuesInDBUpdates(
			common.PlatformTypeValue(params.ClusterUpdateParams.Platform.Type), params.ClusterUpdateParams.Platform, updates)
		if err != nil {
			return fmt.Errorf("failed setting platform values, error is: %w", err)
		}
		err = b.providerRegistry.SetPlatformUsages(
			common.PlatformTypeValue(params.ClusterUpdateParams.Platform.Type), params.ClusterUpdateParams.Platform, usages, b.usageApi)
		if err != nil {
			return fmt.Errorf("failed setting platform usages, error is: %w", err)
		}
	}
	return nil
}

// createNetworkParamsCompatibilityPropagation is a backwards compatibility adapter adding ability
// to configure clutster networking using the following structures
//
//   * MachineNetworkCidr
//   * ClusterNetworkCidr
//   * ClusterNetworkHostPrefix
//   * ServiceNetworkCidr
//
// Please note those will take precedence over the more complex introduced as part of dual-stack
// and multi-network support, i.e.
//
//   * MachineNetworks
//   * ClusterNetworks
//   * ServiceNetworks
func createNetworkParamsCompatibilityPropagation(params installer.V2RegisterClusterParams) {
	if params.NewClusterParams.ServiceNetworkCidr != nil {
		serviceNetwork := []*models.ServiceNetwork{}
		if *params.NewClusterParams.ServiceNetworkCidr != "" {
			serviceNetwork = []*models.ServiceNetwork{{
				Cidr: models.Subnet(swag.StringValue(params.NewClusterParams.ServiceNetworkCidr)),
			}}
		}
		params.NewClusterParams.ServiceNetworks = serviceNetwork
	}

	if params.NewClusterParams.ClusterNetworkCidr != nil || params.NewClusterParams.ClusterNetworkHostPrefix != 0 {
		net := []*models.ClusterNetwork{}
		var netCidr models.Subnet
		var netHostPrefix int64

		if params.NewClusterParams.ClusterNetworkCidr != nil {
			netCidr = models.Subnet(*params.NewClusterParams.ClusterNetworkCidr)
		}
		if params.NewClusterParams.ClusterNetworkHostPrefix != 0 {
			netHostPrefix = params.NewClusterParams.ClusterNetworkHostPrefix
		}
		if netCidr != "" || netHostPrefix != 0 {
			net = []*models.ClusterNetwork{{Cidr: netCidr, HostPrefix: netHostPrefix}}
		}

		params.NewClusterParams.ClusterNetworks = net
	}
}

// updateNetworkParamsCompatiblityPropagation is an adapter equivalent to the one above, i.e.
// createNetworkParamsCompatibilityPropagation but responsible for handling cluster updates. It
// exists as a separate function because creation and update of the cluster use different data
// structures, i.e. installer.V2RegisterClusterParams and installer.UpdateClusterParams
func updateNetworkParamsCompatiblityPropagation(params installer.V2UpdateClusterParams, cluster *common.Cluster) error {
	if params.ClusterUpdateParams.ServiceNetworkCidr != nil {
		serviceNetwork := []*models.ServiceNetwork{}
		if *params.ClusterUpdateParams.ServiceNetworkCidr != "" {
			serviceNetwork = []*models.ServiceNetwork{{
				Cidr: models.Subnet(swag.StringValue(params.ClusterUpdateParams.ServiceNetworkCidr)),
			}}
		}
		params.ClusterUpdateParams.ServiceNetworks = serviceNetwork
	}
	if params.ClusterUpdateParams.MachineNetworkCidr != nil {
		machineNetwork := []*models.MachineNetwork{}
		if *params.ClusterUpdateParams.MachineNetworkCidr != "" {
			machineNetwork = []*models.MachineNetwork{{
				Cidr: models.Subnet(swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr)),
			}}
		}
		params.ClusterUpdateParams.MachineNetworks = machineNetwork
	}

	if params.ClusterUpdateParams.ClusterNetworkCidr != nil || params.ClusterUpdateParams.ClusterNetworkHostPrefix != nil {
		clusterNetwork := []*models.ClusterNetwork{}
		var netCidr models.Subnet
		var netHostPrefix int64

		if cluster.ClusterNetworks[0] != nil {
			netCidr = cluster.ClusterNetworks[0].Cidr
			netHostPrefix = cluster.ClusterNetworks[0].HostPrefix
		}

		if params.ClusterUpdateParams.ClusterNetworkCidr != nil {
			netCidr = models.Subnet(*params.ClusterUpdateParams.ClusterNetworkCidr)
		}
		if params.ClusterUpdateParams.ClusterNetworkHostPrefix != nil {
			netHostPrefix = *params.ClusterUpdateParams.ClusterNetworkHostPrefix
		}
		if netCidr != "" || netHostPrefix != 0 {
			clusterNetwork = []*models.ClusterNetwork{{Cidr: netCidr, HostPrefix: netHostPrefix}}
		}

		params.ClusterUpdateParams.ClusterNetworks = clusterNetwork
	}

	return nil
}

// updateNetworkParams takes care of 3 modes:
// 1. Bare metal installation
// 2. None-platform multi-node
// 3. None-platform single-node (Machine CIDR must be defined)
func (b *bareMetalInventory) updateNetworkParams(params installer.V2UpdateClusterParams, cluster *common.Cluster, updates map[string]interface{},
	usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger, interactivity Interactivity) error {
	var err error
	vipDhcpAllocation := swag.BoolValue(cluster.VipDhcpAllocation)
	userManagedNetworking := swag.BoolValue(cluster.UserManagedNetworking)

	if params.ClusterUpdateParams.NetworkType != nil && params.ClusterUpdateParams.NetworkType != cluster.NetworkType {
		updates["network_type"] = swag.StringValue(params.ClusterUpdateParams.NetworkType)
		b.setNetworkTypeUsage(params.ClusterUpdateParams.NetworkType, usages)
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) != userManagedNetworking {
		if !swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) && cluster.CPUArchitecture != common.DefaultCPUArchitecture {
			err = errors.Errorf("disabling User Managed Networking is not allowed for clusters with non-x86_64 CPU architecture")
			return common.NewApiError(http.StatusBadRequest, err)
		}

		// User network mode has changed
		userManagedNetworking = swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking)
		updates["user_managed_networking"] = userManagedNetworking
		cluster.MachineNetworks = []*models.MachineNetwork{}
	}

	if userManagedNetworking {
		err, vipDhcpAllocation = setCommonUserNetworkManagedParams(params.ClusterUpdateParams, common.IsSingleNodeCluster(cluster), updates, log)
		if err != nil {
			return err
		}
	} else {
		if params.ClusterUpdateParams.VipDhcpAllocation != nil && swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation) != vipDhcpAllocation {
			// VIP DHCP mode has changed
			vipDhcpAllocation = swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation)
			updates["vip_dhcp_allocation"] = vipDhcpAllocation
			cluster.MachineNetworks = []*models.MachineNetwork{}
		}

		if vipDhcpAllocation {
			primaryMachineCIDR := ""
			if network.IsMachineCidrAvailable(cluster) {
				primaryMachineCIDR = network.GetMachineCidrById(cluster, 0)
			}
			err = b.updateDhcpNetworkParams(updates, params, primaryMachineCIDR, log)
		} else {
			// The primary Machine CIDR can be calculated on not none-platform machines
			// (the machines are on the same network)
			err = b.updateNonDhcpNetworkParams(updates, cluster, params, log, interactivity)
		}
		if err != nil {
			return err
		}
	}

	if err = b.updateNetworks(db, params, updates, cluster, userManagedNetworking, vipDhcpAllocation); err != nil {
		return err
	}

	b.setUsage(vipDhcpAllocation, usage.VipDhcpAllocationUsage, nil, usages)
	return nil
}

func setCommonUserNetworkManagedParams(params *models.V2ClusterUpdateParams, singleNodeCluster bool, updates map[string]interface{}, log logrus.FieldLogger) (error, bool) {
	err := validateUserManagedNetworkConflicts(params, singleNodeCluster, log)
	if err != nil {
		return err, false
	}
	updates["vip_dhcp_allocation"] = false
	updates["api_vip"] = ""
	updates["ingress_vip"] = ""

	return nil, false
}

func (b *bareMetalInventory) updateNtpSources(params installer.V2UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.AdditionalNtpSource != nil {
		ntpSource := swag.StringValue(params.ClusterUpdateParams.AdditionalNtpSource)
		additionalNtpSourcesDefined := ntpSource != ""

		if additionalNtpSourcesDefined && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err := errors.Errorf("Invalid NTP source: %s", ntpSource)
			log.WithError(err)
			return common.NewApiError(http.StatusBadRequest, err)
		}
		updates["additional_ntp_source"] = ntpSource

		//if additional ntp sources are defined by the user, report usage of this feature
		b.setUsage(additionalNtpSourcesDefined, usage.AdditionalNtpSourceUsage, &map[string]interface{}{
			"source_count": len(strings.Split(ntpSource, ","))}, usages)
	}
	return nil
}

func validateUserManagedNetworkConflicts(params *models.V2ClusterUpdateParams, singleNodeCluster bool, log logrus.FieldLogger) error {
	if params.VipDhcpAllocation != nil && swag.BoolValue(params.VipDhcpAllocation) {
		err := errors.Errorf("VIP DHCP Allocation cannot be enabled with User Managed Networking")
		log.WithError(err)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if params.IngressVip != nil {
		err := errors.Errorf("Ingress VIP cannot be set with User Managed Networking")
		log.WithError(err)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if params.APIVip != nil {
		err := errors.Errorf("API VIP cannot be set with User Managed Networking")
		log.WithError(err)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if common.IsSliceNonEmpty(params.MachineNetworks) && !singleNodeCluster {
		err := errors.Errorf("Machine Network CIDR cannot be set with User Managed Networking")
		log.WithError(err)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	return nil
}

func optionalParam(data *string, field string, updates map[string]interface{}) {
	if data != nil {
		updates[field] = swag.StringValue(data)
	}
}

func (b *bareMetalInventory) setUsage(enabled bool, name string, props *map[string]interface{}, usages map[string]models.Usage) {
	if enabled {
		b.usageApi.Add(usages, name, props)
	} else {
		b.usageApi.Remove(usages, name)
	}
}

func (b *bareMetalInventory) setDefaultUsage(cluster *models.Cluster) error {
	usages := make(map[string]models.Usage)
	b.setUsage(swag.BoolValue(cluster.VipDhcpAllocation), usage.VipDhcpAllocationUsage, nil, usages)
	b.setUsage(cluster.AdditionalNtpSource != "", usage.AdditionalNtpSourceUsage, &map[string]interface{}{
		"source_count": len(strings.Split(cluster.AdditionalNtpSource, ","))}, usages)
	b.setUsage(swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone,
		usage.HighAvailabilityModeUsage, nil, usages)
	b.setProxyUsage(&cluster.HTTPProxy, &cluster.HTTPProxy, &cluster.NoProxy, usages)
	olmOperators := funk.Filter(cluster.MonitoredOperators, func(op *models.MonitoredOperator) bool {
		return op != nil && op.OperatorType == models.OperatorTypeOlm
	}).([]*models.MonitoredOperator)
	b.setOperatorsUsage(olmOperators, []*models.MonitoredOperator{}, usages)
	b.setNetworkTypeUsage(cluster.NetworkType, usages)
	b.setDiskEncryptionUsage(cluster, cluster.DiskEncryption, usages)
	//write all the usages to the cluster object
	err := b.providerRegistry.SetPlatformUsages(common.PlatformTypeValue(cluster.Platform.Type), cluster.Platform, usages, b.usageApi)
	if err != nil {
		return fmt.Errorf("failed setting platform usages, error is: %w", err)
	}
	featusage, _ := json.Marshal(usages)
	cluster.FeatureUsage = string(featusage)
	return nil
}

func (b *bareMetalInventory) setNetworkTypeUsage(networkType *string, usages map[string]models.Usage) {
	switch swag.StringValue(networkType) {
	case models.ClusterNetworkTypeOVNKubernetes:
		b.setUsage(true, usage.OVNNetworkTypeUsage, nil, usages)
		b.setUsage(false, usage.SDNNetworkTypeUsage, nil, usages)
	case models.ClusterNetworkTypeOpenShiftSDN:
		b.setUsage(true, usage.SDNNetworkTypeUsage, nil, usages)
		b.setUsage(false, usage.OVNNetworkTypeUsage, nil, usages)
	}
}

func (b *bareMetalInventory) setProxyUsage(httpProxy *string, httpsProxy *string, noProxy *string, usages map[string]models.Usage) {
	props := map[string]interface{}{}
	enabled := false
	if swag.StringValue(httpProxy) != "" {
		props["http_proxy"] = 1
		enabled = true
	}
	if swag.StringValue(httpsProxy) != "" {
		props["https_proxy"] = 1
		enabled = true
	}
	if swag.StringValue(noProxy) != "" {
		props["no_proxy"] = 1
		enabled = true
	}
	if enabled {
		b.usageApi.Add(usages, usage.ProxyUsage, &props)
	} else {
		b.usageApi.Remove(usages, usage.ProxyUsage)
	}
}

func (b *bareMetalInventory) setOperatorsUsage(updateOLMOperators []*models.MonitoredOperator, removedOLMOperators []*models.MonitoredOperator, usages map[string]models.Usage) {
	for _, operator := range updateOLMOperators {
		b.usageApi.Add(usages, strings.ToUpper(operator.Name), nil)
	}

	for _, operator := range removedOLMOperators {
		b.usageApi.Remove(usages, strings.ToUpper(operator.Name))
	}
}

func (b *bareMetalInventory) updateHostRoles(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.HostsRoles {
		hostRole := params.ClusterUpdateParams.HostsRoles[i]
		log.Infof("Update host %s to role: %s", hostRole.ID, hostRole.Role)
		host, err := common.GetHostFromDB(db, params.ClusterID.String(), hostRole.ID.String())
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				hostRole.ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateRole(ctx, &host.Host, models.HostRole(hostRole.Role), db)
		if err != nil {
			log.WithError(err).Errorf("failed to set role <%s> host <%s> in cluster <%s>",
				hostRole.Role, hostRole.ID,
				params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostNames(ctx context.Context, params installer.UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger) error {
	var host_count = 0
	for i := range params.ClusterUpdateParams.HostsNames {
		hostName := params.ClusterUpdateParams.HostsNames[i]
		log.Infof("Update host %s to request hostname %s", hostName.ID,
			hostName.Hostname)
		host, err := common.GetHostFromDB(db, params.ClusterID.String(), hostName.ID.String())
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				hostName.ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		if err = hostutil.ValidateHostname(hostName.Hostname); err != nil {
			log.WithError(err).Errorf("invalid hostname format: %s", err)
			return err
		}
		err = b.hostApi.UpdateHostname(ctx, &host.Host, hostName.Hostname, db)
		if err != nil {
			log.WithError(err).Errorf("failed to set hostname <%s> host <%s> in cluster <%s>",
				hostName.Hostname, hostName.ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
		host_count = host_count + 1
		b.setUsage(true, usage.RequestedHostnameUsage,
			&map[string]interface{}{"host_count": host_count}, usages)
	}
	return nil
}

func (b *bareMetalInventory) updateHostsDiskSelection(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.DisksSelectedConfig {
		disksConfig := params.ClusterUpdateParams.DisksSelectedConfig[i]
		hostId := string(disksConfig.ID)
		host, err := common.GetHostFromDB(db, params.ClusterID.String(), hostId)

		if err != nil {
			return common.NewApiError(http.StatusNotFound, err)
		}

		disksToInstallOn := funk.Filter(disksConfig.DisksConfig, func(diskConfigParams *models.DiskConfigParams) bool {
			return models.DiskRoleInstall == diskConfigParams.Role
		}).([]*models.DiskConfigParams)

		installationDiskId := ""

		if len(disksToInstallOn) > 1 {
			return common.NewApiError(http.StatusConflict, errors.New("duplicate setting of installation path by the user"))
		} else if len(disksToInstallOn) == 1 {
			installationDiskId = *disksToInstallOn[0].ID
		}

		log.Infof("Update host %s to install from disk id %s", hostId, installationDiskId)
		err = b.hostApi.UpdateInstallationDisk(ctx, db, &host.Host, installationDiskId)
		if err != nil {
			log.WithError(err).Errorf("failed to set installation disk path <%s> host <%s> in cluster <%s>",
				installationDiskId,
				hostId,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostsMachineConfigPoolNames(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.HostsMachineConfigPoolNames {
		poolNameConfig := params.ClusterUpdateParams.HostsMachineConfigPoolNames[i]
		log.Infof("Update host %s to machineConfigPoolName %s", poolNameConfig.ID,
			poolNameConfig.MachineConfigPoolName)
		host, err := common.GetHostFromDB(db, params.ClusterID.String(), poolNameConfig.ID.String())

		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				poolNameConfig.ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateMachineConfigPoolName(ctx, db, &host.Host, poolNameConfig.MachineConfigPoolName)
		if err != nil {
			log.WithError(err).Errorf("failed to set machine config pool name <%s> host <%s> in cluster <%s>",
				poolNameConfig.MachineConfigPoolName, poolNameConfig.ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostsData(ctx context.Context, params installer.UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger) error {
	if err := b.updateHostRoles(ctx, params, db, log); err != nil {
		return err
	}

	if err := b.updateHostNames(ctx, params, usages, db, log); err != nil {
		return err
	}

	if err := b.updateHostsDiskSelection(ctx, params, db, log); err != nil {
		return err
	}

	if err := b.updateHostsMachineConfigPoolNames(ctx, params, db, log); err != nil {
		return err
	}

	return nil
}

func (b *bareMetalInventory) updateClusterNetworkVMUsage(cluster *common.Cluster, updateParams *models.V2ClusterUpdateParams, usages map[string]models.Usage, log logrus.FieldLogger) {
	vmHosts := make([]string, 0)
	userManagedNetwork := cluster.UserManagedNetworking != nil && *cluster.UserManagedNetworking
	if updateParams != nil && updateParams.UserManagedNetworking != nil {
		userManagedNetwork = *updateParams.UserManagedNetworking
	}
	if !userManagedNetwork {
		for _, host := range cluster.Hosts {
			hostInventory, err := common.UnmarshalInventory(host.Inventory)
			if err != nil {
				err = errors.Wrap(err, "Failed to update usage flag for 'Cluster managed network with VMs'.")
				log.Error(err)
				return
			}
			isHostVirtual := hostInventory.SystemVendor != nil && hostInventory.SystemVendor.Virtual
			if isHostVirtual {
				vmHosts = append(vmHosts, string(*host.ID))
			}
		}
	}
	b.setUsage(len(vmHosts) > 0, usage.ClusterManagedNetworkWithVMs, &map[string]interface{}{"VM Hosts": vmHosts}, usages)
}

func (b *bareMetalInventory) updateClusterCPUFeatureUsage(cluster *common.Cluster, usages map[string]models.Usage) {
	isARM64CPU := cluster.CPUArchitecture == "arm64"
	b.setUsage(isARM64CPU, usage.CPUArchitectureARM64, nil, usages)
}

func (b *bareMetalInventory) updateOperatorsData(_ context.Context, cluster *common.Cluster, params installer.V2UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.OlmOperators == nil {
		return nil
	}

	updateOLMOperators, err := b.getOLMOperators(params.ClusterUpdateParams.OlmOperators)
	if err != nil {
		return err
	}

	for _, updatedOperator := range updateOLMOperators {
		updatedOperator.ClusterID = *cluster.ID
		if err = db.Save(updatedOperator).Error; err != nil {
			err = errors.Wrapf(err, "failed to update operator %s of cluster %s", updatedOperator.Name, params.ClusterID)
			log.Error(err)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	// After we aligned to the new update OLM info, we need to delete the old OLMs remainders that are still connected to the cluster
	var removedOLMOperators []*models.MonitoredOperator
	for _, clusterOperator := range cluster.MonitoredOperators {
		if clusterOperator.OperatorType != models.OperatorTypeOlm {
			continue
		}

		if !operators.IsEnabled(updateOLMOperators, clusterOperator.Name) {
			removedOLMOperators = append(removedOLMOperators, clusterOperator)
			if err = db.Where("name = ? and cluster_id = ?", clusterOperator.Name, params.ClusterID).Delete(&models.MonitoredOperator{}).Error; err != nil {
				err = errors.Wrapf(err, "failed to delete operator %s of cluster %s", clusterOperator.Name, params.ClusterID)
				log.Error(err)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}
	b.setOperatorsUsage(updateOLMOperators, removedOLMOperators, usages)

	return nil
}

func (b *bareMetalInventory) getOLMOperators(newOperators []*models.OperatorCreateParams) ([]*models.MonitoredOperator, error) {
	monitoredOperators := make([]*models.MonitoredOperator, 0)

	for _, newOperator := range newOperators {
		operator, err := b.operatorManagerApi.GetOperatorByName(newOperator.Name)
		if err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		operator.Properties = newOperator.Properties

		monitoredOperators = append(monitoredOperators, operator)
	}

	return b.operatorManagerApi.ResolveDependencies(monitoredOperators)
}

func (b *bareMetalInventory) updateHostsAndClusterStatus(ctx context.Context, cluster *common.Cluster, db *gorm.DB, log logrus.FieldLogger) error {
	err := b.refreshClusterHosts(ctx, cluster, db, log)
	if err != nil {
		return err
	}

	if _, err = b.clusterApi.RefreshStatus(ctx, cluster, db); err != nil {
		log.WithError(err).Errorf("failed to validate or update cluster %s state", cluster.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return nil
}

func (b *bareMetalInventory) calculateHostNetworks(log logrus.FieldLogger, cluster *common.Cluster) []*models.HostNetwork {
	cidrHostsMap := make(map[string]map[strfmt.UUID]bool)
	for _, h := range cluster.Hosts {
		if h.Inventory == "" {
			continue
		}
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			log.WithError(err).Warnf("Could not parse inventory of host %s", *h.ID)
			continue
		}
		for _, intf := range inventory.Interfaces {
			var addrRange []string
			if b.Config.IPv6Support {
				addrRange = append(intf.IPV4Addresses, intf.IPV6Addresses...)
			} else {
				addrRange = intf.IPV4Addresses
			}
			for _, address := range addrRange {
				_, ipnet, err := net.ParseCIDR(address)
				if err != nil {
					log.WithError(err).Warnf("Could not parse CIDR %s", address)
					continue
				}
				cidr := ipnet.String()
				uuidSet, ok := cidrHostsMap[cidr]
				if !ok {
					uuidSet = make(map[strfmt.UUID]bool)
				}
				uuidSet[*h.ID] = true
				cidrHostsMap[cidr] = uuidSet
			}
		}
	}
	ret := make([]*models.HostNetwork, 0)
	for k, v := range cidrHostsMap {
		slice := make([]strfmt.UUID, 0)
		for k := range v {
			slice = append(slice, k)
		}
		ret = append(ret, &models.HostNetwork{
			Cidr:    k,
			HostIds: slice,
		})
	}
	return ret
}

func (b *bareMetalInventory) ListClusters(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
	v2Params := installer.V2ListClustersParams{
		AmsSubscriptionIds:      params.AmsSubscriptionIds,
		GetUnregisteredClusters: params.GetUnregisteredClusters,
		OpenshiftClusterID:      params.OpenshiftClusterID,
		WithHosts:               params.WithHosts,
	}
	clusters, err := b.listClustersInternal(ctx, v2Params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) listClustersInternal(ctx context.Context, params installer.V2ListClustersParams) ([]*models.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	db := b.db
	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !identity.IsAdmin(ctx) {
			return nil, common.NewApiError(http.StatusForbidden, errors.New("only admin users are allowed to get unregistered clusters"))
		}
		db = db.Unscoped()
	}
	var dbClusters []*common.Cluster
	var clusters []*models.Cluster
	whereCondition := make([]string, 0)

	if user := identity.AddUserFilter(ctx, ""); user != "" {
		whereCondition = append(whereCondition, user)
	}

	if params.OpenshiftClusterID != nil {
		whereCondition = append(whereCondition, fmt.Sprintf("openshift_cluster_id = '%s'", *params.OpenshiftClusterID))
	}

	if len(params.AmsSubscriptionIds) > 0 {
		whereCondition = append(whereCondition, fmt.Sprintf("ams_subscription_id IN %s", common.ToSqlList(params.AmsSubscriptionIds)))
	}

	dbClusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading,
		common.DeleteRecordsState(swag.BoolValue(params.GetUnregisteredClusters)), strings.Join(whereCondition, " AND "))
	if err != nil {
		log.WithError(err).Error("Failed to list clusters in db")
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	// we need to fetch Hosts association to allow AfterFind hook to run
	for _, c := range dbClusters {
		if !params.WithHosts {
			c.Hosts = []*models.Host{}
		}
		for _, h := range c.Hosts {
			// Clear this field as it is not needed to be sent via API
			h.FreeAddresses = ""
		}
		clusters = append(clusters, &c.Cluster)
	}
	return clusters, nil
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	v2Params := installer.V2GetClusterParams{
		ClusterID:               params.ClusterID,
		DiscoveryAgentVersion:   params.DiscoveryAgentVersion,
		GetUnregisteredClusters: params.GetUnregisteredClusters,
	}
	c, err := b.GetClusterInternal(ctx, v2Params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetClusterOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) GetClusterInternal(ctx context.Context, params installer.V2GetClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)

	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !identity.IsAdmin(ctx) {
			return nil, common.NewInfraError(http.StatusForbidden,
				errors.New("only admin users are allowed to get unregistered clusters"))
		}
	}

	cluster, err := common.GetClusterFromDBWhere(b.db, common.UseEagerLoading,
		common.DeleteRecordsState(swag.BoolValue(params.GetUnregisteredClusters)), "id = ?", params.ClusterID)
	if err != nil {
		return nil, err
	}

	cluster.HostNetworks = b.calculateHostNetworks(log, cluster)
	for _, host := range cluster.Hosts {
		if err = b.customizeHost(&cluster.Cluster, host); err != nil {
			return nil, err
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

	imageInfo, err := b.getImageInfo(cluster.ID)
	if err != nil {
		return nil, err
	}
	cluster.ImageInfo = imageInfo

	return cluster, nil
}

func (b *bareMetalInventory) getImageInfo(clusterId *strfmt.UUID) (*models.ImageInfo, error) {
	infraEnv, err := common.GetInfraEnvFromDB(b.db, *clusterId)
	if err == nil {
		imageInfo := &models.ImageInfo{
			DownloadURL:         infraEnv.DownloadURL,
			SizeBytes:           infraEnv.SizeBytes,
			CreatedAt:           time.Time(infraEnv.GeneratedAt),
			ExpiresAt:           infraEnv.ImageExpiresAt,
			SSHPublicKey:        infraEnv.SSHAuthorizedKey,
			Type:                common.ImageTypeValue(infraEnv.Type),
			StaticNetworkConfig: infraEnv.StaticNetworkConfig,
			GeneratorVersion:    infraEnv.GeneratorVersion,
		}
		return imageInfo, nil
	} else {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	return &models.ImageInfo{}, nil
}

func (b *bareMetalInventory) generateV2NextStepRunnerCommand(ctx context.Context, params *installer.V2RegisterHostParams) *models.HostRegistrationResponseAO1NextStepRunnerCommand {

	currentImageTag := extractImageTag(b.AgentDockerImg)
	if params.NewHostParams.DiscoveryAgentVersion != currentImageTag {
		log := logutil.FromContext(ctx, b.log)
		log.Infof("Host %s in infra-env %s has outdated agent image %s, updating to %s",
			params.NewHostParams.HostID.String(), params.InfraEnvID.String(), params.NewHostParams.DiscoveryAgentVersion, currentImageTag)
	}

	config := hostcommands.V2NextStepRunnerConfig{
		ServiceBaseURL:       b.ServiceBaseURL,
		InfraEnvID:           params.InfraEnvID.String(),
		HostID:               params.NewHostParams.HostID.String(),
		UseCustomCACert:      b.ServiceCACertPath != "",
		NextStepRunnerImage:  b.AgentDockerImg,
		SkipCertVerification: b.SkipCertVerification,
	}
	command, args := hostcommands.V2GetNextStepRunnerCommand(&config)
	return &models.HostRegistrationResponseAO1NextStepRunnerCommand{
		Command: command,
		Args:    *args,
	}
}

func extractImageTag(fullName string) string {
	suffix := strings.Split(fullName, ":")
	return suffix[len(suffix)-1]
}

func returnRegisterHostTransitionError(
	defaultCode int32,
	err error) middleware.Responder {
	if isRegisterHostForbiddenDueWrongBootOrder(err) {
		return installer.NewV2RegisterHostForbidden().WithPayload(
			&models.InfraError{
				Code:    swag.Int32(http.StatusForbidden),
				Message: swag.String(err.Error()),
			})
	}
	return common.NewApiError(defaultCode, err)
}

func isRegisterHostForbiddenDueWrongBootOrder(err error) bool {
	if serr, ok := err.(*common.ApiErrorResponse); ok {
		return serr.StatusCode() == http.StatusForbidden
	}
	return false
}

func (b *bareMetalInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
	if err := b.DeregisterHostInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewDeregisterHostNoContent()
}

func (b *bareMetalInventory) DeregisterHostInternal(ctx context.Context, params installer.DeregisterHostParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister host: %s cluster %s", params.HostID, params.ClusterID)

	h, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err := b.hostApi.UnRegisterHost(ctx, params.HostID.String(), params.ClusterID.String()); err != nil {
		// TODO: check error type
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// TODO: need to check that host can be deleted from the cluster
	eventgen.SendHostDeregisteredEvent(ctx, b.eventsHandler, params.HostID, h.InfraEnvID, &params.ClusterID,
		hostutil.GetHostnameForMsg(&h.Host))
	return nil
}

func (b *bareMetalInventory) V2DeregisterHostInternal(ctx context.Context, params installer.V2DeregisterHostParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister host: %s infra env %s", params.HostID, params.InfraEnvID)

	h, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err = b.hostApi.UnRegisterHost(ctx, params.HostID.String(), params.InfraEnvID.String()); err != nil {
		// TODO: check error type
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// TODO: need to check that host can be deleted from the cluster
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	var clusterID strfmt.UUID
	if err != nil {
		log.WithError(err).Warnf("Get InfraEnv %s", params.InfraEnvID.String())
		return err
	}
	clusterID = infraEnv.ClusterID
	eventgen.SendHostDeregisteredEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, &clusterID,
		hostutil.GetHostnameForMsg(&h.Host))
	return nil
}

func (b *bareMetalInventory) GetHost(_ context.Context, params installer.GetHostParams) middleware.Responder {
	// TODO: validate what is the error
	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())

	if err != nil {
		return installer.NewGetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	cluster, err := common.GetClusterFromDB(b.db, *host.ClusterID, common.SkipEagerLoading)
	if err != nil {
		return installer.NewGetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}
	if err := b.customizeHost(&cluster.Cluster, &host.Host); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// Clear this field as it is not needed to be sent via API
	host.FreeAddresses = ""
	return installer.NewGetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	cluster, err := common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("failed to get list of hosts for cluster %s", params.ClusterID)
		return installer.NewListHostsInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, host := range cluster.Hosts {
		if err := b.customizeHost(&cluster.Cluster, host); err != nil {
			return common.GenerateErrorResponder(err)
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

	return installer.NewListHostsOK().WithPayload(cluster.Hosts)
}

func (b *bareMetalInventory) UpdateHostInstallerArgsInternal(ctx context.Context, params installer.UpdateHostInstallerArgsParams) (*models.Host, error) {

	log := logutil.FromContext(ctx, b.log)

	err := hostutil.ValidateInstallerArgs(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	h, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	argsBytes, err := json.Marshal(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, err
	}

	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and cluster_id = ?"), params.HostID, params.ClusterID).Update("installer_args", string(argsBytes)).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update host %s", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	// TODO: pass InfraEnvID instead of ClusterID
	eventgen.SendHostInstallerArgsAppliedEvent(ctx, b.eventsHandler, params.HostID, h.InfraEnvID, &params.ClusterID, hostutil.GetHostnameForMsg(&h.Host))
	log.Infof("Custom installer arguments were applied to host %s in cluster %s", params.HostID, params.ClusterID)

	h, err = b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return &h.Host, nil
}

func (b *bareMetalInventory) UpdateHostInstallerArgs(ctx context.Context, params installer.UpdateHostInstallerArgsParams) middleware.Responder {
	updatedHost, err := b.UpdateHostInstallerArgsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateHostInstallerArgsCreated().WithPayload(updatedHost)
}

func shouldHandle(params installer.V2PostStepReplyParams) bool {
	switch params.Reply.StepType {
	case models.StepTypeInstallationDiskSpeedCheck, models.StepTypeContainerImageAvailability:
		/*
		   In case that the command sent 0 length output is should not be handled.  When disk speed check takes a long time,
		   we don't want to run 2 such commands concurrently.  The prior running disk-speed-check, there is a verification
		   that such command is not already running.  If it does, then the command returns immediately without running
		   disk-speed-check.
		   TODO Maybe do the same for other commands as well.
		*/
		return len(params.Reply.Output) > 0
	}
	return true
}

func (b *bareMetalInventory) handleReplyError(params installer.V2PostStepReplyParams, ctx context.Context, log logrus.FieldLogger, h *models.Host, exitCode int64) error {
	if exitCode == MediaDisconnected {
		if err := b.handleMediaDisconnection(params, ctx, log, h); err != nil {
			return err
		}
	}

	switch params.Reply.StepType {
	case models.StepTypeInstall:
		// Handle case of installation error due to an already running assisted-installer.
		if params.Reply.ExitCode == ContainerAlreadyRunningExitCode && strings.Contains(params.Reply.Error, "the container name \"assisted-installer\" is already in use") {
			log.Warnf("Install command failed due to an already running installation: %s", params.Reply.Error)
			return nil
		}
		//if it's install step - need to move host to error
		return b.hostApi.HandleInstallationFailure(ctx, h)
	case models.StepTypeContainerImageAvailability:
		stepReply, err := filterReplyByType(params)
		if err != nil {
			return err
		}
		return b.processImageAvailabilityResponse(ctx, h, stepReply)
	case models.StepTypeInstallationDiskSpeedCheck:
		stepReply, err := filterReplyByType(params)
		if err != nil {
			return err
		}
		return b.processDiskSpeedCheckResponse(ctx, h, stepReply, exitCode)
	}
	return nil
}

func (b *bareMetalInventory) handleMediaDisconnection(params installer.V2PostStepReplyParams, ctx context.Context, log logrus.FieldLogger, h *models.Host) error {
	statusInfo := fmt.Sprintf("%s - %s", string(models.HostStageFailed), mediaDisconnectionMessage)

	// Install command reports its status with a different API, directly from the assisted-installer.
	// Just adding our diagnose to the existing error message.
	if swag.StringValue(h.Status) == models.HostStatusError && h.StatusInfo != nil {
		// Add the message only once
		if strings.Contains(*h.StatusInfo, statusInfo) {
			return nil
		}

		statusInfo = fmt.Sprintf("%s. %s", statusInfo, *h.StatusInfo)
	} else if params.Reply.Error != "" {
		statusInfo = fmt.Sprintf("%s. %s", statusInfo, params.Reply.Error)
	}

	_, err := hostutil.UpdateHostStatus(ctx, log, b.db, b.eventsHandler, *h.ClusterID, *h.ID,
		swag.StringValue(h.Status), models.HostStatusError, statusInfo)

	return err
}

func (b *bareMetalInventory) updateFreeAddressesReport(ctx context.Context, host *models.Host, freeAddressesReport string) error {
	var (
		err           error
		freeAddresses models.FreeNetworksAddresses
	)
	log := logutil.FromContext(ctx, b.log)
	log.Debugf("Free addresses for host %s are: %s", host.ID.String(), freeAddresses)

	if err = json.Unmarshal([]byte(freeAddressesReport), &freeAddresses); err != nil {
		log.WithError(err).Warnf("Json unmarshal free addresses of host %s", host.ID.String())
		return err
	}
	if len(freeAddresses) == 0 {
		err = errors.Errorf("Free addresses for host %s is empty", host.ID.String())
		log.WithError(err).Warn("Update free addresses")
		return err
	}
	if err = b.db.Model(&common.Host{}).Where("id = ? and cluster_id = ?", host.ID.String(),
		host.ClusterID.String()).UpdateColumn("free_addresses", freeAddressesReport).Error; err != nil {
		log.WithError(err).Warnf("Update free addresses of host %s", host.ID.String())
		return err
	}
	// Gorm sets the number of changed rows in AffectedRows and not the number of matched rows.  Therefore, if the report hasn't changed
	// from the previous report, the AffectedRows will be 0 but it will still be correct.  So no error reporting needed for AffectedRows == 0
	return nil
}

func (b *bareMetalInventory) processDhcpAllocationResponse(ctx context.Context, host *models.Host, dhcpAllocationResponseStr string) error {
	log := logutil.FromContext(ctx, b.log)

	cluster, err := common.GetClusterFromDB(common.LoadTableFromDB(b.db, common.MachineNetworksTable),
		strfmt.UUID(host.ClusterID.String()), common.SkipEagerLoading)
	if err != nil {
		log.WithError(err).Warnf("Get cluster %s", host.ClusterID.String())
		return err
	}
	if !swag.BoolValue(cluster.VipDhcpAllocation) {
		err = errors.Errorf("DHCP not enabled in cluster %s", host.ClusterID.String())
		log.WithError(err).Warn("processDhcpAllocationResponse")
		return err
	}
	var dhcpAllocationReponse models.DhcpAllocationResponse
	if err = json.Unmarshal([]byte(dhcpAllocationResponseStr), &dhcpAllocationReponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal dhcp allocation from host %s", host.ID.String())
		return err
	}
	apiVip := dhcpAllocationReponse.APIVipAddress.String()
	ingressVip := dhcpAllocationReponse.IngressVipAddress.String()
	primaryMachineCIDR := ""
	if network.IsMachineCidrAvailable(cluster) {
		primaryMachineCIDR = network.GetMachineCidrById(cluster, 0)
	}

	isApiVipInMachineCIDR, err := network.IpInCidr(apiVip, primaryMachineCIDR)
	if err != nil {
		log.WithError(err).Warn("Ip in CIDR for API VIP")
		return err
	}

	isIngressVipInMachineCIDR, err := network.IpInCidr(ingressVip, primaryMachineCIDR)
	if err != nil {
		log.WithError(err).Warn("Ip in CIDR for Ingress VIP")
		return err
	}

	if !(isApiVipInMachineCIDR && isIngressVipInMachineCIDR) {
		err = errors.Errorf("At least of the IPs (%s, %s) is not in machine CIDR %s", apiVip, ingressVip, primaryMachineCIDR)
		log.WithError(err).Warn("IP in CIDR")
		return err
	}

	err = network.VerifyLease(dhcpAllocationReponse.APIVipLease)
	if err != nil {
		log.WithError(err).Warnf("API Vip not validated")
		return err
	}
	err = network.VerifyLease(dhcpAllocationReponse.IngressVipLease)
	if err != nil {
		log.WithError(err).Warnf("Ingress Vip not validated")
		return err
	}
	return b.clusterApi.SetVipsData(ctx, cluster, apiVip, ingressVip, dhcpAllocationReponse.APIVipLease, dhcpAllocationReponse.IngressVipLease, b.db)
}

func (b *bareMetalInventory) processNtpSynchronizerResponse(ctx context.Context, host *models.Host, ntpSynchronizerResponseStr string) error {
	var ntpSynchronizerResponse models.NtpSynchronizationResponse

	log := logutil.FromContext(ctx, b.log)

	if err := json.Unmarshal([]byte(ntpSynchronizerResponseStr), &ntpSynchronizerResponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal ntp synchronizer response from host %s", host.ID.String())
		return err
	}

	return b.hostApi.UpdateNTP(ctx, host, ntpSynchronizerResponse.NtpSources, b.db)
}

func (b *bareMetalInventory) processDiskSpeedCheckResponse(ctx context.Context, h *models.Host, diskPerfCheckResponseStr string, exitCode int64) error {
	var diskPerfCheckResponse models.DiskSpeedCheckResponse

	log := logutil.FromContext(ctx, b.log)

	if err := json.Unmarshal([]byte(diskPerfCheckResponseStr), &diskPerfCheckResponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal FIO perf check response from host %s", h.ID.String())
		return err
	}

	if exitCode == 0 {
		b.metricApi.DiskSyncDuration(*h.ID, diskPerfCheckResponse.Path, diskPerfCheckResponse.IoSyncDuration)

		thresholdMs, err := b.getInstallationDiskSpeedThresholdMs(ctx, h)
		if err != nil {
			log.WithError(err).Warnf("Error getting installation disk speed threshold for host %s", h.ID.String())
			return err
		}
		if diskPerfCheckResponse.IoSyncDuration > thresholdMs {
			// If the 99th percentile of fdatasync durations is more than 10ms, it's not fast enough for etcd.
			// See: https://www.ibm.com/cloud/blog/using-fio-to-tell-whether-your-storage-is-fast-enough-for-etcd
			msg := fmt.Sprintf("Host's disk %s is slower than the supported speed, and may cause degraded cluster performance (fdatasync duration: %d ms)",
				diskPerfCheckResponse.Path, diskPerfCheckResponse.IoSyncDuration)
			log.Warnf(msg)
			eventgen.SendDiskSpeedSlowerThanSupportedEvent(ctx, b.eventsHandler, *h.ID, h.InfraEnvID, h.ClusterID,
				diskPerfCheckResponse.Path, diskPerfCheckResponse.IoSyncDuration)
		}
	}

	return b.hostApi.SetDiskSpeed(ctx, h, diskPerfCheckResponse.Path, diskPerfCheckResponse.IoSyncDuration, exitCode, nil)
}

func (b *bareMetalInventory) updateDomainNameResolutionResponse(ctx context.Context, host *models.Host, domainResolutionResponseJson string) error {
	var domainResolutionResponse models.DomainResolutionResponse

	log := logutil.FromContext(ctx, b.log)
	log.Debugf("The response for domain name resolution on host %s is: %s", host.ID.String(), domainResolutionResponseJson)

	if err := json.Unmarshal([]byte(domainResolutionResponseJson), &domainResolutionResponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal domain name resolution of host %s", host.ID.String())
		return err
	}
	return b.hostApi.UpdateDomainNameResolution(ctx, host, domainResolutionResponse, b.db)
}

func (b *bareMetalInventory) getInstallationDiskSpeedThresholdMs(ctx context.Context, h *models.Host) (int64, error) {
	cluster, err := common.GetClusterFromDB(b.db, *h.ClusterID, common.UseEagerLoading)
	if err != nil {
		return 0, err
	}

	return b.hwValidator.GetInstallationDiskSpeedThresholdMs(ctx, cluster, h)
}

func (b *bareMetalInventory) processImageAvailabilityResponse(ctx context.Context, host *models.Host, responseStr string) error {
	var response models.ContainerImageAvailabilityResponse

	log := logutil.FromContext(ctx, b.log)

	if err := json.Unmarshal([]byte(responseStr), &response); err != nil {
		log.WithError(err).Warnf("Json unmarshal %s image availability response from host %s", responseStr, host.ID.String())
		return err
	}

	for _, image := range response.Images {
		if err := b.hostApi.UpdateImageStatus(ctx, host, image, b.db); err != nil {
			return err
		}
	}

	return nil
}

func handleReplyByType(params installer.V2PostStepReplyParams, b *bareMetalInventory, ctx context.Context, host models.Host, stepReply string) error {
	var err error
	switch params.Reply.StepType {
	case models.StepTypeInventory:
		err = b.hostApi.UpdateInventory(ctx, &host, stepReply)
	case models.StepTypeConnectivityCheck:
		err = b.hostApi.UpdateConnectivityReport(ctx, &host, stepReply)
	case models.StepTypeAPIVipConnectivityCheck:
		err = b.hostApi.UpdateApiVipConnectivityReport(ctx, &host, stepReply)
	case models.StepTypeFreeNetworkAddresses:
		err = b.updateFreeAddressesReport(ctx, &host, stepReply)
	case models.StepTypeDhcpLeaseAllocate:
		err = b.processDhcpAllocationResponse(ctx, &host, stepReply)
	case models.StepTypeNtpSynchronizer:
		err = b.processNtpSynchronizerResponse(ctx, &host, stepReply)
	case models.StepTypeContainerImageAvailability:
		err = b.processImageAvailabilityResponse(ctx, &host, stepReply)
	case models.StepTypeInstallationDiskSpeedCheck:
		err = b.processDiskSpeedCheckResponse(ctx, &host, stepReply, 0)
	case models.StepTypeDomainResolution:
		err = b.updateDomainNameResolutionResponse(ctx, &host, stepReply)
	}
	return err
}

func logReplyReceived(params installer.V2PostStepReplyParams, log logrus.FieldLogger, host *common.Host) {
	if !shouldStepReplyBeLogged(params.Reply, host) {
		return
	}

	message := fmt.Sprintf("Received step reply <%s> from infra-env <%s> host <%s> exit-code <%d> stderr <%s>",
		params.Reply.StepID, params.InfraEnvID, params.HostID, params.Reply.ExitCode, params.Reply.Error)
	messageWithOutput := fmt.Sprintf("%s stdout <%s>", message, params.Reply.Output)

	if params.Reply.ExitCode != 0 {
		log.Error(messageWithOutput)
		return
	}

	if params.Reply.StepType == models.StepTypeFreeNetworkAddresses {
		log.Info(message)
	} else {
		log.Info(messageWithOutput)
	}
}

func shouldStepReplyBeLogged(reply *models.StepReply, host *common.Host) bool {
	// Host with a disconnected ISO device is unstable and all the steps should be failed
	// Currently the assisted-service logs are full with the media disconnection errors.
	// Here we are filtering these errors and log the message once per host.
	// TODO: Create a new state "unstable" in the state machine with no commands.
	// TODO: Maybe we should collect the logs even in this state.
	notFirstMediaDisconnectionFailure := *host.Status == models.HostStatusError && host.StatusInfo != nil &&
		strings.Contains(*host.StatusInfo, mediaDisconnectionMessage)
	return !(reply.ExitCode == MediaDisconnected && notFirstMediaDisconnectionFailure)
}

func filterReplyByType(params installer.V2PostStepReplyParams) (string, error) {
	var stepReply string
	var err error

	// To make sure we store only information defined in swagger we unmarshal and marshal the stepReplyParams.
	switch params.Reply.StepType {
	case models.StepTypeInventory:
		stepReply, err = filterReply(&models.Inventory{}, params.Reply.Output)
	case models.StepTypeConnectivityCheck:
		stepReply, err = filterReply(&models.ConnectivityReport{}, params.Reply.Output)
	case models.StepTypeAPIVipConnectivityCheck:
		stepReply, err = filterReply(&models.APIVipConnectivityResponse{}, params.Reply.Output)
	case models.StepTypeFreeNetworkAddresses:
		stepReply, err = filterReply(&models.FreeNetworksAddresses{}, params.Reply.Output)
	case models.StepTypeDhcpLeaseAllocate:
		stepReply, err = filterReply(&models.DhcpAllocationResponse{}, params.Reply.Output)
	case models.StepTypeNtpSynchronizer:
		stepReply, err = filterReply(&models.NtpSynchronizationResponse{}, params.Reply.Output)
	case models.StepTypeContainerImageAvailability:
		stepReply, err = filterReply(&models.ContainerImageAvailabilityResponse{}, params.Reply.Output)
	case models.StepTypeInstallationDiskSpeedCheck:
		stepReply, err = filterReply(&models.DiskSpeedCheckResponse{}, params.Reply.Output)
	case models.StepTypeDomainResolution:
		stepReply, err = filterReply(&models.DomainResolutionResponse{}, params.Reply.Output)
	}

	return stepReply, err
}

// filterReply return only the expected parameters from the input.
func filterReply(expected interface{}, input string) (string, error) {
	if err := json.Unmarshal([]byte(input), expected); err != nil {
		return "", err
	}
	reply, err := json.Marshal(expected)
	if err != nil {
		return "", err
	}
	return string(reply), nil
}

func (b *bareMetalInventory) DisableHost(ctx context.Context, params installer.DisableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Info("disabling host: ", params.HostID)

	txSuccess := false
	tx := b.db.Begin()

	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	host, err := common.GetHostFromDB(transaction.AddForUpdateQueryOption(tx), params.ClusterID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		eventgen.SendDisableHostFetchFailedEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
			hostutil.GetHostnameForMsg(&host.Host))
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.hostApi.DisableHost(ctx, &host.Host, tx); err != nil {
		log.WithError(err).Errorf("failed to disable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		eventgen.SendHostDisableFailedEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
			hostutil.GetHostnameForMsg(&host.Host))
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "disable host", host, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	eventgen.SendHostDisabledEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
		hostutil.GetHostnameForMsg(&host.Host))

	c, err = b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: *c.ID})
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewDisableHostOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Info("enable host: ", params.HostID)

	txSuccess := false
	tx := b.db.Begin()

	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	host, err := common.GetHostFromDB(transaction.AddForUpdateQueryOption(tx), params.ClusterID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		eventgen.SendEnableHostFetchFailedEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
			hostutil.GetHostnameForMsg(&host.Host))

		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.hostApi.EnableHost(ctx, &host.Host, tx); err != nil {
		log.WithError(err).Errorf("failed to enable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		eventgen.SendEnableHostDisableFailedEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
			hostutil.GetHostnameForMsg(&host.Host))
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "enable host", host, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	eventgen.SendHostEnabledEvent(ctx, b.eventsHandler, params.HostID, host.InfraEnvID, &params.ClusterID,
		hostutil.GetHostnameForMsg(&host.Host))

	c, err = b.GetClusterInternal(ctx, installer.V2GetClusterParams{ClusterID: *c.ID})
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewEnableHostOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) refreshHostAndClusterStatuses(
	ctx context.Context,
	eventName string,
	host *common.Host,
	clusterID *strfmt.UUID,
	db *gorm.DB) (*common.Cluster, error) {

	logger := logutil.FromContext(ctx, b.log)

	var err error

	defer func() {
		if err == nil {
			return
		}
		logger.WithError(err).Errorf("%s:", eventName)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			eventgen.SendRefreshHostOrClusterStatusesFailedEvent(ctx, b.eventsHandler, *host.ID, host.InfraEnvID, clusterID, err.Error())
		}
	}()
	err = b.setMajorityGroupForCluster(clusterID, db)
	if err != nil {
		return nil, err
	}
	err = b.refreshHostStatus(ctx, host.ID, clusterID, db)
	if err != nil {
		return nil, err
	}

	c, err := b.refreshClusterStatus(ctx, clusterID, db)
	return c, err
}

func (b *bareMetalInventory) refreshHostStatus(
	ctx context.Context,
	hostID *strfmt.UUID,
	clusterID *strfmt.UUID,
	db *gorm.DB) error {

	h, err := common.GetHostFromDB(db, clusterID.String(), hostID.String())
	if err != nil {
		return err
	}

	if err := b.hostApi.RefreshStatus(ctx, &h.Host, db); err != nil {
		return errors.Wrapf(err, "failed to refresh status of host: %s", h.ID)
	}

	return nil
}

func (b *bareMetalInventory) setMajorityGroupForCluster(clusterID *strfmt.UUID, db *gorm.DB) error {
	return b.clusterApi.SetConnectivityMajorityGroupsForCluster(*clusterID, db)
}

func (b *bareMetalInventory) refreshClusterStatus(
	ctx context.Context,
	clusterID *strfmt.UUID,
	db *gorm.DB) (*common.Cluster, error) {

	cluster, err := common.GetClusterFromDB(db, *clusterID, common.SkipEagerLoading)
	if err != nil {
		return nil, err
	}

	updatedCluster, err := b.clusterApi.RefreshStatus(ctx, cluster, db)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to refresh status of cluster: %s", cluster.ID)
	}

	return updatedCluster, nil
}

func (b *bareMetalInventory) V2GetPresignedForClusterFiles(ctx context.Context, params installer.V2GetPresignedForClusterFilesParams) middleware.Responder {
	presigned, err := b.getPresignedForClusterFiles(ctx, &params.ClusterID, params.FileName, params.AdditionalName, params.HostID, params.LogsType)
	if err != nil {
		return err
	}

	return installer.NewV2GetPresignedForClusterFilesOK().WithPayload(presigned)
}

func (b *bareMetalInventory) GetPresignedForClusterFiles(ctx context.Context, params installer.GetPresignedForClusterFilesParams) middleware.Responder {
	presigned, err := b.getPresignedForClusterFiles(ctx, &params.ClusterID, params.FileName, params.AdditionalName, params.HostID, params.LogsType)
	if err != nil {
		return err
	}

	return installer.NewGetPresignedForClusterFilesOK().WithPayload(presigned)
}

func (b *bareMetalInventory) getPresignedForClusterFiles(ctx context.Context, clusterId *strfmt.UUID, fileName string, additionalName *string, hostID *strfmt.UUID, logsType *string) (*models.Presigned, middleware.Responder) {
	log := logutil.FromContext(ctx, b.log)

	if err := b.checkFileDownloadAccess(ctx, fileName); err != nil {
		payload := common.GenerateInfraError(http.StatusForbidden, err)
		return nil, installer.NewGetPresignedForClusterFilesForbidden().WithPayload(payload)
	}

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if !b.objectHandler.IsAwsS3() {
		return nil, common.NewApiError(http.StatusBadRequest, errors.New("Failed to generate presigned URL: invalid backend"))
	}
	var err error
	fullFileName := fmt.Sprintf("%s/%s", clusterId, fileName)
	downloadFilename := fileName
	if fileName == manifests.ManifestFolder {
		if additionalName != nil {
			additionalName := *additionalName
			fullFileName = manifests.GetManifestObjectName(*clusterId, additionalName)
			downloadFilename = additionalName[strings.LastIndex(additionalName, "/")+1:]
		} else {
			err = errors.New("Additional name must be provided for 'manifests' file name, prefaced with folder name, e.g.: openshift/99-openshift-xyz.yaml")
			return nil, common.GenerateErrorResponder(err)
		}
	}

	if fileName == "logs" {
		if hostID != nil && swag.StringValue(logsType) == "" {
			logsType = swag.String(string(models.LogsTypeHost))
		}
		fullFileName, downloadFilename, err = b.getLogFileForDownload(ctx, clusterId, hostID, swag.StringValue(logsType))
		if err != nil {
			return nil, common.GenerateErrorResponder(err)
		}
	} else if err = b.checkFileForDownload(ctx, clusterId.String(), fileName); err != nil {
		return nil, common.GenerateErrorResponder(err)
	}

	duration, _ := time.ParseDuration("10m")
	url, err := b.objectHandler.GeneratePresignedDownloadURL(ctx, fullFileName, downloadFilename, duration)
	if err != nil {
		log.WithError(err).Errorf("failed to generate presigned URL: %s from cluster: %s", fileName, clusterId.String())
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return &models.Presigned{URL: &url}, nil
}

func (b *bareMetalInventory) DownloadMinimalInitrd(ctx context.Context, params installer.DownloadMinimalInitrdParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if common.ImageTypeValue(infraEnv.Type) != models.ImageTypeMinimalIso {
		err = fmt.Errorf("Only %v image type supported but %v specified.", models.ImageTypeMinimalIso, infraEnv.Type)
		log.WithError(err)
		return common.NewApiError(http.StatusConflict, err)
	}

	var netFiles []staticnetworkconfig.StaticNetworkConfigData
	if infraEnv.StaticNetworkConfig != "" {
		netFiles, err = b.staticNetworkConfig.GenerateStaticNetworkConfigData(ctx, infraEnv.StaticNetworkConfig)
		if err != nil {
			log.WithError(err).Errorf("Failed to create static network config data")
			return common.GenerateErrorResponder(err)
		}
	}

	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
	infraEnvProxyInfo := isoeditor.ClusterProxyInfo{
		HTTPProxy:  httpProxy,
		HTTPSProxy: httpsProxy,
		NoProxy:    noProxy,
	}

	minimalInitrd, err := isoeditor.RamdiskImageArchive(netFiles, &infraEnvProxyInfo)
	if err != nil {
		log.WithError(err).Error("Failed to create ramdisk image archive")
		return common.GenerateErrorResponder(err)
	}

	if len(minimalInitrd) == 0 {
		return installer.NewDownloadMinimalInitrdNoContent()
	}

	return installer.NewDownloadMinimalInitrdOK().WithPayload(ioutil.NopCloser(bytes.NewReader(minimalInitrd)))
}

func (b *bareMetalInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	if params.FileName == "discovery.ign" {
		infraEnv, err := common.GetInfraEnvFromDB(b.db, params.ClusterID)
		if err != nil {
			return common.GenerateErrorResponder(err)
		}

		cfg, err := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, false, b.authHandler.AuthType())
		if err != nil {
			log.WithError(err).Error("Failed to format ignition config")
			return common.GenerateErrorResponder(err)
		}

		return filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(ioutil.NopCloser(strings.NewReader(cfg))), params.FileName, int64(len(cfg)))
	}

	if err := b.checkFileDownloadAccess(ctx, params.FileName); err != nil {
		payload := common.GenerateInfraError(http.StatusForbidden, err)
		return installer.NewDownloadClusterFilesForbidden().WithPayload(payload)
	}

	respBody, contentLength, err := b.DownloadClusterFilesInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(respBody), params.FileName, contentLength)

}

func (b *bareMetalInventory) DownloadClusterFilesInternal(ctx context.Context, params installer.DownloadClusterFilesParams) (io.ReadCloser, int64, error) {

	log := logutil.FromContext(ctx, b.log)
	if err := b.checkFileForDownload(ctx, params.ClusterID.String(), params.FileName); err != nil {
		return nil, 0, err
	}

	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", params.ClusterID, params.FileName))
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", params.FileName, params.ClusterID.String())
		return nil, 0, common.NewApiError(http.StatusConflict, err)
	}

	return respBody, contentLength, nil
}

func (b *bareMetalInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	if err := b.checkFileForDownload(ctx, params.ClusterID.String(), constants.Kubeconfig); err != nil {
		return common.GenerateErrorResponder(err)
	}
	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", params.ClusterID, constants.Kubeconfig))
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(respBody), constants.Kubeconfig, contentLength)
}

func (b *bareMetalInventory) getLogFileForDownload(ctx context.Context, clusterId *strfmt.UUID, hostId *strfmt.UUID, logsType string) (string, string, error) {
	var fileName string
	var downloadFileName string
	c, err := b.getCluster(ctx, clusterId.String(), common.UseEagerLoading, common.IncludeDeletedRecords)
	if err != nil {
		return "", "", err
	}
	switch logsType {
	case string(models.LogsTypeHost):
		if hostId == nil {
			return "", "", common.NewApiError(http.StatusBadRequest, errors.Errorf("Host ID must be provided for downloading host logs"))
		}

		var hostObject *common.Host
		hostObject, err = common.GetClusterHostFromDB(b.db, clusterId.String(), hostId.String())
		if err != nil {
			return "", "", err
		}
		if time.Time(hostObject.LogsCollectedAt).Equal(time.Time{}) {
			return "", "", common.NewApiError(http.StatusConflict, errors.Errorf("Logs for host %s were not found", hostId))
		}
		fileName = b.getLogsFullName(clusterId.String(), hostObject.ID.String())
		role := string(hostObject.Role)
		if hostObject.Bootstrap {
			role = string(models.HostRoleBootstrap)
		}
		downloadFileName = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), role, sanitize.Name(hostutil.GetHostnameForMsg(&hostObject.Host)))
	case string(models.LogsTypeController):
		if time.Time(c.Cluster.ControllerLogsCollectedAt).Equal(time.Time{}) {
			return "", "", common.NewApiError(http.StatusConflict, errors.Errorf("Controller Logs for cluster %s were not found", clusterId))
		}
		fileName = b.getLogsFullName(clusterId.String(), logsType)
		downloadFileName = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), c.ID, logsType)
	default:
		fileName, err = b.prepareClusterLogs(ctx, c)
		downloadFileName = fmt.Sprintf("%s_%s.tar", sanitize.Name(c.Name), c.ID)
		if err != nil {
			return "", "", common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	return fileName, downloadFileName, nil
}

func (b *bareMetalInventory) checkFileForDownload(ctx context.Context, clusterID, fileName string) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Checking cluster cluster file for download: %s for cluster %s", fileName, clusterID)

	if !funk.Contains(clusterPkg.S3FileNames, fileName) && fileName != manifests.ManifestFolder {
		err := errors.Errorf("invalid cluster file %s", fileName)
		log.WithError(err).Errorf("failed download file: %s from cluster: %s", fileName, clusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}

	cluster, err := b.getCluster(ctx, clusterID)
	if err != nil {
		return err
	}

	switch fileName {
	case constants.Kubeconfig:
		err = clusterPkg.CanDownloadKubeconfig(cluster)
	case manifests.ManifestFolder:
		// do nothing. manifests can be downloaded at any given cluster state
	default:
		err = clusterPkg.CanDownloadFiles(cluster)
	}
	if err != nil {
		log.WithError(err).Errorf("failed to get file for cluster %s in current state", clusterID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) checkFileDownloadAccess(ctx context.Context, fileName string) error {
	// the OCM payload is only set in the cloud environment when the auth type is RHSSO
	if funk.Contains(clusterPkg.ClusterOwnerFileNames, fileName) && b.authHandler.AuthType() == auth.TypeRHSSO {
		authPayload := ocm.PayloadFromContext(ctx)
		if ocm.UserRole != authPayload.Role {
			errMsg := fmt.Sprintf("File '%v' is accessible only for cluster owners", fileName)
			return errors.New(errMsg)
		}
	}
	return nil
}

func (b *bareMetalInventory) UpdateHostIgnitionInternal(ctx context.Context, params installer.UpdateHostIgnitionParams) (*models.Host, error) {
	log := logutil.FromContext(ctx, b.log)

	h, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	if params.HostIgnitionParams.Config != "" {
		_, err = ignition.ParseToLatest([]byte(params.HostIgnitionParams.Config))
		if err != nil {
			log.WithError(err).Errorf("Failed to parse host ignition config patch %s", params.HostIgnitionParams)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and cluster_id = ?"), params.HostID, params.ClusterID).Update("ignition_config_overrides", params.HostIgnitionParams.Config).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendHostDiscoveryIgnitionConfigAppliedEvent(ctx, b.eventsHandler, params.HostID,
		params.ClusterID, hostutil.GetHostnameForMsg(&h.Host))
	log.Infof("Custom discovery ignition config was applied to host %s in cluster %s", params.HostID, params.ClusterID)
	h, err = b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	return &h.Host, nil
}

func (b *bareMetalInventory) UpdateHostIgnition(ctx context.Context, params installer.UpdateHostIgnitionParams) middleware.Responder {
	_, err := b.UpdateHostIgnitionInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateHostIgnitionCreated()
}

func (b *bareMetalInventory) GetHostIgnition(ctx context.Context, params installer.GetHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	_, respBody, _, err := b.downloadHostIgnition(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to download host %s ignition", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	respBytes, err := ioutil.ReadAll(respBody)
	if err != nil {
		log.WithError(err).Errorf("failed to read ignition content for host %s", params.HostID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return installer.NewGetHostIgnitionOK().WithPayload(&models.HostIgnitionParams{Config: string(respBytes)})
}

func (b *bareMetalInventory) DownloadHostIgnition(ctx context.Context, params installer.DownloadHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	fileName, respBody, contentLength, err := b.downloadHostIgnition(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to download host %s ignition", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(installer.NewDownloadHostIgnitionOK().WithPayload(respBody), fileName, contentLength)
}

func (b *bareMetalInventory) V2DownloadHostIgnition(ctx context.Context, params installer.V2DownloadHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	fileName, respBody, contentLength, err := b.v2DownloadHostIgnition(ctx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to download host %s ignition", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(installer.NewV2DownloadHostIgnitionOK().WithPayload(respBody), fileName, contentLength)
}

// v2DownloadHostIgnition returns the ignition file name, the content as an io.ReadCloser, and the file content length
func (b *bareMetalInventory) v2DownloadHostIgnition(ctx context.Context, infraEnvID string, hostID string) (string, io.ReadCloser, int64, error) {
	infraEnvHost, err := common.GetHostFromDB(b.db, infraEnvID, hostID)
	if err != nil {
		err = errors.Errorf("host %s not found in infra env %s", hostID, infraEnvID)
		return "", nil, 0, common.NewApiError(http.StatusNotFound, err)
	}

	// If host is not assigned to any cluster, we fail the ignition download.
	if infraEnvHost.ClusterID == nil {
		err = errors.Errorf("Cluster not found for host %s in infra env %s", hostID, infraEnvID)
		return "", nil, 0, common.NewApiError(http.StatusNotFound, err)
	}

	c, err := b.getCluster(ctx, infraEnvHost.ClusterID.String(), common.SkipEagerLoading)
	if err != nil {
		return "", nil, 0, err
	}

	// check if cluster is in the correct state to download files
	err = clusterPkg.CanDownloadFiles(c)
	if err != nil {
		return "", nil, 0, common.NewApiError(http.StatusConflict, err)
	}

	fileName := hostutil.IgnitionFileName(&infraEnvHost.Host)
	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", infraEnvHost.ClusterID.String(), fileName))
	if err != nil {
		return "", nil, 0, common.NewApiError(http.StatusInternalServerError, err)
	}

	return fileName, respBody, contentLength, nil
}

// downloadHostIgnition returns the ignition file name, the content as an io.ReadCloser, and the file content length
func (b *bareMetalInventory) downloadHostIgnition(ctx context.Context, clusterID string, hostID string) (string, io.ReadCloser, int64, error) {
	c, err := b.getCluster(ctx, clusterID, common.UseEagerLoading)
	if err != nil {
		return "", nil, 0, err
	}

	// check if host id is in cluster
	var host *models.Host
	for i, h := range c.Hosts {
		if h.ID.String() == hostID {
			host = c.Hosts[i]
			break
		}
	}
	if host == nil {
		err = errors.Errorf("host %s not found in cluster %s", hostID, clusterID)
		return "", nil, 0, common.NewApiError(http.StatusNotFound, err)
	}

	// check if cluster is in the correct state to download files
	err = clusterPkg.CanDownloadFiles(c)
	if err != nil {
		return "", nil, 0, common.NewApiError(http.StatusConflict, err)
	}

	fileName := hostutil.IgnitionFileName(host)
	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterID, fileName))
	if err != nil {
		return "", nil, 0, common.NewApiError(http.StatusInternalServerError, err)
	}

	return fileName, respBody, contentLength, nil
}

func (b *bareMetalInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	c, err := b.GetCredentialsInternal(ctx, installer.V2GetCredentialsParams(params))
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetCredentialsOK().WithPayload(c)
}

func (b *bareMetalInventory) GetCredentialsInternal(ctx context.Context, params installer.V2GetCredentialsParams) (*models.Credentials, error) {

	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	db := common.LoadTableFromDB(b.db, common.MonitoredOperatorsTable)
	if err := db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		return nil, err
	}
	if !b.clusterApi.IsOperatorAvailable(&cluster, operators.OperatorConsole.Name) {
		err := errors.New("console-url isn't available yet, it will be once console operator is ready as part of cluster finalizing stage")
		log.WithError(err)
		return nil, common.NewApiError(http.StatusConflict, err)
	}
	objectName := fmt.Sprintf("%s/%s", params.ClusterID, "kubeadmin-password")
	r, _, err := b.objectHandler.Download(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get clusters %s object", objectName)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	defer r.Close()
	password, err := ioutil.ReadAll(r)
	if err != nil {
		log.WithError(errors.Errorf("%s", password)).Errorf("Failed to get clusters %s", objectName)
		return nil, common.NewApiError(http.StatusConflict, errors.New(string(password)))
	}
	return &models.Credentials{
		Username:   DefaultUser,
		Password:   string(password),
		ConsoleURL: common.GetConsoleUrl(cluster.Name, cluster.BaseDNSDomain),
	}, nil
}

func (b *bareMetalInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Update host %s install progress", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host %s", params.HostID)
		return installer.NewUpdateHostInstallProgressNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if params.HostProgress.CurrentStage != host.Progress.CurrentStage || params.HostProgress.ProgressInfo != host.Progress.ProgressInfo {
		if err := b.hostApi.UpdateInstallProgress(ctx, &host.Host, params.HostProgress); err != nil {
			log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
			return installer.NewUpdateHostInstallProgressInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}

		event := fmt.Sprintf("reached installation stage %s", params.HostProgress.CurrentStage)
		if params.HostProgress.ProgressInfo != "" {
			event += fmt.Sprintf(": %s", params.HostProgress.ProgressInfo)
		}

		log.Info(fmt.Sprintf("Host %s in cluster %s: %s", host.ID, host.ClusterID, event))
		eventgen.SendHostInstallProgressUpdatedEvent(ctx, b.eventsHandler, *host.ID, host.InfraEnvID, host.ClusterID, hostutil.GetHostnameForMsg(&host.Host), event)

		if err := b.clusterApi.UpdateInstallProgress(ctx, params.ClusterID); err != nil {
			log.WithError(err).Errorf("failed to update cluster %s progress", params.ClusterID)
			return installer.NewUpdateHostInstallProgressInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	return installer.NewUpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	return b.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
		ClusterID:             params.ClusterID,
		DiscoveryAgentVersion: params.DiscoveryAgentVersion,
		IngressCertParams:     params.IngressCertParams,
	})
}

// Merging given ingress ca certificate into kubeconfig
// Code was taken from openshift installer
func mergeIngressCaIntoKubeconfig(kubeconfigData []byte, ingressCa []byte, log logrus.FieldLogger) ([]byte, error) {

	kconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		log.WithError(err).Errorf("Failed to convert kubeconfig data")
		return nil, err
	}
	if kconfig == nil || len(kconfig.Clusters) == 0 {
		err = errors.Errorf("kubeconfig is missing expected data")
		log.Error(err)
		return nil, err
	}

	for _, c := range kconfig.Clusters {
		clusterCABytes := c.CertificateAuthorityData
		if len(clusterCABytes) == 0 {
			err = errors.Errorf("kubeconfig CertificateAuthorityData not found")
			log.Errorf("%e, data %s", err, c.CertificateAuthorityData)
			return nil, err
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(clusterCABytes) {
			err = errors.Errorf("cluster CA found in kubeconfig not valid PEM format")
			log.Errorf("%e, ca :%s", err, clusterCABytes)
			return nil, err
		}
		if !certPool.AppendCertsFromPEM(ingressCa) {
			err = errors.Errorf("given ingress-ca is not valid PEM format")
			log.Errorf("%e %s", err, ingressCa)
			return nil, err
		}

		newCA := append(ingressCa, clusterCABytes...)
		c.CertificateAuthorityData = newCA
	}

	kconfigAsByteArray, err := clientcmd.Write(*kconfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert kubeconfig")
	}
	return kconfigAsByteArray, nil
}

func setPullSecret(cluster *common.Cluster, pullSecret string) {
	cluster.PullSecret = pullSecret
	if pullSecret != "" {
		cluster.PullSecretSet = true
	} else {
		cluster.PullSecretSet = false
	}
}

func setInfraEnvPullSecret(infraEnv *common.InfraEnv, pullSecret string) {
	infraEnv.PullSecret = pullSecret
	if pullSecret != "" {
		infraEnv.PullSecretSet = true
	} else {
		infraEnv.PullSecretSet = false
	}
}

func (b *bareMetalInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	return b.V2CancelInstallation(ctx, installer.V2CancelInstallationParams(params))
}

func (b *bareMetalInventory) CancelInstallationInternal(ctx context.Context, params installer.V2CancelInstallationParams) (*common.Cluster, error) {

	log := logutil.FromContext(ctx, b.log)
	log.Infof("canceling installation for cluster %s", params.ClusterID)

	cluster := &common.Cluster{}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("cancel installation failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("cancel installation failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		msg := "Failed to cancel installation: error starting DB transaction"
		log.WithError(tx.Error).Errorf(msg)
		eventgen.SendCancelInstallStartFailedEvent(ctx, b.eventsHandler, *cluster.ID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	var err error
	if cluster, err = common.GetClusterFromDBForUpdate(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("Failed to cancel installation: could not find cluster %s", params.ClusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewApiError(http.StatusNotFound, err)
		}
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	// cancellation is made by setting the cluster and and hosts states to error.
	if err := b.clusterApi.CancelInstallation(ctx, cluster, "Installation was cancelled by user", tx); err != nil {
		return nil, err
	}
	for _, h := range cluster.Hosts {
		if err := b.hostApi.CancelInstallation(ctx, h, "Installation was cancelled by user", tx); err != nil {
			return nil, err
		}
		if err := b.customizeHost(&cluster.Cluster, h); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf("Failed to cancel installation: error committing DB transaction (%s)", err)
		eventgen.SendCancelInstallCommitFailedEvent(ctx, b.eventsHandler, *cluster.ID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	return cluster, nil
}

func (b *bareMetalInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	return b.V2ResetCluster(ctx, installer.V2ResetClusterParams{ClusterID: params.ClusterID})
}

func (b *bareMetalInventory) InstallHost(ctx context.Context, params installer.InstallHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var h *models.Host
	var cluster *common.Cluster
	var err error

	log.Info("Install single day2 host: ", params.HostID)
	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}
	for i := range cluster.Hosts {
		if *cluster.Hosts[i].ID == params.HostID {
			h = cluster.Hosts[i]
			break
		}
	}
	if h == nil {
		log.WithError(err).Errorf("host %s not found", params.HostID)
		return common.NewApiError(http.StatusNotFound, err)
	}

	if !hostutil.IsDay2Host(h) {
		log.Errorf("InstallHost for host %s is forbidden: not a Day2 hosts", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when adding hosts to an existing cluster"))
	}

	if swag.StringValue(h.Status) != models.HostStatusKnown {
		log.Errorf("Install host for host %s, state %s is forbidden: host not in Known state", params.HostID.String(), swag.StringValue(h.Status))
		return common.NewApiError(http.StatusConflict, fmt.Errorf("cannot install host in state %s", swag.StringValue(h.Status)))
	}

	_, err = b.hostApi.AutoAssignRole(ctx, h, b.db)
	if err != nil {
		log.Errorf("Failed to update role for host %s", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	err = b.hostApi.RefreshStatus(ctx, h, b.db)
	if err != nil {
		log.Errorf("Failed to refresh host %s", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	if swag.StringValue(h.Status) != models.HostStatusKnown {
		return common.NewApiError(http.StatusConflict, fmt.Errorf("cannot install host in state %s after refresh", swag.StringValue(h.Status)))
	}
	err = b.createAndUploadNodeIgnition(ctx, cluster, h)
	if err != nil {
		log.Errorf("Failed to upload ignition for host %s", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}
	err = b.hostApi.Install(ctx, h, b.db)
	if err != nil {
		// we just logs the error, each host install is independent
		log.Errorf("Failed to move host %s to installing", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}

	return installer.NewInstallHostAccepted().WithPayload(h)
}

func (b *bareMetalInventory) V2ResetHost(ctx context.Context, params installer.V2ResetHostParams) middleware.Responder {
	host, err := b.resetHost(ctx, params.HostID, params.InfraEnvID)
	if err != nil {
		return err
	}
	return installer.NewV2ResetHostOK().WithPayload(host)
}

func (b *bareMetalInventory) ResetHost(ctx context.Context, params installer.ResetHostParams) middleware.Responder {
	host, err := b.resetHost(ctx, params.HostID, params.ClusterID)
	if err != nil {
		return err
	}
	return installer.NewResetHostOK().WithPayload(host)
}

func (b *bareMetalInventory) resetHost(ctx context.Context, hostId, infraEnvId strfmt.UUID) (*models.Host, middleware.Responder) {
	log := logutil.FromContext(ctx, b.log)
	log.Info("Resetting host: ", hostId)
	host, err := common.GetHostFromDB(b.db, infraEnvId.String(), hostId.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", hostId)
			return nil, common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", hostId)
		eventgen.SendHostResetFetchFailedEvent(ctx, b.eventsHandler, hostId, infraEnvId, hostutil.GetHostnameForMsg(&host.Host))
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if !hostutil.IsDay2Host(&host.Host) {
		log.Errorf("ResetHost for host %s is forbidden: not a Day2 hosts", hostId)
		return nil, common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when adding hosts to an existing cluster"))
	}

	if host.ClusterID == nil {
		err = fmt.Errorf("host %s is not bound to any cluster, cannot reset host", hostId)
		log.Errorf("ResetHost for host %s is forbidden: not a Day2 hosts", hostId)
		return nil, common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when host assigned to an existing cluster"))
	}

	cluster, err := common.GetClusterFromDB(b.db, *host.ClusterID, common.SkipEagerLoading)
	if err != nil {
		err = fmt.Errorf("can not find a cluster for host %s, cannot reset host", hostId)
		log.Errorln(err.Error())
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.db.Transaction(func(tx *gorm.DB) error {
		if errResponse := b.hostApi.ResetHost(ctx, &host.Host, "host was reset by user", tx); errResponse != nil {
			return errResponse
		}
		if err = b.customizeHost(&cluster.Cluster, &host.Host); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, common.GenerateErrorResponder(err)
	}

	return &host.Host, nil
}

func (b *bareMetalInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	return b.V2CompleteInstallation(ctx, installer.V2CompleteInstallationParams{
		ClusterID:             params.ClusterID,
		CompletionParams:      params.CompletionParams,
		DiscoveryAgentVersion: params.DiscoveryAgentVersion,
	})
}

func (b *bareMetalInventory) deleteDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.dnsApi.DeleteDNSRecordSets(ctx, &cluster)
}

func (b *bareMetalInventory) validateIgnitionEndpointURL(ignitionEndpoint *models.IgnitionEndpoint, log logrus.FieldLogger) error {
	if ignitionEndpoint == nil || ignitionEndpoint.URL == nil {
		return nil
	}
	if err := validations.ValidateHTTPFormat(*ignitionEndpoint.URL); err != nil {
		log.WithError(err).Errorf("Invalid Ignition endpoint URL: %s", *ignitionEndpoint.URL)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	return nil
}

func (b *bareMetalInventory) validateDNSDomain(cluster common.Cluster, params installer.V2UpdateClusterParams, log logrus.FieldLogger) error {
	clusterName := swag.StringValue(params.ClusterUpdateParams.Name)
	if clusterName == "" {
		clusterName = cluster.Name
	}
	clusterBaseDomain := swag.StringValue(params.ClusterUpdateParams.BaseDNSDomain)
	if clusterBaseDomain == "" {
		clusterBaseDomain = cluster.BaseDNSDomain
	}
	if clusterBaseDomain != "" {
		if err := b.dnsApi.ValidateDNSName(clusterName, clusterBaseDomain); err != nil {
			log.WithError(err).Errorf("Invalid cluster domain: %s.%s", clusterName, clusterBaseDomain)
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}
	dnsDomain, err := b.dnsApi.GetDNSDomain(clusterName, clusterBaseDomain)
	if err == nil && dnsDomain != nil {
		// Cluster's baseDNSDomain is defined in config (BaseDNSDomains map)
		if err = b.dnsApi.ValidateBaseDNS(dnsDomain); err != nil {
			log.WithError(err).Errorf("Invalid base DNS domain: %s", clusterBaseDomain)
			return common.NewApiError(http.StatusConflict, errors.New("Base DNS domain isn't configured properly"))
		}
		if err = b.dnsApi.ValidateDNSRecords(cluster, dnsDomain); err != nil {
			log.WithError(err).Errorf("DNS records already exist for cluster: %s", params.ClusterID)
			return common.NewApiError(http.StatusConflict,
				errors.New("DNS records already exist for cluster - please change 'Cluster Name'"))
		}
	}
	return nil
}

func ipAsUint(ipStr string, log logrus.FieldLogger) uint64 {
	parts := strings.Split(ipStr, ".")
	if len(parts) != 4 {
		log.Warnf("Invalid ip %s", ipStr)
		return 0
	}
	var result uint64 = 0
	for _, p := range parts {
		result = result << 8
		converted, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			log.WithError(err).Warnf("Conversion of %s to uint", p)
			return 0
		}
		result += converted
	}
	return result
}

func applyLimit(ret models.FreeAddressesList, limitParam *int64) models.FreeAddressesList {
	if limitParam != nil && *limitParam >= 0 && *limitParam < int64(len(ret)) {
		return ret[:*limitParam]
	}
	return ret
}

func (b *bareMetalInventory) getFreeAddresses(_ context.Context, params installer.GetFreeAddressesParams, log logrus.FieldLogger) (models.FreeAddressesList, error) {
	var hosts []*models.Host
	err := b.db.Select("free_addresses").Find(&hosts, "cluster_id = ? and status in (?)", params.ClusterID.String(), []string{models.HostStatusInsufficient, models.HostStatusKnown}).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Error retreiving hosts for cluster %s", params.ClusterID.String()))
	}
	if len(hosts) == 0 {
		return nil, common.NewApiError(http.StatusNotFound, errors.Errorf("No hosts where found for cluster %s", params.ClusterID))
	}
	resultingSet := network.MakeFreeAddressesSet(hosts, params.Network, params.Prefix, log)

	ret := models.FreeAddressesList{}
	for a := range resultingSet {
		ret = append(ret, a)
	}

	// Sort addresses
	sort.Slice(ret, func(i, j int) bool {
		return ipAsUint(ret[i].String(), log) < ipAsUint(ret[j].String(), log)
	})

	ret = applyLimit(ret, params.Limit)

	return ret, nil
}

func (b *bareMetalInventory) GetFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	results, err := b.getFreeAddresses(ctx, params, log)
	if err != nil {
		log.WithError(err).Warn("GetFreeAddresses")
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetFreeAddressesOK().WithPayload(results)
}

func (b *bareMetalInventory) UpdateClusterLogsProgress(ctx context.Context, params installer.UpdateClusterLogsProgressParams) middleware.Responder {
	return b.V2UpdateClusterLogsProgress(ctx, installer.V2UpdateClusterLogsProgressParams{
		ClusterID:          params.ClusterID,
		LogsProgressParams: params.LogsProgressParams,
	})
}

func (b *bareMetalInventory) UpdateHostLogsProgress(ctx context.Context, params installer.UpdateHostLogsProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("update log progress on host %s on %s cluster to %s", params.HostID, params.ClusterID, common.LogStateValue(params.LogsProgressParams.LogsState))
	currentHost, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err == nil {
		err = b.hostApi.UpdateLogsProgress(ctx, &currentHost.Host, string(common.LogStateValue(params.LogsProgressParams.LogsState)))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on cluster %s host %s", common.LogStateValue(params.LogsProgressParams.LogsState), params.ClusterID.String(), params.HostID.String())
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateHostLogsProgressNoContent()
}

func (b *bareMetalInventory) UploadLogs(ctx context.Context, params installer.UploadLogsParams) middleware.Responder {
	err := b.v1uploadLogs(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUploadLogsNoContent()
}

func (b *bareMetalInventory) v1uploadLogs(ctx context.Context, params installer.UploadLogsParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Uploading logs from cluster %s", params.ClusterID)

	defer func() {
		// Closing file and removing all temporary files created by Multipart
		params.Upfile.Close()
		params.HTTPRequest.Body.Close()
		err := params.HTTPRequest.MultipartForm.RemoveAll()
		if err != nil {
			log.WithError(err).Warnf("Failed to delete temporary files used for upload")
		}
	}()

	if params.LogsType == string(models.LogsTypeHost) {
		dbHost, err := b.GetCommonHostInternal(ctx, params.ClusterID.String(), params.HostID.String())
		if err != nil {
			return err
		}

		err = b.uploadHostLogs(ctx, dbHost, params.Upfile)
		if err != nil {
			return err
		}

		eventgen.SendHostLogsUploadedEvent(ctx, b.eventsHandler, *params.HostID, dbHost.InfraEnvID, &params.ClusterID,
			hostutil.GetHostnameForMsg(&dbHost.Host))
		return nil
	}

	currentCluster, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return err
	}
	fileName := b.getLogsFullName(params.ClusterID.String(), params.LogsType)
	log.Debugf("Start upload log file %s to bucket %s", fileName, b.S3Bucket)
	err = b.objectHandler.UploadStream(ctx, params.Upfile, fileName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload %s to s3", fileName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if params.LogsType == string(models.LogsTypeController) {
		firstClusterLogCollectionEvent := false
		if time.Time(currentCluster.ControllerLogsCollectedAt).Equal(time.Time{}) {
			firstClusterLogCollectionEvent = true
		}
		err = b.clusterApi.SetUploadControllerLogsAt(ctx, currentCluster, b.db)
		if err != nil {
			log.WithError(err).Errorf("Failed update cluster %s controller_logs_collected_at flag", params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		err = b.clusterApi.UpdateLogsProgress(ctx, currentCluster, string(models.LogsStateCollecting))
		if err != nil {
			log.WithError(err).Errorf("Failed update cluster %s log progress %s", params.ClusterID, string(models.LogsStateCollecting))
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		if firstClusterLogCollectionEvent { // Issue an event only for the very first cluster log collection event.
			eventgen.SendClusterLogsUploadedEvent(ctx, b.eventsHandler, params.ClusterID)
		}
	}

	log.Infof("Done uploading file %s", fileName)
	return nil
}

func (b *bareMetalInventory) uploadHostLogs(ctx context.Context, host *common.Host, upFile io.ReadCloser) error {
	log := logutil.FromContext(ctx, b.log)

	var logPrefix string
	if host.ClusterID != nil {
		logPrefix = host.ClusterID.String()
	} else {
		logPrefix = host.InfraEnvID.String()
	}
	fileName := b.getLogsFullName(logPrefix, host.ID.String())

	log.Debugf("Start upload log file %s to bucket %s", fileName, b.S3Bucket)
	err := b.objectHandler.UploadStream(ctx, upFile, fileName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload %s to s3 for host %s", fileName, host.ID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.hostApi.SetUploadLogsAt(ctx, &host.Host, b.db)
	if err != nil {
		log.WithError(err).Errorf("Failed update host %s logs_collected_at flag", host.ID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	err = b.hostApi.UpdateLogsProgress(ctx, &host.Host, string(models.LogsStateCollecting))
	if err != nil {
		log.WithError(err).Errorf("Failed update host %s log progress %s", host.ID.String(), string(models.LogsStateCollecting))
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (b *bareMetalInventory) DownloadClusterLogs(ctx context.Context, params installer.DownloadClusterLogsParams) middleware.Responder {
	return b.V2DownloadClusterLogs(ctx, installer.V2DownloadClusterLogsParams(params))
}

func (b *bareMetalInventory) UploadHostLogs(ctx context.Context, params installer.UploadHostLogsParams) middleware.Responder {
	err := b.v1uploadLogs(ctx, installer.UploadLogsParams{ClusterID: params.ClusterID, HostID: &params.HostID, HTTPRequest: params.HTTPRequest,
		LogsType: string(models.LogsTypeHost), Upfile: params.Upfile})
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUploadHostLogsNoContent()
}

func (b *bareMetalInventory) DownloadHostLogs(ctx context.Context, params installer.DownloadHostLogsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Downloading logs from host %s in cluster %s", params.HostID, params.ClusterID)
	fileName, downloadFileName, err := b.getLogFileForDownload(ctx, &params.ClusterID, &params.HostID, "host")
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	respBody, contentLength, err := b.objectHandler.Download(ctx, fileName)
	if err != nil {
		if _, ok := err.(common.NotFound); ok {
			log.WithError(err).Warnf("File not found %s", fileName)
			return common.NewApiError(http.StatusNotFound, errors.Errorf("Logs for host %s were not found", params.HostID))
		}

		log.WithError(err).Errorf("failed to download file %s", fileName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return filemiddleware.NewResponder(installer.NewDownloadHostLogsOK().WithPayload(respBody), downloadFileName, contentLength)
}

func (b *bareMetalInventory) prepareClusterLogs(ctx context.Context, cluster *common.Cluster) (string, error) {
	fileName, err := b.clusterApi.CreateTarredClusterLogs(ctx, cluster, b.objectHandler)
	if err != nil {
		return "", err
	}
	return fileName, nil
}

func (b *bareMetalInventory) getLogsFullName(clusterId string, logId string) string {
	return fmt.Sprintf("%s/logs/%s/logs.tar.gz", clusterId, logId)
}

func (b *bareMetalInventory) getHost(ctx context.Context, clusterId string, hostId string) (*common.Host, error) {
	log := logutil.FromContext(ctx, b.log)
	host, err := common.GetHostFromDB(b.db, clusterId, hostId)
	if err != nil {
		log.WithError(err).Errorf("failed to find host: %s", hostId)
		return nil, common.NewApiError(http.StatusNotFound, errors.Errorf("Host %s not found", hostId))
	}
	return host, nil
}

func (b *bareMetalInventory) GetCommonHostInternal(_ context.Context, infraEnvId, hostId string) (*common.Host, error) {
	return common.GetHostFromDB(b.db, infraEnvId, hostId)
}

func (b *bareMetalInventory) UpdateHostApprovedInternal(ctx context.Context, infraEnvId, hostId string, approved bool) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Updating Approved to %t Host %s InfraEnv %s", approved, hostId, infraEnvId)
	dbHost, err := common.GetHostFromDB(b.db, infraEnvId, hostId)
	if err != nil {
		return err
	}
	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and infra_env_id = ?"), hostId, infraEnvId).Update("approved", approved).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update 'approved' in host: %s", hostId)
		return err
	}
	eventgen.SendHostApprovedUpdatedEvent(ctx, b.eventsHandler, *dbHost.ID, strfmt.UUID(infraEnvId),
		hostutil.GetHostnameForMsg(&dbHost.Host), approved)
	return nil
}

func (b *bareMetalInventory) getCluster(ctx context.Context, clusterID string, flags ...interface{}) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)

	cluster, err := common.GetClusterFromDBWhere(b.db,
		common.EagerLoadingState(funk.Contains(flags, common.UseEagerLoading)),
		common.DeleteRecordsState(funk.Contains(flags, common.IncludeDeletedRecords)),
		"id = ?", clusterID)

	if err != nil {
		log.Error(err)
		return nil, err
	}
	return cluster, nil
}

func (b *bareMetalInventory) customizeHost(cluster *models.Cluster, host *models.Host) error {
	var isSno = false
	if cluster != nil {
		isSno = swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
	}
	host.ProgressStages = b.hostApi.GetStagesByRole(host.Role, host.Bootstrap, isSno)
	host.RequestedHostname = hostutil.GetHostnameForMsg(host)
	return nil
}

func proxySettingsChanged(params *models.V2ClusterUpdateParams, cluster *common.Cluster) bool {
	if (params.HTTPProxy != nil && cluster.HTTPProxy != swag.StringValue(params.HTTPProxy)) ||
		(params.HTTPSProxy != nil && cluster.HTTPSProxy != swag.StringValue(params.HTTPSProxy)) ||
		(params.NoProxy != nil && cluster.NoProxy != swag.StringValue(params.NoProxy)) {
		return true
	}
	return false
}

// computes the cluster proxy hash in order to identify if proxy settings were changed which will indicated if
// new ISO file should be generated to contain new proxy settings
func computeProxyHash(proxy *models.Proxy) (string, error) {
	var proxyHash string
	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(proxy)
	proxyHash += httpProxy
	proxyHash += httpsProxy
	proxyHash += noProxy
	// #nosec
	h := md5.New()
	_, err := h.Write([]byte(proxyHash))
	if err != nil {
		return "", err
	}
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs), nil
}

func validateProxySettings(httpProxy, httpsProxy, noProxy, ocpVersion *string) error {
	if httpProxy != nil && *httpProxy != "" {
		if err := validations.ValidateHTTPProxyFormat(*httpProxy); err != nil {
			return errors.Errorf("Failed to validate HTTP Proxy: %s", err)
		}
	}
	if httpsProxy != nil && *httpsProxy != "" {
		if err := validations.ValidateHTTPProxyFormat(*httpsProxy); err != nil {
			return errors.Errorf("Failed to validate HTTPS Proxy: %s", err)
		}
	}
	if noProxy != nil && *noProxy != "" {
		if ocpVersion == nil {
			return errors.Errorf("Cannot validate NoProxy: Unknown OpenShift version")
		}
		if err := validations.ValidateNoProxyFormat(*noProxy, *ocpVersion); err != nil {
			return err
		}
	}
	return nil
}

func secretValidationToUserError(err error) error {

	if _, ok := err.(*validations.PullSecretError); ok {
		return err
	}

	return errors.New("Failed validating pull secret")
}

func (b *bareMetalInventory) GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error) {
	return b.clusterApi.GetClusterByKubeKey(key)
}

func (b *bareMetalInventory) GetHostByKubeKey(key types.NamespacedName) (*common.Host, error) {
	h, err := b.hostApi.GetHostByKubeKey(key)
	if err != nil {
		return nil, err
	}

	//customize the host as done in the REST API
	var cluster *common.Cluster
	var c *models.Cluster
	if h.ClusterID != nil {
		cluster, err = common.GetClusterFromDB(b.db, *h.ClusterID, common.SkipEagerLoading)
		if err != nil {
			return h, fmt.Errorf("can not find a cluster for host %s", h.ID.String())
		}
		c = &cluster.Cluster
	}

	err = b.customizeHost(c, &h.Host)
	return h, err
}

func (b *bareMetalInventory) GetClusterHostRequirements(ctx context.Context, params installer.GetClusterHostRequirementsParams) middleware.Responder {

	cluster, err := b.getCluster(ctx, params.ClusterID.String(), common.UseEagerLoading)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	requirementsList := models.ClusterHostRequirementsList{}
	for _, clusterHost := range cluster.Hosts {
		hostRequirements, err := b.hwValidator.GetClusterHostRequirements(ctx, cluster, clusterHost)
		if err != nil {
			return common.GenerateErrorResponder(err)
		}
		requirementsList = append(requirementsList, hostRequirements)
	}

	return installer.NewGetClusterHostRequirementsOK().WithPayload(requirementsList)
}

func (b *bareMetalInventory) GetPreflightRequirements(ctx context.Context, params installer.GetPreflightRequirementsParams) middleware.Responder {
	return b.V2GetPreflightRequirements(ctx, installer.V2GetPreflightRequirementsParams{ClusterID: params.ClusterID})
}

func (b *bareMetalInventory) V2ResetHostValidation(ctx context.Context, params installer.V2ResetHostValidationParams) middleware.Responder {
	err := b.hostApi.ResetHostValidation(ctx, params.HostID, params.InfraEnvID, params.ValidationID, nil)
	if err != nil {
		log := logutil.FromContext(ctx, b.log)
		log.WithError(err).Error("Reset host validation")
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2ResetHostValidationOK()
}

func (b *bareMetalInventory) ResetHostValidation(ctx context.Context, params installer.ResetHostValidationParams) middleware.Responder {
	err := b.hostApi.ResetHostValidation(ctx, params.HostID, params.ClusterID, params.ValidationID, nil)
	if err != nil {
		log := logutil.FromContext(ctx, b.log)
		log.WithError(err).Error("Reset host validation")
		return common.GenerateErrorResponder(err)
	}
	return installer.NewResetHostValidationOK()
}

func (b *bareMetalInventory) AddReleaseImage(ctx context.Context, releaseImageUrl, pullSecret string) (*models.ReleaseImage, error) {
	log := logutil.FromContext(ctx, b.log)

	// Create a new OpenshiftVersion and add it to versions cache
	releaseImage, err := b.versionsHandler.AddReleaseImage(releaseImageUrl, pullSecret)
	if err != nil {
		log.WithError(err).Errorf("Failed to add OCP version for release image: %s", releaseImageUrl)
		return nil, err
	}

	return releaseImage, nil
}

func (b *bareMetalInventory) DeregisterInfraEnv(ctx context.Context, params installer.DeregisterInfraEnvParams) middleware.Responder {
	if err := b.DeregisterInfraEnvInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewDeregisterInfraEnvNoContent()
}

func (b *bareMetalInventory) DeregisterInfraEnvInternal(ctx context.Context, params installer.DeregisterInfraEnvParams) error {
	log := logutil.FromContext(ctx, b.log)
	var infraEnv *common.InfraEnv
	var err error
	success := false
	log.Infof("Deregister infraEnv id %s", params.InfraEnvID)

	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully deregistered InfraEnv %s", params.InfraEnvID.String())
			log.Info(msg)
			eventgen.SendInfraEnvDeregisteredEvent(ctx, b.eventsHandler, params.InfraEnvID)
		} else {
			errMsg := fmt.Sprintf("Failed to deregister InfraEnv %s", params.InfraEnvID.String())
			errString := ""
			if err != nil {
				errString = err.Error()
				errMsg = fmt.Sprintf("%s. Error: %s", errMsg, errString)
			}
			log.Errorf(errMsg)
			eventgen.SendInfraEnvDeregisterFailedEvent(ctx, b.eventsHandler, params.InfraEnvID, errString)
		}
	}()

	if infraEnv, err = common.GetInfraEnvFromDB(b.db, params.InfraEnvID); err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	hosts, err := common.GetHostsFromDBWhere(b.db, "infra_env_id = ?", params.InfraEnvID)
	if err != nil {
		return err
	}
	if len(hosts) > 0 {
		msg := fmt.Sprintf("failed to deregister infraEnv %s, %d hosts are still associated", params.InfraEnvID, len(hosts))
		log.Error(msg)
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf(msg))
	}

	// Delete discovery image for deregistered infraEnv
	discoveryImage := fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, params.InfraEnvID.String()))
	exists, err := b.objectHandler.DoesObjectExist(ctx, discoveryImage)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", params.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if exists {
		_, err = b.objectHandler.DeleteObject(ctx, discoveryImage)
		if err != nil {
			log.WithError(err).Errorf("failed to deregister infraEnv %s", params.InfraEnvID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err = b.db.Delete(infraEnv).Error; err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", params.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	success = true
	return nil
}

func (b *bareMetalInventory) GetInfraEnvHostsInternal(ctx context.Context, infraEnvId strfmt.UUID) ([]*common.Host, error) {
	return common.GetInfraEnvHostsFromDB(b.db, infraEnvId)
}

func (b *bareMetalInventory) DownloadInfraEnvDiscoveryImage(ctx context.Context, params installer.DownloadInfraEnvDiscoveryImageParams) middleware.Responder {
	return b.DownloadISOInternal(ctx, params.InfraEnvID)
}

func (b *bareMetalInventory) DownloadInfraEnvDiscoveryImageHeaders(ctx context.Context, params installer.DownloadInfraEnvDiscoveryImageHeadersParams) middleware.Responder {
	return b.DownloadISOHeadersInternal(ctx, params.InfraEnvID)
}

func (b *bareMetalInventory) GetInfraEnv(ctx context.Context, params installer.GetInfraEnvParams) middleware.Responder {
	i, err := b.GetInfraEnvInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetInfraEnvOK().WithPayload(&i.InfraEnv)
}

func (b *bareMetalInventory) GetInfraEnvInternal(ctx context.Context, params installer.GetInfraEnvParams) (*common.InfraEnv, error) {
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return nil, err
	}
	return infraEnv, nil
}

func (b *bareMetalInventory) ListInfraEnvs(ctx context.Context, params installer.ListInfraEnvsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	db := b.db
	var dbInfraEnvs []*common.InfraEnv
	var infraEnvs []*models.InfraEnv
	whereCondition := identity.AddUserFilter(ctx, "")

	dbInfraEnvs, err := common.GetInfraEnvsFromDBWhere(db, whereCondition)
	if err != nil {
		log.WithError(err).Error("Failed to list infraEnvs in db")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	for _, i := range dbInfraEnvs {
		infraEnvs = append(infraEnvs, &i.InfraEnv)
	}
	return installer.NewListInfraEnvsOK().WithPayload(infraEnvs)
}

func (b *bareMetalInventory) RegisterInfraEnv(ctx context.Context, params installer.RegisterInfraEnvParams) middleware.Responder {
	i, err := b.RegisterInfraEnvInternal(ctx, nil, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterInfraEnvCreated().WithPayload(&i.InfraEnv)
}

func (b *bareMetalInventory) RegisterInfraEnvInternal(
	ctx context.Context,
	kubeKey *types.NamespacedName,
	params installer.RegisterInfraEnvParams) (*common.InfraEnv, error) {

	id := strfmt.UUID(uuid.New().String())
	url := installer.GetInfraEnvURL{InfraEnvID: id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	log.Infof("Register infraenv: %s with id %s", swag.StringValue(params.InfraenvCreateParams.Name), id)
	success := false
	var err error
	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully registered InfraEnv %s with id %s",
				swag.StringValue(params.InfraenvCreateParams.Name), id)
			log.Info(msg)
			eventgen.SendInfraEnvRegisteredEvent(ctx, b.eventsHandler, id)
		} else {
			errMsg := fmt.Sprintf("Failed to register InfraEnv %s with id %s",
				swag.StringValue(params.InfraenvCreateParams.Name), id)
			errString := ""
			if err != nil {
				errString = err.Error()
				errMsg = fmt.Sprintf("%s. Error: %s", errMsg, errString)
			}
			log.Errorf(errMsg)
			eventgen.SendInfraEnvRegistrationFailedEvent(ctx, b.eventsHandler, id, errString)
		}
	}()

	params = b.setDefaultRegisterInfraEnvParams(ctx, params)

	if params.InfraenvCreateParams.Proxy != nil {
		if err = validateProxySettings(params.InfraenvCreateParams.Proxy.HTTPProxy,
			params.InfraenvCreateParams.Proxy.HTTPSProxy,
			params.InfraenvCreateParams.Proxy.NoProxy, params.InfraenvCreateParams.OpenshiftVersion); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.InfraenvCreateParams.AdditionalNtpSources != nil && swag.StringValue(params.InfraenvCreateParams.AdditionalNtpSources) != b.Config.DefaultNTPSource {
		ntpSource := swag.StringValue(params.InfraenvCreateParams.AdditionalNtpSources)

		if ntpSource != "" && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err = errors.Errorf("Invalid NTP source: %s", ntpSource)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	} else {
		params.InfraenvCreateParams.AdditionalNtpSources = swag.String(b.Config.DefaultNTPSource)
	}

	osImage, err := b.getOsImageOrLatest(params.InfraenvCreateParams.OpenshiftVersion, params.InfraenvCreateParams.CPUArchitecture)
	if err != nil {
		return nil, err
	}

	if params.InfraenvCreateParams.SSHAuthorizedKey != nil && *params.InfraenvCreateParams.SSHAuthorizedKey != "" {
		if err = validations.ValidateSSHPublicKey(*params.InfraenvCreateParams.SSHAuthorizedKey); err != nil {
			err = errors.Errorf("SSH key is not valid")
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.InfraenvCreateParams.StaticNetworkConfig != nil {
		if err = b.staticNetworkConfig.ValidateStaticConfigParams(ctx, params.InfraenvCreateParams.StaticNetworkConfig); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if err = b.validateInfraEnvIgnitionParams(ctx, params.InfraenvCreateParams.IgnitionConfigOverride); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	err = b.validateClusterInfraEnvRegister(params.InfraenvCreateParams.ClusterID, params.InfraenvCreateParams.CPUArchitecture)
	if err != nil {
		return nil, err
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	// generate key for signing rhsso image auth tokens
	imageTokenKey, err := gencrypto.HMACKey(32)
	if err != nil {
		return nil, err
	}

	infraEnv := common.InfraEnv{
		Generated: false,
		InfraEnv: models.InfraEnv{
			ID:                     &id,
			Href:                   swag.String(url.String()),
			Kind:                   swag.String(models.InfraEnvKindInfraEnv),
			Name:                   params.InfraenvCreateParams.Name,
			UserName:               ocm.UserNameFromContext(ctx),
			OrgID:                  ocm.OrgIDFromContext(ctx),
			EmailDomain:            ocm.EmailDomainFromContext(ctx),
			OpenshiftVersion:       *osImage.OpenshiftVersion,
			IgnitionConfigOverride: params.InfraenvCreateParams.IgnitionConfigOverride,
			StaticNetworkConfig:    b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.InfraenvCreateParams.StaticNetworkConfig),
			Type:                   common.ImageTypePtr(params.InfraenvCreateParams.ImageType),
			AdditionalNtpSources:   swag.StringValue(params.InfraenvCreateParams.AdditionalNtpSources),
			SSHAuthorizedKey:       swag.StringValue(params.InfraenvCreateParams.SSHAuthorizedKey),
			CPUArchitecture:        params.InfraenvCreateParams.CPUArchitecture,
		},
		KubeKeyNamespace: kubeKey.Namespace,
		ImageTokenKey:    imageTokenKey,
	}

	if params.InfraenvCreateParams.ClusterID != nil {
		infraEnv.ClusterID = *params.InfraenvCreateParams.ClusterID
	}
	if params.InfraenvCreateParams.Proxy != nil {
		proxy := models.Proxy{
			HTTPProxy:  params.InfraenvCreateParams.Proxy.HTTPProxy,
			HTTPSProxy: params.InfraenvCreateParams.Proxy.HTTPSProxy,
			NoProxy:    params.InfraenvCreateParams.Proxy.NoProxy,
		}
		infraEnv.Proxy = &proxy
		var infraEnvProxyHash string
		infraEnvProxyHash, err = computeProxyHash(&proxy)
		if err != nil {
			msg := "Failed to compute infraEnv proxy hash"
			log.Error(msg, err)
			return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
		}
		infraEnv.ProxyHash = infraEnvProxyHash
	}

	pullSecret := swag.StringValue(params.InfraenvCreateParams.PullSecret)
	err = b.secretValidator.ValidatePullSecret(pullSecret, ocm.UserNameFromContext(ctx), b.authHandler)
	if err != nil {
		err = errors.Wrap(secretValidationToUserError(err), "pull secret for new infraEnv is invalid")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	ps, err := b.updatePullSecret(pullSecret, log)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest,
			errors.New("Failed to update Pull-secret with additional credentials"))
	}
	setInfraEnvPullSecret(&infraEnv, ps)

	if err = updateSSHAuthorizedKey(&infraEnv); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.db.Create(&infraEnv).Error; err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.GenerateInfraEnvISOInternal(ctx, &infraEnv); err != nil {
		return nil, err
	}

	success = true
	return b.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *infraEnv.ID})
}

func (b *bareMetalInventory) setDefaultRegisterInfraEnvParams(_ context.Context, params installer.RegisterInfraEnvParams) installer.RegisterInfraEnvParams {
	if params.InfraenvCreateParams.Proxy != nil &&
		params.InfraenvCreateParams.Proxy.HTTPProxy != nil &&
		(params.InfraenvCreateParams.Proxy.HTTPSProxy == nil || *params.InfraenvCreateParams.Proxy.HTTPSProxy == "") {
		params.InfraenvCreateParams.Proxy.HTTPSProxy = params.InfraenvCreateParams.Proxy.HTTPProxy
	}

	if params.InfraenvCreateParams.AdditionalNtpSources == nil {
		params.InfraenvCreateParams.AdditionalNtpSources = &b.Config.DefaultNTPSource
	}

	// set the default value for REST API case, in case it was not provided in the request
	if params.InfraenvCreateParams.ImageType == "" {
		params.InfraenvCreateParams.ImageType = models.ImageType(b.Config.ISOImageType)
	}

	if params.InfraenvCreateParams.CPUArchitecture == "" {
		// Specifying architecture in params is optional, fallback to default
		params.InfraenvCreateParams.CPUArchitecture = common.DefaultCPUArchitecture
	}

	return params
}

func (b *bareMetalInventory) getOsImageOrLatest(version *string, cpuArch string) (*models.OsImage, error) {
	var osImage *models.OsImage
	var err error
	if swag.StringValue(version) != "" {
		osImage, err = b.versionsHandler.GetOsImage(swag.StringValue(version), cpuArch)
		if err != nil {
			err = errors.Errorf("No OS image for Openshift version %s", swag.StringValue(version))
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	} else {
		osImage, err = b.versionsHandler.GetLatestOsImage(cpuArch)
		if err != nil {
			err = errors.Errorf("Failed to get latest OS image")
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}
	return osImage, nil
}

func (b *bareMetalInventory) validateClusterInfraEnvRegister(clusterId *strfmt.UUID, arch string) error {
	if clusterId != nil {
		cluster, err := common.GetClusterFromDB(b.db, *clusterId, common.SkipEagerLoading)
		if err != nil {
			err = errors.Errorf("Cluster ID %s does not exists", clusterId.String())
			return common.NewApiError(http.StatusBadRequest, err)
		}

		if cluster.CPUArchitecture != arch {
			err = errors.Errorf("Specified CPU architecture doesn't match the cluster (%s)",
				cluster.CPUArchitecture)
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) UpdateInfraEnv(ctx context.Context, params installer.UpdateInfraEnvParams) middleware.Responder {
	i, err := b.UpdateInfraEnvInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateInfraEnvCreated().WithPayload(&i.InfraEnv)
}

func (b *bareMetalInventory) UpdateInfraEnvInternal(ctx context.Context, params installer.UpdateInfraEnvParams) (*common.InfraEnv, error) {
	log := logutil.FromContext(ctx, b.log)
	var infraEnv *common.InfraEnv
	var err error
	var pullSecretBackup string
	pullSecretUpdated := false

	if params.InfraEnvUpdateParams.PullSecret != "" {
		pullSecretUpdated = true
		pullSecretBackup = params.InfraEnvUpdateParams.PullSecret
		params.InfraEnvUpdateParams.PullSecret = "pull secret was updated but will not be printed for security reasons."
	}
	log.Infof("update infraEnv %s with params: %+v", params.InfraEnvID, params.InfraEnvUpdateParams)

	if pullSecretUpdated {
		params.InfraEnvUpdateParams.PullSecret = pullSecretBackup
	}

	if params, err = b.validateAndUpdateInfraEnvParams(ctx, &params); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	success := false
	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully updated InfraEnv with id %s", params.InfraEnvID)
			log.Info(msg)
		} else {
			errWrapperLog := log
			if err != nil {
				errWrapperLog = log.WithError(err)
			}
			errWrapperLog.Errorf("Failed to update InfraEnv with id %s", params.InfraEnvID)
		}
	}()

	if infraEnv, err = common.GetInfraEnvFromDB(b.db, params.InfraEnvID); err != nil {
		log.WithError(err).Errorf("failed to get infraEnv: %s", params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	if params, err = b.validateAndUpdateInfraEnvProxyParams(ctx, &params, &infraEnv.OpenshiftVersion); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateInfraEnvIgnitionParams(ctx, params.InfraEnvUpdateParams.IgnitionConfigOverride); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if params.InfraEnvUpdateParams.StaticNetworkConfig != nil {
		if err = b.staticNetworkConfig.ValidateStaticConfigParams(ctx, params.InfraEnvUpdateParams.StaticNetworkConfig); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	err = b.updateInfraEnvData(ctx, infraEnv, params, b.db, log)
	if err != nil {
		log.WithError(err).Error("updateInfraEnvData")
		return nil, err
	}

	success = true

	if infraEnv, err = common.GetInfraEnvFromDB(b.db, params.InfraEnvID); err != nil {
		log.WithError(err).Errorf("failed to get infraEnv %s after update", params.InfraEnvID)
		return nil, err
	}

	if err = b.GenerateInfraEnvISOInternal(ctx, infraEnv); err != nil {
		return nil, err
	}

	return b.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *infraEnv.ID})
}

func (b *bareMetalInventory) updateInfraEnvData(ctx context.Context, infraEnv *common.InfraEnv, params installer.UpdateInfraEnvParams, db *gorm.DB, log logrus.FieldLogger) error {
	updates := map[string]interface{}{}
	if params.InfraEnvUpdateParams.Proxy != nil {
		proxyHash, err := computeProxyHash(params.InfraEnvUpdateParams.Proxy)
		if err != nil {
			return err
		}
		if proxyHash != infraEnv.ProxyHash {
			optionalParam(params.InfraEnvUpdateParams.Proxy.HTTPProxy, "proxy_http_proxy", updates)
			optionalParam(params.InfraEnvUpdateParams.Proxy.HTTPSProxy, "proxy_https_proxy", updates)
			optionalParam(params.InfraEnvUpdateParams.Proxy.NoProxy, "proxy_no_proxy", updates)
			updates["proxy_hash"] = proxyHash
		}
	}

	inputSSHKey := swag.StringValue(params.InfraEnvUpdateParams.SSHAuthorizedKey)
	if inputSSHKey != "" && inputSSHKey != infraEnv.SSHAuthorizedKey {
		updates["ssh_authorized_key"] = inputSSHKey
	}

	if err := b.updateInfraEnvNtpSources(params, infraEnv, updates, log); err != nil {
		return err
	}

	if params.InfraEnvUpdateParams.IgnitionConfigOverride != "" && params.InfraEnvUpdateParams.IgnitionConfigOverride != infraEnv.IgnitionConfigOverride {
		updates["ignition_config_override"] = params.InfraEnvUpdateParams.IgnitionConfigOverride
	}

	if params.InfraEnvUpdateParams.ImageType != "" && params.InfraEnvUpdateParams.ImageType != common.ImageTypeValue(infraEnv.Type) {
		updates["type"] = params.InfraEnvUpdateParams.ImageType
	}

	if params.InfraEnvUpdateParams.StaticNetworkConfig != nil {
		staticNetworkConfig := b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.InfraEnvUpdateParams.StaticNetworkConfig)
		if staticNetworkConfig != infraEnv.StaticNetworkConfig {
			updates["static_network_config"] = staticNetworkConfig
		}
	}

	if params.InfraEnvUpdateParams.PullSecret != "" && params.InfraEnvUpdateParams.PullSecret != infraEnv.PullSecret {
		infraEnv.PullSecret = params.InfraEnvUpdateParams.PullSecret
		updates["pull_secret"] = params.InfraEnvUpdateParams.PullSecret
		updates["pull_secret_set"] = true
	}

	if len(updates) > 0 {
		updates["generated"] = false
		dbReply := db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates)
		if dbReply.Error != nil {
			return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(dbReply.Error, "failed to update infraEnv: %s", params.InfraEnvID))
		}
	}

	return nil
}

func (b *bareMetalInventory) validateAndUpdateInfraEnvParams(ctx context.Context, params *installer.UpdateInfraEnvParams) (installer.UpdateInfraEnvParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if params.InfraEnvUpdateParams.PullSecret != "" {
		if err := b.secretValidator.ValidatePullSecret(params.InfraEnvUpdateParams.PullSecret, ocm.UserNameFromContext(ctx), b.authHandler); err != nil {
			log.WithError(err).Errorf("Pull secret for infraEnv %s is invalid", params.InfraEnvID)
			return installer.UpdateInfraEnvParams{}, err
		}
		ps, errUpdate := b.updatePullSecret(params.InfraEnvUpdateParams.PullSecret, log)
		if errUpdate != nil {
			return installer.UpdateInfraEnvParams{}, errors.New("Failed to update Pull-secret with additional credentials")
		}
		params.InfraEnvUpdateParams.PullSecret = ps
	}

	if sshPublicKey := swag.StringValue(params.InfraEnvUpdateParams.SSHAuthorizedKey); sshPublicKey != "" {
		sshPublicKey = strings.TrimSpace(sshPublicKey)
		if err := validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
			return installer.UpdateInfraEnvParams{}, err
		}
		*params.InfraEnvUpdateParams.SSHAuthorizedKey = sshPublicKey
	}

	return *params, nil
}

func (b *bareMetalInventory) validateAndUpdateInfraEnvProxyParams(ctx context.Context, params *installer.UpdateInfraEnvParams, ocpVersion *string) (installer.UpdateInfraEnvParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if params.InfraEnvUpdateParams.Proxy != nil {
		if params.InfraEnvUpdateParams.Proxy.HTTPProxy != nil &&
			(params.InfraEnvUpdateParams.Proxy.HTTPSProxy == nil || *params.InfraEnvUpdateParams.Proxy.HTTPSProxy == "") {
			params.InfraEnvUpdateParams.Proxy.HTTPSProxy = params.InfraEnvUpdateParams.Proxy.HTTPProxy
		}

		if err := validateProxySettings(params.InfraEnvUpdateParams.Proxy.HTTPProxy,
			params.InfraEnvUpdateParams.Proxy.HTTPSProxy,
			params.InfraEnvUpdateParams.Proxy.NoProxy, ocpVersion); err != nil {
			log.WithError(err).Errorf("Failed to validate Proxy settings")
			return installer.UpdateInfraEnvParams{}, err
		}
	}

	return *params, nil
}

func (b *bareMetalInventory) validateInfraEnvIgnitionParams(ctx context.Context, ignitionConfigOverride string) error {

	log := logutil.FromContext(ctx, b.log)

	if ignitionConfigOverride != "" {
		_, err := ignition.ParseToLatest([]byte(ignitionConfigOverride))
		if err != nil {
			log.WithError(err).Errorf("Failed to parse ignition config patch %s", ignitionConfigOverride)
			return err
		}
	}

	return nil
}

func (b *bareMetalInventory) updateInfraEnvNtpSources(params installer.UpdateInfraEnvParams, infraEnv *common.InfraEnv, updates map[string]interface{}, log logrus.FieldLogger) error {
	if params.InfraEnvUpdateParams.AdditionalNtpSources != nil {
		ntpSource := swag.StringValue(params.InfraEnvUpdateParams.AdditionalNtpSources)
		additionalNtpSourcesDefined := ntpSource != ""

		if additionalNtpSourcesDefined && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err := errors.Errorf("Invalid NTP source: %s", ntpSource)
			log.WithError(err)
			return common.NewApiError(http.StatusBadRequest, err)
		}
		if ntpSource != infraEnv.AdditionalNtpSources {
			updates["additional_ntp_sources"] = ntpSource
		}
	}
	return nil
}

func (b *bareMetalInventory) GetInfraEnvByKubeKey(key types.NamespacedName) (*common.InfraEnv, error) {
	infraEnv, err := common.GetInfraEnvFromDBWhere(b.db, "name = ? and kube_key_namespace = ?", key.Name, key.Namespace)
	if err != nil {
		return nil, err
	}
	return infraEnv, nil
}

func (b *bareMetalInventory) V2RegisterHost(ctx context.Context, params installer.V2RegisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Register host: %+v", params)

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("RegisterHost failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("RegisterHost failed")
			tx.Rollback()
		}
	}()

	infraEnv, err := common.GetInfraEnvFromDB(transaction.AddForUpdateQueryOption(tx), params.InfraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env: %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}

	dbHost, err := common.GetHostFromDB(transaction.AddForUpdateQueryOption(tx), params.InfraEnvID.String(), params.NewHostParams.HostID.String())
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.WithError(err).Errorf("failed to get host %s in infra-env: %s",
			*params.NewHostParams.HostID, params.InfraEnvID.String())
		return installer.NewV2RegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// In case host doesn't exists check if the cluster accept new hosts registration
	newRecord := err != nil && errors.Is(err, gorm.ErrRecordNotFound)

	url := installer.V2GetHostURL{InfraEnvID: params.InfraEnvID, HostID: *params.NewHostParams.HostID}
	kind := swag.String(models.HostKindHost)

	// We immediately set the role to master in single node clusters to have more strict (master) validations.
	// Typically, the validations are "weak" because an auto-assign host has the potential to only be a worker,
	// which has less strict hardware requirements. This early role assignment results in clearer, more early
	// errors for the user in case of insufficient hardware. In the future, single-node clusters might support
	// extra nodes (as workers). In that case, this line might need to be removed.
	defaultRole := models.HostRoleAutoAssign

	host := &models.Host{
		ID:                    params.NewHostParams.HostID,
		Href:                  swag.String(url.String()),
		Kind:                  kind,
		CheckedInAt:           strfmt.DateTime(time.Now()),
		DiscoveryAgentVersion: params.NewHostParams.DiscoveryAgentVersion,
		UserName:              ocm.UserNameFromContext(ctx),
		Role:                  defaultRole,
		InfraEnvID:            *infraEnv.ID,
	}

	var cluster *common.Cluster
	var c *models.Cluster
	cluster, err = b.getBoundCluster(transaction.AddForUpdateQueryOption(tx), infraEnv, dbHost)
	if err != nil {
		log.WithError(err).Errorf("Bound Cluster get")
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if cluster != nil {
		if newRecord {
			if err = b.clusterApi.AcceptRegistration(cluster); err != nil {
				log.WithError(err).Errorf("failed to register host <%s> to infra-env %s due to: %s",
					params.NewHostParams.HostID, params.InfraEnvID.String(), err.Error())
				eventgen.SendRegisterHostToInfraEnvFailedEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID, params.InfraEnvID, cluster.ID, err.Error())

				return common.NewApiError(http.StatusConflict, err)
			}
		}
		if common.IsSingleNodeCluster(cluster) {
			host.Role = models.HostRoleMaster
			host.Bootstrap = true
		}
		if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
			host.Kind = swag.String(models.HostKindAddToExistingClusterHost)
		}
		host.ClusterID = cluster.ID
		c = &cluster.Cluster
	}

	if err = b.hostApi.RegisterHost(ctx, host, tx); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> infra-env <%s>",
			params.NewHostParams.HostID.String(), params.InfraEnvID.String())
		uerr := errors.Wrap(err, fmt.Sprintf("Failed to register host %s", hostutil.GetHostnameForMsg(host)))

		eventgen.SendHostRegistrationFailedEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID, params.InfraEnvID, host.ClusterID, uerr.Error())
		return returnRegisterHostTransitionError(http.StatusBadRequest, err)
	}

	if err = b.customizeHost(c, host); err != nil {
		eventgen.SendHostRegistrationSettingPropertiesFailedEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID, params.InfraEnvID, host.ClusterID)
		return common.GenerateErrorResponder(err)
	}

	eventgen.SendHostRegistrationSucceededEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID,
		params.InfraEnvID, host.ClusterID, hostutil.GetHostnameForMsg(host))

	hostRegistration := models.HostRegistrationResponse{
		Host:                  *host,
		NextStepRunnerCommand: b.generateV2NextStepRunnerCommand(ctx, &params),
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewV2RegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// Create AgentCR if needed, after commit in DB as KubeKey will be updated.
	if err := b.crdUtils.CreateAgentCR(ctx, log, params.NewHostParams.HostID.String(), infraEnv, cluster); err != nil {
		log.WithError(err).Errorf("Fail to create Agent CR, deleting host. Namespace: %s, InfraEnv: %s, HostID: %s", infraEnv.KubeKeyNamespace, swag.StringValue(infraEnv.Name), params.NewHostParams.HostID.String())
		if err2 := b.hostApi.UnRegisterHost(ctx, params.NewHostParams.HostID.String(), params.InfraEnvID.String()); err2 != nil {
			return installer.NewV2RegisterHostInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
		return installer.NewV2RegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	txSuccess = true

	return installer.NewV2RegisterHostCreated().WithPayload(&hostRegistration)
}

func (b *bareMetalInventory) V2GetHostIgnition(ctx context.Context, params installer.V2GetHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	_, respBody, _, err := b.v2DownloadHostIgnition(ctx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to download host %s ignition", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	respBytes, err := ioutil.ReadAll(respBody)
	if err != nil {
		log.WithError(err).Errorf("failed to read ignition content for host %s", params.HostID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return installer.NewV2GetHostIgnitionOK().WithPayload(&models.HostIgnitionParams{Config: string(respBytes)})
}

func (b *bareMetalInventory) V2GetNextSteps(ctx context.Context, params installer.V2GetNextStepsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var steps models.Steps

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("get next steps failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("get next steps failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	//TODO check the error type
	host, err := common.GetHostFromDB(tx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host: %s", params.HostID)
		return installer.NewV2GetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	host.CheckedInAt = strfmt.DateTime(time.Now())
	if err = tx.Model(&host).UpdateColumn("checked_in_at", host.CheckedInAt).Error; err != nil {
		log.WithError(err).Errorf("failed to update host: %s", params.HostID.String())
		return installer.NewV2GetNextStepsInternalServerError()
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewV2GetNextStepsInternalServerError()
	}
	txSuccess = true

	steps, err = b.hostApi.GetNextSteps(ctx, &host.Host)
	if err != nil {
		log.WithError(err).Errorf("failed to get steps for host %s infra-env %s", params.HostID.String(), params.InfraEnvID.String())
	}

	return installer.NewV2GetNextStepsOK().WithPayload(&steps)
}

func (b *bareMetalInventory) V2PostStepReply(ctx context.Context, params installer.V2PostStepReplyParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())

	if err != nil {
		log.WithError(err).Errorf("Failed to find host <%s> infra-env <%s> step <%s> exit code %d stdout <%s> stderr <%s>",
			params.HostID, params.InfraEnvID, params.Reply.StepID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)
		return installer.NewV2PostStepReplyNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	logReplyReceived(params, log, host)

	if params.Reply.ExitCode != 0 {
		handlingError := b.handleReplyError(params, ctx, log, &host.Host, params.Reply.ExitCode)
		if handlingError != nil {
			log.WithError(handlingError).Errorf("Failed handling reply error for host <%s> infra-env <%s>", params.HostID, params.InfraEnvID)
		}
		return installer.NewV2PostStepReplyNoContent()
	}

	if !shouldHandle(params) {
		return installer.NewV2PostStepReplyNoContent()
	}

	stepReply, err := filterReplyByType(params)
	if err != nil {
		log.WithError(err).Errorf("Failed decode <%s> reply for host <%s> infra-env <%s>",
			params.Reply.StepID, params.HostID, params.InfraEnvID)
		return installer.NewV2PostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = handleReplyByType(params, b, ctx, host.Host, stepReply)
	if err != nil {
		log.WithError(err).Errorf("Failed to update step reply for host <%s> infra-env <%s> step <%s>",
			params.HostID, params.InfraEnvID, params.Reply.StepID)
		return installer.NewV2PostStepReplyInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewV2PostStepReplyNoContent()
}

func (b *bareMetalInventory) V2GetHost(ctx context.Context, params installer.V2GetHostParams) middleware.Responder {
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return installer.NewV2GetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return common.GenerateErrorResponder(err)
		}
	}

	var c *models.Cluster
	if host.ClusterID != nil {
		cluster, err := common.GetClusterFromDB(b.db, *host.ClusterID, common.SkipEagerLoading)
		if err != nil {
			err = fmt.Errorf("can not find a cluster for host %s", params.HostID.String())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		c = &cluster.Cluster
	}

	if err := b.customizeHost(c, &host.Host); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// Clear this field as it is not needed to be sent via API
	host.FreeAddresses = ""
	return installer.NewV2GetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) V2UpdateHostInstallProgress(ctx context.Context, params installer.V2UpdateHostInstallProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Update host %s install progress", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host %s", params.HostID)
		return installer.NewV2UpdateHostInstallProgressNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if host.ClusterID == nil {
		err = fmt.Errorf("host %s is not bound to any custer, cannot update progress", params.HostID)
		log.WithError(err).Error()
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	// Adding a transaction will require to update all lower layer to work with tx instead of db.
	if params.HostProgress.CurrentStage != host.Progress.CurrentStage || params.HostProgress.ProgressInfo != host.Progress.ProgressInfo {
		if err := b.hostApi.UpdateInstallProgress(ctx, &host.Host, params.HostProgress); err != nil {
			log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
			return installer.NewUpdateHostInstallProgressInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}

		event := fmt.Sprintf("reached installation stage %s", params.HostProgress.CurrentStage)
		if params.HostProgress.ProgressInfo != "" {
			event += fmt.Sprintf(": %s", params.HostProgress.ProgressInfo)
		}

		log.Info(fmt.Sprintf("Host %s in cluster %s: %s", host.ID, host.ClusterID, event))
		eventgen.SendHostInstallProgressUpdatedEvent(ctx, b.eventsHandler, *host.ID, host.InfraEnvID, host.ClusterID, hostutil.GetHostnameForMsg(&host.Host), event)
		if err := b.clusterApi.UpdateInstallProgress(ctx, *host.ClusterID); err != nil {
			log.WithError(err).Errorf("failed to update cluster %s progress", host.ClusterID)
			return installer.NewUpdateHostInstallProgressInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	return installer.NewV2UpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) BindHost(ctx context.Context, params installer.BindHostParams) middleware.Responder {
	h, err := b.BindHostInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewBindHostOK().WithPayload(&h.Host)
}

func (b *bareMetalInventory) BindHostInternal(ctx context.Context, params installer.BindHostParams) (*common.Host, error) {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Binding host %s to cluster %s", params.HostID, params.BindHostParams.ClusterID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host <%s> in infraEnv <%s>",
			params.HostID, params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	if host.ClusterID != nil {
		return nil, common.NewApiError(http.StatusConflict, errors.Errorf("Host %s is already bound to cluster %s", params.HostID, *host.ClusterID))
	}
	cluster, err := common.GetClusterFromDB(b.db, *params.BindHostParams.ClusterID, common.SkipEagerLoading)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, errors.Errorf("Failed to find cluster %s", params.BindHostParams.ClusterID))
	}
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infra env %s", params.InfraEnvID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if cluster.CPUArchitecture != infraEnv.CPUArchitecture {
		err = errors.Errorf("InfraEnv's CPU architecture (%s) doesn't match the cluster (%s)",
			infraEnv.CPUArchitecture, cluster.CPUArchitecture)
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.clusterApi.AcceptRegistration(cluster); err != nil {
		log.WithError(err).Errorf("failed to bind host <%s> to cluster %s due to: %s",
			params.HostID.String(), *params.BindHostParams.ClusterID, err.Error())
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	if err = b.hostApi.BindHost(ctx, &host.Host, *params.BindHostParams.ClusterID, b.db); err != nil {
		log.WithError(err).Errorf("Failed to bind host <%s> to cluster <%s>",
			params.HostID, *params.BindHostParams.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	host, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return host, nil
}

func (b *bareMetalInventory) UnbindHostInternal(ctx context.Context, params installer.UnbindHostParams) (*common.Host, error) {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Unbinding host %s", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host <%s> in infraEnv <%s>",
			params.HostID, params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	if host.ClusterID == nil {
		return nil, common.NewApiError(http.StatusConflict, errors.Errorf("Host %s is already unbound", params.HostID))
	}

	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infra env %s", params.InfraEnvID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	if infraEnv.ClusterID != "" {
		return nil, common.NewApiError(http.StatusConflict, errors.Errorf("Cannot unbind Host %s. InfraEnv %s is bound to Cluster %s", params.HostID, params.InfraEnvID, infraEnv.ClusterID))
	}

	if err = b.hostApi.UnbindHost(ctx, &host.Host, b.db); err != nil {
		log.WithError(err).Errorf("Failed to unbind host <%s>", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	host, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	return host, nil
}

func (b *bareMetalInventory) UnbindHost(ctx context.Context, params installer.UnbindHostParams) middleware.Responder {
	h, err := b.UnbindHostInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUnbindHostOK().WithPayload(&h.Host)
}

func (b *bareMetalInventory) V2ListHosts(ctx context.Context, params installer.V2ListHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	hosts, err := common.GetInfraEnvHostsFromDB(b.db, params.InfraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get list of hosts for infra-env %s", params.InfraEnvID)
		return installer.NewV2ListHostsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, h := range hosts {
		if err := b.customizeHost(nil, &h.Host); err != nil {
			return common.GenerateErrorResponder(err)
		}
		// Clear this field as it is not needed to be sent via API
		h.FreeAddresses = ""
	}

	return installer.NewV2ListHostsOK().WithPayload(common.ToModelsHosts(hosts))
}

func (b *bareMetalInventory) V2DeregisterHost(ctx context.Context, params installer.V2DeregisterHostParams) middleware.Responder {
	if err := b.V2DeregisterHostInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2DeregisterHostNoContent()
}

func (b *bareMetalInventory) V2UpdateHostInstallerArgs(ctx context.Context, params installer.V2UpdateHostInstallerArgsParams) middleware.Responder {
	updatedHost, err := b.V2UpdateHostInstallerArgsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostInstallerArgsCreated().WithPayload(updatedHost)
}

func (b *bareMetalInventory) V2UpdateHostInstallerArgsInternal(ctx context.Context, params installer.V2UpdateHostInstallerArgsParams) (*models.Host, error) {

	log := logutil.FromContext(ctx, b.log)

	err := hostutil.ValidateInstallerArgs(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	h, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	argsBytes, err := json.Marshal(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, err
	}

	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and infra_env_id = ?"), params.HostID, params.InfraEnvID).Update("installer_args", string(argsBytes)).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update host %s", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendHostInstallerArgsAppliedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, h.ClusterID,
		hostutil.GetHostnameForMsg(&h.Host))
	log.Infof("Custom installer arguments were applied to host %s in infra env %s", params.HostID, params.InfraEnvID)

	h, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return &h.Host, nil
}

func (b *bareMetalInventory) V2UpdateHostIgnition(ctx context.Context, params installer.V2UpdateHostIgnitionParams) middleware.Responder {
	_, err := b.V2UpdateHostIgnitionInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostIgnitionCreated()
}

func (b *bareMetalInventory) V2UpdateHostIgnitionInternal(ctx context.Context, params installer.V2UpdateHostIgnitionParams) (*models.Host, error) {
	log := logutil.FromContext(ctx, b.log)

	h, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	if params.HostIgnitionParams.Config != "" {
		_, err = ignition.ParseToLatest([]byte(params.HostIgnitionParams.Config))
		if err != nil {
			log.WithError(err).Errorf("Failed to parse host ignition config patch %s", params.HostIgnitionParams)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and infra_env_id = ?"), params.HostID, params.InfraEnvID).Update("ignition_config_overrides", params.HostIgnitionParams.Config).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendHostDiscoveryIgnitionConfigAppliedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID,
		hostutil.GetHostnameForMsg(&h.Host))
	log.Infof("Custom discovery ignition config was applied to host %s in infra-env %s", params.HostID, params.InfraEnvID)
	h, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	return &h.Host, nil
}

func (b *bareMetalInventory) V2DownloadInfraEnvFiles(ctx context.Context, params installer.V2DownloadInfraEnvFilesParams) middleware.Responder {
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infra env %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}
	if params.FileName == "discovery.ign" {
		cfg, err2 := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, false, b.authHandler.AuthType())
		if err2 != nil {
			b.log.WithError(err).Error("Failed to format ignition config")
			return common.GenerateErrorResponder(err)
		}
		return filemiddleware.NewResponder(installer.NewV2DownloadInfraEnvFilesOK().WithPayload(ioutil.NopCloser(strings.NewReader(cfg))), params.FileName, int64(len(cfg)))
	} else {
		return common.GenerateErrorResponder(common.NewApiError(http.StatusBadRequest, err))
	}
}

func (b *bareMetalInventory) V2DownloadClusterCredentials(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) middleware.Responder {
	respBody, contentLength, err := b.V2DownloadClusterCredentialsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return filemiddleware.NewResponder(installer.NewV2DownloadClusterCredentialsOK().WithPayload(respBody), params.FileName, contentLength)
}

func (b *bareMetalInventory) V2DownloadClusterFiles(ctx context.Context, params installer.V2DownloadClusterFilesParams) middleware.Responder {
	respBody, contentLength, err := b.V2DownloadClusterFilesInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return filemiddleware.NewResponder(installer.NewV2DownloadClusterFilesOK().WithPayload(respBody), params.FileName, contentLength)
}

func (b *bareMetalInventory) V2DownloadClusterFilesInternal(ctx context.Context, params installer.V2DownloadClusterFilesParams) (io.ReadCloser, int64, error) {
	return b.v2DownloadClusterFilesInternal(ctx, params.FileName, params.ClusterID.String())
}

func (b *bareMetalInventory) V2DownloadClusterCredentialsInternal(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) (io.ReadCloser, int64, error) {
	return b.v2DownloadClusterFilesInternal(ctx, params.FileName, params.ClusterID.String())
}

func (b *bareMetalInventory) v2DownloadClusterFilesInternal(ctx context.Context, fileName, clusterId string) (io.ReadCloser, int64, error) {
	log := logutil.FromContext(ctx, b.log)
	if err := b.checkFileForDownload(ctx, clusterId, fileName); err != nil {
		return nil, 0, err
	}

	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterId, fileName))
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", fileName, clusterId)
		return nil, 0, common.NewApiError(http.StatusConflict, err)
	}

	return respBody, contentLength, nil
}

func (b *bareMetalInventory) V2UpdateHostLogsProgress(ctx context.Context, params installer.V2UpdateHostLogsProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("update log progress on host %s infra-env %s to %s", params.HostID, params.InfraEnvID, common.LogStateValue(params.LogsProgressParams.LogsState))
	currentHost, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err == nil {
		err = b.hostApi.UpdateLogsProgress(ctx, &currentHost.Host, string(common.LogStateValue(params.LogsProgressParams.LogsState)))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on infra-env %s host %s", common.LogStateValue(params.LogsProgressParams.LogsState), params.InfraEnvID.String(), params.HostID.String())
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostLogsProgressNoContent()
}

func (b *bareMetalInventory) V2UpdateHostInternal(ctx context.Context, params installer.V2UpdateHostParams) (*common.Host, error) {
	log := logutil.FromContext(ctx, b.log)

	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host <%s>, infra env <%s>", params.HostID, params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	txSuccess := false
	tx := b.db.Begin()

	defer func() {
		if !txSuccess {
			log.Error("update host failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update host failed")
			tx.Rollback()
		}
	}()

	err = b.updateHostRole(ctx, host, params.HostUpdateParams.HostRole, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostName(ctx, host, params.HostUpdateParams.HostName, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostDisksSelectionConfig(ctx, host, params.HostUpdateParams.DisksSelectedConfig, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostMachineConfigPoolName(ctx, host, params.HostUpdateParams.MachineConfigPoolName, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostIgnitionEndpointToken(ctx, host, params.HostUpdateParams.IgnitionEndpointToken, tx)
	if err != nil {
		return nil, err
	}

	err = b.refreshAfterUpdate(ctx, host, tx)
	if err != nil {
		log.WithError(err).Errorf("Failed to refresh host %s, infra env %s during update", host.ID, host.InfraEnvID)
		return nil, err
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	txSuccess = true

	// get host after update
	host, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host <%s>, infra env <%s> after update", params.HostID, params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	//get bound cluster
	var c *models.Cluster
	var cluster *common.Cluster
	if host.ClusterID != nil {
		cluster, err = common.GetClusterFromDB(b.db, *host.ClusterID, common.SkipEagerLoading)
		if err != nil {
			err = fmt.Errorf("can not find a cluster for host %s", params.HostID.String())
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
		c = &cluster.Cluster
	}

	err = b.customizeHost(c, &host.Host)
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return host, nil
}

func (b *bareMetalInventory) updateHostRole(ctx context.Context, host *common.Host, hostRole *string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if hostRole == nil {
		log.Infof("No request for role update for host %s", host.ID)
		return nil
	}
	err := b.hostApi.UpdateRole(ctx, &host.Host, models.HostRole(*hostRole), db)
	if err != nil {
		log.WithError(err).Errorf("failed to set role <%s> host <%s>, infra env <%s>",
			*hostRole, host.ID, host.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (b *bareMetalInventory) updateHostName(ctx context.Context, host *common.Host, hostname *string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if hostname == nil {
		log.Infof("No request for hostname update for host %s", host.ID)
		return nil
	}
	err := hostutil.ValidateHostname(*hostname)
	if err != nil {
		log.WithError(err).Errorf("invalid hostname format: %s", err)
		return err
	}
	err = b.hostApi.UpdateHostname(ctx, &host.Host, *hostname, db)
	if err != nil {
		log.WithError(err).Errorf("failed to set hostname <%s> host <%s> infra-env <%s>",
			*hostname, host.ID, host.InfraEnvID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) updateHostDisksSelectionConfig(ctx context.Context, host *common.Host, disksSelectedConfig []*models.DiskConfigParams, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if disksSelectedConfig == nil {
		log.Infof("No request for disk selection config update for host %s", host.ID)
		return nil
	}
	disksToInstallOn := funk.Filter(disksSelectedConfig, func(diskConfigParams *models.DiskConfigParams) bool {
		return models.DiskRoleInstall == diskConfigParams.Role
	}).([]*models.DiskConfigParams)

	installationDiskId := ""

	if len(disksToInstallOn) > 1 {
		return common.NewApiError(http.StatusConflict, errors.New("duplicate setting of installation path by the user"))
	} else if len(disksToInstallOn) == 1 {
		installationDiskId = *disksToInstallOn[0].ID
	}

	log.Infof("Update host %s to install from disk id %s", host.ID, installationDiskId)
	err := b.hostApi.UpdateInstallationDisk(ctx, db, &host.Host, installationDiskId)
	if err != nil {
		log.WithError(err).Errorf("failed to set installation disk path <%s> host <%s> ubfra env <%s>",
			installationDiskId,
			host.ID,
			host.InfraEnvID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) updateHostMachineConfigPoolName(ctx context.Context, host *common.Host, machineConfigPoolName *string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if machineConfigPoolName == nil {
		log.Infof("No request for machine config pool name update for host %s", host.ID)
		return nil
	}
	err := b.hostApi.UpdateMachineConfigPoolName(ctx, db, &host.Host, *machineConfigPoolName)
	if err != nil {
		log.WithError(err).Errorf("Failed to set machine config pool name <%s> host <%s> infra env <%s>",
			*machineConfigPoolName, host.ID,
			host.InfraEnvID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) updateHostIgnitionEndpointToken(ctx context.Context, host *common.Host, token *string, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if token == nil {
		log.Infof("No request for ignition endpoint token update for host %s", host.ID)
		return nil
	}
	err := b.hostApi.UpdateIgnitionEndpointToken(ctx, db, &host.Host, *token)
	if err != nil {
		log.WithError(err).Errorf("Failed to set ignition endpoint token host <%s> infra env <%s>",
			host.ID,
			host.InfraEnvID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) refreshAfterUpdate(ctx context.Context, host *common.Host, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if host.ClusterID != nil {
		if host.Inventory != "" {
			err := b.hostApi.RefreshInventory(ctx, nil, &host.Host, db)
			if err != nil {
				log.WithError(err).Errorf("failed to update inventory of host %s cluster %s", host.ID, host.ClusterID)
				return err
			}
		}
	}
	err := b.hostApi.RefreshStatus(ctx, &host.Host, db)
	if err != nil {
		log.WithError(err).Errorf("Failed to refresh host %s, infra env %s during update", host.ID, host.InfraEnvID)
	}
	return err
}

func (b *bareMetalInventory) getBoundCluster(db *gorm.DB, infraEnv *common.InfraEnv, host *common.Host) (*common.Cluster, error) {
	var clusterID strfmt.UUID
	if infraEnv.ClusterID != "" {
		clusterID = infraEnv.ClusterID
	} else if host != nil && host.Host.ClusterID != nil {
		clusterID = *host.Host.ClusterID
	}

	if clusterID != "" {
		cluster, err := common.GetClusterFromDB(db, clusterID, common.SkipEagerLoading)
		if err != nil {
			return nil, err
		}
		return cluster, nil
	}
	return nil, nil
}
