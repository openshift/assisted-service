package bminventory

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

const kubeconfig = "kubeconfig"

const (
	ResourceKindHost    = "Host"
	ResourceKindCluster = "Cluster"
)

const DefaultUser = "kubeadmin"
const ConsoleUrlPrefix = "https://console-openshift-console.apps"

var (
	DefaultClusterNetworkCidr       = "10.128.0.0/14"
	DefaultClusterNetworkHostPrefix = int64(23)
	DefaultServiceNetworkCidr       = "172.30.0.0/16"
)

type Config struct {
	ImageBuilder         string            `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/assisted-iso-create:latest"`
	AgentDockerImg       string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/assisted-installer-agent:latest"`
	IgnitionGenerator    string            `envconfig:"IGNITION_GENERATE_IMAGE" default:"quay.io/ocpmetal/assisted-ignition-generator:latest"` // TODO: update the latest once the repository has git workflow
	ServiceBaseURL       string            `envconfig:"SERVICE_BASE_URL"`
	S3EndpointURL        string            `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket             string            `envconfig:"S3_BUCKET" default:"test"`
	ImageExpirationTime  time.Duration     `envconfig:"IMAGE_EXPIRATION_TIME" default:"60m"`
	AwsAccessKeyID       string            `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey   string            `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	BaseDNSDomains       map[string]string `envconfig:"BASE_DNS_DOMAINS" default:""`
	SkipCertVerification bool              `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	InstallRHCa          bool              `envconfig:"INSTALL_RH_CA" default:"false"`
	ServiceIPs           string            `envconfig:"SERVICE_IPS" default:""`
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

type IgnitionConfigFormat struct {
	Ignition struct {
		Version string `json:"version"`
	} `json:"ignition"`
	Passwd struct {
		Users []struct {
			Name              string   `json:"name"`
			PasswordHash      string   `json:"passwordHash"`
			SSHAuthorizedKeys []string `json:"sshAuthorizedKeys"`
			Groups            []string `json:"groups"`
		} `json:"users"`
	} `json:"passwd"`
	Systemd struct {
		Units []struct {
			Name     string `json:"name"`
			Enabled  bool   `json:"enabled"`
			Contents string `json:"contents"`
		} `json:"units"`
	} `json:"systemd"`
	Storage struct {
		Files []struct {
			Filesystem string `json:"filesystem"`
			Path       string `json:"path"`
			Mode       int    `json:"mode"`
			Contents   struct {
				Source string `json:"source"`
			} `json:"contents"`
		} `json:"files"`
	} `json:"storage"`
}

type OnPremIgnitionConfigFormat struct {
	Storage struct {
		Files []struct {
			Filesystem string `json:"filesystem"`
			Path       string `json:"path"`
			Mode       int    `json:"mode"`
			Contents   struct {
				Source string `json:"source"`
			} `json:"contents"`
		} `json:"files"`
	} `json:"storage"`
}

const ignitionConfig = `{
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
  "contents": "[Service]\nType=simple\nRestart=always\nRestartSec=3\nStartLimitIntervalSec=0\nEnvironment=HTTP_PROXY={{.HTTPProxy}}\nEnvironment=http_proxy={{.HTTPProxy}}\nEnvironment=HTTPS_PROXY={{.HTTPSProxy}}\nEnvironment=https_proxy={{.HTTPSProxy}}\nEnvironment=NO_PROXY={{.NoProxy}}\nEnvironment=no_proxy={{.NoProxy}}\nEnvironment=PULL_SECRET_TOKEN={{.PullSecretToken}}\nExecStartPre=podman run --privileged --rm -v /usr/local/bin:/hostbin {{.AgentDockerImg}} cp /usr/bin/agent /hostbin\nExecStart=/usr/local/bin/agent --url {{.ServiceBaseURL}} --cluster-id {{.clusterId}} --agent-version {{.AgentDockerImg}} --insecure={{.SkipCertVerification}}\n\n[Install]\nWantedBy=multi-user.target"
  }]
  },
  "storage": {
	  "files": [{
		"overwrite": true,
		"path": "/etc/motd",
		"mode": 644,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:,{{.AGENT_MOTD}}" }
	  }{{if .RH_ROOT_CA}},
	  {
		"overwrite": true,
		"path": "/etc/pki/ca-trust/source/anchors/rh-it-root-ca.crt",
		"mode": 644,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:,{{.RH_ROOT_CA}}" }
	  }{{end}}]
	}
  }`
  

const onPremIgnitionConfig = `{
	"storage": {
		"files": [{
		"filesystem": "root",
		"path": "/etc/hosts",
		"mode": 420,
		"append": true,
		"contents": { "source": "{{.ASSISTED_INSTALLER_IPS}}" }
		}]
	  }
	}`

var clusterFileNames = []string{
	"kubeconfig",
	"bootstrap.ign",
	"master.ign",
	"worker.ign",
	"metadata.json",
	"kubeadmin-password",
	"kubeconfig-noingress",
	"install-config.yaml",
}

type bareMetalInventory struct {
	Config
	db            *gorm.DB
	log           logrus.FieldLogger
	hostApi       host.API
	clusterApi    cluster.API
	eventsHandler events.Handler
	objectHandler s3wrapper.API
	metricApi     metrics.API
	generator     generator.ISOInstallConfigGenerator
}
