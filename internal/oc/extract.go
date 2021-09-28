package oc

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=extract.go -package=oc -destination=mock_extract.go
type Extracter interface {
	Extract(imageIndexPath string, openshiftVersion string, filePath string, pullSecret string, insecure bool) (string, error)
	ExtractDatabaseIndex(releaseImageMirror string, openshiftVersion string, pullSecret string) (string, error)
}

type extract struct {
	executer executer.Executer
	config   Config
	log      logrus.FieldLogger
}

func NewExtracter(executer executer.Executer, config Config, log logrus.FieldLogger) Extracter {
	return &extract{executer, config, log}
}

const (
	imageIndex           = "registry.redhat.io/redhat/redhat-operator-index"
	dbFile               = "/database/index.db"
	templateImageExtract = "oc image extract %s:v%s --path=%s:%s --insecure=%t"
)

// ExtractDatabaseIndex extracts the databse idnex from the redhat-operator-index
func (r *extract) ExtractDatabaseIndex(releaseImageMirror string, openshiftVersion string, pullSecret string) (string, error) {
	if releaseImageMirror != "" {
		return r.Extract(imageIndex, openshiftVersion, dbFile, pullSecret, true)
	} else {
		return r.Extract(imageIndex, openshiftVersion, dbFile, pullSecret, false)
	}
}

// Extract extracts the file from image index to the temporary file
func (r *extract) Extract(imageIndexPath string, openshiftVersion string, filePath string, pullSecret string, insecure bool) (string, error) {
	file, err := ioutil.TempFile("", filepath.Base(filePath))
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf(templateImageExtract, imageIndexPath, openshiftVersion, filePath, file.Name(), insecure)
	_, err = execute(r.log, r.executer, pullSecret, cmd)
	if err != nil {
		return "", err
	}
	return file.Name(), nil
}
