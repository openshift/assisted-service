package oc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=extract.go -package=oc -destination=mock_extract.go
type Extracter interface {
	Extract(log logrus.FieldLogger, imageIndexPath string, openshiftVersion string, filePath string, pullSecret string, insecure bool) (string, error)
	ExtractDatabaseIndex(log logrus.FieldLogger, releaseImageMirror string, openshiftVersion string, pullSecret string) (string, error)
}

type extract struct {
	executer executer.Executer
	config   Config
}

func NewExtracter(executer executer.Executer, config Config) Extracter {
	return &extract{executer, config}
}

const (
	imageIndex           = "registry.redhat.io/redhat/redhat-operator-index"
	dbFile               = "/database/index.db"
	templateImageExtract = "oc image extract %s:v%s --path=%s:%s --insecure=%t"
)

// ExtractDatabaseIndex extracts the databse idnex from the redhat-operator-index
func (r *extract) ExtractDatabaseIndex(log logrus.FieldLogger, releaseImageMirror string, openshiftVersion string, pullSecret string) (string, error) {
	if releaseImageMirror != "" {
		return r.Extract(log, imageIndex, openshiftVersion, dbFile, pullSecret, true)
	} else {
		return r.Extract(log, imageIndex, openshiftVersion, dbFile, pullSecret, false)
	}
}

// Extract extracts the file from image index to the temporary file
func (r *extract) Extract(log logrus.FieldLogger, imageIndexPath string, openshiftVersion string, filePath string, pullSecret string, insecure bool) (string, error) {
	file, err := os.CreateTemp("", filepath.Base(filePath))
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf(templateImageExtract, imageIndexPath, openshiftVersion, filePath, file.Name(), insecure)
	_, err = execute(log, r.executer, pullSecret, cmd, ocAuthArgument)
	if err != nil {
		return "", err
	}
	return file.Name(), nil
}
