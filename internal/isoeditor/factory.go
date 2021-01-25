package isoeditor

import (
	"io/ioutil"

	"github.com/openshift/assisted-service/internal/isoutil"

	"github.com/sirupsen/logrus"
)

//go:generate mockgen -package=isoeditor -destination=mock_factory.go -self_package=github.com/openshift/assisted-service/internal/isoeditor . Factory
type Factory interface {
	NewEditor(isoPath string, openshiftVersion string, log logrus.FieldLogger) (Editor, error)
}

type RhcosFactory struct{}

func (f *RhcosFactory) NewEditor(isoPath string, openshiftVersion string, log logrus.FieldLogger) (Editor, error) {
	isoTmpWorkDir, err := ioutil.TempDir("", "isoeditor")
	if err != nil {
		return nil, err
	}
	return &rhcosEditor{
		isoHandler:       isoutil.NewHandler(isoPath, isoTmpWorkDir),
		openshiftVersion: openshiftVersion,
		log:              log,
	}, nil
}
