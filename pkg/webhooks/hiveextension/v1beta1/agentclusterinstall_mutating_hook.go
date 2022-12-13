package v1beta1

import (
	"encoding/json"
	"net/http"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// AgentClusterInstallMutatingAdmissionHook is a struct that is used to reference what code should be run by the generic-admission-server.
type AgentClusterInstallMutatingAdmissionHook struct {
	decoder *admission.Decoder
}

// NewAgentClusterInstallMutatingAdmissionHook constructs a new AgentClusterInstallMutatingAdmissionHook
func NewAgentClusterInstallMutatingAdmissionHook(decoder *admission.Decoder) *AgentClusterInstallMutatingAdmissionHook {
	return &AgentClusterInstallMutatingAdmissionHook{decoder: decoder}
}

// MutatingResource is the resource to use for hosting your admission webhook. (see https://github.com/openshift/generic-admission-server)
// The generic-admission-server uses the data below to register this webhook so when kube apiserver calls the REST path
// "/apis/admission.agentinstall.openshift.io/v1/agentclusterinstallmutators" the generic-admission-server calls
// the Admit() method below.
func (a *AgentClusterInstallMutatingAdmissionHook) MutatingResource() (plural schema.GroupVersionResource, singular string) {
	log.WithFields(log.Fields{
		"group":    agentClusterInstallAdmissionGroup,
		"version":  agentClusterInstallAdmissionVersion,
		"resource": "agentclusterinstallmutator",
	}).Info("Registering mutating REST resource")
	// NOTE: This GVR is meant to be different than the AgentClusterInstall CRD GVR which has group "hivextension.openshift.io".
	return schema.GroupVersionResource{
			Group:    agentClusterInstallAdmissionGroup,
			Version:  agentClusterInstallAdmissionVersion,
			Resource: "agentclusterinstallmutators",
		},
		"agentclusterinstallmutator"
}

// Initialize implements the AdmissionHook API. (see https://github.com/openshift/generic-admission-server)
// This function is called by generic-admission-server on startup to setup any special initialization
// that your webhook needs.
func (a *AgentClusterInstallMutatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.WithFields(log.Fields{
		"group":    agentClusterInstallAdmissionGroup,
		"version":  agentClusterInstallAdmissionVersion,
		"resource": "agentclusterinstallmutator",
	}).Info("Initializing validation REST resource")
	return nil // No initialization needed right now.
}

// Admit is called to decide whether to accept the admission request. The returned AdmissionResponse may
// use the Patch field to mutate the object from the passed AdmissionRequest. It implements the MutatingAdmissionHookV1
// interface. (see https://github.com/openshift/generic-admission-server)
func (a *AgentClusterInstallMutatingAdmissionHook) Admit(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "Admit",
	})

	if !shouldValidate(admissionSpec) {
		contextLogger.Info("Skipping mutation for request")
		// The request object isn't something that this mutator should validate.
		// Therefore, we say that it's allowed.
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	contextLogger.Info("Mutating request")
	if admissionSpec.Operation == admissionv1.Update || admissionSpec.Operation == admissionv1.Create {
		return a.SetDefaults(admissionSpec)
	}

	// all other operations are explicitly allowed
	contextLogger.Info("No changes were made")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

func (a *AgentClusterInstallMutatingAdmissionHook) SetDefaults(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "SetDefaults",
	})

	newObject := &hiveext.AgentClusterInstall{}
	if err := a.decoder.DecodeRaw(admissionSpec.Object, newObject); err != nil {
		contextLogger.Errorf("Failed unmarshaling Object: %v", err.Error())
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
				Message: err.Error(),
			},
		}
	}

	// Add the new data to the contextLogger
	contextLogger.Data["object.Name"] = newObject.Name

	var patchList = make([]map[string]interface{}, 0)

	// if UserNetworkManagement is not set by the user apply the defaults:
	// true for SNO and false for multi-node
	if !installAlreadyStarted(newObject.Status.Conditions) && newObject.DeletionTimestamp.IsZero() {
		if newObject.Spec.Networking.UserManagedNetworking == nil {
			patchList = append(patchList, patchUserManagedNetworking(newObject, contextLogger))
		}
	}

	if len(patchList) > 0 {
		body, err := json.Marshal(patchList)
		if err != nil {
			contextLogger.Errorf("Failed marshaling patch: %v", err.Error())
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: err.Error(),
				},
			}
		}

		contextLogger.Infof("Mutating object: %s", string(body))
		var patchType admissionv1.PatchType = admissionv1.PatchTypeJSONPatch
		return &admissionv1.AdmissionResponse{
			Allowed:   true,
			Patch:     body,
			PatchType: &patchType,
		}
	}

	// If we get here noting was changed
	contextLogger.Info("No changes were made")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

func patchUserManagedNetworking(newObject *hiveext.AgentClusterInstall, logger *log.Entry) map[string]interface{} {
	var message string
	var userManagedNetworking bool

	if isSNO(newObject) {
		message = "setting UserManagedNetworking to true with SNO"
		userManagedNetworking = true
	} else {
		message = "setting UserManagedNetworking to false"
		userManagedNetworking = false
	}

	log.Info(message)
	return map[string]interface{}{
		"op":    "replace",
		"path":  "/spec/networking/userManagedNetworking",
		"value": userManagedNetworking,
	}
}
