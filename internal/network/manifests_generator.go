package network

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"text/template"

	"github.com/go-openapi/swag"
	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/tang"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

//go:generate mockgen -source=manifests_generator.go -package=network -destination=mock_manifests_generator.go

type ManifestsGeneratorAPI interface {
	AddChronyManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddDnsmasqForSingleNode(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddNodeIpHint(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error
	AddTelemeterManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddSchedulableMastersManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddDiskEncryptionManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	IsSNODNSMasqEnabled() bool
}

type Config struct {
	ServiceBaseURL          string `envconfig:"SERVICE_BASE_URL"`
	EnableSingleNodeDnsmasq bool   `envconfig:"ENABLE_SINGLE_NODE_DNSMASQ" default:"false"`
}

type ManifestsGenerator struct {
	manifestsApi manifestsapi.ManifestsAPI
	Config       Config
}

func NewManifestsGenerator(manifestsApi manifestsapi.ManifestsAPI, config Config) *ManifestsGenerator {
	return &ManifestsGenerator{
		manifestsApi: manifestsApi,
		Config:       config,
	}
}

const defaultChronyConf = `
pool 0.rhel.pool.ntp.org iburst
driftfile /var/lib/chrony/drift
makestep 1.0 3
rtcsync
logdir /var/log/chrony`

const ntpMachineConfigManifest = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: {{.ROLE}}
  name: 50-{{.ROLE}}s-chrony-configuration
spec:
  config:
    ignition:
      version: 3.1.0
    storage:
      files:
      - contents:
          source: data:text/plain;charset=utf-8;base64,{{.CHRONY_CONTENT}}
        mode: 420
        path: /etc/chrony.conf
        overwrite: true
`

const snoDnsmasqConf = `
address=/apps.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
address=/api-int.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
address=/api.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
`

const unmanagedResolvConf = `
[main]
rc-manager=unmanaged
`

const forceDnsDispatcherScript = `#!/bin/bash
export IP="{{.HOST_IP}}"
export BASE_RESOLV_CONF=/run/NetworkManager/resolv.conf
if [ "$2" = "dhcp4-change" ] || [ "$2" = "dhcp6-change" ] || [ "$2" = "up" ] || [ "$2" = "connectivity-change" ]; then
	export TMP_FILE=$(mktemp /etc/forcedns_resolv.conf.XXXXXX)
	cp  $BASE_RESOLV_CONF $TMP_FILE
	chmod --reference=$BASE_RESOLV_CONF $TMP_FILE
	sed -i -e "s/{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}//" \
	-e "s/search /& {{.CLUSTER_NAME}}.{{.DNS_DOMAIN}} /" \
	-e "0,/nameserver/s/nameserver/& $IP\n&/" $TMP_FILE
	mv $TMP_FILE /etc/resolv.conf
fi
`

const dnsMachineConfigManifest = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 50-master-dnsmasq-configuration
spec:
  config:
    ignition:
      version: 3.1.0
    storage:
      files:
        - contents:
            source: data:text/plain;charset=utf-8;base64,{{.DNSMASQ_CONTENT}}
          mode: 420
          path: /etc/dnsmasq.d/single-node.conf
          overwrite: true
        - contents:
            source: data:text/plain;charset=utf-8;base64,{{.FORCE_DNS_SCRIPT}}
          mode: 365
          path: /etc/NetworkManager/dispatcher.d/forcedns
          overwrite: true
        - contents:
            source: data:text/plain;charset=utf-8;base64,{{.UNMANAGED_RESOLV_CONF}}
          mode: 420
          path: /etc/NetworkManager/conf.d/single-node.conf
          overwrite: true
    systemd:
      units:
        - name: dnsmasq.service
          enabled: true
          contents: |
            [Unit]
            Description=Run dnsmasq to provide local dns for Single Node OpenShift
            Before=kubelet.service crio.service
            After=network.target

            [Service]
            ExecStart=/usr/sbin/dnsmasq -k

            [Install]
            WantedBy=multi-user.target
`

const schedulableMastersManifestPatch = `---
- op: replace
  path: /spec/mastersSchedulable
  value: true
`

func createChronyManifestContent(c *common.Cluster, role models.HostRole, log logrus.FieldLogger) ([]byte, error) {
	sources := make([]string, 0)

	for _, host := range c.Hosts {
		if host.NtpSources == "" {
			continue
		}

		var ntpSources []*models.NtpSource
		if err := json.Unmarshal([]byte(host.NtpSources), &ntpSources); err != nil {
			return nil, errors.Wrapf(err, "Failed to unmarshal %s", host.NtpSources)
		}

		for _, source := range ntpSources {
			if !funk.Contains(sources, source.SourceName) {
				sources = append(sources, source.SourceName)
			}
		}
	}

	content := defaultChronyConf[:]

	for _, source := range sources {
		content += fmt.Sprintf("\nserver %s iburst", source)
	}

	var manifestParams = map[string]interface{}{
		"CHRONY_CONTENT": base64.StdEncoding.EncodeToString([]byte(content)),
		"ROLE":           string(role),
	}

	return fillTemplate(manifestParams, ntpMachineConfigManifest, log)
}

func (m *ManifestsGenerator) AddChronyManifest(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error {
	for _, role := range []models.HostRole{models.HostRoleMaster, models.HostRoleWorker} {
		content, err := createChronyManifestContent(cluster, role, log)

		if err != nil {
			return errors.Wrapf(err, "Failed to create chrony manifest content for role %s cluster id %s", role, *cluster.ID)
		}

		chronyManifestFileName := fmt.Sprintf("50-%ss-chrony-configuration.yaml", string(role))
		err = m.createManifests(ctx, cluster, chronyManifestFileName, content)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *ManifestsGenerator) AddSchedulableMastersManifest(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error {
	content := []byte(schedulableMastersManifestPatch)
	schedulableMastersManifestFile := "cluster-scheduler-02-config.yml.patch_ai_set_masters_schedulable"
	err := m.createManifests(ctx, cluster, schedulableMastersManifestFile, content)
	if err != nil {
		return err
	}
	return nil
}

const diskEncryptionManifest = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  name: {{ .ROLE }}-{{ .MODE }}
  labels:
    machineconfiguration.openshift.io/role: {{ .ROLE }}
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      luks:
        - name: root
          device: /dev/disk/by-partlabel/root
          clevis:
		  {{- if eq .MODE "tpm" }}
            tpm2: true
		  {{- else if eq .MODE "tang" }}
            tang:
            {{- range .TANG_SERVERS }}
              - url: {{ .Url }}
                thumbprint: {{ .Thumbprint }}
            {{- end }}
		  {{- end }}
          options: [--cipher, aes-cbc-essiv:sha256]
          wipeVolume: true
      filesystems:
        - device: /dev/mapper/root
          format: xfs
          wipeFilesystem: true
          label: root
{{- if eq .MODE "tang" }}
  kernelArguments:
    - rd.neednet=1
{{- end }}`

func (m *ManifestsGenerator) createDiskEncryptionManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster,
	manifestParams map[string]interface{}) error {

	log.Infof("Creating manifest to encrypt installation disk on %s nodes using %s encryption", manifestParams["ROLE"], manifestParams["ROLE"])

	content, err := fillTemplate(manifestParams, diskEncryptionManifest, log)
	if err != nil {
		log.WithError(err).Error("Failed to parse disk encryption manifest's template")
		return err
	}

	filename := fmt.Sprintf("99-openshift-%s-%s-encryption.yaml", manifestParams["ROLE"], manifestParams["MODE"])
	if err := m.createManifests(ctx, c, filename, content); err != nil {
		log.WithError(err).Errorf("Failed to create manifest to encrypt installation disk")
		return err
	}

	return nil
}

func (m *ManifestsGenerator) AddDiskEncryptionManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {

	if swag.StringValue(c.DiskEncryption.EnableOn) == models.DiskEncryptionEnableOnNone {
		return nil
	}

	manifestParams := map[string]interface{}{}

	switch *c.DiskEncryption.Mode {

	case models.DiskEncryptionModeTpmv2:

		manifestParams["MODE"] = "tpm"

	case models.DiskEncryptionModeTang:

		tangServers, err := tang.UnmarshalTangServers(c.DiskEncryption.TangServers)
		if err != nil {
			log.WithError(err).Error("failed to unmarshal tang_server from cluster object")
			return err
		}

		manifestParams["MODE"] = "tang"
		manifestParams["TANG_SERVERS"] = tangServers
	}

	switch *c.DiskEncryption.EnableOn {

	case models.DiskEncryptionEnableOnAll:

		manifestParams["ROLE"] = "master"
		if err := m.createDiskEncryptionManifest(ctx, log, c, manifestParams); err != nil {
			return err
		}

		manifestParams["ROLE"] = "worker"
		if err := m.createDiskEncryptionManifest(ctx, log, c, manifestParams); err != nil {
			return err
		}

	case models.DiskEncryptionEnableOnMasters:

		manifestParams["ROLE"] = "master"
		if err := m.createDiskEncryptionManifest(ctx, log, c, manifestParams); err != nil {
			return err
		}

	case models.DiskEncryptionEnableOnWorkers:

		manifestParams["ROLE"] = "worker"
		if err := m.createDiskEncryptionManifest(ctx, log, c, manifestParams); err != nil {
			return err
		}
	}

	return nil
}

func (m *ManifestsGenerator) createManifests(ctx context.Context, cluster *common.Cluster, filename string, content []byte) error {
	// all relevant logs of creating manifest will be inside CreateClusterManifest
	_, err := m.manifestsApi.CreateClusterManifestInternal(ctx, operations.V2CreateClusterManifestParams{
		ClusterID: *cluster.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(content)),
			FileName: &filename,
			Folder:   swag.String(models.ManifestFolderOpenshift),
		},
	})

	if err != nil {
		return errors.Wrapf(err, "Failed to create manifest %s", filename)
	}

	return nil
}

func (m *ManifestsGenerator) IsSNODNSMasqEnabled() bool {
	return m.Config.EnableSingleNodeDnsmasq
}

func (m *ManifestsGenerator) AddDnsmasqForSingleNode(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error {
	if !m.IsSNODNSMasqEnabled() {
		return nil
	}

	filename := "dnsmasq-bootstrap-in-place.yaml"

	content, err := createDnsmasqForSingleNode(log, cluster)
	if err != nil {
		log.WithError(err).Errorf("Failed to create dnsmasq manifest")
		return err
	}

	return m.createManifests(ctx, cluster, filename, content)
}

func createDnsmasqForSingleNode(log logrus.FieldLogger, cluster *common.Cluster) ([]byte, error) {
	hostIp, err := GetIpForSingleNodeInstallation(cluster, log)
	if err != nil {
		return nil, err
	}

	var manifestParams = map[string]interface{}{
		"CLUSTER_NAME": cluster.Cluster.Name,
		"DNS_DOMAIN":   cluster.Cluster.BaseDNSDomain,
		"HOST_IP":      hostIp,
	}

	log.Infof("Creating dnsmasq manifest with values: cluster name: %q, domain - %q, host ip - %q",
		cluster.Cluster.Name, cluster.Cluster.BaseDNSDomain, hostIp)

	content, err := fillTemplate(manifestParams, snoDnsmasqConf, log)
	if err != nil {
		return nil, err
	}
	dnsmasqContent := base64.StdEncoding.EncodeToString(content)

	content, err = fillTemplate(manifestParams, forceDnsDispatcherScript, log)
	if err != nil {
		return nil, err
	}
	forceDnsDispatcherScriptContent := base64.StdEncoding.EncodeToString(content)

	manifestParams = map[string]interface{}{
		"DNSMASQ_CONTENT":       dnsmasqContent,
		"FORCE_DNS_SCRIPT":      forceDnsDispatcherScriptContent,
		"UNMANAGED_RESOLV_CONF": base64.StdEncoding.EncodeToString([]byte(unmanagedResolvConf)),
	}

	content, err = fillTemplate(manifestParams, dnsMachineConfigManifest, log)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func fillTemplate(manifestParams map[string]interface{}, templateData string, log logrus.FieldLogger) ([]byte, error) {
	tmpl, err := template.New("template").Parse(templateData)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create template")
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, manifestParams); err != nil {
		log.WithError(err).Errorf("Failed to set manifest params %v to template", manifestParams)
		return nil, err
	}
	return buf.Bytes(), nil
}

const (
	redirectTelemeterStageManifest = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-monitoring-config
  namespace: openshift-monitoring
data:
  config.yaml: |
    telemeterClient:
      telemeterServerURL: {{.TELEMETER_SERVER_URL}}
`

	stageServiceBaseURL       = "https://api.stage.openshift.com"
	integrationServiceBaseURL = "https://api.integration.openshift.com"
	stageTelemeterURL         = "https://infogw.api.stage.openshift.com"
	dummyURL                  = "https://dummy.invalid"
)

// Default Telemeter server is prod.
// In case the cluster is created in stage env we need to redirct to Telemter-stage
// Note: There is no Telemeter-integraion so in this and any other cases we will redirect the metrics to a dummy URL
func (m *ManifestsGenerator) AddTelemeterManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {

	manifestParams := map[string]interface{}{}

	switch m.Config.ServiceBaseURL {

	case stageServiceBaseURL:

		log.Infof("Creating manifest to redirect metrics from installed cluster to telemeter-stage")
		manifestParams["TELEMETER_SERVER_URL"] = stageTelemeterURL

	case integrationServiceBaseURL:

		log.Infof("Creating manifest to redirect metrics from installed cluster to dummy URL")
		manifestParams["TELEMETER_SERVER_URL"] = dummyURL

	default:
		return nil

	}

	content, err := fillTemplate(manifestParams, redirectTelemeterStageManifest, log)
	if err != nil {
		log.WithError(err).Error("Failed to parse metrics redirection's template")
		return err
	}

	if err := m.createManifests(ctx, c, "redirect-telemeter.yaml", content); err != nil {

		log.WithError(err).Error("Failed to create manifest to redirect metrics from installed cluster")
		return err
	}

	return nil
}

// NewConfig returns network config if env vars can be parsed
func NewConfig() (*Config, error) {
	networkCfg := Config{}
	if err := envconfig.Process("", &networkCfg); err != nil {
		return &networkCfg, errors.Wrapf(err, "failed to process env var to build network config")
	}
	return &networkCfg, nil
}

const nodeIpHint = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: {{.ROLE}}
  name: 10-{{.ROLE}}s-node-ip-hint
spec:
  config:
    ignition:
      version: 3.1.0
    storage:
      files:
        - contents:
            source: data:text/plain;charset=utf-8;base64,{{.NODE_IP_CONTENT}}
            verification: {}
          filesystem: root
          mode: 420
          path: /etc/default/nodeip-configuration
`

// Add node ip hint (is supported from 4.10 but it makes no harm to push this file to any version)
// it will allow us to tell to node-ip script which ip kubelet should run with
// https://github.com/openshift/machine-config-operator/commit/a0c9a3caa54018eb89eb5bdd6ec1b8fbf97f6fb7
func (m *ManifestsGenerator) AddNodeIpHint(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error {
	if !common.IsSingleNodeCluster(cluster) {
		return nil
	}

	if hintSupported, err := common.VersionGreaterOrEqual(cluster.OpenshiftVersion, "4.10.15"); err != nil || !hintSupported {
		return err
	}

	if !IsMachineCidrAvailable(cluster) {
		return fmt.Errorf("node-ip-hint allowed only if machine network was configured")
	}

	log.Infof("Adding node add ip hint manifests")
	for _, role := range []models.HostRole{models.HostRoleMaster, models.HostRoleWorker} {
		filename := fmt.Sprintf("node-ip-hint-%s.yaml", role)
		content, err := createNodeIpHintContent(log, cluster, string(role))
		if err != nil {
			log.WithError(err).Errorf("Failed to create node ip hint manifest")
			return err
		}

		if err := m.createManifests(ctx, cluster, filename, content); err != nil {
			return err
		}
	}
	return nil
}

func createNodeIpHintContent(log logrus.FieldLogger, cluster *common.Cluster, role string) ([]byte, error) {
	log.Infof("Creating content for node-ip-hint manifest")
	machineCidr := cluster.MachineNetworks[0]
	ip, _, err := net.ParseCIDR(string(machineCidr.Cidr))
	if err != nil {
		log.WithError(err).Warn("Failed to parse machine cidr for node ip hint content")
		return nil, err
	}

	content := fmt.Sprintf("KUBELET_NODEIP_HINT=%s", ip)

	var manifestParams = map[string]interface{}{
		"NODE_IP_CONTENT": base64.StdEncoding.EncodeToString([]byte(content)),
		"ROLE":            role,
	}

	return fillTemplate(manifestParams, nodeIpHint, log)
}
