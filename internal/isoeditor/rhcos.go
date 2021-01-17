package isoeditor

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/openshift/assisted-service/restapi/operations/bootfiles"
	"github.com/sirupsen/logrus"
)

const (
	// BaseIsoTempDir is a temporary directory pattern for the extracted base ISO
	BaseIsoTempDir string = "baseiso"
)

type Editor interface {
	CreateMinimalISOTemplate(serviceBaseURL string) (string, error)
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

	if err := e.fixTemplateConfigs(serviceBaseURL); err != nil {
		e.log.WithError(err).Warnf("Failed to edit template configs")
		return "", err
	}

	isoPath, err := tempFileName()
	if err != nil {
		return "", err
	}

	e.log.Infof("Creating minimal ISO template: %s", isoPath)
	if err := e.create(isoPath); err != nil {
		return "", err
	}

	return isoPath, nil
}

func (e *rhcosEditor) create(outPath string) error {
	volumeID, err := e.isoHandler.VolumeIdentifier()
	if err != nil {
		return err
	}
	if err = e.isoHandler.Create(outPath, volumeID); err != nil {
		return err
	}

	return nil
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
