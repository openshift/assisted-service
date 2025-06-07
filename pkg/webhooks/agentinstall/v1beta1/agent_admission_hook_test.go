package v1beta1

import (
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	apiserver "github.com/openshift/generic-admission-server/pkg/apiserver"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("agent web hook init", func() {
	It("ValidatingResource", func() {
		data := NewAgentValidatingAdmissionHook(createDecoder())
		expectedPlural := schema.GroupVersionResource{
			Group:    "admission.agentinstall.openshift.io",
			Version:  "v1",
			Resource: "agentvalidators",
		}
		expectedSingular := "agentvalidator"

		plural, singular := data.ValidatingResource()
		Expect(plural).To(Equal(expectedPlural))
		Expect(singular).To(Equal(expectedSingular))

	})

	It("Initialize", func() {
		data := NewAgentValidatingAdmissionHook(createDecoder())
		err := data.Initialize(nil, nil)
		Expect(err).To(BeNil())
	})

	It("Check implements interface ", func() {
		var hook interface{} = NewAgentValidatingAdmissionHook(createDecoder())
		_, ok := hook.(apiserver.ValidatingAdmissionHookV1)
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("agent web validate", func() {
	cases := []struct {
		name            string
		newSpec         v1beta1.AgentSpec
		newStatus       v1beta1.AgentStatus
		oldSpec         v1beta1.AgentSpec
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
				Resource: "agents",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and resource, wrong version",
			gvr: &metav1.GroupVersionResource{
				Group:    "agent-install.openshift.io",
				Version:  "not the right version",
				Resource: "agents",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and version, wrong resource",
			gvr: &metav1.GroupVersionResource{
				Group:    "agent-install.openshift.io",
				Version:  "v1beta1",
				Resource: "not the right resource",
			},
			expectedAllowed: true,
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName is mutable, no State",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "oldNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName.Namespace is immutable for day 1 host, state installing",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
			newStatus:       v1beta1.AgentStatus{DebugInfo: v1beta1.DebugInfo{State: models.HostStatusInstalling}, Kind: models.HostKindHost},
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName.Namespace is immutable for day 2 host, state installing",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
			newStatus:       v1beta1.AgentStatus{DebugInfo: v1beta1.DebugInfo{State: models.HostStatusInstalling}, Kind: models.HostKindAddToExistingClusterHost},
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName.Name is immutable for day 2 host, state installing",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "oldNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
			newStatus:       v1beta1.AgentStatus{DebugInfo: v1beta1.DebugInfo{State: models.HostStatusInstalling}, Kind: models.HostKindAddToExistingClusterHost},
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName can be unset for day 2 host, state installing",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: nil,
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			newStatus:       v1beta1.AgentStatus{DebugInfo: v1beta1.DebugInfo{State: models.HostStatusInstalling}, Kind: models.HostKindAddToExistingClusterHost},
		},
		{
			name: "Test Agent.Spec.ClusterDeploymentName.Namespace is mutable, state known",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			newStatus:       v1beta1.AgentStatus{DebugInfo: v1beta1.DebugInfo{State: models.HostStatusKnown}},
		},
		{
			name: "Test Agent update does not fail when ClusterReference is set and remains the same",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test Agent update does not fail when ClusterReference is nil and remains the same",
			newSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: nil,
			},
			oldSpec: v1beta1.AgentSpec{
				ClusterDeploymentName: nil,
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
	}

	for i := range cases {
		tc := cases[i]
		It(tc.name, func() {
			data := NewAgentValidatingAdmissionHook(createDecoder())
			newObject := &v1beta1.Agent{
				Spec:   tc.newSpec,
				Status: tc.newStatus,
			}

			oldObject := &v1beta1.Agent{
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
					Group:    "agent-install.openshift.io",
					Version:  "v1beta1",
					Resource: "agents",
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
