package v1beta1

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-openapi/swag"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/provider"
	"github.com/openshift/assisted-service/models"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	log "github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	agentClusterInstallGroup    = "extensions.hive.openshift.io"
	agentClusterInstallVersion  = "v1beta1"
	agentClusterInstallResource = "agentclusterinstalls"

	agentClusterInstallAdmissionGroup   = "admission.agentinstall.openshift.io"
	agentClusterInstallAdmissionVersion = "v1"
)

var (
	mutableFields = []string{"ClusterMetadata", "IgnitionEndpoint"}
)

// AgentClusterInstallValidatingAdmissionHook is a struct that is used to reference what code should be run by the generic-admission-server.
type AgentClusterInstallValidatingAdmissionHook struct {
	decoder *admission.Decoder
}

// NewAgentClusterInstallValidatingAdmissionHook constructs a new AgentClusterInstallValidatingAdmissionHook
func NewAgentClusterInstallValidatingAdmissionHook(decoder *admission.Decoder) *AgentClusterInstallValidatingAdmissionHook {
	return &AgentClusterInstallValidatingAdmissionHook{decoder: decoder}
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
//                    webhook is accessed by the kube apiserver.
// For example, generic-admission-server uses the data below to register the webhook on the REST resource "/apis/admission.agentinstall.openshift.io/v1/agentclusterinstallvalidators".
//              When the kube apiserver calls this registered REST resource, the generic-admission-server calls the Validate() method below.
func (a *AgentClusterInstallValidatingAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	log.WithFields(log.Fields{
		"group":    agentClusterInstallAdmissionGroup,
		"version":  agentClusterInstallAdmissionVersion,
		"resource": "agentclusterinstallvalidator",
	}).Info("Registering validation REST resource")
	// NOTE: This GVR is meant to be different than the AgentClusterInstall CRD GVR which has group "hivextension.openshift.io".
	return schema.GroupVersionResource{
			Group:    agentClusterInstallAdmissionGroup,
			Version:  agentClusterInstallAdmissionVersion,
			Resource: "agentclusterinstallvalidators",
		},
		"agentclusterinstallvalidator"
}

// Initialize is called by generic-admission-server on startup to set up any special initialization that your webhook needs.
func (a *AgentClusterInstallValidatingAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.WithFields(log.Fields{
		"group":    agentClusterInstallAdmissionGroup,
		"version":  agentClusterInstallAdmissionVersion,
		"resource": "agentclusterinstallvalidator",
	}).Info("Initializing validation REST resource")
	return nil // No initialization needed right now.
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
// Usually it's the kube apiserver that is making the admission validation request.
func (a *AgentClusterInstallValidatingAdmissionHook) Validate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "Validate",
	})

	if !shouldValidate(admissionSpec) {
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

	if admissionSpec.Operation == admissionv1.Create {
		return a.validateCreate(admissionSpec)
	}

	// all other operations are explicitly allowed
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// shouldValidate explicitly checks if the request should be validated. For example, this webhook may have accidentally been registered to check
// the validity of some other type of object with a different GVR.
func shouldValidate(admissionSpec *admissionv1.AdmissionRequest) bool {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "shouldValidate",
	})

	if admissionSpec.Resource.Group != agentClusterInstallGroup {
		contextLogger.Debug("Returning False, not our group")
		return false
	}

	if admissionSpec.Resource.Version != agentClusterInstallVersion {
		contextLogger.Debug("Returning False, it's our group, but not the right version")
		return false
	}

	if admissionSpec.Resource.Resource != agentClusterInstallResource {
		contextLogger.Debug("Returning False, it's our group and version, but not the right resource")
		return false
	}

	// If we get here, then we're supposed to validate the object.
	contextLogger.Debug("Returning True, passed all prerequisites.")
	return true
}

func (a *AgentClusterInstallValidatingAdmissionHook) validateCreate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateCreate",
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

	// verify that UserNetworkManagement is not set to false with SNO.
	// if the user leave this field empty it is fine because the AI knows
	// what to set as default
	if isUserManagedNetworkingSetToFalseWithSNO(newObject) {
		message := "UserManagedNetworking must be set to true with SNO"
		contextLogger.Errorf("Failed validation: %v", message)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusConflict, Reason: metav1.StatusReasonConflict,
				Message: message,
			},
		}
	}
	if err := validateCreatePlatformAndUMN(newObject); err != nil {
		contextLogger.Errorf("Failed validation: %s", err.Error())
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusConflict, Reason: metav1.StatusReasonConflict,
				Message: err.Error(),
			},
		}
	}

	// If we get here, then all checks passed, so the object is valid.
	contextLogger.Info("Successful validation")
	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}

}

// validateUpdate specifically validates update operations for AgentClusterInstall objects.
func (a *AgentClusterInstallValidatingAdmissionHook) validateUpdate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	contextLogger := log.WithFields(log.Fields{
		"operation": admissionSpec.Operation,
		"group":     admissionSpec.Resource.Group,
		"version":   admissionSpec.Resource.Version,
		"resource":  admissionSpec.Resource.Resource,
		"method":    "validateUpdate",
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

	oldObject := &hiveext.AgentClusterInstall{}
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

	if !areImageSetRefsEqual(oldObject.Spec.ImageSetRef, newObject.Spec.ImageSetRef) {
		message := "Attempted to change AgentClusterInstall.ImageSetRef which is immutable"
		contextLogger.Errorf("Failed validation: %v", message)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
				Message: message,
			},
		}
	}

	if isUserManagedNetworkingSetToFalseWithSNO(newObject) {
		message := "UserManagedNetworking must be set to true with SNO"
		contextLogger.Errorf("Failed validation: %v", message)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusConflict, Reason: metav1.StatusReasonConflict,
				Message: message,
			},
		}
	}

	if err := validateUpdatePlatformAndUMNUpdate(oldObject, newObject); err != nil {
		contextLogger.Errorf("Failed validation: %s", err.Error())
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Status: metav1.StatusFailure, Code: http.StatusConflict, Reason: metav1.StatusReasonConflict,
				Message: err.Error(),
			},
		}
	}

	if installAlreadyStarted(newObject.Status.Conditions) {
		ignoreChanges := mutableFields
		// MGMT-12794 This function returns true if the ProvisionRequirements field
		// has changed after installation completion. A change to this section has no effect
		// at this stage, but it is needed to serve some CI/CD gitops flows.
		if installCompleted(newObject.Status.Conditions) {
			ignoreChanges = append(ignoreChanges, "ProvisionRequirements")
		}
		hasChangedImmutableField, unsupportedDiff := hasChangedImmutableField(&oldObject.Spec, &newObject.Spec, ignoreChanges)
		if hasChangedImmutableField {
			message := fmt.Sprintf("Attempted to change AgentClusterInstall.Spec which is immutable after install started, except for %s fields. Unsupported change: \n%s", strings.Join(mutableFields, ","), unsupportedDiff)
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

func installAlreadyStarted(conditions []hivev1.ClusterInstallCondition) bool {
	cond := FindStatusCondition(conditions, hiveext.ClusterCompletedCondition)
	if cond == nil {
		return false
	}
	switch cond.Reason {
	case hiveext.ClusterInstallationFailedReason, hiveext.ClusterInstalledReason, hiveext.ClusterInstallationInProgressReason, hiveext.ClusterAlreadyInstallingReason:
		return true
	default:
		return false
	}
}

func installCompleted(conditions []hivev1.ClusterInstallCondition) bool {
	cond := FindStatusCondition(conditions, hiveext.ClusterCompletedCondition)
	if cond == nil {
		return false
	}
	return cond.Reason == hiveext.ClusterInstalledReason || cond.Reason == hiveext.ClusterInstallationFailedReason
}

// FindStatusCondition finds the conditionType in conditions.
func FindStatusCondition(conditions []hivev1.ClusterInstallCondition, conditionType string) *hivev1.ClusterInstallCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// hasChangedImmutableField determines if a AgentClusterInstall.spec immutable field was changed.
// it returns the diff string that shows the changes that are not supported
func hasChangedImmutableField(oldObject, cd *hiveext.AgentClusterInstallSpec, mutableFields []string) (bool, string) {
	r := &diffReporter{}
	opts := cmp.Options{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(hiveext.AgentClusterInstallSpec{}, mutableFields...),
		cmp.Reporter(r),
	}
	return !cmp.Equal(oldObject, cd, opts), r.String()
}

func areImageSetRefsEqual(imageSetRef1 *hivev1.ClusterImageSetReference, imageSetRef2 *hivev1.ClusterImageSetReference) bool {
	if imageSetRef1 == nil && imageSetRef2 == nil {
		return true
	} else if imageSetRef1 != nil && imageSetRef2 != nil {
		return imageSetRef1.Name == imageSetRef2.Name
	} else {
		return false
	}
}

func isUserManagedNetworkingSetToFalseWithSNO(newObject *hiveext.AgentClusterInstall) bool {
	return isSNO(newObject) &&
		newObject.Spec.Networking.UserManagedNetworking != nil &&
		!*newObject.Spec.Networking.UserManagedNetworking
}

func validateCreatePlatformAndUMN(newObject *hiveext.AgentClusterInstall) error {
	platform := common.PlatformTypeToPlatform(newObject.Spec.PlatformType)
	_, _, err := provider.GetActualCreateClusterPlatformParams(
		platform, newObject.Spec.Networking.UserManagedNetworking, getHighAvailabilityMode(newObject, nil), "")
	return err
}

func validateUpdatePlatformAndUMNUpdate(oldObject, newObject *hiveext.AgentClusterInstall) error {
	var (
		platform              *models.Platform
		userManagedNetworking *bool
	)

	if newObject.Spec.PlatformType != "" {
		platform = common.PlatformTypeToPlatform(newObject.Spec.PlatformType)
	} else {
		platform = common.PlatformTypeToPlatform(oldObject.Spec.PlatformType)
	}

	if newObject.Spec.Networking.UserManagedNetworking != nil {
		userManagedNetworking = newObject.Spec.Networking.UserManagedNetworking
	} else {
		userManagedNetworking = oldObject.Spec.Networking.UserManagedNetworking
	}

	_, _, err := provider.GetActualCreateClusterPlatformParams(
		platform, userManagedNetworking, getHighAvailabilityMode(oldObject, newObject), "")
	return err
}

func isSNO(newObject *hiveext.AgentClusterInstall) bool {
	return newObject.Spec.ProvisionRequirements.ControlPlaneAgents == 1 &&
		newObject.Spec.ProvisionRequirements.WorkerAgents == 0
}

func getHighAvailabilityMode(originalObject, updatesObject *hiveext.AgentClusterInstall) *string {
	if originalObject == nil {
		return swag.String("")
	}

	controlPlaneAgents := originalObject.Spec.ProvisionRequirements.ControlPlaneAgents
	workerAgents := originalObject.Spec.ProvisionRequirements.WorkerAgents

	if updatesObject != nil {
		if controlPlaneAgents != updatesObject.Spec.ProvisionRequirements.ControlPlaneAgents {
			controlPlaneAgents = updatesObject.Spec.ProvisionRequirements.ControlPlaneAgents
		}
		if workerAgents != updatesObject.Spec.ProvisionRequirements.WorkerAgents {
			workerAgents = updatesObject.Spec.ProvisionRequirements.WorkerAgents
		}
	}

	if controlPlaneAgents == 1 && workerAgents == 0 { // SNO
		return swag.String(models.ClusterHighAvailabilityModeNone)
	}
	return swag.String(models.ClusterHighAvailabilityModeFull)
}

// diffReporter is a simple custom reporter that only records differences
// detected during comparison.
type diffReporter struct {
	path  cmp.Path
	diffs []string
}

func (r *diffReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

func (r *diffReporter) Report(rs cmp.Result) {
	if !rs.Equal() {
		p := r.path.String()
		vx, vy := r.path.Last().Values()
		r.diffs = append(r.diffs, fmt.Sprintf("\t%s: (%+v => %+v)", p, vx, vy))
	}
}

func (r *diffReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

func (r *diffReporter) String() string {
	return strings.Join(r.diffs, "\n")
}
