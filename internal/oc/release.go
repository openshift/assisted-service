package oc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
	"github.com/thedevsaddam/retry"
)

const (
	mcoImageName        = "machine-config-operator"
	mustGatherImageName = "must-gather"
	DefaultTries        = 5
	DefaltRetryDelay    = time.Second * 5
)

type Config struct {
	MaxTries   uint
	RetryDelay time.Duration
}

//go:generate mockgen -source=release.go -package=oc -destination=mock_release.go
type Release interface {
	GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMustGatherImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetOpenshiftVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMajorMinorVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, platformType models.PlatformType) (string, error)
}

type release struct {
	executer executer.Executer
	config   Config

	// A map for caching images (image name > release image URL > image)
	imagesMap map[string]map[string]string
}

func NewRelease(executer executer.Executer, config Config) Release {
	return &release{executer, config, make(map[string]map[string]string)}
}

const (
	templateGetImage   = "oc adm release info --image-for=%s --insecure=%t %s"
	templateGetVersion = "oc adm release info -o template --template '{{.metadata.version}}' --insecure=%t %s"
	templateExtract    = "oc adm release extract --command=%s --to=%s --insecure=%t %s"
)

// GetMCOImage gets mcoImage url from the releaseImageMirror if provided.
// Else gets it from the source releaseImage
func (r *release) GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, mcoImageName, releaseImage, releaseImageMirror, pullSecret)
}

// GetMustGatherImage gets must-gather image URL from the release image or releaseImageMirror, if provided.
func (r *release) GetMustGatherImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, mustGatherImageName, releaseImage, releaseImageMirror, pullSecret)
}

func (r *release) getImageByName(log logrus.FieldLogger, imageName, releaseImage, releaseImageMirror, pullSecret string) (string, error) {
	var image string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("neither releaseImage, nor releaseImageMirror are provided")
	}
	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		image, err = r.getImageFromRelease(log, imageName, releaseImageMirror, pullSecret, true)
		if err != nil {
			log.WithError(err).Errorf("failed to get %s image from mirror release image %s", imageName, releaseImageMirror)
			return "", err
		}
	} else {
		image, err = r.getImageFromRelease(log, imageName, releaseImage, pullSecret, false)
		if err != nil {
			log.WithError(err).Errorf("failed to get %s image from release image %s", imageName, releaseImage)
			return "", err
		}
	}
	return image, err
}

func (r *release) GetOpenshiftVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	var openshiftVersion string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage nor releaseImageMirror provided")
	}
	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		openshiftVersion, err = r.getOpenshiftVersionFromRelease(log, releaseImageMirror, pullSecret, true)
		if err != nil {
			log.WithError(err).Errorf("failed to get image openshift version from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		openshiftVersion, err = r.getOpenshiftVersionFromRelease(log, releaseImage, pullSecret, false)
		if err != nil {
			log.WithError(err).Errorf("failed to get image openshift version from release image %s", releaseImage)
			return "", err
		}
	}

	return openshiftVersion, err
}

func (r *release) GetMajorMinorVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	openshiftVersion, err := r.GetOpenshiftVersion(log, releaseImage, releaseImageMirror, pullSecret)
	if err != nil {
		return "", err
	}

	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

func (r *release) getImageFromRelease(log logrus.FieldLogger, imageName, releaseImage, pullSecret string, insecure bool) (string, error) {
	// Fetch image URL from cache
	if image, ok := r.imagesMap[imageName][releaseImage]; ok {
		return image, nil
	}

	cmd := fmt.Sprintf(templateGetImage, imageName, insecure, releaseImage)

	log.Infof("Fetching image from OCP release (%s)", cmd)
	image, err := execute(log, r.executer, pullSecret, cmd)
	if err != nil {
		return "", err
	}

	// Update image URL in cache
	r.imagesMap[imageName] = make(map[string]string)
	r.imagesMap[imageName][releaseImage] = image

	return image, nil
}

func (r *release) getOpenshiftVersionFromRelease(log logrus.FieldLogger, releaseImage string, pullSecret string, insecure bool) (string, error) {
	cmd := fmt.Sprintf(templateGetVersion, insecure, releaseImage)
	version, err := execute(log, r.executer, pullSecret, cmd)
	if err != nil {
		return "", err
	}
	// Trimming as output is retrieved wrapped with single quotes.
	return strings.Trim(version, "'"), nil
}

// Extract openshift-baremetal-install binary from releaseImageMirror if provided.
// Else extract from the source releaseImage
func (r *release) Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, platformType models.PlatformType) (string, error) {
	var path string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage or releaseImageMirror provided")
	}
	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		path, err = r.extractFromRelease(log, releaseImageMirror, cacheDir, pullSecret, true, platformType)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		path, err = r.extractFromRelease(log, releaseImage, cacheDir, pullSecret, false, platformType)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from release image %s", releaseImageMirror)
			return "", err
		}
	}
	return path, err
}

// extractFromRelease returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image.
func (r *release) extractFromRelease(log logrus.FieldLogger, releaseImage, cacheDir, pullSecret string, insecure bool, platformType models.PlatformType) (string, error) {
	var binary string
	if platformType == models.PlatformTypeNone {
		binary = "openshift-install"
	} else {
		binary = "openshift-baremetal-install"
	}

	workdir := filepath.Join(cacheDir, releaseImage)
	log.Infof("extracting %s binary to %s", binary, workdir)
	err := os.MkdirAll(workdir, 0755)
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf(templateExtract, binary, workdir, insecure, releaseImage)
	_, err = retry.Do(r.config.MaxTries, r.config.RetryDelay, execute, log, r.executer, pullSecret, cmd)
	if err != nil {
		return "", err
	}
	// set path
	path := filepath.Join(workdir, binary)
	log.Info("Successfully extracted $s binary from the release to: $s", binary, path)
	return path, nil
}

func execute(log logrus.FieldLogger, executer executer.Executer, pullSecret string, command string) (string, error) {
	// write pull secret to a temp file
	ps, err := executer.TempFile("", "registry-config")
	if err != nil {
		return "", err
	}
	defer func() {
		ps.Close()
		os.Remove(ps.Name())
	}()
	_, err = ps.Write([]byte(pullSecret))
	if err != nil {
		return "", err
	}
	// flush the buffer to ensure the file can be read
	ps.Close()
	executeCommand := command[:] + " --registry-config=" + ps.Name()
	args := strings.Split(executeCommand, " ")

	stdout, stderr, exitCode := executer.Execute(args[0], args[1:]...)

	if exitCode == 0 {
		return strings.TrimSpace(stdout), nil
	} else {
		err = fmt.Errorf("command '%s' exited with non-zero exit code %d: %s\n%s", executeCommand, exitCode, stdout, stderr)
		log.Error(err)
		return "", err
	}
}
