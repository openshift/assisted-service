package v1beta1

import (
	"net/http"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/openshift/assisted-service/api/v1beta1"
	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	agentClassificationResource         = "agentclassifications"
	agentClassificationAdmissionGroup   = "admission.agentinstall.openshift.io"
	agentClassificationAdmissionVersion = "v1"
	ClassificationLabelPrefix           = "agentclassification." + v1beta1.Group + "/"
)

// AgentClassificationValidatingAdmissionHook is a struct that is used to reference what code should be run by the generic-admission-server.
type AgentClassificationValidatingAdmissionHook struct {
	decoder *admission.Decoder
}

// NewAgentClassificationValidatingAdmissionHook constructs a new AgentClassificationValidatingAdmissionHook
func NewAgentClassificationValidatingAdmissionHook(decoder *admission.Decoder) *AgentClassificationValidatingAdmissionHook {
	return &AgentClassificationValidatingAdmissionHook{decoder: decoder}
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
//
//	webhook is accessed by the kube apiserver.
//
// For example, generic-admission-server uses the data below to register the webhook on the REST resource "/apis/admission.agentinstall.openshift.io/v1/agentclassificationvalidators".
//
//	When the kube apiserver calls this registered REST resource, the generic-admission-server calls the Validate() method below.
func (a *AgentClassificationValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	log.WithFields(log.Fields{
		"group":    agentClassificationAdmissionGroup,
		"version":  agentClassificationAdmissionVersion,
		"resource": "agentclassificationvalidator",
	}).Info("Registering validation REST resource")
	// NOTE: This GVR is meant to be different than the AgentClassification CRD GVR which has group "agent-install.openshift.io".
	return schema.GroupVersionResource{
			Group:    agentClassificationAdmissionGroup,
			Version:  agentClassificationAdmissionVersion,
			Resource: "agentclassificationvalidators",
		},
		"agentclassificationvalidator"
}

// Initialize is called by generic-admission-server on startup to setup any special initialization that your webhook needs.
func (a *AgentClassificationValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.WithFields(log.Fields{
		"group":    agentClassificationAdmissionGroup,
		"version":  agentClassificationAdmissionVersion,
		"resource": "agentclassificationvalidator",
	}).Info("Initializing validation REST resource")
	return nil // No initialization needed right now.
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
// Usually it's the kube apiserver that is making the admission validation request.
func (a *AgentClassificationValidatingAdmissionHook) Validate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
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
func (a *AgentClassificationValidatingAdmissionHook) shouldValidate(admissionSpec *admissionv1.AdmissionRequest) bool {
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

	if admissionSpec.Resource.Resource != agentClassificationResource {
		contextLogger.Debug("Returning False, it's our group and version, but not the right resource")
		return false
	}

	// If we get here, then we're supposed to validate the object.
	contextLogger.Debug("Returning True, passed all prerequisites.")
	return true
}

// validateCreate specifically validates create operations for AgentClassification objects.
func (a *AgentClassificationValidatingAdmissionHook) validateCreate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateCreate",
	})

	newObject := &v1beta1.AgentClassification{}
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

	// Validate the specified label key and value
	f := field.NewPath("spec")
	errs := validation.ValidateLabels(map[string]string{ClassificationLabelPrefix + newObject.Spec.LabelKey: newObject.Spec.LabelValue}, f)
	if strings.HasPrefix(newObject.Spec.LabelValue, "QUERYERROR") {
		errs = append(errs, field.Invalid(f, newObject.Spec.LabelValue, "label must not start with QUERYERROR as this is reserved"))
	}

	// Validate that we can parse the specified query
	_, err := gojq.Parse(newObject.Spec.Query)
	if err != nil {
		errs = append(errs, field.Invalid(f, newObject.Spec.Query, err.Error()))
	}

	if len(errs) > 0 {
		contextLogger.Infof("Validation failed: %s", errs.ToAggregate().Error())
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
				Message: errs.ToAggregate().Error(),
			},
		}
	}

	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// validateUpdate specifically validates update operations for AgentClassification objects.
func (a *AgentClassificationValidatingAdmissionHook) validateUpdate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateUpdate",
	})

	newObject := &v1beta1.AgentClassification{}
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

	oldObject := &v1beta1.AgentClassification{}
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

	// Validate that the label key and value haven't changed
	if (oldObject.Spec.LabelKey != newObject.Spec.LabelKey) || (oldObject.Spec.LabelValue != newObject.Spec.LabelValue) {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
				Message: "Label modified: the specified label may not be modified after creation",
			},
		}
	}

	// If we get here, then all checks passed, so the object is valid.
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}
