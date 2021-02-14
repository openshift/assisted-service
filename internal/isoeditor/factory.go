package isoeditor

import (
	"context"
	"io/ioutil"

	"github.com/openshift/assisted-service/internal/isoutil"
	"github.com/sirupsen/logrus"
)

type Config struct {
	ConcurrentEdits int `envconfig:"CONCURRENT_EDITS" default:"10"`
}

type EditFunc func(myEditor Editor) error

//go:generate mockgen -package=isoeditor -destination=mock_factory.go -self_package=github.com/openshift/assisted-service/internal/isoeditor . Factory
type Factory interface {
	WithEditor(ctx context.Context, isoPath string, openshiftVersion string, log logrus.FieldLogger, proc EditFunc) error
}

type token struct{}
type RhcosFactory struct {
	// "semaphore" for tracking editors in use, send to checkout, receive to checkin
	sem chan token
}

func NewFactory(config Config) Factory {
	f := &RhcosFactory{
		sem: make(chan token, config.ConcurrentEdits),
	}
	return f
}

func (f *RhcosFactory) WithEditor(ctx context.Context, isoPath string, openshiftVersion string, log logrus.FieldLogger, proc EditFunc) error {
	select {
	case f.sem <- token{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() {
		<-f.sem
	}()

	ed, err := f.newEditor(isoPath, openshiftVersion, log)
	if err != nil {
		return err
	}

	return proc(ed)
}

func (f *RhcosFactory) newEditor(isoPath string, openshiftVersion string, log logrus.FieldLogger) (Editor, error) {
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
