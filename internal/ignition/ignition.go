package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coreos/ignition/v2/config/merge"
	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_32 "github.com/coreos/ignition/v2/config/v3_2"
	config_32_trans "github.com/coreos/ignition/v2/config/v3_2/translate"
	config_32_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/coreos/vcontext/report"
	"github.com/go-openapi/swag"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators/lso"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	masterIgn = "master.ign"
	workerIgn = "worker.ign"
)

const CopyStaticIpsConfigurationsScript = `
#!/bin/bash

mkdir -p /etc/NetworkManager/system-connections-merged

/usr/bin/cp /etc/NetworkManager/system-connections/* /etc/NetworkManager/system-connections-merged
`

const CopyStaticIpsConfigServiceContents = `
[Unit]
Description=Copy Static Ip configuration
Before=NetworkManager.service
DefaultDependencies=no
[Service]
User=root
Type=oneshot
TimeoutSec=10
ExecStart=/bin/bash /usr/local/bin/copy-static-ip-configuration.sh
PrivateTmp=yes
RemainAfterExit=no
[Install]
WantedBy=multi-user.target
`

const SystemConnectionsMerged = `
[Unit]
After=systemd-tmpfiles-setup.service
[Mount]
Where=/etc/NetworkManager/system-connections-merged
What=overlay
Type=overlay
Options=lowerdir=/etc/NetworkManager/system-connections,upperdir=/run/nm-system-connections,workdir=/run/nm-system-connections-work
[Install]
WantedBy=multi-user.target
`

const kniTempFile = `
D /run/nm-system-connections 0755 root root - -
D /run/nm-system-connections-work 0755 root root - -
d /etc/NetworkManager/system-connections-merged 0755 root root -
`

var fileNames = [...]string{
	"bootstrap.ign",
	masterIgn,
	"metadata.json",
	workerIgn,
	"kubeconfig-noingress",
	"kubeadmin-password",
	"install-config.yaml",
}

// Generator can generate ignition files and upload them to an S3-like service
type Generator interface {
	Generate(ctx context.Context, installConfig []byte, ocsValidatorConfig *ocs.Config) error
	UploadToS3(ctx context.Context) error
	UpdateEtcHosts(string) error
}

type installerGenerator struct {
	log                      logrus.FieldLogger
	workDir                  string
	cluster                  *common.Cluster
	releaseImage             string
	releaseImageMirror       string
	installerDir             string
	serviceCACert            string
	encodedDhcpFileContents  string
	s3Client                 s3wrapper.API
	enableMetal3Provisioning bool
}

// NewGenerator returns a generator that can generate ignition files
func NewGenerator(workDir string, installerDir string, cluster *common.Cluster, releaseImage string, releaseImageMirror string,
	serviceCACert string, s3Client s3wrapper.API, log logrus.FieldLogger) Generator {
	return &installerGenerator{
		cluster:                  cluster,
		log:                      log,
		releaseImage:             releaseImage,
		releaseImageMirror:       releaseImageMirror,
		workDir:                  workDir,
		installerDir:             installerDir,
		serviceCACert:            serviceCACert,
		s3Client:                 s3Client,
		enableMetal3Provisioning: true,
	}
}

// UploadToS3 uploads generated ignition and related files to the configured
// S3-compatible storage
func (g *installerGenerator) UploadToS3(ctx context.Context) error {
	return uploadToS3(ctx, g.workDir, g.cluster, g.s3Client, g.log)
}
func (g *installerGenerator) checkLsoEnabled() bool {
	result := false
	if g.cluster.Operators != "" {
		var operators models.Operators
		if err := json.Unmarshal([]byte(g.cluster.Operators), &operators); err != nil {
			g.log.Error("Failed to get Cluster Operators ", err)
			return false
		}
		for _, operator := range operators {
			if operator.OperatorType == models.OperatorTypeLso && swag.BoolValue(operator.Enabled) {
				result = true
				break
			}
		}
	}
	g.log.Info("LSO is set to ", result)
	return result
}

func (g *installerGenerator) checkOcsEnabled() bool {
	result := false
	if g.cluster.Operators != "" {
		var operators models.Operators
		if err := json.Unmarshal([]byte(g.cluster.Operators), &operators); err != nil {
			g.log.Error("Failed to get Cluster Operators ", err)
			return false
		}
		for _, operator := range operators {
			if operator.OperatorType == models.OperatorTypeOcs && swag.BoolValue(operator.Enabled) {
				result = true
				break
			}
		}
	}
	g.log.Info("OCS is set to ", result)
	return result
}

func (g *installerGenerator) createManifestDirectory(installerPath string, envVars []string) error {
	g.log.Info("Creating Manifest directory")
	err := g.runCreateCommand(installerPath, "manifests", envVars)
	if err != nil {
		g.log.Error("Error occured while creating manifest directory ", err)
		return err
	}
	return nil
}

func (g *installerGenerator) generateLsoManifests() error {
	g.log.Info("Creating LSO Manifests")
	manifests, err := lso.Manifests(g.cluster.Cluster.OpenshiftVersion)
	if err != nil {
		g.log.Error("Error creating LSO manifests ", err)
		return err
	}
	manifestDirPath := filepath.Join(g.workDir, "manifests")
	for name, manifest := range manifests {
		manifestPath := filepath.Join(manifestDirPath, name)
		err := ioutil.WriteFile(manifestPath, []byte(manifest), 0600)
		if err != nil {
			g.log.Errorf("Failed to write file %s %s", manifestPath, name)
			return err
		}
	}
	return nil
}
func (g *installerGenerator) generateOcsManifests(ocsValidatorConfig *ocs.Config) error {
	g.log.Info("Creating OCS Manifests")
	manifests, err := ocs.Manifests(ocsValidatorConfig.OCSMinimalDeployment, g.cluster.OpenshiftVersion, ocsValidatorConfig.OCSDisksAvailable, len(g.cluster.Cluster.Hosts))
	if err != nil {
		g.log.Error("Cannot generate OCS manifests due to ", err)
		return err
	}
	manifestDirPath := filepath.Join(g.workDir, "manifests")
	for name, manifest := range manifests {
		manifestPath := filepath.Join(manifestDirPath, name)
		err := ioutil.WriteFile(manifestPath, []byte(manifest), 0600)
		if err != nil {
			g.log.Errorf("Failed to write file %s %s", manifestPath, name)
			return err
		}
	}
	return nil
}

func (g *installerGenerator) generateOperatorsManifests(ctx context.Context, installerPath string, envVars []string, ocsValidatorConfig *ocs.Config) error {
	lsoEnabled := false
	ocsEnabled := g.checkOcsEnabled()
	if ocsEnabled {
		lsoEnabled = true // if OCS is enabled, LSO must be enabled by default
	} else {
		lsoEnabled = g.checkLsoEnabled()
	}
	if lsoEnabled || ocsEnabled {
		err := g.createManifestDirectory(installerPath, envVars)
		if err != nil {
			g.log.Error("Failed to create Manifest directory ", err)
			return err
		}
	}

	if lsoEnabled {
		err := g.generateLsoManifests()
		if err != nil {
			g.log.Error(err)
			return err
		}
	}

	if ocsEnabled {
		err := g.generateOcsManifests(ocsValidatorConfig)
		if err != nil {
			g.log.Error(err)
			return err
		}
	}
	return nil
}

// Generate generates ignition files and applies modifications.
func (g *installerGenerator) Generate(ctx context.Context, installConfig []byte, ocsValidatorConfig *ocs.Config) error {
	installerPath, err := installercache.Get(g.releaseImage, g.releaseImageMirror, g.installerDir, g.cluster.PullSecret, g.log)
	if err != nil {
		return err
	}
	installConfigPath := filepath.Join(g.workDir, "install-config.yaml")

	g.enableMetal3Provisioning, err = common.VersionGreaterOrEqual(g.cluster.Cluster.OpenshiftVersion, "4.7")
	if err != nil {
		return err
	}

	g.encodedDhcpFileContents, err = network.GetEncodedDhcpParamFileContents(g.cluster)
	if err != nil {
		wrapped := errors.Wrapf(err, "Could not create DHCP encoded file")
		g.log.WithError(wrapped).Errorf("GenerateInstallConfig")
		return wrapped
	}
	envVars := append(os.Environ(),
		"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE="+g.releaseImage,
		"OPENSHIFT_INSTALL_INVOKER=assisted-installer",
	)

	// write installConfig to install-config.yaml so openshift-install can read it
	err = ioutil.WriteFile(installConfigPath, installConfig, 0600)
	if err != nil {
		g.log.Errorf("Failed to write file %s", installConfigPath)
		return err
	}

	manifestFiles, err := manifests.GetClusterManifests(ctx, g.cluster.ID, g.s3Client)
	if err != nil {
		g.log.WithError(err).Errorf("Failed to check if cluster %s has manifests", g.cluster.ID)
		return err
	}

	// invoke 'create manifests' command and download cluster manifests to manifests folder
	if len(manifestFiles) > 0 {
		err = g.runCreateCommand(installerPath, "manifests", envVars)
		if err != nil {
			return err
		}
		// download manifests files to working directory
		for _, manifest := range manifestFiles {
			g.log.Infof("Adding manifest %s to working dir for cluster %s", manifest, g.cluster.ID)
			err = g.downloadManifest(ctx, manifest)
			if err != nil {
				_ = os.Remove(filepath.Join(g.workDir, "manifests"))
				_ = os.Remove(filepath.Join(g.workDir, "openshift"))
				g.log.WithError(err).Errorf("Failed to download manifest %s to working dir for cluster %s", manifest, g.cluster.ID)
				return err
			}
		}

	}

	err = g.generateOperatorsManifests(ctx, installerPath, envVars, ocsValidatorConfig)
	if err != nil {
		return err
	}

	if swag.StringValue(g.cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone {
		err = g.bootstrapInPlaceIgnitionsCreate(installerPath, envVars)
	} else {
		err = g.runCreateCommand(installerPath, "ignition-configs", envVars)
	}
	if err != nil {
		g.log.Error(err)
		return err
	}

	// parse ignition and update BareMetalHosts
	bootstrapPath := filepath.Join(g.workDir, "bootstrap.ign")
	err = g.updateBootstrap(bootstrapPath)
	if err != nil {
		return err
	}

	err = g.updateIgnitions()
	if err != nil {
		g.log.Error(err)
		return err
	}

	err = g.createHostIgnitions()
	if err != nil {
		g.log.Error(err)
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
	err = ioutil.WriteFile(installConfigPath, installConfig, 0600)
	if err != nil {
		g.log.Errorf("Failed to write file %s", installConfigPath)
		return err
	}

	err = os.Remove(filepath.Join(g.workDir, "auth"))
	if err != nil {
		return err
	}
	return nil
}

func (g *installerGenerator) bootstrapInPlaceIgnitionsCreate(installerPath string, envVars []string) error {
	err := g.runCreateCommand(installerPath, "single-node-ignition-config", envVars)
	if err != nil {
		return errors.Wrapf(err, "Failed to create single node ignitions")
	}

	// In case of single node rename bootstrap Ignition file
	err = os.Rename(filepath.Join(g.workDir, "bootstrap-in-place-for-live-iso.ign"), filepath.Join(g.workDir, "bootstrap.ign"))
	if err != nil {
		return errors.Wrapf(err, "Failed to rename bootstrap-in-place-for-live-iso.ign")
	}

	//TODO: Is BIP usable in 4.6? Are we safe using 3.2 in all cases here?
	config := config_32_types.Config{Ignition: config_32_types.Ignition{Version: config_32_types.MaxVersion.String()}}

	for _, file := range []string{masterIgn, workerIgn} {
		err = writeIgnitionFile(filepath.Join(g.workDir, file), &config)
		if err != nil {
			return errors.Wrapf(err, "Failed to create %s", file)
		}
	}

	return nil
}

func bmhIsMaster(bmh *bmh_v1alpha1.BareMetalHost) bool {
	return strings.Contains(bmh.Name, "-master-")
}

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

// ExtractClusterID gets a local path of a "bootstrap.ign" file and extracts the OpenShift cluster ID
func ExtractClusterID(reader io.ReadCloser) (string, error) {
	bs, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}

	config, err := ParseTo32(bs)
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

// updateBootstrap adds a status annotation to each BareMetalHost defined in the
// bootstrap ignition file
func (g *installerGenerator) updateBootstrap(bootstrapPath string) error {
	config, err := parseIgnitionFile(bootstrapPath)
	if err != nil {
		g.log.Error(err)
		return err
	}

	newFiles := []config_32_types.File{}

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
			bmh, err := fileToBMH(&config.Storage.Files[i]) //nolint,shadow
			if err != nil {
				g.log.Errorf("error parsing File contents to BareMetalHost: %v", err)
				return err
			}

			// get corresponding host
			var host *models.Host
			if bmhIsMaster(bmh) {
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
			g.log.Infof("modifying BareMetalHost ignition file %s", file.Node.Path)
			err = g.modifyBMHFile(&config.Storage.Files[i], bmh, host)
			if err != nil {
				return err
			}
		}
		newFiles = append(newFiles, config.Storage.Files[i])
	}

	config.Storage.Files = newFiles
	if swag.StringValue(g.cluster.HighAvailabilityMode) != models.ClusterHighAvailabilityModeNone {
		setFileInIgnition(config, "/opt/openshift/assisted-install-bootstrap", "data:,", false, 420)
	}
	err = writeIgnitionFile(bootstrapPath, config)
	if err != nil {
		g.log.Error(err)
		return err
	}
	g.log.Infof("Updated file %s", bootstrapPath)

	return nil
}

func isBMHFile(file *config_32_types.File) bool {
	return strings.Contains(file.Node.Path, "openshift-cluster-api_hosts")
}

func isMOTD(file *config_32_types.File) bool {
	return file.Node.Path == "/etc/motd"
}

func isBaremetalProvisioningConfig(file *config_32_types.File) bool {
	return strings.Contains(file.Node.Path, "baremetal-provisioning-config")
}

func fileToBMH(file *config_32_types.File) (*bmh_v1alpha1.BareMetalHost, error) {
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

// fixMOTDFile is a workaround for a bug in machine-config-operator, where it
// incorrectly parses igition when a File is configured to append content
// instead of overwrite. Currently, /etc/motd is the only file involved in
// provisioning that is configured for appending. This code converts it to
// overwriting the existing /etc/motd with whatever content had been indended
// to be appened.
// https://github.com/openshift/machine-config-operator/issues/2086
func (g *installerGenerator) fixMOTDFile(file *config_32_types.File) {
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
func (g *installerGenerator) modifyBMHFile(file *config_32_types.File, bmh *bmh_v1alpha1.BareMetalHost, host *models.Host) error {
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
			hw.NIC[i].IP = g.getInterfaceIP(iface.IPV4Addresses[0])
		case len(iface.IPV6Addresses) > 0:
			hw.NIC[i].IP = g.getInterfaceIP(iface.IPV6Addresses[0])
		}
	}
	for i, disk := range inventory.Disks {
		hw.Storage[i] = bmh_v1alpha1.Storage{
			Name:         disk.Name,
			Vendor:       disk.Vendor,
			SizeBytes:    bmh_v1alpha1.Capacity(disk.SizeBytes),
			Model:        disk.Model,
			WWN:          disk.Wwn,
			HCTL:         disk.Hctl,
			SerialNumber: disk.Serial,
			Rotational:   (disk.DriveType == "HDD"),
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

	encodedBMH := base64.StdEncoding.EncodeToString(buf.Bytes())
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
	setFileInIgnition(config, "/etc/keepalived/unsupported-monitor.conf", g.encodedDhcpFileContents, false, 0o644)
	encodedApiVip := network.GetEncodedApiVipLease(g.cluster)
	if encodedApiVip != "" {
		setFileInIgnition(config, "/etc/keepalived/lease-api", encodedApiVip, false, 0o644)
	}
	encodedIngressVip := network.GetEncodedIngressVipLease(g.cluster)
	if encodedIngressVip != "" {
		setFileInIgnition(config, "/etc/keepalived/lease-ingress", encodedIngressVip, false, 0o644)
	}
	err = writeIgnitionFile(path, config)
	if err != nil {
		return err
	}
	return nil
}

func encodeIpv6Contents() string {
	return fmt.Sprintf("data:,%s", url.PathEscape(common.Ipv6DuidRuntimeConf))
}

func (g *installerGenerator) addIpv6FileInIgnition(ignition string) error {
	path := filepath.Join(g.workDir, ignition)
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}
	setFileInIgnition(config, "/etc/NetworkManager/conf.d/01-ipv6.conf", encodeIpv6Contents(), false, 0o644)
	err = writeIgnitionFile(path, config)
	if err != nil {
		return err
	}
	return nil
}

func (g *installerGenerator) addStaticIPsConfigToIgnition(ignition string) error {
	path := filepath.Join(g.workDir, ignition)
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}
	is47Version, err := common.VersionGreaterOrEqual(g.cluster.OpenshiftVersion, "4.7")
	if err != nil {
		return err
	}
	if (swag.BoolValue(g.cluster.UserManagedNetworking) && is47Version) || !is47Version {
		// add overlay configuration for NM in case of 4.7 and None platform.
		// TODO - remove once this configuration is integrated in MCO for None platform (Bugzilla 1928473)
		setFileInIgnition(config, "/etc/tmpfiles.d/kni.conf", fmt.Sprintf("data:,%s", url.PathEscape(kniTempFile)), false, 420)
		setUnitInIgnition(config, SystemConnectionsMerged, "etc-NetworkManager-system\\x2dconnections\\x2dmerged.mount", true)
	}
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

	ipv6Only, err := network.AreIpv6OnlyHosts(g.cluster.Hosts, g.log)
	if err != nil {
		return err
	}
	if ipv6Only {
		for _, ignition := range []string{masterIgn, workerIgn} {
			if err = g.addIpv6FileInIgnition(ignition); err != nil {
				return err
			}
			if g.cluster.ImageInfo.StaticIpsConfig != "" {
				if err := g.addStaticIPsConfigToIgnition(ignition); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (g *installerGenerator) UpdateEtcHosts(serviceIPs string) error {
	masterPath := filepath.Join(g.workDir, masterIgn)

	if serviceIPs != "" {
		err := setEtcHostsInIgnition(models.HostRoleMaster, masterPath, g.workDir, GetServiceIPHostnames(serviceIPs))
		if err != nil {
			return errors.Wrapf(err, "error adding Etc Hosts to ignition %s", masterPath)
		}
	}

	workerPath := filepath.Join(g.workDir, workerIgn)
	if serviceIPs != "" {
		err := setEtcHostsInIgnition(models.HostRoleWorker, workerPath, g.workDir, GetServiceIPHostnames(serviceIPs))
		if err != nil {
			return errors.Wrapf(err, "error adding Etc Hosts to ignition %s", workerPath)
		}
	}
	return nil
}

// sortHosts sorts hosts into masters and workers, excluding disabled hosts
func sortHosts(hosts []*models.Host) ([]*models.Host, []*models.Host) {
	masters := []*models.Host{}
	workers := []*models.Host{}
	for i := range hosts {
		switch {
		case hosts[i].Status != nil && *hosts[i].Status == models.HostStatusDisabled:
			continue
		case hosts[i].Role == models.HostRoleMaster:
			masters = append(masters, hosts[i])
		default:
			workers = append(workers, hosts[i])
		}
	}

	// sort them so the result is repeatable
	sort.SliceStable(masters, func(i, j int) bool { return masters[i].RequestedHostname < masters[j].RequestedHostname })
	sort.SliceStable(workers, func(i, j int) bool { return workers[i].RequestedHostname < workers[j].RequestedHostname })
	return masters, workers
}

// UploadToS3 uploads the generated files to S3
func uploadToS3(ctx context.Context, workDir string, cluster *common.Cluster, s3Client s3wrapper.API, log logrus.FieldLogger) error {
	toUpload := fileNames[:]
	for _, host := range cluster.Hosts {
		if swag.StringValue(host.Status) != models.HostStatusDisabled {
			toUpload = append(toUpload, hostutil.IgnitionFileName(host))
		}
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

func ParseTo32(content []byte) (*config_32_types.Config, error) {
	configv32, _, err := config_32.Parse(content)
	if err != nil {
		configv31, _, err := config_31.Parse(content)
		if err != nil {
			return nil, errors.Errorf("error parsing ignition: %v", err)
		}
		configv32 = config_32_trans.Translate(configv31)
		configv32.Ignition.Version = "3.1.0"
	}

	return &configv32, nil
}

func parseIgnitionFile(path string) (*config_32_types.Config, error) {
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Errorf("error reading file %s: %v", path, err)
	}
	return ParseTo32(configBytes)
}

func (g *installerGenerator) getInterfaceIP(cidr string) string {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		g.log.Warnf("Failed to parse cidr %s for filling BMH CR", cidr)
		return ""
	}
	return ip.String()
}

// writeIgnitionFile writes an ignition config to a given path on disk
func writeIgnitionFile(path string, config *config_32_types.Config) error {
	updatedBytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(path, updatedBytes, 0600)
	if err != nil {
		return errors.Wrapf(err, "error writing file %s", path)
	}

	return nil
}

func setFileInIgnition(config *config_32_types.Config, filePath string, fileContents string, appendContent bool, mode int) {
	rootUser := "root"
	file := config_32_types.File{
		Node: config_32_types.Node{
			Path:      filePath,
			Overwrite: nil,
			Group:     config_32_types.NodeGroup{},
			User:      config_32_types.NodeUser{Name: &rootUser},
		},
		FileEmbedded1: config_32_types.FileEmbedded1{
			Append: []config_32_types.Resource{},
			Contents: config_32_types.Resource{
				Source: &fileContents,
			},
			Mode: &mode,
		},
	}
	if appendContent {
		file.FileEmbedded1.Append = []config_32_types.Resource{
			{
				Source: &fileContents,
			},
		}
		file.FileEmbedded1.Contents = config_32_types.Resource{}
	}
	config.Storage.Files = append(config.Storage.Files, file)
}

func setUnitInIgnition(config *config_32_types.Config, contents, name string, enabled bool) {
	newUnit := config_32_types.Unit{
		Contents: swag.String(contents),
		Name:     name,
		Enabled:  swag.Bool(enabled),
	}
	config.Systemd.Units = append(config.Systemd.Units, newUnit)
}

func setCACertInIgnition(role models.HostRole, path string, workDir string, caCertFile string) error {
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}

	var caCertData []byte
	caCertData, err = ioutil.ReadFile(caCertFile)
	if err != nil {
		return err
	}

	setFileInIgnition(config, common.HostCACertPath, fmt.Sprintf("data:,%s", url.PathEscape(string(caCertData))), false, 420)

	fileName := fmt.Sprintf("%s.ign", role)
	err = writeIgnitionFile(filepath.Join(workDir, fileName), config)
	if err != nil {
		return err
	}

	return nil
}

func writeHostFiles(hosts []*models.Host, baseFile string, workDir string) error {
	g := new(errgroup.Group)
	for i := range hosts {
		host := hosts[i]
		g.Go(func() error {
			config, err := parseIgnitionFile(filepath.Join(workDir, baseFile))
			if err != nil {
				return err
			}

			hostname, err := hostutil.GetCurrentHostName(host)
			if err != nil {
				return errors.Wrapf(err, "failed to get hostname for host %s", host.ID)
			}

			setFileInIgnition(config, "/etc/hostname", fmt.Sprintf("data:,%s", hostname), false, 420)

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

			err = ioutil.WriteFile(filepath.Join(workDir, hostutil.IgnitionFileName(host)), configBytes, 0600)
			if err != nil {
				return errors.Wrapf(err, "failed to write ignition for host %s", host.ID)
			}

			return nil
		})
	}

	return g.Wait()
}

// createHostIgnitions builds an ignition file for each host in the cluster based on the generated <role>.ign file
func (g *installerGenerator) createHostIgnitions() error {
	masters, workers := sortHosts(g.cluster.Hosts)

	err := writeHostFiles(masters, masterIgn, g.workDir)
	if err != nil {
		return errors.Wrapf(err, "error writing master host ignition files")
	}

	err = writeHostFiles(workers, workerIgn, g.workDir)
	if err != nil {
		return errors.Wrapf(err, "error writing worker host ignition files")
	}

	return nil
}

func MergeIgnitionConfig(base []byte, overrides []byte) (string, error) {
	baseConfig, err := ParseTo32(base)
	if err != nil {
		return "", err
	}

	overrideConfig, err := ParseTo32(overrides)
	if err != nil {
		return "", err
	}

	mergeResult, _ := merge.MergeStructTranscribe(*baseConfig, *overrideConfig)
	res, err := json.Marshal(mergeResult)
	if err != nil {
		return "", err
	}

	// Validate after we marshal to use the Parse functions
	var report report.Report
	if baseConfig.Ignition.Version == "3.1.0" {
		_, report, err = config_31.Parse(res)
	} else {
		_, report, err = config_32.Parse(res)
	}
	if err != nil {
		return "", err
	}
	if report.IsFatal() {
		return "", errors.Errorf("merged ignition config is invalid: %s", report.String())
	}

	return string(res), nil
}

func setEtcHostsInIgnition(role models.HostRole, path string, workDir string, content string) error {
	config, err := parseIgnitionFile(path)
	if err != nil {
		return err
	}

	setFileInIgnition(config, "/etc/hosts", dataurl.EncodeBytes([]byte(content)), true, 420)

	fileName := fmt.Sprintf("%s.ign", role)
	err = writeIgnitionFile(filepath.Join(workDir, fileName), config)
	if err != nil {
		return err
	}
	return nil
}

func GetServiceIPHostnames(serviceIPs string) string {
	ips := strings.Split(strings.TrimSpace(serviceIPs), ",")
	content := ""
	for _, ip := range ips {
		if ip != "" {
			content = content + fmt.Sprintf(ip+" assisted-api.local.openshift.io\n")
		}
	}
	return content
}

func (g *installerGenerator) runCreateCommand(installerPath, command string, envVars []string) error {
	cmd := exec.Command(installerPath, "create", command, "--dir", g.workDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = envVars
	err := cmd.Run()
	if err != nil {
		g.log.Errorf("error running openshift-install create %s", command)
		g.log.Error(out.String())
		return err
	}
	return nil
}

func (g *installerGenerator) downloadManifest(ctx context.Context, manifest string) error {
	respBody, _, err := g.s3Client.Download(ctx, manifest)
	if err != nil {
		return err
	}
	content, err := ioutil.ReadAll(respBody)
	if err != nil {
		return err
	}
	// manifest has full path as object-key on s3: clusterID/manifests/[manifests|openshift]/filename
	// clusterID/manifests should be trimmed
	prefix := manifests.GetManifestObjectName(*g.cluster.ID, "")
	targetPath := filepath.Join(g.workDir, strings.TrimPrefix(manifest, prefix))
	err = ioutil.WriteFile(targetPath, content, 0600)
	if err != nil {
		return err
	}
	return nil
}

func SetHostnameForNodeIgnition(ignition []byte, host *models.Host) ([]byte, error) {
	config, err := ParseTo32(ignition)
	if err != nil {
		return nil, errors.Errorf("error parsing ignition: %v", err)
	}

	hostname, err := hostutil.GetCurrentHostName(host)
	if err != nil {
		return nil, errors.Errorf("failed to get hostname for host %s", host.ID)
	}

	setFileInIgnition(config, "/etc/hostname", fmt.Sprintf("data:,%s", hostname), false, 420)

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return configBytes, nil
}
