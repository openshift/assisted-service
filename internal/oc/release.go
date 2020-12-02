package oc

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=release.go -package=oc -destination=mock_release.go
type Release interface {
	GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string) (string, error)
}

type release struct {
}

func NewRelease() Release {
	return &release{}
}

var execCommand = exec.Command

func execute(log logrus.FieldLogger, pullSecret string, command string, args ...string) (string, error) {
	// write pull secret to a temp file
	ps, err := ioutil.TempFile("", "registry-config")
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

	cmd := execCommand(command, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if err != nil {
		log.Error(out.String())
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// GetMCOImage gets mcoImage url from the releaseImageMirror if provided.
// Else gets it from the source releaseImage
func (r *release) GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	var mcoImage string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage or releaseImageMirror provided")
	}
	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		mcoImage, err = getMCOImageFromRelease(log, releaseImageMirror, pullSecret, true)
		if err != nil {
			log.WithError(err).Errorf("failed to get mco image from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		mcoImage, err = getMCOImageFromRelease(log, releaseImage, pullSecret, false)
		if err != nil {
			log.WithError(err).Errorf("failed to get mco image from release image %s", releaseImage)
			return "", err
		}
	}
	return mcoImage, err
}

func getMCOImageFromRelease(log logrus.FieldLogger, releaseImage string, pullSecret string, insecure bool) (string, error) {
	cmd := fmt.Sprintf("oc adm release info --image-for=machine-config-operator --insecure=%t %s", insecure, releaseImage)
	args := strings.Split(cmd, " ")
	mcoImage, err := execute(log, pullSecret, args[0], args[1:]...)
	if err != nil {
		log.WithError(err).Errorf("error running \"oc adm release info\" for release %s", releaseImage)
		return "", err
	}
	return mcoImage, nil
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
		path, err = extractFromRelease(log, releaseImageMirror, cacheDir, pullSecret, true)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		path, err = extractFromRelease(log, releaseImage, cacheDir, pullSecret, false)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from release image %s", releaseImageMirror)
			return "", err
		}
	}
	return path, err
}

// extractFromRelease returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image.
func extractFromRelease(log logrus.FieldLogger, releaseImage, cacheDir, pullSecret string, insecure bool) (string, error) {
	workdir := filepath.Join(cacheDir, releaseImage)
	log.Infof("extracting openshift-baremetal-install binary to %s", workdir)
	err := os.MkdirAll(workdir, 0755)
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf("oc adm release extract --command=openshift-baremetal-install --to=%s --insecure=%t %s", workdir, insecure, releaseImage)
	args := strings.Split(cmd, " ")
	_, err = execute(log, pullSecret, args[0], args[1:]...)
	if err != nil {
		log.WithError(err).Errorf("error running \"oc adm release extract\" for release %s", releaseImage)
		return "", err
	}

	// set path
	path := filepath.Join(workdir, "openshift-baremetal-install")

	return path, nil
}
