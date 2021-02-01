package bminventory

import (
	"bytes"
	"context"
	"io"

	"k8s.io/apimachinery/pkg/types"

	// #nosec
	"crypto/md5"
	"crypto/x509"
	"encoding/base64"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	ign_3_1 "github.com/coreos/ignition/v2/config/v3_1"
	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/kennygrant/sanitize"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/events"
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
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/leader"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/transaction"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"github.com/vincent-petithory/dataurl"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const kubeconfig = "kubeconfig"

const DefaultUser = "kubeadmin"
const ConsoleUrlPrefix = "https://console-openshift-console.apps"

// 125 is the generic exit code for cases the error is in podman / docker and not the container we tried to run
const ContainerAlreadyRunningExitCode = 125

var (
	DefaultClusterNetworkCidr       = "10.128.0.0/14"
	DefaultClusterNetworkHostPrefix = int64(23)
	DefaultServiceNetworkCidr       = "172.30.0.0/16"
)

type Config struct {
	AgentDockerImg           string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	ServiceBaseURL           string            `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath        string            `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	S3EndpointURL            string            `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket                 string            `envconfig:"S3_BUCKET" default:"test"`
	ImageExpirationTime      time.Duration     `envconfig:"IMAGE_EXPIRATION_TIME" default:"4h"`
	AwsAccessKeyID           string            `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey       string            `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	BaseDNSDomains           map[string]string `envconfig:"BASE_DNS_DOMAINS" default:""`
	SkipCertVerification     bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	InstallRHCa              bool              `envconfig:"INSTALL_RH_CA" default:"false"`
	RhQaRegCred              string            `envconfig:"REGISTRY_CREDS" default:""`
	AgentTimeoutStart        time.Duration     `envconfig:"AGENT_TIMEOUT_START" default:"3m"`
	ServiceIPs               string            `envconfig:"SERVICE_IPS" default:""`
	DeletedUnregisteredAfter time.Duration     `envconfig:"DELETED_UNREGISTERED_AFTER" default:"168h"`
	DefaultNTPSource         string            `envconfig:"NTP_DEFAULT_SERVER"`
	ISOCacheDir              string            `envconfig:"ISO_CACHE_DIR" default:"/tmp/isocache"`
}

const agentMessageOfTheDay = `
**  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  ** **  **  **  **  **  **  **
This is a host being installed by the OpenShift Assisted Installer.
It will be installed from scratch during the installation.
The primary service is agent.service.  To watch its status run e.g
sudo journalctl -u agent.service
**  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  ** **  **  **  **  **  **  **
`

const redhatRootCA = `
-----BEGIN CERTIFICATE-----
MIIENDCCAxygAwIBAgIJANunI0D662cnMA0GCSqGSIb3DQEBCwUAMIGlMQswCQYD
VQQGEwJVUzEXMBUGA1UECAwOTm9ydGggQ2Fyb2xpbmExEDAOBgNVBAcMB1JhbGVp
Z2gxFjAUBgNVBAoMDVJlZCBIYXQsIEluYy4xEzARBgNVBAsMClJlZCBIYXQgSVQx
GzAZBgNVBAMMElJlZCBIYXQgSVQgUm9vdCBDQTEhMB8GCSqGSIb3DQEJARYSaW5m
b3NlY0ByZWRoYXQuY29tMCAXDTE1MDcwNjE3MzgxMVoYDzIwNTUwNjI2MTczODEx
WjCBpTELMAkGA1UEBhMCVVMxFzAVBgNVBAgMDk5vcnRoIENhcm9saW5hMRAwDgYD
VQQHDAdSYWxlaWdoMRYwFAYDVQQKDA1SZWQgSGF0LCBJbmMuMRMwEQYDVQQLDApS
ZWQgSGF0IElUMRswGQYDVQQDDBJSZWQgSGF0IElUIFJvb3QgQ0ExITAfBgkqhkiG
9w0BCQEWEmluZm9zZWNAcmVkaGF0LmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEP
ADCCAQoCggEBALQt9OJQh6GC5LT1g80qNh0u50BQ4sZ/yZ8aETxt+5lnPVX6MHKz
bfwI6nO1aMG6j9bSw+6UUyPBHP796+FT/pTS+K0wsDV7c9XvHoxJBJJU38cdLkI2
c/i7lDqTfTcfLL2nyUBd2fQDk1B0fxrskhGIIZ3ifP1Ps4ltTkv8hRSob3VtNqSo
GxkKfvD2PKjTPxDPWYyruy9irLZioMffi3i/gCut0ZWtAyO3MVH5qWF/enKwgPES
X9po+TdCvRB/RUObBaM761EcrLSM1GqHNueSfqnho3AjLQ6dBnPWlo638Zm1VebK
BELyhkLWMSFkKwDmne0jQ02Y4g075vCKvCsCAwEAAaNjMGEwHQYDVR0OBBYEFH7R
4yC+UehIIPeuL8Zqw3PzbgcZMB8GA1UdIwQYMBaAFH7R4yC+UehIIPeuL8Zqw3Pz
bgcZMA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgGGMA0GCSqGSIb3DQEB
CwUAA4IBAQBDNvD2Vm9sA5A9AlOJR8+en5Xz9hXcxJB5phxcZQ8jFoG04Vshvd0e
LEnUrMcfFgIZ4njMKTQCM4ZFUPAieyLx4f52HuDopp3e5JyIMfW+KFcNIpKwCsak
oSoKtIUOsUJK7qBVZxcrIyeQV2qcYOeZhtS5wBqIwOAhFwlCET7Ze58QHmS48slj
S9K0JAcps2xdnGu0fkzhSQxY8GPQNFTlr6rYld5+ID/hHeS76gq0YG3q6RLWRkHf
4eTkRjivAlExrFzKcljC4axKQlnOvVAzz+Gm32U0xPBF4ByePVxCJUHw1TsyTmel
RxNEp7yHoXcwn+fXna+t5JWh1gxUZty3
-----END CERTIFICATE-----`

const selinuxPolicy = `
module assisted 1.0;
require {
        type chronyd_t;
        type container_file_t;
        type spc_t;
        class unix_dgram_socket sendto;
        class dir search;
        class sock_file write;
}
#============= chronyd_t ==============
allow chronyd_t container_file_t:dir search;
allow chronyd_t container_file_t:sock_file write;
allow chronyd_t spc_t:unix_dgram_socket sendto;
`

const ignitionConfigFormat = `{
  "ignition": {
    "version": "3.1.0"{{if .PROXY_SETTINGS}},
    {{.PROXY_SETTINGS}}{{end}}
  },
  "passwd": {
    "users": [
      {{.userSshKey}}
    ]
  },
  "systemd": {
    "units": [{
      "name": "agent.service",
      "enabled": true,
      "contents": "[Service]\nType=simple\nRestart=always\nRestartSec=3\nStartLimitInterval=0\nEnvironment=HTTP_PROXY={{.HTTPProxy}}\nEnvironment=http_proxy={{.HTTPProxy}}\nEnvironment=HTTPS_PROXY={{.HTTPSProxy}}\nEnvironment=https_proxy={{.HTTPSProxy}}\nEnvironment=NO_PROXY={{.NoProxy}}\nEnvironment=no_proxy={{.NoProxy}}{{if .PullSecretToken}}\nEnvironment=PULL_SECRET_TOKEN={{.PullSecretToken}}{{end}}\nTimeoutStartSec={{.AgentTimeoutStartSec}}\nExecStartPre=podman run --privileged --rm -v /usr/local/bin:/hostbin {{.AgentDockerImg}} cp /usr/bin/agent /hostbin\nExecStart=/usr/local/bin/agent --url {{.ServiceBaseURL}} --cluster-id {{.clusterId}} --agent-version {{.AgentDockerImg}} --insecure={{.SkipCertVerification}}  {{if .HostCACertPath}}--cacert {{.HostCACertPath}}{{end}}\n\n[Install]\nWantedBy=multi-user.target"
	},
	{
		"name": "selinux.service",
		"enabled": true,
		"contents": "[Service]\nType=oneshot\nExecStartPre=checkmodule -M -m -o /root/assisted.mod /root/assisted.te\nExecStartPre=semodule_package -o /root/assisted.pp -m /root/assisted.mod\nExecStart=semodule -i /root/assisted.pp\n\n[Install]\nWantedBy=multi-user.target"
	}{{if .StaticIPsData}},
        {
                "name": "configure-static-ip.service",
                "enabled": true,
                "contents": "[Unit]\nDescription=Static IP Configuration\nBefore=NetworkManager.service\nDefaultDependencies=no\n[Service]\nUser=root\nType=oneshot\nTimeoutSec=10\nExecStart=/bin/bash /usr/local/bin/configure-static-ip.sh\nPrivateTmp=yes\nRemainAfterExit=no\n[Install]\nWantedBy=multi-user.target"
        }{{end}}]
  },
  "storage": {
    "files": [{
      "overwrite": true,
      "path": "/etc/motd",
      "mode": 420,
      "user": {
          "name": "root"
      },
      "contents": { "source": "data:,{{.AGENT_MOTD}}" }
	},
    {
      "overwrite": true,
      "path": "/etc/NetworkManager/conf.d/01-ipv6.conf",
      "mode": 420,
      "user": {
          "name": "root"
      },
      "contents": { "source": "data:,{{.IPv6_CONF}}" }
    },
	{
		"overwrite": true,
		"path": "/root/.docker/config.json",
		"mode": 420,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:,{{.PULL_SECRET}}" }
	},
	{
		"overwrite": true,
		"path": "/root/assisted.te",
		"mode": 420,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:text/plain;base64,{{.SELINUX_POLICY}}" }
	}{{if .StaticIPsData}},
	{
		"path": "/etc/static_ips_config.csv",
		"mode": 420,
		"overwrite": true,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:text/plain;base64,{{.StaticIPsData}}" }
        },
	{
		"path": "/usr/local/bin/configure-static-ip.sh",
		"mode": 493,
		"overwrite": true,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:text/plain;base64,{{.StaticIPsConfigScript}}"}
        }{{end}}{{if .RH_ROOT_CA}},
	{
	  "overwrite": true,
	  "path": "/etc/pki/ca-trust/source/anchors/rh-it-root-ca.crt",
	  "mode": 420,
	  "user": {
	      "name": "root"
	  },
	  "contents": { "source": "data:,{{.RH_ROOT_CA}}" }
	}{{end}}{{if .HostCACertPath}},
	{
	  "path": "{{.HostCACertPath}}",
	  "mode": 420,
	  "overwrite": true,
	  "user": {
		"name": "root"
	  },
	  "contents": { "source": "{{.ServiceCACertData}}" }
	}{{end}}{{if .ServiceIPs}},
	{
	  "path": "/etc/hosts",
	  "mode": 420,
	  "user": {
	    "name": "root"
	  },
	  "append": [{ "source": "{{.ServiceIPs}}" }]
  	}{{end}}]
  }
}`

const nodeIgnitionFormat = `{
  "ignition": {
    "version": "3.1.0",
    "config": {
      "merge": [{
        "source": "{{.SOURCE}}"
      }]
    }
  }
}`

type OCPClusterAPI interface {
	RegisterOCPCluster(ctx context.Context) error
}

//go:generate mockgen -package bminventory -destination mock_installer_internal.go . InstallerInternals
type InstallerInternals interface {
	RegisterClusterInternal(ctx context.Context, kubeKey *types.NamespacedName, params installer.RegisterClusterParams) (*common.Cluster, error)
	GetClusterInternal(ctx context.Context, params installer.GetClusterParams) (*common.Cluster, error)
	UpdateClusterInternal(ctx context.Context, params installer.UpdateClusterParams) (*common.Cluster, error)
	GenerateClusterISOInternal(ctx context.Context, params installer.GenerateClusterISOParams) (*common.Cluster, error)
	GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error)
	InstallClusterInternal(ctx context.Context, params installer.InstallClusterParams) (*common.Cluster, error)
	DeregisterClusterInternal(ctx context.Context, params installer.DeregisterClusterParams) error
}

type bareMetalInventory struct {
	Config
	db               *gorm.DB
	log              logrus.FieldLogger
	hostApi          host.API
	clusterApi       cluster.API
	eventsHandler    events.Handler
	objectHandler    s3wrapper.API
	metricApi        metrics.API
	generator        generator.ISOInstallConfigGenerator
	authHandler      auth.AuthHandler
	k8sClient        k8sclient.K8SClient
	leaderElector    leader.Leader
	secretValidator  validations.PullSecretValidator
	versionsHandler  versions.Handler
	isoEditorFactory isoeditor.Factory
}

func (b *bareMetalInventory) UpdateClusterInstallProgress(ctx context.Context, params installer.UpdateClusterInstallProgressParams) middleware.Responder {
	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	if err := b.clusterApi.UpdateInstallProgress(ctx, c, params.ClusterProgress); err != nil {
		b.log.WithError(err).Error("Failed to update install progress")
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateClusterInstallProgressNoContent()
}

func NewBareMetalInventory(
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi cluster.API,
	cfg Config,
	generator generator.ISOInstallConfigGenerator,
	eventsHandler events.Handler,
	objectHandler s3wrapper.API,
	metricApi metrics.API,
	authHandler auth.AuthHandler,
	k8sClient k8sclient.K8SClient,
	leaderElector leader.Leader,
	pullSecretValidator validations.PullSecretValidator,
	versionsHandler versions.Handler,
	isoEditorFactory isoeditor.Factory,
) *bareMetalInventory {
	return &bareMetalInventory{
		db:               db,
		log:              log,
		Config:           cfg,
		hostApi:          hostApi,
		clusterApi:       clusterApi,
		generator:        generator,
		eventsHandler:    eventsHandler,
		objectHandler:    objectHandler,
		metricApi:        metricApi,
		authHandler:      authHandler,
		k8sClient:        k8sClient,
		leaderElector:    leaderElector,
		secretValidator:  pullSecretValidator,
		versionsHandler:  versionsHandler,
		isoEditorFactory: isoEditorFactory,
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

func (b *bareMetalInventory) formatIgnitionFile(cluster *common.Cluster, params installer.GenerateClusterISOParams, logger logrus.FieldLogger, safeForLogs bool) (string, error) {
	creds, err := validations.ParsePullSecret(cluster.PullSecret)
	if err != nil {
		return "", err
	}
	pullSecretToken := ""
	if b.authHandler.EnableAuth {
		r, ok := creds["cloud.openshift.com"]
		if !ok {
			return "", errors.Errorf("Pull secret does not contain auth for cloud.openshift.com")
		}
		pullSecretToken = r.AuthRaw
	}

	proxySettings, err := proxySettingsForIgnition(cluster.HTTPProxy, cluster.HTTPSProxy, cluster.NoProxy)
	if err != nil {
		return "", err
	}
	rhCa := ""
	if b.Config.InstallRHCa {
		rhCa = url.PathEscape(redhatRootCA)
	}
	var ignitionParams = map[string]string{
		"userSshKey":           b.getUserSshKey(params),
		"AgentDockerImg":       b.AgentDockerImg,
		"ServiceBaseURL":       strings.TrimSpace(b.ServiceBaseURL),
		"clusterId":            cluster.ID.String(),
		"PullSecretToken":      pullSecretToken,
		"AGENT_MOTD":           url.PathEscape(agentMessageOfTheDay),
		"IPv6_CONF":            url.PathEscape(common.Ipv6DuidDiscoveryConf),
		"PULL_SECRET":          url.PathEscape(cluster.PullSecret),
		"RH_ROOT_CA":           rhCa,
		"PROXY_SETTINGS":       proxySettings,
		"HTTPProxy":            cluster.HTTPProxy,
		"HTTPSProxy":           cluster.HTTPSProxy,
		"NoProxy":              cluster.NoProxy,
		"SkipCertVerification": strconv.FormatBool(b.SkipCertVerification),
		"AgentTimeoutStartSec": strconv.FormatInt(int64(b.AgentTimeoutStart.Seconds()), 10),
		"SELINUX_POLICY":       base64.StdEncoding.EncodeToString([]byte(selinuxPolicy)),
	}
	if safeForLogs {
		for _, key := range []string{"userSshKey", "PullSecretToken", "PULL_SECRET", "RH_ROOT_CA"} {
			ignitionParams[key] = "*****"
		}
	}
	if b.ServiceCACertPath != "" {
		var caCertData []byte
		caCertData, err = ioutil.ReadFile(b.ServiceCACertPath)
		if err != nil {
			return "", err
		}
		ignitionParams["ServiceCACertData"] = dataurl.EncodeBytes(caCertData)
		ignitionParams["HostCACertPath"] = common.HostCACertPath
	}
	if b.Config.ServiceIPs != "" {
		ignitionParams["ServiceIPs"] = dataurl.EncodeBytes([]byte(ignition.GetServiceIPHostnames(b.Config.ServiceIPs)))
	}

	if cluster.ImageInfo.StaticIpsConfig != "" && params.ImageCreateParams.ImageType == models.ImageTypeFullIso {
		ignitionParams["StaticIPsData"] = b64.StdEncoding.EncodeToString([]byte(cluster.ImageInfo.StaticIpsConfig))
		ignitionParams["StaticIPsConfigScript"] = b64.StdEncoding.EncodeToString([]byte(constants.ConfigStaticIpsScript))
	}

	tmpl, err := template.New("ignitionConfig").Parse(ignitionConfigFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, ignitionParams); err != nil {
		return "", err
	}

	res := buf.String()
	if cluster.IgnitionConfigOverrides != "" {
		res, err = ignition.MergeIgnitionConfig(buf.Bytes(), []byte(cluster.IgnitionConfigOverrides))
		if err != nil {
			return "", err
		}
		logger.Infof("Applying ignition overrides %s for cluster %s, resulting ignition: %s", cluster.IgnitionConfigOverrides, cluster.ID, res)
	}

	return res, nil
}

func (b *bareMetalInventory) getUserSshKey(params installer.GenerateClusterISOParams) string {
	sshKey := params.ImageCreateParams.SSHPublicKey
	if sshKey == "" {
		return ""
	}
	return fmt.Sprintf(`{
		"name": "core",
		"passwordHash": "!",
		"sshAuthorizedKeys": [
		"%s"],
		"groups": [ "sudo" ]}`, sshKey)
}

func (b *bareMetalInventory) GetDiscoveryIgnition(ctx context.Context, params installer.GetDiscoveryIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	isoParams := installer.GenerateClusterISOParams{ClusterID: params.ClusterID, ImageCreateParams: &models.ImageCreateParams{}}

	cfg, err := b.formatIgnitionFile(c, isoParams, log, false)
	if err != nil {
		log.WithError(err).Error("Failed to format ignition config")
		return common.GenerateErrorResponder(err)
	}

	configParams := models.DiscoveryIgnitionParams{Config: cfg}
	return installer.NewGetDiscoveryIgnitionOK().WithPayload(&configParams)
}

func (b *bareMetalInventory) UpdateDiscoveryIgnition(ctx context.Context, params installer.UpdateDiscoveryIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	_, report, err := ign_3_1.Parse([]byte(params.DiscoveryIgnitionParams.Config))
	if err != nil {
		log.WithError(err).Errorf("Failed to parse ignition config patch %s", params.DiscoveryIgnitionParams)
		return installer.NewUpdateDiscoveryIgnitionBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	if report.IsFatal() {
		err = errors.Errorf("Ignition config patch %s failed validation: %s", params.DiscoveryIgnitionParams, report.String())
		log.Error(err)
		return installer.NewUpdateDiscoveryIgnitionBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = b.db.Model(&common.Cluster{}).Where(identity.AddUserFilter(ctx, "id = ?"), params.ClusterID).Update("ignition_config_overrides", params.DiscoveryIgnitionParams.Config).Error
	if err != nil {
		return installer.NewUpdateDiscoveryIgnitionInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	existed, err := b.objectHandler.DeleteObject(ctx, getImageName(*c.ID))
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if existed {
		b.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityInfo, "Deleted image from backend because its ignition was updated. The image may be regenerated at any time.", time.Now())
	}

	return installer.NewUpdateDiscoveryIgnitionCreated()
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	c, err := b.RegisterClusterInternal(ctx, nil, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewRegisterClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) RegisterClusterInternal(
	ctx context.Context,
	kubeKey *types.NamespacedName,
	params installer.RegisterClusterParams) (*common.Cluster, error) {

	log := logutil.FromContext(ctx, b.log)
	id := strfmt.UUID(uuid.New().String())
	url := installer.GetClusterURL{ClusterID: id}
	log.Infof("Register cluster: %s with id %s", swag.StringValue(params.NewClusterParams.Name), id)
	success := false
	defer func() {
		if success {
			msg := fmt.Sprintf("Successfully registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
			log.Info(msg)
			b.eventsHandler.AddEvent(ctx, id, nil, models.EventSeverityInfo, msg, time.Now())
		} else {
			log.Errorf("Failed to registered cluster %s with id %s",
				swag.StringValue(params.NewClusterParams.Name), id)
		}
	}()

	if params.NewClusterParams.HTTPProxy != nil &&
		(params.NewClusterParams.HTTPSProxy == nil || *params.NewClusterParams.HTTPSProxy == "") {
		params.NewClusterParams.HTTPSProxy = params.NewClusterParams.HTTPProxy
	}
	if err := validateProxySettings(params.NewClusterParams.HTTPProxy,
		params.NewClusterParams.HTTPSProxy,
		params.NewClusterParams.NoProxy); err != nil {
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if params.NewClusterParams.ClusterNetworkCidr == nil {
		params.NewClusterParams.ClusterNetworkCidr = &DefaultClusterNetworkCidr
	}
	if params.NewClusterParams.ClusterNetworkHostPrefix == 0 {
		params.NewClusterParams.ClusterNetworkHostPrefix = DefaultClusterNetworkHostPrefix
	}
	if params.NewClusterParams.ServiceNetworkCidr == nil {
		params.NewClusterParams.ServiceNetworkCidr = &DefaultServiceNetworkCidr
	}
	if params.NewClusterParams.VipDhcpAllocation == nil {
		params.NewClusterParams.VipDhcpAllocation = swag.Bool(true)
	}
	if params.NewClusterParams.UserManagedNetworking == nil {
		params.NewClusterParams.UserManagedNetworking = swag.Bool(false)
	}

	if swag.BoolValue(params.NewClusterParams.UserManagedNetworking) {
		if swag.BoolValue(params.NewClusterParams.VipDhcpAllocation) {
			err := errors.Errorf("VIP DHCP Allocation cannot be enabled with User Managed Networking")
			log.WithError(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		if params.NewClusterParams.IngressVip != "" {
			err := errors.Errorf("Ingress VIP cannot be set with User Managed Networking")
			log.WithError(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if swag.StringValue(params.NewClusterParams.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		log.Infof("HA mode is None, seeting UserManagedNetworking to true ")
		userManagedNetworking := true
		params.NewClusterParams.UserManagedNetworking = &userManagedNetworking
	}

	if params.NewClusterParams.AdditionalNtpSource == nil {
		params.NewClusterParams.AdditionalNtpSource = &b.Config.DefaultNTPSource
	} else {
		ntpSource := swag.StringValue(params.NewClusterParams.AdditionalNtpSource)

		if ntpSource != "" && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err := errors.Errorf("Invalid NTP source: %s", ntpSource)
			log.WithError(err)
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if !b.versionsHandler.IsOpenshiftVersionSupported(swag.StringValue(params.NewClusterParams.OpenshiftVersion)) {
		return nil, common.NewApiError(http.StatusBadRequest,
			errors.Errorf("Openshift version %s is not supported",
				swag.StringValue(params.NewClusterParams.OpenshiftVersion)))
	}

	if kubeKey == nil {
		kubeKey = &types.NamespacedName{}
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
			OpenshiftVersion:         swag.StringValue(params.NewClusterParams.OpenshiftVersion),
			ServiceNetworkCidr:       swag.StringValue(params.NewClusterParams.ServiceNetworkCidr),
			SSHPublicKey:             params.NewClusterParams.SSHPublicKey,
			UpdatedAt:                strfmt.DateTime{},
			UserName:                 auth.UserNameFromContext(ctx),
			OrgID:                    auth.OrgIDFromContext(ctx),
			EmailDomain:              auth.EmailDomainFromContext(ctx),
			HTTPProxy:                swag.StringValue(params.NewClusterParams.HTTPProxy),
			HTTPSProxy:               swag.StringValue(params.NewClusterParams.HTTPSProxy),
			NoProxy:                  swag.StringValue(params.NewClusterParams.NoProxy),
			VipDhcpAllocation:        params.NewClusterParams.VipDhcpAllocation,
			UserManagedNetworking:    params.NewClusterParams.UserManagedNetworking,
			AdditionalNtpSource:      swag.StringValue(params.NewClusterParams.AdditionalNtpSource),
			Operators:                convertFromClusterOperators(params.NewClusterParams.Operators),
			HighAvailabilityMode:     params.NewClusterParams.HighAvailabilityMode,
		},
		KubeKeyName:      kubeKey.Name,
		KubeKeyNamespace: kubeKey.Namespace,
	}

	if proxyHash, err := computeClusterProxyHash(params.NewClusterParams.HTTPProxy,
		params.NewClusterParams.HTTPSProxy,
		params.NewClusterParams.NoProxy); err != nil {
		log.Error("Failed to compute cluster proxy hash", err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("Failed to compute cluster proxy hash"))
	} else {
		cluster.ProxyHash = proxyHash
	}

	pullSecret := swag.StringValue(params.NewClusterParams.PullSecret)
	err := b.secretValidator.ValidatePullSecret(pullSecret, auth.UserNameFromContext(ctx), b.authHandler)
	if err != nil {
		log.WithError(err).Errorf("Pull secret for new cluster is invalid")
		return nil, common.NewApiError(http.StatusBadRequest, secretValidationToUserError(err))
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

	if sshPublicKey := swag.StringValue(&cluster.SSHPublicKey); sshPublicKey != "" {
		sshPublicKey = strings.TrimSpace(cluster.SSHPublicKey)
		if err = validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		cluster.SSHPublicKey = sshPublicKey
	}

	err = b.clusterApi.RegisterCluster(ctx, &cluster)
	if err != nil {
		log.Errorf("failed to register cluster %s ", swag.StringValue(params.NewClusterParams.Name))
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	success = true
	b.metricApi.ClusterRegistered(swag.StringValue(params.NewClusterParams.OpenshiftVersion), *cluster.ID, cluster.EmailDomain)
	return b.GetClusterInternal(ctx, installer.GetClusterParams{ClusterID: *cluster.ID})
}

func convertFromClusterOperators(operators models.ListOperators) string {
	if operators == nil {
		return ""
	} else {
		var clusterOperators []*models.ClusterOperator
		for _, operator := range operators {
			clusterOperators = append(clusterOperators, &models.ClusterOperator{OperatorType: operator.OperatorType, Enabled: operator.Enabled, Properties: operator.Properties})
		}
		reply, err := json.Marshal(clusterOperators)
		if err != nil {
			return ""
		}
		return string(reply)
	}
}

func (b *bareMetalInventory) RegisterAddHostsCluster(ctx context.Context, params installer.RegisterAddHostsClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	id := params.NewAddHostsClusterParams.ID
	url := installer.GetClusterURL{ClusterID: *id}
	apivipDnsname := swag.StringValue(params.NewAddHostsClusterParams.APIVipDnsname)
	clusterName := swag.StringValue(params.NewAddHostsClusterParams.Name)
	openshiftVersion := swag.StringValue(params.NewAddHostsClusterParams.OpenshiftVersion)

	log.Infof("Register add-hosts-cluster: %s with id %s", clusterName, id.String())

	if cluster.ClusterExists(b.db, *id) {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("AddHostsCluster for AI cluster %s already exists", id))
	}

	newCluster := common.Cluster{Cluster: models.Cluster{
		ID:               id,
		Href:             swag.String(url.String()),
		Kind:             swag.String(models.ClusterKindAddHostsCluster),
		Name:             clusterName,
		OpenshiftVersion: openshiftVersion,
		UserName:         auth.UserNameFromContext(ctx),
		OrgID:            auth.OrgIDFromContext(ctx),
		EmailDomain:      auth.EmailDomainFromContext(ctx),
		UpdatedAt:        strfmt.DateTime{},
		APIVipDNSName:    swag.String(apivipDnsname),
	}}

	err := validations.ValidateClusterNameFormat(clusterName)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	// After registering the cluster, its status should be 'ClusterStatusAddingHosts'
	err = b.clusterApi.RegisterAddHostsCluster(ctx, &newCluster)
	if err != nil {
		log.Errorf("failed to register cluster %s ", clusterName)
		return installer.NewRegisterAddHostsClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	b.metricApi.ClusterRegistered(openshiftVersion, *newCluster.ID, newCluster.EmailDomain)
	return installer.NewRegisterAddHostsClusterCreated().WithPayload(&newCluster.Cluster)
}

func (b *bareMetalInventory) formatNodeIgnitionFile(address string, machineConfigPoolName string) ([]byte, error) {
	var ignitionParams = map[string]string{
		// https://github.com/openshift/machine-config-operator/blob/master/docs/MachineConfigServer.md#endpoint
		"SOURCE": "http://" + address + fmt.Sprintf(":22624/config/%s", machineConfigPoolName),
	}
	tmpl, err := template.New("nodeIgnition").Parse(nodeIgnitionFormat)
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, ignitionParams); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (b *bareMetalInventory) createAndUploadNodeIgnition(ctx context.Context, cluster *common.Cluster, host *models.Host) error {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Starting createAndUploadNodeIgnition for cluster %s, host %s", cluster.ID, host.ID)
	address := cluster.APIVip
	if address == "" {
		address = swag.StringValue(cluster.APIVipDNSName)
	}
	ignitionBytes, err := b.formatNodeIgnitionFile(address, host.MachineConfigPoolName)
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

func (b *bareMetalInventory) DeregisterClusterInternal(ctx context.Context, params installer.DeregisterClusterParams) error {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	log.Infof("Deregister cluster id %s", params.ClusterID)

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	if err := b.deleteDNSRecordSets(ctx, cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	err := b.clusterApi.DeregisterCluster(ctx, &cluster)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister cluster cluster %s", params.ClusterID)
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
	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Started image download", time.Now())

	return filemiddleware.NewResponder(installer.NewDownloadClusterISOOK().WithPayload(reader),
		fmt.Sprintf("cluster-%s-discovery.iso", params.ClusterID.String()),
		contentLength)
}

func (b *bareMetalInventory) updateImageInfoPostUpload(ctx context.Context, cluster *common.Cluster, clusterProxyHash string, imageType models.ImageType) error {
	updates := map[string]interface{}{}
	imgName := getImageName(*cluster.ID)
	imgSize, err := b.objectHandler.GetObjectSizeBytes(ctx, imgName)
	if err != nil {
		return errors.New("Failed to generate image: error fetching size")
	}
	updates["image_size_bytes"] = imgSize
	cluster.ImageInfo.SizeBytes = &imgSize

	// Presigned URL only works with AWS S3 because Scality is not exposed
	if b.objectHandler.IsAwsS3() {
		signedURL, err := b.objectHandler.GeneratePresignedDownloadURL(ctx, imgName, imgName, b.Config.ImageExpirationTime)
		if err != nil {
			return errors.New("Failed to generate image: error generating URL")
		}
		updates["image_download_url"] = signedURL
		cluster.ImageInfo.DownloadURL = signedURL
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
	var cluster common.Cluster

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

	if err := tx.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	/* We need to ensure that the metadata in the DB matches the image that will be uploaded to S3,
	so we check that at least 10 seconds have past since the previous request to reduce the chance
	of a race between two consecutive requests.
	*/
	now := time.Now()
	previousCreatedAt := time.Time(cluster.ImageInfo.CreatedAt)
	if previousCreatedAt.Add(10 * time.Second).After(now) {
		log.Error("request came too soon after previous request")
		msg := "Failed to generate image: another request to generate an image has been recently submitted - please wait a few seconds and try again"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
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

	staticIpsConfig := formatStaticIPs(params.ImageCreateParams.StaticIpsConfig)

	vlansConfig := formatVlans(params.ImageCreateParams.VlansConfig)

	var imageExists bool
	if cluster.ImageInfo.SSHPublicKey == params.ImageCreateParams.SSHPublicKey &&
		cluster.ProxyHash == clusterProxyHash &&
		cluster.ImageInfo.StaticIpsConfig == staticIpsConfig &&
		cluster.ImageInfo.VlansConfig == vlansConfig &&
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
	updates["image_download_url"] = ""
	updates["image_static_ips_config"] = staticIpsConfig
	updates["image_vlans_config"] = vlansConfig
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
	if err = b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		msg := "Failed to generate image: error fetching updated cluster metadata"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	if imageExists {
		if err = b.updateImageInfoPostUpload(ctx, &cluster, clusterProxyHash, params.ImageCreateParams.ImageType); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}

		log.Infof("Re-used existing cluster <%s> image", params.ClusterID)
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Re-used existing image rather than generating a new one", time.Now())
		return &cluster, nil
	}
	ignitionConfig, formatErr := b.formatIgnitionFile(&cluster, params, log, false)
	if formatErr != nil {
		log.WithError(formatErr).Errorf("failed to format ignition config file for cluster %s", cluster.ID)
		msg := "Failed to generate image: error formatting ignition file"
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, msg, time.Now())
		return nil, common.NewApiError(http.StatusInternalServerError, formatErr)
	}

	if err = b.objectHandler.Upload(ctx, []byte(ignitionConfig), fmt.Sprintf("%s/discovery.ign", cluster.ID)); err != nil {
		log.WithError(err).Errorf("Upload discovery ignition failed for cluster %s", cluster.ID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	objectPrefix := fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, cluster.ID.String())

	if params.ImageCreateParams.ImageType == models.ImageTypeMinimalIso {
		if err := b.generateClusterMinimalISO(ctx, log, &cluster, ignitionConfig, objectPrefix); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	} else {
		baseISOName, err := b.objectHandler.GetBaseIsoObject(cluster.OpenshiftVersion)
		if err != nil {
			log.WithError(err).Errorf("Failed to get source object name for cluster %s with ocp version %s", cluster.ID, cluster.OpenshiftVersion)
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}

		if err := b.objectHandler.UploadISO(ctx, ignitionConfig, baseISOName, objectPrefix); err != nil {
			log.WithError(err).Errorf("Upload ISO failed for cluster %s", cluster.ID)
			b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityError, "Failed to upload image", time.Now())
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	if err := b.updateImageInfoPostUpload(ctx, &cluster, clusterProxyHash, params.ImageCreateParams.ImageType); err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	ignitionConfigForLogging, _ := b.formatIgnitionFile(&cluster, params, log, true)
	log.Infof("Generated cluster <%s> image with ignition config %s", params.ClusterID, ignitionConfigForLogging)

	msg := "Generated image"

	var msgExtras []string

	if cluster.HTTPProxy != "" {
		msgExtras = append(msgExtras, fmt.Sprintf(`proxy URL is "%s"`, cluster.HTTPProxy))
	}

	if params.ImageCreateParams.ImageType != "" {
		msgExtras = append(msgExtras, fmt.Sprintf(`Image type is "%s"`, string(params.ImageCreateParams.ImageType)))
	}

	sshExtra := "SSH public key is not set"
	if params.ImageCreateParams.SSHPublicKey != "" {
		sshExtra = "SSH public key is set"
	}

	msgExtras = append(msgExtras, sshExtra)

	msg = fmt.Sprintf("%s (%s)", msg, strings.Join(msgExtras, ", "))

	b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, msg, time.Now())
	return &cluster, nil
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

	log.Infof("Creating minimal ISO for cluster %s", cluster.ID)
	editor, err := b.isoEditorFactory.NewEditor(isoPath, cluster.OpenshiftVersion, log)
	if err != nil {
		log.WithError(err).Errorf("Failed to create iso editor for cluster %s with iso file %s", cluster.ID, isoPath)
		return err
	}

	clusterISOPath, err := editor.CreateClusterMinimalISO(ignitionConfig, cluster.ImageInfo.StaticIpsConfig)
	if err != nil {
		log.WithError(err).Errorf("Failed to create minimal discovery ISO for cluster %s", cluster.ID)
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

type clusterInstaller struct {
	ctx    context.Context
	b      *bareMetalInventory
	log    logrus.FieldLogger
	params installer.InstallClusterParams
}

func (c *clusterInstaller) installHosts(cluster *common.Cluster, tx *gorm.DB) error {
	success := true
	err := errors.Errorf("Failed to install cluster <%s>", cluster.ID.String())
	for i := range cluster.Hosts {
		if installErr := c.b.hostApi.Install(c.ctx, cluster.Hosts[i], tx); installErr != nil {
			success = false
			// collect multiple errors
			err = errors.Wrap(installErr, err.Error())
		}
	}
	if !success {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
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

func (c clusterInstaller) install(tx *gorm.DB) error {
	var cluster common.Cluster
	var err error

	// in case host monitor already updated the state we need to use FOR UPDATE option
	tx = transaction.AddForUpdateQueryOption(tx)

	if err = tx.Preload("Hosts").First(&cluster, "id = ?", c.params.ClusterID).Error; err != nil {
		return errors.Wrapf(err, "failed to find cluster %s", c.params.ClusterID)
	}

	if err = c.b.createDNSRecordSets(c.ctx, cluster); err != nil {
		return errors.Wrapf(err, "failed to create DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	if err = c.b.clusterApi.Install(c.ctx, &cluster, tx); err != nil {
		return errors.Wrapf(err, "failed to install cluster %s", cluster.ID.String())
	}

	// move hosts states to installing
	if err = c.installHosts(&cluster, tx); err != nil {
		return err
	}

	return nil
}

func (b *bareMetalInventory) storeOpenshiftClusterID(ctx context.Context, clusterID string) error {
	log := logutil.FromContext(ctx, b.log)
	log.Debug("Downloading bootstrap ignition file")
	reader, _, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", clusterID, "bootstrap.ign"))
	if err != nil {
		log.WithError(err).Error("Failed downloading bootstrap ignition file")
		return err
	}

	var openshiftClusterID string
	log.Debug("Extracting Openshift cluster ID from ignition file")
	openshiftClusterID, err = ignition.ExtractClusterID(reader)
	if err != nil {
		log.WithError(err).Error("Failed extracting Openshift cluster ID from ignition file")
		return err
	}
	log.Debugf("Got OpenShift cluster ID of %s", openshiftClusterID)

	log.Debugf("Storing Openshift cluster ID of cluster %s to DB", clusterID)
	if err = b.db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update(
		"openshift_cluster_id", openshiftClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed storing Openshift cluster ID of cluster %s to DB", clusterID)
		return err
	}

	return nil
}

func (b *bareMetalInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	c, err := b.InstallClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewInstallClusterAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) InstallClusterInternal(ctx context.Context, params installer.InstallClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	var err error

	log.Infof("preparing for cluster %s installation", params.ClusterID)
	if err = b.db.Preload("Hosts", "status <> ?", models.HostStatusDisabled).
		First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
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

	if err = b.refreshAllHosts(ctx, &cluster); err != nil {
		return nil, err
	}
	if _, err = b.clusterApi.RefreshStatus(ctx, &cluster, b.db); err != nil {
		return nil, err
	}

	// Reload again after refresh
	if err = b.db.Preload("Hosts", "status <> ?", models.HostStatusDisabled).First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return nil, common.NewApiError(http.StatusNotFound, err)
	}
	// Verify cluster is ready to install
	if ok, reason := b.clusterApi.IsReadyForInstallation(&cluster); !ok {
		return nil, common.NewApiError(http.StatusConflict,
			errors.Errorf("Cluster is not ready for installation, %s", reason))
	}

	// prepare cluster and hosts for installation
	err = b.db.Transaction(func(tx *gorm.DB) error {
		// in case host monitor already updated the state we need to use FOR UPDATE option
		tx = transaction.AddForUpdateQueryOption(tx)

		if err = b.clusterApi.PrepareForInstallation(ctx, &cluster, tx); err != nil {
			return err
		}

		for i := range cluster.Hosts {
			if err = b.hostApi.PrepareForInstallation(ctx, cluster.Hosts[i], tx); err != nil {
				return err
			}
		}
		if err = b.setBootstrapHost(ctx, cluster, tx); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if err = b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return nil, err
	}

	// Delete previews installation log files from object storage (if exist).
	if err := b.clusterApi.DeleteClusterLogs(ctx, &cluster, b.objectHandler); err != nil {
		log.WithError(err).Warnf("Failed deleting s3 logs of cluster %s", cluster.ID.String())
	}

	go func() {
		var err error
		asyncCtx := requestid.ToContext(context.Background(), requestid.FromContext(ctx))

		defer func() {
			if err != nil {
				log.WithError(err).Warn("Cluster installation initialization failed")
				b.clusterApi.HandlePreInstallError(asyncCtx, &cluster, err)
			}
		}()

		if err = b.generateClusterInstallConfig(asyncCtx, cluster); err != nil {
			return
		}
		log.Infof("generated ignition for cluster %s", cluster.ID.String())

		log.Infof("Storing OpenShift cluster ID of cluster %s to DB", cluster.ID.String())
		if err = b.storeOpenshiftClusterID(ctx, cluster.ID.String()); err != nil {
			return
		}

		cInstaller := clusterInstaller{
			ctx:    asyncCtx, // Need a new context for async part
			b:      b,
			log:    log,
			params: params,
		}
		if err = b.db.Transaction(cInstaller.install); err != nil {
			return
		}

		// send metric and event that installation process has been started
		b.metricApi.InstallationStarted(cluster.OpenshiftVersion, *cluster.ID, cluster.EmailDomain, strconv.FormatBool(swag.BoolValue(cluster.UserManagedNetworking)))
		b.metricApi.ClusterHostInstallationCount(*cluster.ID, cluster.EmailDomain, len(cluster.Hosts), cluster.OpenshiftVersion)
		b.eventsHandler.AddEvent(
			ctx, *cluster.ID, nil, models.EventSeverityInfo,
			fmt.Sprintf("Updated status of cluster %s to installing", cluster.Name), time.Now())
	}()

	log.Infof("Successfully prepared cluster <%s> for installation", params.ClusterID.String())
	return &cluster, nil
}

func (b *bareMetalInventory) InstallHosts(ctx context.Context, params installer.InstallHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	var err error

	if err = b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
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

	if err = b.refreshAllHosts(ctx, &cluster); err != nil {
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

	if err = tx.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return common.GenerateErrorResponder(err)
	}

	// move hosts to installing
	for i := range cluster.Hosts {
		if swag.StringValue(cluster.Hosts[i].Status) != models.HostStatusKnown {
			continue
		}
		err = b.createAndUploadNodeIgnition(ctx, &cluster, cluster.Hosts[i])
		if err != nil {
			log.Error("Failed to upload ignition for host %s", cluster.Hosts[i].RequestedHostname)
			continue
		}
		if installErr := b.hostApi.Install(ctx, cluster.Hosts[i], tx); installErr != nil {
			// we just logs the error, each host install is independent
			log.Error("Failed to move host %s to installing", cluster.Hosts[i].RequestedHostname)
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
	log := logutil.FromContext(ctx, b.log)

	c, err := b.getCluster(ctx, params.ClusterID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	cfg, err := installcfg.GetInstallConfig(log, c, false, "")
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewGetClusterInstallConfigOK().WithPayload(string(cfg))
}

func (b *bareMetalInventory) UpdateClusterInstallConfig(ctx context.Context, params installer.UpdateClusterInstallConfigParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	query := "id = ?"

	err := b.db.First(&cluster, query, params.ClusterID).Error
	if err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewUpdateClusterInstallConfigNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewUpdateClusterInstallConfigInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err = installcfg.ValidateInstallConfigPatch(log, &cluster, params.InstallConfigParams); err != nil {
		return installer.NewUpdateClusterInstallConfigBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = b.db.Model(&common.Cluster{}).Where(query, params.ClusterID).Update("install_config_overrides", params.InstallConfigParams).Error
	if err != nil {
		return installer.NewUpdateClusterInstallConfigInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewUpdateClusterInstallConfigCreated()
}

func (b *bareMetalInventory) generateClusterInstallConfig(ctx context.Context, cluster common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)

	cfg, err := installcfg.GetInstallConfig(log, &cluster, b.Config.InstallRHCa, redhatRootCA)
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
		log.WithError(err).Errorf("failed generating kubeconfig files for cluster %s", cluster.ID)
		return errors.Wrapf(err, "failed generating kubeconfig files for cluster %s", cluster.ID)
	}

	return nil
}

func (b *bareMetalInventory) refreshClusterHosts(ctx context.Context, cluster *common.Cluster, tx *gorm.DB, log logrus.FieldLogger) error {
	err := b.setMajorityGroupForCluster(cluster.ID, tx)
	if err != nil {
		log.WithError(err).Errorf("Failed to set cluster %s majority groups", cluster.ID.String())
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	for _, h := range cluster.Hosts {
		var host models.Host
		var err error
		if err = tx.Take(&host, "id = ? and cluster_id = ?",
			h.ID.String(), cluster.ID.String()).Error; err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				h.ID.String(), cluster.ID.String())
			return common.NewApiError(http.StatusNotFound, err)
		}
		if err = b.hostApi.RefreshStatus(ctx, &host, tx); err != nil {
			log.WithError(err).Errorf("failed to refresh state of host %s cluster %s", *h.ID, cluster.ID.String())
			return common.NewApiError(http.StatusInternalServerError, err)
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

func (b *bareMetalInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	c, err := b.UpdateClusterInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return installer.NewUpdateClusterCreated().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) UpdateClusterInternal(ctx context.Context, params installer.UpdateClusterParams) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	var err error
	log.Info("update cluster ", params.ClusterID)
	if swag.StringValue(params.ClusterUpdateParams.PullSecret) != "" {
		err = b.secretValidator.ValidatePullSecret(*params.ClusterUpdateParams.PullSecret, auth.UserNameFromContext(ctx), b.authHandler)
		if err != nil {
			log.WithError(err).Errorf("Pull secret for cluster %s is invalid", params.ClusterID)
			return nil, common.NewApiError(http.StatusBadRequest, secretValidationToUserError(err))
		}
		ps, errUpdate := b.updatePullSecret(*params.ClusterUpdateParams.PullSecret, log)
		if errUpdate != nil {
			return nil, common.NewApiError(http.StatusBadRequest,
				errors.New("Failed to update Pull-secret with additional credentials"))
		}
		params.ClusterUpdateParams.PullSecret = &ps
	}
	if newClusterName := swag.StringValue(params.ClusterUpdateParams.Name); newClusterName != "" {
		if err = validations.ValidateClusterNameFormat(newClusterName); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
	}

	if sshPublicKey := swag.StringValue(params.ClusterUpdateParams.SSHPublicKey); sshPublicKey != "" {
		sshPublicKey = strings.TrimSpace(sshPublicKey)
		if err = validations.ValidateSSHPublicKey(sshPublicKey); err != nil {
			return nil, common.NewApiError(http.StatusBadRequest, err)
		}
		*params.ClusterUpdateParams.SSHPublicKey = sshPublicKey
	}

	if params.ClusterUpdateParams.HTTPProxy != nil &&
		(params.ClusterUpdateParams.HTTPSProxy == nil || *params.ClusterUpdateParams.HTTPSProxy == "") {
		params.ClusterUpdateParams.HTTPSProxy = params.ClusterUpdateParams.HTTPProxy
	}
	if err = validateProxySettings(params.ClusterUpdateParams.HTTPProxy,
		params.ClusterUpdateParams.HTTPSProxy,
		params.ClusterUpdateParams.NoProxy); err != nil {
		log.WithError(err).Errorf("Failed to validate Proxy settings")
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

	if err = tx.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	if err = b.clusterApi.VerifyClusterUpdatability(&cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", params.ClusterID)
		return nil, common.NewApiError(http.StatusConflict, err)
	}

	if err = b.noneHaModeClusterUpdateValidations(&cluster, params); err != nil {
		log.WithError(err).Warnf("Unsupported update params in none ha mode")
		return nil, common.NewApiError(http.StatusBadRequest, err)
	}

	if err = b.validateDNSDomain(params, log); err != nil {
		return nil, err
	}

	err = b.updateClusterData(ctx, &cluster, params, tx, log)
	if err != nil {
		log.WithError(err).Error("updateClusterData")
		return nil, err
	}

	err = b.updateHostsData(ctx, params, tx, log)
	if err != nil {
		return nil, err
	}

	err = b.updateHostsAndClusterStatus(ctx, &cluster, tx, log)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("DB error, failed to commit"))
	}
	txSuccess = true

	if proxySettingsChanged(params.ClusterUpdateParams, &cluster) {
		b.eventsHandler.AddEvent(ctx, params.ClusterID, nil, models.EventSeverityInfo, "Proxy settings changed", time.Now())
	}

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	cluster.HostNetworks = calculateHostNetworks(log, &cluster)
	for _, host := range cluster.Hosts {
		if err := b.customizeHost(host); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}

	return &cluster, nil
}

func setMachineNetworkCIDRForUpdate(updates map[string]interface{}, machineNetworkCIDR string) {
	updates["machine_network_cidr"] = machineNetworkCIDR
	updates["machine_network_cidr_updated_at"] = time.Now()
}

func (b *bareMetalInventory) updateNonDhcpNetworkParams(updates map[string]interface{}, cluster *common.Cluster, params installer.UpdateClusterParams, log logrus.FieldLogger, machineCidr *string) error {
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
	*machineCidr, err = network.CalculateMachineNetworkCIDR(apiVip, ingressVip, cluster.Hosts)
	if err != nil {
		log.WithError(err).Errorf("failed to calculate machine network cidr for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	setMachineNetworkCIDRForUpdate(updates, *machineCidr)

	err = network.VerifyVips(cluster.Hosts, *machineCidr, apiVip, ingressVip, false, log)
	if err != nil {
		log.WithError(err).Errorf("VIP verification failed for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	return nil
}

func (b *bareMetalInventory) updateDhcpNetworkParams(updates map[string]interface{}, cluster *common.Cluster, params installer.UpdateClusterParams, log logrus.FieldLogger, machineCidr *string) error {
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
		return network.VerifyMachineCIDR(swag.StringValue(params.ClusterUpdateParams.MachineNetworkCidr), cluster.Hosts, log)
	}
	return nil
}

func (b *bareMetalInventory) updateClusterData(ctx context.Context, cluster *common.Cluster, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	var err error
	updates := map[string]interface{}{}
	machineCidr := cluster.MachineNetworkCidr
	serviceCidr := cluster.ServiceNetworkCidr
	clusterCidr := cluster.ClusterNetworkCidr
	hostNetworkPrefix := cluster.ClusterNetworkHostPrefix
	vipDhcpAllocation := swag.BoolValue(cluster.VipDhcpAllocation)
	userManagedNetworking := swag.BoolValue(cluster.UserManagedNetworking)
	optionalParam(params.ClusterUpdateParams.Name, "name", updates)
	optionalParam(params.ClusterUpdateParams.BaseDNSDomain, "base_dns_domain", updates)
	optionalParam(params.ClusterUpdateParams.HTTPProxy, "http_proxy", updates)
	optionalParam(params.ClusterUpdateParams.HTTPSProxy, "https_proxy", updates)
	optionalParam(params.ClusterUpdateParams.NoProxy, "no_proxy", updates)
	optionalParam(params.ClusterUpdateParams.SSHPublicKey, "ssh_public_key", updates)
	if params.ClusterUpdateParams.ClusterNetworkCidr != nil {
		if err = network.VerifySubnetCIDR(*params.ClusterUpdateParams.ClusterNetworkCidr); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
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
		if err = network.VerifySubnetCIDR(*params.ClusterUpdateParams.ServiceNetworkCidr); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
		serviceCidr = *params.ClusterUpdateParams.ServiceNetworkCidr
		updates["service_network_cidr"] = serviceCidr
	}
	if params.ClusterUpdateParams.AdditionalNtpSource != nil {
		ntpSource := swag.StringValue(params.ClusterUpdateParams.AdditionalNtpSource)

		if ntpSource != "" && !validations.ValidateAdditionalNTPSource(ntpSource) {
			err = errors.Errorf("Invalid NTP source: %s", ntpSource)
			log.WithError(err)
			return common.NewApiError(http.StatusBadRequest, err)
		}

		updates["additional_ntp_source"] = ntpSource
	}

	if params.ClusterUpdateParams.UserManagedNetworking != nil && swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking) != userManagedNetworking {
		userManagedNetworking = swag.BoolValue(params.ClusterUpdateParams.UserManagedNetworking)
		updates["user_managed_networking"] = userManagedNetworking
		if userManagedNetworking {
			err = validateUserManagedNetworkConflicts(params.ClusterUpdateParams, log)
			if err != nil {
				return err
			}
			updates["vip_dhcp_allocation"] = false
			vipDhcpAllocation = false
			updates["api_vip"] = ""
			updates["ingress_vip"] = ""
			setMachineNetworkCIDRForUpdate(updates, "")
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
			err = b.updateDhcpNetworkParams(updates, cluster, params, log, &machineCidr)
		} else {
			err = b.updateNonDhcpNetworkParams(updates, cluster, params, log, &machineCidr)
		}
		if err != nil {
			return err
		}
	}

	if err = network.VerifyClusterCIDRsNotOverlap(machineCidr, clusterCidr, serviceCidr, userManagedNetworking); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if params.ClusterUpdateParams.Operators != nil {
		clusterOperators := convertFromClusterOperators(params.ClusterUpdateParams.Operators)
		updates["operators"] = clusterOperators
		cluster.Operators = clusterOperators
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
	if params.ClusterUpdateParams.APIVipDNSName != nil && swag.StringValue(cluster.Kind) == models.ClusterKindAddHostsCluster {
		log.Infof("Updating api vip to %s for day2 cluster %s", *params.ClusterUpdateParams.APIVipDNSName, cluster.ID)
		updates["api_vip_dns_name"] = *params.ClusterUpdateParams.APIVipDNSName
	}
	dbReply := db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "failed to update cluster: %s", params.ClusterID))
	}

	return nil
}

func validateUserManagedNetworkConflicts(params *models.ClusterUpdateParams, log logrus.FieldLogger) error {
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
	if params.MachineNetworkCidr != nil {
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

func (b *bareMetalInventory) updateHostRoles(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {

	for i := range params.ClusterUpdateParams.HostsRoles {
		log.Infof("Update host %s to role: %s", params.ClusterUpdateParams.HostsRoles[i].ID,
			params.ClusterUpdateParams.HostsRoles[i].Role)
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateRole(ctx, &host, models.HostRole(params.ClusterUpdateParams.HostsRoles[i].Role), db)
		if err != nil {
			log.WithError(err).Errorf("failed to set role <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].Role, params.ClusterUpdateParams.HostsRoles[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostNames(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.HostsNames {
		log.Infof("Update host %s to request hostname %s", params.ClusterUpdateParams.HostsNames[i].ID,
			params.ClusterUpdateParams.HostsNames[i].Hostname)
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsNames[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsNames[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		if err = hostutil.ValidateHostname(params.ClusterUpdateParams.HostsNames[i].Hostname); err != nil {
			log.WithError(err).Errorf("invalid hostname format: %s", err)
			return err
		}
		err = b.hostApi.UpdateHostname(ctx, &host, params.ClusterUpdateParams.HostsNames[i].Hostname, db)
		if err != nil {
			log.WithError(err).Errorf("failed to set hostname <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsNames[i].Hostname, params.ClusterUpdateParams.HostsNames[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostsDiskSelection(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.DisksSelectedConfig {
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.DisksSelectedConfig[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.DisksSelectedConfig[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}

		var installationDiskPath = ""
		for _, diskConfig := range params.ClusterUpdateParams.DisksSelectedConfig[i].DisksConfig {
			if models.DiskRoleInstall == diskConfig.Role {
				if installationDiskPath != "" {
					return common.NewApiError(http.StatusConflict,
						fmt.Errorf("duplicate setting of installation path by the user"))
				}
				installationDiskPath = hostutil.FindDiskPathByID(*diskConfig.ID, &host)
			}
		}

		log.Infof("Update host %s to install from path %s",
			params.ClusterUpdateParams.DisksSelectedConfig[i].ID, installationDiskPath)
		err = b.hostApi.UpdateInstallationDiskPath(ctx, db, &host, installationDiskPath)
		if err != nil {
			log.WithError(err).Errorf("failed to set installation disk path <%s> host <%s> in cluster <%s>",
				installationDiskPath,
				params.ClusterUpdateParams.DisksSelectedConfig[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostsMachineConfigPoolNames(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.HostsMachineConfigPoolNames {
		log.Infof("Update host %s to machineConfigPoolName %s", params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].ID,
			params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].MachineConfigPoolName)
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateMachineConfigPoolName(ctx, db, &host, params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].MachineConfigPoolName)
		if err != nil {
			log.WithError(err).Errorf("failed to set machine config pool name <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].MachineConfigPoolName, params.ClusterUpdateParams.HostsMachineConfigPoolNames[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) updateHostsData(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	if err := b.updateHostRoles(ctx, params, db, log); err != nil {
		return err
	}

	if err := b.updateHostNames(ctx, params, db, log); err != nil {
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

func calculateHostNetworks(log logrus.FieldLogger, cluster *common.Cluster) []*models.HostNetwork {
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
			for _, address := range intf.IPV4Addresses {
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
	userFilter := identity.AddUserFilter(ctx, "")
	query := db.Preload("Hosts", func(db *gorm.DB) *gorm.DB {
		if swag.BoolValue(params.GetUnregisteredClusters) {
			return db.Unscoped()
		}
		return db
	}).Where(userFilter)

	if params.OpenshiftClusterID != nil {
		query = query.Where("openshift_cluster_id = ?", *params.OpenshiftClusterID)
	}

	if err := query.Find(&dbClusters).Error; err != nil {
		log.WithError(err).Error("Failed to list clusters in db")
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	for _, c := range dbClusters {
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
	var cluster common.Cluster

	db := b.db
	if swag.BoolValue(params.GetUnregisteredClusters) {
		if !identity.IsAdmin(ctx) {
			return nil, common.NewInfraError(http.StatusForbidden,
				errors.New("only admin users are allowed to get unregistered clusters"))
		}
		db = b.db.Unscoped()
	}

	if err := db.Preload(
		"Hosts", func(db *gorm.DB) *gorm.DB {
			if swag.BoolValue(params.GetUnregisteredClusters) {
				return db.Unscoped()
			}
			return db
		}).First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		// TODO: check for the right error
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	cluster.HostNetworks = calculateHostNetworks(log, &cluster)
	for _, host := range cluster.Hosts {
		if err := b.customizeHost(host); err != nil {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
		// Clear this field as it is not needed to be sent via API
		host.FreeAddresses = ""
	}
	return &cluster, nil
}

func (b *bareMetalInventory) GetHostRequirements(ctx context.Context, params installer.GetHostRequirementsParams) middleware.Responder {
	masterReqs := b.hostApi.GetHostRequirements(models.HostRoleMaster)
	workerReqs := b.hostApi.GetHostRequirements(models.HostRoleWorker)
	return installer.NewGetHostRequirementsOK().WithPayload(
		&models.HostRequirements{
			Master: &masterReqs,
			Worker: &workerReqs,
		})
}

func (b *bareMetalInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
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
		if gorm.IsRecordNotFoundError(err) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	err := tx.First(&host, "id = ? and cluster_id = ?", *params.NewHostParams.HostID, params.ClusterID).Error
	if err != nil && !gorm.IsRecordNotFoundError(err) {
		log.WithError(err).Errorf("failed to get host %s in cluster: %s",
			*params.NewHostParams.HostID, params.ClusterID.String())
		return installer.NewRegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// In case host doesn't exists check if the cluster accept new hosts registration
	if err != nil && gorm.IsRecordNotFoundError(err) {
		if err = b.clusterApi.AcceptRegistration(&cluster); err != nil {
			log.WithError(err).Errorf("failed to register host <%s> to cluster %s due to: %s",
				params.NewHostParams.HostID, params.ClusterID.String(), err.Error())
			b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
				"Failed to register host: cluster cannot accept new hosts in its current state", time.Now())
			return common.NewApiError(http.StatusConflict, err)
		}
	}

	url := installer.GetHostURL{ClusterID: params.ClusterID, HostID: *params.NewHostParams.HostID}
	kind := swag.String(models.HostKindHost)
	switch swag.StringValue(cluster.Kind) {
	case models.ClusterKindAddHostsCluster:
		kind = swag.String(models.HostKindAddToExistingClusterHost)
	case models.ClusterKindAddHostsOCPCluster:
		kind = swag.String(models.HostKindAddToExistingClusterOCPHost)
	}
	host = models.Host{
		ID:                    params.NewHostParams.HostID,
		Href:                  swag.String(url.String()),
		Kind:                  kind,
		ClusterID:             params.ClusterID,
		CheckedInAt:           strfmt.DateTime(time.Now()),
		DiscoveryAgentVersion: params.NewHostParams.DiscoveryAgentVersion,
		UserName:              auth.UserNameFromContext(ctx),
		Role:                  models.HostRoleAutoAssign,
	}

	if err = b.hostApi.RegisterHost(ctx, &host, tx); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> cluster <%s>",
			params.NewHostParams.HostID.String(), params.ClusterID.String())
		uerr := errors.Wrap(err, fmt.Sprintf("Failed to register host %s", hostutil.GetHostnameForMsg(&host)))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
			uerr.Error(), time.Now())
		return returnRegisterHostTransitionError(http.StatusBadRequest, err)
	}

	if err = b.customizeHost(&host); err != nil {
		b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityError,
			"Failed to register host: error setting host properties", time.Now())
		return common.GenerateErrorResponder(err)
	}

	b.eventsHandler.AddEvent(ctx, params.ClusterID, params.NewHostParams.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s: registered to cluster", hostutil.GetHostnameForMsg(&host)), time.Now())

	hostRegistration := models.HostRegistrationResponse{
		Host:                  host,
		NextStepRunnerCommand: b.generateNextStepRunnerCommand(ctx, &params),
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
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
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister host: %s cluster %s", params.HostID, params.ClusterID)

	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		Delete(&models.Host{}).Error; err != nil {
		// TODO: check error type
		return installer.NewDeregisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	// TODO: need to check that host can be deleted from the cluster
	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s: deregistered from cluster", params.HostID.String()), time.Now())
	return installer.NewDeregisterHostNoContent()
}

func (b *bareMetalInventory) GetHost(ctx context.Context, params installer.GetHostParams) middleware.Responder {
	var host models.Host
	// TODO: validate what is the error
	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		First(&host).Error; err != nil {
		return installer.NewGetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if err := b.customizeHost(&host); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// Clear this field as it is not needed to be sent via API
	host.FreeAddresses = ""
	return installer.NewGetHostOK().WithPayload(&host)
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

func (b *bareMetalInventory) UpdateHostInstallerArgs(ctx context.Context, params installer.UpdateHostInstallerArgsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	err := hostutil.ValidateInstallerArgs(params.InstallerArgsParams.Args)
	if err != nil {
		return installer.NewUpdateHostInstallerArgsBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	_, err = b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	argsBytes, err := json.Marshal(params.InstallerArgsParams.Args)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	err = b.db.Model(&models.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and cluster_id = ?"), params.HostID, params.ClusterID).Update("installer_args", string(argsBytes)).Error
	if err != nil {
		log.WithError(err).Errorf("failed to update host %s", params.HostID)
		return installer.NewUpdateHostInstallerArgsInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	h, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		log.WithError(err).Errorf("failed to get host %s after update", params.HostID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return installer.NewUpdateHostInstallerArgsCreated().WithPayload(h)
}

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var steps models.Steps
	var host models.Host

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
	if err := tx.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find host: %s", params.HostID)
		return installer.NewGetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	host.CheckedInAt = strfmt.DateTime(time.Now())
	if err := tx.Model(&host).Update("checked_in_at", host.CheckedInAt).Error; err != nil {
		log.WithError(err).Errorf("failed to update host: %s", params.ClusterID)
		return installer.NewGetNextStepsInternalServerError()
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewGetNextStepsInternalServerError()
	}
	txSuccess = true

	var err error
	steps, err = b.hostApi.GetNextSteps(ctx, &host)
	if err != nil {
		log.WithError(err).Errorf("failed to get steps for host %s cluster %s", params.HostID, params.ClusterID)
	}

	return installer.NewGetNextStepsOK().WithPayload(&steps)
}

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	var err error
	log := logutil.FromContext(ctx, b.log)
	msg := fmt.Sprintf("Received step reply <%s> from cluster <%s> host <%s>  exit-code <%d> stdout <%s> stderr <%s>", params.Reply.StepID, params.ClusterID,
		params.HostID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)

	var host models.Host
	if err = b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed to find host <%s> cluster <%s> step <%s> exit code %d stdout <%s> stderr <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)
		return installer.NewPostStepReplyNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	//check the output exit code
	if params.Reply.ExitCode != 0 {
		err = errors.New(msg)
		log.WithError(err).Errorf("Exit code is <%d> ", params.Reply.ExitCode)
		handlingError := b.handleReplyError(params, ctx, log, &host)
		if handlingError != nil {
			log.WithError(handlingError).Errorf("Failed handling reply error for host <%s> cluster <%s>", params.HostID, params.ClusterID)
		}
		return installer.NewPostStepReplyNoContent()
	}

	log.Infof(msg)

	var stepReply string
	stepReply, err = filterReplyByType(params)
	if err != nil {
		log.WithError(err).Errorf("Failed decode <%s> reply for host <%s> cluster <%s>",
			params.Reply.StepID, params.HostID, params.ClusterID)
		return installer.NewPostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = handleReplyByType(params, b, ctx, host, stepReply)
	if err != nil {
		log.WithError(err).Errorf("Failed to update step reply for host <%s> cluster <%s> step <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID)
		return installer.NewPostStepReplyInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewPostStepReplyNoContent()
}

func (b *bareMetalInventory) handleReplyError(params installer.PostStepReplyParams, ctx context.Context, log logrus.FieldLogger, h *models.Host) error {

	if params.Reply.StepType == models.StepTypeInstall {
		// Handle case of installation error due to an already running assisted-installer.
		if params.Reply.ExitCode == ContainerAlreadyRunningExitCode && strings.Contains(params.Reply.Error, "the container name \"assisted-installer\" is already in use") {
			log.Warnf("Install command failed due to an already running installation: %s", params.Reply.Error)
			return nil
		}
		//if it's install step - need to move host to error
		return b.hostApi.HandleInstallationFailure(ctx, h)
	}
	return nil
}

func (b *bareMetalInventory) updateFreeAddressesReport(ctx context.Context, host *models.Host, freeAddressesReport string) error {
	var (
		err           error
		freeAddresses models.FreeNetworksAddresses
	)
	log := logutil.FromContext(ctx, b.log)
	if err = json.Unmarshal([]byte(freeAddressesReport), &freeAddresses); err != nil {
		log.WithError(err).Warnf("Json unmarshal free addresses of host %s", host.ID.String())
		return err
	}
	if len(freeAddresses) == 0 {
		err = errors.Errorf("Free addresses for host %s is empty", host.ID.String())
		log.WithError(err).Warn("Update free addresses")
		return err
	}
	if err = b.db.Model(&models.Host{}).Where("id = ? and cluster_id = ?", host.ID.String(),
		host.ClusterID.String()).Updates(map[string]interface{}{"free_addresses": freeAddressesReport}).Error; err != nil {
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

func (b *bareMetalInventory) processFioPerfCheckResponse(ctx context.Context, h *models.Host, fioPerfCheckResponseStr string) error {
	var fioPerfCheckResponse models.FioPerfCheckResponse

	log := logutil.FromContext(ctx, b.log)

	if err := json.Unmarshal([]byte(fioPerfCheckResponseStr), &fioPerfCheckResponse); err != nil {
		log.WithError(err).Warnf("Json unmarshal FIO perf check response from host %s", h.ID.String())
		return err
	}

	if fioPerfCheckResponse.IoSyncDuration > hostcommands.FioDurationThresholdMs {
		// If the 99th percentile of fdatasync durations is more than 10ms, it's not fast enough for etcd.
		// See: https://www.ibm.com/cloud/blog/using-fio-to-tell-whether-your-storage-is-fast-enough-for-etcd
		msg := fmt.Sprintf("Host's disk %s is slower than the supported speed, and may cause degraded cluster performance (fdatasync duration: %d ms)",
			fioPerfCheckResponse.Path, fioPerfCheckResponse.IoSyncDuration)
		log.Warnf(msg)
		b.eventsHandler.AddEvent(ctx, h.ClusterID, h.ID, models.EventSeverityWarning, msg, time.Now())
	}

	b.metricApi.DiskSyncDuration(h.ClusterID, *h.ID, fioPerfCheckResponse.Path, fioPerfCheckResponse.IoSyncDuration)

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
	case models.StepTypeFioPerfCheck:
		err = b.processFioPerfCheckResponse(ctx, &host, stepReply)
	}
	return err
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
	case models.StepTypeFioPerfCheck:
		stepReply, err = filterReply(&models.FioPerfCheckResponse{}, params.Reply.Output)
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
	var host models.Host

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

	if err := tx.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to disable host %s: error fetching host from DB", params.HostID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err := b.hostApi.DisableHost(ctx, &host, tx); err != nil {
		log.WithError(err).Errorf("failed to disable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		msg := fmt.Sprintf("Failed to disable host %s: error disabling host in current status",
			hostutil.GetHostnameForMsg(&host))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "disable host", &params.HostID, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s disabled by user", hostutil.GetHostnameForMsg(&host)), time.Now())
	return installer.NewDisableHostOK().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host

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

	if err := tx.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to enable host %s: error fetching host from DB", params.HostID)
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err := b.hostApi.EnableHost(ctx, &host, tx); err != nil {
		log.WithError(err).Errorf("failed to enable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		msg := fmt.Sprintf("Failed to enable host %s: error disabling host in current status",
			hostutil.GetHostnameForMsg(&host))
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	c, err := b.refreshHostAndClusterStatuses(ctx, "enable host", &params.HostID, &params.ClusterID, tx)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return common.NewApiError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction"))
	}
	txSuccess = true

	b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityInfo,
		fmt.Sprintf("Host %s enabled by user", hostutil.GetHostnameForMsg(&host)), time.Now())
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
		if !gorm.IsRecordNotFoundError(err) {
			b.eventsHandler.AddEvent(
				ctx,
				*clusterID,
				hostID,
				models.EventSeverityError,
				err.Error(),
				time.Now())
		}
		err = common.NewApiError(http.StatusInternalServerError, err)
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

	var h models.Host

	filter := "id = ? and cluster_id = ?"
	if err := db.First(&h, filter, hostID, clusterID).Error; err != nil {
		return err
	}

	if err := b.hostApi.RefreshStatus(ctx, &h, db); err != nil {
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

	var c common.Cluster

	if err := db.First(&c, "id = ?", clusterID).Error; err != nil {
		return nil, err
	}

	updatedCluster, err := b.clusterApi.RefreshStatus(ctx, &c, db)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to refresh status of cluster: %s", c.ID)
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
	log := logutil.FromContext(ctx, b.log)
	if err := b.checkFileForDownload(ctx, params.ClusterID.String(), params.FileName); err != nil {
		return common.GenerateErrorResponder(err)
	}

	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", params.ClusterID, params.FileName))
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", params.FileName, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(respBody), params.FileName, contentLength)
}

func (b *bareMetalInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	if err := b.checkFileForDownload(ctx, params.ClusterID.String(), kubeconfig); err != nil {
		return common.GenerateErrorResponder(err)
	}

	respBody, contentLength, err := b.objectHandler.Download(ctx, fmt.Sprintf("%s/%s", params.ClusterID, kubeconfig))
	if err != nil {
		return common.NewApiError(http.StatusConflict, err)
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(respBody), kubeconfig, contentLength)
}

func (b *bareMetalInventory) getLogFileForDownload(ctx context.Context, clusterId *strfmt.UUID, hostId *strfmt.UUID, logsType string) (string, string, error) {
	var fileName string
	var downloadFileName string
	c, err := b.getCluster(ctx, clusterId.String(), returnHosts(true), includeDeleted(true))
	if err != nil {
		return "", "", err
	}
	switch logsType {
	case string(models.LogsTypeHost):
		if hostId == nil {
			return "", "", common.NewApiError(http.StatusBadRequest, errors.Errorf("Host ID must be provided for downloading host logs"))
		}
		var hostObject *models.Host
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
		downloadFileName = fmt.Sprintf("%s_%s_%s.tar.gz", sanitize.Name(c.Name), role, sanitize.Name(hostutil.GetHostnameForMsg(hostObject)))
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
	var c common.Cluster
	log.Infof("Checking cluster cluster file for download: %s for cluster %s", fileName, clusterID)

	if !funk.Contains(cluster.S3FileNames, fileName) && fileName != manifests.ManifestFolder {
		err := errors.Errorf("invalid cluster file %s", fileName)
		log.WithError(err).Errorf("failed download file: %s from cluster: %s", fileName, clusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if err := b.db.First(&c, "id = ?", clusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", clusterID)
		if gorm.IsRecordNotFoundError(err) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	var err error
	switch fileName {
	case kubeconfig:
		err = cluster.CanDownloadKubeconfig(&c)
	case manifests.ManifestFolder, "discovery.ign":
		// do nothing. manifests can be downloaded at any given cluster state
	default:
		err = cluster.CanDownloadFiles(&c)
	}
	if err != nil {
		log.WithError(err).Errorf("failed to get file for cluster %s in current state", clusterID)
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) UpdateHostIgnition(ctx context.Context, params installer.UpdateHostIgnitionParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	_, err := b.getHost(ctx, params.ClusterID.String(), params.HostID.String())
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	_, report, err := ign_3_1.Parse([]byte(params.HostIgnitionParams.Config))
	if err != nil {
		log.WithError(err).Errorf("Failed to parse host ignition config patch %s", params.HostIgnitionParams)
		return installer.NewUpdateHostIgnitionBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	if report.IsFatal() {
		err = errors.Errorf("Host ignition config patch %s failed validation: %s", params.HostIgnitionParams, report.String())
		log.Error(err)
		return installer.NewUpdateHostIgnitionBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = b.db.Model(&models.Host{}).Where(identity.AddUserFilter(ctx, "id = ? and cluster_id = ?"), params.HostID, params.ClusterID).Update("ignition_config_overrides", params.HostIgnitionParams.Config).Error
	if err != nil {
		return installer.NewUpdateHostIgnitionInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
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
	c, err := b.getCluster(ctx, clusterID, returnHosts(true))
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
	err = cluster.CanDownloadFiles(c)
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
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return common.NewApiError(http.StatusNotFound, err)
		} else {
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	if err := b.clusterApi.GetCredentials(&cluster); err != nil {
		log.WithError(err).Errorf("failed to get credentials of cluster %s", params.ClusterID.String())
		return common.NewApiError(http.StatusConflict, err)
	}
	objectName := fmt.Sprintf("%s/%s", params.ClusterID, "kubeadmin-password")
	r, _, err := b.objectHandler.Download(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to get clusters %s object", objectName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	defer r.Close()
	password, err := ioutil.ReadAll(r)
	if err != nil {
		log.WithError(errors.Errorf("%s", password)).Errorf("Failed to get clusters %s", objectName)
		return common.NewApiError(http.StatusConflict, errors.New(string(password)))
	}
	return installer.NewGetCredentialsOK().WithPayload(
		&models.Credentials{
			Username:   DefaultUser,
			Password:   string(password),
			ConsoleURL: fmt.Sprintf("%s.%s.%s", ConsoleUrlPrefix, cluster.Name, cluster.BaseDNSDomain),
		})
}

func (b *bareMetalInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	log.Info("Update host %s install progress", params.HostID)
	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find host %s", params.HostID)
		return installer.NewUpdateHostInstallProgressNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}
	if err := b.hostApi.UpdateInstallProgress(ctx, &host, params.HostProgress); err != nil {
		log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
		return installer.NewUpdateHostInstallProgressInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	event := fmt.Sprintf("reached installation stage %s", params.HostProgress.CurrentStage)

	if params.HostProgress.ProgressInfo != "" {
		event += fmt.Sprintf(": %s", params.HostProgress.ProgressInfo)
	}

	log.Info(fmt.Sprintf("Host %s in cluster %s: %s", host.ID, host.ClusterID, event))
	msg := fmt.Sprintf("Host %s: %s", hostutil.GetHostnameForMsg(&host), event)

	b.eventsHandler.AddEvent(ctx, host.ClusterID, host.ID, models.EventSeverityInfo, msg, time.Now())
	return installer.NewUpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("UploadClusterIngressCert for cluster %s with params %s", params.ClusterID, params.IngressCertParams)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
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

	objectName := fmt.Sprintf("%s/%s", cluster.ID, kubeconfig)
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

	noingress := fmt.Sprintf("%s/%s-noingress", cluster.ID, kubeconfig)
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
	log := logutil.FromContext(ctx, b.log)
	log.Infof("canceling installation for cluster %s", params.ClusterID)

	var c common.Cluster

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
		b.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError, msg, time.Now())
		return installer.NewCancelInstallationInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New(msg)))
	}

	if err := tx.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed to cancel installation: could not find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewCancelInstallationNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewCancelInstallationInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// cancellation is made by setting the cluster and and hosts states to error.
	if err := b.clusterApi.CancelInstallation(ctx, &c, "Installation was canceled by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}
	for _, h := range c.Hosts {
		if err := b.hostApi.CancelInstallation(ctx, h, "Installation was canceled by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewCancelInstallationInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf("Failed to cancel installation: error committing DB transaction (%s)", err)
		msg := "Failed to cancel installation: error committing DB transaction"
		b.eventsHandler.AddEvent(ctx, *c.ID, nil, models.EventSeverityError, msg, time.Now())
		return installer.NewCancelInstallationInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewCancelInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("resetting cluster %s", params.ClusterID)

	var c common.Cluster

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

	if err := tx.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewResetClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.clusterApi.ResetCluster(ctx, &c, "cluster was reset by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// abort installation files generation job if running.
	if err := b.generator.AbortInstallConfig(ctx, c); err != nil {
		return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, h := range c.Hosts {
		if err := b.hostApi.ResetHost(ctx, h, "cluster was reset by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.DeleteClusterFiles(ctx, &c, b.objectHandler); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err := b.deleteDNSRecordSets(ctx, c); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", c.BaseDNSDomain)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewResetClusterAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) InstallHost(ctx context.Context, params installer.InstallHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var h *models.Host
	var cluster common.Cluster

	log.Info("Install single day2 host: ", params.HostID)
	err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error
	if err != nil {
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
		return common.NewApiError(http.StatusConflict, fmt.Errorf("Method only allowed when adding hosts to an existing cluster"))
	}

	if swag.StringValue(h.Status) != models.HostStatusKnown {
		log.Errorf("Install host for host %s, state %s is forbidden: host not in Known state", params.HostID.String(), swag.StringValue(h.Status))
		return common.NewApiError(http.StatusConflict, fmt.Errorf("Cannot install host in state %s", swag.StringValue(h.Status)))
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
		return common.NewApiError(http.StatusConflict, fmt.Errorf("Cannot install host in state %s after refresh", swag.StringValue(h.Status)))
	}
	err = b.createAndUploadNodeIgnition(ctx, &cluster, h)
	if err != nil {
		log.Error("Failed to upload ignition for host %s", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}
	err = b.hostApi.Install(ctx, h, b.db)
	if err != nil {
		// we just logs the error, each host install is independent
		log.Error("Failed to move host %s to installing", h.RequestedHostname)
		return common.GenerateErrorResponder(err)
	}

	return installer.NewInstallHostAccepted().WithPayload(h)
}

func (b *bareMetalInventory) ResetHost(ctx context.Context, params installer.ResetHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var h models.Host

	log.Info("Reseting host: ", params.HostID)
	if err := b.db.First(&h, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		msg := fmt.Sprintf("Failed to reset host %s: error fetching host from DB", params.HostID.String())
		b.eventsHandler.AddEvent(ctx, params.ClusterID, &params.HostID, models.EventSeverityError, msg, time.Now())
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if !hostutil.IsDay2Host(&h) {
		log.Errorf("ResetHost for host %s is forbiddent: not a Day2 hosts", params.HostID.String())
		return common.NewApiError(http.StatusConflict, fmt.Errorf("Method only allowed when adding hosts to an existing cluster"))
	}

	err := b.db.Transaction(func(tx *gorm.DB) error {
		if err := b.hostApi.ResetHost(ctx, &h, "host was reset by user", tx); err != nil {
			return err
		}
		if err := b.customizeHost(&h); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	return installer.NewResetHostOK().WithPayload(&h)
}

func (b *bareMetalInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	log.Infof("complete cluster %s installation", params.ClusterID)

	var c common.Cluster
	if err := b.db.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return common.NewApiError(http.StatusNotFound, err)
		}
		return common.GenerateErrorResponder(err)
	}

	if err := b.clusterApi.CompleteInstallation(ctx, &c, *params.CompletionParams.IsSuccess, params.CompletionParams.ErrorInfo); err != nil {
		log.WithError(err).Errorf("Failed to set complete cluster state on %s ", params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return installer.NewCompleteInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) createDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.changeDNSRecordSets(ctx, cluster, false)
}

func (b *bareMetalInventory) deleteDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.changeDNSRecordSets(ctx, cluster, true)
}

func (b *bareMetalInventory) changeDNSRecordSets(ctx context.Context, cluster common.Cluster, delete bool) error {
	log := logutil.FromContext(ctx, b.log)

	domain, err := b.getDNSDomain(cluster.Name, cluster.BaseDNSDomain)
	if err != nil {
		return err
	}
	if domain == nil {
		// No supported base DNS domain specified
		return nil
	}

	switch domain.Provider {
	case "route53":
		var dnsProvider dnsproviders.Provider = dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: "A",
				TTL:           60,
			},
			HostedZoneID: domain.ID,
			SharedCreds:  true,
		}

		dnsRecordSetFunc := dnsProvider.CreateRecordSet
		if delete {
			dnsRecordSetFunc = dnsProvider.DeleteRecordSet
		}

		// Create/Delete A record for API virtual IP
		_, err := dnsRecordSetFunc(domain.APIDomainName, cluster.APIVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.APIDomainName, cluster.APIVip)
			return err
		}
		// Create/Delete A record for Ingress virtual IP
		_, err = dnsRecordSetFunc(domain.IngressDomainName, cluster.IngressVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.IngressDomainName, cluster.IngressVip)
			return err
		}
		log.Infof("Successfully created DNS records for base domain: %s", cluster.BaseDNSDomain)
	}
	return nil
}

type dnsDomain struct {
	Name              string
	ID                string
	Provider          string
	APIDomainName     string
	IngressDomainName string
}

func (b *bareMetalInventory) getDNSDomain(clusterName, baseDNSDomainName string) (*dnsDomain, error) {
	var dnsDomainID string
	var dnsProvider string

	// Parse base domains from config
	if val, ok := b.Config.BaseDNSDomains[baseDNSDomainName]; ok {
		re := regexp.MustCompile("/")
		if !re.MatchString(val) {
			return nil, errors.New(fmt.Sprintf("Invalid DNS domain: %s", val))
		}
		s := re.Split(val, 2)
		dnsDomainID = s[0]
		dnsProvider = s[1]
	} else {
		// No base domains defined in config
		return nil, nil
	}

	if dnsDomainID == "" || dnsProvider == "" {
		// Specified domain is not defined in config
		return nil, nil
	}

	return &dnsDomain{
		Name:              baseDNSDomainName,
		ID:                dnsDomainID,
		Provider:          dnsProvider,
		APIDomainName:     fmt.Sprintf("%s.%s.%s", "api", clusterName, baseDNSDomainName),
		IngressDomainName: fmt.Sprintf("*.%s.%s.%s", "apps", clusterName, baseDNSDomainName),
	}, nil
}

func (b *bareMetalInventory) validateDNSDomain(params installer.UpdateClusterParams, log logrus.FieldLogger) error {
	clusterName := swag.StringValue(params.ClusterUpdateParams.Name)
	clusterBaseDomain := swag.StringValue(params.ClusterUpdateParams.BaseDNSDomain)
	if clusterBaseDomain != "" {
		if err := validations.ValidateDomainNameFormat(clusterBaseDomain); err != nil {
			log.WithError(err).Errorf("Invalid cluster base domain: %s", clusterBaseDomain)
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}
	dnsDomain, err := b.getDNSDomain(clusterName, clusterBaseDomain)
	if err == nil && dnsDomain != nil {
		// Cluster's baseDNSDomain is defined in config (BaseDNSDomains map)
		if err = b.validateBaseDNS(dnsDomain); err != nil {
			log.WithError(err).Errorf("Invalid base DNS domain: %s", clusterBaseDomain)
			return common.NewApiError(http.StatusConflict, errors.New("Base DNS domain isn't configured properly"))
		}
		if err = b.validateDNSRecords(dnsDomain); err != nil {
			log.WithError(err).Errorf("DNS records already exist for cluster: %s", params.ClusterID)
			return common.NewApiError(http.StatusConflict,
				errors.New("DNS records already exist for cluster - please change 'Cluster Name'"))
		}
	}
	return nil
}

func (b *bareMetalInventory) validateBaseDNS(domain *dnsDomain) error {
	return validations.ValidateBaseDNS(domain.Name, domain.ID, domain.Provider)
}

func (b *bareMetalInventory) validateDNSRecords(domain *dnsDomain) error {
	vipAddresses := []string{domain.APIDomainName, domain.IngressDomainName}
	return validations.CheckDNSRecordsExistence(vipAddresses, domain.ID, domain.Provider)
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

func (b *bareMetalInventory) getFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams, log logrus.FieldLogger) (models.FreeAddressesList, error) {
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

	err = b.hostApi.SetUploadLogsAt(ctx, currentHost, b.db)
	if err != nil {
		log.WithError(err).Errorf("Failed update host %s logs_collected_at flag", hostId)
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
		if _, ok := err.(s3wrapper.NotFound); ok {
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
		if _, ok := err.(s3wrapper.NotFound); ok {
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

func (b *bareMetalInventory) getHost(ctx context.Context, clusterId string, hostId string) (*models.Host, error) {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host

	if err := b.db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error; err != nil {
		log.WithError(err).Errorf("failed to find host: %s", hostId)
		return nil, common.NewApiError(http.StatusNotFound, errors.Errorf("Host %s not found", hostId))
	}
	return &host, nil
}

type returnHosts bool
type includeDeleted bool

func (b *bareMetalInventory) getCluster(ctx context.Context, clusterID string, flags ...interface{}) (*common.Cluster, error) {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	isUnscoped := funk.Contains(flags, includeDeleted(true))
	db := b.db
	if isUnscoped {
		db = b.db.Unscoped()
	}
	if funk.Contains(flags, returnHosts(true)) {
		db = db.Preload("Hosts", func(db *gorm.DB) *gorm.DB {
			if isUnscoped {
				return db.Unscoped()
			}
			return db
		})
	}

	if err := db.First(&cluster, "id = ?", clusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed to find cluster in db: %s", clusterID)
		if gorm.IsRecordNotFoundError(err) {
			return nil, common.NewApiError(http.StatusNotFound, err)
		} else {
			return nil, common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return &cluster, nil
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

func validateProxySettings(httpProxy, httpsProxy, noProxy *string) error {
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
		if err := validations.ValidateNoProxyFormat(*noProxy); err != nil {
			return err
		}
	}
	return nil
}

func proxySettingsForIgnition(httpProxy, httpsProxy, noProxy string) (string, error) {
	if httpProxy == "" && httpsProxy == "" {
		return "", nil
	}

	proxySettings := `"proxy": { {{.httpProxy}}{{.httpsProxy}}{{.noProxy}} }`
	var httpProxyAttr, httpsProxyAttr, noProxyAttr string
	if httpProxy != "" {
		httpProxyAttr = `"httpProxy": "` + httpProxy + `"`
	}
	if httpsProxy != "" {
		if httpProxy != "" {
			httpsProxyAttr = ", "
		}
		httpsProxyAttr += `"httpsProxy": "` + httpsProxy + `"`
	}
	if noProxy != "" {
		noProxyStr, err := json.Marshal(strings.Split(noProxy, ","))
		if err != nil {
			return "", err
		}
		noProxyAttr = `, "noProxy": ` + string(noProxyStr)
	}
	var proxyParams = map[string]string{
		"httpProxy":  httpProxyAttr,
		"httpsProxy": httpsProxyAttr,
		"noProxy":    noProxyAttr,
	}

	tmpl, err := template.New("proxySettings").Parse(proxySettings)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, proxyParams); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (b *bareMetalInventory) RegisterOCPCluster(ctx context.Context) error {
	log := logutil.FromContext(ctx, b.log)
	id := strfmt.UUID(uuid.New().String())
	url := installer.GetClusterURL{ClusterID: id}
	clusterName := "ocp-assisted-service-cluster"

	log.Infof("Register OCP cluster: %s with id %s", clusterName, id.String())

	apiVIP, baseDNSDomain, machineCidr, sshKey, err := b.getInstallConfigParamsFromOCP(log)
	if err != nil {
		return err
	}

	openshiftVersion, err := b.getOpenshiftVersionFromOCP(log)
	if err != nil {
		return err
	}

	cluster := common.Cluster{Cluster: models.Cluster{
		ID:                 &id,
		Href:               swag.String(url.String()),
		Kind:               swag.String(models.ClusterKindAddHostsOCPCluster),
		Name:               clusterName,
		OpenshiftVersion:   openshiftVersion,
		UserName:           auth.UserNameFromContext(ctx),
		OrgID:              auth.OrgIDFromContext(ctx),
		EmailDomain:        auth.EmailDomainFromContext(ctx),
		UpdatedAt:          strfmt.DateTime{},
		APIVip:             apiVIP,
		BaseDNSDomain:      baseDNSDomain,
		MachineNetworkCidr: machineCidr,
		SSHPublicKey:       sshKey,
	}}

	err = b.setPullSecretFromOCP(&cluster, log)
	if err != nil {
		return err
	}

	err = validations.ValidateClusterNameFormat(clusterName)
	if err != nil {
		log.WithError(err).Errorf("Failed to validate cluster name: %s", clusterName)
		return err
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
		log.WithError(err).Errorf("Failed to open transaction during RegisterOCPCluster")
		return err
	}

	// After registering the cluster, its status should be 'ClusterStatusAddingHosts'
	err = b.clusterApi.RegisterAddHostsOCPCluster(&cluster, tx)
	if err != nil {
		log.WithError(err).Errorf("failed to register cluster %s ", clusterName)
		return err
	}

	err = b.createInstalledOCPHosts(ctx, &cluster, tx, log)
	if err != nil {
		log.WithError(err).Errorf("failed to create installed nodes for ocp cluster %s ", clusterName)
		return err
	}
	if err := tx.Commit().Error; err != nil {
		log.WithError(err).Errorf("Failed to commit transaction in register OCP cluster")
		return err
	}
	txSuccess = true
	return nil
}

func (b *bareMetalInventory) getInstallConfigParamsFromOCP(log logrus.FieldLogger) (string, string, string, string, error) {
	configMap, err := b.k8sClient.GetConfigMap("kube-system", "cluster-config-v1")
	if err != nil {
		log.WithError(err).Errorf("Failed to get configmap cluster-config-v1 from namespace kube-system")
		return "", "", "", "", err
	}
	apiVIP, err := k8sclient.GetApiVIP(configMap, log)
	if err != nil {
		log.WithError(err).Errorf("Failed to get api VIP from configmap cluster-config-v1 from namespace kube-system")
		return "", "", "", "", err
	}
	log.Infof("apiVIP is %s", apiVIP)
	baseDomain, err := k8sclient.GetBaseDNSDomain(configMap, log)
	if err != nil {
		log.WithError(err).Errorf("Failed to get base domain from configmap cluster-config-v1 from namespace kube-system")
		return "", "", "", "", err
	}
	log.Infof("baseDomain is %s", baseDomain)
	machineCidr, err := k8sclient.GetMachineNetworkCIDR(configMap, log)
	if err != nil {
		log.WithError(err).Errorf("Failed to get machineCidr from configmap cluster-config-v1 from namespace kube-system")
		return "", "", "", "", err
	}
	log.Infof("machineCidr is %s", machineCidr)
	sshKey, err := k8sclient.GetSSHPublicKey(configMap, log)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ssh public key from configmap cluster-config-v1 from namespace kube-system")
		return "", "", "", "", err
	}
	return apiVIP, baseDomain, machineCidr, sshKey, nil
}

func (b *bareMetalInventory) getOpenshiftVersionFromOCP(log logrus.FieldLogger) (string, error) {
	clusterVersion, err := b.k8sClient.GetClusterVersion("version")
	if err != nil {
		log.WithError(err).Errorf("Failed to get cluster version from OCP")
		return "", err
	}
	return k8sclient.GetClusterVersion(clusterVersion)
}

func (b bareMetalInventory) PermanentlyDeleteUnregisteredClustersAndHosts() {
	if !b.leaderElector.IsLeader() {
		b.log.Debugf("Not a leader, exiting periodic clusters and hosts deletion")
		return
	}

	olderThen := strfmt.DateTime(time.Now().Add(-b.Config.DeletedUnregisteredAfter))
	b.log.Debugf(
		"Permanently deleting all clusters that were de-registered before %s",
		olderThen)
	if err := b.clusterApi.PermanentClustersDeletion(context.Background(), olderThen, b.objectHandler); err != nil {
		b.log.WithError(err).Errorf("Failed deleting de-registered clusters")
		return
	}

	b.log.Debugf(
		"Permanently deleting all hosts that were soft-deleted before %s",
		olderThen)
	if err := b.hostApi.PermanentHostsDeletion(olderThen); err != nil {
		b.log.WithError(err).Errorf("Failed deleting soft-deleted hosts")
		return
	}
}

func secretValidationToUserError(err error) error {

	if _, ok := err.(*validations.PullSecretError); ok {
		return err
	}

	return errors.New("Failed validating pull secret")
}

func (b *bareMetalInventory) createInstalledOCPHosts(ctx context.Context, cluster *common.Cluster, tx *gorm.DB, log logrus.FieldLogger) error {
	nodes, err := b.k8sClient.ListNodes()
	if err != nil {
		log.WithError(err).Errorf("Failed to list OCP nodes")
		return err
	}

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if !k8sclient.IsNodeReady(node) {
			log.Infof("Node %s is not in ready state, skipping..", node.Name)
			continue
		}
		id := strfmt.UUID(uuid.New().String())
		url := installer.GetHostURL{ClusterID: *cluster.ID, HostID: id}
		hostname := node.Name
		role := k8sclient.GetNodeRole(node)

		inventory, err := b.getOCPHostInventory(node, cluster.MachineNetworkCidr)
		if err != nil {
			log.WithError(err).Errorf("Failed to create inventory for host %s, cluster %s", id, *cluster.ID)
			return err
		}

		host := models.Host{
			ID:                &id,
			Href:              swag.String(url.String()),
			Kind:              swag.String(models.HostKindAddToExistingClusterOCPHost),
			ClusterID:         *cluster.ID,
			CheckedInAt:       strfmt.DateTime(time.Now()),
			UserName:          auth.UserNameFromContext(ctx),
			Role:              role,
			RequestedHostname: hostname,
			Inventory:         inventory,
		}

		err = b.hostApi.RegisterInstalledOCPHost(ctx, &host, tx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *bareMetalInventory) getOCPHostInventory(node *v1.Node, machineNetworkCidr string) (string, error) {
	hostname := node.Name
	ip := k8sclient.GetNodeInternalIP(node)
	ipWithCidr, err := network.CreateIpWithCidr(ip, machineNetworkCidr)
	if err != nil {
		return "", err
	}
	arch := node.Status.NodeInfo.Architecture
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: append(make([]string, 0), ipWithCidr),
				MacAddress:    "some MAC address",
			},
		},
		Hostname: hostname,
		CPU:      &models.CPU{Architecture: arch},
		Memory:   &models.Memory{},
		Disks:    []*models.Disk{{}},
	}
	ret, err := json.Marshal(&inventory)
	return string(ret), err
}

func (b *bareMetalInventory) setPullSecretFromOCP(cluster *common.Cluster, log logrus.FieldLogger) error {
	secret, err := b.k8sClient.GetSecret("openshift-config", "pull-secret")
	if err != nil {
		log.WithError(err).Errorf("Failed to get secret pull-secret from openshift-config namespce")
		return err
	}
	pullSecret, err := k8sclient.GetDataByKeyFromSecret(secret, ".dockerconfigjson")
	if err != nil {
		log.WithError(err).Errorf("Failed to extract .dockerconfigjson from secret pull-secret")
		return err
	}
	setPullSecret(cluster, pullSecret)
	return nil
}

func formatStaticIPs(staticIpsConfig []*models.StaticIPConfig) string {
	lines := make([]string, len(staticIpsConfig))

	// construct static IPs config to string
	for i, entry := range staticIpsConfig {
		elements := []string{entry.Mac}
		elements = append(elements, getStaticIPsData(entry.IPV4Config)...)
		elements = append(elements, getStaticIPsData((*models.StaticIPV4Config)(entry.IPV6Config))...)
		lines[i] = strings.Join(elements, ";")
	}

	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

func formatVlans(vlansConfig []*models.VlanConfig) string {
	lines := make([]string, len(vlansConfig))
	// construct vlans config to string
	for i, entry := range vlansConfig {
		elements := []string{entry.Mac, strconv.FormatInt(entry.ID, 10)}
		elements = append(elements, getStaticIPsData(entry.IPV4Config)...)
		elements = append(elements, getStaticIPsData((*models.StaticIPV4Config)(entry.IPV6Config))...)
		lines[i] = strings.Join(elements, ";")
	}

	sort.Strings(lines)

	return strings.Join(lines, "\n")
}

func getStaticIPsData(static_config *models.StaticIPV4Config) []string {
	if static_config != nil {
		return []string{static_config.IP, static_config.Mask, static_config.DNS, static_config.Gateway}
	}
	return []string{"", "", "", ""}
}

func (b *bareMetalInventory) GetClusterByKubeKey(key types.NamespacedName) (*common.Cluster, error) {
	return b.clusterApi.GetClusterByKubeKey(key)
}
