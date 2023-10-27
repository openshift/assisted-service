package v1beta1

import (
	"net/http"

	"github.com/openshift/assisted-service/api/v1beta1"
	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	infraEnvGroup    = "agent-install.openshift.io"
	infraEnvVersion  = "v1beta1"
	infraEnvResource = "infraenvs"

	infraEnvAdmissionGroup   = "admission.agentinstall.openshift.io"
	infraEnvAdmissionVersion = "v1"
)

// InfraEnvValidatingAdmissionHook is a struct that is used to reference what code should be run by the generic-admission-server.
type InfraEnvValidatingAdmissionHook struct {
	decoder *admission.Decoder
}

// NewInfraEnvValidatingAdmissionHook constructs a new NewInfraEnvValidatingAdmissionHook
func NewInfraEnvValidatingAdmissionHook(decoder *admission.Decoder) *InfraEnvValidatingAdmissionHook {
	return &InfraEnvValidatingAdmissionHook{decoder: decoder}
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
//
//	webhook is accessed by the kube apiserver.
//
// For example, generic-admission-server uses the data below to register the webhook on the REST resource "/apis/admission.agentinstall.openshift.io/v1/infraenvvalidators".
//
//	When the kube apiserver calls this registered REST resource, the generic-admission-server calls the Validate() method below.
func (a *InfraEnvValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	log.WithFields(log.Fields{
		"group":    infraEnvAdmissionGroup,
		"version":  infraEnvAdmissionVersion,
		"resource": "infraenvvalidator",
	}).Info("Registering validation REST resource")
	// NOTE: This GVR is meant to be different than the InfraEnv CRD GVR which has group "agent-install.openshift.io".
	return schema.GroupVersionResource{
			Group:    infraEnvAdmissionGroup,
			Version:  infraEnvAdmissionVersion,
			Resource: "infraenvvalidators",
		},
		"infraenvvalidator"
}

// Initialize is called by generic-admission-server on startup to setup any special initialization that your webhook needs.
func (a *InfraEnvValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.WithFields(log.Fields{
		"group":    infraEnvAdmissionGroup,
		"version":  infraEnvAdmissionVersion,
		"resource": "infraenvvalidator",
	}).Info("Initializing validation REST resource")
	return nil // No initialization needed right now.
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
// Usually it's the kube apiserver that is making the admission validation request.
func (a *InfraEnvValidatingAdmissionHook) Validate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
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

	if admissionSpec.Operation == admissionv1.Create {
		return a.validateCreate(admissionSpec)
	}

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
func (a *InfraEnvValidatingAdmissionHook) shouldValidate(admissionSpec *admissionv1.AdmissionRequest) bool {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "shouldValidate",
	})

	if admissionSpec.Resource.Group != infraEnvGroup {
		contextLogger.Debug("Returning False, not our group")
		return false
	}

	if admissionSpec.Resource.Version != infraEnvVersion {
		contextLogger.Debug("Returning False, it's our group, but not the right version")
		return false
	}

	if admissionSpec.Resource.Resource != infraEnvResource {
		contextLogger.Debug("Returning False, it's our group and version, but not the right resource")
		return false
	}

	// If we get here, then we're supposed to validate the object.
	contextLogger.Debug("Returning True, passed all prerequisites.")
	return true
}

func areClusterRefsEqual(clusterRef1 *v1beta1.ClusterReference, clusterRef2 *v1beta1.ClusterReference) bool {
	if clusterRef1 == nil && clusterRef2 == nil {
		return true
	} else if clusterRef1 != nil && clusterRef2 != nil {
		return (clusterRef1.Name == clusterRef2.Name && clusterRef1.Namespace == clusterRef2.Namespace)
	} else {
		return false
	}
}

// validateUpdate specifically validates create operations for InfraEnv objects.
func (a *InfraEnvValidatingAdmissionHook) validateCreate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateCreate",
	})

	newObject := &v1beta1.InfraEnv{}
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

	// Ensure that ClusterRef and OSImageVersion are not both specified
	if newObject.Spec.ClusterRef != nil && newObject.Spec.OSImageVersion != "" {
		message := "Either Spec.ClusterRef or Spec.OSImageVersion should be specified (not both)."
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

	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// validateUpdate specifically validates update operations for InfraEnv objects.
func (a *InfraEnvValidatingAdmissionHook) validateUpdate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateUpdate",
	})

	newObject := &v1beta1.InfraEnv{}
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

	oldObject := &v1beta1.InfraEnv{}
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

	if !areClusterRefsEqual(oldObject.Spec.ClusterRef, newObject.Spec.ClusterRef) {
		message := "Attempted to change Spec.ClusterRef which is immutable after InfraEnv creation."
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

	// If we get here, then all checks passed, so the object is valid.
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}
