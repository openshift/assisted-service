package testutil

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func Log() *logrus.Logger {
	l := logrus.New()
	if !testing.Verbose() {
		l.Level = logrus.WarnLevel
	} else {
		l.Level = logrus.DebugLevel
	}
	return l
}
