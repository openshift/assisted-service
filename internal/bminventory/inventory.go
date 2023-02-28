package bminventory

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
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
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/infraenv"
	installcfgdata "github.com/openshift/assisted-service/internal/installcfg"
	installcfg "github.com/openshift/assisted-service/internal/installcfg/builder"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/lvm"
	"github.com/openshift/assisted-service/internal/provider"
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
	"github.com/openshift/assisted-service/pkg/stream"
	"github.com/openshift/assisted-service/pkg/transaction"
	pkgvalidations "github.com/openshift/assisted-service/pkg/validations"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/clientcmd"
)

const DefaultUser = "kubeadmin"

const WindowBetweenRequestsInSeconds = 10 * time.Second

const (
	MediaDisconnected int64 = 256
	// 125 is the generic exit code for cases the error is in podman / docker and not the container we tried to run
	ContainerAlreadyRunningExitCode = 125

	// The following constants are controlling the script type served for iPXE

	// Always boot the discovery ISO when using iPXE
	DiscoveryImageAlways = "discovery-image-always"

	// Apply boot order control when using iPXE
	BootOrderControl = "boot-order-control"
)

type Config struct {
	ignition.IgnitionConfig
	AgentDockerImg                      string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	ServiceBaseURL                      string            `envconfig:"SERVICE_BASE_URL"`
	ImageServiceBaseURL                 string            `envconfig:"IMAGE_SERVICE_BASE_URL"`
	ServiceCACertPath                   string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	S3EndpointURL                       string            `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket                            string            `envconfig:"S3_BUCKET" default:"test"`
	ImageExpirationTime                 time.Duration     `envconfig:"IMAGE_EXPIRATION_TIME" default:"4h"`
	AwsAccessKeyID                      string            `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey                  string            `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	BaseDNSDomains                      map[string]string `envconfig:"BASE_DNS_DOMAINS" default:""`
	SkipCertVerification                bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	InstallRHCa                         bool              `envconfig:"INSTALL_RH_CA" default:"false"`
	RhQaRegCred                         string            `envconfig:"REGISTRY_CREDS" default:""`
	AgentTimeoutStart                   time.Duration     `envconfig:"AGENT_TIMEOUT_START" default:"3m"`
	ServiceIPs                          string            `envconfig:"SERVICE_IPS" default:""`
	DefaultNTPSource                    string            `envconfig:"NTP_DEFAULT_SERVER"`
	ISOCacheDir                         string            `envconfig:"ISO_CACHE_DIR" default:"/tmp/isocache"`
	DefaultClusterNetworkCidr           string            `envconfig:"CLUSTER_NETWORK_CIDR" default:"10.128.0.0/14"`
	DefaultClusterNetworkHostPrefix     int64             `envconfig:"CLUSTER_NETWORK_HOST_PREFIX" default:"23"`
	DefaultClusterNetworkCidrIPv6       string            `envconfig:"CLUSTER_NETWORK_CIDR_IPV6" default:"fd01::/48"`
	DefaultClusterNetworkHostPrefixIPv6 int64             `envconfig:"CLUSTER_NETWORK_HOST_PREFIX_IPV6" default:"64"`
	DefaultServiceNetworkCidr           string            `envconfig:"SERVICE_NETWORK_CIDR" default:"172.30.0.0/16"`
	DefaultServiceNetworkCidrIPv6       string            `envconfig:"SERVICE_NETWORK_CIDR_IPV6" default:"fd02::/112"`
	ISOImageType                        string            `envconfig:"ISO_IMAGE_TYPE" default:"full-iso"`
	IPv6Support                         bool              `envconfig:"IPV6_SUPPORT" default:"true"`
	DiskEncryptionSupport               bool              `envconfig:"DISK_ENCRYPTION_SUPPORT" default:"true"`

	// InfraEnv ID for the ephemeral installer. Should not be set explicitly.Ephemeral (agent) installer sets this env var
	InfraEnvID strfmt.UUID `envconfig:"INFRA_ENV_ID" default:""`
}

const minimalOpenShiftVersionForSingleNode = "4.8.0-0.0"
const minimalOpenShiftVersionForDefaultNetworkTypeOVNKubernetes = "4.12.0-0.0"
const minimalOpenShiftVersionForConsoleCapability = "4.12.0-0.0"
const minimalOpenShiftVersionForNutanix = "4.11.0-0.0"

type Interactivity bool

const (
	Interactive    Interactivity = true
	NonInteractive Interactivity = false
)

type OCPClusterAPI interface {
	RegisterOCPCluster(ctx context.Context) error
}

//go:generate mockgen --build_flags=--mod=mod -package bminventory -destination mock_installer_internal.go . InstallerInternals
type InstallerInternals interface {
	RegisterClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.V2RegisterClusterParams) (*common.Cluster, error)
	GetClusterInternal(ctx context.Context, params installer.V2GetClusterParams) (*common.Cluster, error)
	UpdateClusterNonInteractive(ctx context.Context, params installer.V2UpdateClusterParams) (*common.Cluster, error)
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	GetHostByKubeKey(key types.NamespacedName) (*common.Host, error)
	InstallClusterInternal(ctx context.Context, params installer.V2InstallClusterParams) (*common.Cluster, error)
	DeregisterClusterInternal(ctx context.Context, cluster *common.Cluster) error
	V2DeregisterHostInternal(ctx context.Context, params installer.V2DeregisterHostParams, interactivity Interactivity) error
	GetCommonHostInternal(ctx context.Context, infraEnvId string, hostId string) (*common.Host, error)
	UpdateHostApprovedInternal(ctx context.Context, infraEnvId string, hostId string, approved bool) error
	V2UpdateHostInstallerArgsInternal(ctx context.Context, params installer.V2UpdateHostInstallerArgsParams) (*models.Host, error)
	V2UpdateHostIgnitionInternal(ctx context.Context, params installer.V2UpdateHostIgnitionParams) (*models.Host, error)
	GetCredentialsInternal(ctx context.Context, params installer.V2GetCredentialsParams) (*models.Credentials, error)
	V2DownloadClusterFilesInternal(ctx context.Context, params installer.V2DownloadClusterFilesParams) (io.ReadCloser, int64, error)
	V2DownloadClusterCredentialsInternal(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) (io.ReadCloser, int64, error)
	V2ImportClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, id *strfmt.UUID, params installer.V2ImportClusterParams) (*common.Cluster, error)
	InstallSingleDay2HostInternal(ctx context.Context, clusterId strfmt.UUID, infraEnvId strfmt.UUID, hostId strfmt.UUID) error
	UpdateClusterInstallConfigInternal(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) (*common.Cluster, error)
	CancelInstallationInternal(ctx context.Context, params installer.V2CancelInstallationParams) (*common.Cluster, error)
	TransformClusterToDay2Internal(ctx context.Context, clusterID strfmt.UUID) (*common.Cluster, error)
	GetClusterSupportedPlatformsInternal(ctx context.Context, params installer.GetClusterSupportedPlatformsParams) (*[]models.PlatformType, error)
	V2UpdateHostInternal(ctx context.Context, params installer.V2UpdateHostParams, interactivity Interactivity) (*common.Host, error)
	GetInfraEnvByKubeKey(key types.NamespacedName) (*common.InfraEnv, error)
	UpdateInfraEnvInternal(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) (*common.InfraEnv, error)
	RegisterInfraEnvInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterInfraEnvParams) (*common.InfraEnv, error)
	DeregisterInfraEnvInternal(ctx context.Context, params installer.DeregisterInfraEnvParams) error
	UnbindHostInternal(ctx context.Context, params installer.UnbindHostParams, reclaimHost bool, interactivity Interactivity) (*common.Host, error)
	BindHostInternal(ctx context.Context, params installer.BindHostParams) (*common.Host, error)
	GetInfraEnvHostsInternal(ctx context.Context, infraEnvId strfmt.UUID) ([]*common.Host, error)
	GetKnownHostApprovedCounts(clusterID strfmt.UUID) (registered, approved int, err error)
	HostWithCollectedLogsExists(clusterId strfmt.UUID) (bool, error)
	GetKnownApprovedHosts(clusterId strfmt.UUID) ([]*common.Host, error)
	ValidatePullSecret(secret string, username string) error
	GetInfraEnvInternal(ctx context.Context, params installer.GetInfraEnvParams) (*common.InfraEnv, error)
	V2UpdateHostInstallProgressInternal(ctx context.Context, params installer.V2UpdateHostInstallProgressParams) error
}

//go:generate mockgen --build_flags=--mod=mod -package bminventory -destination mock_crd_utils.go . CRDUtils
type CRDUtils interface {
	CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId string, infraenv *common.InfraEnv, cluster *common.Cluster) error
}
type bareMetalInventory struct {
	Config
	db                   *gorm.DB
	stream               stream.EventStreamWriter
	log                  logrus.FieldLogger
	hostApi              host.API
	clusterApi           clusterPkg.API
	infraEnvApi          infraenv.API
	dnsApi               dns.DNSApi
	eventsHandler        eventsapi.Handler
	objectHandler        s3wrapper.API
	metricApi            metrics.API
	usageApi             usage.API
	operatorManagerApi   operators.API
	generator            generator.ISOInstallConfigGenerator
	authHandler          auth.Authenticator
	authzHandler         auth.Authorizer
	k8sClient            k8sclient.K8SClient
	ocmClient            *ocm.Client
	leaderElector        leader.Leader
	secretValidator      validations.PullSecretValidator
	versionsHandler      versions.Handler
	osImages             versions.OSImages
	crdUtils             CRDUtils
	IgnitionBuilder      ignition.IgnitionBuilder
	hwValidator          hardware.Validator
	installConfigBuilder installcfg.InstallConfigBuilder
	staticNetworkConfig  staticnetworkconfig.StaticNetworkConfig
	gcConfig             garbagecollector.Config
	providerRegistry     registry.ProviderRegistry
	insecureIPXEURLs     bool
}

func NewBareMetalInventory(
	db *gorm.DB,
	stream stream.EventStreamWriter,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi clusterPkg.API,
	infraEnvApi infraenv.API,
	cfg Config,
	generator generator.ISOInstallConfigGenerator,
	eventsHandler eventsapi.Handler,
	objectHandler s3wrapper.API,
	metricApi metrics.API,
	usageApi usage.API,
	operatorManagerApi operators.API,
	authHandler auth.Authenticator,
	authzHandler auth.Authorizer,
	k8sClient k8sclient.K8SClient,
	ocmClient *ocm.Client,
	leaderElector leader.Leader,
	pullSecretValidator validations.PullSecretValidator,
	versionsHandler versions.Handler,
	osImages versions.OSImages,
	crdUtils CRDUtils,
	IgnitionBuilder ignition.IgnitionBuilder,
	hwValidator hardware.Validator,
	dnsApi dns.DNSApi,
	installConfigBuilder installcfg.InstallConfigBuilder,
	staticNetworkConfig staticnetworkconfig.StaticNetworkConfig,
	gcConfig garbagecollector.Config,
	providerRegistry registry.ProviderRegistry,
	insecureIPXEURLs bool,
) *bareMetalInventory {
	return &bareMetalInventory{
		db:                   db,
		stream:               stream,
		log:                  log,
		Config:               cfg,
		hostApi:              hostApi,
		clusterApi:           clusterApi,
		infraEnvApi:          infraEnvApi,
		dnsApi:               dnsApi,
		generator:            generator,
		eventsHandler:        eventsHandler,
		objectHandler:        objectHandler,
		metricApi:            metricApi,
		usageApi:             usageApi,
		operatorManagerApi:   operatorManagerApi,
		authHandler:          authHandler,
		authzHandler:         authzHandler,
		k8sClient:            k8sClient,
		ocmClient:            ocmClient,
		leaderElector:        leaderElector,
		secretValidator:      pullSecretValidator,
		versionsHandler:      versionsHandler,
		osImages:             osImages,
		crdUtils:             crdUtils,
		IgnitionBuilder:      IgnitionBuilder,
		hwValidator:          hwValidator,
		installConfigBuilder: installConfigBuilder,
		staticNetworkConfig:  staticNetworkConfig,
		gcConfig:             gcConfig,
		providerRegistry:     providerRegistry,
		insecureIPXEURLs:     insecureIPXEURLs,
	}
}

func (b *bareMetalInventory) ValidatePullSecret(secret string, username string) error {
	return b.secretValidator.ValidatePullSecret(secret, username)
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

func (b *bareMetalInventory) setDefaultRegisterClusterParams(ctx context.Context, params installer.V2RegisterClusterParams, id strfmt.UUID) (installer.V2RegisterClusterParams, error) {
	log := logutil.FromContext(ctx, b.log)

	if params.NewClusterParams.APIVip != "" && len(params.NewClusterParams.APIVips) == 0 {
		params.NewClusterParams.APIVips = []*models.APIVip{{IP: models.IP(params.NewClusterParams.APIVip), ClusterID: id}}
	}

	if params.NewClusterParams.IngressVip != "" && len(params.NewClusterParams.IngressVip) == 0 {
		params.NewClusterParams.IngressVips = []*models.IngressVip{{IP: models.IP(params.NewClusterParams.IngressVip), ClusterID: id}}
	}

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
		params.NewClusterParams.VipDhcpAllocation = swag.Bool(false)
	}
	if params.NewClusterParams.Hyperthreading == nil {
		params.NewClusterParams.Hyperthreading = swag.String(models.ClusterHyperthreadingAll)
	}
	if params.NewClusterParams.SchedulableMasters == nil {
		params.NewClusterParams.SchedulableMasters = swag.Bool(false)
	}
	if params.NewClusterParams.HighAvailabilityMode == nil {
		params.NewClusterParams.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
	}

	log.Infof("Verifying cluster platform and user-managed-networking, got platform=%s and userManagedNetworking=%t", getPlatformType(params.NewClusterParams.Platform), swag.BoolValue(params.NewClusterParams.UserManagedNetworking))
	platform, userManagedNetworking, err := provider.GetActualCreateClusterPlatformParams(params.NewClusterParams.Platform, params.NewClusterParams.UserManagedNetworking, params.NewClusterParams.HighAvailabilityMode)
	if err != nil {
		log.Error(err)
		return params, err
	}

	params.NewClusterParams.Platform = platform
	params.NewClusterParams.UserManagedNetworking = userManagedNetworking
	log.Infof("Cluster high-availability-mode is set to %s, setting platform type to %s and user-managed-networking to %t", swag.StringValue(params.NewClusterParams.HighAvailabilityMode), getPlatformType(platform), swag.BoolValue(userManagedNetworking))

	if params.NewClusterParams.AdditionalNtpSource == nil {
		params.NewClusterParams.AdditionalNtpSource = &b.Config.DefaultNTPSource
	}
	if params.NewClusterParams.DiskEncryption == nil {
		params.NewClusterParams.DiskEncryption = &models.DiskEncryption{
			EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
			Mode:     swag.String(models.DiskEncryptionModeTpmv2),
		}
	}

	params.NewClusterParams.NetworkType, err = getDefaultNetworkType(params)
	if err != nil {
		return params, err
	}

	return params, nil
}

// If the cluster is SNO or the OpenShift version >= 4.12.0-0.0,
// the network type will be set to "OVNKubernetes", otherwise it will be set to "OpenShiftSDN"
func getDefaultNetworkType(params installer.V2RegisterClusterParams) (*string, error) {
	if params.NewClusterParams.NetworkType != nil {
		return params.NewClusterParams.NetworkType, nil
	}

	isOpenShiftVersionRecentEnough, err := common.BaseVersionGreaterOrEqual(swag.StringValue(params.NewClusterParams.OpenshiftVersion), minimalOpenShiftVersionForDefaultNetworkTypeOVNKubernetes)
	if err != nil {
		return nil, err
	}

	isSingleNodeCluster := swag.StringValue(params.NewClusterParams.HighAvailabilityMode) == models.ClusterCreateParamsHighAvailabilityModeNone

	if isOpenShiftVersionRecentEnough || isSingleNodeCluster {
		return swag.String(models.ClusterCreateParamsNetworkTypeOVNKubernetes), nil
	} else {
		return swag.String(models.ClusterCreateParamsNetworkTypeOpenShiftSDN), nil
	}
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

	if getPlatformType(params.NewClusterParams.Platform) == string(models.PlatformTypeNutanix) {
		if err = verifyMinimalOpenShiftVersionForNutanix(swag.StringValue(params.NewClusterParams.OpenshiftVersion)); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if err = b.validateIgnitionEndpointURL(params.NewClusterParams.IgnitionEndpoint, log); err != nil {
		return err
	}

	if params.NewClusterParams.AdditionalNtpSource != nil {
		ntpSource := swag.StringValue(params.NewClusterParams.AdditionalNtpSource)

		if ntpSource != "" && !pkgvalidations.ValidateAdditionalNTPSource(ntpSource) {
			err = errors.Errorf("Invalid NTP source: %s", ntpSource)
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.NewClusterParams.Tags != nil {
		if err := pkgvalidations.ValidateTags(swag.StringValue(params.NewClusterParams.Tags)); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.NewClusterParams.Platform != nil {
		if err := validations.ValidateHighAvailabilityModeWithPlatform(params.NewClusterParams.HighAvailabilityMode, params.NewClusterParams.Platform); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	return nil
}

func (b *bareMetalInventory) RegisterClusterInternal(
	ctx context.Context,
	kubeKey *types.NamespacedName,
	params installer.V2RegisterClusterParams) (*common.Cluster, error) {

	id := strfmt.UUID(uuid.New().String())
	url := installer.V2GetClusterURL{ClusterID: id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	log.Infof("Register cluster: %s with id %s and params %+v", swag.StringValue(params.NewClusterParams.Name), id, params.NewClusterParams)
	success := false
	var err error
	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
			log.Info(msg)
			eventgen.SendClusterRegistrationSucceededEvent(ctx, b.eventsHandler, id, models.ClusterKindCluster)
		} else {
			errWrapperLog := log
			errStr := fmt.Sprintf("Failed to registered cluster %s with id %s", swag.StringValue(params.NewClusterParams.Name), id)
			if err != nil {
				errWrapperLog = log.WithError(err)
				eventgen.SendClusterRegistrationFailedEvent(ctx, b.eventsHandler, id, err.Error(), models.ClusterKindCluster)
			} else {
				eventgen.SendClusterRegistrationFailedEvent(ctx, b.eventsHandler, id, errStr, models.ClusterKindCluster)
			}
			errWrapperLog.Errorf(errStr)
		}
	}()

	if err = validations.ValidateClusterCreateIPAddresses(b.IPv6Support, id, params.NewClusterParams); err != nil {
		b.log.WithError(err).Error("Cannot register cluster. Failed VIP validations")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	if err = validations.ValidateDualStackNetworks(params.NewClusterParams, false); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	params, err = b.setDefaultRegisterClusterParams(ctx, params, id)
	if err != nil {
		return nil, err
	}

	if err = b.validateRegisterClusterInternalParams(&params, log); err != nil {
		return nil, err
	}

	cpuArchitecture, err := b.getNewClusterCPUArchitecture(params.NewClusterParams)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	releaseImage, err := b.versionsHandler.GetReleaseImage(ctx, swag.StringValue(params.NewClusterParams.OpenshiftVersion),
		cpuArchitecture, swag.StringValue(params.NewClusterParams.PullSecret))
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
	log.Infof("selected cluster release image: arch=%s, openshiftVersion=%s, url=%s",
		swag.StringValue(releaseImage.CPUArchitecture),
		swag.StringValue(releaseImage.OpenshiftVersion),
		swag.StringValue(releaseImage.URL))

	if len(releaseImage.CPUArchitectures) > 1 {
		log.Infof("Setting cluster as multi-arch because of the release image (requested was %s)", cpuArchitecture)
		cpuArchitecture = common.MultiCPUArchitecture
		// (MGMT-11859) Additional check here ensures that the customer cannot just guess a version
		//              with multiarch in order to get access to that release payload.
		var multiarchAllowed bool
		multiarchAllowed, err = b.authzHandler.HasOrgBasedCapability(ctx, ocm.MultiarchCapabilityName)
		if err != nil {
			log.WithError(err).Errorf("error getting user %s capability", ocm.MultiarchCapabilityName)
		}
		if err != nil || !multiarchAllowed {
			err = common.NewApiError(http.StatusBadRequest, errors.New("multiarch clusters are not available"))
			return nil, err
		}
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	monitoredOperators := b.operatorManagerApi.GetSupportedOperatorsByType(models.OperatorTypeBuiltin)

	cluster := common.Cluster{
		Cluster: models.Cluster{
			ID:                           &id,
			Href:                         swag.String(url.String()),
			Kind:                         swag.String(models.ClusterKindCluster),
			APIVip:                       params.NewClusterParams.APIVip,
			APIVips:                      params.NewClusterParams.APIVips,
			BaseDNSDomain:                params.NewClusterParams.BaseDNSDomain,
			IngressVip:                   params.NewClusterParams.IngressVip,
			IngressVips:                  params.NewClusterParams.IngressVips,
			Name:                         swag.StringValue(params.NewClusterParams.Name),
			OpenshiftVersion:             *releaseImage.Version,
			OcpReleaseImage:              *releaseImage.URL,
			SSHPublicKey:                 params.NewClusterParams.SSHPublicKey,
			UserName:                     ocm.UserNameFromContext(ctx),
			OrgID:                        ocm.OrgIDFromContext(ctx),
			EmailDomain:                  ocm.EmailDomainFromContext(ctx),
			HTTPProxy:                    swag.StringValue(params.NewClusterParams.HTTPProxy),
			HTTPSProxy:                   swag.StringValue(params.NewClusterParams.HTTPSProxy),
			NoProxy:                      swag.StringValue(params.NewClusterParams.NoProxy),
			VipDhcpAllocation:            params.NewClusterParams.VipDhcpAllocation,
			NetworkType:                  params.NewClusterParams.NetworkType,
			UserManagedNetworking:        params.NewClusterParams.UserManagedNetworking,
			AdditionalNtpSource:          swag.StringValue(params.NewClusterParams.AdditionalNtpSource),
			MonitoredOperators:           monitoredOperators,
			HighAvailabilityMode:         params.NewClusterParams.HighAvailabilityMode,
			Hyperthreading:               swag.StringValue(params.NewClusterParams.Hyperthreading),
			SchedulableMasters:           params.NewClusterParams.SchedulableMasters,
			SchedulableMastersForcedTrue: swag.Bool(true),
			Platform:                     params.NewClusterParams.Platform,
			ClusterNetworks:              params.NewClusterParams.ClusterNetworks,
			ServiceNetworks:              params.NewClusterParams.ServiceNetworks,
			MachineNetworks:              params.NewClusterParams.MachineNetworks,
			CPUArchitecture:              cpuArchitecture,
			IgnitionEndpoint:             params.NewClusterParams.IgnitionEndpoint,
			Tags:                         swag.StringValue(params.NewClusterParams.Tags),
		},
		KubeKeyName:                 kubeKey.Name,
		KubeKeyNamespace:            kubeKey.Namespace,
		TriggerMonitorTimestamp:     time.Now(),
		MachineNetworkCidrUpdatedAt: time.Now(),
	}

	if params.NewClusterParams.OlmOperators != nil {
		var newOLMOperators []*models.MonitoredOperator
		newOLMOperators, err = b.getOLMOperators(&cluster, params.NewClusterParams.OlmOperators, log)
		if err != nil {
			return nil, err
		}

		err = b.operatorManagerApi.EnsureOperatorPrerequisite(&cluster, *releaseImage.Version, newOLMOperators)
		if err != nil {
			log.Error(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}

		monitoredOperators = append(monitoredOperators, newOLMOperators...)
	}

	cluster.MonitoredOperators = monitoredOperators

	pullSecret := swag.StringValue(params.NewClusterParams.PullSecret)
	err = b.ValidatePullSecret(pullSecret, ocm.UserNameFromContext(ctx))
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

	if err = validations.ValidateClusterNameFormat(swag.StringValue(params.NewClusterParams.Name),
		getPlatformType(params.NewClusterParams.Platform)); err != nil {
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

	err = b.clusterApi.RegisterCluster(ctx, &cluster)
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
	b.metricApi.ClusterRegistered()
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
		log.WithError(err).Errorf("Failed to update ams_subscription_id in cluster %s, rolling back AMS subscription and cluster registration", *cluster.ID)
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

func verifyMinimalOpenShiftVersionForNutanix(requestedOpenshiftVersion string) error {
	ocpVersion, err := version.NewVersion(requestedOpenshiftVersion)
	if err != nil {
		return errors.Errorf("Failed to parse OCP version %s", requestedOpenshiftVersion)
	}
	minimalVersionForSno, err := version.NewVersion(minimalOpenShiftVersionForNutanix)
	if err != nil {
		return errors.Errorf("Failed to parse minimal OCP version %s", minimalOpenShiftVersionForSingleNode)
	}
	if ocpVersion.LessThan(minimalVersionForSno) {
		return errors.Errorf("Invalid OCP version (%s) for Nutanix, Nutanix integration is supported for version 4.11 and above", requestedOpenshiftVersion)
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

	if swag.StringValue(newClusterParams.NetworkType) == models.ClusterNetworkTypeOpenShiftSDN {
		return errors.Errorf("OpenShiftSDN network type is not allowed in single node mode")
	}

	return nil
}

func (b *bareMetalInventory) getNewClusterCPUArchitecture(newClusterParams *models.ClusterCreateParams) (string, error) {
	if newClusterParams.CPUArchitecture == common.MultiCPUArchitecture {
		return newClusterParams.CPUArchitecture, nil
	}
	if newClusterParams.CPUArchitecture == "" || newClusterParams.CPUArchitecture == common.DefaultCPUArchitecture {
		// Empty value implies x86_64 (default architecture),
		// which is supported for now regardless of the release images list.
		// TODO: remove once release images list is exclusively used.
		return common.DefaultCPUArchitecture, nil
	}

	if !swag.BoolValue(newClusterParams.UserManagedNetworking) && !featuresupport.IsFeatureSupported(swag.StringValue(newClusterParams.OpenshiftVersion),
		models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING) {

		return "", errors.Errorf("Non x86_64 CPU architectures for version %s are supported only with User Managed Networking", swag.StringValue(newClusterParams.OpenshiftVersion))
	}

	cpuArchitectures := b.osImages.GetCPUArchitectures(*newClusterParams.OpenshiftVersion)
	for _, cpuArchitecture := range cpuArchitectures {
		if cpuArchitecture == newClusterParams.CPUArchitecture {
			return cpuArchitecture, nil
		}
	}

	// Didn't find requested architecture in the release images list
	return "", errors.Errorf("Requested CPU architecture %s is not available", newClusterParams.CPUArchitecture)
}

// importedClusterBaseDomain attempts to convert the API hostname provided
// by users during cluster import into a cluster base domain. If the hostname
// is an IP address and not a domain, or if the provided domain is not an API
// domain, this function returns an empty string. This function also returns
// the cluster name that is implied by the domain.
//
// Examples:
//
// importedClusterBaseDomain("api.cluster.example.com") returns ("example.com", "cluster")
// importedClusterBaseDomain("api-int.cluster.example.com") returns ("", "")
// importedClusterBaseDomain("192.168.111.111") returns ("", "")
// importedClusterBaseDomain("2001:db8::ff") returns ("", "")
func importedClusterBaseDomain(hostname string) (string, string) {
	if net.ParseIP(hostname) != nil {
		return "", ""
	}

	domainComponents := strings.SplitN(hostname, ".", 3)
	if len(domainComponents) != 3 {
		return "", ""
	}

	api := domainComponents[0]
	clusterName := domainComponents[1]
	clusterBaseDomain := domainComponents[2]

	if api != "api" {
		return "", ""
	}

	return clusterBaseDomain, clusterName
}

func (b *bareMetalInventory) V2ImportClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, id *strfmt.UUID,
	params installer.V2ImportClusterParams) (*common.Cluster, error) {
	url := installer.V2GetClusterURL{ClusterID: *id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	apiHostname := swag.StringValue(params.NewImportClusterParams.APIVipDnsname)
	clusterName := swag.StringValue(params.NewImportClusterParams.Name)

	log.Infof("Import add-hosts-cluster: %s, id %s, openshift cluster id %s", clusterName, id.String(), params.NewImportClusterParams.OpenshiftClusterID)

	if clusterPkg.ClusterExists(b.db, *id) {
		return nil, common.NewApiError(http.StatusBadRequest, fmt.Errorf("AddHostsCluster for AI cluster %s already exists", id))
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	baseDomain := ""
	if importedBaseDomain, importedClusterName := importedClusterBaseDomain(apiHostname); importedBaseDomain != "" {
		baseDomain = importedBaseDomain

		// Explicitly ignore the user-provided cluster name as we derive a more
		// accurate name from the API domain the user provided. The cluster
		// name from the API domain takes precedence because the cluster name
		// is used in DNS validations and it is more likely to be correct. For
		// example, some versions of the UI provide a cluster name with a junk
		// prefix "scale-up-" that could later mess with DNS validations, as
		// that's obviously incorrect.
		clusterName = importedClusterName
	}

	newCluster := common.Cluster{Cluster: models.Cluster{
		ID:                 id,
		Href:               swag.String(url.String()),
		Kind:               swag.String(models.ClusterKindAddHostsCluster),
		Name:               clusterName,
		OpenshiftClusterID: common.StrFmtUUIDVal(params.NewImportClusterParams.OpenshiftClusterID),
		UserName:           ocm.UserNameFromContext(ctx),
		OrgID:              ocm.OrgIDFromContext(ctx),
		EmailDomain:        ocm.EmailDomainFromContext(ctx),
		APIVipDNSName:      swag.String(apiHostname),
		BaseDNSDomain:      baseDomain,
		HostNetworks:       []*models.HostNetwork{},
		Hosts:              []*models.Host{},
		Platform:           &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
		Imported:           swag.Bool(true),
	},
		KubeKeyName:      kubeKey.Name,
		KubeKeyNamespace: kubeKey.Namespace,
	}

	err := validations.ValidateClusterNameFormat(clusterName, getPlatformType(newCluster.Platform))
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	// After registering the cluster, its status should be 'ClusterStatusAddingHosts'
	err = b.clusterApi.RegisterAddHostsCluster(ctx, &newCluster)
	if err != nil {
		log.Errorf("failed to register cluster %s ", clusterName)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	b.metricApi.ClusterRegistered()
	return &newCluster, nil
}

func (b *bareMetalInventory) createAndUploadDay2NodeIgnition(ctx context.Context, cluster *common.Cluster, host *models.Host, ignitionEndpointToken string) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Starting createAndUploadDay2NodeIgnition for cluster %s, host %s", cluster.ID, host.ID)

	ignitionEndpointUrl, err := hostutil.GetIgnitionEndpoint(cluster, host)
	if err != nil {
		return errors.Wrapf(err, "Failed to build ignition endpoint for host %s in cluster %s", host.ID, cluster.ID)
	}

	var caCert *string = nil
	if cluster.IgnitionEndpoint != nil {
		caCert = cluster.IgnitionEndpoint.CaCertificate
	}

	fullIgnition, err := b.IgnitionBuilder.FormatSecondDayWorkerIgnitionFile(ignitionEndpointUrl, caCert, ignitionEndpointToken, host)
	if err != nil {
		return errors.Wrapf(err, "Failed to create ignition string for cluster %s, host %s", cluster.ID, host.ID)
	}

	fileName := fmt.Sprintf("%s/%s-%s.ign", cluster.ID, common.GetEffectiveRole(host), host.ID)
	log.Infof("Uploading ignition file <%s>", fileName)
	err = b.objectHandler.Upload(ctx, fullIgnition, fileName)
	if err != nil {
		return errors.Errorf("Failed to upload worker ignition for cluster %s", cluster.ID)
	}
	return nil
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

// DeregisterClusterInternal contains only what is required for the cluster deployment controller to deregister the cluster
func (b *bareMetalInventory) DeregisterClusterInternal(ctx context.Context, cluster *common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister cluster id %s", cluster.ID)

	if err := b.clusterApi.DeregisterCluster(ctx, cluster); err != nil {
		log.WithError(err).Errorf("failed to deregister cluster %s", cluster.ID)
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
			if err = b.hostApi.UnRegisterHost(ctx, h); err != nil {
				log.WithError(err).Errorf("failed to delete host: %s", h.ID.String())
				return err
			}
			eventgen.SendHostDeregisteredEvent(ctx, b.eventsHandler, *h.ID, h.InfraEnvID, cluster.ID,
				hostutil.GetHostnameForMsg(h))
		} else if h.ClusterID != nil {
			if err = b.hostApi.UnbindHost(ctx, h, b.db, false); err != nil {
				log.WithError(err).Errorf("Failed to unbind host <%s>", h.ID.String())
				return err
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) updateExternalImageInfo(ctx context.Context, infraEnv *common.InfraEnv, infraEnvProxyHash string, imageType models.ImageType) error {
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

	osImage, err := b.osImages.GetOsImageOrLatest(infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
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
		infraEnv.DownloadURL, expiresAt, err = b.generateImageDownloadURL(ctx, infraEnv.ID.String(), string(imageType), version, arch, infraEnv.ImageTokenKey)
		if err != nil {
			return errors.Wrap(err, "failed to create download URL")
		}

		details := b.getIgnitionConfigForLogging(ctx, infraEnv, b.log, imageType)

		eventgen.SendImageInfoUpdatedEvent(ctx, b.eventsHandler, common.StrFmtUUIDPtr(infraEnv.ClusterID), *infraEnv.ID, details)
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

func (b *bareMetalInventory) GenerateInfraEnvISOInternal(ctx context.Context, infraEnv *common.InfraEnv) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("prepare image for infraEnv %s", infraEnv.ID)

	if !infraEnv.PullSecretSet {
		errMsg := "Can't generate infraEnv ISO without pull secret"
		log.Error(errMsg)
		return common.NewApiError(http.StatusBadRequest, errors.New(errMsg))
	}

	now := time.Now()
	updates := map[string]interface{}{}
	updates["generated_at"] = strfmt.DateTime(now)
	updates["image_expires_at"] = strfmt.DateTime(now.Add(b.Config.ImageExpirationTime))
	dbReply := b.db.Model(&common.InfraEnv{}).Where("id = ?", infraEnv.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update infra env: %s", infraEnv.ID)
		msg := "Failed to generate image: error updating metadata"
		return common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	err := b.createAndUploadNewImage(ctx, log, infraEnv.ProxyHash, *infraEnv.ID, common.ImageTypeValue(infraEnv.Type), true, false)
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

	return b.updateExternalImageInfo(ctx, infraEnv, infraEnvProxyHash, imageType)
}

func (b *bareMetalInventory) getIgnitionConfigForLogging(ctx context.Context, infraEnv *common.InfraEnv, log logrus.FieldLogger, imageType models.ImageType) string {
	ignitionConfigForLogging, _ := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, true, b.authHandler.AuthType(), string(imageType))
	log.Infof("Generated infra env <%s> image with ignition config", infraEnv.ID)
	log.Debugf("Ignition for infra env <%s>: %s", infraEnv.ID, ignitionConfigForLogging)
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

func (b *bareMetalInventory) refreshAllHostsOnInstall(ctx context.Context, cluster *common.Cluster) error {
	err := b.setMajorityGroupForCluster(cluster.ID, b.db)
	if err != nil {
		return err
	}
	err = b.detectAndStoreCollidingIPsForCluster(cluster.ID, b.db)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to detect and store colliding IPs for cluster %s", cluster.ID.String())
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
	var err error
	var cluster *common.Cluster

	log.Infof("preparing for cluster %s installation", params.ClusterID)
	if cluster, err = common.GetClusterFromDBWithHosts(b.db, params.ClusterID); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	var autoAssigned bool

	// auto select hosts roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		var updated bool
		sortedHosts, canRefreshRoles := host.SortHosts(cluster.Hosts)
		if canRefreshRoles {
			for i := range sortedHosts {
				updated, err = b.hostApi.AutoAssignRole(ctx, cluster.Hosts[i], tx)
				if err != nil {
					return err
				}
				autoAssigned = autoAssigned || updated
			}
		}
		hasIgnoredValidations := common.IgnoredValidationsAreSet(cluster)
		if hasIgnoredValidations {
			eventgen.SendValidationsIgnoredEvent(ctx, b.eventsHandler, *cluster.ID)
		}
		//usage for auto role selection is measured only for day1 clusters with more than
		//3 hosts (which would automatically be assigned as masters if the hw is sufficient)
		if usages, u_err := usage.Unmarshal(cluster.Cluster.FeatureUsage); u_err == nil {
			report := cluster.Cluster.TotalHostCount > common.MinMasterHostsNeededForInstallation && autoAssigned
			if hasIgnoredValidations {
				b.setUsage(true, usage.ValidationsIgnored, nil, usages)
			}
			b.setUsage(report, usage.AutoAssignRoleUsage, nil, usages)
			b.usageApi.Save(tx, *cluster.ID, usages)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if autoAssigned {
		if err = b.refreshAllHostsOnInstall(ctx, cluster); err != nil {
			return nil, err
		}
		if _, err = b.clusterApi.RefreshStatus(ctx, cluster, b.db); err != nil {
			return nil, err
		}

		// Reload again after refresh
		if cluster, err = common.GetClusterFromDBWithHosts(b.db, params.ClusterID); err != nil {
			return nil, common.NewApiError(http.StatusNotFound, err)
		}
	}
	// Verify cluster is ready to install
	if ok, reason := b.clusterApi.IsReadyForInstallation(cluster); !ok {
		return nil, common.NewApiError(http.StatusConflict,
			errors.Errorf("Cluster is not ready for installation, %s validation_info=%s", reason, cluster.ValidationsInfo))
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
	if err = b.clusterApi.DeleteClusterLogs(ctx, cluster, b.objectHandler); err != nil {
		log.WithError(err).Warnf("Failed deleting s3 logs of cluster %s", cluster.ID.String())
	}

	clusterInfraenvs, err := b.getClusterInfraenvs(ctx, cluster)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infraenvs for cluster %s", cluster.ID.String())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("Failed to get infraenvs for cluster"))
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

		if err = b.generateClusterInstallConfig(asyncCtx, *cluster, clusterInfraenvs); err != nil {
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
			log.Errorf("InstallSingleDay2HostInternal failed to recover: %s", r)
			log.Error(string(debug.Stack()))
			tx.Rollback()
		}
	}()

	// in case host monitor already updated the state we need to use FOR UPDATE option
	if cluster, err = common.GetClusterFromDBForUpdate(tx, clusterId, common.UseEagerLoading); err != nil {
		return err
	}

	// move host to installing
	err = b.createAndUploadDay2NodeIgnition(ctx, cluster, &h.Host, h.IgnitionEndpointToken)
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
	err = b.createAndUploadDay2NodeIgnition(ctx, cluster, h, host.IgnitionEndpointToken)
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
	return installer.NewV2InstallHostAccepted().WithPayload(h)
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
		err2 := fmt.Errorf("error getting cluster, error: %w", err)
		b.log.Error(err2.Error())
		return nil, err2
	}
	// no hosts or SNO
	if len(cluster.Hosts) == 0 || common.IsSingleNodeCluster(cluster) {
		b.log.Infof("GetSupportedPlatforms - No hosts or cluster is SNO, setting supported-platform to [%s]", models.PlatformTypeNone)
		return &[]models.PlatformType{models.PlatformTypeNone}, nil
	}
	hostSupportedPlatforms, err := b.providerRegistry.GetSupportedProvidersByHosts(cluster.Hosts)
	if err != nil {
		err2 := fmt.Errorf("error while checking supported platforms, error: %w", err)
		b.log.Error(err2.Error())
		return nil, err2
	}

	b.log.Infof("Found %d supported-platforms for cluster %s", len(hostSupportedPlatforms), cluster.ID)
	return &hostSupportedPlatforms, nil
}

func (b *bareMetalInventory) GetClusterSupportedPlatforms(ctx context.Context, params installer.GetClusterSupportedPlatformsParams) middleware.Responder {
	supportedPlatforms, err := b.GetClusterSupportedPlatformsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetClusterSupportedPlatformsOK().WithPayload(*supportedPlatforms)
}

func (b *bareMetalInventory) UpdateClusterInstallConfigInternal(ctx context.Context, params installer.V2UpdateClusterInstallConfigParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var clusterInfraenvs []*common.InfraEnv
	var err error
	query := "id = ?"

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("UpdateClusterInstallConfigInternal failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Errorf("UpdateClusterInstallConfigInternal failed to recover: %s", r)
			log.Error(string(debug.Stack()))
			tx.Rollback()
		}
	}()

	if cluster, err = common.GetClusterFromDBForUpdate(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		return nil, err
	}

	clusterInfraenvs, err = b.getClusterInfraenvs(ctx, cluster)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infraenvs for cluster %s", cluster.ID.String())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("Failed to get infraenvs for cluster"))
	}

	if err = b.installConfigBuilder.ValidateInstallConfigPatch(cluster, clusterInfraenvs, params.InstallConfigParams); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	// Set install config overrides feature usage
	err = b.setInstallConfigOverridesUsage(cluster.Cluster.FeatureUsage, params.InstallConfigParams, params.ClusterID, tx)
	if err != nil {
		// Failure to set the feature usage isn't a failure to update the install config override so we only print the error instead of returning it
		log.WithError(err).Errorf("failed to set install config overrides feature usage for cluster %s", params.ClusterID)
	}

	cluster.InstallConfigOverrides = params.InstallConfigParams
	err = tx.Model(&common.Cluster{}).Where(query, params.ClusterID).Update("install_config_overrides", params.InstallConfigParams).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update install config overrides")
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.updateMonitoredOperators(tx, cluster)
	if err != nil {
		log.WithError(err).Error("failed to update monitored operators")
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	err = tx.Commit().Error
	if err != nil {
		log.Error(err)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	txSuccess = true
	eventgen.SendInstallConfigAppliedEvent(ctx, b.eventsHandler, params.ClusterID)
	log.Infof("Custom install config was applied to cluster %s", params.ClusterID)

	return cluster, nil
}

func (b *bareMetalInventory) setInstallConfigOverridesUsage(featureUsages string, installConfigParams string, clusterID strfmt.UUID, db *gorm.DB) error {
	usages, err := usage.Unmarshal(featureUsages)
	if err != nil {
		return err
	}
	var installConfigOverrides map[string]interface{}
	err = json.Unmarshal([]byte(installConfigParams), &installConfigOverrides)
	if err != nil {
		return err
	} else if len(installConfigOverrides) < 1 {
		return nil
	}
	props := usages[usage.InstallConfigOverrides].Data
	if props == nil {
		props = make(map[string]interface{})
	}
	for installConfigKey, installConfigValue := range installConfigOverrides {
		key := installConfigKey
		switch secondaryKeys := installConfigValue.(type) {
		case map[string]interface{}:
			for secondaryKey := range secondaryKeys {
				key = key + " " + secondaryKey
			}
		}
		props[key] = true
	}
	b.setUsage(true, usage.InstallConfigOverrides, &props, usages)
	b.usageApi.Save(db, clusterID, usages)
	return nil
}

func (b *bareMetalInventory) generateClusterInstallConfig(ctx context.Context, cluster common.Cluster, clusterInfraenvs []*common.InfraEnv) error {
	log := logutil.FromContext(ctx, b.log)

	rhRootCa := ignition.RedhatRootCA
	if !b.Config.InstallRHCa {
		rhRootCa = ""
	}

	cfg, err := b.installConfigBuilder.GetInstallConfig(&cluster, clusterInfraenvs, rhRootCa)
	if err != nil {
		log.WithError(err).Errorf("failed to get install config for cluster %s", cluster.ID)
		return errors.Wrapf(err, "failed to get install config for cluster %s", cluster.ID)
	}

	releaseImage, err := b.versionsHandler.GetReleaseImage(ctx, cluster.OpenshiftVersion, cluster.CPUArchitecture, cluster.PullSecret)
	if err != nil {
		msg := fmt.Sprintf("failed to get OpenshiftVersion for cluster %s with openshift version %s", cluster.ID, cluster.OpenshiftVersion)
		log.WithError(err).Errorf(msg)
		return errors.Wrapf(err, msg)
	}

	installerReleaseImageOverride := ""
	if isBaremetalBinaryFromAnotherReleaseImageRequired(cluster.CPUArchitecture, cluster.OpenshiftVersion, cluster.Platform.Type) {
		defaultArchImage, err := b.versionsHandler.GetReleaseImage(ctx, cluster.OpenshiftVersion, common.DefaultCPUArchitecture, cluster.PullSecret)
		if err != nil {
			msg := fmt.Sprintf("failed to get image for installer image override "+
				"for cluster %s with openshift version %s and %s arch", cluster.ID, cluster.OpenshiftVersion, cluster.CPUArchitecture)
			log.WithError(err).Errorf(msg)
			return errors.Wrapf(err, msg)
		}
		log.Infof("Overriding %s baremetal installer image image: %s with %s: %s", cluster.CPUArchitecture,
			*releaseImage.URL, common.DefaultCPUArchitecture, *defaultArchImage.URL)
		installerReleaseImageOverride = *defaultArchImage.URL
	}

	if err := b.generator.GenerateInstallConfig(ctx, cluster, cfg, *releaseImage.URL, installerReleaseImageOverride); err != nil {
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

	err = b.detectAndStoreCollidingIPsForCluster(cluster.ID, tx)
	if err != nil {
		log.WithError(err).Errorf("Failed to detect and store colliding IPs for cluster %s", cluster.ID.String())
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
				log.Infof("ignoring wrong status error (%s) for host %s in cluster %s", err.Error(), host.ID, cluster.ID.String())
			default:
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
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

	if swag.StringValue(params.ClusterUpdateParams.NetworkType) == models.ClusterNetworkTypeOpenShiftSDN {
		return errors.Errorf("OpenShiftSDN network type is not allowed in single node mode")
	}

	return nil
}

func (b *bareMetalInventory) validateAndUpdateClusterParams(ctx context.Context, cluster *common.Cluster, params *installer.V2UpdateClusterParams) (installer.V2UpdateClusterParams, error) {
	log := logutil.FromContext(ctx, b.log)

	if swag.StringValue(params.ClusterUpdateParams.PullSecret) != "" {
		if err := b.ValidatePullSecret(*params.ClusterUpdateParams.PullSecret, ocm.UserNameFromContext(ctx)); err != nil {
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
		platform := getPlatformType(params.ClusterUpdateParams.Platform)
		if platform == "" {
			platform = getPlatformType(cluster.Platform)
		}
		if err := validations.ValidateClusterNameFormat(*params.ClusterUpdateParams.Name, platform); err != nil {
			return installer.V2UpdateClusterParams{}, err
		}
	}

	if err := validations.ValidateClusterUpdateVIPAddresses(b.IPv6Support, cluster, params.ClusterUpdateParams); err != nil {
		b.log.WithError(err).Errorf("Cluster %s failed VIP validations", params.ClusterID)
		return installer.V2UpdateClusterParams{}, err

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

func getPlatformType(platform *models.Platform) string {
	if platform != nil && platform.Type != nil {
		return string(*platform.Type)
	}
	return ""
}

func (b *bareMetalInventory) v2UpdateClusterInternal(ctx context.Context, params installer.V2UpdateClusterParams, interactivity Interactivity) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var err error
	log.Infof("update cluster %s with params: %+v", params.ClusterID, params.ClusterUpdateParams)

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Errorf("update cluster failed to recover: %s", r)
			log.Error(string(debug.Stack()))
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

	if params, err = b.validateAndUpdateClusterParams(ctx, cluster, &params); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}
	alreadyDualStack := network.CheckIfClusterIsDualStack(cluster)
	if err = validations.ValidateDualStackNetworks(params.ClusterUpdateParams, alreadyDualStack); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = validateProxySettings(params.ClusterUpdateParams.HTTPProxy, params.ClusterUpdateParams.HTTPSProxy,
		params.ClusterUpdateParams.NoProxy, &cluster.OpenshiftVersion); err != nil {
		log.WithError(err).Errorf("Failed to validate Proxy settings")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.clusterApi.VerifyClusterUpdatability(cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", params.ClusterID)
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	log.Infof("Current cluster platform is set to %s and user-managed-networking is set to %t", getPlatformType(cluster.Platform), swag.BoolValue(cluster.UserManagedNetworking))
	log.Infof("Verifying cluster platform and user-managed-networking, got platform=%s and userManagedNetworking=%t", getPlatformType(params.ClusterUpdateParams.Platform), swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking))
	platform, userManagedNetworking, err := provider.GetActualUpdateClusterPlatformParams(params.ClusterUpdateParams.Platform, params.ClusterUpdateParams.UserManagedNetworking, cluster)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	params.ClusterUpdateParams.Platform = platform
	params.ClusterUpdateParams.UserManagedNetworking = userManagedNetworking
	log.Infof("Platform verification completed, setting platform type to %s and user-managed-networking to %t", getPlatformType(platform), swag.BoolValue(userManagedNetworking))

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

	if err = validations.ValidateHighAvailabilityModeWithPlatform(cluster.HighAvailabilityMode, platform); err != nil {
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

	if interactivity == Interactive {
		err = b.updateHostsAndClusterStatus(ctx, cluster, tx, log)
		if err != nil {
			log.WithError(err).Errorf("failed to validate or update cluster %s state or its hosts", cluster.ID)
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
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
	for _, host := range cluster.Hosts {
		b.customizeHost(&cluster.Cluster, host)
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

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
	// In order to check if the cluster is dual-stack or single-stack we are building a structure
	// that is a merge of the current cluster configuration and new configuration coming from
	// V2UpdateClusterParams. This ensures that the reqDualStack flag reflects a desired state and
	// not the stale one.
	// We need to do it because updateNonDhcpNetworkParams is called before updateNetworks, so
	// inside this function here cluster object has still values before applying requested changes.
	targetConfiguration := common.Cluster{}
	targetConfiguration.ClusterNetworks = cluster.ClusterNetworks
	targetConfiguration.ServiceNetworks = cluster.ServiceNetworks
	targetConfiguration.MachineNetworks = cluster.MachineNetworks
	targetConfiguration.APIVips = cluster.APIVips
	targetConfiguration.IngressVips = cluster.IngressVips

	if params.ClusterUpdateParams.ClusterNetworks != nil {
		targetConfiguration.ClusterNetworks = params.ClusterUpdateParams.ClusterNetworks
	}
	if params.ClusterUpdateParams.ServiceNetworks != nil {
		targetConfiguration.ServiceNetworks = params.ClusterUpdateParams.ServiceNetworks
	}
	if params.ClusterUpdateParams.MachineNetworks != nil {
		targetConfiguration.MachineNetworks = params.ClusterUpdateParams.MachineNetworks
	}
	reqDualStack := network.CheckIfClusterIsDualStack(&targetConfiguration)

	if params.ClusterUpdateParams.APIVip != nil {
		updates["api_vip"] = *params.ClusterUpdateParams.APIVip
	}
	if params.ClusterUpdateParams.APIVips != nil {
		targetConfiguration.APIVips = params.ClusterUpdateParams.APIVips
	}
	if params.ClusterUpdateParams.IngressVip != nil {
		updates["ingress_vip"] = *params.ClusterUpdateParams.IngressVip
	}
	if params.ClusterUpdateParams.IngressVips != nil {
		targetConfiguration.IngressVips = params.ClusterUpdateParams.IngressVips
	}
	if params.ClusterUpdateParams.MachineNetworks != nil &&
		common.IsSliceNonEmpty(params.ClusterUpdateParams.MachineNetworks) &&
		!reqDualStack {
		err := errors.New("Setting Machine network CIDR is forbidden when cluster is not in vip-dhcp-allocation mode")
		log.WithError(err).Warnf("Set Machine Network CIDR")
		return common.NewApiError(http.StatusBadRequest, err)
	}
	var err error
	err = validations.VerifyParsableVIPs(targetConfiguration.APIVips, targetConfiguration.IngressVips)
	if err != nil {
		log.WithError(err).Errorf("Failed validating VIPs of cluster id=%s", params.ClusterID)
		return err
	}

	err = network.ValidateNoVIPAddressesDuplicates(targetConfiguration.APIVips, targetConfiguration.IngressVips)
	if err != nil {
		log.WithError(err).Errorf("VIP verification failed for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if interactivity == Interactive && (params.ClusterUpdateParams.APIVips != nil || params.ClusterUpdateParams.IngressVips != nil) {
		var primaryMachineNetworkCidr string
		var secondaryMachineNetworkCidr string

		matchRequired := network.GetApiVipById(&targetConfiguration, 0) != "" || network.GetIngressVipById(&targetConfiguration, 0) != ""

		// We want to calculate Machine Network based on the API/Ingress VIPs only in case of the
		// single-stack cluster. Auto calculation is not supported for dual-stack in which we
		// require that user explicitly provides all the Machine Networks.
		if reqDualStack {
			if params.ClusterUpdateParams.MachineNetworks != nil {
				cluster.MachineNetworks = params.ClusterUpdateParams.MachineNetworks
				primaryMachineNetworkCidr = string(params.ClusterUpdateParams.MachineNetworks[0].Cidr)
			} else {
				primaryMachineNetworkCidr = network.GetPrimaryMachineCidrForUserManagedNetwork(cluster, log)
			}

			err = network.VerifyMachineNetworksDualStack(targetConfiguration.MachineNetworks, reqDualStack)
			if err != nil {
				log.WithError(err).Warnf("Verify dual-stack machine networks")
				return common.NewApiError(http.StatusBadRequest, err)
			}
			secondaryMachineNetworkCidr, err = network.GetSecondaryMachineCidr(cluster)
			if err != nil {
				return common.NewApiError(http.StatusBadRequest, err)
			}

			if err = network.VerifyVips(cluster.Hosts, primaryMachineNetworkCidr, network.GetApiVipById(&targetConfiguration, 0), network.GetIngressVipById(&targetConfiguration, 0), log); err != nil {
				log.WithError(err).Warnf("Verify VIPs")
				return common.NewApiError(http.StatusBadRequest, err)
			}

			if len(targetConfiguration.IngressVips) == 2 && len(targetConfiguration.APIVips) == 2 { // in case there's a second set of VIPs
				if err = network.VerifyVips(cluster.Hosts, secondaryMachineNetworkCidr, network.GetApiVipById(&targetConfiguration, 1), network.GetIngressVipById(&targetConfiguration, 1), log); err != nil {
					log.WithError(err).Warnf("Verify VIPs")
					return common.NewApiError(http.StatusBadRequest, err)
				}
			}

		} else {
			primaryMachineNetworkCidr, err = network.CalculateMachineNetworkCIDR(network.GetApiVipById(&targetConfiguration, 0), network.GetIngressVipById(&targetConfiguration, 0), cluster.Hosts, matchRequired)
			if err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Calculate machine network CIDR"))
			}
			if primaryMachineNetworkCidr != "" {
				// We set the machine networks in the ClusterUpdateParams, so they will be viewed as part of the request
				// to update the cluster

				// Earlier in this function, if reqDualStack was false and the MachineNetworks was non-empty, the function
				// returned with an error.  Therefore, params.ClusterUpdateParams.MachineNetworks is empty here before
				// the assignment below.
				params.ClusterUpdateParams.MachineNetworks = []*models.MachineNetwork{{Cidr: models.Subnet(primaryMachineNetworkCidr)}}
			}
			if err = network.VerifyVips(cluster.Hosts, primaryMachineNetworkCidr, network.GetApiVipById(&targetConfiguration, 0), network.GetIngressVipById(&targetConfiguration, 0), log); err != nil {
				log.WithError(err).Warnf("Verify VIPs")
				return common.NewApiError(http.StatusBadRequest, err)
			}
		}
	}

	return nil
}

func (b *bareMetalInventory) updateDhcpNetworkParams(db *gorm.DB, id *strfmt.UUID, updates map[string]interface{}, params installer.V2UpdateClusterParams, primaryMachineCIDR string) error {
	if err := validations.ValidateVIPsWereNotSetDhcpMode(swag.StringValue(params.ClusterUpdateParams.APIVip), swag.StringValue(params.ClusterUpdateParams.IngressVip),
		params.ClusterUpdateParams.APIVips, params.ClusterUpdateParams.IngressVips); err != nil {
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
		emptyCluster := common.Cluster{Cluster: models.Cluster{ID: id}}
		if err := network.UpdateVipsTables(db, &emptyCluster, true, true); err != nil {
			return err
		}
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

	if err = b.updatePlatformParams(params, updates, usages); err != nil {
		return err
	}

	if err = b.updateNetworkParams(params, cluster, updates, usages, db, log, interactivity); err != nil {
		return err
	}

	if err = b.updateNtpSources(params, updates, usages, log); err != nil {
		return err
	}

	if err = b.updateClusterTags(params, updates, usages, log); err != nil {
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

	if params.ClusterUpdateParams.Hyperthreading != nil {
		b.setUsage(*params.ClusterUpdateParams.Hyperthreading != models.ClusterHyperthreadingNone, usage.HyperthreadingUsage,
			&map[string]interface{}{"hyperthreading_enabled": *params.ClusterUpdateParams.Hyperthreading}, usages)
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && cluster.HighAvailabilityMode != nil {
		b.setUserManagedNetworkingAndMultiNodeUsage(swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking), *cluster.HighAvailabilityMode, usages)
	}

	if len(updates) > 0 {
		updates["trigger_monitor_timestamp"] = time.Now()
		err = db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates).Error
		if err != nil {
			return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "failed to update cluster: %s", params.ClusterID))
		}
	}

	return nil
}

func wereClusterVipsUpdated(clusterVips []string, paramsVips []string) bool {
	if len(clusterVips) != len(paramsVips) {
		return true
	}
	for i := range clusterVips {
		if clusterVips[i] != paramsVips[i] {
			return true
		}
	}
	return false
}

func (b *bareMetalInventory) updateVips(db *gorm.DB, params installer.V2UpdateClusterParams, cluster *common.Cluster) error {
	var apiVipUpdated bool
	var ingressVipUpdated bool

	paramVips := common.Cluster{
		Cluster: models.Cluster{
			APIVips:     params.ClusterUpdateParams.APIVips,
			IngressVips: params.ClusterUpdateParams.IngressVips,
		},
	}

	if params.ClusterUpdateParams.APIVips != nil && len(params.ClusterUpdateParams.APIVips) > 0 {
		if wereClusterVipsUpdated(network.GetApiVips(cluster), network.GetApiVips(&paramVips)) {
			apiVipUpdated = true
			cluster.APIVips = params.ClusterUpdateParams.APIVips
		}
	}
	if params.ClusterUpdateParams.IngressVips != nil && len(params.ClusterUpdateParams.IngressVips) > 0 {
		if wereClusterVipsUpdated(network.GetIngressVips(cluster), network.GetIngressVips(&paramVips)) {
			ingressVipUpdated = true
			cluster.IngressVips = params.ClusterUpdateParams.IngressVips
		}
	}

	if apiVipUpdated || ingressVipUpdated {
		return network.UpdateVipsTables(db, cluster, apiVipUpdated, ingressVipUpdated)
	}

	return nil
}

func (b *bareMetalInventory) updateNetworks(db *gorm.DB, params installer.V2UpdateClusterParams, updates map[string]interface{},
	cluster *common.Cluster, userManagedNetworking, vipDhcpAllocation bool) error {
	var err error
	var updated bool

	if params.ClusterUpdateParams.ClusterNetworks != nil && !network.AreClusterNetworksIdentical(params.ClusterUpdateParams.ClusterNetworks, cluster.ClusterNetworks) {
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
		updated = true
	}

	if params.ClusterUpdateParams.ServiceNetworks != nil && !network.AreServiceNetworksIdentical(params.ClusterUpdateParams.ServiceNetworks, cluster.ServiceNetworks) {
		for _, serviceNetwork := range params.ClusterUpdateParams.ServiceNetworks {
			if err = network.VerifyClusterOrServiceCIDR(string(serviceNetwork.Cidr)); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Service network CIDR %s", string(serviceNetwork.Cidr)))
			}
		}
		cluster.ServiceNetworks = params.ClusterUpdateParams.ServiceNetworks
		updated = true
	}

	if params.ClusterUpdateParams.MachineNetworks != nil && !network.AreMachineNetworksIdentical(params.ClusterUpdateParams.MachineNetworks, cluster.MachineNetworks) {
		for _, machineNetwork := range params.ClusterUpdateParams.MachineNetworks {
			if err = network.VerifyMachineCIDR(string(machineNetwork.Cidr), common.IsSingleNodeCluster(cluster)); err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrapf(err, "Machine network CIDR '%s'", string(machineNetwork.Cidr)))
			}
		}
		cluster.MachineNetworks = params.ClusterUpdateParams.MachineNetworks
		updates["machine_network_cidr_updated_at"] = time.Now()
		updated = true
	}

	if swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation) != swag.BoolValue(cluster.VipDhcpAllocation) ||
		swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) != swag.BoolValue(cluster.UserManagedNetworking) {
		updated = true
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
	if updated {
		updates["trigger_monitor_timestamp"] = time.Now()
		return b.updateNetworkTables(db, cluster, params)
	}
	return nil
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
				err = errors.Wrapf(err, "failed to update cluster network %s of cluster %s", clusterNetwork.Cidr, *cluster.ID)
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
				err = errors.Wrapf(err, "failed to update service network %s of cluster %s", serviceNetwork.Cidr, params.ClusterID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}

	// Updating Machine CIDR can happen only in the following scenarios
	// * explicitly provided as a payload
	// * autocalculation based on the API/Ingress VIP and Host subnet
	// * reset because change of the state of UserManagedNetworking or VipDhcpAllocation
	//
	// In case of autocalculation, the new value is injected into ClusterUpdateParams therefore
	// no additional detection of this scenario is required.
	if params.ClusterUpdateParams.MachineNetworks != nil ||
		params.ClusterUpdateParams.UserManagedNetworking != cluster.UserManagedNetworking ||
		params.ClusterUpdateParams.VipDhcpAllocation != cluster.VipDhcpAllocation {
		if err = db.Where("cluster_id = ?", *cluster.ID).Delete(&models.MachineNetwork{}).Error; err != nil {
			err = errors.Wrapf(err, "failed to delete machine networks of cluster %s", *cluster.ID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
		for _, machineNetwork := range cluster.MachineNetworks {
			machineNetwork.ClusterID = *cluster.ID
			// MGMT-8853: Nothing is done when there's a conflict since there's no change to what's being inserted/updated.
			if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(machineNetwork).Error; err != nil {
				err = errors.Wrapf(err, "failed to update machine network %s of cluster %s", machineNetwork.Cidr, params.ClusterID)
				return common.NewApiError(http.StatusInternalServerError, err)
			}
		}
	}

	return nil
}

func (b *bareMetalInventory) updatePlatformParams(params installer.V2UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage) error {
	if params.ClusterUpdateParams.Platform != nil && common.PlatformTypeValue(params.ClusterUpdateParams.Platform.Type) != "" {
		updates["platform_type"] = params.ClusterUpdateParams.Platform.Type

		err := b.providerRegistry.SetPlatformUsages(
			common.PlatformTypeValue(params.ClusterUpdateParams.Platform.Type), usages, b.usageApi)
		if err != nil {
			return fmt.Errorf("failed setting platform usages, error is: %w", err)
		}
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
		if !swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) &&
			(cluster.CPUArchitecture != common.DefaultCPUArchitecture &&
				!featuresupport.IsFeatureSupported(cluster.OpenshiftVersion, models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING)) {
			err = errors.Errorf("disabling User Managed Networking is not allowed for clusters with non-x86_64 CPU architecture")
			return common.NewApiError(http.StatusBadRequest, err)
		}
		// User network mode has changed
		userManagedNetworking = swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking)
		updates["user_managed_networking"] = userManagedNetworking
		cluster.MachineNetworks = []*models.MachineNetwork{}
	}

	if userManagedNetworking {
		err, vipDhcpAllocation = setCommonUserNetworkManagedParams(db, cluster.ID, params.ClusterUpdateParams, common.IsSingleNodeCluster(cluster), updates, log)
		if err != nil {
			return err
		}
	} else {
		if params.ClusterUpdateParams.VipDhcpAllocation != nil && swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation) != vipDhcpAllocation {
			// We are removing all the previously configured VIPs because the VIP DHCP mode has changed. Doing so will prevent situations where:
			// 1. Non-DHCP mode use VIPs previously obtained via DHCP transparently.
			// 2. DHCP mode to be fed with manually provided VIPs.
			vipDhcpAllocation = swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation)
			updates["vip_dhcp_allocation"] = vipDhcpAllocation
			updates["machine_network_cidr_updated_at"] = time.Now()
			updates["api_vip"] = ""
			updates["ingress_vip"] = ""
			cluster.MachineNetworks = []*models.MachineNetwork{}
			emptyCluster := common.Cluster{Cluster: models.Cluster{ID: cluster.ID}}
			if err = network.UpdateVipsTables(db, &emptyCluster, true, true); err != nil {
				return err
			}
		}

		if vipDhcpAllocation {
			primaryMachineCIDR := ""
			if network.IsMachineCidrAvailable(cluster) {
				primaryMachineCIDR = network.GetMachineCidrById(cluster, 0)
			}
			err = b.updateDhcpNetworkParams(db, cluster.ID, updates, params, primaryMachineCIDR)
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
	if err = b.updateVips(db, params, cluster); err != nil {
		return err
	}

	b.setUsage(vipDhcpAllocation, usage.VipDhcpAllocationUsage, nil, usages)
	b.setUsage(network.CheckIfClusterIsDualStack(cluster), usage.DualStackUsage, nil, usages)
	b.setUsage(len(cluster.APIVips) > 1, usage.DualStackVipsUsage, nil, usages)
	return nil
}

func setCommonUserNetworkManagedParams(db *gorm.DB, id *strfmt.UUID, params *models.V2ClusterUpdateParams, singleNodeCluster bool, updates map[string]interface{}, log logrus.FieldLogger) (error, bool) {
	err := validateUserManagedNetworkConflicts(params, singleNodeCluster, log)
	if err != nil {
		return err, false
	}
	updates["vip_dhcp_allocation"] = false
	updates["api_vip"] = ""
	updates["ingress_vip"] = ""
	emptyCluster := common.Cluster{Cluster: models.Cluster{ID: id}}
	if err = network.UpdateVipsTables(db, &emptyCluster, true, true); err != nil {
		return err, false
	}

	return nil, false
}

func (b *bareMetalInventory) updateNtpSources(params installer.V2UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.AdditionalNtpSource != nil {
		ntpSource := swag.StringValue(params.ClusterUpdateParams.AdditionalNtpSource)
		additionalNtpSourcesDefined := ntpSource != ""

		if additionalNtpSourcesDefined && !pkgvalidations.ValidateAdditionalNTPSource(ntpSource) {
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
	if err := validations.ValidateVIPsWereNotSetUserManagedNetworking(swag.StringValue(params.APIVip), swag.StringValue(params.IngressVip), params.APIVips, params.IngressVips, swag.BoolValue(params.VipDhcpAllocation)); err != nil {
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
	b.setUsage(network.CheckIfClusterModelIsDualStack(cluster), usage.DualStackUsage, nil, usages)
	b.setUsage(len(cluster.APIVips) > 1, usage.DualStackVipsUsage, nil, usages)
	b.setDiskEncryptionUsage(cluster, cluster.DiskEncryption, usages)
	b.setUsage(cluster.Tags != "", usage.ClusterTags, nil, usages)
	b.setUsage(cluster.Hyperthreading != models.ClusterHyperthreadingNone, usage.HyperthreadingUsage,
		&map[string]interface{}{"hyperthreading_enabled": cluster.Hyperthreading}, usages)
	b.setUserManagedNetworkingAndMultiNodeUsage(swag.BoolValue(cluster.UserManagedNetworking), swag.StringValue(cluster.HighAvailabilityMode), usages)
	//write all the usages to the cluster object
	err := b.providerRegistry.SetPlatformUsages(common.PlatformTypeValue(cluster.Platform.Type), usages, b.usageApi)
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

func (b *bareMetalInventory) setUserManagedNetworkingAndMultiNodeUsage(userManagedNetworking bool, highAvailabilityMode string, usages map[string]models.Usage) {
	b.setUsage(userManagedNetworking && highAvailabilityMode == models.ClusterCreateParamsHighAvailabilityModeFull,
		usage.UserManagedNetworkingWithMultiNode, nil, usages)
}

func (b *bareMetalInventory) setOperatorsUsage(updateOLMOperators []*models.MonitoredOperator, removedOLMOperators []*models.MonitoredOperator, usages map[string]models.Usage) {
	for _, operator := range updateOLMOperators {
		b.usageApi.Add(usages, strings.ToUpper(operator.Name), nil)
	}

	for _, operator := range removedOLMOperators {
		b.usageApi.Remove(usages, strings.ToUpper(operator.Name))
	}
}

func (b *bareMetalInventory) updateClusterTags(params installer.V2UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.Tags != nil {
		tags := swag.StringValue(params.ClusterUpdateParams.Tags)
		if err := pkgvalidations.ValidateTags(tags); err != nil {
			log.WithError(err)
			return common.NewApiError(http.StatusBadRequest, err)
		}
		updates["tags"] = tags

		// if tags are defined by user, report usage of this feature
		b.setUsage(tags != "", usage.ClusterTags, nil, usages)
	}
	return nil
}

func (b *bareMetalInventory) updateClusterNetworkVMUsage(cluster *common.Cluster, updateParams *models.V2ClusterUpdateParams, usages map[string]models.Usage, log logrus.FieldLogger) {
	platform := cluster.Platform
	usageEnable := true

	if updateParams != nil && updateParams.Platform != nil {
		platform = updateParams.Platform
	}

	if platform != nil && platform.Type != nil && *platform.Type != models.PlatformTypeBaremetal && *platform.Type != models.PlatformTypeNone {
		usageEnable = false
	}

	userManagedNetwork := cluster.UserManagedNetworking != nil && *cluster.UserManagedNetworking

	if updateParams != nil && updateParams.UserManagedNetworking != nil {
		userManagedNetwork = *updateParams.UserManagedNetworking
	}

	if userManagedNetwork {
		usageEnable = false
	}

	data := make(map[string]interface{})

	if usageEnable {
		vmHosts := make([]string, 0)

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

		usageEnable = len(vmHosts) > 0
		data["VM Hosts"] = vmHosts
	}

	b.setUsage(usageEnable, usage.ClusterManagedNetworkWithVMs, &data, usages)
}

func (b *bareMetalInventory) updateClusterCPUFeatureUsage(cluster *common.Cluster, usages map[string]models.Usage) {
    switch cluster.CPUArchitecture {
    case common.ARM64CPUArchitecture:
        b.setUsage(true, usage.CPUArchitectureARM64, nil, usages)
    case common.PowerCPUArchitecture:
        b.setUsage(true, usage.CPUArchitecturePpc64le, nil, usages)
    case common.S390xCPUArchitecture:
        b.setUsage(true, usage.CPUArchitectureS390x, nil, usages)
    }
}

func (b *bareMetalInventory) updateOperatorsData(ctx context.Context, cluster *common.Cluster, params installer.V2UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger) error {
	if params.ClusterUpdateParams.OlmOperators == nil {
		return nil
	}

	updateOLMOperators, err := b.getOLMOperators(cluster, params.ClusterUpdateParams.OlmOperators, log)
	if err != nil {
		return err
	}

	err = b.operatorManagerApi.EnsureOperatorPrerequisite(cluster, cluster.OpenshiftVersion, updateOLMOperators)
	if err != nil {
		log.Error(err)
		return common.NewApiError(http.StatusBadRequest, err)
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
	//update the usage statistics about operators
	b.setOperatorsUsage(updateOLMOperators, removedOLMOperators, usages)

	//At this point, if any operators are updated, retrigger auto-assign
	//role calculation. This reset is needed because operators may affect
	//the role assignment logic.
	if len(updateOLMOperators) > 0 || len(removedOLMOperators) > 0 {
		if count, reset_err := common.ResetAutoAssignRoles(db, params.ClusterID.String()); reset_err != nil {
			log.WithError(err).Errorf("fail to reset auto-assign role in cluster %s", params.ClusterID.String())
			return common.NewApiError(http.StatusInternalServerError, reset_err)
		} else {
			log.Infof("resetting auto-assing roles on cluster %s after operator setup has changed: %d hosts affected", params.ClusterID.String(), count)
		}
	}

	return nil
}

func (b *bareMetalInventory) getOLMOperators(cluster *common.Cluster, newOperators []*models.OperatorCreateParams, log logrus.FieldLogger) ([]*models.MonitoredOperator, error) {
	monitoredOperators := make([]*models.MonitoredOperator, 0)

	for _, newOperator := range newOperators {
		if newOperator.Name == "ocs" {
			newOperator.Name = "odf"
		}
		operator, err := b.operatorManagerApi.GetOperatorByName(newOperator.Name)
		if err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}

		operator.Properties = newOperator.Properties
		monitoredOperators = append(monitoredOperators, operator)
	}

	operatorDependencies, err := b.operatorManagerApi.ResolveDependencies(cluster, monitoredOperators)
	if err != nil {
		return nil, err
	}

	for _, monitoredOperator := range operatorDependencies {
		// TODO - Need to find a better way for creating LVMO/LVMS operator on different openshift-version
		if monitoredOperator.Name == "lvm" {
			lvmsMetMinOpenshiftVersion, err := common.BaseVersionGreaterOrEqual(cluster.OpenshiftVersion, lvm.LvmsMinOpenshiftVersion)
			if err != nil {
				log.Warnf("Error parsing cluster.OpenshiftVersion: %s, setting subscription name to %s", err.Error(), lvm.LvmsSubscriptionName)
				monitoredOperator.SubscriptionName = lvm.LvmsSubscriptionName
			} else if lvmsMetMinOpenshiftVersion {
				log.Infof("LVMS minimum requirement met (OpenshiftVersion=%s), setting subscription name to %s ", cluster.OpenshiftVersion, lvm.LvmsSubscriptionName)
				monitoredOperator.SubscriptionName = lvm.LvmsSubscriptionName
			} else {
				log.Infof("LVMS minimum requirement didn't met (OpenshiftVersion=%s), setting subscription name to %s ", cluster.OpenshiftVersion, lvm.LvmoSubscriptionName)
				monitoredOperator.SubscriptionName = lvm.LvmoSubscriptionName
			}
		}

	}

	return operatorDependencies, nil
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
			if b.Config.IPv6Support || (len(intf.IPV4Addresses) > 0 && len(intf.IPV6Addresses) > 0) {
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

func (b *bareMetalInventory) listClustersInternal(ctx context.Context, params installer.V2ListClustersParams) ([]*models.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	db := b.db

	var dbClusters []*common.Cluster
	var clusters []*models.Cluster

	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !b.authzHandler.IsAdmin(ctx) {
			return nil, common.NewApiError(http.StatusForbidden, errors.New("only admin users are allowed to get unregistered clusters"))
		}
		db = db.Unscoped()
	}

	db = b.authzHandler.OwnedByUser(ctx, db, swag.StringValue(params.Owner))

	if params.OpenshiftClusterID != nil {
		db = db.Where("openshift_cluster_id = ?", *params.OpenshiftClusterID)
	}

	if len(params.AmsSubscriptionIds) > 0 {
		db = db.Where("ams_subscription_id IN (?)", params.AmsSubscriptionIds)
	}

	dbClusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading,
		common.DeleteRecordsState(swag.BoolValue(params.GetUnregisteredClusters)))
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

func (b *bareMetalInventory) GetClusterInternal(ctx context.Context, params installer.V2GetClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)

	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !b.authzHandler.IsAdmin(ctx) {
			return nil, common.NewInfraError(http.StatusForbidden,
				errors.New("only admin users are allowed to get unregistered clusters"))
		}
	}

	eager := common.UseEagerLoading
	db := b.db
	if swag.BoolValue(params.ExcludeHosts) {
		db = common.LoadClusterTablesFromDB(db, common.HostsTable)
		eager = common.SkipEagerLoading
	}
	cluster, err := common.GetClusterFromDBWhere(db, eager,
		common.DeleteRecordsState(swag.BoolValue(params.GetUnregisteredClusters)), "id = ?", params.ClusterID)
	if err != nil {
		return nil, err
	}

	cluster.HostNetworks = b.calculateHostNetworks(log, cluster)
	for _, host := range cluster.Hosts {
		b.customizeHost(&cluster.Cluster, host)
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

func (b *bareMetalInventory) generateV2NextStepRunnerCommand(ctx context.Context, params *installer.V2RegisterHostParams) (*models.HostRegistrationResponseAO1NextStepRunnerCommand, error) {

	tagOnly := containsTagOnly(params.NewHostParams.DiscoveryAgentVersion)
	if params.NewHostParams.DiscoveryAgentVersion != readConfiguredAgentImage(b.AgentDockerImg, tagOnly) {
		log := logutil.FromContext(ctx, b.log)
		log.Infof("Host %s in infra-env %s uses an outdated agent image %s, updating to %s",
			params.NewHostParams.HostID.String(), params.InfraEnvID.String(), params.NewHostParams.DiscoveryAgentVersion, b.AgentDockerImg)
	}

	config := hostcommands.NextStepRunnerConfig{
		ServiceBaseURL:       b.ServiceBaseURL,
		InfraEnvID:           params.InfraEnvID,
		HostID:               *params.NewHostParams.HostID,
		UseCustomCACert:      b.ServiceCACertPath != "",
		NextStepRunnerImage:  b.AgentDockerImg,
		SkipCertVerification: b.SkipCertVerification,
	}
	command, args, err := hostcommands.GetNextStepRunnerCommand(&config)
	return &models.HostRegistrationResponseAO1NextStepRunnerCommand{
		Command: command,
		Args:    *args,
	}, err
}

func containsTagOnly(agentVersion string) bool {
	return !strings.Contains(agentVersion, ":")
}

func readConfiguredAgentImage(fullName string, tagOnly bool) string {
	if tagOnly {
		suffix := strings.Split(fullName, ":")
		return suffix[len(suffix)-1]
	}
	return fullName
}

func returnRegisterHostTransitionError(
	defaultCode int32,
	err error) middleware.Responder {
	if isRegisterHostForbidden(err) {
		return installer.NewV2RegisterHostForbidden().WithPayload(
			&models.InfraError{
				Code:    swag.Int32(http.StatusForbidden),
				Message: swag.String(err.Error()),
			})
	}
	return common.NewApiError(defaultCode, err)
}

func isRegisterHostForbidden(err error) bool {
	if serr, ok := err.(*common.ApiErrorResponse); ok {
		return serr.StatusCode() == http.StatusForbidden
	}
	return false
}

func (b *bareMetalInventory) V2DeregisterHostInternal(ctx context.Context, params installer.V2DeregisterHostParams, interactivity Interactivity) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister host: %s infra env %s", params.HostID, params.InfraEnvID)

	h, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err = b.hostApi.UnRegisterHost(ctx, &h.Host); err != nil {
		// TODO: check error type
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// TODO: need to check that host can be deleted from the cluster
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		log.WithError(err).Warnf("Get InfraEnv %s", params.InfraEnvID.String())
		return err
	}
	eventgen.SendHostDeregisteredEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, common.StrFmtUUIDPtr(infraEnv.ClusterID),
		hostutil.GetHostnameForMsg(&h.Host))

	if h.ClusterID != nil {
		if interactivity == Interactive {
			if _, err = b.refreshClusterStatus(ctx, h.ClusterID, b.db); err != nil {
				log.WithError(err).Warnf("Failed to refresh cluster after de-registerating host <%s>", params.HostID)
			}
		}
		if err := b.clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *h.ClusterID); err != nil {
			log.WithError(err).Errorf("Failed to refresh SchedulableMastersForcedTrue while de-registering host <%s> to cluster <%s>", h.ID, h.ClusterID)
			return err
		}
	}
	return nil
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
		if err := b.hostApi.HandleMediaDisconnected(ctx, h); err != nil {
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

	case models.StepTypeTangConnectivityCheck:
		return b.hostApi.UpdateTangConnectivityReport(ctx, h, params.Reply.Error)

	case models.StepTypeDownloadBootArtifacts:
		log.Errorf("Failed to download boot artifacts to reclaim host %s, output: %s, error: %s", h.ID, params.Reply.Output, params.Reply.Error)
		return b.hostApi.HandleReclaimFailure(ctx, h)
	}
	return nil
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
		b.metricApi.DiskSyncDuration(diskPerfCheckResponse.IoSyncDuration)

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

func (b *bareMetalInventory) processUpgradeAgentResponse(ctx context.Context, h *models.Host,
	responseJSON string) error {
	log := logutil.FromContext(ctx, b.log)

	var response models.UpgradeAgentResponse
	err := json.Unmarshal([]byte(responseJSON), &response)
	if err != nil {
		log.WithError(err).Errorf(
			"failed to unmarshal upgrade agent response from host '%s'",
			h.ID.String(),
		)
		return err
	}

	switch response.Result {
	case models.UpgradeAgentResultSuccess:
		eventgen.SendUpgradeAgentFinishedEvent(
			ctx,
			b.eventsHandler,
			*h.ID,
			hostutil.GetHostnameForMsg(h),
			h.InfraEnvID,
			h.ClusterID,
			response.AgentImage,
		)
	case models.UpgradeAgentResultFailure:
		eventgen.SendUpgradeAgentFailedEvent(
			ctx,
			b.eventsHandler,
			*h.ID,
			hostutil.GetHostnameForMsg(h),
			h.InfraEnvID,
			h.ClusterID,
			response.AgentImage,
		)
	}

	return nil
}

func (b *bareMetalInventory) getInstallationDiskSpeedThresholdMs(ctx context.Context, h *models.Host) (int64, error) {
	cluster, err := common.GetClusterFromDB(b.db, *h.ClusterID, common.SkipEagerLoading)
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
	case models.StepTypeTangConnectivityCheck:
		err = b.hostApi.UpdateTangConnectivityReport(ctx, &host, stepReply)
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
	case models.StepTypeUpgradeAgent:
		err = b.processUpgradeAgentResponse(ctx, &host, stepReply)
	case models.StepTypeDownloadBootArtifacts:
		err = b.hostApi.HandleReclaimBootArtifactDownload(ctx, &host)
	case models.StepTypeVerifyVips:
		err = b.HandleVerifyVipsResponse(ctx, &host, stepReply)
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
	// Currently the assisted-service logs are full of the media disconnection errors.
	// Here we are filtering these errors and log the message once per host.
	return !(reply.ExitCode == MediaDisconnected && host.MediaStatus != nil && *host.MediaStatus == models.HostMediaStatusDisconnected)
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
	case models.StepTypeTangConnectivityCheck:
		stepReply, err = filterReply(&models.TangConnectivityResponse{}, params.Reply.Output)
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
	case models.StepTypeUpgradeAgent:
		stepReply, err = filterReply(&models.UpgradeAgentResponse{}, params.Reply.Output)
	case models.StepTypeVerifyVips:
		stepReply, err = filterReply(&models.VerifyVipsResponse{}, params.Reply.Output)
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

func (b *bareMetalInventory) setMajorityGroupForCluster(clusterID *strfmt.UUID, db *gorm.DB) error {
	return b.clusterApi.SetConnectivityMajorityGroupsForCluster(*clusterID, db)
}

func (b *bareMetalInventory) detectAndStoreCollidingIPsForCluster(clusterID *strfmt.UUID, db *gorm.DB) error {
	return b.clusterApi.DetectAndStoreCollidingIPsForCluster(*clusterID, db)
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
	log := logutil.FromContext(ctx, b.log)

	if err := b.checkFileDownloadAccess(ctx, params.FileName); err != nil {
		payload := common.GenerateInfraError(http.StatusForbidden, err)
		return installer.NewV2GetPresignedForClusterFilesForbidden().WithPayload(payload)
	}

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if !b.objectHandler.IsAwsS3() {
		return common.NewApiError(http.StatusBadRequest, errors.New("Failed to generate presigned URL: invalid backend"))
	}
	var err error
	fullFileName := fmt.Sprintf("%s/%s", params.ClusterID.String(), params.FileName)
	downloadFilename := params.FileName
	if params.FileName == manifests.ManifestFolder {
		if params.AdditionalName != nil {
			additionalName := *params.AdditionalName
			fullFileName = manifests.GetManifestObjectName(params.ClusterID, additionalName)
			downloadFilename = additionalName[strings.LastIndex(additionalName, "/")+1:]
		} else {
			err = errors.New("Additional name must be provided for 'manifests' file name, prefaced with folder name, e.g.: openshift/99-openshift-xyz.yaml")
			return common.GenerateErrorResponder(err)
		}
	}

	logsType := params.LogsType
	if params.FileName == "logs" {
		if params.HostID != nil && swag.StringValue(logsType) == "" {
			logsType = swag.String(string(models.LogsTypeHost))
		}
		fullFileName, downloadFilename, err = b.getLogFileForDownload(ctx, &params.ClusterID, params.HostID, swag.StringValue(logsType))
		if err != nil {
			return common.GenerateErrorResponder(err)
		}
	} else if err = b.checkFileForDownload(ctx, params.ClusterID.String(), params.FileName); err != nil {
		return common.GenerateErrorResponder(err)
	}

	duration, _ := time.ParseDuration("10m")
	url, err := b.objectHandler.GeneratePresignedDownloadURL(ctx, fullFileName, downloadFilename, duration)
	if err != nil {
		log.WithError(err).Errorf("failed to generate presigned URL: %s from cluster: %s", params.FileName, params.ClusterID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return installer.NewV2GetPresignedForClusterFilesOK().WithPayload(&models.PresignedURL{URL: &url})
}

func (b *bareMetalInventory) DownloadMinimalInitrd(ctx context.Context, params installer.DownloadMinimalInitrdParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		return common.GenerateErrorResponder(err)
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

	return installer.NewDownloadMinimalInitrdOK().WithPayload(io.NopCloser(bytes.NewReader(minimalInitrd)))
}

func (b *bareMetalInventory) getLogFileForDownload(ctx context.Context, clusterId *strfmt.UUID, hostId *strfmt.UUID, logsType string) (string, string, error) {
	var fileName string
	var downloadFileName string
	var hostObject *common.Host

	c, err := b.getCluster(ctx, clusterId.String(), common.UseEagerLoading, common.IncludeDeletedRecords)
	if err != nil {
		return "", "", err
	}
	b.log.Debugf("log type to download: %s", logsType)
	switch logsType {
	case string(models.LogsTypeHost):
		if hostId == nil {
			return "", "", common.NewApiError(http.StatusBadRequest, errors.Errorf("Host ID must be provided for downloading host logs"))
		}
		hostObject, err = common.GetClusterHostFromDB(b.db, clusterId.String(), hostId.String())
		if err != nil {
			return "", "", err
		}
		if time.Time(hostObject.LogsCollectedAt).Equal(time.Time{}) {
			return "", "", common.NewApiError(http.StatusConflict, errors.Errorf("Logs for host %s were not found", hostId))
		}
		role := string(hostObject.Role)
		if hostObject.Bootstrap {
			role = string(models.HostRoleBootstrap)
		}
		name := sanitize.Name(hostutil.GetHostnameForMsg(&hostObject.Host))
		downloadFileName = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), role, name)
		fileName, err = b.preparHostLogs(ctx, c, &hostObject.Host)
		if err != nil {
			return "", "", err
		}
	case string(models.LogsTypeController):
		if time.Time(c.Cluster.ControllerLogsCollectedAt).Equal(time.Time{}) {
			return "", "", common.NewApiError(http.StatusConflict, errors.Errorf("Controller Logs for cluster %s were not found", clusterId))
		}
		fileName = b.getLogsFullName(logsType, clusterId.String(), logsType)
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
	log.Infof("Checking cluster file for download: %s for cluster %s", fileName, clusterID)

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
			errMsg := fmt.Sprintf("File '%s' is accessible only for cluster owners", fileName)
			return errors.New(errMsg)
		}
	}
	return nil
}

func (b *bareMetalInventory) V2DownloadHostIgnition(ctx context.Context, params installer.V2DownloadHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	fileName, respBody, contentLength, err := b.v2DownloadHostIgnition(ctx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to download host %s ignition", params.HostID)
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(installer.NewV2DownloadHostIgnitionOK().WithPayload(respBody), fileName, contentLength, nil)
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
	password, err := io.ReadAll(r)
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
			log.Errorf("cancel installation failed to recover: %s", r)
			log.Error(string(debug.Stack()))
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
		b.customizeHost(&cluster.Cluster, h)
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf("Failed to cancel installation: error committing DB transaction (%s)", err)
		eventgen.SendCancelInstallCommitFailedEvent(ctx, b.eventsHandler, *cluster.ID)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	return cluster, nil
}

func (b *bareMetalInventory) V2ResetHost(ctx context.Context, params installer.V2ResetHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Info("Resetting host: ", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID.String())
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID.String())
		eventgen.SendHostResetFetchFailedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, hostutil.GetHostnameForMsg(&host.Host))
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if !hostutil.IsDay2Host(&host.Host) {
		log.Errorf("ResetHost for host %s is forbidden: not a Day2 hosts", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when adding hosts to an existing cluster"))
	}

	if host.ClusterID == nil {
		log.Errorf("host %s is not bound to any cluster, cannot reset host", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when host assigned to an existing cluster"))
	}

	cluster, err := common.GetClusterFromDB(b.db, *host.ClusterID, common.SkipEagerLoading)
	if err != nil {
		err = fmt.Errorf("can not find a cluster for host %s, cannot reset host", params.HostID.String())
		log.Errorln(err.Error())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.db.Transaction(func(tx *gorm.DB) error {
		if errResponse := b.hostApi.ResetHost(ctx, &host.Host, "host was reset by user", tx); errResponse != nil {
			return errResponse
		}
		b.customizeHost(&cluster.Cluster, &host.Host)
		return nil
	})

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewV2ResetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) deleteDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.dnsApi.DeleteDNSRecordSets(ctx, &cluster)
}

func (b *bareMetalInventory) validateIgnitionEndpointURL(ignitionEndpoint *models.IgnitionEndpoint, log logrus.FieldLogger) error {
	if ignitionEndpoint == nil || ignitionEndpoint.URL == nil {
		return nil
	}
	if err := pkgvalidations.ValidateHTTPFormat(*ignitionEndpoint.URL); err != nil {
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

func (b *bareMetalInventory) uploadHostLogs(ctx context.Context, host *common.Host, logsType string, upFile io.ReadCloser) error {
	log := logutil.FromContext(ctx, b.log)

	var logPrefix string
	if host.ClusterID != nil {
		logPrefix = host.ClusterID.String()
	} else {
		logPrefix = host.InfraEnvID.String()
	}
	fileName := b.getLogsFullName(logsType, logPrefix, host.ID.String())

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

func (b *bareMetalInventory) prepareClusterLogs(ctx context.Context, cluster *common.Cluster) (string, error) {
	fileName, err := b.clusterApi.PrepareClusterLogFile(ctx, cluster, b.objectHandler)
	if err != nil {
		return "", err
	}
	return fileName, nil
}

func (b *bareMetalInventory) preparHostLogs(ctx context.Context, cluster *common.Cluster, host *models.Host) (string, error) {
	fileName, err := b.clusterApi.PrepareHostLogFile(ctx, cluster, host, b.objectHandler)
	if err != nil {
		return "", err
	}
	return fileName, nil
}

func (b *bareMetalInventory) getLogsFullName(logType string, clusterId string, logId string) string {
	filename := "logs.tar.gz"
	if logType == string(models.LogsTypeNodeBoot) {
		filename = fmt.Sprintf("boot_%s", filename)
	}
	return fmt.Sprintf("%s/logs/%s/%s", clusterId, logId, filename)
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

// Updates host's approved field by a specified flag.
// Used execlusively by kube-api.
func (b *bareMetalInventory) UpdateHostApprovedInternal(ctx context.Context, infraEnvId, hostId string, approved bool) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Updating Approved to %t Host %s InfraEnv %s", approved, hostId, infraEnvId)
	dbHost, err := common.GetHostFromDB(b.db, infraEnvId, hostId)
	if err != nil {
		return err
	}
	err = b.db.Model(&common.Host{}).Where("id = ? and infra_env_id = ?", hostId, infraEnvId).Update("approved", approved).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update 'approved' in host: %s", hostId)
		return err
	}
	eventgen.SendHostApprovedUpdatedEvent(ctx, b.eventsHandler, *dbHost.ID, strfmt.UUID(infraEnvId),
		hostutil.GetHostnameForMsg(&dbHost.Host), approved)
	return nil
}

func (b *bareMetalInventory) getClusterInfraenvs(ctx context.Context, c *common.Cluster) ([]*common.InfraEnv, error) {
	// Cluster hosts usually originate from the same infraenv, keep track
	// of which ones we've already seen so we don't pull them twice
	infraenvIDSet := make(map[string]bool)

	infraenvs := make([]*common.InfraEnv, 0)
	for _, host := range c.Hosts {
		if host.InfraEnvID == "" {
			return nil, fmt.Errorf("host %s has no infra_env_id", host.ID)
		}

		if _, ok := infraenvIDSet[string(host.InfraEnvID)]; ok {
			// We already have this infraenv in the list
			continue
		}

		infraenv, err := common.GetInfraEnvFromDB(b.db, host.InfraEnvID)
		if err != nil {
			return nil, fmt.Errorf("listing cluster infraenvs failed for host %s infra_env_id %s: %w",
				host.ID, host.InfraEnvID, err)
		}

		infraenvs = append(infraenvs, infraenv)
		infraenvIDSet[string(host.InfraEnvID)] = true
	}

	return infraenvs, nil
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

// customizeHost sets the host progress and hostname; cluster may be nil
func (b *bareMetalInventory) customizeHost(cluster *models.Cluster, host *models.Host) {
	var isSno = false
	if cluster != nil {
		isSno = swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
	}
	host.ProgressStages = b.hostApi.GetStagesByRole(host, isSno)
	host.RequestedHostname = hostutil.GetHostnameForMsg(host)
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
		if err := pkgvalidations.ValidateHTTPProxyFormat(*httpProxy); err != nil {
			return errors.Errorf("Failed to validate HTTP Proxy: %s", err)
		}
	}
	if httpsProxy != nil && *httpsProxy != "" {
		if err := pkgvalidations.ValidateHTTPProxyFormat(*httpsProxy); err != nil {
			return errors.Errorf("Failed to validate HTTPS Proxy: %s", err)
		}
	}
	if noProxy != nil && *noProxy != "" {
		if err := validations.ValidateNoProxyFormat(*noProxy, swag.StringValue(ocpVersion)); err != nil {
			return err
		}
	}
	return nil
}

// validateArchitectureAndVersion validates if architecture specified inside Infraenv matches one
// specified for the cluster. For single-arch clusters the validation needs to only compare values
// of the params. For multiarch cluster we want to see if the multiarch release image contains the
// the architecture specifically requested by the InfraEnv. We don't need to explicitly validate if
// the OS image exists because if not, this will be detected by the function generating the ISO.
func validateArchitectureAndVersion(v versions.Handler, c *common.Cluster, cpuArch, ocpVersion string) error {
	// For late-binding we don't know the cluster yet
	if c == nil {
		return nil
	}
	if ocpVersion == "" {
		ocpVersion = c.OpenshiftVersion
	}
	if c.CPUArchitecture != common.MultiCPUArchitecture {
		if c.CPUArchitecture != "" && c.CPUArchitecture != cpuArch {
			return errors.Errorf("Specified CPU architecture (%s) doesn't match the cluster (%s)", cpuArch, c.CPUArchitecture)
		}
	} else {
		if err := v.ValidateReleaseImageForRHCOS(ocpVersion, cpuArch); err != nil {
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
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			c = &cluster.Cluster
		}
	}

	b.customizeHost(c, &h.Host)
	return h, nil
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

func (b *bareMetalInventory) DeregisterInfraEnv(ctx context.Context, params installer.DeregisterInfraEnvParams) middleware.Responder {
	if err := b.DeregisterInfraEnvInternal(ctx, params); err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewDeregisterInfraEnvNoContent()
}

func (b *bareMetalInventory) DeregisterInfraEnvInternal(ctx context.Context, params installer.DeregisterInfraEnvParams) error {
	log := logutil.FromContext(ctx, b.log)
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

	if _, err = common.GetInfraEnvFromDB(b.db, params.InfraEnvID); err != nil {
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

	if err = b.infraEnvApi.DeregisterInfraEnv(ctx, params.InfraEnvID); err != nil {
		log.WithError(err).Errorf("failed to deregister infraEnv %s", params.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	success = true
	return nil
}

func (b *bareMetalInventory) GetInfraEnvHostsInternal(ctx context.Context, infraEnvId strfmt.UUID) ([]*common.Host, error) {
	return common.GetInfraEnvHostsFromDB(b.db, infraEnvId)
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

	db = b.authzHandler.OwnedByUser(ctx, db, swag.StringValue(params.Owner))

	if params.ClusterID != nil {
		db = db.Where("cluster_id = ?", params.ClusterID)
	}

	dbInfraEnvs, err := common.GetInfraEnvsFromDBWhere(db)
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

	var id strfmt.UUID
	log := logutil.FromContext(ctx, b.log)

	if b.Config.InfraEnvID != "" {
		id = b.Config.InfraEnvID
		log.Debugf("Using randomly generated infra env id %s by agent installer", id)
	} else {
		id = strfmt.UUID(uuid.New().String())
	}
	url := installer.GetInfraEnvURL{InfraEnvID: id}

	params.InfraenvCreateParams.CPUArchitecture = common.NormalizeCPUArchitecture(params.InfraenvCreateParams.CPUArchitecture)

	log = log.WithField(ctxparams.ClusterId, id)
	log.Infof("Register infraenv: %s with id %s", swag.StringValue(params.InfraenvCreateParams.Name), id)

	tx := b.db.Begin()
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
			tx.Rollback()
			eventgen.SendInfraEnvRegistrationFailedEvent(ctx, b.eventsHandler, id, errString)
		}
	}()

	params = b.setDefaultRegisterInfraEnvParams(ctx, params)

	var cluster *common.Cluster
	clusterId := params.InfraenvCreateParams.ClusterID
	if clusterId != nil {
		cluster, err = common.GetClusterFromDB(b.db, *clusterId, common.SkipEagerLoading)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = errors.Errorf("Cluster ID %s does not exist", clusterId.String())
				return nil, common.NewApiError(http.StatusNotFound, err)
			}
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}

		if err = b.checkUpdateAccessToObj(ctx, cluster, "cluster", clusterId); err != nil {
			return nil, err
		}
	}

	// The OpenShift installer validation code for the additional trust bundle
	// is buggy and doesn't react well to additional newlines at the end of the
	// certs. We need to strip them out to not bother assisted users with this
	// quirk.
	params.InfraenvCreateParams.AdditionalTrustBundle = strings.TrimSpace(params.InfraenvCreateParams.AdditionalTrustBundle)

	if err = b.validateInfraEnvCreateParams(ctx, params, cluster); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	staticNetworkConfig, err := b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.InfraenvCreateParams.StaticNetworkConfig)
	if err != nil {
		return nil, err
	}

	osImage, err := b.osImages.GetOsImageOrLatest(params.InfraenvCreateParams.OpenshiftVersion, params.InfraenvCreateParams.CPUArchitecture)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	// generate key for signing rhsso image auth tokens
	imageTokenKey, err := gencrypto.HMACKey(32)
	if err != nil {
		return nil, err
	}

	if err = b.validateKernelArguments(ctx, params.InfraenvCreateParams.KernelArguments); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	var kernelArguments *string
	if len(params.InfraenvCreateParams.KernelArguments) > 0 {
		var b []byte
		b, err = json.Marshal(&params.InfraenvCreateParams.KernelArguments)
		if err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "failed to format kernel arguments as json"))
		}
		kernelArguments = swag.String(string(b))
	}

	if params.InfraenvCreateParams.IgnitionConfigOverride != "" {
		if err = validations.ValidateIgnitionImageSize(params.InfraenvCreateParams.IgnitionConfigOverride); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
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
			StaticNetworkConfig:    staticNetworkConfig,
			Type:                   common.ImageTypePtr(params.InfraenvCreateParams.ImageType),
			AdditionalNtpSources:   swag.StringValue(params.InfraenvCreateParams.AdditionalNtpSources),
			SSHAuthorizedKey:       swag.StringValue(params.InfraenvCreateParams.SSHAuthorizedKey),
			CPUArchitecture:        params.InfraenvCreateParams.CPUArchitecture,
			KernelArguments:        kernelArguments,
			AdditionalTrustBundle:  params.InfraenvCreateParams.AdditionalTrustBundle,
		},
		KubeKeyNamespace: kubeKey.Namespace,
		ImageTokenKey:    imageTokenKey,
	}

	if clusterId != nil {
		infraEnv.ClusterID = *clusterId
		// Set the feature usage for ignition config overrides since cluster id exists
		if err = b.setIgnitionConfigOverrideUsage(infraEnv.ClusterID, params.InfraenvCreateParams.IgnitionConfigOverride, "infra-env", tx); err != nil {
			// Failure to set the feature usage isn't a failure to create the infraenv so we only print the error instead of returning it
			log.WithError(err).Warnf("failed to set ignition config override usage for cluster %s", infraEnv.ClusterID)
		}

		if err = b.setStaticNetworkUsage(tx, infraEnv.ClusterID, infraEnv.StaticNetworkConfig); err != nil {
			log.WithError(err).Warnf("failed to set static network usage for cluster %s", infraEnv.ClusterID)
		}

		if err = b.setDiscoveryKernelArgumentsUsage(tx, infraEnv.ClusterID, params.InfraenvCreateParams.KernelArguments); err != nil {
			log.WithError(err).Warnf("failed to set discovery kernel arguments usage for cluster %s", infraEnv.ClusterID)
		}
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
	err = b.ValidatePullSecret(pullSecret, ocm.UserNameFromContext(ctx))
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

	if err = tx.Create(&infraEnv).Error; err != nil {
		log.WithError(err).Error("failed to create infraenv")
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = tx.Commit().Error; err != nil {
		log.WithError(err).Error("failed to commit transaction registering infraenv")
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	success = true
	if err = b.GenerateInfraEnvISOInternal(ctx, &infraEnv); err != nil {
		return nil, err
	}

	return b.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *infraEnv.ID})
}

func (b *bareMetalInventory) validateInfraEnvCreateParams(ctx context.Context, params installer.RegisterInfraEnvParams, cluster *common.Cluster) error {
	var err error

	if err = validateArchitectureAndVersion(b.versionsHandler, cluster, params.InfraenvCreateParams.CPUArchitecture, params.InfraenvCreateParams.OpenshiftVersion); err != nil {
		return err
	}

	if params.InfraenvCreateParams.Proxy != nil {
		if err = validateProxySettings(params.InfraenvCreateParams.Proxy.HTTPProxy,
			params.InfraenvCreateParams.Proxy.HTTPSProxy,
			params.InfraenvCreateParams.Proxy.NoProxy, nil); err != nil {
			return err
		}
	}

	ntpSource := swag.StringValue(params.InfraenvCreateParams.AdditionalNtpSources)
	if ntpSource != b.Config.DefaultNTPSource && !pkgvalidations.ValidateAdditionalNTPSource(ntpSource) {
		err = errors.Errorf("Invalid NTP source: %s", ntpSource)
		return err
	}

	if params.InfraenvCreateParams.SSHAuthorizedKey != nil && *params.InfraenvCreateParams.SSHAuthorizedKey != "" {
		if err = validations.ValidateSSHPublicKey(*params.InfraenvCreateParams.SSHAuthorizedKey); err != nil {
			err = errors.Errorf("SSH key is not valid")
			return err
		}
	}

	if params.InfraenvCreateParams.StaticNetworkConfig != nil {
		if err = b.staticNetworkConfig.ValidateStaticConfigParams(params.InfraenvCreateParams.StaticNetworkConfig); err != nil {
			return err
		}
	}

	if params.InfraenvCreateParams.AdditionalTrustBundle != "" {
		if err = validations.ValidatePEMCertificateBundle(params.InfraenvCreateParams.AdditionalTrustBundle); err != nil {
			return err
		}
	}

	if err = b.validateInfraEnvIgnitionParams(ctx, params.InfraenvCreateParams.IgnitionConfigOverride); err != nil {
		return err
	}

	return nil
}

func (b *bareMetalInventory) setDefaultRegisterInfraEnvParams(_ context.Context, params installer.RegisterInfraEnvParams) installer.RegisterInfraEnvParams {
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

	if params.InfraenvCreateParams.AdditionalNtpSources == nil {
		params.InfraenvCreateParams.AdditionalNtpSources = swag.String(b.Config.DefaultNTPSource)
	}

	return params
}

// Sets the feature usage for ignition config overrides for a cluster if a cluster exists
// level is one of [host, infra-env] since the ignition config can be overridden at both these
// levels
func (b *bareMetalInventory) setIgnitionConfigOverrideUsage(clusterId strfmt.UUID, ignitionConfigOverride, level string, db *gorm.DB) error {
	var cluster *common.Cluster
	cluster, err := common.GetClusterFromDBForUpdate(db, clusterId, common.SkipEagerLoading)
	if err != nil {
		return err
	}
	if usages, uerr := usage.Unmarshal(cluster.Cluster.FeatureUsage); uerr == nil {
		props := usages[usage.IgnitionConfigOverrideUsage].Data
		if props == nil {
			props = make(map[string]interface{})
		}
		// props is in the format: level=boolean
		// i.e. "infra-env"=true

		ignitionIsOverridden := false

		// Determine the overall ignition config override usage
		if ignitionConfigOverride == "" {
			props[level] = false
			// The ignition config override is empty for this level, so we need to see if
			// at least one level of ignition config override is set,
			// to determine if we keep the overall usage set to true
			for _, val := range props {
				if val.(bool) {
					ignitionIsOverridden = true
				}
			}
		} else {
			props[level] = true
			ignitionIsOverridden = true
		}

		b.setUsage(ignitionIsOverridden, usage.IgnitionConfigOverrideUsage, &props, usages)
		b.usageApi.Save(db, clusterId, usages)
	}
	return nil
}

// Sets the feature usage for static network config for a cluster
func (b *bareMetalInventory) setStaticNetworkUsage(db *gorm.DB, clusterId strfmt.UUID, staticNetworkConfig string) error {
	var err error

	cluster, err := common.GetClusterFromDBForUpdate(db, clusterId, common.SkipEagerLoading)
	if err != nil {
		return err
	}

	usages, err := usage.Unmarshal(cluster.Cluster.FeatureUsage)
	if err != nil {
		return err
	}

	isStaticNetworkUsed := staticNetworkConfig != ""

	b.setUsage(isStaticNetworkUsed, usage.StaticNetworkConfigUsage, nil, usages)
	b.usageApi.Save(db, *cluster.ID, usages)

	return nil
}

// Sets the feature usage for discovery kernel arguments
func (b *bareMetalInventory) setDiscoveryKernelArgumentsUsage(db *gorm.DB, clusterId strfmt.UUID, kargs models.KernelArguments) error {
	var err error

	cluster, err := common.GetClusterFromDBForUpdate(db, clusterId, common.SkipEagerLoading)
	if err != nil {
		return err
	}

	usages, err := usage.Unmarshal(cluster.Cluster.FeatureUsage)
	if err != nil {
		return err
	}

	isDiscoveryKernelArgsUsed := len(kargs) > 0

	b.setUsage(isDiscoveryKernelArgsUsed, usage.DiscoveryKernelArgumentsUsage, nil, usages)
	b.usageApi.Save(db, *cluster.ID, usages)

	return nil
}

func (b *bareMetalInventory) UpdateInfraEnv(ctx context.Context, params installer.UpdateInfraEnvParams) middleware.Responder {
	i, err := b.UpdateInfraEnvInternal(ctx, params, nil)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateInfraEnvCreated().WithPayload(&i.InfraEnv)
}

func (b *bareMetalInventory) UpdateInfraEnvInternal(ctx context.Context, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string) (*common.InfraEnv, error) {
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
	if internalIgnitionConfig != nil {
		log.Infof("update infraEnv %s internalIgnitionConfig: %s", params.InfraEnvID, *internalIgnitionConfig)
	}

	if pullSecretUpdated {
		params.InfraEnvUpdateParams.PullSecret = pullSecretBackup
	}

	if params, err = b.validateAndUpdateInfraEnvParams(ctx, &params); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	success := false
	tx := b.db.Begin()
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
			tx.Rollback()
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}
	}()

	if infraEnv, err = common.GetInfraEnvFromDB(tx, params.InfraEnvID); err != nil {
		log.WithError(err).Errorf("failed to get infraEnv: %s", params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	if params.InfraEnvUpdateParams.Proxy != nil {
		if err = validateProxySettings(params.InfraEnvUpdateParams.Proxy.HTTPProxy,
			params.InfraEnvUpdateParams.Proxy.HTTPSProxy,
			params.InfraEnvUpdateParams.Proxy.NoProxy, nil); err != nil {
			log.WithError(err).Errorf("Failed to validate Proxy settings")
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if err = b.validateInfraEnvIgnitionParams(ctx, params.InfraEnvUpdateParams.IgnitionConfigOverride); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if params.InfraEnvUpdateParams.StaticNetworkConfig != nil {
		if err = b.staticNetworkConfig.ValidateStaticConfigParams(params.InfraEnvUpdateParams.StaticNetworkConfig); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.InfraEnvUpdateParams.AdditionalTrustBundle != nil {
		if *params.InfraEnvUpdateParams.AdditionalTrustBundle != "" {
			if err = validations.ValidatePEMCertificateBundle(*params.InfraEnvUpdateParams.AdditionalTrustBundle); err != nil {
				return nil, common.NewApiError(http.StatusBadRequest, err)
			}
		}
	}

	if err = b.validateKernelArguments(ctx, params.InfraEnvUpdateParams.KernelArguments); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	err = b.updateInfraEnvData(ctx, infraEnv, params, internalIgnitionConfig, tx, log)
	if err != nil {
		log.WithError(err).Error("updateInfraEnvData")
		return nil, err
	}

	tx.Commit()
	success = true

	if infraEnv, err = common.GetInfraEnvFromDB(b.db, params.InfraEnvID); err != nil {
		log.WithError(err).Errorf("failed to get infraEnv %s after update", params.InfraEnvID)
		return nil, err
	}
	if infraEnv != nil {
		b.notifyEventStream(ctx, &infraEnv.InfraEnv)
	}

	var cluster *common.Cluster
	clusterId := infraEnv.ClusterID
	if clusterId != "" {
		cluster, err = common.GetClusterFromDB(b.db, clusterId, common.SkipEagerLoading)
		if err != nil {
			// We don't want to fail here if cluster is not found. It's not responsability of this place
			// to verify that, so if there is a real issue with non-existing cluster it will be detected
			// and raised by someone else.
			cluster = nil
		}
	}
	if err = validateArchitectureAndVersion(b.versionsHandler, cluster, infraEnv.CPUArchitecture, infraEnv.OpenshiftVersion); err != nil {
		return nil, err
	}

	if err = b.GenerateInfraEnvISOInternal(ctx, infraEnv); err != nil {
		return nil, err
	}

	return b.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *infraEnv.ID})
}

func (b *bareMetalInventory) updateInfraEnvData(ctx context.Context, infraEnv *common.InfraEnv, params installer.UpdateInfraEnvParams, internalIgnitionConfig *string, db *gorm.DB, log logrus.FieldLogger) error {
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
	if params.InfraEnvUpdateParams.KernelArguments != nil {
		if len(params.InfraEnvUpdateParams.KernelArguments) > 0 {
			b, err := json.Marshal(&params.InfraEnvUpdateParams.KernelArguments)
			if err != nil {
				return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "failed to format kernel arguments as json"))
			}
			updates["kernel_arguments"] = string(b)
		} else {
			updates["kernel_arguments"] = gorm.Expr("NULL")
		}
		if infraEnv.ClusterID != "" {
			if err := b.setDiscoveryKernelArgumentsUsage(db, infraEnv.ClusterID, params.InfraEnvUpdateParams.KernelArguments); err != nil {
				log.WithError(err).Warnf("failed to set discovery kernel arguments usage for cluster %s", infraEnv.ClusterID)
			}
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
		if err := validations.ValidateIgnitionImageSize(params.InfraEnvUpdateParams.IgnitionConfigOverride); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}

		updates["ignition_config_override"] = params.InfraEnvUpdateParams.IgnitionConfigOverride
		// Set the feature usage for ignition config overrides since it changed
		if err := b.setIgnitionConfigOverrideUsage(infraEnv.ClusterID, params.InfraEnvUpdateParams.IgnitionConfigOverride, "infra-env", db); err != nil {
			// Failure to set the feature usage isn't a failure to update the infraenv so we only print the error instead of returning it
			log.WithError(err).Warnf("failed to set ignition config override usage for cluster %s", infraEnv.ClusterID)
		}
	}

	if params.InfraEnvUpdateParams.ImageType != "" && params.InfraEnvUpdateParams.ImageType != common.ImageTypeValue(infraEnv.Type) {
		updates["type"] = params.InfraEnvUpdateParams.ImageType
	}

	if params.InfraEnvUpdateParams.AdditionalTrustBundle != nil && *params.InfraEnvUpdateParams.AdditionalTrustBundle != infraEnv.AdditionalTrustBundle {
		updates["additional_trust_bundle"] = params.InfraEnvUpdateParams.AdditionalTrustBundle
	}

	if params.InfraEnvUpdateParams.StaticNetworkConfig != nil {
		staticNetworkConfig, err := b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.InfraEnvUpdateParams.StaticNetworkConfig)
		if err != nil {
			return err
		}
		if staticNetworkConfig != infraEnv.StaticNetworkConfig {
			updates["static_network_config"] = staticNetworkConfig

			if err = b.setStaticNetworkUsage(db, infraEnv.ClusterID, staticNetworkConfig); err != nil {
				log.WithError(err).Warnf("failed to set static network usage for cluster %s", infraEnv.ClusterID)
			}
		}
	}

	if params.InfraEnvUpdateParams.PullSecret != "" && params.InfraEnvUpdateParams.PullSecret != infraEnv.PullSecret {
		infraEnv.PullSecret = params.InfraEnvUpdateParams.PullSecret
		updates["pull_secret"] = params.InfraEnvUpdateParams.PullSecret
		updates["pull_secret_set"] = true
	}

	if internalIgnitionConfig != nil && *internalIgnitionConfig != infraEnv.InternalIgnitionConfigOverride {
		updates["internal_ignition_config_override"] = internalIgnitionConfig
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
		if err := b.ValidatePullSecret(params.InfraEnvUpdateParams.PullSecret, ocm.UserNameFromContext(ctx)); err != nil {
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

	// The OpenShift installer validation code for the additional trust bundle
	// is buggy and doesn't react well to additional newlines at the end of the
	// certs. We need to strip them out to not bother assisted users with this
	// quirk.
	if params.InfraEnvUpdateParams.AdditionalTrustBundle != nil {
		AdditionalTrustBundleTrimmed := strings.TrimSpace(*params.InfraEnvUpdateParams.AdditionalTrustBundle)
		params.InfraEnvUpdateParams.AdditionalTrustBundle = &AdditionalTrustBundleTrimmed
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

// TODO: modify (or remove) this validation when replace and delete operations are supported
func (b *bareMetalInventory) validateKernelArguments(ctx context.Context, kernelArguments models.KernelArguments) error {
	log := logutil.FromContext(ctx, b.log)
	for _, arg := range kernelArguments {
		if arg.Operation != models.KernelArgumentOperationAppend {
			err := errors.Errorf("Only kernel argument operation %s is supported.  Got %s", models.KernelArgumentOperationAppend, arg.Operation)
			log.WithError(err).Error("validate kernel arguments")
			return err
		}
	}
	return nil
}

func (b *bareMetalInventory) updateInfraEnvNtpSources(params installer.UpdateInfraEnvParams, infraEnv *common.InfraEnv, updates map[string]interface{}, log logrus.FieldLogger) error {
	if params.InfraEnvUpdateParams.AdditionalNtpSources != nil {
		ntpSource := swag.StringValue(params.InfraEnvUpdateParams.AdditionalNtpSources)
		additionalNtpSourcesDefined := ntpSource != ""

		if additionalNtpSourcesDefined && !pkgvalidations.ValidateAdditionalNTPSource(ntpSource) {
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
			log.Errorf("RegisterHost failed to recover: %s", r)
			log.Error(string(debug.Stack()))
			tx.Rollback()
		}
	}()

	infraEnv, err := common.GetInfraEnvFromDB(tx, params.InfraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get infra env: %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}

	var cluster *common.Cluster
	var c *models.Cluster

	// The query for cluster must appear before the host query to avoid potential deadlock
	cluster, err = b.getBoundClusterForUpdate(tx, infraEnv, params.InfraEnvID, *params.NewHostParams.HostID)
	if err != nil {
		log.WithError(err).Errorf("Bound Cluster get")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	_, err = common.GetHostFromDB(transaction.AddForUpdateQueryOption(tx), params.InfraEnvID.String(), params.NewHostParams.HostID.String())
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
		ID:                       params.NewHostParams.HostID,
		Href:                     swag.String(url.String()),
		Kind:                     kind,
		RegisteredAt:             strfmt.DateTime(time.Now()),
		CheckedInAt:              strfmt.DateTime(time.Now()),
		DiscoveryAgentVersion:    params.NewHostParams.DiscoveryAgentVersion,
		UserName:                 ocm.UserNameFromContext(ctx),
		Role:                     defaultRole,
		SuggestedRole:            defaultRole,
		InfraEnvID:               *infraEnv.ID,
		IgnitionEndpointTokenSet: false,
	}

	if cluster != nil {
		if newRecord {
			if err = b.clusterApi.AcceptRegistration(cluster); err != nil {
				log.WithError(err).Errorf("failed to register host <%s> to infra-env %s due to: %s",
					params.NewHostParams.HostID, params.InfraEnvID.String(), err.Error())
				eventgen.SendHostRegistrationFailedEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID, params.InfraEnvID, cluster.ID, err.Error())

				return common.NewApiError(http.StatusConflict, err)
			}
		}

		if common.IsDay2Cluster(cluster) {
			host.Kind = swag.String(models.HostKindAddToExistingClusterHost)
			host.Role = models.HostRoleWorker
			host.MachineConfigPoolName = string(models.HostRoleWorker)
		} else if common.IsSingleNodeCluster(cluster) {
			// The question of whether the host's cluster is single node or not only matters for a Day 1 installation.
			host.Role = models.HostRoleMaster
			host.Bootstrap = true
		}

		host.ClusterID = cluster.ID
		c = &cluster.Cluster
	}

	//day2 host is always a worker
	if hostutil.IsDay2Host(host) {
		host.Role = models.HostRoleWorker
		host.MachineConfigPoolName = string(models.HostRoleWorker)
	}

	host.SuggestedRole = host.Role

	if err = b.hostApi.RegisterHost(ctx, host, tx); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> infra-env <%s>",
			params.NewHostParams.HostID.String(), params.InfraEnvID.String())
		uerr := errors.Wrap(err, fmt.Sprintf("Failed to register host %s", hostutil.GetHostnameForMsg(host)))

		eventgen.SendHostRegistrationFailedEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID, params.InfraEnvID, host.ClusterID, uerr.Error())
		return returnRegisterHostTransitionError(http.StatusBadRequest, err)
	}

	b.customizeHost(c, host)

	eventgen.SendHostRegistrationSucceededEvent(ctx, b.eventsHandler, *params.NewHostParams.HostID,
		params.InfraEnvID, host.ClusterID, hostutil.GetHostnameForMsg(host))

	nextStepRunnerCommand, err := b.generateV2NextStepRunnerCommand(ctx, &params)
	if err != nil {
		log.WithError(err).Errorf("Fail to create nextStepRunnerCommand")
		return common.GenerateErrorResponder(err)
	}

	hostRegistration := models.HostRegistrationResponse{
		Host:                  *host,
		NextStepRunnerCommand: nextStepRunnerCommand,
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewV2RegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// Create AgentCR if needed, after commit in DB as KubeKey will be updated.
	if err := b.crdUtils.CreateAgentCR(ctx, log, params.NewHostParams.HostID.String(), infraEnv, cluster); err != nil {
		log.WithError(err).Errorf("Fail to create Agent CR, deleting host. Namespace: %s, InfraEnv: %s, HostID: %s", infraEnv.KubeKeyNamespace, swag.StringValue(infraEnv.Name), params.NewHostParams.HostID.String())
		if err2 := b.hostApi.UnRegisterHost(ctx, host); err2 != nil {
			return installer.NewV2RegisterHostInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
		return installer.NewV2RegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if host.ClusterID != nil {
		if err := b.clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *host.ClusterID); err != nil {
			log.WithError(err).Errorf("Failed to refresh SchedulableMastersForcedTrue while registering host <%s> to cluster <%s>", host.ID, host.ClusterID)
			return installer.NewV2RegisterHostInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
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

	respBytes, err := io.ReadAll(respBody)
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
			log.Errorf("get next steps failed to recover: %s", r)
			log.Error(string(debug.Stack()))
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewV2UpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	//TODO check the error type
	host, err := common.GetHostFromDB(tx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host: %s", params.HostID)
		return installer.NewV2GetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	updates := make(map[string]interface{})
	updates["checked_in_at"] = time.Now()
	if swag.Int64Value(params.Timestamp) != 0 {
		updates["timestamp"] = swag.Int64Value(params.Timestamp)
	}
	if params.DiscoveryAgentVersion != nil {
		updates["discovery_agent_version"] = *params.DiscoveryAgentVersion
	}
	if err = tx.Model(&host).Updates(updates).Error; err != nil {
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

	if host.MediaStatus != nil && *host.MediaStatus == models.HostMediaStatusDisconnected {
		err = b.hostApi.UpdateMediaConnected(ctx, &host.Host)

		if err != nil {
			log.WithError(err).Errorf("Failed update media status of host <%s> infra-env <%s> to connected", params.HostID, params.InfraEnvID)
			return installer.NewV2PostStepReplyInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
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

	b.customizeHost(c, &host.Host)

	// Clear this field as it is not needed to be sent via API
	host.FreeAddresses = ""
	return installer.NewV2GetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) V2UpdateHostInstallProgress(ctx context.Context, params installer.V2UpdateHostInstallProgressParams) middleware.Responder {
	err := b.V2UpdateHostInstallProgressInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewV2UpdateHostInstallProgressOK()
}
func (b *bareMetalInventory) V2UpdateHostInstallProgressInternal(ctx context.Context, params installer.V2UpdateHostInstallProgressParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Update host %s install progress", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host %s", params.HostID)
		return err
	}

	if host.ClusterID == nil {
		err = fmt.Errorf("host %s is not bound to any cluster, cannot update progress", params.HostID)
		log.WithError(err).Error()
		return common.NewApiError(http.StatusBadRequest, err)
	}

	stageChanged := params.HostProgress.CurrentStage != host.Progress.CurrentStage

	// Adding a transaction will require to update all lower layer to work with tx instead of db.
	if stageChanged || params.HostProgress.ProgressInfo != host.Progress.ProgressInfo {
		if err := b.hostApi.UpdateInstallProgress(ctx, &host.Host, params.HostProgress); err != nil {
			log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
			return err
		}

		event := fmt.Sprintf("reached installation stage %s", params.HostProgress.CurrentStage)
		if params.HostProgress.ProgressInfo != "" {
			event += fmt.Sprintf(": %s", params.HostProgress.ProgressInfo)
		}

		log.Info(fmt.Sprintf("Host %s in cluster %s: %s", host.ID, host.ClusterID, event))
		eventgen.SendHostInstallProgressUpdatedEvent(ctx, b.eventsHandler, *host.ID, host.InfraEnvID, host.ClusterID, hostutil.GetHostnameForMsg(&host.Host), event)
		if stageChanged {
			if err := b.clusterApi.UpdateInstallProgress(ctx, *host.ClusterID); err != nil {
				log.WithError(err).Errorf("failed to update cluster %s progress", host.ClusterID)
				return err
			}
		}
	}

	return nil
}

func (b *bareMetalInventory) BindHost(ctx context.Context, params installer.BindHostParams) middleware.Responder {
	h, err := b.BindHostInternal(ctx, params)
	if err != nil {
		eventgen.SendHostBindFailedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, params.BindHostParams.ClusterID, err.Error())
		return common.GenerateErrorResponder(err)
	}
	eventgen.SendHostBindSucceededEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, params.BindHostParams.ClusterID, hostutil.GetHostnameForMsg(&h.Host))
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
	if err = b.checkUpdateAccessToObj(ctx, cluster, "cluster", params.BindHostParams.ClusterID); err != nil {
		return nil, err
	}
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infra env %s", params.InfraEnvID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if cluster.CPUArchitecture != "" && cluster.CPUArchitecture != common.MultiCPUArchitecture && cluster.CPUArchitecture != infraEnv.CPUArchitecture {
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

	if err = b.clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *cluster.ID); err != nil {
		log.WithError(err).Errorf("Failed to refresh SchedulableMastersForcedTrue while binding host <%s> to cluster <%s>", host.ID, host.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	host, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	return host, nil
}

func (b *bareMetalInventory) UnbindHostInternal(ctx context.Context, params installer.UnbindHostParams, reclaimHost bool, interactivity Interactivity) (*common.Host, error) {
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

	if err = b.hostApi.UnbindHost(ctx, &host.Host, b.db, reclaimHost); err != nil {
		log.WithError(err).Errorf("Failed to unbind host <%s>", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if interactivity == Interactive {
		if _, err = b.refreshClusterStatus(ctx, host.ClusterID, b.db); err != nil {
			log.WithError(err).Warnf("Failed to refresh cluster after unbind of host <%s>", params.HostID)
		}
	}

	if err = b.clusterApi.RefreshSchedulableMastersForcedTrue(ctx, *host.ClusterID); err != nil {
		log.WithError(err).Errorf("Failed to refresh SchedulableMastersForcedTrue while unbinding host <%s> to cluster <%s>", host.ID, host.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	host, err = common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}
	return host, nil
}

func (b *bareMetalInventory) UnbindHost(ctx context.Context, params installer.UnbindHostParams) middleware.Responder {
	h, err := b.UnbindHostInternal(ctx, params, false, Interactive)
	if err != nil {
		eventgen.SendHostUnbindFailedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, err.Error())
		return common.GenerateErrorResponder(err)
	}
	eventgen.SendHostUnbindSucceededEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID, hostutil.GetHostnameForMsg(&h.Host))
	return installer.NewUnbindHostOK().WithPayload(&h.Host)
}

func (b *bareMetalInventory) V2ListHosts(ctx context.Context, params installer.V2ListHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	// Check that the InfraEnv exists in DB before searching for hosts bound to it.
	_, err := b.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: params.InfraEnvID})
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	hosts, err := common.GetInfraEnvHostsFromDB(b.db, params.InfraEnvID)
	if err != nil {
		log.WithError(err).Errorf("failed to get list of hosts for infra-env %s", params.InfraEnvID)
		return installer.NewV2ListHostsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, h := range hosts {
		b.customizeHost(nil, &h.Host)
		// Clear this field as it is not needed to be sent via API
		h.FreeAddresses = ""
	}

	return installer.NewV2ListHostsOK().WithPayload(common.ToModelsHosts(hosts))
}

func (b *bareMetalInventory) V2DeregisterHost(ctx context.Context, params installer.V2DeregisterHostParams) middleware.Responder {
	if err := b.V2DeregisterHostInternal(ctx, params, Interactive); err != nil {
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

	err := pkgvalidations.ValidateInstallerArgs(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	h, err := common.GetHostFromDB(b.db, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	if err = b.checkUpdateAccessToObj(ctx, h, "host", &params.HostID); err != nil {
		return nil, err
	}

	argsBytes, err := json.Marshal(params.InstallerArgsParams.Args)
	if err != nil {
		return nil, err
	}

	err = b.db.Model(&common.Host{}).Where("id = ? and infra_env_id = ?", params.HostID, params.InfraEnvID).Update("installer_args", string(argsBytes)).Error
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

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("UpdateHostIgnition failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("UpdateHostIgnition failed")
			tx.Rollback()
		}
	}()

	h, err := common.GetHostFromDB(tx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		return nil, err
	}

	if err = b.checkUpdateAccessToObj(ctx, h, "host", &params.HostID); err != nil {
		return nil, err
	}

	if params.HostIgnitionParams.Config != "" {
		_, err = ignition.ParseToLatest([]byte(params.HostIgnitionParams.Config))
		if err != nil {
			log.WithError(err).Errorf("Failed to parse host ignition config patch %s", params.HostIgnitionParams)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	} else {
		log.Infof("Removing custom ignition override from host %s in infra-env %s", params.HostID, params.InfraEnvID)
	}

	err = tx.Model(&common.Host{}).Where("id = ? and infra_env_id = ?", params.HostID, params.InfraEnvID).Update("ignition_config_overrides", params.HostIgnitionParams.Config).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	eventgen.SendHostDiscoveryIgnitionConfigAppliedEvent(ctx, b.eventsHandler, params.HostID, params.InfraEnvID,
		hostutil.GetHostnameForMsg(&h.Host))
	log.Infof("Custom discovery ignition config was applied to host %s in infra-env %s", params.HostID, params.InfraEnvID)
	h, err = common.GetHostFromDB(tx, params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	// Set the feature usage for ignition config overrides
	if err = b.setIgnitionConfigOverrideUsage(*h.ClusterID, params.HostIgnitionParams.Config, "host", tx); err != nil {
		// Failure to set the feature usage isn't a failure to update the host ignition so we only print the error instead of returning it
		log.WithError(err).Warnf("failed to set ignition config override usage for cluster %s", h.ClusterID)
	}

	tx.Commit()
	txSuccess = true
	return &h.Host, nil
}

func (b *bareMetalInventory) V2DownloadInfraEnvFiles(ctx context.Context, params installer.V2DownloadInfraEnvFilesParams) middleware.Responder {
	if params.IpxeScriptType != nil && params.FileName != "ipxe-script" {
		return common.NewApiError(http.StatusBadRequest, errors.New(`"ipxe_script_type"" can be set only for "ipxe-script"`))
	}
	infraEnv, err := common.GetInfraEnvFromDB(b.db, params.InfraEnvID)
	if err != nil {
		b.log.WithError(err).Errorf("Failed to get infra env %s", params.InfraEnvID)
		return common.GenerateErrorResponder(err)
	}

	var content, filename string
	switch params.FileName {
	case "discovery.ign":
		discoveryIsoType := swag.StringValue(params.DiscoveryIsoType)
		content, err = b.IgnitionBuilder.FormatDiscoveryIgnitionFile(ctx, infraEnv, b.IgnitionConfig, false, b.authHandler.AuthType(), discoveryIsoType)
		if err != nil {
			b.log.WithError(err).Error("Failed to format ignition config")
			return common.GenerateErrorResponder(err)
		}
		filename = params.FileName
	case "ipxe-script":
		content, err = b.infraEnvIPXEScript(ctx, infraEnv, params.Mac, params.IpxeScriptType)
		if err != nil {
			b.log.WithError(err).Error("Failed to create ipxe script")
			return common.GenerateErrorResponder(err)
		}
		filename = fmt.Sprintf("%s-%s", params.InfraEnvID, params.FileName)
	case "static-network-config":
		if infraEnv.StaticNetworkConfig != "" {
			netFiles, err := b.staticNetworkConfig.GenerateStaticNetworkConfigData(ctx, infraEnv.StaticNetworkConfig)
			if err != nil {
				b.log.WithError(err).Errorf("Failed to create static network config data")
				return common.GenerateErrorResponder(err)
			}
			buffer, err := staticnetworkconfig.GenerateStaticNetworkConfigArchive(netFiles)
			if err != nil {
				b.log.WithError(err).Errorf("Failed to create static network config archive")
				return common.GenerateErrorResponder(err)
			}
			content = buffer.String()
			filename = fmt.Sprintf("%s-%s.tar", params.InfraEnvID, params.FileName)
		}
	default:
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("unknown file type for download: %s", params.FileName))
	}

	return filemiddleware.NewResponder(
		installer.NewV2DownloadInfraEnvFilesOK().WithPayload(io.NopCloser(strings.NewReader(content))),
		filename,
		int64(len(content)),
		infraEnv.UpdatedAt,
	)
}

func (b *bareMetalInventory) V2DownloadClusterCredentials(ctx context.Context, params installer.V2DownloadClusterCredentialsParams) middleware.Responder {
	fileName := params.FileName
	respBody, contentLength, err := b.V2DownloadClusterCredentialsInternal(ctx, params)

	// Kubeconfig-noingress has been created during the installation, but it does not have the ingress CA.
	// At the finalizing phase, we create the kubeconfig file and add the ingress CA.
	// An ingress CA isn't required for normal login but for oauth login which isn't a common use case.
	// Here we fallback to the kubeconfig-noingress for the kubeconfig filename.
	if err != nil && params.FileName == constants.Kubeconfig {
		fileName = constants.KubeconfigNoIngress
		respBody, contentLength, err = b.v2DownloadClusterFilesInternal(ctx, constants.KubeconfigNoIngress, params.ClusterID.String())
	}

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(installer.NewV2DownloadClusterCredentialsOK().WithPayload(respBody), fileName, contentLength, nil)
}

func (b *bareMetalInventory) V2DownloadClusterFiles(ctx context.Context, params installer.V2DownloadClusterFilesParams) middleware.Responder {
	respBody, contentLength, err := b.V2DownloadClusterFilesInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return filemiddleware.NewResponder(installer.NewV2DownloadClusterFilesOK().WithPayload(respBody), params.FileName, contentLength, nil)
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

// Get the cluster id from the host.  The host is not locked for update during this query
func (b *bareMetalInventory) getClusterIDFromHost(db *gorm.DB, hostID, infraEnvID strfmt.UUID) (strfmt.UUID, error) {
	h, err := common.GetHostFromDB(db.Select("cluster_id"), infraEnvID.String(), hostID.String())
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", common.NewApiError(http.StatusInternalServerError,
			errors.Wrapf(err, "failed to get cluster id for host %s infra-env %s", hostID.String(), infraEnvID.String()))
	}
	if h == nil || h.ClusterID == nil {
		return "", nil
	}
	return *h.ClusterID, nil
}

func (b *bareMetalInventory) V2UpdateHostInternal(ctx context.Context, params installer.V2UpdateHostParams, interactivity Interactivity) (*common.Host, error) {
	log := logutil.FromContext(ctx, b.log)
	var c *models.Cluster
	var cluster *common.Cluster
	var usages usage.FeatureUsage = make(usage.FeatureUsage)
	var clusterID strfmt.UUID
	var err error

	txSuccess := false
	tx := b.db.Begin()

	defer func() {
		if !txSuccess {
			log.Error("update host failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Errorf("update host failed to recover: %s", r)
			log.Error(string(debug.Stack()))
			tx.Rollback()
		}
	}()

	// Get the cluster id from the host.  The host is not locked for update during this query
	if clusterID, err = b.getClusterIDFromHost(tx, params.HostID, params.InfraEnvID); err != nil {
		return nil, err
	}
	if clusterID != "" {

		// Get first the bound cluster to verify that the cluster is locked before the host
		// to avoid deadlocks
		cluster, err = common.GetClusterFromDBForUpdate(tx, clusterID, common.SkipEagerLoading)
		if err != nil {
			err = fmt.Errorf("can not find a cluster for host %s", params.HostID.String())
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	host, err := common.GetHostFromDB(transaction.AddForUpdateQueryOption(tx), params.InfraEnvID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host <%s>, infra env <%s>", params.HostID, params.InfraEnvID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	err = b.updateHostRole(ctx, host, params.HostUpdateParams.HostRole, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostName(ctx, host, params.HostUpdateParams.HostName, usages, tx)
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
	err = b.updateNodeLabels(ctx, host, params.HostUpdateParams.NodeLabels, tx)
	if err != nil {
		return nil, err
	}
	err = b.updateHostSkipFormattingDisks(ctx, host, params.HostUpdateParams.DisksSkipFormatting, tx)
	if err != nil {
		return nil, err
	}

	//get bound cluster
	if cluster != nil {
		c = &cluster.Cluster

		//in case a cluster is bound, report the host related usages
		//make sure that host information is added to the existing data
		//and not replacing it
		if funk.NotEmpty(usages) {
			if clusterusage, e := usage.Unmarshal(cluster.FeatureUsage); e == nil {
				for k, v := range usages {
					clusterusage[k] = v
				}
				b.usageApi.Save(tx, *cluster.ID, clusterusage)
			}
		}
	}

	if interactivity == Interactive {
		err = b.refreshAfterUpdate(ctx, cluster, host, tx)
		if err != nil {
			log.WithError(err).Errorf("Failed to refresh host %s, infra env %s during update", host.ID, host.InfraEnvID)
			return nil, err
		}
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

	b.customizeHost(c, &host.Host)

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

func (b *bareMetalInventory) updateHostName(ctx context.Context, host *common.Host, hostname *string, usages usage.FeatureUsage, db *gorm.DB) error {
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
	b.setUsage(true, usage.RequestedHostnameUsage, &map[string]interface{}{"host_count": 1.0}, usages)
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
		log.WithError(err).Errorf("failed to set installation disk path <%s> host <%s> infra env <%s>",
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

func (b *bareMetalInventory) updateNodeLabels(ctx context.Context, host *common.Host, nodeLabelsList []*models.NodeLabelParams, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)
	if nodeLabelsList == nil {
		log.Infof("No request for node labels update for host %s", host.ID)
		return nil
	}

	nodeLabelsMap := make(map[string]string)
	for _, nl := range nodeLabelsList {
		nodeLabelsMap[*nl.Key] = *nl.Value
	}

	errs := validation.ValidateLabels(nodeLabelsMap, field.NewPath("node_labels"))
	if len(errs) != 0 {
		return errors.Errorf("%s", errs.ToAggregate().Error())
	}

	nodeLabelsStr, err := common.MarshalNodeLabels(nodeLabelsList)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal node labels for host %s", host.ID)
	}

	err = b.hostApi.UpdateNodeLabels(ctx, &host.Host, nodeLabelsStr, db)
	if err != nil {
		log.WithError(err).Errorf("failed to set labels <%s> host <%s>, infra env <%s>",
			nodeLabelsStr, host.ID, host.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (b *bareMetalInventory) updateHostSkipFormattingDisks(ctx context.Context, host *common.Host, diskSkipFormattingParams []*models.DiskSkipFormattingParams, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)

	if len(diskSkipFormattingParams) == 0 {
		return nil
	}

	// Get a list of disks
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("inventory unmarshal failed: %w", err))
	}
	inventoryDiskIdentifiers := make([]string, 0, len(inventory.Disks))
	for _, disk := range inventory.Disks {
		inventoryDiskIdentifiers = append(inventoryDiskIdentifiers, common.GetDeviceIdentifier(disk))
	}

	// Start with the original list of currently skipped disks
	newDiskSkipFormattingIdentifiers := common.GetSkippedFormattingDiskIdentifiers(&host.Host)

	// Apply user modifications to this skipped disks list
	for _, diskSkipFormattingParam := range diskSkipFormattingParams {
		if diskSkipFormattingParam.DiskID == nil || diskSkipFormattingParam.SkipFormatting == nil {
			return common.NewApiError(http.StatusBadRequest, errors.New("Missing required disk formatting param fields"))
		}
		paramDiskID, paramSkipFormatting := *diskSkipFormattingParam.DiskID, *diskSkipFormattingParam.SkipFormatting

		if funk.Contains(newDiskSkipFormattingIdentifiers, paramDiskID) {
			if paramSkipFormatting {
				log.Infof("Disk %s is already in the skip list %s", paramDiskID, host.SkipFormattingDisks)
			} else {
				log.Infof("Removing disk %s from the skip list %s", paramDiskID, host.SkipFormattingDisks)
				newDiskSkipFormattingIdentifiers = funk.FilterString(newDiskSkipFormattingIdentifiers, func(diskIdentifier string) bool {
					return diskIdentifier != paramDiskID
				})
			}
		} else {
			if paramSkipFormatting {
				if !funk.ContainsString(inventoryDiskIdentifiers, paramDiskID) {
					return common.NewApiError(http.StatusBadRequest, fmt.Errorf(
						"Disk identifier %s doesn't match any disk in the inventory, it cannot be skipped. Inventory disk identifiers are: %s",
						paramDiskID, strings.Join(inventoryDiskIdentifiers, ", ")))
				}

				log.Infof("Adding disk %s to the skip list %s", paramDiskID, host.SkipFormattingDisks)
				newDiskSkipFormattingIdentifiers = append(newDiskSkipFormattingIdentifiers, paramDiskID)
			} else {
				log.Infof("Disk %s is already not in the skip list %s", paramDiskID, host.SkipFormattingDisks)
			}
		}
	}

	// Replace the original list in the database with the user-modified list
	skipDiskFormattingIdentifiersJoined := strings.Join(newDiskSkipFormattingIdentifiers, ",")
	err := b.hostApi.UpdateNodeSkipDiskFormatting(ctx, &host.Host, skipDiskFormattingIdentifiersJoined, db)
	if err != nil {
		log.WithError(err).Errorf("failed to set skip disk formatting <%s> host <%s>, infra env <%s>",
			skipDiskFormattingIdentifiersJoined, host.ID, host.InfraEnvID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return nil
}

func (b *bareMetalInventory) refreshAfterUpdate(ctx context.Context, cluster *common.Cluster, host *common.Host, db *gorm.DB) error {
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
		return err
	}

	if host.ClusterID != nil {
		_, err = b.clusterApi.RefreshStatus(ctx, cluster, db)
		if err != nil {
			log.WithError(err).Errorf("Failed to refresh cluster %s, infra env %s during host update", host.ID, host.InfraEnvID)
			return err
		}
	}
	return err
}

func (b *bareMetalInventory) getBoundClusterForUpdate(db *gorm.DB, infraEnv *common.InfraEnv, infraEnvID, hostID strfmt.UUID) (*common.Cluster, error) {
	var err error
	var clusterID strfmt.UUID
	if infraEnv.ClusterID != "" {
		clusterID = infraEnv.ClusterID
	} else {

		// This query is not locked for update.  Therefore, it will not be part of deadlock
		if clusterID, err = b.getClusterIDFromHost(db, hostID, infraEnvID); err != nil {
			return nil, err
		}
	}

	if clusterID != "" {
		cluster, err := common.GetClusterFromDBForUpdate(db, clusterID, common.SkipEagerLoading)
		if err != nil {
			return nil, err
		}
		return cluster, nil
	}
	return nil, nil
}

// Checks whether the specified obj can be updated by the user.
// If not allowed, checks whether it can be read by the user.
// This is required in order to return an appropriate error for
// users who are only allowed to read.
func (b *bareMetalInventory) checkUpdateAccessToObj(ctx context.Context, obj interface{}, objType string, objId *strfmt.UUID) error {
	canWrite, err1 := b.authzHandler.HasAccessTo(ctx, obj, auth.UpdateAction)
	if !canWrite {
		canRead, err2 := b.authzHandler.HasAccessTo(ctx, obj, auth.ReadAction)
		if !canRead {
			msg := "Object Not Found"
			b.log.WithError(err2).Error(msg)
			return common.NewApiError(http.StatusNotFound, errors.New(msg))
		}
		msg := fmt.Sprintf("Unauthorized to update %s with ID %s", objType, objId)
		b.log.WithError(err1).Error(msg)
		return common.NewApiError(http.StatusForbidden, errors.New(msg))
	}
	return nil
}

func (b *bareMetalInventory) ListClusterHosts(ctx context.Context, params installer.ListClusterHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	db := b.db
	if params.Role != nil {
		db = db.Where("role = ?", swag.StringValue(params.Role))
	}
	if params.Status != nil {
		db = db.Where("status = ?", swag.StringValue(params.Status))
	}
	var hostList models.HostList
	err := db.Find(&hostList, "cluster_id = ?", params.ClusterID.String()).Error
	if err != nil {
		log.WithError(err).Errorf("Failed to get cluster %s hosts", params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}
	withInventory := swag.BoolValue(params.WithInventory)
	withConnectivity := swag.BoolValue(params.WithConnectivity)
	for _, h := range hostList {
		h.FreeAddresses = ""
		if !withInventory {
			h.Inventory = ""
		}
		if !withConnectivity {
			h.Connectivity = ""
		}
	}
	return installer.NewListClusterHostsOK().WithPayload(hostList)
}

func (b *bareMetalInventory) GetKnownHostApprovedCounts(clusterID strfmt.UUID) (registered, approved int, err error) {
	return b.hostApi.GetKnownHostApprovedCounts(clusterID)
}

func (b *bareMetalInventory) HostWithCollectedLogsExists(clusterId strfmt.UUID) (bool, error) {
	return b.hostApi.HostWithCollectedLogsExists(clusterId)
}

func (b *bareMetalInventory) GetKnownApprovedHosts(clusterId strfmt.UUID) ([]*common.Host, error) {
	return b.hostApi.GetKnownApprovedHosts(clusterId)
}

// In case cpu architecture is not x86_64 and platform is baremetal, we should extract openshift-baremetal-installer
// from x86_64 release image as there is no x86_64 openshift-baremetal-installer executable in arm image.
// This flow does not affect the multiarch release images and is meant purely for using arm64 release image with the x86 hub.
// Implementation of handling the multiarch images is done directly in the `oc` binary and relies on the fact that `oc adm release extract`
// will automatically use the image matching the Hub's architecture.
func isBaremetalBinaryFromAnotherReleaseImageRequired(cpuArchitecture, version string, platform *models.PlatformType) bool {
	return cpuArchitecture != common.MultiCPUArchitecture &&
		cpuArchitecture != common.NormalizeCPUArchitecture(runtime.GOARCH) &&
		common.PlatformTypeValue(platform) == models.PlatformTypeBaremetal &&
		featuresupport.IsFeatureSupported(version,
			models.FeatureSupportLevelFeaturesItems0FeatureIDARM64ARCHITECTUREWITHCLUSTERMANAGEDNETWORKING)
}

// updateMonitoredOperators checks the content of the installer configuration and updates the list
// of monitored operators accordingly. For example, if the installer configuration uses the
// capabilities mechanism to disable the console then the console operator is removed from the list
// of monitored operators.
func (b *bareMetalInventory) updateMonitoredOperators(tx *gorm.DB, cluster *common.Cluster) error {
	// Get the complete installer configuration, including the overrides:
	installConfigData, err := b.installConfigBuilder.GetInstallConfig(cluster, nil, "")
	if err != nil {
		return err
	}
	var installConfig installcfgdata.InstallerConfigBaremetal
	err = yaml.Unmarshal(installConfigData, &installConfig)
	if err != nil {
		return err
	}

	// Since version 4.12 it is possible to disable the console via the capabilities section of
	// the installer configuration. The way to do it is to set the base capability set to `None`
	// and then explicitly list all the enabled capabilities.
	consoleEnabled := true
	logFields := logrus.Fields{
		"cluster_id":      cluster.ID,
		"cluster_version": cluster.OpenshiftVersion,
		"minimal_version": minimalOpenShiftVersionForConsoleCapability,
	}
	consoleCapabilitySupported, err := common.BaseVersionGreaterOrEqual(
		cluster.OpenshiftVersion,
		minimalOpenShiftVersionForConsoleCapability,
	)
	if err != nil {
		return err
	}
	if consoleCapabilitySupported {
		capabilities := installConfig.Capabilities
		if capabilities != nil {
			logFields["baseline_capability_set"] = capabilities.BaselineCapabilitySet
			logFields["additional_enabled_capabilities"] = capabilities.AdditionalEnabledCapabilities
			if capabilities.BaselineCapabilitySet == "None" {
				consoleEnabled = false
				for _, capability := range capabilities.AdditionalEnabledCapabilities {
					if capability == "Console" {
						consoleEnabled = true
						break
					}
				}
			}
		}
		if consoleEnabled {
			b.log.WithFields(logFields).Info(
				"Console is enabled because the cluster version supports the " +
					"capability and it has been explicitly enabled by " +
					"the user",
			)
		} else {
			b.log.WithFields(logFields).Info(
				"Console is disabled because the cluster version supports the " +
					"capability and it hasn't been explicitly enabled by " +
					"the user",
			)
		}
	} else {
		consoleEnabled = true
		b.log.WithFields(logFields).Info(
			"Console is enabled because the cluster version doesn't support " +
				"the capability",
		)
	}

	// Add or remove the console operator to the list of monitored operators:
	consoleOperator := operators.OperatorConsole
	consoleOperator.ClusterID = *cluster.ID
	if consoleEnabled {
		b.log.WithFields(logFields).Info(
			"Adding the console to the set of monitored operators",
		)
		err = tx.FirstOrCreate(&consoleOperator).Error
	} else {
		b.log.WithFields(logFields).Info(
			"Removing the console from the set of monitored operators",
		)
		err = tx.Delete(&consoleOperator).Error
	}
	return err
}

func (b *bareMetalInventory) notifyEventStream(ctx context.Context, infraEnv *models.InfraEnv) {
	if b.stream == nil || infraEnv == nil {
		return
	}
	key := infraEnv.ClusterID.String()
	err := b.stream.Write(ctx, "InfraEnv", []byte(key), infraEnv)
	if err != nil {
		b.log.WithError(err).WithFields(logrus.Fields{
			"infra_env_id": infraEnv.ID,
			"cluster_id":   infraEnv.ClusterID,
		}).Warn("failed to stream event for infraenv")
	}
}

func (b *bareMetalInventory) HandleVerifyVipsResponse(ctx context.Context, host *models.Host, stepReply string) error {
	if host.ClusterID == nil || *host.ClusterID == "" {
		return errors.Errorf("host %s infra-env %s: empty cluster id", host.ID.String(), host.InfraEnvID.String())
	}
	return b.clusterApi.HandleVerifyVipsResponse(ctx, *host.ClusterID, stepReply)
}
