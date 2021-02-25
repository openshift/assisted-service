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
}

type ManifestsGenerator struct {
	manifestsApi restapi.ManifestsAPI
}

func NewManifestsGenerator(manifestsApi restapi.ManifestsAPI) *ManifestsGenerator {
	return &ManifestsGenerator{
		manifestsApi: manifestsApi,
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
  name: {{.ROLE}}s-chrony-configuration
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
address=/{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}/{{.HOST_IP}}
addn-hosts=/etc/api-int.host
`

const apiIntHosts = `
{{.HOST_IP}} api-int api-int.{{.CLUSTER_NAME}}.{{.DNS_DOMAIN}}
`

const forceDnsDispatcherScript = `
export IP="{{.HOST_IP}}"
if [ "$2" = "dhcp4-change" ] || [ "$2" = "dhcp6-change" ] || [ "$2" = "up" ] || [ "$2" = "connectivity-change" ]; then
    grep -q $IP /etc/resolv.conf
    if [ "$?" != "0" ] ; then
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
    machineconfiguration.openshift.io/role: {{.ROLE}}
  name: 99-{{.ROLE}}s-dnsmasq-configuration
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
          source: data:text/plain;charset=utf-8;base64,{{.DNSMASQ_CONF}}
          verification: {}
        filesystem: root
        mode: 420
        path: /etc/dnsmasq.d/sno.conf
      - contents:
          source: data:text/plain;charset=utf-8;base64,{{.API_INT_HOST_FILE}}
          verification: {}
        filesystem: root
        mode: 420
        path: /etc/api-int.host
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
         Description=Run dnsmasq to provide local dns
         Before=kubelet.service crio.service
         After=network.target
         
         [Service]
         ExecStart=/usr/sbin/dnsmasq -k
         
         [Install]
         WantedBy=multi-user.target
`

func createChronyManifestContent(c *common.Cluster, role models.HostRole) (string, error) {
	sources := make([]string, 0)

	for _, host := range c.Hosts {
		if swag.StringValue(host.Status) == models.HostStatusDisabled || host.NtpSources == "" {
			continue
		}

		var ntpSources []*models.NtpSource
		if err := json.Unmarshal([]byte(host.NtpSources), &ntpSources); err != nil {
			return "", errors.Wrapf(err, "Failed to unmarshal %s", host.NtpSources)
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

	tmpl, err := template.New("chronyManifest").Parse(ntpMachineConfigManifest)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to create template")
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, manifestParams); err != nil {
		return "", errors.Wrapf(err, "Failed to set manifest params %v to template", manifestParams)
	}
	return buf.String(), nil
}

func (m *ManifestsGenerator) AddChronyManifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {
	for _, role := range []models.HostRole{models.HostRoleMaster, models.HostRoleWorker} {
		content, err := createChronyManifestContent(c, role)

		if err != nil {
			return errors.Wrapf(err, "Failed to create chrony manifest content for role %s cluster id %s", role, *c.ID)
		}

		chronyManifestFileName := fmt.Sprintf("%ss-chrony-configuration.yaml", string(role))
		folder := models.ManifestFolderOpenshift
		base64Content := base64.StdEncoding.EncodeToString([]byte(content))

		response := m.manifestsApi.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
			ClusterID: *c.ID,
			CreateManifestParams: &models.CreateManifestParams{
				Content:  &base64Content,
				FileName: &chronyManifestFileName,
				Folder:   &folder,
			},
		})

		if _, ok := response.(*operations.CreateClusterManifestCreated); !ok {
			if apiErr, ok := response.(*common.ApiErrorResponse); ok {
				return errors.Wrapf(apiErr, "Failed to create manifest %s", chronyManifestFileName)
			}

			return errors.Errorf("Failed to create manifest %s", chronyManifestFileName)
		}
	}

	return nil
}

func fillTemplate(manifestParams map[string]string, templateData string) ([]byte, error) {
	tmpl, err := template.New("template").Parse(templateData)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create template")
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, manifestParams); err != nil {
		return nil, errors.Wrapf(err, "Failed to set manifest params %v to template", manifestParams)
	}
	return buf.Bytes(), nil
}

func (m *ManifestsGenerator) AddDnsmasqForSingleNode(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {
	apiVip, _ := GetMachineCIDRIP(c.Hosts[0], c)
	if apiVip == "" {
		return errors.Errorf("failed to get ip for bootstrap in place dnsmasq manifest")
	}

	var manifestParams = map[string]string{
		"CLUSTER_NAME": c.Cluster.Name,
		"DNS_DOMAIN":   c.Cluster.BaseDNSDomain,
		"HOST_IP":      apiVip,
	}

	conf, _ := fillTemplate(manifestParams, snoDnsmasqConf)
	dnsmasqConf := base64.StdEncoding.EncodeToString(conf)

	conf, _ = fillTemplate(manifestParams, apiIntHosts)
	apiIntHostsContent := base64.StdEncoding.EncodeToString(conf)

	conf, _ = fillTemplate(manifestParams, forceDnsDispatcherScript)
	forceDnsDispatcherScriptContent := base64.StdEncoding.EncodeToString(conf)

	manifestParams = map[string]string{
		"DNSMASQ_CONF":      dnsmasqConf,
		"API_INT_HOST_FILE": apiIntHostsContent,
		"FORCE_DNS_SCRIPT":  forceDnsDispatcherScriptContent,
		"ROLE":              "master",
	}

	conf, _ = fillTemplate(manifestParams, dnsMachineConfigManifest)
	filename := "dnsmasq-bootstrap-in-place.yaml"

	response := m.manifestsApi.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
		ClusterID: *c.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(conf)),
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
