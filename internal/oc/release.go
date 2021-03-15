package oc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
)

const (
	mcoImageName        = "machine-config-operator"
	mustGatherImageName = "must-gather"
)

//go:generate mockgen -source=release.go -package=oc -destination=mock_release.go
type Release interface {
	GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMustGatherImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetOpenshiftVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMajorMinorVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error)
}

type release struct {
	executer executer.Executer
}

func NewRelease(executer executer.Executer) Release {
	return &release{executer}
}

const (
	templateGetImage   = "oc adm release info --image-for=%s --insecure=%t %s"
	templateGetVersion = "oc adm release info -o template --template '{{.metadata.version}}' --insecure=%t %s"
	templateExtract    = "oc adm release extract --command=openshift-baremetal-install --to=%s --insecure=%t %s"
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
	cmd := fmt.Sprintf(templateGetImage, imageName, insecure, releaseImage)
	args := strings.Split(cmd, " ")
	image, err := r.execute(log, pullSecret, args[0], args[1:]...)
	if err != nil {
		log.WithError(err).Errorf("error running \"oc adm release info\" for release %s", releaseImage)
		return "", err
	}
	return image, nil
}

func (r *release) getOpenshiftVersionFromRelease(log logrus.FieldLogger, releaseImage string, pullSecret string, insecure bool) (string, error) {
	cmd := fmt.Sprintf(templateGetVersion, insecure, releaseImage)
	args := strings.Split(cmd, " ")
	version, err := r.execute(log, pullSecret, args[0], args[1:]...)
	if err != nil {
		log.WithError(err).Errorf("error running \"oc adm release info\" for release %s", releaseImage)
		return "", err
	}
	return version, nil
}

// Extract openshift-baremetal-install binary from releaseImageMirror if provided.
// Else extract from the source releaseImage
func (r *release) Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error) {
	var path string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage or releaseImageMirror provided")
	}
	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		path, err = r.extractFromRelease(log, releaseImageMirror, cacheDir, pullSecret, true)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		path, err = r.extractFromRelease(log, releaseImage, cacheDir, pullSecret, false)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from release image %s", releaseImageMirror)
			return "", err
		}
	}
	return path, err
}

// extractFromRelease returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image.
func (r *release) extractFromRelease(log logrus.FieldLogger, releaseImage, cacheDir, pullSecret string, insecure bool) (string, error) {
	workdir := filepath.Join(cacheDir, releaseImage)
	log.Infof("extracting openshift-baremetal-install binary to %s", workdir)
	err := os.MkdirAll(workdir, 0755)
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf(templateExtract, workdir, insecure, releaseImage)
	args := strings.Split(cmd, " ")
	_, err = r.execute(log, pullSecret, args[0], args[1:]...)
	if err != nil {
		log.WithError(err).Errorf("error running \"oc adm release extract\" for release %s", releaseImage)
		return "", err
	}

	// set path
	path := filepath.Join(workdir, "openshift-baremetal-install")

	return path, nil
}

func (r *release) execute(log logrus.FieldLogger, pullSecret string, command string, args ...string) (string, error) {
	// write pull secret to a temp file
	ps, err := r.executer.TempFile("", "registry-config")
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

	args = append(args, "--registry-config="+ps.Name())

	stdout, stderr, exitCode := r.executer.Execute(command, args...)

	if exitCode == 0 {
		return strings.TrimSpace(stdout), nil
	} else {
		return "", fmt.Errorf("command %s exited with non-zero exit code %d: %s\n%s", command, exitCode, stdout, stderr)
	}
}
