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
	AddIpv6Manifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error
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

const ipv6MachineConfigManifestFormat = `
apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: %s
  name: %s-ipv6-net-config
spec:
  config:
    ignition:
      version: 3.1.0
    storage:
      files:
        - contents:
            source: data:text/plain;charset=utf-8;base64,%s
          mode: 644 
          overwrite: true
          path: /etc/NetworkManager/conf.d/01-ipv6.conf`

const Ipv6ManifestName = "%s-ipv6-configuration.yaml"

func createIpv6MachineConfig(role string) string {
	return fmt.Sprintf(ipv6MachineConfigManifestFormat, role, role, base64.StdEncoding.EncodeToString([]byte(common.Ipv6DuidMcoConf)))
}

func (m *ManifestsGenerator) addIpv6ManifestForRole(ctx context.Context, log logrus.FieldLogger, c *common.Cluster, role string) error {
	folder := models.ManifestFolderOpenshift
	base64Content := base64.StdEncoding.EncodeToString([]byte(createIpv6MachineConfig(role)))

	response := m.manifestsApi.CreateClusterManifest(ctx, operations.CreateClusterManifestParams{
		ClusterID: *c.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  &base64Content,
			FileName: swag.String(fmt.Sprintf(Ipv6ManifestName, role)),
			Folder:   &folder,
		},
	})

	if _, ok := response.(*operations.CreateClusterManifestCreated); !ok {
		if apiErr, ok := response.(*common.ApiErrorResponse); ok {
			return errors.Wrapf(apiErr, "Failed to create manifest %s", Ipv6ManifestName)
		}

		return errors.Errorf("Failed to create manifest %s", Ipv6ManifestName)
	}
	return nil
}

func (m *ManifestsGenerator) AddIpv6Manifest(ctx context.Context, log logrus.FieldLogger, c *common.Cluster) error {
	for _, role := range []string{"master", "worker"} {
		if err := m.addIpv6ManifestForRole(ctx, log, c, role); err != nil {
			return err
		}
	}
	return nil
}
