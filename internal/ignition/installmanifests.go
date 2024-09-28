package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	config_latest_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/system"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	masterIgn      = "master.ign"
	workerIgn      = "worker.ign"
	nodeIpHintFile = "/etc/default/nodeip-configuration"
)

const highlyAvailableInfrastructureTopologyPatch = `---
- op: replace
  path: /status/infrastructureTopology
  value: HighlyAvailable
`

type clusterVersion struct {
	APIVersion string `yaml:"apiVersion"`
	Metadata   struct {
		Namespace string `yaml:"namespace"`
		Name      string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Upstream  string `yaml:"upstream"`
		Channel   string `yaml:"channel"`
		ClusterID string `yaml:"clusterID"`
	} `yaml:"spec"`
}

// Generator can generate ignition files and upload them to an S3-like service
type Generator interface {
	Generate(ctx context.Context, installConfig []byte) error
	UploadToS3(ctx context.Context) error
}

type installerGenerator struct {
	log                           logrus.FieldLogger
	workDir                       string
	cluster                       *common.Cluster
	releaseImage                  string
	releaseImageMirror            string
	installerDir                  string
	serviceCACert                 string
	encodedDhcpFileContents       string
	s3Client                      s3wrapper.API
	enableMetal3Provisioning      bool
	installInvoker                string
	providerRegistry              registry.ProviderRegistry
	installerReleaseImageOverride string
	clusterTLSCertOverrideDir     string
	installerCache                *installercache.Installers
	nodeIpAllocations             map[strfmt.UUID]*network.NodeIpAllocation
}

var fileNames = [...]string{
	"bootstrap.ign",
	masterIgn,
	"metadata.json",
	workerIgn,
	"kubeconfig-noingress",
	"kubeadmin-password",
	"install-config.yaml",
}

// NewGenerator returns a generator that can generate ignition files
func NewGenerator(workDir string, installerDir string, cluster *common.Cluster, releaseImage string, releaseImageMirror string,
	serviceCACert string, installInvoker string, s3Client s3wrapper.API, log logrus.FieldLogger, providerRegistry registry.ProviderRegistry,
	installerReleaseImageOverride, clusterTLSCertOverrideDir string, storageCapacityLimit int64) Generator {
	return &installerGenerator{
		cluster:                       cluster,
		log:                           log,
		releaseImage:                  releaseImage,
		releaseImageMirror:            releaseImageMirror,
		workDir:                       workDir,
		installerDir:                  installerDir,
		serviceCACert:                 serviceCACert,
		s3Client:                      s3Client,
		enableMetal3Provisioning:      true,
		installInvoker:                installInvoker,
		providerRegistry:              providerRegistry,
		installerReleaseImageOverride: installerReleaseImageOverride,
		clusterTLSCertOverrideDir:     clusterTLSCertOverrideDir,
		installerCache:                installercache.New(installerDir, storageCapacityLimit, log),
	}
}

// UploadToS3 uploads generated ignition and related files to the configured
// S3-compatible storage
func (g *installerGenerator) UploadToS3(ctx context.Context) error {
	return uploadToS3(ctx, g.workDir, g.cluster, g.s3Client, g.log)
}

func (g *installerGenerator) allocateNodeIpsIfNeeded(log logrus.FieldLogger) {
	if common.IsMultiNodeNonePlatformCluster(g.cluster) {
		nodeIpAllocations, err := network.GenerateNonePlatformAddressAllocation(g.cluster, log)
		if err != nil {
			log.WithError(err).Warnf("failed to generate ip address allocation for cluster %s", *g.cluster.ID)
		} else {
			g.nodeIpAllocations = nodeIpAllocations
		}
	}
}

// Generate generates ignition files and applies modifications.
func (g *installerGenerator) Generate(ctx context.Context, installConfig []byte) error {
	var err error
	log := logutil.FromContext(ctx, g.log)

	defer func() {
		if err != nil {
			os.Remove(filepath.Join(g.workDir, "manifests"))
			os.Remove(filepath.Join(g.workDir, "openshift"))
		}
	}()

	// In case we don't want to override image for extracting installer use release one
	if g.installerReleaseImageOverride == "" {
		g.installerReleaseImageOverride = g.releaseImage
	}

	mirrorRegistriesBuilder := mirrorregistries.New()
	ocRelease := oc.NewRelease(
		&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay},
		mirrorRegistriesBuilder,
		system.NewLocalSystemInfo(),
	)

	release, err := g.installerCache.Get(g.installerReleaseImageOverride, g.releaseImageMirror,
		g.cluster.PullSecret, ocRelease, g.cluster.OpenshiftVersion)
	if err != nil {
		return errors.Wrap(err, "failed to get installer path")
	}
	//cleanup resources at the end
	defer release.Release()

	installerPath := release.Path
	installConfigPath := filepath.Join(g.workDir, "install-config.yaml")

	g.enableMetal3Provisioning, err = common.VersionGreaterOrEqual(g.cluster.Cluster.OpenshiftVersion, "4.7")
	if err != nil {
		return err
	}

	g.encodedDhcpFileContents, err = network.GetEncodedDhcpParamFileContents(g.cluster)
	if err != nil {
		wrapped := errors.Wrapf(err, "Could not create DHCP encoded file")
		log.WithError(wrapped).Errorf("GenerateInstallConfig")
		return wrapped
	}

	g.allocateNodeIpsIfNeeded(log)

	envVars := append(os.Environ(),
		"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE="+g.releaseImage,
		"OPENSHIFT_INSTALL_INVOKER="+g.installInvoker,
	)
	if g.clusterTLSCertOverrideDir != "" {
		envVars = append(envVars, "OPENSHIFT_INSTALL_LOAD_CLUSTER_CERTS=true")
	}

	if envVars, err = g.addBootstrapKubeletIpIfRequired(log, envVars); err != nil {
		return err
	}

	// write installConfig to install-config.yaml so openshift-install can read it
	err = os.WriteFile(installConfigPath, installConfig, 0600)
	if err != nil {
		log.Errorf("failed to write file %s", installConfigPath)
		return err
	}

	manifestFiles, err := manifests.GetClusterManifests(ctx, g.cluster.ID, g.s3Client)
	if err != nil {
		log.WithError(err).Errorf("failed to check if cluster %s has manifests", g.cluster.ID)
		return err
	}

	err = g.providerRegistry.PreCreateManifestsHook(g.cluster, &envVars, g.workDir)

	if err != nil {
		log.WithError(err).Errorf("failed to run pre manifests creation hook '%s'", common.PlatformTypeValue(g.cluster.Platform.Type))
		return err
	}

	err = g.importClusterTLSCerts(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to import cluster TLS certs")
		return err
	}

	err = g.runCreateCommand(ctx, installerPath, "manifests", envVars)
	if err != nil {
		return err
	}
	err = g.providerRegistry.PostCreateManifestsHook(g.cluster, &envVars, g.workDir)
	if err != nil {
		log.WithError(err).Errorf("failed to run post manifests creation hook '%s'", common.PlatformTypeValue(g.cluster.Platform.Type))
		return err
	}

	// download manifests files to working directory
	for _, manifest := range manifestFiles {
		log.Infof("adding manifest %s to working dir for cluster %s", manifest, g.cluster.ID)
		err = g.downloadManifest(ctx, manifest)
		if err != nil {
			log.WithError(err).Errorf("Failed to download manifest %s to working dir for cluster %s", manifest, g.cluster.ID)
			return err
		}
	}

	err = g.expandUserMultiDocYamls(ctx)
	if err != nil {
		log.WithError(err).Errorf("failed expand multi-document yaml for cluster '%s'", g.cluster.ID)
		return err
	}

	err = g.applyManifestPatches(ctx)
	if err != nil {
		log.WithError(err).Errorf("failed to apply manifests' patches for cluster '%s'", g.cluster.ID)
		return err
	}

	err = g.applyInfrastructureCRPatch(ctx)
	if err != nil {
		log.WithError(err).Errorf("failed to patch the infrastructure CR manifest '%s'", common.PlatformTypeValue(g.cluster.Platform.Type))
		return err
	}

	if swag.StringValue(g.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		err = g.bootstrapInPlaceIgnitionsCreate(ctx, installerPath, envVars)
	} else {
		err = g.runCreateCommand(ctx, installerPath, "ignition-configs", envVars)
	}
	if err != nil {
		log.Error(err)
		return err
	}

	// parse ignition and update BareMetalHosts
	bootstrapPath := filepath.Join(g.workDir, "bootstrap.ign")
	err = g.updateBootstrap(ctx, bootstrapPath)
	if err != nil {
		return err
	}

	err = g.updateIgnitions()
	if err != nil {
		log.Error(err)
		return err
	}

	err = g.createHostIgnitions()
	if err != nil {
		log.Error(err)
		return err
	}

	// move all files into the working directory
	err = os.Rename(filepath.Join(g.workDir, "auth/kubeadmin-password"), filepath.Join(g.workDir, "kubeadmin-password"))
	if err != nil {
		return err
	}
	// after installation completes, a new kubeconfig will be created and made
	// available that includes ingress details, so we rename this one
	err = os.Rename(filepath.Join(g.workDir, "auth/kubeconfig"), filepath.Join(g.workDir, "kubeconfig-noingress"))
	if err != nil {
		return err
	}
	// We want to save install-config.yaml
	// Installer deletes it so we need to write it one more time
	err = os.WriteFile(installConfigPath, installConfig, 0600)
	if err != nil {
		log.Errorf("Failed to write file %s", installConfigPath)
		return err
	}

	err = os.Remove(filepath.Join(g.workDir, "auth"))
	if err != nil {
		return err
	}
	return nil
}

func (g *installerGenerator) addBootstrapKubeletIpIfRequired(log logrus.FieldLogger, envVars []string) ([]string, error) {
	// setting bootstrap kubelet node ip
	log.Debugf("Adding bootstrap ip to env vars")
	if !common.IsMultiNodeNonePlatformCluster(g.cluster) {
		bootstrapIp, err := network.GetPrimaryMachineCIDRIP(common.GetBootstrapHost(g.cluster), g.cluster)
		if err != nil {
			log.WithError(err).Warn("Failed to get bootstrap primary ip for kubelet service update.")
			return envVars, err
		}
		envVars = append(envVars, "OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP="+bootstrapIp)
	} else if g.nodeIpAllocations != nil {
		bootstrapHost := common.GetBootstrapHost(g.cluster)
		if bootstrapHost != nil {
			allocation, ok := g.nodeIpAllocations[lo.FromPtr(bootstrapHost.ID)]
			if ok {
				envVars = append(envVars, "OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP="+allocation.NodeIp)
				g.log.Infof("Set OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP=%s for host %s", allocation.NodeIp, lo.FromPtr(bootstrapHost.ID))

			}
		}
	} else {
		log.Errorf("Unable to set OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP for multinode none platform cluster %s - missing node ip allocations",
			lo.FromPtr(g.cluster.ID))
	}
	return envVars, nil
}

func (g *installerGenerator) applyManifestPatches(ctx context.Context) error {
	log := logutil.FromContext(ctx, g.log)
	manifestsOpenShiftDir := filepath.Join(g.workDir, "openshift")
	manifestsManifestsDir := filepath.Join(g.workDir, "manifests")

	// File path walks the directory in lexical order, which means it's possible to have some control on
	// how files are being walked through by using a numeric prefix for the patch extension. For example:
	// - cluster-scheduler-02-config.yml.patch_01_set_schedulable_masters
	// - cluster-scheduler-02-config.yml.patch_02_something_else
	directories := []string{manifestsOpenShiftDir, manifestsManifestsDir}
	for i := range directories {
		directory := directories[i]
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// We allow files that have the following extension .y(a)ml.patch(_something).
			// This allows to pushing multuple patches for the same Manifest.
			extension := filepath.Ext(info.Name())
			if !strings.HasPrefix(extension, ".patch") {
				return nil
			}

			// This is the path to the patch
			manifestPatchPath := filepath.Join(directory, info.Name())
			log.Debugf("Applying the following patch: %s", manifestPatchPath)
			manifestPatch, err := os.ReadFile(manifestPatchPath)
			if err != nil {
				return errors.Wrapf(err, "failed to read manifest patch %s", manifestPatchPath)
			}
			log.Debugf("read the manifest at %s", manifestPatchPath)

			// Let's look for the actual manifest. Code first looks in the `openshift` directory and
			// fallsback to the `manifests` directory if no patch was found in the former.
			manifestPath := filepath.Join(manifestsOpenShiftDir, strings.TrimSuffix(info.Name(), extension))
			if _, err = os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
				log.Debugf("Manifest %s does not exist. Trying with the openshift dir next")
				manifestPath = filepath.Join(g.workDir, "manifests", strings.TrimSuffix(info.Name(), extension))
			}

			data, err := os.ReadFile(manifestPath)
			if err != nil {
				return errors.Wrapf(err, "failed to read manifest %s", manifestPath)
			}
			log.Debugf("read the manifest at %s", manifestPath)

			// Let's apply the patch now since both files have been read
			data, err = common.ApplyYamlPatch(data, manifestPatch)
			if err != nil {
				return errors.Wrapf(err, "failed to patch manifest \"%s\"", manifestPath)
			}
			log.Debugf("applied the yaml patch to the manifest at %s: \n %s", manifestPath, string(data[:]))

			err = os.WriteFile(manifestPath, data, 0600)
			if err != nil {
				return errors.Wrapf(err, "failed to write manifest \"%s\"", manifestPath)
			}

			log.Debugf("wrote the resulting manifest at %s", manifestPath)

			err = os.Remove(manifestPatchPath)
			if err != nil {
				return errors.Wrapf(err, "failed to remove patch %s", manifestPatchPath)
			}
			return nil
		})
		if err != nil {
			return errors.Wrapf(err, "failed to apply patches")
		}
	}
	return nil
}

func (g *installerGenerator) applyInfrastructureCRPatch(ctx context.Context) error {
	log := logutil.FromContext(ctx, g.log)

	// We are only patching the InfrastructureCR if the hosts count is 4
	// and the three masters are schedulable.
	if len(g.cluster.Hosts) != 4 {
		log.Debugf("number of hosts is different than 4, no need to patch the Infrastructure CR %d", len(g.cluster.Hosts))
		return nil
	}

	log.Infof("Patching Infrastructure CR: Number of hosts: %d", len(g.cluster.Hosts))

	infraManifest := filepath.Join(g.workDir, "manifests", "cluster-infrastructure-02-config.yml")
	data, err := os.ReadFile(infraManifest)
	if err != nil {
		return errors.Wrapf(err, "failed to read Infrastructure Manifest \"%s\"", infraManifest)
	}
	log.Debugf("read the infrastructure manifest at %s", infraManifest)

	data, err = common.ApplyYamlPatch(data, []byte(highlyAvailableInfrastructureTopologyPatch))
	if err != nil {
		return errors.Wrapf(err, "failed to patch Infrastructure Manifest \"%s\"", infraManifest)
	}
	log.Debugf("applied the yaml patch to the infrastructure manifest at %s: \n %s", infraManifest, string(data[:]))

	err = os.WriteFile(infraManifest, data, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to write Infrastructure Manifest \"%s\"", infraManifest)
	}
	log.Debugf("wrote the resulting infrastructure manifest at %s", infraManifest)

	return nil
}

func (g *installerGenerator) importClusterTLSCerts(ctx context.Context) error {
	if g.clusterTLSCertOverrideDir == "" {
		return nil
	}
	log := logutil.FromContext(ctx, g.log).WithField("inputDir", g.clusterTLSCertOverrideDir)
	log.Debug("Checking for cluster TLS certs dir")

	entries, err := os.ReadDir(g.clusterTLSCertOverrideDir)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to read cluster TLS certs dir \"%s\"", g.clusterTLSCertOverrideDir)
	}
	log.Info("Found cluster TLS certs dir")

	outDir := filepath.Join(g.workDir, "tls")
	log = log.WithField("outputDir", outDir).WithField("cluster", g.cluster.ID)
	if err := os.Mkdir(outDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create cluster TLS certs output dir \"%s\"", outDir)
	}
	log.Info("Created cluster TLS certs dir")
	tlsFS := os.DirFS(g.clusterTLSCertOverrideDir)

	copyFile := func(filename string) error {
		log.WithField("filename", filename).Info("Copying cluster TLS cert file")

		f, err := tlsFS.Open(filename)
		if err != nil {
			return errors.Wrapf(err, "failed to open cluster TLS cert file \"%s\"", filename)
		}
		defer f.Close()
		c, err := io.ReadAll(f)
		if err != nil {
			return errors.Wrapf(err, "failed to read cluster TLS cert file \"%s\"", filename)
		}
		err = os.WriteFile(filepath.Join(outDir, filename), c, 0600)
		if err != nil {
			return errors.Wrapf(err, "failed to write cluster TLS cert file \"%s\"", filename)
		}

		return nil
	}

	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if err := copyFile(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (g *installerGenerator) bootstrapInPlaceIgnitionsCreate(ctx context.Context, installerPath string, envVars []string) error {
	err := g.runCreateCommand(ctx, installerPath, "single-node-ignition-config", envVars)
	if err != nil {
		return errors.Wrapf(err, "Failed to create single node ignitions")
	}

	bootstrapPath := filepath.Join(g.workDir, "bootstrap.ign")
	// In case of single node rename bootstrap Ignition file
	err = os.Rename(filepath.Join(g.workDir, "bootstrap-in-place-for-live-iso.ign"), bootstrapPath)
	if err != nil {
		return errors.Wrapf(err, "Failed to rename bootstrap-in-place-for-live-iso.ign")
	}

	bootstrapConfig, err := parseIgnitionFile(bootstrapPath)
	if err != nil {
		return err
	}
	//Although BIP works with 4.8 and above we want to support early 4.8 CI images
	// To that end we set the dummy master ignition version to the same version as the bootstrap ignition
	config := config_latest_types.Config{Ignition: config_latest_types.Ignition{Version: bootstrapConfig.Ignition.Version}}
	for _, file := range []string{masterIgn, workerIgn} {
		err = writeIgnitionFile(filepath.Join(g.workDir, file), &config)
		if err != nil {
			return errors.Wrapf(err, "Failed to create %s", file)
		}
	}

	return nil
}

// expandUserMultiDocYamls finds if user uploaded multi document yaml files and
// split them into several files
func (g *installerGenerator) expandUserMultiDocYamls(ctx context.Context) error {
	log := logutil.FromContext(ctx, g.log)

	metadata, err := manifests.GetManifestMetadata(ctx, g.cluster.ID, g.s3Client)
	if err != nil {
		return errors.Wrapf(err, "Failed to retrieve manifest matadata")
	}
	userManifests, err := manifests.ResolveManifestNamesFromMetadata(
		manifests.FilterMetadataOnManifestSource(metadata, constants.ManifestSourceUserSupplied),
	)
	if err != nil {
		return errors.Wrapf(err, "Failed to resolve manifest names from metadata")
	}

	// pass a random token to expandMultiDocYaml in order to prevent name
	// clashes when spliting one file into several ones
	randomToken := uuid.NewString()[:7]

	for _, manifest := range userManifests {
		log.Debugf("Looking at expanding manifest file %s", manifest)

		extension := filepath.Ext(manifest)
		if !(extension == ".yaml" || extension == ".yml") {
			continue
		}

		manifestPath := filepath.Join(g.workDir, manifest)
		err := g.expandMultiDocYaml(ctx, manifestPath, randomToken)
		if err != nil {
			return err
		}
	}

	return nil
}

// expandMultiDocYaml splits a multi document yaml file into several files
// if the the file given in input contains only one document, the file is left untouched
func (g *installerGenerator) expandMultiDocYaml(ctx context.Context, manifestPath string, uniqueToken string) error {
	var err error

	log := logutil.FromContext(ctx, g.log)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errors.Wrapf(err, "Failed to read %s", manifestPath)
	}

	// read each yaml document contained in the file into a slice
	manifestContentList := [][]byte{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc interface{}
		err = dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Wrapf(err, "Failed to parse yaml document %s", manifestPath)
		}

		// skip empty documents
		if doc == nil {
			continue
		}

		manifestContent, marshalError := yaml.Marshal(doc)
		if marshalError != nil {
			return errors.Wrapf(err, "Failed to re-encode yaml file %s", manifestPath)
		}
		manifestContentList = append(manifestContentList, manifestContent)
	}

	if len(manifestContentList) <= 1 {
		return nil
	}

	log.Infof("Expanding multi-document yaml file %s into %d files", manifestPath, len(manifestContentList))

	// if the yaml file contains more than one document,
	// split it into several files
	for idx, content := range manifestContentList {
		fileExt := filepath.Ext(manifestPath)
		fileWithoutExt := strings.TrimSuffix(manifestPath, fileExt)
		filename := fmt.Sprintf("%s-%s-%02d%s", fileWithoutExt, uniqueToken, idx, fileExt)

		err = os.WriteFile(filename, content, 0600)
		if err != nil {
			return errors.Wrapf(err, "Failed write %s", filename)
		}

		log.Debugf("Created manifest file %s out of %s", filename, manifestPath)
	}

	err = os.Remove(manifestPath)
	if err != nil {
		return errors.Wrapf(err, "failed to remove multi-doc yaml %s", manifestPath)
	}

	return nil
}

// updateBootstrap adds a status annotation to each BareMetalHost defined in the
// bootstrap ignition file
func (g *installerGenerator) updateBootstrap(ctx context.Context, bootstrapPath string) error {
	log := logutil.FromContext(ctx, g.log)
	//nolint:shadow
	config, err := parseIgnitionFile(bootstrapPath)
	if err != nil {
		g.log.Error(err)
		return err
	}

	newFiles := []config_latest_types.File{}

	masters, workers := sortHosts(g.cluster.Hosts)
	for i, file := range config.Storage.Files {
		switch {
		case isBaremetalProvisioningConfig(&config.Storage.Files[i]):
			if !g.enableMetal3Provisioning {
				// drop this from the list of Files because we don't want to run BMO
				continue
			}
		case isMOTD(&config.Storage.Files[i]):
			// workaround for https://github.com/openshift/machine-config-operator/issues/2086
			g.fixMOTDFile(&config.Storage.Files[i])
		case isBMHFile(&config.Storage.Files[i]):
			// extract bmh
			bmh, err2 := fileToBMH(&config.Storage.Files[i]) //nolint,shadow
			if err2 != nil {
				log.Errorf("error parsing File contents to BareMetalHost: %v", err2)
				return err2
			}

			// get corresponding host
			var host *models.Host
			masterHostnames := getHostnames(masters)
			workerHostnames := getHostnames(workers)

			// The BMH files in the ignition are sorted according to hostname (please see the implementation in installcfg/installcfg.go).
			// The masters and workers are also sorted by hostname.  This enables us to correlate correctly the host and the BMH file
			if bmhIsMaster(bmh, masterHostnames, workerHostnames) {
				if len(masters) == 0 {
					return errors.Errorf("Not enough registered masters to match with BareMetalHosts")
				}
				host, masters = masters[0], masters[1:]
			} else {
				if len(workers) == 0 {
					return errors.Errorf("Not enough registered workers to match with BareMetalHosts")
				}
				host, workers = workers[0], workers[1:]
			}

			// modify bmh
			log.Infof("modifying BareMetalHost ignition file %s", file.Node.Path)
			err = g.modifyBMHFile(&config.Storage.Files[i], bmh, host)
			if err != nil {
				return err
			}
		}
		newFiles = append(newFiles, config.Storage.Files[i])
	}

	config.Storage.Files = newFiles
	if swag.StringValue(g.cluster.HighAvailabilityMode) != models.ClusterHighAvailabilityModeNone {
		setFileInIgnition(config, "/opt/openshift/assisted-install-bootstrap", "data:,", false, 420, false)
	}

	// add new Network Manager config file that disables handling of /etc/resolv.conf
	// as there is no network scripts added in SNO mode (None) we should not touch Netmanager config
	if !common.IsSingleNodeCluster(g.cluster) {
		setNMConfigration(config)
	}

	err = writeIgnitionFile(bootstrapPath, config)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Infof("Updated file %s", bootstrapPath)

	return nil
}

// fixMOTDFile is a workaround for a bug in machine-config-operator, where it
// incorrectly parses igition when a File is configured to append content
// instead of overwrite. Currently, /etc/motd is the only file involved in
// provisioning that is configured for appending. This code converts it to
// overwriting the existing /etc/motd with whatever content had been indended
// to be appened.
// https://github.com/openshift/machine-config-operator/issues/2086
func (g *installerGenerator) fixMOTDFile(file *config_latest_types.File) {
	if file.Contents.Source != nil {
		// the bug only happens if Source == nil, so no need to take action
		return
	}
	if len(file.Append) == 1 {
		file.Contents.Source = file.Append[0].Source
		file.Append = file.Append[:0]
		return
	}
	g.log.Info("could not apply workaround to file /etc/motd for MCO bug. The workaround may no longer be necessary.")
}

// modifyBMHFile modifies the File contents so that the serialized BareMetalHost
// includes a status annotation
func (g *installerGenerator) modifyBMHFile(file *config_latest_types.File, bmh *bmh_v1alpha1.BareMetalHost, host *models.Host) error {
	inventory := models.Inventory{}
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		return err
	}

	hw := bmh_v1alpha1.HardwareDetails{
		CPU: bmh_v1alpha1.CPU{
			Arch:           inventory.CPU.Architecture,
			Model:          inventory.CPU.ModelName,
			ClockMegahertz: bmh_v1alpha1.ClockSpeed(inventory.CPU.Frequency),
			Flags:          inventory.CPU.Flags,
			Count:          int(inventory.CPU.Count),
		},
		Hostname: host.RequestedHostname,
		NIC:      make([]bmh_v1alpha1.NIC, len(inventory.Interfaces)),
		Storage:  make([]bmh_v1alpha1.Storage, len(inventory.Disks)),
	}
	if inventory.Memory != nil {
		hw.RAMMebibytes = int(inventory.Memory.PhysicalBytes / 1024 / 1024)
	}
	for i, iface := range inventory.Interfaces {
		hw.NIC[i] = bmh_v1alpha1.NIC{
			Name:      iface.Name,
			Model:     iface.Product,
			MAC:       iface.MacAddress,
			SpeedGbps: int(iface.SpeedMbps / 1024),
		}
		switch {
		case len(iface.IPV4Addresses) > 0:
			hw.NIC[i].IP = g.selectInterfaceIPInsideMachineCIDR(iface.IPV4Addresses)
		case len(iface.IPV6Addresses) > 0:
			hw.NIC[i].IP = g.selectInterfaceIPInsideMachineCIDR(iface.IPV6Addresses)
		}
	}
	for i, disk := range inventory.Disks {
		device := disk.Path
		if disk.ByPath != "" {
			device = disk.ByPath
		}
		hw.Storage[i] = bmh_v1alpha1.Storage{
			Name:         device,
			Vendor:       disk.Vendor,
			SizeBytes:    bmh_v1alpha1.Capacity(disk.SizeBytes),
			Model:        disk.Model,
			WWN:          disk.Wwn,
			HCTL:         disk.Hctl,
			SerialNumber: disk.Serial,
			Rotational:   (disk.DriveType == models.DriveTypeHDD),
		}
	}
	if inventory.SystemVendor != nil {
		hw.SystemVendor = bmh_v1alpha1.HardwareSystemVendor{
			Manufacturer: inventory.SystemVendor.Manufacturer,
			ProductName:  inventory.SystemVendor.ProductName,
			SerialNumber: inventory.SystemVendor.SerialNumber,
		}
	}
	status := bmh_v1alpha1.BareMetalHostStatus{
		HardwareDetails: &hw,
		PoweredOn:       true,
	}
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return err
	}
	metav1.SetMetaDataAnnotation(&bmh.ObjectMeta, bmh_v1alpha1.StatusAnnotation, string(statusJSON))
	if g.enableMetal3Provisioning {
		bmh.Spec.ExternallyProvisioned = true
	}

	serializer := k8sjson.NewSerializerWithOptions(
		k8sjson.DefaultMetaFactory, nil, nil,
		k8sjson.SerializerOptions{
			Yaml:   true,
			Pretty: true,
			Strict: true,
		},
	)
	buf := bytes.Buffer{}
	err = serializer.Encode(bmh, &buf)
	if err != nil {
		return err
	}

	// remove status if exists
	res := bytes.Split(buf.Bytes(), []byte("status:\n"))
	encodedBMH := base64.StdEncoding.EncodeToString(res[0])
	source := "data:text/plain;charset=utf-8;base64," + encodedBMH
	file.Contents.Source = &source

	return nil
}

func (g *installerGenerator) updateDhcpFiles() error {
	path := filepath.Join(g.workDir, masterIgn)
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}
	setFileInIgnition(config, "/etc/keepalived/unsupported-monitor.conf", g.encodedDhcpFileContents, false, 0o644, false)
	encodedApiVip := network.GetEncodedApiVipLease(g.cluster)
	if encodedApiVip != "" {
		setFileInIgnition(config, "/etc/keepalived/lease-api", encodedApiVip, false, 0o644, false)
	}
	encodedIngressVip := network.GetEncodedIngressVipLease(g.cluster)
	if encodedIngressVip != "" {
		setFileInIgnition(config, "/etc/keepalived/lease-ingress", encodedIngressVip, false, 0o644, false)
	}
	err = writeIgnitionFile(path, config)
	if err != nil {
		return err
	}
	return nil
}

// addIpv6FileInIgnition adds a NetworkManager configuration ensuring that IPv6 DHCP requests use
// consistent client identification.
func (g *installerGenerator) addIpv6FileInIgnition(ignition string) error {
	path := filepath.Join(g.workDir, ignition)
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}
	is410Version, err := common.VersionGreaterOrEqual(g.cluster.OpenshiftVersion, "4.10.0-0.alpha")
	if err != nil {
		return err
	}
	v6config := common.Ipv6DuidRuntimeConfPre410
	if is410Version {
		v6config = common.Ipv6DuidRuntimeConf
	}
	setFileInIgnition(config, "/etc/NetworkManager/conf.d/01-ipv6.conf", encodeIpv6Contents(v6config), false, 0o644, false)
	err = writeIgnitionFile(path, config)
	if err != nil {
		return err
	}
	return nil
}

func (g *installerGenerator) updateIgnitions() error {
	masterPath := filepath.Join(g.workDir, masterIgn)
	caCertFile := g.serviceCACert

	if caCertFile != "" {
		err := setCACertInIgnition(models.HostRoleMaster, masterPath, g.workDir, caCertFile)
		if err != nil {
			return errors.Wrapf(err, "error adding CA cert to ignition %s", masterPath)
		}
	}

	if g.encodedDhcpFileContents != "" {
		if err := g.updateDhcpFiles(); err != nil {
			return errors.Wrapf(err, "error adding DHCP file to ignition %s", masterPath)
		}
	}

	workerPath := filepath.Join(g.workDir, workerIgn)
	if caCertFile != "" {
		err := setCACertInIgnition(models.HostRoleWorker, workerPath, g.workDir, caCertFile)
		if err != nil {
			return errors.Wrapf(err, "error adding CA cert to ignition %s", workerPath)
		}
	}

	_, ipv6, err := network.GetClusterAddressStack(g.cluster.Hosts)
	if err != nil {
		return err
	}
	if ipv6 {
		for _, ignition := range []string{masterIgn, workerIgn} {
			if err = g.addIpv6FileInIgnition(ignition); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *installerGenerator) selectInterfaceIPInsideMachineCIDR(interfaceCIDRs []string) string {
	machineCIDRs := make([]string, len(g.cluster.MachineNetworks))
	for i, machineNetwork := range g.cluster.MachineNetworks {
		machineCIDRs[i] = string(machineNetwork.Cidr)
	}
	log := g.log.WithFields(logrus.Fields{
		"interface_cidrs": interfaceCIDRs,
		"machine_cidrs":   machineCIDRs,
	})
	for _, interfaceCIDR := range interfaceCIDRs {
		interfaceIP, _, err := net.ParseCIDR(interfaceCIDR)
		if err != nil {
			log.WithError(err).WithFields(logrus.Fields{
				"interface_cidr": interfaceCIDR,
			}).Error("Failed to parse interface CIDR")
			continue
		}
		for _, machineCIDR := range machineCIDRs {
			_, machineNetwork, err := net.ParseCIDR(machineCIDR)
			if err != nil {
				log.WithError(err).Error("Failed to parse machine CIDR")
				continue
			}
			if machineNetwork.Contains(interfaceIP) {
				log.WithFields(logrus.Fields{
					"machine_cidr": machineCIDR,
					"interface_ip": interfaceIP,
				}).Info("Selected interface IP")
				return interfaceIP.String()
			}
		}
	}
	if len(interfaceCIDRs) > 0 {
		firstCIDR := interfaceCIDRs[0]
		firstIP, _, err := net.ParseCIDR(interfaceCIDRs[0])
		if err != nil {
			log.WithError(err).WithFields(logrus.Fields{
				"first_cidr": firstCIDR,
			}).Error("Failed to parse first interface CIDR")
			return ""
		}
		log.WithFields(logrus.Fields{
			"first_cidr": firstCIDR,
			"first_ip":   firstIP,
		}).Warn("Failed to find an interface IP within the machine CIDR, will use the first IP")
		return firstIP.String()
	}
	log.Warn("There are no interface IP addresses")
	return ""
}

func (g *installerGenerator) getManifestContent(ctx context.Context, manifest string) (string, error) {
	respBody, _, err := g.s3Client.Download(ctx, manifest)
	if err != nil {
		return "", err
	}
	content, err := io.ReadAll(respBody)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (g *installerGenerator) clusterHasMCP(poolName string, clusterId *strfmt.UUID) (bool, error) {
	var err error
	ctx := context.Background()
	manifestList, err := manifests.GetClusterManifests(ctx, clusterId, g.s3Client)
	if err != nil {
		return false, err
	}
	for _, manifest := range manifestList {
		content, err := g.getManifestContent(ctx, manifest)
		if err != nil {
			return false, err
		}
		exists, err := machineConfilePoolExists(manifest, content, poolName)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func (g *installerGenerator) updatePointerIgnitionMCP(poolName string, ignitionStr string) (string, error) {
	config, err := ParseToLatest([]byte(ignitionStr))
	if err != nil {
		return "", err
	}
	for i := range config.Ignition.Config.Merge {
		r := &config.Ignition.Config.Merge[i]
		if r.Source != nil {
			r.Source = swag.String(strings.Replace(swag.StringValue(r.Source), "config/worker", "config/"+poolName, 1))
		}
	}
	b, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (g *installerGenerator) modifyPointerIgnitionMCP(poolName string, ignitionStr string, clusterId *strfmt.UUID) (string, error) {
	var (
		mcpExists bool
		err       error
		ret       string
	)
	mcpExists, err = g.clusterHasMCP(poolName, clusterId)
	if err != nil {
		g.log.WithError(err).Errorf("failed to find if machine config pool %s exists", poolName)
		return "", err
	}
	if mcpExists {
		ret, err = g.updatePointerIgnitionMCP(poolName, ignitionStr)
		if err != nil {
			g.log.WithError(err).Errorf("failed to update pointer ignition for pool %s", poolName)
			return "", err
		}
		return ret, nil
	}
	return "", errors.Errorf("machine config pool %s was not found", poolName)
}

func (g *installerGenerator) writeSingleHostFile(host *models.Host, baseFile string, workDir string) error {
	config, err := parseIgnitionFile(filepath.Join(workDir, baseFile))
	if err != nil {
		return err
	}

	hostname, err := hostutil.GetCurrentHostName(host)
	if err != nil {
		return errors.Wrapf(err, "failed to get hostname for host %s", host.ID)
	}

	setFileInIgnition(config, "/etc/hostname", fmt.Sprintf("data:,%s", hostname), false, 420, true)
	if common.IsSingleNodeCluster(g.cluster) {
		machineCidr := g.cluster.MachineNetworks[0]
		ip, _, errP := net.ParseCIDR(string(machineCidr.Cidr))
		if errP != nil {
			return errors.Wrapf(errP, "Failed to parse machine cidr for node ip hint content")
		}
		setFileInIgnition(config, nodeIpHintFile, fmt.Sprintf("data:,KUBELET_NODEIP_HINT=%s", ip), false, 420, true)
	} else if g.nodeIpAllocations != nil && common.IsMultiNodeNonePlatformCluster(g.cluster) {
		allocation, ok := g.nodeIpAllocations[lo.FromPtr(host.ID)]
		if ok {
			setFileInIgnition(config, nodeIpHintFile, fmt.Sprintf("data:,KUBELET_NODEIP_HINT=%s", allocation.HintIp), false, 420, true)
			g.log.Infof("Set KUBELET_NODEIP_HINT=%s for host %s", allocation.HintIp, lo.FromPtr(host.ID))
		}
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	if host.IgnitionConfigOverrides != "" {
		merged, mergeErr := MergeIgnitionConfig(configBytes, []byte(host.IgnitionConfigOverrides))
		if mergeErr != nil {
			return errors.Wrapf(mergeErr, "failed to apply ignition config overrides for host %s", host.ID)
		}
		configBytes = []byte(merged)
	}

	if host.Role == models.HostRoleWorker && host.MachineConfigPoolName != "" {
		var override string
		override, err = g.modifyPointerIgnitionMCP(host.MachineConfigPoolName, string(configBytes), host.ClusterID)
		if err != nil {
			return errors.Wrapf(err, "failed to set machine config pool %s to pointer ignition for host %s",
				host.MachineConfigPoolName, host.ID.String())
		}
		configBytes = []byte(override)
	}

	err = os.WriteFile(filepath.Join(workDir, hostutil.IgnitionFileName(host)), configBytes, 0600)
	if err != nil {
		return errors.Wrapf(err, "failed to write ignition for host %s", host.ID)
	}

	return nil
}

func (g *installerGenerator) writeHostFiles(hosts []*models.Host, baseFile string, workDir string) error {
	errGroup := new(errgroup.Group)
	for i := range hosts {
		host := hosts[i]
		errGroup.Go(func() error {
			return g.writeSingleHostFile(host, baseFile, workDir)
		})
	}

	return errGroup.Wait()
}

// createHostIgnitions builds an ignition file for each host in the cluster based on the generated <role>.ign file
func (g *installerGenerator) createHostIgnitions() error {
	masters, workers := sortHosts(g.cluster.Hosts)

	err := g.writeHostFiles(masters, masterIgn, g.workDir)
	if err != nil {
		return errors.Wrapf(err, "error writing master host ignition files")
	}

	err = g.writeHostFiles(workers, workerIgn, g.workDir)
	if err != nil {
		return errors.Wrapf(err, "error writing worker host ignition files")
	}

	return nil
}

func (g *installerGenerator) runCreateCommand(ctx context.Context, installerPath, command string, envVars []string) error {
	log := logutil.FromContext(ctx, g.log)
	cmd := exec.Command(installerPath, "create", command, "--dir", g.workDir) //nolint:gosec
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = envVars
	err := cmd.Run()
	if err != nil {
		log.WithError(err).
			Errorf("error running openshift-install create %s, stdout: %s", command, out.String())
		return errors.Wrapf(err, "error running openshift-install %s,  %s", command, firstN(out.String(), 512))
	}
	return nil
}

func (g *installerGenerator) downloadManifest(ctx context.Context, manifest string) error {
	respBody, _, err := g.s3Client.Download(ctx, manifest)
	if err != nil {
		return err
	}
	content, err := io.ReadAll(respBody)
	if err != nil {
		return err
	}

	if len(content) == 0 {
		// Ignore any empty files.
		return nil
	}

	// manifest has full path as object-key on s3: clusterID/manifests/[manifests|openshift]/filename
	// clusterID/manifests should be trimmed
	prefix := manifests.GetManifestObjectName(*g.cluster.ID, "")
	targetPath := filepath.Join(g.workDir, strings.TrimPrefix(manifest, prefix))

	err = os.WriteFile(targetPath, content, 0600)
	if err != nil {
		return err
	}
	return nil
}

// UploadToS3 uploads the generated files to S3
func uploadToS3(ctx context.Context, workDir string, cluster *common.Cluster, s3Client s3wrapper.API, log logrus.FieldLogger) error {
	toUpload := fileNames[:]
	for _, host := range cluster.Hosts {
		toUpload = append(toUpload, hostutil.IgnitionFileName(host))
	}

	for _, fileName := range toUpload {
		fullPath := filepath.Join(workDir, fileName)
		key := filepath.Join(cluster.ID.String(), fileName)
		err := s3Client.UploadFile(ctx, fullPath, key)
		if err != nil {
			log.Errorf("Failed to upload file %s as object %s", fullPath, key)
			return err
		}
		_, err = s3Client.UpdateObjectTimestamp(ctx, key)
		if err != nil {
			return err
		}
		log.Infof("Uploaded file %s as object %s", fullPath, key)
	}

	return nil
}

func getHostnames(hosts []*models.Host) []string {
	ret := make([]string, 0)
	for _, h := range hosts {
		ret = append(ret, hostutil.GetHostnameForMsg(h))
	}
	return ret

}

func bmhIsMaster(bmh *bmh_v1alpha1.BareMetalHost, masterHostnames, workerHostnames []string) bool {
	if funk.ContainsString(masterHostnames, bmh.Name) {
		return true
	}
	if funk.ContainsString(workerHostnames, bmh.Name) {
		return false
	}

	// For backward compatibility in case the name is not in the (masterHostnames, workerHostnames)
	return strings.Contains(bmh.Name, "-master-")
}

// ExtractClusterID gets a local path of a "bootstrap.ign" file and extracts the OpenShift cluster ID
func ExtractClusterID(reader io.ReadCloser) (string, error) {
	bs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	config, err := ParseToLatest(bs)
	if err != nil {
		return "", err
	}

	for _, f := range config.Storage.Files {
		if f.Node.Path != "/opt/openshift/manifests/cvo-overrides.yaml" {
			continue
		}

		source := f.FileEmbedded1.Contents.Key()
		dataURL, err := dataurl.DecodeString(source)
		if err != nil {
			return "", err
		}

		cv := clusterVersion{}
		err = yaml.Unmarshal(dataURL.Data, &cv)
		if err != nil {
			return "", err
		}

		if cv.Spec.ClusterID == "" {
			return "", errors.New("no ClusterID field in cvo-overrides file")
		}

		return cv.Spec.ClusterID, nil
	}

	return "", errors.New("could not find cvo-overrides file")
}

func isBMHFile(file *config_latest_types.File) bool {
	return strings.Contains(file.Node.Path, "openshift-cluster-api_hosts")
}

func isMOTD(file *config_latest_types.File) bool {
	return file.Node.Path == "/etc/motd"
}

func isBaremetalProvisioningConfig(file *config_latest_types.File) bool {
	return strings.Contains(file.Node.Path, "baremetal-provisioning-config")
}

func fileToBMH(file *config_latest_types.File) (*bmh_v1alpha1.BareMetalHost, error) {
	parts := strings.Split(*file.Contents.Source, "base64,")
	if len(parts) != 2 {
		return nil, errors.Errorf("could not parse source for file %s", file.Node.Path)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	bmh := &bmh_v1alpha1.BareMetalHost{}
	_, _, err = scheme.Codecs.UniversalDeserializer().Decode(decoded, nil, bmh)
	if err != nil {
		return nil, err
	}

	return bmh, nil
}

func encodeIpv6Contents(config string) string {
	return fmt.Sprintf("data:,%s", url.PathEscape(config))
}

// sortHosts sorts hosts into masters and workers, excluding disabled hosts
func sortHosts(hosts []*models.Host) ([]*models.Host, []*models.Host) {
	masters := []*models.Host{}
	workers := []*models.Host{}
	for i := range hosts {
		switch {
		case common.GetEffectiveRole(hosts[i]) == models.HostRoleMaster:
			masters = append(masters, hosts[i])
		default:
			workers = append(workers, hosts[i])
		}
	}

	// sort them so the result is repeatable
	sort.SliceStable(masters, func(i, j int) bool {
		return hostutil.GetHostnameForMsg(masters[i]) < hostutil.GetHostnameForMsg(masters[j])
	})
	sort.SliceStable(workers, func(i, j int) bool {
		return hostutil.GetHostnameForMsg(workers[i]) < hostutil.GetHostnameForMsg(workers[j])
	})
	return masters, workers
}

func setNMConfigration(config *config_latest_types.Config) {
	fileContents := "data:text/plain;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(common.UnmanagedResolvConf))
	setFileInIgnition(config, "/etc/NetworkManager/conf.d/99-kni.conf", fileContents, false, 420, false)
}

func setCACertInIgnition(role models.HostRole, path string, workDir string, caCertFile string) error {
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}

	var caCertData []byte
	caCertData, err = os.ReadFile(caCertFile)
	if err != nil {
		return err
	}

	setFileInIgnition(config, common.HostCACertPath, fmt.Sprintf("data:,%s", url.PathEscape(string(caCertData))), false, 420, false)

	fileName := fmt.Sprintf("%s.ign", role)
	err = writeIgnitionFile(filepath.Join(workDir, fileName), config)
	if err != nil {
		return err
	}

	return nil
}

func machineConfilePoolExists(manifestFname, content, poolName string) (bool, error) {
	var (
		manifest struct {
			Kind     string
			Metadata *struct {
				Name string
			}
		}
		err error
	)
	ext := filepath.Ext(manifestFname)
	switch ext {
	case ".yml", ".yaml":
		err = yaml.Unmarshal([]byte(content), &manifest)
	case ".json":
		err = json.Unmarshal([]byte(content), &manifest)
	default:
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return manifest.Kind == "MachineConfigPool" && manifest.Metadata != nil && manifest.Metadata.Name == poolName, nil
}

func firstN(s string, n int) string {
	const suffix string = " <TRUNCATED>"
	if len(s) > n+len(suffix) {
		return s[:(n-len(suffix))] + suffix
	}
	return s
}
