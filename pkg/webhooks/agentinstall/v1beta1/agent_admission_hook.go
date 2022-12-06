package v1beta1

import (
	"net/http"

	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	log "github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	agentResource = "agents"

	agentAdmissionGroup   = "admission.agentinstall.openshift.io"
	agentAdmissionVersion = "v1"
)

// AgentValidatingAdmissionHook is a struct that is used to reference what code should be run by the generic-admission-server.
type AgentValidatingAdmissionHook struct {
	decoder *admission.Decoder
}

// NewAgentValidatingAdmissionHook constructs a new NewAgentValidatingAdmissionHook
func NewAgentValidatingAdmissionHook(decoder *admission.Decoder) *AgentValidatingAdmissionHook {
	return &AgentValidatingAdmissionHook{decoder: decoder}
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
//                    webhook is accessed by the kube apiserver.
// For example, generic-admission-server uses the data below to register the webhook on the REST resource "/apis/admission.agentinstall.openshift.io/v1/agentvalidators".
//              When the kube apiserver calls this registered REST resource, the generic-admission-server calls the Validate() method below.
func (a *AgentValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	log.WithFields(log.Fields{
		"group":    agentAdmissionGroup,
		"version":  agentAdmissionVersion,
		"resource": "agentvalidator",
	}).Info("Registering validation REST resource")
	// NOTE: This GVR is meant to be different than the Agent CRD GVR which has group "agent-install.openshift.io".
	return schema.GroupVersionResource{
			Group:    agentAdmissionGroup,
			Version:  agentAdmissionVersion,
			Resource: "agentvalidators",
		},
		"agentvalidator"
}

// Initialize is called by generic-admission-server on startup to setup any special initialization that your webhook needs.
func (a *AgentValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.WithFields(log.Fields{
		"group":    agentAdmissionGroup,
		"version":  agentAdmissionVersion,
		"resource": "agentvalidator",
	}).Info("Initializing validation REST resource")
	return nil // No initialization needed right now.
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
// Usually it's the kube apiserver that is making the admission validation request.
func (a *AgentValidatingAdmissionHook) Validate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "Validate",
	})

	if !a.shouldValidate(admissionSpec) {
		contextLogger.Info("Skipping validation for request")
		// The request object isn't something that this validator should validate.
		// Therefore, we say that it's allowed.
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	contextLogger.Info("Validating request")

	if admissionSpec.Operation == admissionv1.Update {
		return a.validateUpdate(admissionSpec)
	}

	// We're only validating updates at this time, so all other operations are explicitly allowed.
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// shouldValidate explicitly checks if the request should be validated. For example, this webhook may have accidentally been registered to check
// the validity of some other type of object with a different GVR.
func (a *AgentValidatingAdmissionHook) shouldValidate(admissionSpec *admissionv1.AdmissionRequest) bool {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "shouldValidate",
	})

	if admissionSpec.Resource.Group != v1beta1.Group {
		contextLogger.Debug("Returning False, not our group")
		return false
	}

	if admissionSpec.Resource.Version != v1beta1.Version {
		contextLogger.Debug("Returning False, it's our group, but not the right version")
		return false
	}

	if admissionSpec.Resource.Resource != agentResource {
		contextLogger.Debug("Returning False, it's our group and version, but not the right resource")
		return false
	}

	// If we get here, then we're supposed to validate the object.
	contextLogger.Debug("Returning True, passed all prerequisites.")
	return true
}

// validateUpdate specifically validates update operations for Agent objects.
func (a *AgentValidatingAdmissionHook) validateUpdate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateUpdate",
	})

	newObject := &v1beta1.Agent{}
	err := a.decoder.DecodeRaw(admissionSpec.Object, newObject)
	if err != nil {
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

	oldObject := &v1beta1.Agent{}
	if err := a.decoder.DecodeRaw(admissionSpec.OldObject, oldObject); err != nil {
		contextLogger.Errorf("Failed unmarshaling OldObject: %v", err.Error())
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
				Message: err.Error(),
			},
		}
	}

	if !areClusterRefsEqual(oldObject.Spec.ClusterDeploymentName, newObject.Spec.ClusterDeploymentName) {
		installingStatuses := []string{
			models.HostStatusPreparingForInstallation,
			models.HostStatusPreparingFailed,
			models.HostStatusPreparingSuccessful,
			models.HostStatusInstalling,
			models.HostStatusInstallingInProgress,
			models.HostStatusInstallingPendingUserAction,
		}
		if funk.ContainsString(installingStatuses, newObject.Status.DebugInfo.State) {
			message := "Attempted to change Spec.ClusterDeploymentName which is immutable during Agent installation."
			contextLogger.Infof("Failed validation: %v", message)
			contextLogger.Error(message)
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: message,
				},
			}
		}
	}

	// If we get here, then all checks passed, so the object is valid.
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}
