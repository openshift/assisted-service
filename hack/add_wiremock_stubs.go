package main

import (
	"encoding/json"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/subsystem"
	"github.com/sirupsen/logrus"
)

var Options struct {
	AuthType       auth.AuthType `envconfig:"AUTH_TYPE"`
	OCMBaseURL     string        `envconfig:"OCM_URL"`
	ReleaseSources string        `envconfig:"RELEASE_SOURCES" default:""`
}

func main() {
	log := logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{})

	err := envconfig.Process("wiremock", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Infof(
		"Starting add_wiremock_stubs.go with AUTH_TYPE='%s', OCM_URL='%s' and RELEASE_SOURCES='%s",
		Options.AuthType, Options.OCMBaseURL, Options.ReleaseSources,
	)

	if Options.AuthType == auth.TypeRHSSO {
		releaseSourcesString := os.Getenv("RELEASE_SOURCES")
		var releaseSources = models.ReleaseSources{}
		if err := json.Unmarshal([]byte(releaseSourcesString), &releaseSources); err != nil {
			log.Fatal("Fail to parse release sources, ", err)
		}

		wiremock := &subsystem.WireMock{
			OCMHost:        Options.OCMBaseURL,
			ReleaseSources: releaseSources,
		}

		err := wiremock.DeleteAllWiremockStubs()
		if err != nil {
			log.Fatal("Fail to delete all wiremock stubs, ", err)
		}
		log.Info("Wiremock stubs deleted successfully")

		if err = wiremock.CreateWiremockStubsForOCM(); err != nil {
			log.Fatal("Failed to init wiremock stubs, ", err)
		}
		log.Info("Wiremock stubs added successfully")
	}
}
