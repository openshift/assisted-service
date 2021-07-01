package bminventory

import (
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
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/hashicorp/go-version"
	"github.com/jinzhu/gorm"
	"github.com/kennygrant/sanitize"
	clusterPkg "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/identity"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	ctxparams "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/conversions"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/pkg/transaction"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
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
	RegisterClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterClusterParams) (*common.Cluster, error)
	GetClusterInternal(ctx context.Context, params installer.GetClusterParams) (*common.Cluster, error)
	UpdateClusterNonInteractive(ctx context.Context, params installer.UpdateClusterParams) (*common.Cluster, error)
	GenerateClusterISOInternal(ctx context.Context, params installer.GenerateClusterISOParams) (*common.Cluster, error)
	UpdateDiscoveryIgnitionInternal(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) error
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	GetHostByKubeKey(key types.NamespacedName) (*common.Host, error)
	InstallClusterInternal(ctx context.Context, params installer.InstallClusterParams) (*common.Cluster, error)
	DeregisterClusterInternal(ctx context.Context, params installer.DeregisterClusterParams) error
	DeregisterHostInternal(ctx context.Context, params installer.DeregisterHostParams) error
	GetCommonHostInternal(ctx context.Context, clusterId string, hostId string) (*common.Host, error)
	UpdateHostApprovedInternal(ctx context.Context, clusterId string, hostId string, approved bool) error
	UpdateHostInstallerArgsInternal(ctx context.Context, params installer.UpdateHostInstallerArgsParams) (*models.Host, error)
	UpdateHostIgnitionInternal(ctx context.Context, params installer.UpdateHostIgnitionParams) (*models.Host, error)
	GetCredentialsInternal(ctx context.Context, params installer.GetCredentialsParams) (*models.Credentials, error)
	DownloadClusterFilesInternal(ctx context.Context, params installer.DownloadClusterFilesParams) (io.ReadCloser, int64, error)
	RegisterAddHostsClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterAddHostsClusterParams) (*common.Cluster, error)
	InstallSingleDay2HostInternal(ctx context.Context, clusterId strfmt.UUID, hostId strfmt.UUID) error
	UpdateClusterInstallConfigInternal(ctx context.Context, params installer.UpdateClusterInstallConfigParams) (*common.Cluster, error)
	CancelInstallationInternal(ctx context.Context, params installer.CancelInstallationParams) (*common.Cluster, error)
	AddOpenshiftVersion(ctx context.Context, ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error)
}

//go:generate mockgen -package bminventory -destination mock_crd_utils.go . CRDUtils
type CRDUtils interface {
	CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId, clusterNamespace, clusterName string, clusterID *strfmt.UUID) error
}
type bareMetalInventory struct {
	Config
	db                   *gorm.DB
	log                  logrus.FieldLogger
	hostApi              host.API
	clusterApi           clusterPkg.API
	dnsApi               dns.DNSApi
	eventsHandler        events.Handler
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
}

func NewBareMetalInventory(
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi clusterPkg.API,
	cfg Config,
	generator generator.ISOInstallConfigGenerator,
	eventsHandler events.Handler,
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

	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	cfg, err := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(c, b.IgnitionConfig, false, b.authHandler.AuthType())
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

	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
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

	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Custom discovery ignition config was applied to the cluster", time.Now())
	log.Infof("Custom discovery ignition config was applied to cluster %s", params.ClusterID)

	existed, err := b.objectHandler.DeleteObject(ctx, getImageName(*c.ID))
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if existed {
		b.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo, "Deleted image from backend because its ignition was updated. The image may be regenerated at any time.", time.Now())
	}

	return nil
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	c, err := b.RegisterClusterInternal(ctx, nil, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) setDefaultRegisterClusterParams(_ context.Context, params installer.RegisterClusterParams) installer.RegisterClusterParams {
	if params.NewClusterParams.ClusterNetworkCidr == nil {
		params.NewClusterParams.ClusterNetworkCidr = &b.Config.DefaultClusterNetworkCidr
	}
	if params.NewClusterParams.ClusterNetworkHostPrefix == 0 {
		params.NewClusterParams.ClusterNetworkHostPrefix = b.Config.DefaultClusterNetworkHostPrefix
	}
	if params.NewClusterParams.ServiceNetworkCidr == nil {
		params.NewClusterParams.ServiceNetworkCidr = &b.Config.DefaultServiceNetworkCidr
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

	return params
}

func (b *bareMetalInventory) RegisterClusterInternal(
	ctx context.Context,
	kubeKey *types.NamespacedName,
	params installer.RegisterClusterParams) (*common.Cluster, error) {

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
			b.eventsHandler.AddEvent(ctx, id, nil, models.EventSeverityInfo, msg, time.Now())
		} else {
			errWrapperLog := log
			if err != nil {
				errWrapperLog = log.WithError(err)
			}
			errWrapperLog.Errorf("Failed to registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
		}
	}()

	if err = validations.ValidateIPAddressFamily(b.IPv6Support, params.NewClusterParams.ClusterNetworkCidr, params.NewClusterParams.ServiceNetworkCidr,
		&params.NewClusterParams.IngressVip); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if params.NewClusterParams.HTTPProxy != nil &&
		(params.NewClusterParams.HTTPSProxy == nil || *params.NewClusterParams.HTTPSProxy == "") {
		params.NewClusterParams.HTTPSProxy = params.NewClusterParams.HTTPProxy
	}

	if err = validateProxySettings(params.NewClusterParams.HTTPProxy,
		params.NewClusterParams.HTTPSProxy,
		params.NewClusterParams.NoProxy, params.NewClusterParams.OpenshiftVersion); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	params = b.setDefaultRegisterClusterParams(ctx, params)

	if swag.StringValue(params.NewClusterParams.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		// verify minimal OCP version
		err = verifyMinimalOpenShiftVersionForSingleNode(swag.StringValue(params.NewClusterParams.OpenshiftVersion))
		if err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		log.Infof("HA mode is None, setting UserManagedNetworking to true and VipDhcpAllocation to false")
		params.NewClusterParams.UserManagedNetworking = swag.Bool(true)
		// in case of single node VipDhcpAllocation should be always false
		params.NewClusterParams.VipDhcpAllocation = swag.Bool(false)
	}

	if swag.BoolValue(params.NewClusterParams.UserManagedNetworking) {
		if swag.BoolValue(params.NewClusterParams.VipDhcpAllocation) {
			err = errors.Errorf("VIP DHCP Allocation cannot be enabled with User Managed Networking")
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		if params.NewClusterParams.IngressVip != "" {
			err = errors.Errorf("Ingress VIP cannot be set with User Managed Networking")
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if params.NewClusterParams.AdditionalNtpSource == nil {
		params.NewClusterParams.AdditionalNtpSource = &b.Config.DefaultNTPSource
	} else {
		ntpSource := swag.StringValue(params.NewClusterParams.AdditionalNtpSource)

		if ntpSource != "" && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err = errors.Errorf("Invalid NTP source: %s", ntpSource)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	openshiftVersion, err := b.versionsHandler.GetVersion(swag.StringValue(params.NewClusterParams.OpenshiftVersion))
	if err != nil {
		err = errors.Errorf("Openshift version %s is not supported",
			swag.StringValue(params.NewClusterParams.OpenshiftVersion))
		return nil, common.NewApiError(http.StatusBadRequest, err)
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
			ID:                       &id,
			Href:                     swag.String(url.String()),
			Kind:                     swag.String(models.ClusterKindCluster),
			BaseDNSDomain:            params.NewClusterParams.BaseDNSDomain,
			ClusterNetworkCidr:       swag.StringValue(params.NewClusterParams.ClusterNetworkCidr),
			ClusterNetworkHostPrefix: params.NewClusterParams.ClusterNetworkHostPrefix,
			IngressVip:               params.NewClusterParams.IngressVip,
			Name:                     swag.StringValue(params.NewClusterParams.Name),
			OpenshiftVersion:         *openshiftVersion.ReleaseVersion,
			OcpReleaseImage:          *openshiftVersion.ReleaseImage,
			ServiceNetworkCidr:       swag.StringValue(params.NewClusterParams.ServiceNetworkCidr),
			SSHPublicKey:             params.NewClusterParams.SSHPublicKey,
			UpdatedAt:                strfmt.DateTime{},
			UserName:                 ocm.UserNameFromContext(ctx),
			OrgID:                    ocm.OrgIDFromContext(ctx),
			EmailDomain:              ocm.EmailDomainFromContext(ctx),
			HTTPProxy:                swag.StringValue(params.NewClusterParams.HTTPProxy),
			HTTPSProxy:               swag.StringValue(params.NewClusterParams.HTTPSProxy),
			NoProxy:                  swag.StringValue(params.NewClusterParams.NoProxy),
			VipDhcpAllocation:        params.NewClusterParams.VipDhcpAllocation,
			UserManagedNetworking:    params.NewClusterParams.UserManagedNetworking,
			AdditionalNtpSource:      swag.StringValue(params.NewClusterParams.AdditionalNtpSource),
			MonitoredOperators:       monitoredOperators,
			HighAvailabilityMode:     params.NewClusterParams.HighAvailabilityMode,
			Hyperthreading:           swag.StringValue(params.NewClusterParams.Hyperthreading),
		},
		KubeKeyName:             kubeKey.Name,
		KubeKeyNamespace:        kubeKey.Namespace,
		TriggerMonitorTimestamp: time.Now(),
	}

	proxyHash, err := computeClusterProxyHash(params.NewClusterParams.HTTPProxy,
		params.NewClusterParams.HTTPSProxy,
		params.NewClusterParams.NoProxy)
	if err != nil {
		err = errors.Wrapf(err, "failed to compute cluster proxy hash")
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("Failed to compute cluster proxy hash"))
	} else {
		cluster.ProxyHash = proxyHash
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

	b.setDefaultUsage(&cluster.Cluster)

	err = b.clusterApi.RegisterCluster(ctx, &cluster)
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if b.ocmClient != nil && b.ocmClient.Config.WithAMSSubscriptions {
		if err = b.integrateWithAMSClusterRegistration(ctx, &cluster); err != nil {
			err = errors.Wrapf(err, "cluster %s failed to integrate with AMS on cluster registration", id)
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	success = true
	b.metricApi.ClusterRegistered(cluster.OpenshiftVersion, *cluster.ID, cluster.EmailDomain)
	return b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *cluster.ID})
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
func (b *bareMetalInventory) RegisterAddHostsCluster(ctx context.Context, params installer.RegisterAddHostsClusterParams) middleware.Responder {
	c, err := b.RegisterAddHostsClusterInternal(ctx, nil, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterAddHostsClusterCreated().WithPayload(&c.Cluster)

}
func (b *bareMetalInventory) RegisterAddHostsClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterAddHostsClusterParams) (*common.Cluster, error) {
	id := params.NewAddHostsClusterParams.ID
	url := installer.GetClusterURL{ClusterID: *id}

	log := logutil.FromContext(ctx, b.log).WithField(ctxparams.ClusterId, id)
	apivipDnsname := swag.StringValue(params.NewAddHostsClusterParams.APIVipDnsname)
	clusterName := swag.StringValue(params.NewAddHostsClusterParams.Name)
	inputOpenshiftVersion := swag.StringValue(params.NewAddHostsClusterParams.OpenshiftVersion)

	log.Infof("Register add-hosts-cluster: %s, id %s, version %s", clusterName, id.String(), inputOpenshiftVersion)

	if clusterPkg.ClusterExists(b.db, *id) {
		return nil, common.NewApiError(http.StatusBadRequest, fmt.Errorf("AddHostsCluster for AI cluster %s already exists", id))
	}

	openshiftVersion, err := b.versionsHandler.GetVersion(inputOpenshiftVersion)
	if err != nil {
		log.WithError(err).Errorf("Failed to get opnshift version supported by versions map from version %s", inputOpenshiftVersion)
		return nil, common.NewApiError(http.StatusBadRequest, fmt.Errorf("failed to get opnshift version supported by versions map from version %s", inputOpenshiftVersion))
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
	}

	newCluster := common.Cluster{Cluster: models.Cluster{
		ID:               id,
		Href:             swag.String(url.String()),
		Kind:             swag.String(models.ClusterKindAddHostsCluster),
		Name:             clusterName,
		OpenshiftVersion: *openshiftVersion.ReleaseVersion,
		OcpReleaseImage:  *openshiftVersion.ReleaseImage,
		UserName:         ocm.UserNameFromContext(ctx),
		OrgID:            ocm.OrgIDFromContext(ctx),
		EmailDomain:      ocm.EmailDomainFromContext(ctx),
		UpdatedAt:        strfmt.DateTime{},
		APIVipDNSName:    swag.String(apivipDnsname),
		HostNetworks:     []*models.HostNetwork{},
		Hosts:            []*models.Host{},
	},
		KubeKeyName:      kubeKey.Name,
		KubeKeyNamespace: kubeKey.Namespace,
	}

	err = validations.ValidateClusterNameFormat(clusterName)
	if err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	// After registering the cluster, its status should be 'ClusterStatusAddingHosts'
	err = b.clusterApi.RegisterAddHostsCluster(ctx, &newCluster)
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
	address := cluster.APIVip
	if address == "" {
		address = swag.StringValue(cluster.APIVipDNSName)
	}
	ignitionBytes, err := b.IgnitionBuilder.FormatSecondDayWorkerIgnitionFile(address, host.MachineConfigPoolName)
	if err != nil {
		return errors.Errorf("Failed to create ignition string for cluster %s", cluster.ID)
	}
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
	if err := b.DeregisterClusterInternal(ctx, params); err != nil {
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

func (b *bareMetalInventory) DeregisterClusterInternal(ctx context.Context, params installer.DeregisterClusterParams) error {
	log := logutil.FromContext(ctx, b.log)
	var cluster *common.Cluster
	var err error
	log.Infof("Deregister cluster id %s", params.ClusterID)

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	if b.ocmClient != nil && b.ocmClient.Config.WithAMSSubscriptions {
		if err = b.integrateWithAMSClusterDeregistration(ctx, cluster); err != nil {
			log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster deregistration", params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err = b.deleteDNSRecordSets(ctx, *cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	err = b.clusterApi.DeregisterCluster(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister cluster %s", params.ClusterID)
		return common.NewApiError(http.StatusNotFound, err)
	}
	return nil
}

func (b *bareMetalInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s", params.ClusterID)
		return common.NewApiError(http.StatusNotFound, err)
	}

	imgName := getImageName(*cluster.ID)
	exists, err := b.objectHandler.DoesObjectExist(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for cluster %s", cluster.ID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError,
			"Failed to download image: error fetching from storage backend", time.Now())
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	if !exists {
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError,
			"Failed to download image: the image was not found (perhaps it expired) - please generate the image and try again", time.Now())
		return installer.NewDownloadClusterISONotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, errors.New("The image was not found "+
				"(perhaps it expired) - please generate the image and try again")))
	}
	reader, contentLength, err := b.objectHandler.Download(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for cluster %s", cluster.ID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError,
			"Failed to download image: error fetching from storage backend", time.Now())
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo,
		fmt.Sprintf(`Started image download (image type is "%s")`, cluster.ImageInfo.Type), time.Now())

	return filemiddleware.NewResponder(installer.NewDownloadClusterISOOK().WithPayload(reader),
		fmt.Sprintf("cluster-%s-discovery.iso", params.ClusterID.String()),
		contentLength)
}

func (b *bareMetalInventory) DownloadClusterISOHeaders(ctx context.Context, params installer.DownloadClusterISOHeadersParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s", params.ClusterID)
		return common.NewApiError(http.StatusNotFound, err)
	}

	imgName := getImageName(*cluster.ID)
	exists, err := b.objectHandler.DoesObjectExist(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO for cluster %s", cluster.ID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError,
			"Failed to download image: error fetching from storage backend", time.Now())
		return installer.NewDownloadClusterISOHeadersInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	if !exists {
		return installer.NewDownloadClusterISOHeadersNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, errors.New("The image was not found")))
	}
	imgSize, err := b.objectHandler.GetObjectSizeBytes(ctx, imgName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO size for cluster %s", cluster.ID.String())
		return common.NewApiError(http.StatusBadRequest, err)
	}
	return installer.NewDownloadClusterISOHeadersOK().WithContentLength(imgSize)
}

func (b *bareMetalInventory) updateImageInfoPostUpload(ctx context.Context, cluster *common.Cluster, clusterProxyHash string, imageType models.ImageType, generated bool) error {
	updates := map[string]interface{}{}
	imgName := getImageName(*cluster.ID)
	imgSize, err := b.objectHandler.GetObjectSizeBytes(ctx, imgName)
	if err != nil {
		return errors.New("Failed to generate image: error fetching size")
	}
	updates["image_size_bytes"] = imgSize
	cluster.ImageInfo.SizeBytes = &imgSize

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if generated {
		downloadURL := ""
		if b.objectHandler.IsAwsS3() {
			downloadURL, err = b.objectHandler.GeneratePresignedDownloadURL(ctx, imgName, imgName, b.Config.ImageExpirationTime)
			if err != nil {
				return errors.New("Failed to generate image: error generating URL")
			}
		} else {
			var downloadClusterISOURL = &installer.DownloadClusterISOURL{ClusterID: *cluster.ID}
			clusterISOURL, err := downloadClusterISOURL.Build()
			if err != nil {
				return errors.New("Failed to generate image: error generating cluster ISO URL")
			}
			downloadURL = fmt.Sprintf("%s%s", b.Config.ServiceBaseURL, clusterISOURL.RequestURI())
			if b.authHandler.AuthType() == auth.TypeLocal {
				downloadURL, err = gencrypto.SignURL(downloadURL, cluster.ID.String())
				if err != nil {
					return errors.Wrap(err, "Failed to sign cluster ISO URL")
				}
			}
		}
		updates["image_download_url"] = downloadURL
		cluster.ImageInfo.DownloadURL = downloadURL
		updates["image_generated"] = true
		cluster.ImageGenerated = true
	}

	if cluster.ProxyHash != clusterProxyHash {
		updates["proxy_hash"] = clusterProxyHash
		cluster.ProxyHash = clusterProxyHash
	}

	updates["image_type"] = imageType
	cluster.ImageInfo.Type = imageType

	dbReply := b.db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		return errors.New("Failed to generate image: error updating image record")
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
		if err := b.staticNetworkConfig.ValidateStaticConfigParams(params.ImageCreateParams.StaticNetworkConfig); err != nil {
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
		msg := "Failed to generate image: error starting DB transaction"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction"))
	}

	cluster, err := common.GetClusterFromDB(tx, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, err
	}

	/* We need to ensure that the metadata in the DB matches the image that will be uploaded to S3,
	so we check that at least 10 seconds have past since the previous request to reduce the chance
	of a race between two consecutive requests.
	*/
	now := time.Now()
	previousCreatedAt := time.Time(cluster.ImageInfo.CreatedAt)
	if previousCreatedAt.Add(WindowBetweenRequestsInSeconds).After(now) {
		log.Error("request came too soon after previous request")
		return nil, common.NewApiError(
			http.StatusConflict,
			errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again."))
	}

	if !cluster.PullSecretSet {
		errMsg := "Can't generate cluster ISO without pull secret"
		log.Error(errMsg)
		return nil, common.NewApiError(http.StatusBadRequest, errors.New(errMsg))
	}

	/* If the request has the same parameters as the previous request and the image is still in S3,
	just refresh the timestamp.
	*/
	clusterProxyHash, err := computeClusterProxyHash(&cluster.HTTPProxy, &cluster.HTTPSProxy, &cluster.NoProxy)
	if err != nil {
		msg := "Failed to compute cluster proxy hash"
		log.Error(msg, err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	staticNetworkConfig := b.staticNetworkConfig.FormatStaticNetworkConfigForDB(params.ImageCreateParams.StaticNetworkConfig)

	var imageExists bool
	if cluster.ImageInfo.SSHPublicKey == params.ImageCreateParams.SSHPublicKey &&
		cluster.ProxyHash == clusterProxyHash &&
		cluster.ImageInfo.StaticNetworkConfig == staticNetworkConfig &&
		cluster.ImageGenerated &&
		cluster.ImageInfo.Type == params.ImageCreateParams.ImageType {
		imgName := getImageName(params.ClusterID)
		imageExists, err = b.objectHandler.UpdateObjectTimestamp(ctx, imgName)
		if err != nil {
			log.WithError(err).Errorf("failed to contact storage backend")
			b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError,
				"Failed to generate image: error contacting storage backend", time.Now())
			return nil, common.NewApiError(http.StatusInternalServerError, errors.New("failed to contact storage backend"))
		}
	}

	updates := map[string]interface{}{}
	updates["image_ssh_public_key"] = params.ImageCreateParams.SSHPublicKey
	updates["image_created_at"] = strfmt.DateTime(now)
	updates["image_expires_at"] = strfmt.DateTime(now.Add(b.Config.ImageExpirationTime))
	updates["image_static_network_config"] = staticNetworkConfig
	if !imageExists {
		// set image-generated indicator to false before the attempt to genearate the image in order to have an explicit
		// state of the image creation based on the cluster parameters which will be committed to the DB
		updates["image_generated"] = false
		updates["image_download_url"] = ""
	}
	dbReply := tx.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update cluster: %s", params.ClusterID)
		msg := "Failed to generate image: error updating metadata"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		msg := "Failed to generate image: error committing the transaction"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}
	txSuccess = true
	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		msg := "Failed to generate image: error fetching updated cluster metadata"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return nil, err
	}

	if imageExists {
		if err = b.updateImageInfoPostUpload(ctx, cluster, clusterProxyHash, params.ImageCreateParams.ImageType, false); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}

		log.Infof("Re-used existing cluster <%s> image", params.ClusterID)
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo,
			fmt.Sprintf(`Re-used existing image rather than generating a new one (image type is "%s")`,
				cluster.ImageInfo.Type),
			time.Now())
		return b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *cluster.ID})
	}

	err = b.createAndUploadNewImage(ctx, log, clusterProxyHash, cluster, params)
	if err != nil {
		return nil, err
	}

	return b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *cluster.ID})
}

func (b *bareMetalInventory) createAndUploadNewImage(ctx context.Context, log logrus.FieldLogger, clusterProxyHash string,
	cluster *common.Cluster, params installer.GenerateClusterISOParams) error {
	// Setting ImageInfo.Type at this point in order to pass it to FormatDiscoveryIgnitionFile without saving it to the DB.
	// Saving it to the DB will be done after a successful image generation by updateImageInfoPostUpload
	cluster.ImageInfo.Type = params.ImageCreateParams.ImageType
	ignitionConfig, err := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(cluster, b.IgnitionConfig, false, b.authHandler.AuthType())
	if err != nil {
		log.WithError(err).Errorf("failed to format ignition config file for cluster %s", cluster.ID)
		msg := "Failed to generate image: error formatting ignition file"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.objectHandler.Upload(ctx, []byte(ignitionConfig), fmt.Sprintf("%s/discovery.ign", cluster.ID)); err != nil {
		log.WithError(err).Errorf("Upload discovery ignition failed for cluster %s", cluster.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	objectPrefix := fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, cluster.ID.String())

	if params.ImageCreateParams.ImageType == models.ImageTypeMinimalIso {
		if err := b.generateClusterMinimalISO(ctx, log, cluster, ignitionConfig, objectPrefix); err != nil {
			log.WithError(err).Errorf("Failed to generate minimal ISO for cluster %s", cluster.ID)
			b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, "Failed to generate minimal ISO", time.Now())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	} else {
		baseISOName, err := b.objectHandler.GetBaseIsoObject(cluster.OpenshiftVersion)
		if err != nil {
			log.WithError(err).Errorf("Failed to get source object name for cluster %s with ocp version %s", cluster.ID, cluster.OpenshiftVersion)
			return common.NewApiError(http.StatusInternalServerError, err)
		}

		if err := b.objectHandler.UploadISO(ctx, ignitionConfig, baseISOName, objectPrefix); err != nil {
			log.WithError(err).Errorf("Upload ISO failed for cluster %s", cluster.ID)
			b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, "Failed to upload image", time.Now())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err := b.updateImageInfoPostUpload(ctx, cluster, clusterProxyHash, params.ImageCreateParams.ImageType, true); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	msg := b.getIgnitionConfigForLogging(cluster, params, log)
	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, msg, time.Now())

	return nil
}

func (b *bareMetalInventory) getIgnitionConfigForLogging(cluster *common.Cluster, params installer.GenerateClusterISOParams, log logrus.FieldLogger) string {
	ignitionConfigForLogging, _ := b.IgnitionBuilder.FormatDiscoveryIgnitionFile(cluster, b.IgnitionConfig, true, b.authHandler.AuthType())
	log.Infof("Generated cluster <%s> image with ignition config %s", params.ClusterID, ignitionConfigForLogging)
	msg := "Generated image"
	var msgExtras []string

	if cluster.HTTPProxy != "" {
		msgExtras = append(msgExtras, fmt.Sprintf(`proxy URL is "%s"`, cluster.HTTPProxy))
	}

	msgExtras = append(msgExtras, fmt.Sprintf(`Image type is "%s"`, string(params.ImageCreateParams.ImageType)))

	sshExtra := "SSH public key is not set"
	if params.ImageCreateParams.SSHPublicKey != "" {
		sshExtra = "SSH public key is set"
	}

	msgExtras = append(msgExtras, sshExtra)

	msg = fmt.Sprintf("%s (%s)", msg, strings.Join(msgExtras, ", "))
	return msg
}

func (b *bareMetalInventory) generateClusterMinimalISO(ctx context.Context, log logrus.FieldLogger,
	cluster *common.Cluster, ignitionConfig, objectPrefix string) error {

	baseISOName, err := b.objectHandler.GetMinimalIsoObjectName(cluster.OpenshiftVersion)
	if err != nil {
		log.WithError(err).Errorf("Failed to get source object name for cluster %s with ocp version %s", cluster.ID, cluster.OpenshiftVersion)
		return err
	}

	isoPath, err := s3wrapper.GetFile(ctx, b.objectHandler, baseISOName, b.ISOCacheDir, true)
	if err != nil {
		log.WithError(err).Errorf("Failed to download minimal ISO template %s", baseISOName)
		return err
	}

	var clusterISOPath string
	err = b.isoEditorFactory.WithEditor(ctx, isoPath, log, func(editor isoeditor.Editor) error {
		log.Infof("Creating minimal ISO for cluster %s", cluster.ID)
		var createError error
		clusterProxyInfo := isoeditor.ClusterProxyInfo{
			HTTPProxy:  cluster.HTTPProxy,
			HTTPSProxy: cluster.HTTPSProxy,
			NoProxy:    cluster.NoProxy,
		}
		clusterISOPath, createError = editor.CreateClusterMinimalISO(ignitionConfig, cluster.ImageInfo.StaticNetworkConfig, &clusterProxyInfo)
		return createError
	})

	if err != nil {
		log.WithError(err).Errorf("Failed to create minimal discovery ISO cluster %s with iso file %s", cluster.ID, isoPath)
		return err
	}

	log.Infof("Uploading minimal ISO for cluster %s", cluster.ID)
	if err := b.objectHandler.UploadFile(ctx, clusterISOPath, fmt.Sprintf("%s.iso", objectPrefix)); err != nil {
		os.Remove(clusterISOPath)
		log.WithError(err).Errorf("Failed to upload minimal discovery ISO for cluster %s", cluster.ID)
		return err
	}
	return os.Remove(clusterISOPath)
}

func getImageName(clusterID strfmt.UUID) string {
	return fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, clusterID.String()))
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
	c, err := b.InstallClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewInstallClusterAccepted().WithPayload(&c.Cluster)
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

func (b *bareMetalInventory) InstallClusterInternal(ctx context.Context, params installer.InstallClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	cluster := &common.Cluster{}
	var err error

	log.Infof("preparing for cluster %s installation", params.ClusterID)
	if cluster, err = common.GetClusterFromDBWithoutDisabledHosts(b.db, params.ClusterID); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	// auto select hosts roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		for i := range cluster.Hosts {
			if err = b.hostApi.AutoAssignRole(ctx, cluster.Hosts[i], tx); err != nil {
				return err
			}
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
		// in case host monitor already updated the state we need to use FOR UPDATE option
		tx = transaction.AddForUpdateQueryOption(tx)

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
		asyncCtx := requestid.ToContext(context.Background(), requestid.FromContext(ctx))

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

		if b.ocmClient != nil && b.ocmClient.Config.WithAMSSubscriptions {
			if err = b.integrateWithAMSClusterPreInstallation(asyncCtx, cluster.AmsSubscriptionID, strfmt.UUID(openshiftClusterID)); err != nil {
				log.WithError(err).Errorf("Cluster %s failed to integrate with AMS on cluster pre installation", params.ClusterID)
				return
			}
		}
	}()

	log.Infof("Successfully prepared cluster <%s> for installation", params.ClusterID.String())
	return cluster, nil
}

func (b *bareMetalInventory) InstallSingleDay2HostInternal(ctx context.Context, clusterId strfmt.UUID, hostId strfmt.UUID) error {

	log := logutil.FromContext(ctx, b.log)
	var err error
	var cluster *common.Cluster
	var h *common.Host

	if cluster, err = common.GetClusterFromDB(b.db, clusterId, common.UseEagerLoading); err != nil {
		return err
	}
	if h, err = b.getHost(ctx, clusterId.String(), hostId.String()); err != nil {
		return err
	}
	// auto select host roles if not selected yet.
	err = b.db.Transaction(func(tx *gorm.DB) error {
		if err = b.hostApi.AutoAssignRole(ctx, &h.Host, tx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err = b.setMajorityGroupForCluster(cluster.ID, b.db); err != nil {
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
	tx = transaction.AddForUpdateQueryOption(tx)

	if cluster, err = common.GetClusterFromDB(tx, clusterId, common.UseEagerLoading); err != nil {
		return err
	}

	// move host to installing
	err = b.createAndUploadNodeIgnition(ctx, cluster, &h.Host)
	if err != nil {
		log.Errorf("Failed to upload ignition for host %s", h.RequestedHostname)
		return err
	}
	if installErr := b.hostApi.Install(ctx, &h.Host, tx); installErr != nil {
		log.Errorf("Failed to move host %s to installing", h.RequestedHostname)
		return installErr
	}

	err = tx.Commit().Error
	if err != nil {
		log.Error(err)
		return err
	}
	txSuccess = true

	return nil
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
			if err = b.hostApi.AutoAssignRole(ctx, cluster.Hosts[i], tx); err != nil {
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
	tx = transaction.AddForUpdateQueryOption(tx)

	if cluster, err = common.GetClusterFromDB(tx, params.ClusterID, common.UseEagerLoading); err != nil {
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

	masterNodesIds, err := b.clusterApi.GetMasterNodesIds(ctx, &cluster, db)
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
	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	cfg, err := b.installConfigBuilder.GetInstallConfig(c, false, "")
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewGetClusterInstallConfigOK().WithPayload(string(cfg))
}

func (b *bareMetalInventory) GetClusterDefaultConfig(_ context.Context, _ installer.GetClusterDefaultConfigParams) middleware.Responder {
	body := models.ClusterDefaultConfig{}

	body.NtpSource = b.Config.DefaultNTPSource
	body.ClusterNetworkCidr = b.Config.DefaultClusterNetworkCidr
	body.ServiceNetworkCidr = b.Config.DefaultServiceNetworkCidr
	body.ClusterNetworkHostPrefix = b.Config.DefaultClusterNetworkHostPrefix
	body.InactiveDeletionHours = int64(b.gcConfig.DeregisterInactiveAfter.Hours())

	return installer.NewGetClusterDefaultConfigOK().WithPayload(&body)
}

func (b *bareMetalInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	_, err := b.UpdateClusterInstallConfigInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateClusterInstallConfigCreated()
}

func (b *bareMetalInventory) UpdateClusterInstallConfigInternal(ctx context.Context, params installer.UpdateClusterInstallConfigParams) (*common.Cluster, error) {
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

	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Custom install config was applied to the cluster", time.Now())
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

	releaseImage, err := b.versionsHandler.GetReleaseImage(cluster.OpenshiftVersion)

	if err != nil {
		log.WithError(err).Errorf("failed to get release image for cluster %s with openshift version %s", cluster.ID, cluster.OpenshiftVersion)
		return errors.Wrapf(err, "failed to get release image for cluster %s with openshift version %s", cluster.ID, cluster.OpenshiftVersion)
	}

	if err := b.generator.GenerateInstallConfig(ctx, cluster, cfg, releaseImage); err != nil {
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

func (b *bareMetalInventory) validateAndUpdateClusterParams(ctx context.Context, params *installer.UpdateClusterParams) (installer.UpdateClusterParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if swag.StringValue(params.ClusterUpdateParams.PullSecret) != "" {
		if err := b.secretValidator.ValidatePullSecret(*params.ClusterUpdateParams.PullSecret, ocm.UserNameFromContext(ctx), b.authHandler); err != nil {
			log.WithError(err).Errorf("Pull secret for cluster %s is invalid", params.ClusterID)
			return installer.UpdateClusterParams{}, err
		}
		ps, errUpdate := b.updatePullSecret(*params.ClusterUpdateParams.PullSecret, log)
		if errUpdate != nil {
			return installer.UpdateClusterParams{}, errors.New("Failed to update Pull-secret with additional credentials")
		}
		params.ClusterUpdateParams.PullSecret = &ps
	}

	if swag.StringValue(params.ClusterUpdateParams.Name) != "" {
		if err := validations.ValidateClusterNameFormat(*params.ClusterUpdateParams.Name); err != nil {
			return installer.UpdateClusterParams{}, err
		}
	}

	if err := validations.ValidateIPAddressFamily(b.IPv6Support, params.ClusterUpdateParams.ClusterNetworkCidr, params.ClusterUpdateParams.ServiceNetworkCidr,
		params.ClusterUpdateParams.MachineNetworkCidr, params.ClusterUpdateParams.APIVip, params.ClusterUpdateParams.IngressVip); err != nil {
		return installer.UpdateClusterParams{}, common.NewApiError(http.StatusBadRequest, err)
	}

	if sshPublicKey := swag.StringValue(params.ClusterUpdateParams.SSHPublicKey); sshPublicKey != "" {
		sshPublicKey = strings.TrimSpace(sshPublicKey)
		if err := validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
			return installer.UpdateClusterParams{}, err
		}
		*params.ClusterUpdateParams.SSHPublicKey = sshPublicKey
	}

	return *params, nil
}

func (b *bareMetalInventory) validateAndUpdateProxyParams(ctx context.Context, params *installer.UpdateClusterParams, ocpVersion *string) (installer.UpdateClusterParams, error) {

	log := logutil.FromContext(ctx, b.log)

	if params.ClusterUpdateParams.HTTPProxy != nil &&
		(params.ClusterUpdateParams.HTTPSProxy == nil || *params.ClusterUpdateParams.HTTPSProxy == "") {
		params.ClusterUpdateParams.HTTPSProxy = params.ClusterUpdateParams.HTTPProxy
	}

	if err := validateProxySettings(params.ClusterUpdateParams.HTTPProxy,
		params.ClusterUpdateParams.HTTPSProxy,
		params.ClusterUpdateParams.NoProxy, ocpVersion); err != nil {
		log.WithError(err).Errorf("Failed to validate Proxy settings")
		return installer.UpdateClusterParams{}, err
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
func (b *bareMetalInventory) UpdateClusterNonInteractive(ctx context.Context, params installer.UpdateClusterParams) (*common.Cluster, error) {
	return b.updateClusterInternal(ctx, params, NonInteractive)
}

func (b *bareMetalInventory) updateClusterInternal(ctx context.Context, params installer.UpdateClusterParams, interactivity Interactivity) (*common.Cluster, error) {
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
	tx = transaction.AddForUpdateQueryOption(tx)

	if cluster, err = common.GetClusterFromDB(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	if params, err = b.validateAndUpdateProxyParams(ctx, &params, &cluster.OpenshiftVersion); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.clusterApi.VerifyClusterUpdatability(cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", params.ClusterID)
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	if err = b.noneHaModeClusterUpdateValidations(cluster, params); err != nil {
		log.WithError(err).Warnf("Unsupported update params in none ha mode")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateDNSDomain(*cluster, params, log); err != nil {
		return nil, err
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

	err = b.updateHostsData(ctx, params, usages, tx, log)
	if err != nil {
		return nil, err
	}

	err = b.updateOperatorsData(ctx, cluster, params, usages, tx, log)
	if err != nil {
		return nil, err
	}

	err = b.updateHostsAndClusterStatus(ctx, cluster, tx, log)
	if err != nil {
		return nil, err
	}

	b.usageApi.Save(tx, params.ClusterID, usages)

	newClusterName := swag.StringValue(params.ClusterUpdateParams.Name)
	if b.ocmClient != nil && b.ocmClient.Config.WithAMSSubscriptions && newClusterName != "" && newClusterName != cluster.Name {
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
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Proxy settings changed", time.Now())
	}

	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return nil, err
	}

	cluster.HostNetworks = b.calculateHostNetworks(log, cluster)
	for _, host := range cluster.Hosts {
		if err := b.customizeHost(host); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
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

func setMachineNetworkCIDRForUpdate(updates map[string]interface{}, machineNetworkCIDR string) {
	updates["machine_network_cidr"] = machineNetworkCIDR
	updates["machine_network_cidr_updated_at"] = time.Now()
}

func (b *bareMetalInventory) updateNonDhcpNetworkParams(updates map[string]interface{}, cluster *common.Cluster, params installer.UpdateClusterParams, log logrus.FieldLogger, machineCidr *string, interactivity Interactivity) error {
	apiVip := cluster.APIVip
	ingressVip := cluster.IngressVip
	if params.ClusterUpdateParams.APIVip != nil {
		updates["api_vip"] = *params.ClusterUpdateParams.APIVip
		apiVip = *params.ClusterUpdateParams.APIVip
	}
	if params.ClusterUpdateParams.IngressVip != nil {
		updates["ingress_vip"] = *params.ClusterUpdateParams.IngressVip
		ingressVip = *params.ClusterUpdateParams.IngressVip
	}
	if params.ClusterUpdateParams.MachineNetworkCidr != nil {
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
		var machineNetworkCidr string
		matchRequired := apiVip != "" || ingressVip != ""
		machineNetworkCidr, err = network.CalculateMachineNetworkCIDR(apiVip, ingressVip, cluster.Hosts, matchRequired)
		if err != nil {
			return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Calculate machine network CIDR"))
		}
		if machineNetworkCidr != swag.StringValue(machineCidr) {
			*machineCidr = machineNetworkCidr
			setMachineNetworkCIDRForUpdate(updates, machineNetworkCidr)
		}
		err = network.VerifyVips(cluster.Hosts, swag.StringValue(machineCidr), apiVip, ingressVip, false, log)
		if err != nil {
			log.WithError(err).Warnf("Verify VIPs")
			return common.NewApiError(http.StatusBadRequest, err)
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

func (b *bareMetalInventory) updateDhcpNetworkParams(updates map[string]interface{}, params installer.UpdateClusterParams, log logrus.FieldLogger, machineCidr *string) error {
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
	if params.ClusterUpdateParams.MachineNetworkCidr != nil &&
		*machineCidr != swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr) {
		*machineCidr = swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr)
		setMachineNetworkCIDRForUpdate(updates, *machineCidr)
		updates["api_vip"] = ""
		updates["ingress_vip"] = ""
		return network.VerifyMachineCIDR(swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr))
	}
	return nil
}

func (b *bareMetalInventory) updateClusterData(_ context.Context, cluster *common.Cluster, params installer.UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger, interactivity Interactivity) error {
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

	if err = b.updateNetworkParams(params, cluster, updates, usages, log, interactivity); err != nil {
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
	if len(updates) > 0 {
		updates["trigger_monitor_timestamp"] = time.Now()
		dbReply := db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
		if dbReply.Error != nil {
			return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "failed to update cluster: %s", params.ClusterID))
		}
	}

	return nil
}

func (b *bareMetalInventory) updateNetworkParams(params installer.UpdateClusterParams, cluster *common.Cluster, updates map[string]interface{}, usages map[string]models.Usage, log logrus.FieldLogger, interactivity Interactivity) error {
	var err error
	machineCidr := cluster.MachineNetworkCidr
	serviceCidr := cluster.ServiceNetworkCidr
	clusterCidr := cluster.ClusterNetworkCidr
	hostNetworkPrefix := cluster.ClusterNetworkHostPrefix
	vipDhcpAllocation := swag.BoolValue(cluster.VipDhcpAllocation)
	userManagedNetworking := swag.BoolValue(cluster.UserManagedNetworking)

	if params.ClusterUpdateParams.ClusterNetworkCidr != nil {
		if err = network.VerifyClusterOrServiceCIDR(*params.ClusterUpdateParams.ClusterNetworkCidr); err != nil {
			return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Cluster network CIDR"))
		}
		clusterCidr = *params.ClusterUpdateParams.ClusterNetworkCidr
		updates["cluster_network_cidr"] = clusterCidr
	}
	if params.ClusterUpdateParams.ClusterNetworkHostPrefix != nil {
		if err = network.VerifyNetworkHostPrefix(*params.ClusterUpdateParams.ClusterNetworkHostPrefix); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
		hostNetworkPrefix = *params.ClusterUpdateParams.ClusterNetworkHostPrefix
		updates["cluster_network_host_prefix"] = hostNetworkPrefix
	}
	if clusterCidr != "" {
		err = network.VerifyClusterCidrSize(int(hostNetworkPrefix), clusterCidr, len(cluster.Hosts))
		if err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}
	if params.ClusterUpdateParams.ServiceNetworkCidr != nil {
		if err = network.VerifyClusterOrServiceCIDR(*params.ClusterUpdateParams.ServiceNetworkCidr); err != nil {
			return common.NewApiError(http.StatusBadRequest, errors.Wrap(err, "Service network CIDR"))
		}
		serviceCidr = *params.ClusterUpdateParams.ServiceNetworkCidr
		updates["service_network_cidr"] = serviceCidr
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) != userManagedNetworking {
		userManagedNetworking = swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking)
		updates["user_managed_networking"] = userManagedNetworking
		machineCidr = ""
	}
	if userManagedNetworking && !common.IsSingleNodeCluster(cluster) {
		err, vipDhcpAllocation = setCommonUserNetworkManagedParams(params.ClusterUpdateParams, common.IsSingleNodeCluster(cluster), machineCidr, updates, log)
		if err != nil {
			return err
		}
	}

	if params.ClusterUpdateParams.VipDhcpAllocation != nil && swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation) != vipDhcpAllocation {
		vipDhcpAllocation = swag.BoolValue(params.ClusterUpdateParams.VipDhcpAllocation)
		updates["vip_dhcp_allocation"] = vipDhcpAllocation
		updates["api_vip"] = ""
		updates["ingress_vip"] = ""
		machineCidr = ""
		setMachineNetworkCIDRForUpdate(updates, machineCidr)
	}
	if !userManagedNetworking {
		if vipDhcpAllocation {
			err = b.updateDhcpNetworkParams(updates, params, log, &machineCidr)
		} else {
			err = b.updateNonDhcpNetworkParams(updates, cluster, params, log, &machineCidr, interactivity)
		}
		if err != nil {
			return err
		}
	}

	if params.ClusterUpdateParams.MachineNetworkCidr != nil && common.IsSingleNodeCluster(cluster) {
		machineCidr = swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr)
		if err = network.VerifyMachineCIDR(machineCidr); err != nil {
			log.WithError(err).Warningf("Given machine cidr %q is not valid", machineCidr)
			return common.NewApiError(http.StatusBadRequest, err)
		}
		err, vipDhcpAllocation = setCommonUserNetworkManagedParams(params.ClusterUpdateParams, common.IsSingleNodeCluster(cluster), machineCidr, updates, log)
		if err != nil {
			return err
		}
	}

	if err = network.VerifyClusterCIDRsNotOverlap(machineCidr, clusterCidr, serviceCidr, userManagedNetworking); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if err = validations.ValidateVipDHCPAllocationWithIPv6(vipDhcpAllocation, machineCidr); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	b.setUsage(vipDhcpAllocation, usage.VipDhcpAllocationUsage, nil, usages)
	return nil
}

func setCommonUserNetworkManagedParams(params *models.ClusterUpdateParams, singleNodeCluster bool, machineCidr string, updates map[string]interface{}, log logrus.FieldLogger) (error, bool) {
	err := validateUserManagedNetworkConflicts(params, singleNodeCluster, log)
	if err != nil {
		return err, false
	}
	updates["vip_dhcp_allocation"] = false
	updates["api_vip"] = ""
	updates["ingress_vip"] = ""

	setMachineNetworkCIDRForUpdate(updates, machineCidr)
	return nil, false
}

func (b *bareMetalInventory) updateNtpSources(params installer.UpdateClusterParams, updates map[string]interface{}, usages map[string]models.Usage, log logrus.FieldLogger) error {
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

func validateUserManagedNetworkConflicts(params *models.ClusterUpdateParams, singleNodeCluster bool, log logrus.FieldLogger) error {
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
	if params.MachineNetworkCidr != nil && !singleNodeCluster {
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

func (b *bareMetalInventory) setDefaultUsage(cluster *models.Cluster) {
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
	featusage, _ := json.Marshal(usages)
	cluster.FeatureUsage = string(featusage)
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

func (b *bareMetalInventory) updateOperatorsData(_ context.Context, cluster *common.Cluster, params installer.UpdateClusterParams, usages map[string]models.Usage, db *gorm.DB, log logrus.FieldLogger) error {
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
	log := logutil.FromContext(ctx, b.log)
	db := b.db
	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !identity.IsAdmin(ctx) {
			return installer.NewListClustersForbidden().WithPayload(common.GenerateInfraError(
				http.StatusForbidden, errors.New("only admin users are allowed to get unregistered clusters")))
		}
		db = db.Unscoped()
	}
	var dbClusters []*common.Cluster
	var clusters []*models.Cluster
	whereCondition := identity.AddUserFilter(ctx, "")

	if params.OpenshiftClusterID != nil {
		whereCondition += fmt.Sprintf(" AND openshift_cluster_id = '%s'", *params.OpenshiftClusterID)
	}

	if len(params.AmsSubscriptionIds) > 0 {
		whereCondition += fmt.Sprintf(" AND ams_subscription_id IN %s", common.ToSqlList(params.AmsSubscriptionIds))
	}

	dbClusters, err := common.GetClustersFromDBWhere(db, common.UseEagerLoading,
		common.DeleteRecordsState(swag.BoolValue(params.GetUnregisteredClusters)), whereCondition)
	if err != nil {
		log.WithError(err).Error("Failed to list clusters in db")
		return common.NewApiError(http.StatusInternalServerError, err)
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
	return installer.NewListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	c, err := b.GetClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetClusterOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) GetClusterInternal(ctx context.Context, params installer.GetClusterParams) (*common.Cluster, error) {
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
		if err := b.customizeHost(host); err != nil {
			return nil, err
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}
	return cluster, nil
}

func (b *bareMetalInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	log.Infof("Register host: %+v", params)

	txSuccess := false
	tx := b.db.Begin()
	tx = transaction.AddForUpdateQueryOption(tx)
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

	if err := tx.First(&cluster, "id = ?", params.ClusterID.String()).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	_, err := common.GetHostFromDB(tx, params.ClusterID.String(), params.NewHostParams.HostID.String())
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.WithError(err).Errorf("failed to get host %s in cluster: %s",
			*params.NewHostParams.HostID, params.ClusterID.String())
		return installer.NewRegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// In case host doesn't exists check if the cluster accept new hosts registration
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		if err = b.clusterApi.AcceptRegistration(&cluster); err != nil {
			log.WithError(err).Errorf("failed to register host <%s> to cluster %s due to: %s",
				params.NewHostParams.HostID, params.ClusterID.String(), err.Error())
			b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
				err.Error(), time.Now())
			return common.NewApiError(http.StatusConflict, err)
		}
	}

	url := installer.GetHostURL{ClusterID: params.ClusterID, HostID: *params.NewHostParams.HostID}
	kind := swag.String(models.HostKindHost)
	if swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		kind = swag.String(models.HostKindAddToExistingClusterHost)
	}

	// We immediately set the role to master in single node clusters to have more strict (master) validations.
	// Typically, the validations are "weak" because an auto-assign host has the potential to only be a worker,
	// which has less strict hardware requirements. This early role assignment results in clearer, more early
	// errors for the user in case of insufficient hardware. In the future, single-node clusters might support
	// extra nodes (as workers). In that case, this line might need to be removed.
	defaultRole := models.HostRoleAutoAssign
	if common.IsSingleNodeCluster(&cluster) {
		defaultRole = models.HostRoleMaster
	}

	host := &models.Host{
		ID:                    params.NewHostParams.HostID,
		Href:                  swag.String(url.String()),
		Kind:                  kind,
		ClusterID:             params.ClusterID,
		CheckedInAt:           strfmt.DateTime(time.Now()),
		DiscoveryAgentVersion: params.NewHostParams.DiscoveryAgentVersion,
		UserName:              ocm.UserNameFromContext(ctx),
		Role:                  defaultRole,
	}

	if err = b.hostApi.RegisterHost(ctx, host, tx); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> cluster <%s>",
			params.NewHostParams.HostID.String(), params.ClusterID.String())
		uerr := errors.Wrap(err, fmt.Sprintf("Failed to register host %s", hostutil.GetHostnameForMsg(host)))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
			uerr.Error(), time.Now())
		return returnRegisterHostTransitionError(http.StatusBadRequest, err)
	}

	if err = b.customizeHost(host); err != nil {
		b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
			"Failed to register host: error setting host properties", time.Now())
		return common.GenerateErrorResponder(err)
	}

	b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s: registered to cluster", hostutil.GetHostnameForMsg(host)), time.Now())

	hostRegistration := models.HostRegistrationResponse{
		Host:                  *host,
		NextStepRunnerCommand: b.generateNextStepRunnerCommand(ctx, &params),
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewRegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.crdUtils.CreateAgentCR(ctx, log, params.NewHostParams.HostID.String(), cluster.KubeKeyNamespace, cluster.KubeKeyName, cluster.ID); err != nil {
		log.WithError(err).Errorf("Fail to create Agent CR. Namespace: %s, Cluster: %s, HostID: %s", cluster.KubeKeyNamespace, cluster.KubeKeyName, params.NewHostParams.HostID.String())
		return installer.NewRegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	txSuccess = true

	return installer.NewRegisterHostCreated().WithPayload(&hostRegistration)
}

func (b *bareMetalInventory) generateNextStepRunnerCommand(ctx context.Context, params *installer.RegisterHostParams) *models.HostRegistrationResponseAO1NextStepRunnerCommand {

	currentImageTag := extractImageTag(b.AgentDockerImg)
	if params.NewHostParams.DiscoveryAgentVersion != currentImageTag {
		log := logutil.FromContext(ctx, b.log)
		log.Infof("Host %s in cluster %s has outdated agent image %s, updating to %s",
			params.NewHostParams.HostID.String(), params.ClusterID.String(), params.NewHostParams.DiscoveryAgentVersion, currentImageTag)
	}

	config := hostcommands.NextStepRunnerConfig{
		ServiceBaseURL:       b.ServiceBaseURL,
		ClusterID:            params.ClusterID.String(),
		HostID:               params.NewHostParams.HostID.String(),
		UseCustomCACert:      b.ServiceCACertPath != "",
		NextStepRunnerImage:  b.AgentDockerImg,
		SkipCertVerification: b.SkipCertVerification,
	}
	command, args := hostcommands.GetNextStepRunnerCommand(&config)
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
		return installer.NewRegisterHostForbidden().WithPayload(
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

	if err := b.hostApi.UnRegisterHost(ctx, params.HostID.String(), params.ClusterID.String()); err != nil {
		// TODO: check error type
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// TODO: need to check that host can be deleted from the cluster
	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s: deregistered from cluster", params.HostID.String()), time.Now())
	return nil
}

func (b *bareMetalInventory) GetHost(_ context.Context, params installer.GetHostParams) middleware.Responder {
	// TODO: validate what is the error
	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())

	if err != nil {
		return installer.NewGetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if err := b.customizeHost(&host.Host); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// Clear this field as it is not needed to be sent via API
	host.FreeAddresses = ""
	return installer.NewGetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var hosts []*models.Host
	if err := b.db.Find(&hosts, "cluster_id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get list of hosts for cluster %s", params.ClusterID)
		return installer.NewListHostsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, host := range hosts {
		if err := b.customizeHost(host); err != nil {
			return common.GenerateErrorResponder(err)
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

	return installer.NewListHostsOK().WithPayload(hosts)
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

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom installer arguments were applied", hostutil.GetHostnameForMsg(&h.Host)), time.Now())
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

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
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
	host, err := common.GetHostFromDB(tx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to find host: %s", params.HostID)
		return installer.NewGetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	host.CheckedInAt = strfmt.DateTime(time.Now())
	if err = tx.Model(&host).UpdateColumn("checked_in_at", host.CheckedInAt).Error; err != nil {
		log.WithError(err).Errorf("failed to update host: %s", params.ClusterID)
		return installer.NewGetNextStepsInternalServerError()
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewGetNextStepsInternalServerError()
	}
	txSuccess = true

	steps, err = b.hostApi.GetNextSteps(ctx, &host.Host)
	if err != nil {
		log.WithError(err).Errorf("failed to get steps for host %s cluster %s", params.HostID, params.ClusterID)
	}

	return installer.NewGetNextStepsOK().WithPayload(&steps)
}

func shouldHandle(params installer.PostStepReplyParams) bool {
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

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())

	if err != nil {
		log.WithError(err).Errorf("Failed to find host <%s> cluster <%s> step <%s> exit code %d stdout <%s> stderr <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)
		return installer.NewPostStepReplyNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	logReplyReceived(params, log, host)

	if params.Reply.ExitCode != 0 {
		handlingError := b.handleReplyError(params, ctx, log, &host.Host, params.Reply.ExitCode)
		if handlingError != nil {
			log.WithError(handlingError).Errorf("Failed handling reply error for host <%s> cluster <%s>", params.HostID, params.ClusterID)
		}
		return installer.NewPostStepReplyNoContent()
	}

	if !shouldHandle(params) {
		return installer.NewPostStepReplyNoContent()
	}

	stepReply, err := filterReplyByType(params)
	if err != nil {
		log.WithError(err).Errorf("Failed decode <%s> reply for host <%s> cluster <%s>",
			params.Reply.StepID, params.HostID, params.ClusterID)
		return installer.NewPostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = handleReplyByType(params, b, ctx, host.Host, stepReply)
	if err != nil {
		log.WithError(err).Errorf("Failed to update step reply for host <%s> cluster <%s> step <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID)
		return installer.NewPostStepReplyInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewPostStepReplyNoContent()
}

func (b *bareMetalInventory) handleReplyError(params installer.PostStepReplyParams, ctx context.Context, log logrus.FieldLogger, h *models.Host, exitCode int64) error {
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
	case models.StepTypeInstallationDiskSpeedCheck:
		stepReply, err := filterReplyByType(params)
		if err != nil {
			return err
		}
		return b.processDiskSpeedCheckResponse(ctx, h, stepReply, exitCode)
	}
	return nil
}

func (b *bareMetalInventory) handleMediaDisconnection(params installer.PostStepReplyParams, ctx context.Context, log logrus.FieldLogger, h *models.Host) error {
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

	_, err := hostutil.UpdateHostStatus(ctx, log, b.db, b.eventsHandler, h.ClusterID, *h.ID,
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
	var (
		err                   error
		dhcpAllocationReponse models.DhcpAllocationResponse
		cluster               common.Cluster
	)
	log := logutil.FromContext(ctx, b.log)
	if err = b.db.Take(&cluster, "id = ?", host.ClusterID.String()).Error; err != nil {
		log.WithError(err).Warnf("Get cluster %s", host.ClusterID.String())
		return err
	}
	if !swag.BoolValue(cluster.VipDhcpAllocation) {
		err = errors.Errorf("DHCP not enabled in cluster %s", host.ClusterID.String())
		log.WithError(err).Warn("processDhcpAllocationResponse")
		return err
	}
	if err = json.Unmarshal([]byte(dhcpAllocationResponseStr), &dhcpAllocationReponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal dhcp allocation from host %s", host.ID.String())
		return err
	}
	apiVip := dhcpAllocationReponse.APIVipAddress.String()
	ingressVip := dhcpAllocationReponse.IngressVipAddress.String()
	isApiVipInMachineCIDR, err := network.IpInCidr(apiVip, cluster.MachineNetworkCidr)
	if err != nil {
		log.WithError(err).Warn("Ip in CIDR for API VIP")
		return err
	}

	isIngressVipInMachineCIDR, err := network.IpInCidr(ingressVip, cluster.MachineNetworkCidr)
	if err != nil {
		log.WithError(err).Warn("Ip in CIDR for Ingress VIP")
		return err
	}

	if !(isApiVipInMachineCIDR && isIngressVipInMachineCIDR) {
		err = errors.Errorf("At least of the IPs (%s, %s) is not in machine CIDR %s", apiVip, ingressVip, cluster.MachineNetworkCidr)
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
	return b.clusterApi.SetVipsData(ctx, &cluster, apiVip, ingressVip, dhcpAllocationReponse.APIVipLease, dhcpAllocationReponse.IngressVipLease, b.db)
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
			b.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityWarning, msg, time.Now())
		}
	}

	return b.hostApi.SetDiskSpeed(ctx, h, diskPerfCheckResponse.Path, diskPerfCheckResponse.IoSyncDuration, exitCode, nil)
}

func (b *bareMetalInventory) getInstallationDiskSpeedThresholdMs(ctx context.Context, h *models.Host) (int64, error) {
	cluster, err := common.GetClusterFromDB(b.db, h.ClusterID, common.UseEagerLoading)
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

func handleReplyByType(params installer.PostStepReplyParams, b *bareMetalInventory, ctx context.Context, host models.Host, stepReply string) error {
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
	}
	return err
}

func logReplyReceived(params installer.PostStepReplyParams, log logrus.FieldLogger, host *common.Host) {
	if !shouldStepReplyBeLogged(params, host) {
		return
	}

	message := fmt.Sprintf("Received step reply <%s> from cluster <%s> host <%s> exit-code <%d> stderr <%s>",
		params.Reply.StepID, params.ClusterID, params.HostID, params.Reply.ExitCode, params.Reply.Error)
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

func shouldStepReplyBeLogged(params installer.PostStepReplyParams, host *common.Host) bool {
	// Host with a disconnected ISO device is unstable and all the steps should be failed
	// Currently the assisted-service logs are full with the media disconnection errors.
	// Here we are filtering these errors and log the message once per host.
	// TODO: Create a new state "unstable" in the state machine with no commands.
	// TODO: Maybe we should collect the logs even in this state.
	notFirstMediaDisconnectionFailure := *host.Status == models.HostStatusError && host.StatusInfo != nil &&
		strings.Contains(*host.StatusInfo, mediaDisconnectionMessage)
	return !(params.Reply.ExitCode == MediaDisconnected && notFirstMediaDisconnectionFailure)
}

func filterReplyByType(params installer.PostStepReplyParams) (string, error) {
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
	tx = transaction.AddForUpdateQueryOption(tx)

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

	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to disable host %s: error fetching host from DB", params.HostID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.hostApi.DisableHost(ctx, &host.Host, tx); err != nil {
		log.WithError(err).Errorf("failed to disable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		msg := fmt.Sprintf("Failed to disable host %s: error disabling host in current status",
			hostutil.GetHostnameForMsg(&host.Host))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "disable host", &params.HostID, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s disabled by user", hostutil.GetHostnameForMsg(&host.Host)), time.Now())

	c, err = b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *c.ID})
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
	tx = transaction.AddForUpdateQueryOption(tx)

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

	host, err := common.GetHostFromDB(tx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to enable host %s: error fetching host from DB", params.HostID)
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err = b.hostApi.EnableHost(ctx, &host.Host, tx); err != nil {
		log.WithError(err).Errorf("failed to enable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		msg := fmt.Sprintf("Failed to enable host %s: error disabling host in current status",
			hostutil.GetHostnameForMsg(&host.Host))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "enable host", &params.HostID, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = tx.Commit().Error; err != nil {
		log.Error(err)
		return common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s enabled by user", hostutil.GetHostnameForMsg(&host.Host)), time.Now())

	c, err = b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *c.ID})
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewEnableHostOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) refreshHostAndClusterStatuses(
	ctx context.Context,
	eventName string,
	hostID *strfmt.UUID,
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
			b.eventsHandler.AddEvent(
				ctx,
				*clusterID,
				hostID,
				models.EventSeverityError,
				err.Error(),
				time.Now())
		}
	}()
	err = b.setMajorityGroupForCluster(clusterID, db)
	if err != nil {
		return nil, err
	}
	err = b.refreshHostStatus(ctx, hostID, clusterID, db)
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

func (b *bareMetalInventory) GetPresignedForClusterFiles(ctx context.Context, params installer.GetPresignedForClusterFilesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	// Presigned URL only works with AWS S3 because Scality is not exposed
	if !b.objectHandler.IsAwsS3() {
		return common.NewApiError(http.StatusBadRequest, errors.New("Failed to generate presigned URL: invalid backend"))
	}
	var err error
	fullFileName := fmt.Sprintf("%s/%s", params.ClusterID, params.FileName)
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

	if params.FileName == "logs" {
		if params.HostID != nil && swag.StringValue(params.LogsType) == "" {
			logsType := string(models.LogsTypeHost)
			params.LogsType = &logsType
		}
		fullFileName, downloadFilename, err = b.getLogFileForDownload(ctx, &params.ClusterID, params.HostID, swag.StringValue(params.LogsType))
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
	return installer.NewGetPresignedForClusterFilesOK().WithPayload(&models.Presigned{URL: &url})
}

func (b *bareMetalInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
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
		hostObject, err = b.getHost(ctx, clusterId.String(), hostId.String())
		if err != nil {
			return "", "", err
		}
		if hostObject.LogsCollectedAt == strfmt.DateTime(time.Time{}) {
			return "", "", common.NewApiError(http.StatusConflict, errors.Errorf("Logs for host %s were not found", hostId))
		}
		fileName = b.getLogsFullName(clusterId.String(), hostObject.ID.String())
		role := string(hostObject.Role)
		if hostObject.Bootstrap {
			role = string(models.HostRoleBootstrap)
		}
		downloadFileName = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), role, sanitize.Name(hostutil.GetHostnameForMsg(&hostObject.Host)))
	case string(models.LogsTypeController):
		if c.Cluster.ControllerLogsCollectedAt == strfmt.DateTime(time.Time{}) {
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
	case manifests.ManifestFolder, "discovery.ign":
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

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom discovery ignition config was applied", hostutil.GetHostnameForMsg(&h.Host)), time.Now())
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
	c, err := b.GetCredentialsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetCredentialsOK().WithPayload(c)
}

func (b *bareMetalInventory) GetCredentialsInternal(ctx context.Context, params installer.GetCredentialsParams) (*models.Credentials, error) {

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
		msg := fmt.Sprintf("Host %s: %s", hostutil.GetHostnameForMsg(&host.Host), event)
		b.eventsHandler.AddEvent(ctx, host.ClusterID, host.ID, models.EventSeverityInfo, msg, time.Now())
	}

	return installer.NewUpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("UploadClusterIngressCert for cluster %s with params %s", params.ClusterID, params.IngressCertParams)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return installer.NewUploadClusterIngressCertNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewUploadClusterIngressCertInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.UploadIngressCert(&cluster); err != nil {
		return installer.NewUploadClusterIngressCertBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	objectName := fmt.Sprintf("%s/%s", cluster.ID, constants.Kubeconfig)
	exists, err := b.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload ingress ca")
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if exists {
		log.Infof("Ingress ca for cluster %s already exists", cluster.ID)
		return installer.NewUploadClusterIngressCertCreated()
	}

	noingress := fmt.Sprintf("%s/%s-noingress", cluster.ID, constants.Kubeconfig)
	resp, _, err := b.objectHandler.Download(ctx, noingress)
	if err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	kubeconfigData, err := ioutil.ReadAll(resp)
	if err != nil {
		log.WithError(err).Infof("Failed to convert kubeconfig s3 response to io reader")
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	mergedKubeConfig, err := mergeIngressCaIntoKubeconfig(kubeconfigData, []byte(params.IngressCertParams), log)
	if err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.objectHandler.Upload(ctx, mergedKubeConfig, objectName); err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.Errorf("failed to upload %s to s3", objectName)))
	}
	return installer.NewUploadClusterIngressCertCreated()
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

func (b *bareMetalInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	c, err := b.CancelInstallationInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewCancelInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) CancelInstallationInternal(ctx context.Context, params installer.CancelInstallationParams) (*common.Cluster, error) {

	log := logutil.FromContext(ctx, b.log)
	log.Infof("canceling installation for cluster %s", params.ClusterID)

	cluster := &common.Cluster{}

	txSuccess := false
	tx := b.db.Begin()
	tx = transaction.AddForUpdateQueryOption(tx)
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
		b.eventsHandler.AddEvent(ctx, *cluster.ID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New(msg))
	}

	var err error
	if cluster, err = common.GetClusterFromDB(tx, params.ClusterID, common.UseEagerLoading); err != nil {
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
		if err := b.customizeHost(h); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf("Failed to cancel installation: error committing DB transaction (%s)", err)
		msg := "Failed to cancel installation: error committing DB transaction"
		b.eventsHandler.AddEvent(ctx, *cluster.ID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	return cluster, nil
}

func (b *bareMetalInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("resetting cluster %s", params.ClusterID)

	var cluster *common.Cluster

	txSuccess := false
	tx := b.db.Begin()
	tx = transaction.AddForUpdateQueryOption(tx)
	defer func() {
		if !txSuccess {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	var err error
	if cluster, err = common.GetClusterFromDB(tx, params.ClusterID, common.UseEagerLoading); err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return installer.NewResetClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.clusterApi.ResetCluster(ctx, cluster, "cluster was reset by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}

	for _, h := range cluster.Hosts {
		if err := b.hostApi.ResetHost(ctx, h, "cluster was reset by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.DeleteClusterFiles(ctx, cluster, b.objectHandler); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err := b.deleteDNSRecordSets(ctx, *cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewResetClusterAccepted().WithPayload(&cluster.Cluster)
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

	err = b.hostApi.AutoAssignRole(ctx, h, b.db)
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

func (b *bareMetalInventory) ResetHost(ctx context.Context, params installer.ResetHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Info("Resetting host: ", params.HostID)
	host, err := common.GetHostFromDB(b.db, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to reset host %s: error fetching host from DB", params.HostID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if !hostutil.IsDay2Host(&host.Host) {
		log.Errorf("ResetHost for host %s is forbidden: not a Day2 hosts", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("method only allowed when adding hosts to an existing cluster"))
	}

	err = b.db.Transaction(func(tx *gorm.DB) error {
		if errResponse := b.hostApi.ResetHost(ctx, &host.Host, "host was reset by user", tx); errResponse != nil {
			return errResponse
		}
		if err = b.customizeHost(&host.Host); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewResetHostOK().WithPayload(&host.Host)
}

func (b *bareMetalInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	// TODO: MGMT-4458
	// This function can be removed once the controller will stop sending this request
	// The service is already capable of completing the installation on its own

	log := logutil.FromContext(ctx, b.log)

	log.Infof("complete cluster %s installation", params.ClusterID)

	var cluster *common.Cluster
	var err error
	if cluster, err = common.GetClusterFromDB(b.db, params.ClusterID, common.UseEagerLoading); err != nil {
		return common.GenerateErrorResponder(err)
	}

	if !*params.CompletionParams.IsSuccess {
		if _, err := b.clusterApi.CompleteInstallation(ctx, b.db, cluster, false, params.CompletionParams.ErrorInfo); err != nil {
			log.WithError(err).Errorf("Failed to set complete cluster state on %s ", params.ClusterID.String())
			return common.GenerateErrorResponder(err)
		}
	} else {
		log.Warnf("Cluster %s tried to complete its installation using deprecated CompleteInstallation API. The service decides whether the cluster completed", params.ClusterID)
	}

	return installer.NewCompleteInstallationAccepted().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) deleteDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.dnsApi.DeleteDNSRecordSets(ctx, &cluster)
}

func (b *bareMetalInventory) validateDNSDomain(cluster common.Cluster, params installer.UpdateClusterParams, log logrus.FieldLogger) error {
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
	var err error
	var currentCluster *common.Cluster

	log := logutil.FromContext(ctx, b.log)
	log.Infof("update log progress on %s cluster to %s", params.ClusterID, params.LogsProgressParams.LogsState)
	currentCluster, err = b.getCluster(ctx, params.ClusterID.String())
	if err == nil {
		err = b.clusterApi.UpdateLogsProgress(ctx, currentCluster, string(params.LogsProgressParams.LogsState))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on cluster %s", params.LogsProgressParams.LogsState, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return installer.NewUpdateClusterLogsProgressNoContent()
}

func (b *bareMetalInventory) UpdateHostLogsProgress(ctx context.Context, params installer.UpdateHostLogsProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("update log progress on host %s on %s cluster to %s", params.HostID, params.ClusterID, params.LogsProgressParams.LogsState)
	currentHost, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err == nil {
		err = b.hostApi.UpdateLogsProgress(ctx, &currentHost.Host, string(params.LogsProgressParams.LogsState))
	}
	if err != nil {
		b.log.WithError(err).Errorf("failed to update log progress %s on cluster %s host %s", params.LogsProgressParams.LogsState, params.ClusterID.String(), params.HostID.String())
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateHostLogsProgressNoContent()
}

func (b *bareMetalInventory) UploadLogs(ctx context.Context, params installer.UploadLogsParams) middleware.Responder {
	err := b.uploadLogs(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUploadLogsNoContent()
}

func (b *bareMetalInventory) uploadLogs(ctx context.Context, params installer.UploadLogsParams) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Uploading logs from host %s in cluster %s", params.HostID, params.ClusterID)

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
		err := b.uploadHostLogs(ctx, params.ClusterID.String(), params.HostID.String(), params.Upfile)
		if err != nil {
			return err
		}
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
	}

	log.Infof("Done uploading file %s", fileName)
	return nil
}

func (b *bareMetalInventory) uploadHostLogs(ctx context.Context, clusterId string, hostId string, upFile io.ReadCloser) error {
	log := logutil.FromContext(ctx, b.log)
	currentHost, err := b.getHost(ctx, clusterId, hostId)
	if err != nil {
		return err
	}

	fileName := b.getLogsFullName(clusterId, hostId)

	log.Debugf("Start upload log file %s to bucket %s", fileName, b.S3Bucket)
	err = b.objectHandler.UploadStream(ctx, upFile, fileName)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload %s to s3 for host %s", fileName, hostId)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	err = b.hostApi.SetUploadLogsAt(ctx, &currentHost.Host, b.db)
	if err != nil {
		log.WithError(err).Errorf("Failed update host %s logs_collected_at flag", hostId)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	err = b.hostApi.UpdateLogsProgress(ctx, &currentHost.Host, string(models.LogsStateCollecting))
	if err != nil {
		log.WithError(err).Errorf("Failed update host %s log progress %s", hostId, string(models.LogsStateCollecting))
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (b *bareMetalInventory) DownloadClusterLogs(ctx context.Context, params installer.DownloadClusterLogsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Downloading logs from cluster %s", params.ClusterID)
	fileName, downloadFileName, err := b.getLogFileForDownload(ctx, &params.ClusterID, params.HostID, swag.StringValue(params.LogsType))
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	respBody, contentLength, err := b.objectHandler.Download(ctx, fileName)
	if err != nil {
		if _, ok := err.(common.NotFound); ok {
			log.WithError(err).Warnf("File not found %s", fileName)
			return common.NewApiError(http.StatusNotFound, errors.Errorf("Logs of type %s for cluster %s "+
				"were not found", swag.StringValue(params.LogsType), params.ClusterID))
		}
		log.WithError(err).Errorf("failed to download file %s", fileName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterLogsOK().WithPayload(respBody), downloadFileName, contentLength)
}

func (b *bareMetalInventory) UploadHostLogs(ctx context.Context, params installer.UploadHostLogsParams) middleware.Responder {
	err := b.uploadLogs(ctx, installer.UploadLogsParams{ClusterID: params.ClusterID, HostID: &params.HostID, HTTPRequest: params.HTTPRequest,
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

func (b *bareMetalInventory) GetCommonHostInternal(_ context.Context, clusterId, hostId string) (*common.Host, error) {
	return common.GetHostFromDB(b.db, clusterId, hostId)
}

func (b *bareMetalInventory) UpdateHostApprovedInternal(ctx context.Context, clusterId, hostId string, approved bool) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Updating Approved to %t Host %s Cluster %s", approved, hostId, clusterId)
	dbHost, err := b.GetCommonHostInternal(ctx, clusterId, hostId)
	if err != nil {
		return err
	}
	err = b.db.Model(&common.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and cluster_id = ?"), hostId, clusterId).Update("approved", approved).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update 'approved' in host: %s", hostId)
		return err
	}
	b.eventsHandler.AddEvent(ctx, strfmt.UUID(clusterId), dbHost.ID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s: updated approved to %t", hostutil.GetHostnameForMsg(&dbHost.Host), approved), time.Now())
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

func (b *bareMetalInventory) customizeHost(host *models.Host) error {
	b.customizeHostStages(host)
	b.customizeHostname(host)
	return nil
}

func (b *bareMetalInventory) customizeHostStages(host *models.Host) {
	host.ProgressStages = b.hostApi.GetStagesByRole(host.Role, host.Bootstrap)
}

func (b *bareMetalInventory) customizeHostname(host *models.Host) {
	host.RequestedHostname = hostutil.GetHostnameForMsg(host)
}

func proxySettingsChanged(params *models.ClusterUpdateParams, cluster *common.Cluster) bool {
	if (params.HTTPProxy != nil && cluster.HTTPProxy != swag.StringValue(params.HTTPProxy)) ||
		(params.HTTPSProxy != nil && cluster.HTTPSProxy != swag.StringValue(params.HTTPSProxy)) ||
		(params.NoProxy != nil && cluster.NoProxy != swag.StringValue(params.NoProxy)) {
		return true
	}
	return false
}

// computes the cluster proxy hash in order to identify if proxy settings were changed which will indicated if
// new ISO file should be generated to contain new proxy settings
func computeClusterProxyHash(httpProxy, httpsProxy, noProxy *string) (string, error) {
	var proxyHash string
	if httpProxy != nil {
		proxyHash += *httpProxy
	}
	if httpsProxy != nil {
		proxyHash += *httpsProxy
	}
	if noProxy != nil {
		proxyHash += *noProxy
	}
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
	return b.hostApi.GetHostByKubeKey(key)
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
	cluster, err := b.getCluster(ctx, params.ClusterID.String(), common.UseEagerLoading)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	requirements, err := b.hwValidator.GetPreflightHardwareRequirements(ctx, cluster)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetPreflightRequirementsOK().WithPayload(requirements)
}

func (b *bareMetalInventory) GetHostRequirements(_ context.Context, params installer.GetHostRequirementsParams) middleware.Responder {
	requirements, err := b.hwValidator.GetDefaultVersionRequirements()
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if swag.BoolValue(params.SingleNode) {
		return installer.NewGetHostRequirementsOK().WithPayload(
			&models.HostRequirements{
				Master: hostRequirementsRoleFrom(requirements.SNORequirements),
				Worker: hostRequirementsRoleFrom(requirements.WorkerRequirements),
			})
	}

	return installer.NewGetHostRequirementsOK().WithPayload(
		&models.HostRequirements{
			Master: hostRequirementsRoleFrom(requirements.MasterRequirements),
			Worker: hostRequirementsRoleFrom(requirements.WorkerRequirements),
		})
}

func hostRequirementsRoleFrom(requirements *models.ClusterHostRequirementsDetails) *models.HostRequirementsRole {
	return &models.HostRequirementsRole{
		CPUCores:                         requirements.CPUCores,
		DiskSizeGb:                       requirements.DiskSizeGb,
		InstallationDiskSpeedThresholdMs: requirements.InstallationDiskSpeedThresholdMs,
		RAMGib:                           conversions.MibToGiB(requirements.RAMMib),
		NetworkLatencyThresholdMs:        requirements.NetworkLatencyThresholdMs,
		PacketLossPercentage:             requirements.PacketLossPercentage,
	}
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

func (b *bareMetalInventory) AddOpenshiftVersion(ctx context.Context, ocpReleaseImage, pullSecret string) (*models.OpenshiftVersion, error) {
	log := logutil.FromContext(ctx, b.log)

	// Create a new OpenshiftVersion and add it to versions cache
	openshiftVersion, err := b.versionsHandler.AddOpenshiftVersion(ocpReleaseImage, pullSecret)
	if err != nil {
		log.WithError(err).Errorf("Failed to add OCP version for release image: %s", ocpReleaseImage)
		return nil, err
	}

	return openshiftVersion, nil
}
