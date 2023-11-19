package v1beta1

import (
	"encoding/json"
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/generic-admission-server/pkg/apiserver"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func createDecoder() *admission.Decoder {
	scheme := runtime.NewScheme()
	err := hiveext.AddToScheme(scheme)
	Expect(err).To(BeNil())
	return admission.NewDecoder(scheme)
}

var _ = Describe("ACI web hook init", func() {
	It("ValidatingResource", func() {
		data := NewAgentClusterInstallValidatingAdmissionHook(createDecoder())
		expectedPlural := schema.GroupVersionResource{
			Group:    "admission.agentinstall.openshift.io",
			Version:  "v1",
			Resource: "agentclusterinstallvalidators",
		}
		expectedSingular := "agentclusterinstallvalidator"

		plural, singular := data.ValidatingResource()
		Expect(plural).To(Equal(expectedPlural))
		Expect(singular).To(Equal(expectedSingular))

	})

	It("Initialize", func() {
		data := NewAgentClusterInstallValidatingAdmissionHook(createDecoder())
		err := data.Initialize(nil, nil)
		Expect(err).To(BeNil())
	})

	It("Check implements interface ", func() {
		var hook interface{} = NewAgentClusterInstallValidatingAdmissionHook(createDecoder())
		_, ok := hook.(apiserver.ValidatingAdmissionHookV1)
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("ACI web validate", func() {
	cases := []struct {
		name            string
		newSpec         hiveext.AgentClusterInstallSpec
		conditions      []hivev1.ClusterInstallCondition
		oldSpec         hiveext.AgentClusterInstallSpec
		newObjectRaw    []byte
		oldObjectRaw    []byte
		operation       admissionv1.Operation
		expectedAllowed bool
		gvr             *metav1.GroupVersionResource
	}{
		{
			name:            "Test unable to marshal old object during update",
			oldObjectRaw:    []byte{0},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test doesn't validate with right version and resource, but wrong group",
			gvr: &metav1.GroupVersionResource{
				Group:    "not the right group",
				Version:  "v1beta1",
				Resource: "agentclusterinstalls",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and resource, wrong version",
			gvr: &metav1.GroupVersionResource{
				Group:    "extensions.hive.openshift.io",
				Version:  "not the right version",
				Resource: "agentclusterinstalls",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and version, wrong resource",
			gvr: &metav1.GroupVersionResource{
				Group:    "extensions.hive.openshift.io",
				Version:  "v1beta1",
				Resource: "not the right resource",
			},
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec is mutable (updates allowed) No conditions",
			newSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "somekey",
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "someotherkey",
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec is mutable (updates allowed) Install not started",
			newSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "somekey",
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationNotStartedReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "someotherkey",
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec is immutable (updates not allowed) Install started",
			newSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "somekey",
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationInProgressReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "someotherkey",
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClusterInstall.Spec is immutable (updates not allowed) Install finished",
			newSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "somekey",
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstalledReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "someotherkey",
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClusterInstall.Spec update allowed for provision fields when Install finished",
			newSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       3,
				},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstalledReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec update allowed for ignition endpoint field when Install finished",
			newSpec: hiveext.AgentClusterInstallSpec{
				IgnitionEndpoint: &hiveext.IgnitionEndpoint{
					Url: "http://endpoint-2",
				},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstalledReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				IgnitionEndpoint: &hiveext.IgnitionEndpoint{
					Url: "http://endpoint-1",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec is immutable (updates not allowed) Install failed",
			newSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "somekey",
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationFailedReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				SSHPublicKey: "someotherkey",
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClusterInstall.Spec update allowed for provision fields when Install failed",
			newSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       3,
				},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationFailedReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec.ClusterMetadata is mutable (updates are allowed) Install started",
			newSpec: hiveext.AgentClusterInstallSpec{
				ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID: "clusterid",
				},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationInProgressReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				ClusterMetadata: &hivev1.ClusterMetadata{
					ClusterID: "newclusterid",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClusterInstall.Spec.ImageSetRef is immutable (updates not allowed)",
			newSpec: hiveext.AgentClusterInstallSpec{
				ImageSetRef: &hivev1.ClusterImageSetReference{Name: "someimage"},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstalledReason,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				ImageSetRef: &hivev1.ClusterImageSetReference{Name: "someotherimage"},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClusterInstall.Spec.ImageSetRef is immutable (delete not allowed)",
			newSpec: hiveext.AgentClusterInstallSpec{
				ImageSetRef: &hivev1.ClusterImageSetReference{Name: "someimage"},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstalledReason,
				},
			},
			oldSpec:         hiveext.AgentClusterInstallSpec{},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "ACI create with userManagedNetworking set to false is forbidden",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "ACI create with userManagedNetworking set to true is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with None platform and userManagedNetworking set to true is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with None platform and userManagedNetworking set to false is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.NonePlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "SNO ACI create with None platform and userManagedNetworking set to true is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "Multi node ACI create with None platform and userManagedNetworking set to true is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "SNO ACI create with None platform and userManagedNetworking set to false is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Multi node ACI create with None platform and userManagedNetworking set to false is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "ACI create with BareMetal platform and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with BareMetal platform and userManagedNetworking set to true is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "SNO ACI create with BareMetal platform and userManagedNetworking set to false is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       0,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Multi node ACI create with BareMetal platform and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "Multi node ACI create with BareMetal platform and userManagedNetworking set to true is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.BareMetalPlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Multi node ACI create with BareMetal platform and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with VSpherePlatformType platform and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.VSpherePlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with VSpherePlatformType platform and userManagedNetworking set to true is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.VSpherePlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with NutanixPlatformType platform and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.NutanixPlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "ACI create with NutanixPlatformType platform and userManagedNetworking set to true not is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NutanixPlatformType,
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "ACI update None platform with userManagedNetworking set to false not is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "ACI update None platform with platform BareMetalPlatformType and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "ACI update None platform with platform BareMetalPlatformType and userManagedNetworking set to false is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "ACI update BareMetalPlatformType platform with platform None is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				PlatformType: hiveext.NonePlatformType,
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "ACI update BareMetalPlatformType platform with userManagedNetworking set to true is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Multi node ACI update BareMetalPlatformType platform to SNO is not allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       0,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				PlatformType: hiveext.BareMetalPlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       3,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Multi node ACI update None platform to SNO is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       0,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       3,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "SNO ACI update None platform to Multi node is allowed",
			newSpec: hiveext.AgentClusterInstallSpec{
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 3,
					WorkerAgents:       3,
				},
			},
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:   hiveext.Networking{UserManagedNetworking: swag.Bool(true)},
				PlatformType: hiveext.NonePlatformType,
				ProvisionRequirements: hiveext.ProvisionRequirements{
					ControlPlaneAgents: 1,
					WorkerAgents:       1,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
	}

	for i := range cases {
		tc := cases[i]
		It(tc.name, func() {
			data := NewAgentClusterInstallValidatingAdmissionHook(createDecoder())
			newObject := &hiveext.AgentClusterInstall{
				Spec: tc.newSpec,
			}
			newObject.Status.Conditions = tc.conditions
			oldObject := &hiveext.AgentClusterInstall{
				Spec: tc.oldSpec,
			}

			if tc.newObjectRaw == nil {
				tc.newObjectRaw, _ = json.Marshal(newObject)
			}

			if tc.oldObjectRaw == nil {
				tc.oldObjectRaw, _ = json.Marshal(oldObject)
			}

			if tc.gvr == nil {
				tc.gvr = &metav1.GroupVersionResource{
					Group:    "extensions.hive.openshift.io",
					Version:  "v1beta1",
					Resource: "agentclusterinstalls",
				}
			}

			request := &admissionv1.AdmissionRequest{
				Operation: tc.operation,
				Resource:  *tc.gvr,
				Object: runtime.RawExtension{
					Raw: tc.newObjectRaw,
				},
				OldObject: runtime.RawExtension{
					Raw: tc.oldObjectRaw,
				},
			}

			response := data.Validate(request)

			Expect(response.Allowed).To(Equal(tc.expectedAllowed))
		})
	}

})

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "webhooks tests")
}
