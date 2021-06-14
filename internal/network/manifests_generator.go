package network

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

//go:generate mockgen -source=manifests_generator.go -package=network -destination=mock_manifests_generator.go

type ManifestsGeneratorAPI interface {
	AddChronyManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddDnsmasqForSingleNode(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddDisableVmwareTunnelOffloading(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
	AddTelemeterManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
}

type Config struct {
	ServiceBaseURL string `envconfig:"SERVICE_BASE_URL"`
}

type ManifestsGenerator struct {
	manifestsApi restapi.ManifestsAPI
	Config       Config
}

func NewManifestsGenerator(manifestsApi restapi.ManifestsAPI, config Config) *ManifestsGenerator {
	return &ManifestsGenerator{
		manifestsApi: manifestsApi,
		Config:       config,
	}
}

const defaultChronyConf = `
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
      config: {}
      security:
        tls: {}
      timeouts: {}
      version: 2.2.0
    networkd: {}
    passwd: {}
    storage:
      files:
      - contents:
          source: data:text/plain;charset=utf-8;base64,{{.CHRONY_CONTENT}}
          verification: {}
        filesystem: root
        mode: 420
        path: /etc/chrony.conf
  osImageURL: ""
`

const snoDnsmasqConf = `
address=/apps.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
address=/api-int.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
`

const forceDnsDispatcherScript = `
export IP="{{.HOST_IP}}"
if [ "$2" = "dhcp4-change" ] || [ "$2" = "dhcp6-change" ] || [ "$2" = "up" ] || [ "$2" = "connectivity-change" ]; then
    if ! grep -q "$IP" /etc/resolv.conf; then
      sed -i "s/{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}//" /etc/resolv.conf
      sed -i "s/search /search {{.CLUSTER_NAME}}.{{.DNS_DOMAIN}} /" /etc/resolv.conf
      sed -i "0,/nameserver/s/nameserver/nameserver $IP\nnameserver/" /etc/resolv.conf
    fi
fi
`

const dnsMachineConfigManifest = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: 99-master-dnsmasq-configuration
spec:
  config:
    ignition:
      config: {}
      security:
        tls: {}
      timeouts: {}
      version: 2.2.0
    networkd: {}
    passwd: {}
    storage:
      files:
      - contents:
          source: data:text/plain;charset=utf-8;base64,{{.DNSMASQ_CONTENT}}
          verification: {}
        filesystem: root
        mode: 420
        path: /etc/dnsmasq.d/single-node.conf
      - contents:
          source: data:text/plain;charset=utf-8;base64,{{.FORCE_DNS_SCRIPT}}
          verification: {}
        filesystem: root
        mode: 365
        path: /etc/NetworkManager/dispatcher.d/forcedns
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

func createChronyManifestContent(c *common.Cluster, role models.HostRole, log logrus.FieldLogger) ([]byte, error) {
	sources := make([]string, 0)

	for _, host := range c.Hosts {
		if swag.StringValue(host.Status) == models.HostStatusDisabled || host.NtpSources == "" {
			continue
		}

		var ntpSources []*models.NtpSource
		if err := json.Unmarshal([]byte(host.NtpSources), &ntpSources); err != nil {
			log.Errorln("sss", "sss", "ssss")
			return nil, errors.Wrapf(err, "Failed to unmarshal %s", host.NtpSources)
		}

		for _, source := range ntpSources {
			if source.SourceState == models.SourceStateSynced {
				if !funk.Contains(sources, source.SourceName) {
					sources = append(sources, source.SourceName)
				}

				break
			}
		}
	}

	content := defaultChronyConf[:]

	for _, source := range sources {
		content += fmt.Sprintf("\nserver %s iburst", source)
	}

	var manifestParams = map[string]string{
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

func (m *ManifestsGenerator) createManifests(ctx context.Context, cluster *common.Cluster, filename string, content []byte) error {
	// all relevant logs of creating manifest will be inside CreateClusterManifest
	response := m.manifestsApi.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
		ClusterID: *cluster.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(content)),
			FileName: &filename,
			Folder:   swag.String(models.ManifestFolderOpenshift),
		},
	})

	if _, ok := response.(*operations.CreateClusterManifestCreated); !ok {
		if apiErr, ok := response.(*common.ApiErrorResponse); ok {
			return errors.Wrapf(apiErr, "Failed to create manifest %s", filename)
		}
		return errors.Errorf("Failed to create manifest %s", filename)
	}
	return nil
}

func (m *ManifestsGenerator) AddDnsmasqForSingleNode(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) error {
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

	var manifestParams = map[string]string{
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

	manifestParams = map[string]string{
		"DNSMASQ_CONTENT":  dnsmasqContent,
		"FORCE_DNS_SCRIPT": forceDnsDispatcherScriptContent,
	}

	content, err = fillTemplate(manifestParams, dnsMachineConfigManifest, log)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func fillTemplate(manifestParams map[string]string, templateData string, log logrus.FieldLogger) ([]byte, error) {
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

const disableTunnelOffloadManifest = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: %s
  name: 50-%ss-disable-tunnel-offload
spec:
  config:
    ignition:
      config: {}
      security:
        tls: {}
      timeouts: {}
      version: 2.2.0
    networkd: {}
    passwd: {}
    storage:
      files:
      - contents:
          source: data:text/plain;charset=utf-8;base64,%s
          verification: {}
        filesystem: root
        mode: 493
        path: /etc/NetworkManager/dispatcher.d/05-disable-tunnel-offload
  osImageURL: ""
`

const disableTunnelOffloadScript = `#! /bin/bash

if [ "$2" != "up" ] ; then
	exit 0
fi

driver=$(ethtool -i "$1" | awk '/driver:/{print $2;}')

if [ "$driver" != "vmxnet3" ] ; then 
	exit 0
fi

current=$(ethtool -k "$1" | grep udp_tnl | grep -v '\[fixed\]')

if [ -z "$current" ] ; then
	exit 0
fi

nmcli connection modify $CONNECTION_UUID ethtool.feature-tx-udp_tnl-csum-segmentation off ethtool.feature-tx-udp_tnl-segmentation off
nmcli connection up $CONNECTION_UUID
`

func createDisableTunnelOffloadingContext(role string) string {
	return fmt.Sprintf(disableTunnelOffloadManifest, role, role, base64.StdEncoding.EncodeToString([]byte(disableTunnelOffloadScript)))
}

func (m *ManifestsGenerator) AddDisableVmwareTunnelOffloading(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {
	for _, role := range []string{"master", "worker"} {
		fname := fmt.Sprintf("50-%ss-disable-tunnel-offload.yaml", role)
		if err := m.createManifests(ctx, c, fname, []byte(createDisableTunnelOffloadingContext(role))); err != nil {
			log.WithError(err).Errorf("Failed to create disable tunnel offloading manifest for role %s", role)
			return err
		}
	}
	return nil
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

	manifestParams := map[string]string{}

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
