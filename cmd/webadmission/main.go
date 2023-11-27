package main

import (
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	agentinstallvalidatingwebhooks "github.com/openshift/assisted-service/pkg/webhooks/agentinstall/v1beta1"
	hiveextwebhooks "github.com/openshift/assisted-service/pkg/webhooks/hiveextension/v1beta1"
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
		//validation webhooks
		hiveextwebhooks.NewAgentClusterInstallValidatingAdmissionHook(decoder),
		agentinstallvalidatingwebhooks.NewInfraEnvValidatingAdmissionHook(decoder),
		agentinstallvalidatingwebhooks.NewAgentValidatingAdmissionHook(decoder),
		agentinstallvalidatingwebhooks.NewAgentClassificationValidatingAdmissionHook(decoder),

		//mutating webhooks
		hiveextwebhooks.NewAgentClusterInstallMutatingAdmissionHook(decoder),
	)
}

func createDecoder() *admission.Decoder {
	scheme := runtime.NewScheme()
	err := hiveext.AddToScheme(scheme)
	if err != nil {
		log.WithError(err).Fatal("could not add to hiveext scheme")
	}
	return admission.NewDecoder(scheme)
}
