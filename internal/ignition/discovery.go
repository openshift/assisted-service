package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	clusterPkg "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
)

// Names of some relevant templates:
const (
	discoveryIgnTemplateName = "discovery.ign"
	nodeIgnTemplateName      = "node.ign"
)

const agentMessageOfTheDay = `
**  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  ** **  **  **  **  **  **  **
This is a host being installed by the OpenShift Assisted Installer.
It will be installed from scratch during the installation.

The primary service is agent.service. To watch its status, run:
sudo journalctl -u agent.service

To view the agent log, run:
sudo journalctl TAG=agent
**  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  **  ** **  **  **  **  **  **  **
`

const RedhatRootCA = `
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

const agentFixBZ1964591 = `#!/usr/bin/sh

# This script is a workaround for bugzilla 1964591 where symlinks inside /var/lib/containers/ get
# corrupted under some circumstances.
#
# In order to let agent.service start correctly we are checking here whether the requested
# container image exists and in case "podman images" returns an error we try removing the faulty
# image.
#
# In such a scenario agent.service will detect the image is not present and pull it again. In case
# the image is present and can be detected correctly, no any action is required.

IMAGE=$(echo $1 | sed 's/[@:].*//')
podman images | grep $IMAGE || podman rmi --force $1 || true
`

const okdBinariesOverlayTemplate = `#!/bin/env bash
set -eux
# Fetch an image with OKD rpms
RPMS_IMAGE="%s"
while ! podman pull --quiet "${RPMS_IMAGE}"
do
    echo "Pull failed. Retrying ${RPMS_IMAGE}..."
    sleep 5
done
mnt=$(podman image mount "${RPMS_IMAGE}")
# Install RPMs in overlayed FS
mkdir /tmp/rpms
cp -rvf ${mnt}/rpms/* /tmp/rpms
# If RPMs image contants manifests these need to be copied as well
mkdir -p /opt/openshift/openshift
cp -rvf ${mnt}/manifests/* /opt/openshift/openshift || true
tmpd=$(mktemp -d)
mkdir ${tmpd}/{upper,work}
mount -t overlay -o lowerdir=/usr,upperdir=${tmpd}/upper,workdir=${tmpd}/work overlay /usr
rpm -Uvh /tmp/rpms/*
podman rmi -f "${RPMS_IMAGE}"
# Symlink kubelet pull secret
mkdir -p /var/lib/kubelet
ln -s /root/.docker/config.json /var/lib/kubelet/config.json
# Expand /var to 6G if necessary
if (( $(stat -c%%s /run/ephemeral.xfsloop) > 6*1024*1024*1024 )); then
  exit 0
fi
/bin/truncate -s 6G /run/ephemeral.xfsloop
losetup -c /dev/loop0
xfs_growfs /var
mount -o remount,size=6G /run
`

const okdHoldAgentUntilBinariesLanded = `[Unit]
Wants=okd-overlay.service
After=okd-overlay.service
`

const okdHoldPivot = `[Unit]
ConditionPathExists=/enoent
`

const tempNMConnectionsDir = "/etc/assisted/network"

// IgnitionBuilder defines the ignition formatting methods for the various images
//
//go:generate mockgen -source=discovery.go -package=ignition -destination=mock_ignition_builder.go
type IgnitionBuilder interface {
	FormatDiscoveryIgnitionFile(ctx context.Context, infraEnv *common.InfraEnv, cfg IgnitionConfig, safeForLogs bool, authType auth.AuthType, overrideDiscoveryISOType string) (string, error)
	FormatSecondDayWorkerIgnitionFile(url string, caCert *string, bearerToken, ignitionEndpointHTTPHeaders string, host *models.Host) ([]byte, error)
}

// IgnitionConfig contains the attributes required to build the discovery ignition file
type IgnitionConfig struct {
	AgentDockerImg       string        `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	AgentTimeoutStart    time.Duration `envconfig:"AGENT_TIMEOUT_START" default:"10m"`
	InstallRHCa          bool          `envconfig:"INSTALL_RH_CA" default:"false"`
	ServiceBaseURL       string        `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath    string        `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	SkipCertVerification bool          `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	EnableOKDSupport     bool          `envconfig:"ENABLE_OKD_SUPPORT" default:"true"`
	OKDRPMsImage         string        `envconfig:"OKD_RPMS_IMAGE" default:""`
}

type ignitionBuilder struct {
	log                     logrus.FieldLogger
	templates               *template.Template
	staticNetworkConfig     staticnetworkconfig.StaticNetworkConfig
	mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder
	ocRelease               oc.Release
	versionHandler          versions.Handler
}

func NewBuilder(log logrus.FieldLogger, staticNetworkConfig staticnetworkconfig.StaticNetworkConfig,
	mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder, ocRelease oc.Release, versionHandler versions.Handler) (result IgnitionBuilder, err error) {
	// Parse the templates file system:
	templates, err := loadTemplates()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &ignitionBuilder{
		log:                     log,
		templates:               templates,
		staticNetworkConfig:     staticNetworkConfig,
		mirrorRegistriesBuilder: mirrorRegistriesBuilder,
		ocRelease:               ocRelease,
		versionHandler:          versionHandler,
	}
	return
}

func (ib *ignitionBuilder) shouldAppendOKDFiles(ctx context.Context, infraEnv *common.InfraEnv, cfg IgnitionConfig) (string, bool) {
	if !cfg.EnableOKDSupport {
		return "", false
	}
	// Use OKD override if OKD_RPMS_IMAGE explicitly set in config
	if cfg.OKDRPMsImage != "" {
		return cfg.OKDRPMsImage, true
	}
	// Check if selected payload contains `okd-rpms` image
	releaseImage, err := ib.versionHandler.GetReleaseImage(ctx, infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture, infraEnv.PullSecret)
	if err != nil {
		ib.log.Warnf("unable to find release image for %s/%s", infraEnv.OpenshiftVersion, infraEnv.CPUArchitecture)
		return "", false
	}
	okdRpmsImage, err := ib.ocRelease.GetOKDRPMSImage(ib.log, *releaseImage.URL, "", infraEnv.PullSecret)
	if err != nil {
		return "", false
	}
	return okdRpmsImage, true
}

func (ib *ignitionBuilder) FormatDiscoveryIgnitionFile(ctx context.Context, infraEnv *common.InfraEnv, cfg IgnitionConfig, safeForLogs bool, authType auth.AuthType, overrideDiscoveryISOType string) (string, error) {
	pullSecretToken, err := clusterPkg.AgentToken(infraEnv, authType)
	if err != nil {
		return "", err
	}
	httpProxy, httpsProxy, noProxy := common.GetProxyConfigs(infraEnv.Proxy)
	proxySettings, err := proxySettingsForIgnition(httpProxy, httpsProxy, noProxy)
	if err != nil {
		return "", err
	}
	rhCa := ""
	if cfg.InstallRHCa {
		rhCa = url.PathEscape(RedhatRootCA)
	}
	userSshKey, err := getUserSSHKey(infraEnv.SSHAuthorizedKey)
	if err != nil {
		ib.log.WithError(err).Errorln("Unable to build user SSH public key JSON")
		return "", err
	}

	// If the list of additional NTP sources is empty then we want to pass an empty list to the
	// template, but the Split method returns a slice with one empty element in that case.
	additionalNtpSources := strings.Split(infraEnv.AdditionalNtpSources, ",")
	if len(additionalNtpSources) == 1 && additionalNtpSources[0] == "" {
		additionalNtpSources = []string{}
	}

	var ignitionParams = map[string]interface{}{
		"userSshKey":          userSshKey,
		"AgentDockerImg":      cfg.AgentDockerImg,
		"ServiceBaseURL":      strings.TrimSpace(cfg.ServiceBaseURL),
		"infraEnvId":          infraEnv.ID.String(),
		"PullSecretToken":     pullSecretToken,
		"AGENT_MOTD":          url.PathEscape(agentMessageOfTheDay),
		"AGENT_FIX_BZ1964591": url.PathEscape(agentFixBZ1964591),
		"IPv6_CONF":           url.PathEscape(common.Ipv6DuidDiscoveryConf),
		"PULL_SECRET":         url.PathEscape(infraEnv.PullSecret),
		"RH_ROOT_CA":          rhCa,
		"PROXY_SETTINGS":      proxySettings,
		// escape '%' in agent.service proxy urls,
		// for more info https://github.com/systemd/systemd/blob/a1b2c92d8290c76a29ccd0887a92ac064e1bb5a1/NEWS#L10
		"HTTPProxy":            strings.ReplaceAll(httpProxy, "%", "%%"),
		"HTTPSProxy":           strings.ReplaceAll(httpsProxy, "%", "%%"),
		"NoProxy":              noProxy,
		"SkipCertVerification": strconv.FormatBool(cfg.SkipCertVerification),
		"AgentTimeoutStartSec": strconv.FormatInt(int64(cfg.AgentTimeoutStart.Seconds()), 10),
		"SELINUX_POLICY":       base64.StdEncoding.EncodeToString([]byte(selinuxPolicy)),
		"EnableAgentService":   infraEnv.InternalIgnitionConfigOverride == "",
		"ProfileProxyExports":  dataurl.EncodeBytes([]byte(GetProfileProxyEntries(httpProxy, httpsProxy, noProxy))),
		"AdditionalNtpSources": additionalNtpSources,
	}
	if safeForLogs {
		for _, key := range []string{"userSshKey", "PullSecretToken", "PULL_SECRET", "RH_ROOT_CA"} {
			ignitionParams[key] = "*****"
		}
	}
	if cfg.ServiceCACertPath != "" {
		var caCertData []byte
		caCertData, err = os.ReadFile(cfg.ServiceCACertPath)
		if err != nil {
			return "", err
		}
		ignitionParams["ServiceCACertData"] = dataurl.EncodeBytes(caCertData)
		ignitionParams["HostCACertPath"] = common.HostCACertPath
	}
	if infraEnv.AdditionalTrustBundle != "" {
		ignitionParams["AdditionalTrustBundle"] = dataurl.EncodeBytes([]byte(infraEnv.AdditionalTrustBundle))
		ignitionParams["AdditionalTrustBundlePath"] = common.AdditionalTrustBundlePath
	}

	isoType := overrideDiscoveryISOType
	if overrideDiscoveryISOType == "" {
		isoType = string(common.ImageTypeValue(infraEnv.Type))
	}
	if infraEnv.StaticNetworkConfig != "" && models.ImageType(isoType) == models.ImageTypeFullIso {
		var filesList []staticnetworkconfig.StaticNetworkConfigData
		var newErr error

		// backward compatibility - nmstate.service has been available on RHCOS since version 4.14+, therefore, we should maintain both flows
		var ok bool
		ok, err = staticnetworkconfig.NMStatectlServiceSupported(infraEnv.OpenshiftVersion, common.X86CPUArchitecture)
		if err != nil {
			return "", err
		}

		if ok {
			ib.log.Info("Static network configuration using the nmstatectl service")
			filesList, newErr = ib.prepareStaticNetworkConfigYAMLForIgnition(infraEnv)
			ignitionParams["StaticNetworkConfigWithNmstatectl"] = filesList
			ignitionParams["PreNetworkConfigScript"] = base64.StdEncoding.EncodeToString([]byte(constants.PreNetworkConfigScriptWithNmstatectl))
			ignitionParams["CommonScriptFunctions"] = base64.StdEncoding.EncodeToString([]byte(constants.CommonNetworkScript))
		} else {
			ib.log.Info("Static network configuration using generated keyfiles")
			filesList, newErr = ib.prepareStaticNetworkConfigForIgnition(ctx, infraEnv)
			ignitionParams["StaticNetworkConfig"] = filesList
			ignitionParams["PreNetworkConfigScript"] = base64.StdEncoding.EncodeToString([]byte(constants.PreNetworkConfigScript))
		}

		if newErr != nil {
			ib.log.WithError(newErr).Errorf("Failed to add static network config to ignition for infra env  %s", infraEnv.ID)
			return "", newErr
		}
	}

	if ib.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		caContents, mirrorsErr := ib.mirrorRegistriesBuilder.GetMirrorCA()
		if mirrorsErr != nil {
			ib.log.WithError(mirrorsErr).Errorf("Failed to get the mirror registries CA contents")
			return "", mirrorsErr
		}
		registriesContents, mirrorsErr := ib.mirrorRegistriesBuilder.GetMirrorRegistries()
		if mirrorsErr != nil {
			ib.log.WithError(mirrorsErr).Errorf("Failed to get the mirror registries config contents")
			return "", mirrorsErr
		}
		ignitionParams["MirrorRegistriesConfig"] = base64.StdEncoding.EncodeToString(registriesContents)
		ignitionParams["MirrorRegistriesCAConfig"] = base64.StdEncoding.EncodeToString(caContents)
	}

	if okdRpmsImage, ok := ib.shouldAppendOKDFiles(ctx, infraEnv, cfg); ok {
		okdBinariesOverlay := fmt.Sprintf(okdBinariesOverlayTemplate, okdRpmsImage)
		ignitionParams["OKDBinaries"] = base64.StdEncoding.EncodeToString([]byte(okdBinariesOverlay))
		ignitionParams["OKDHoldPivot"] = base64.StdEncoding.EncodeToString([]byte(okdHoldPivot))
		ignitionParams["OKDHoldAgent"] = base64.StdEncoding.EncodeToString([]byte(okdHoldAgentUntilBinariesLanded))
	}
	tmpl := ib.templates.Lookup(discoveryIgnTemplateName)
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, ignitionParams); err != nil {
		return "", err
	}

	res := buf.String()
	if infraEnv.InternalIgnitionConfigOverride != "" {
		res, err = MergeIgnitionConfig([]byte(res), []byte(infraEnv.InternalIgnitionConfigOverride))
		if err != nil {
			return "", err
		}
		ib.log.Infof("Applying internal ignition override %s for infra env %s", infraEnv.InternalIgnitionConfigOverride, infraEnv.ID)
	}

	if infraEnv.IgnitionConfigOverride != "" {
		res, err = MergeIgnitionConfig([]byte(res), []byte(infraEnv.IgnitionConfigOverride))
		if err != nil {
			return "", err
		}
		ib.log.Infof("Applying ignition override %s for infra env %s, resulting ignition: %s", infraEnv.IgnitionConfigOverride, infraEnv.ID, res)
	}

	return res, nil
}

func (ib *ignitionBuilder) prepareStaticNetworkConfigYAMLForIgnition(infraEnv *common.InfraEnv) ([]staticnetworkconfig.StaticNetworkConfigData, error) {
	filesList, err := ib.staticNetworkConfig.GenerateStaticNetworkConfigDataYAML(infraEnv.StaticNetworkConfig)
	if err != nil {
		ib.log.WithError(err).Errorf("staticNetworkGenerator failed to produce the nmpolicy files for cluster %s", infraEnv.ID)
		return nil, err
	}
	for i := range filesList {
		filesList[i].FilePath = filepath.Join(tempNMConnectionsDir, filesList[i].FilePath)
		filesList[i].FileContents = base64.StdEncoding.EncodeToString([]byte(filesList[i].FileContents))
	}

	return filesList, nil
}

func (ib *ignitionBuilder) prepareStaticNetworkConfigForIgnition(ctx context.Context, infraEnv *common.InfraEnv) ([]staticnetworkconfig.StaticNetworkConfigData, error) {
	filesList, err := ib.staticNetworkConfig.GenerateStaticNetworkConfigData(ctx, infraEnv.StaticNetworkConfig)
	if err != nil {
		ib.log.WithError(err).Errorf("staticNetworkGenerator failed to produce the static network connection files for cluster %s", infraEnv.ID)
		return nil, err
	}
	for i := range filesList {
		filesList[i].FilePath = filepath.Join(tempNMConnectionsDir, filesList[i].FilePath)
		filesList[i].FileContents = base64.StdEncoding.EncodeToString([]byte(filesList[i].FileContents))
	}

	return filesList, nil
}

func (ib *ignitionBuilder) FormatSecondDayWorkerIgnitionFile(url string, caCert *string, bearerToken, ignitionEndpointHTTPHeaders string, host *models.Host) ([]byte, error) {
	var ignitionParams = map[string]interface{}{
		// https://github.com/openshift/machine-config-operator/blob/master/docs/MachineConfigServer.md#endpoint
		"SOURCE":  url,
		"HEADERS": map[string]string{},
		"CACERT":  "",
	}
	if bearerToken != "" {
		ignitionParams["HEADERS"].(map[string]string)["Authorization"] = fmt.Sprintf("Bearer %s", bearerToken)
	}

	if ignitionEndpointHTTPHeaders != "" {
		additionalHeaders := make(map[string]string)
		if err := json.Unmarshal([]byte(ignitionEndpointHTTPHeaders), &additionalHeaders); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal ignition endpoint HTTP headers for host %s", host.ID)
		}
		for k, v := range additionalHeaders {
			ignitionParams["HEADERS"].(map[string]string)[k] = v
		}

	}

	if caCert != nil {
		ignitionParams["CACERT"] = fmt.Sprintf("data:text/plain;base64,%s", *caCert)
	}

	tmpl := ib.templates.Lookup(nodeIgnTemplateName)
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, ignitionParams); err != nil {
		return nil, err
	}

	overrides := buf.String()
	if host.IgnitionConfigOverrides != "" {
		var err error
		overrides, err = MergeIgnitionConfig(buf.Bytes(), []byte(host.IgnitionConfigOverrides))
		if err != nil {
			return []byte(""), errors.Wrapf(err, "Failed to apply ignition override for host %s", host.ID)
		}
		ib.log.Infof("Applied ignition override for host %s", host.ID)
		ib.log.Debugf("Ignition override for day2 host %s: %s", host.ID, overrides)
	}

	res, err := SetHostnameForNodeIgnition([]byte(overrides), host)
	if err != nil {
		return []byte(""), errors.Wrapf(err, "Failed to set hostname in ignition for host %s", host.ID)
	}

	ib.log.Debugf("Final ignition for day2 host %s: %s", host.ID, string(res))
	return res, nil
}

func GetProfileProxyEntries(http_proxy string, https_proxy string, no_proxy string) string {
	entries := []string{}
	if len(http_proxy) > 0 {
		entries = append(entries, fmt.Sprintf("export HTTP_PROXY=%[1]s\nexport http_proxy=%[1]s", http_proxy))
	}
	if len(https_proxy) > 0 {
		entries = append(entries, fmt.Sprintf("export HTTPS_PROXY=%[1]s\nexport https_proxy=%[1]s", https_proxy))
	}
	if len(no_proxy) > 0 {
		entries = append(entries, fmt.Sprintf("export NO_PROXY=%[1]s\nexport no_proxy=%[1]s", no_proxy))
	}
	return strings.Join(entries, "\n") + "\n"
}

func SetHostnameForNodeIgnition(ignition []byte, host *models.Host) ([]byte, error) {
	config, err := ParseToLatest(ignition)
	if err != nil {
		return nil, errors.Errorf("error parsing ignition: %v", err)
	}

	hostname, err := hostutil.GetCurrentHostName(host)
	if err != nil {
		return nil, errors.Errorf("failed to get hostname for host %s", host.ID)
	}

	setFileInIgnition(config, "/etc/hostname", fmt.Sprintf("data:,%s", hostname), false, 420, true)

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return configBytes, nil
}

func getUserSSHKey(sshKey string) (string, error) {
	keys := buildUserSshKeysSlice(sshKey)
	if len(keys) == 0 {
		return "", nil
	}
	userAuthBlock := make(map[string]interface{})
	userAuthBlock["name"] = "core"
	userAuthBlock["passwordHash"] = "!"
	userAuthBlock["sshAuthorizedKeys"] = keys
	userAuthBlock["groups"] = [1]string{"sudo"}
	blockByte, err := json.Marshal(userAuthBlock)
	if err != nil {
		return "", fmt.Errorf("failed to build user ssh key block: %w", err)
	}
	return string(blockByte), nil
}

func buildUserSshKeysSlice(sshKey string) []string {
	keys := strings.Split(sshKey, "\n")
	validKeys := []string{}
	// filter only valid non empty keys
	for i := range keys {
		keys[i] = strings.TrimSpace(keys[i])
		if keys[i] != "" {
			validKeys = append(validKeys, keys[i])
		}
	}
	return validKeys
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
