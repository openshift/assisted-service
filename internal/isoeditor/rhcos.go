package isoeditor

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/restapi/operations/bootfiles"

	"github.com/cavaliercoder/go-cpio"
	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_31_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"github.com/vincent-petithory/dataurl"
)

const (
	// BaseIsoTempDir is a temporary directory pattern for the extracted base ISO
	BaseIsoTempDir string = "baseiso"
)

type Editor interface {
	CreateMinimalISOTemplate(serviceBaseURL string) (string, error)
	CreateClusterMinimalISO(ignition string) (string, error)
}

type rhcosEditor struct {
	isoHandler       isoutil.Handler
	openshiftVersion string
	log              logrus.FieldLogger
}

func NewEditor(isoHandler isoutil.Handler, openshiftVersion string, log logrus.FieldLogger) Editor {
	return &rhcosEditor{
		isoHandler:       isoHandler,
		openshiftVersion: openshiftVersion,
		log:              log,
	}
}

func CreateEditor(isoPath string, openshiftVersion string, log logrus.FieldLogger) Editor {
	isoTmpWorkDir, err := ioutil.TempDir("", BaseIsoTempDir)
	if err != nil {
		return nil
	}
	isoHandler := isoutil.NewHandler(isoPath, isoTmpWorkDir)
	return NewEditor(isoHandler, openshiftVersion, log)
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

	if err := e.addRootFSURL(serviceBaseURL); err != nil {
		e.log.WithError(err).Warnf("Can't add rootfs_url (missing cfg files)")
		return "", err
	}

	e.log.Info("Creating minimal ISO template")
	return e.create()
}

// CreateClusterMinimalISO creates a new rhcos iso with cluser file customizations added
// to the initrd image
func (e *rhcosEditor) CreateClusterMinimalISO(ignition string) (string, error) {
	if err := e.isoHandler.Extract(); err != nil {
		return "", err
	}
	defer func() {
		if err := e.isoHandler.CleanWorkDir(); err != nil {
			e.log.WithError(err).Warnf("Failed to clean isoHandler work dir")
		}
	}()

	if err := e.addIgnitionFiles(ignition); err != nil {
		return "", err
	}

	if err := e.addIgnitionArchive(ignition); err != nil {
		return "", err
	}

	return e.create()
}

func (e *rhcosEditor) addIgnitionArchive(ignition string) error {
	archiveBytes, err := IgnitionImageArchive(ignition)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(e.isoHandler.ExtractedPath("images/ignition.img"), archiveBytes, 0644)
}

func addFile(w *cpio.Writer, f config_31_types.File) error {
	u, err := dataurl.DecodeString(f.Contents.Key())
	if err != nil {
		return err
	}

	var mode cpio.FileMode = 0644
	if f.Mode != nil {
		mode = cpio.FileMode(*f.Mode)
	}

	uid := 0
	if f.User.ID != nil {
		uid = *f.User.ID
	}

	gid := 0
	if f.Group.ID != nil {
		gid = *f.Group.ID
	}

	// add the file
	hdr := &cpio.Header{
		Name: f.Path,
		Mode: mode,
		UID:  uid,
		GID:  gid,
		Size: int64(len(u.Data)),
	}
	if err := w.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := w.Write(u.Data); err != nil {
		return err
	}

	return nil
}

// addIgnitionFiles adds all files referenced in the given ignition config to
// the initrd by creating an additional cpio archive
func (e *rhcosEditor) addIgnitionFiles(ignition string) error {
	config, _, err := config_31.Parse([]byte(ignition))
	if err != nil {
		return err
	}

	f, err := os.Create(e.isoHandler.ExtractedPath("images/assisted_custom_files.img"))
	if err != nil {
		return fmt.Errorf("failed to open image file: %s", err)
	}

	w := cpio.NewWriter(f)
	addedPaths := make([]string, 0)

	// TODO: deal with config.Storage.Directories also?
	for _, f := range config.Storage.Files {
		if err = addFile(w, f); err != nil {
			return fmt.Errorf("failed to add file %s to archive: %v", f.Path, err)
		}

		// Need to add all directories in the file path to ensure it can be created
		// Many files may be in the same directory so we need to track which directories we add to ensure we only add them once
		for dir := filepath.Dir(f.Path); dir != "" && dir != "/"; dir = filepath.Dir(dir) {
			if !funk.Contains(addedPaths, dir) {
				hdr := &cpio.Header{
					Name: dir,
					Mode: 040755,
					Size: 0,
				}
				if err = w.WriteHeader(hdr); err != nil {
					return err
				}
				addedPaths = append(addedPaths, dir)
			}
		}
	}

	if err = w.Close(); err != nil {
		return err
	}

	err = editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), ` coreos.liveiso=\S+`, "")
	if err != nil {
		return err
	}

	err = editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), ` coreos.liveiso=\S+`, "")
	if err != nil {
		return err
	}

	// edit configs to add new image
	err = editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+initrd) (.+| )+$`, "$1 $2 /images/assisted_custom_files.img")
	if err != nil {
		return err
	}
	return editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append.*initrd=\S+) (.*)$`, "${1},/images/assisted_custom_files.img ${2}")
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

func (e *rhcosEditor) addRootFSURL(serviceBaseURL string) error {
	replacement := fmt.Sprintf("$1 $2 coreos.live.rootfs_url=%s", e.getRootFSURL(serviceBaseURL))
	if err := editFile(e.isoHandler.ExtractedPath("EFI/redhat/grub.cfg"), `(?m)^(\s+linux) (.+| )+$`, replacement); err != nil {
		return err
	}
	if err := editFile(e.isoHandler.ExtractedPath("isolinux/isolinux.cfg"), `(?m)^(\s+append) (.+| )+$`, replacement); err != nil {
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

	if err := ioutil.WriteFile(fileName, []byte(newContent), 0644); err != nil {
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
