package main

import (
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hiveextvalidatingwebhooks "github.com/openshift/assisted-service/pkg/validating-webhooks/hiveextension/v1beta1"
	admissionCmd "github.com/openshift/generic-admission-server/pkg/cmd"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func main() {
	log.Info("Starting CRD Validation Webhooks.")

	log.SetLevel(log.InfoLevel)

	decoder := createDecoder()

	admissionCmd.RunAdmissionServer(
		hiveextvalidatingwebhooks.NewAgentClusterInstallValidatingAdmissionHook(decoder),
	)
}

func createDecoder() *admission.Decoder {
	scheme := runtime.NewScheme()
	err := hiveext.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Fatal("could not add to hiveext scheme")
	}
	decoder, err := admission.NewDecoder(scheme)
	if err != nil {
		log.WithError(err).Fatal("could not create a decoder")
	}
	return decoder
}
