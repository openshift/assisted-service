package isoeditor

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"text/template"

	"github.com/cavaliercoder/go-cpio"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/restapi/operations/bootfiles"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const rootfsServiceConfigFormat = `[Service]
Environment=http_proxy={{.HTTP_PROXY}}
Environment=https_proxy={{.HTTPS_PROXY}}
Environment=no_proxy={{.NO_PROXY}}`

type ClusterProxyInfo struct {
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
}

//go:generate mockgen -package=isoeditor -destination=mock_editor.go -self_package=github.com/openshift/assisted-service/internal/isoeditor . Editor
type Editor interface {
	CreateMinimalISOTemplate(serviceBaseURL string) (string, error)
	CreateClusterMinimalISO(ignition string, staticIPConfig string, clusterProxyInfo *ClusterProxyInfo) (string, error)
}

type rhcosEditor struct {
	isoHandler       isoutil.Handler
	openshiftVersion string
	log              logrus.FieldLogger
}

func (e *rhcosEditor) getRootFSURL(serviceBaseURL string) string {
	var downloadBootFilesURL = &bootfiles.DownloadBootFilesURL{
		FileType:         "rootfs.img",
		OpenshiftVersion: e.openshiftVersion,
	}
	url, err := downloadBootFilesURL.Build()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%s%s", serviceBaseURL, url.RequestURI())
}

// Creates the template minimal iso by removing the rootfs and adding the url
// Returns the path to the created iso file
func (e *rhcosEditor) CreateMinimalISOTemplate(serviceBaseURL string) (string, error) {
	if err := e.isoHandler.Extract(); err != nil {
		return "", err
	}
	defer func() {
		if err := e.isoHandler.CleanWorkDir(); err != nil {
			e.log.WithError(err).Warnf("Failed to clean isoHandler work dir")
		}
	}()

	if err := os.Remove(e.isoHandler.ExtractedPath("images/pxeboot/rootfs.img")); err != nil {
		return "", err
	}

	if err := e.fixTemplateConfigs(serviceBaseURL); err != nil {
		e.log.WithError(err).Warnf("Failed to edit template configs")
		return "", err
	}

	e.log.Info("Creating minimal ISO template")
	return e.create()
}

func (e *rhcosEditor) CreateClusterMinimalISO(ignition string, staticIPConfig string, clusterProxyInfo *ClusterProxyInfo) (string, error) {
	if err := e.isoHandler.Extract(); err != nil {
		return "", errors.Wrap(err, "failed to extract iso")
	}
	defer func() {
		if err := e.isoHandler.CleanWorkDir(); err != nil {
			e.log.WithError(err).Warnf("Failed to clean isoHandler work dir")
		}
	}()

	if err := e.addIgnitionArchive(ignition); err != nil {
		return "", errors.Wrap(err, "failed to add ignition archive")
	}

	if staticIPConfig != "" || clusterProxyInfo.HTTPProxy != "" || clusterProxyInfo.HTTPSProxy != "" {
		if err := e.addCustomRAMDisk(staticIPConfig, clusterProxyInfo); err != nil {
			return "", errors.Wrap(err, "failed to add additional ramdisk")
		}
	}

	return e.create()
}

func (e *rhcosEditor) addIgnitionArchive(ignition string) error {
	archiveBytes, err := IgnitionImageArchive(ignition)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(e.isoHandler.ExtractedPath("images/ignition.img"), archiveBytes, 0644) //nolint:gosec
}

func (e *rhcosEditor) addCustomRAMDisk(staticIPConfig string, clusterProxyInfo *ClusterProxyInfo) error {
	imagePath := "/images/assisted_installer_custom.img"
	f, err := os.Create(e.isoHandler.ExtractedPath(imagePath))
	if err != nil {
		return err
	}

	w := cpio.NewWriter(f)
	if staticIPConfig != "" {
		configPath := "/etc/static_ips_config.csv"
		scriptPath := "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-static-ip-config.sh"
		scriptContent := constants.ConfigStaticIpsScript

		if err = addFileToArchive(w, configPath, staticIPConfig, 0o664); err != nil {
			return err
		}
		if err = addFileToArchive(w, scriptPath, scriptContent, 0o755); err != nil {
			return err
		}
	}
	if clusterProxyInfo.HTTPProxy != "" || clusterProxyInfo.HTTPSProxy != "" {
		rootfsServiceConfigPath := "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf"
		rootfsServiceConfig, err1 := e.formatRootfsServiceConfigFile(clusterProxyInfo)
		if err1 != nil {
			return err1
		}
		if err = addFileToArchive(w, rootfsServiceConfigPath, rootfsServiceConfig, 0o664); err != nil {
			return err
		}
	}
	if err = w.Close(); err != nil {
		return err
	}

	// edit config to add new image to initrd
	err = editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+initrd) (.+| )+$`, fmt.Sprintf("$1 $2 %s", imagePath))
	if err != nil {
		return err
	}
	return editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append.*initrd=\S+) (.*)$`, fmt.Sprintf("${1},%s ${2}", imagePath))
}

func (e *rhcosEditor) formatRootfsServiceConfigFile(clusterProxyInfo *ClusterProxyInfo) (string, error) {
	var rootfsServicConfigParams = map[string]string{
		"HTTP_PROXY":  clusterProxyInfo.HTTPProxy,
		"HTTPS_PROXY": clusterProxyInfo.HTTPSProxy,
		"NO_PROXY":    clusterProxyInfo.NoProxy,
	}
	tmpl, err := template.New("rootfsServiceConfig").Parse(rootfsServiceConfigFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, rootfsServicConfigParams); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func addFileToArchive(w *cpio.Writer, path string, content string, mode cpio.FileMode) error {
	// add all the directories in path
	for dir := filepath.Dir(path); dir != "" && dir != "/"; dir = filepath.Dir(dir) {
		hdr := &cpio.Header{
			Name: dir,
			Mode: 040755,
			Size: 0,
		}
		if err := w.WriteHeader(hdr); err != nil {
			return err
		}
	}

	// add the file content
	hdr := &cpio.Header{
		Name: path,
		Mode: mode,
		Size: int64(len(content)),
	}
	if err := w.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}

	return nil
}

func (e *rhcosEditor) create() (string, error) {
	isoPath, err := tempFileName()
	if err != nil {
		return "", err
	}

	volumeID, err := e.isoHandler.VolumeIdentifier()
	if err != nil {
		return "", err
	}
	if err = e.isoHandler.Create(isoPath, volumeID); err != nil {
		return "", err
	}

	return isoPath, nil
}

func (e *rhcosEditor) fixTemplateConfigs(serviceBaseURL string) error {
	// Add the rootfs url
	replacement := fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", e.getRootFSURL(serviceBaseURL))
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+linux) (.+| )+$`, replacement); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append) (.+| )+$`, replacement); err != nil {
		return err
	}

	// Remove the coreos.liveiso parameter
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), ` coreos.liveiso=\S+`, ""); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), ` coreos.liveiso=\S+`, ""); err != nil {
		return err
	}

	return nil
}

func editFile(fileName string, reString string, replacement string) error {
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(reString)
	newContent := re.ReplaceAllString(string(content), replacement)

	if err := ioutil.WriteFile(fileName, []byte(newContent), 0600); err != nil {
		return err
	}

	return nil
}

func tempFileName() (string, error) {
	f, err := ioutil.TempFile("", "isoeditor")
	if err != nil {
		return "", err
	}
	path := f.Name()

	if err := os.Remove(path); err != nil {
		return "", err
	}

	return path, nil
}

func IgnitionImageArchive(ignitionConfig string) ([]byte, error) {
	ignitionBytes := []byte(ignitionConfig)

	// Create CPIO archive
	archiveBuffer := new(bytes.Buffer)
	cpioWriter := cpio.NewWriter(archiveBuffer)
	if err := cpioWriter.WriteHeader(&cpio.Header{Name: "config.ign", Mode: 0o100_644, Size: int64(len(ignitionBytes))}); err != nil {
		return nil, errors.Wrap(err, "Failed to write CPIO header")
	}
	if _, err := cpioWriter.Write(ignitionBytes); err != nil {

		return nil, errors.Wrap(err, "Failed to write CPIO archive")
	}
	if err := cpioWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to close CPIO archive")
	}

	// Run gzip compression
	compressedBuffer := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(compressedBuffer)
	if _, err := gzipWriter.Write(archiveBuffer.Bytes()); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip ignition config")
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip ignition config")
	}

	return compressedBuffer.Bytes(), nil
}
