package oc

import (
	"fmt"
	"io/ioutil"

	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=extract.go -package=oc -destination=mock_extract.go
type Extracter interface {
	Extract(log logrus.FieldLogger, imageIndexPath string, openshiftVersion string, filePath string, pullSecret string) (string, error)
	ExtractDatabaseIndex(log logrus.FieldLogger, openshiftVersion string, pullSecret string) (string, error)
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
	templateImageExtract = "oc image extract %s:v%s --path=%s:%s"
)

// ExtractDatabaseIndex extracts the databse idnex from the redhat-operator-index
func (r *extract) ExtractDatabaseIndex(log logrus.FieldLogger, openshiftVersion string, pullSecret string) (string, error) {
	return r.Extract(log, imageIndex, openshiftVersion, dbFile, pullSecret)
}

// Extract extracts the file from image index to the temporary file
func (r *extract) Extract(log logrus.FieldLogger, imageIndexPath string, openshiftVersion string, filePath string, pullSecret string) (string, error) {
	file, err := ioutil.TempFile("", "index.db")
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf(templateImageExtract, imageIndexPath, openshiftVersion, filePath, file.Name())
	_, err = execute(log, r.executer, pullSecret, cmd)
	if err != nil {
		return "", err
	}
	return file.Name(), nil
}
