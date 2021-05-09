package isoeditor

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"text/template"

	"github.com/cavaliercoder/go-cpio"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/restapi/operations/bootfiles"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const rootfsServiceConfigFormat = `[Service]
Environment=http_proxy={{.HTTP_PROXY}}
Environment=https_proxy={{.HTTPS_PROXY}}
Environment=no_proxy={{.NO_PROXY}}
Environment=HTTP_PROXY={{.HTTP_PROXY}}
Environment=HTTPS_PROXY={{.HTTPS_PROXY}}
Environment=NO_PROXY={{.NO_PROXY}}`

const (
	RamDiskPaddingLength  = uint64(1024 * 1024) // 1MB
	IgnitionPaddingLength = uint64(256 * 1024)  // 256KB
	ignitionImagePath     = "/images/ignition.img"
	ramDiskImagePath      = "/images/assisted_installer_custom.img"
	ignitionHeaderKey     = "coreiso+"
	ramdiskHeaderKey      = "ramdisk+"
	isoSystemAreaSize     = 32768
	ignitionHeaderSize    = 24
	headerLength          = int64(32768) // first 32KB in ISO
)

type ClusterProxyInfo struct {
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
}

type OffsetInfo struct {
	Key    [8]byte
	Offset uint64
	Length uint64
}

//go:generate mockgen -package=isoeditor -destination=mock_editor.go -self_package=github.com/openshift/assisted-service/internal/isoeditor . Editor
type Editor interface {
	CreateMinimalISOTemplate(serviceBaseURL string) (string, error)
	CreateClusterMinimalISO(ignition string, staticNetworkConfig string, clusterProxyInfo *ClusterProxyInfo) (string, error)
}

type rhcosEditor struct {
	isoHandler          isoutil.Handler
	openshiftVersion    string
	log                 logrus.FieldLogger
	workDir             string
	staticNetworkConfig staticnetworkconfig.StaticNetworkConfig
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

	if err := os.Remove(e.isoHandler.ExtractedPath("images/pxeboot/rootfs.img")); err != nil {
		return "", err
	}

	if err := e.embedInitrdPlaceholders(); err != nil {
		e.log.WithError(err).Warnf("Failed to embed initrd placeholders")
		return "", err
	}

	if err := e.fixTemplateConfigs(serviceBaseURL); err != nil {
		e.log.WithError(err).Warnf("Failed to edit template configs")
		return "", err
	}

	e.log.Info("Creating minimal ISO template")
	isoPath, err := e.create()
	if err != nil {
		e.log.WithError(err).Errorf("Failed to minimal create ISO template")
		return "", err
	}

	if err := e.embedOffsetsInSystemArea(isoPath); err != nil {
		e.log.WithError(err).Errorf("Failed to embed offsets in ISO system area")
		return "", err
	}

	return isoPath, nil
}

func (e *rhcosEditor) CreateClusterMinimalISO(ignition string, staticNetworkConfig string, clusterProxyInfo *ClusterProxyInfo) (string, error) {
	clusterISOPath, err := tempFileName(e.workDir)
	if err != nil {
		return "", err
	}

	if err = e.isoHandler.Copy(clusterISOPath); err != nil {
		return "", errors.Wrap(err, "failed to copy iso")
	}

	ignitionOffsetInfo, ramDiskOffsetInfo, err := readHeader(clusterISOPath)
	if err != nil {
		return "", err
	}

	if err := e.addIgnitionArchive(clusterISOPath, ignition, ignitionOffsetInfo.Offset); err != nil {
		return "", errors.Wrap(err, "failed to add ignition archive")
	}

	if staticNetworkConfig != "" || clusterProxyInfo.HTTPProxy != "" || clusterProxyInfo.HTTPSProxy != "" {
		if err := e.addCustomRAMDisk(clusterISOPath, staticNetworkConfig, clusterProxyInfo, ramDiskOffsetInfo); err != nil {
			return "", errors.Wrap(err, "failed to add additional ramdisk")
		}
	}

	if err := e.isoHandler.CleanWorkDir(); err != nil {
		e.log.WithError(err).Warnf("Failed to clean isoHandler work dir")
	}

	return clusterISOPath, nil
}

func (e *rhcosEditor) embedInitrdPlaceholders() error {
	// Create ramdisk image placeholder
	if err := e.createImagePlaceholder(ramDiskImagePath, RamDiskPaddingLength); err != nil {
		return errors.Wrap(err, "Failed to create placeholder for custom ramdisk image")
	}

	return nil
}

func (e *rhcosEditor) embedOffsetsInSystemArea(isoPath string) error {
	ignitionOffset, err := isoutil.GetFileLocation(ignitionImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ignition image offset")
	}

	ramDiskOffset, err := isoutil.GetFileLocation(ramDiskImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ram disk image offset")
	}

	ignitionSize, err := isoutil.GetFileSize(ignitionImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ignition image size")
	}

	ramDiskSize, err := isoutil.GetFileSize(ramDiskImagePath, isoPath)
	if err != nil {
		return errors.Wrap(err, "Failed to get ram disk image size")
	}

	var ignitionOffsetInfo OffsetInfo
	copy(ignitionOffsetInfo.Key[:], ignitionHeaderKey)
	ignitionOffsetInfo.Offset = ignitionOffset
	ignitionOffsetInfo.Length = ignitionSize

	var ramDiskOffsetInfo OffsetInfo
	copy(ramDiskOffsetInfo.Key[:], ramdiskHeaderKey)
	ramDiskOffsetInfo.Offset = ramDiskOffset
	ramDiskOffsetInfo.Length = ramDiskSize

	return writeHeader(&ignitionOffsetInfo, &ramDiskOffsetInfo, isoPath)
}

func (e *rhcosEditor) createImagePlaceholder(imagePath string, paddingLength uint64) error {
	f, err := os.Create(e.isoHandler.ExtractedPath(imagePath))
	if err != nil {
		return err
	}
	defer f.Close()

	err = f.Truncate(int64(paddingLength))
	if err != nil {
		return err
	}

	return nil
}

func (e *rhcosEditor) addIgnitionArchive(clusterISOPath, ignition string, ignitionOffset uint64) error {
	archiveBytes, err := IgnitionImageArchive(ignition)
	if err != nil {
		return err
	}

	return writeAt(archiveBytes, int64(ignitionOffset), clusterISOPath)
}

func (e *rhcosEditor) addCustomRAMDisk(clusterISOPath, staticNetworkConfig string, clusterProxyInfo *ClusterProxyInfo, ramdiskOffsetInfo *OffsetInfo) error {
	buffer := new(bytes.Buffer)
	w := cpio.NewWriter(buffer)
	if staticNetworkConfig != "" {
		filesList, newErr := e.staticNetworkConfig.GenerateStaticNetworkConfigData(staticNetworkConfig)
		if newErr != nil {
			return newErr
		}
		for _, file := range filesList {
			err := addFileToArchive(w, filepath.Join("/etc/assisted/network", file.FilePath), file.FileContents, 0o600)
			if err != nil {
				return err
			}
		}
		scriptPath := "/usr/lib/dracut/hooks/initqueue/settled/90-assisted-pre-static-network-config.sh"
		scriptContent := constants.PreNetworkConfigScript

		if err := addFileToArchive(w, scriptPath, scriptContent, 0o755); err != nil {
			return err
		}
	}
	if clusterProxyInfo.HTTPProxy != "" || clusterProxyInfo.HTTPSProxy != "" {
		rootfsServiceConfigPath := "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf"
		rootfsServiceConfig, err := e.formatRootfsServiceConfigFile(clusterProxyInfo)
		if err != nil {
			return err
		}
		if err := addFileToArchive(w, rootfsServiceConfigPath, rootfsServiceConfig, 0o664); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}

	// Compress custom RAM disk
	compressedArchive, err := getCompressedArchive(buffer)
	if err != nil {
		return err
	}

	// Ensures RAM placeholder is large enough to accommodate the compressed archive
	if uint64(len(compressedArchive)) > ramdiskOffsetInfo.Length {
		return errors.Errorf("Custom RAM disk is larger than the placeholder in ISO (%d bytes > %d bytes)",
			len(compressedArchive), RamDiskPaddingLength)
	}

	return writeAt(compressedArchive, int64(ramdiskOffsetInfo.Offset), clusterISOPath)
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
	// add all the directories in path in the correct order, using dirsStack as FILO
	dirsStack := []string{}
	for dir := filepath.Dir(path); dir != "" && dir != "/"; dir = filepath.Dir(dir) {
		dirsStack = append(dirsStack, dir)
	}
	for i := len(dirsStack) - 1; i >= 0; i-- {
		hdr := &cpio.Header{
			Name: dirsStack[i],
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
	isoPath, err := tempFileName(e.workDir)
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
	rootFSURL := e.getRootFSURL(serviceBaseURL)
	replacement := fmt.Sprintf("$1 $2 'coreos.live.rootfs_url=%s'", rootFSURL)
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+linux) (.+| )+$`, replacement); err != nil {
		return err
	}
	replacement = fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", rootFSURL)
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

	// Edit config to add custom ramdisk image to initrd
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+initrd) (.+| )+$`, fmt.Sprintf("$1 $2 %s", ramDiskImagePath)); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append.*initrd=\S+) (.*)$`, fmt.Sprintf("${1},%s ${2}", ramDiskImagePath)); err != nil {
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

func tempFileName(baseDir string) (string, error) {
	f, err := ioutil.TempFile(baseDir, "isoeditor")
	if err != nil {
		return "", err
	}
	path := f.Name()

	if err := os.Remove(path); err != nil {
		return "", err
	}

	return path, nil
}

func writeAt(b []byte, offset int64, clusterISOPath string) error {
	iso, err := os.OpenFile(clusterISOPath, os.O_WRONLY, 0o664)
	if err != nil {
		return err
	}
	defer iso.Close()

	_, err = iso.WriteAt(b, offset)
	if err != nil {
		return err
	}

	return nil
}

// IgnitionImageArchive takes an ignitionConfig and returns a gzipped CPIO
// archive (in bytes) or err on failure.
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

	return getCompressedArchive(archiveBuffer)
}

func getCompressedArchive(archiveBuffer *bytes.Buffer) ([]byte, error) {
	// Run gzip compression
	compressedBuffer := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(compressedBuffer)
	if _, err := gzipWriter.Write(archiveBuffer.Bytes()); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}

	return compressedBuffer.Bytes(), nil
}

func readHeader(isoPath string) (*OffsetInfo, *OffsetInfo, error) {
	iso, err := os.OpenFile(isoPath, os.O_RDONLY, 0o664)
	if err != nil {
		return nil, nil, err
	}
	defer iso.Close()

	// Read 48 bytes with offsets metadata from the end of system area
	ignitionMetadata := make([]byte, 24)
	_, err = iso.ReadAt(ignitionMetadata, headerLength-24)
	if err != nil {
		return nil, nil, err
	}
	ramdiskMetadata := make([]byte, 24)
	_, err = iso.ReadAt(ramdiskMetadata, headerLength-48)
	if err != nil {
		return nil, nil, err
	}

	ignitionOffsetInfo, err := GetIgnitionArea(ignitionMetadata)
	if err != nil {
		return nil, nil, err
	}
	ramdiskOffsetInfo, err := GetRamDiskArea(ramdiskMetadata)
	if err != nil {
		return nil, nil, err
	}

	return ignitionOffsetInfo, ramdiskOffsetInfo, nil
}

// Writing the offsets of initrd images in the end of system area (first 32KB).
// As the ISO template is generated by us, we know that this area should be empty.
func writeHeader(ignitionOffsetInfo, ramDiskOffsetInfo *OffsetInfo, isoPath string) error {
	iso, err := os.OpenFile(isoPath, os.O_WRONLY, 0o664)
	if err != nil {
		return err
	}
	defer iso.Close()

	// Starting to write from the end of the system area in order to easily support
	// additional offsets (and as done in coreos-assembler/src/cmd-buildextend-live)
	headerEndOffset := int64(32768)

	// Write ignition config
	writtenBytesLength, err := writeOffsetInfo(headerEndOffset, ignitionOffsetInfo, iso)
	if err != nil {
		return err
	}

	// Write ram disk
	_, err = writeOffsetInfo(headerEndOffset-writtenBytesLength, ramDiskOffsetInfo, iso)
	if err != nil {
		return err
	}

	return nil
}

func writeOffsetInfo(headerOffset int64, offsetInfo *OffsetInfo, iso *os.File) (int64, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, offsetInfo)
	if err != nil {
		return 0, err
	}

	bytesLength := int64(buf.Len())
	headerOffset = headerOffset - bytesLength
	_, err = iso.Seek(headerOffset, io.SeekStart)
	if err != nil {
		return 0, err
	}
	_, err = iso.Write(buf.Bytes())
	if err != nil {
		return 0, err
	}

	return bytesLength, nil
}

func validateOffsetInfoKey(offsetInfo *OffsetInfo, key string) error {
	if string(offsetInfo.Key[:]) != key {
		return errors.Errorf("Invalid key in offset metadata: %s", key)
	}
	return nil
}

func GetIgnitionArea(offsetMetadata []byte) (*OffsetInfo, error) {
	offsetInfo, err := ParseOffsetInfo(offsetMetadata)
	if err != nil {
		return nil, err
	}
	if err = validateOffsetInfoKey(offsetInfo, ignitionHeaderKey); err != nil {
		return nil, err
	}
	return offsetInfo, nil
}

func GetRamDiskArea(offsetMetadata []byte) (*OffsetInfo, error) {
	offsetInfo, err := ParseOffsetInfo(offsetMetadata)
	if err != nil {
		return nil, err
	}
	if err = validateOffsetInfoKey(offsetInfo, ramdiskHeaderKey); err != nil {
		return nil, err
	}
	return offsetInfo, nil
}

// ParseOffsetInfo gets a 24 bytes array with offset metadata and
// returns an OffsetInfo struct with the parsed data.
func ParseOffsetInfo(headerBytes []byte) (*OffsetInfo, error) {
	buf := bytes.NewReader(headerBytes)
	offsetInfo := new(OffsetInfo)
	err := binary.Read(buf, binary.LittleEndian, offsetInfo)
	if err != nil {
		return nil, err
	}
	return offsetInfo, nil
}

func EmbedIgnition(inputISOPath, outputISOPath, ignitionConfig string) error {
	coreosIgnitionHeader := make([]byte, ignitionHeaderSize)

	inputISO, err := os.Open(inputISOPath)
	if err != nil {
		return err
	}
	defer inputISO.Close()

	// Reading the last 24 bytes at the end of the system area)
	if _, err = inputISO.ReadAt(coreosIgnitionHeader, isoSystemAreaSize-ignitionHeaderSize); err != nil {
		return err
	}
	ignitionOffsetInfo, err := GetIgnitionArea(coreosIgnitionHeader)
	if err != nil {
		return err
	}

	cpio, err := IgnitionImageArchive(ignitionConfig)
	if err != nil {
		return err
	}

	if uint64(len(cpio)) > ignitionOffsetInfo.Length {
		return errors.Errorf("Compressed Ignition config is too large: %v > %v", len(cpio), int(ignitionOffsetInfo.Length))
	}

	resultISO, err := os.Create(outputISOPath)
	if err != nil {
		return err
	}
	defer resultISO.Close()

	if _, err = io.Copy(resultISO, inputISO); err != nil {
		return err
	}

	// clear out the embed area
	embedArea := make([]byte, ignitionOffsetInfo.Length)
	copy(embedArea, cpio)
	if _, err = resultISO.WriteAt(embedArea, int64(ignitionOffsetInfo.Offset)); err != nil {
		return err
	}

	return nil
}
