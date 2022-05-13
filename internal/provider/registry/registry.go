package registry

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/internal/provider/baremetal"
	"github.com/openshift/assisted-service/internal/provider/ovirt"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	SchedulableMastersManifestFileName = "50-schedulable_masters.yaml"
	SchedulableMastersManifestTemplate = `apiVersion: config.openshift.io/v1
kind: Scheduler
metadata:
  name: cluster
spec:
  mastersSchedulable: {{.SCHEDULABLE_MASTERS}}
  policy:
    name: ""
status: {}
`
)

// ErrNoSuchProvider is returned or thrown in panic when the specified provider is not registered.
var ErrNoSuchProvider = errors.New("no provider with the specified name registered")

// ErrProviderUnSet is returned or thrown in panic when provider has not been set.
var ErrProviderUnSet = errors.New("provider has not been set")

//go:generate mockgen --build_flags=--mod=mod -package registry -destination mock_providerregistry.go . ProviderRegistry
type ProviderRegistry interface {
	Registry
	// GetSupportedProvidersByHosts returns a slice of all the providers names which support
	// installation with the given hosts
	GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error)
	// AddPlatformToInstallConfig adds the provider platform to the installconfig platform field,
	// sets platform fields from values within the cluster model.
	AddPlatformToInstallConfig(p models.PlatformType, cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error
	// SetPlatformValuesInDBUpdates updates the `updates` data structure with platform specific values
	SetPlatformValuesInDBUpdates(p models.PlatformType, platformParams *models.Platform, updates map[string]interface{}) error
	// SetPlatformUsages uses the usageApi to update platform specific usages
	SetPlatformUsages(p models.PlatformType, platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error
	// IsHostSupported checks if the provider supports the host
	IsHostSupported(p models.PlatformType, host *models.Host) (bool, error)
	// AreHostsSupported checks if the provider supports the hosts
	AreHostsSupported(p models.PlatformType, hosts []*models.Host) (bool, error)
	// PreCreateManifestsHook allows the provider to perform additional tasks required before the cluster manifests are created
	PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
	// PostCreateManifestsHook allows the provider to perform additional tasks required after the cluster manifests are created
	PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error
	// InitProviders populates the registry with the implemented providers
	InitProviders(log logrus.FieldLogger)
	// GetActualSchedulableMasters allows the provider to set the default scheduling of workloads on masters
	GetActualSchedulableMasters(cluster *common.Cluster) (bool, error)
	// GenerateProviderManifests allows the registry to add custom manifests
	GenerateProviderManifests(ctx context.Context, cluster *common.Cluster) error
}

//go:generate mockgen --build_flags=--mod=mod -package registry -destination mock_registry.go . Registry
// Registry registers the providers to their names.
type Registry interface {
	// Register registers a provider.
	Register(provider provider.Provider)
	// Get returns a provider registered to a name.
	// if provider is not registered returns an ErrNoSuchProvider
	Get(name string) (provider.Provider, error)
}

type registry struct {
	providers    map[string]provider.Provider
	manifestsAPI manifestsapi.ManifestsAPI
}

// NewProviderRegistry creates a new copy of a Registry.
func NewProviderRegistry(manifestsAPI manifestsapi.ManifestsAPI) ProviderRegistry {
	return &registry{
		providers:    map[string]provider.Provider{},
		manifestsAPI: manifestsAPI,
	}
}

func (r *registry) Register(provider provider.Provider) {
	r.providers[string(provider.Name())] = provider
}

func (r *registry) Get(name string) (provider.Provider, error) {
	if p, ok := r.providers[name]; ok {
		return p, nil
	}
	return nil, ErrNoSuchProvider
}

// Name returns the name of the provider
func (r *registry) Name() models.PlatformType {
	return ""
}

func (r *registry) InitProviders(log logrus.FieldLogger) {
	r.Register(ovirt.NewOvirtProvider(log))
	r.Register(vsphere.NewVsphereProvider(log))
	r.Register(baremetal.NewBaremetalProvider(log))
}

func (r *registry) AddPlatformToInstallConfig(p models.PlatformType, cfg *installcfg.InstallerConfigBaremetal, cluster *common.Cluster) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.AddPlatformToInstallConfig(cfg, cluster)
}

func (r *registry) SetPlatformValuesInDBUpdates(
	p models.PlatformType, platformParams *models.Platform, updates map[string]interface{}) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	for _, provider := range r.providers {
		if err = provider.CleanPlatformValuesFromDBUpdates(updates); err != nil {
			return fmt.Errorf("error while removing platform %v values from database: %w", provider.Name(), err)
		}
	}
	updates[provider.DbFieldPlatformType] = platformParams.Type
	return currentProvider.SetPlatformValuesInDBUpdates(platformParams, updates)
}

func (r *registry) SetPlatformUsages(
	p models.PlatformType, platformParams *models.Platform, usages map[string]models.Usage, usageApi usage.API) error {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return fmt.Errorf("error adding platform to install config, platform provider wasn't set: %w", err)
	}
	return currentProvider.SetPlatformUsages(platformParams, usages, usageApi)
}

func (r *registry) IsHostSupported(p models.PlatformType, host *models.Host) (bool, error) {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return false, fmt.Errorf("error while checking if hosts are supported by platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.IsHostSupported(host)
}

func (r *registry) AreHostsSupported(p models.PlatformType, hosts []*models.Host) (bool, error) {
	currentProvider, err := r.Get(string(p))
	if err != nil {
		return false, fmt.Errorf("error while checking if hosts are supported by platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.AreHostsSupported(hosts)
}

func (r *registry) GetSupportedProvidersByHosts(hosts []*models.Host) ([]models.PlatformType, error) {
	var clusterSupportedPlatforms []models.PlatformType
	if len(hosts) == 0 {
		return nil, nil
	}
	for _, p := range r.providers {
		supported, err := p.AreHostsSupported(hosts)
		if err != nil {
			return nil, fmt.Errorf(
				"error while checking if hosts are supported by platform %s, error %w",
				p.Name(), err)
		}
		if supported {
			clusterSupportedPlatforms = append(clusterSupportedPlatforms, p.Name())
		}
	}
	return clusterSupportedPlatforms, nil
}

func (r *registry) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil || cluster.Platform == nil {
		return errors.New("unable to get the platform type")
	}
	currentProvider, err := r.Get(string(common.PlatformTypeValue(cluster.Platform.Type)))
	if err != nil {
		return fmt.Errorf("error while running pre creation manifests hook on platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.PreCreateManifestsHook(cluster, envVars, workDir)
}

func (r *registry) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil || cluster.Platform == nil {
		return errors.New("unable to get the platform type")
	}
	currentProvider, err := r.Get(string(common.PlatformTypeValue(cluster.Platform.Type)))
	if err != nil {
		return fmt.Errorf("error while running post creation manifests hook on platform %s, error %w",
			currentProvider.Name(), err)
	}
	return currentProvider.PostCreateManifestsHook(cluster, envVars, workDir)
}

func (r *registry) GetActualSchedulableMasters(cluster *common.Cluster) (bool, error) {
	if cluster == nil || cluster.Platform == nil {
		return false, errors.New("unable to get the platform type")
	}
	currentProviderStr := string(common.PlatformTypeValue(cluster.Platform.Type))
	currentProvider, err := r.Get(currentProviderStr)
	if err != nil {
		return false, errors.Wrapf(err, "error while getting the actual schedulable masters value on platform %s",
			currentProviderStr)
	}
	return currentProvider.GetActualSchedulableMasters(cluster)
}

func (r *registry) GenerateProviderManifests(ctx context.Context, cluster *common.Cluster) error {
	manifests, err := r.generateProviderManifestsInternal(cluster)
	if err != nil {
		return errors.Wrapf(err, "failed to generate provider manifests")
	}
	for filename, content := range manifests {
		if err = r.createClusterManifests(ctx, cluster, filename, content); err != nil {
			return fmt.Errorf("error while creating the manifest '%s', error %w", filename, err)
		}
	}
	return nil
}

func (r *registry) generateProviderManifestsInternal(cluster *common.Cluster) (map[string][]byte, error) {
	manifests := make(map[string][]byte)
	filename, content, err := r.generateSchedulableMastersManifest(cluster)
	if err != nil {
		return nil, err
	}
	if filename == "" {
		return nil, errors.New("template filename cannot be empty")
	}
	manifests[filename] = content
	return manifests, nil
}

func (r *registry) generateSchedulableMastersManifest(cluster *common.Cluster) (string, []byte, error) {
	schedulableMasters, err := r.GetActualSchedulableMasters(cluster)
	if err != nil {
		return "", nil, err
	}
	templateParams := map[string]interface{}{
		"SCHEDULABLE_MASTERS": schedulableMasters,
	}
	content, err := fillTemplate(templateParams, SchedulableMastersManifestTemplate, nil)
	if err != nil {
		return "", nil, err
	}
	return SchedulableMastersManifestFileName, content, nil
}

func (r *registry) createClusterManifests(ctx context.Context, cluster *common.Cluster, filename string, content []byte) error {
	// all relevant logs of creating manifest will be inside CreateClusterManifest
	_, err := r.manifestsAPI.CreateClusterManifestInternal(ctx, operations.V2CreateClusterManifestParams{
		ClusterID: *cluster.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(content)),
			FileName: &filename,
			Folder:   swag.String(models.ManifestFolderOpenshift),
		},
	})

	if err != nil {
		return errors.Wrapf(err, "failed to create manifest %s", filename)
	}

	return nil
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
