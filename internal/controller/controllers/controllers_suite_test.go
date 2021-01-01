package controllers

import (
	"io/ioutil"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes/scheme"
)

func init() {
	_ = v1alpha1.AddToScheme(scheme.Scheme)
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "controllers tests")
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}
