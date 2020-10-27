package ignition

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_31_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/coreos/ignition/v2/config/validate"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/manifests"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
)

var fileNames = [...]string{
	"bootstrap.ign",
	"master.ign",
	"metadata.json",
	"worker.ign",
	"kubeconfig-noingress",
	"kubeadmin-password",
	"install-config.yaml",
}

// Generator can generate ignition files and upload them to an S3-like service
type Generator interface {
	Generate(ctx context.Context, installConfig []byte) error
	UploadToS3(ctx context.Context) error
	UpdateEtcHosts(string) error
}

type installerGenerator struct {
	log                     logrus.FieldLogger
	workDir                 string
	cluster                 *common.Cluster
	releaseImage            string
	installerDir            string
	serviceCACert           string
	encodedDhcpFileContents string
	s3Client                s3wrapper.API
}

// NewGenerator returns a generator that can generate ignition files
func NewGenerator(workDir string, installerDir string, cluster *common.Cluster, releaseImage string,
	serviceCACert string, s3Client s3wrapper.API, log logrus.FieldLogger) Generator {
	return &installerGenerator{
		cluster:       cluster,
		log:           log,
		releaseImage:  releaseImage,
		workDir:       workDir,
		installerDir:  installerDir,
		serviceCACert: serviceCACert,
		s3Client:      s3Client,
	}
}

// UploadToS3 uploads generated ignition and related files to the configured
// S3-compatible storage
func (g *installerGenerator) UploadToS3(ctx context.Context) error {
	return uploadToS3(ctx, g.workDir, g.cluster.ID.String(), g.s3Client, g.log)
}

// Generate generates ignition files and applies modifications.
func (g *installerGenerator) Generate(ctx context.Context, installConfig []byte) error {
	installerPath, err := installercache.Get(g.releaseImage, g.installerDir, g.cluster.PullSecret, g.log)
	if err != nil {
		return err
	}
	installConfigPath := filepath.Join(g.workDir, "install-config.yaml")

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

	err = g.runCreateCommand(installerPath, "ignition-configs", envVars)
	if err != nil {
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

func bmhIsMaster(bmh *bmh_v1alpha1.BareMetalHost) bool {
	return strings.Contains(bmh.Name, "-master-")
}

// updateBootstrap adds a status annotation to each BareMetalHost defined in the
// bootstrap ignition file
func (g *installerGenerator) updateBootstrap(bootstrapPath string) error {
	config, err := parseIgnitionFile(bootstrapPath)
	if err != nil {
		g.log.Error(err)
		return err
	}

	newFiles := []config_31_types.File{}

	masters, workers := sortHosts(g.cluster.Hosts)
	for i, file := range config.Storage.Files {
		switch {
		case isBaremetalProvisioningConfig(&config.Storage.Files[i]):
			// drop this from the list of Files because we don't want to run BMO
			continue
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

	err = writeIgnitionFile(bootstrapPath, config)
	if err != nil {
		g.log.Error(err)
		return err
	}
	g.log.Infof("Updated file %s", bootstrapPath)

	return nil
}

func isBMHFile(file *config_31_types.File) bool {
	return strings.Contains(file.Node.Path, "openshift-cluster-api_hosts")
}

func isMOTD(file *config_31_types.File) bool {
	return file.Node.Path == "/etc/motd"
}

func isBaremetalProvisioningConfig(file *config_31_types.File) bool {
	return strings.Contains(file.Node.Path, "baremetal-provisioning-config")
}

func fileToBMH(file *config_31_types.File) (*bmh_v1alpha1.BareMetalHost, error) {
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
func (g *installerGenerator) fixMOTDFile(file *config_31_types.File) {
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
func (g *installerGenerator) modifyBMHFile(file *config_31_types.File, bmh *bmh_v1alpha1.BareMetalHost, host *models.Host) error {
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
			hw.NIC[i].IP = iface.IPV4Addresses[0]
		case len(iface.IPV6Addresses) > 0:
			hw.NIC[i].IP = iface.IPV6Addresses[0]
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
	path := filepath.Join(g.workDir, "master.ign")
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

func (g *installerGenerator) updateIgnitions() error {
	masterPath := filepath.Join(g.workDir, "master.ign")
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

	workerPath := filepath.Join(g.workDir, "worker.ign")
	if caCertFile != "" {
		err := setCACertInIgnition(models.HostRoleWorker, workerPath, g.workDir, caCertFile)
		if err != nil {
			return errors.Wrapf(err, "error adding CA cert to ignition %s", workerPath)
		}
	}

	return nil
}

func (g *installerGenerator) UpdateEtcHosts(serviceIPs string) error {
	masterPath := filepath.Join(g.workDir, "master.ign")

	if serviceIPs != "" {
		err := setEtcHostsInIgnition(models.HostRoleMaster, masterPath, g.workDir, GetServiceIPHostnames(serviceIPs))
		if err != nil {
			return errors.Wrapf(err, "error adding Etc Hosts to ignition %s", masterPath)
		}
	}

	workerPath := filepath.Join(g.workDir, "worker.ign")
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
func uploadToS3(ctx context.Context, workDir string, clusterID string, s3Client s3wrapper.API, log logrus.FieldLogger) error {
	for _, fileName := range fileNames {
		fullPath := filepath.Join(workDir, fileName)
		key := filepath.Join(clusterID, fileName)
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

func parseIgnitionFile(path string) (*config_31_types.Config, error) {
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Errorf("error reading file %s: %v", path, err)
	}

	config, _, err := config_31.Parse(configBytes)
	if err != nil {
		return nil, errors.Errorf("error parsing ignition: %v", err)
	}

	return &config, nil
}

// writeIgnitionFile writes an ignition config to a given path on disk
func writeIgnitionFile(path string, config *config_31_types.Config) error {
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

func setFileInIgnition(config *config_31_types.Config, filePath string, fileContents string, appendContent bool, mode int) {
	rootUser := "root"
	file := config_31_types.File{
		Node: config_31_types.Node{
			Path:      filePath,
			Overwrite: nil,
			Group:     config_31_types.NodeGroup{},
			User:      config_31_types.NodeUser{Name: &rootUser},
		},
		FileEmbedded1: config_31_types.FileEmbedded1{
			Append: []config_31_types.Resource{},
			Contents: config_31_types.Resource{
				Source: &fileContents,
			},
			Mode: &mode,
		},
	}
	if appendContent {
		file.FileEmbedded1.Append = []config_31_types.Resource{
			{
				Source: &fileContents,
			},
		}
		file.FileEmbedded1.Contents = config_31_types.Resource{}
	}
	config.Storage.Files = append(config.Storage.Files, file)
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

func MergeIgnitionConfig(base []byte, overrides []byte) (string, error) {
	baseConfig, report, err := config_31.Parse(base)
	if err != nil {
		return "", err
	}
	if report.IsFatal() {
		return "", errors.Errorf("base ignition config is invalid: %s", report.String())
	}

	overrideConfig, report, err := config_31.Parse(overrides)
	if err != nil {
		return "", err
	}
	if report.IsFatal() {
		return "", errors.Errorf("override ignition config is invalid: %s", report.String())
	}

	config := config_31.Merge(baseConfig, overrideConfig)
	report = validate.ValidateWithContext(config, nil)
	if report.IsFatal() {
		return "", errors.Errorf("merged ignition config is invalid: %s", report.String())
	}

	res, err := json.Marshal(config)
	if err != nil {
		return "", err
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
