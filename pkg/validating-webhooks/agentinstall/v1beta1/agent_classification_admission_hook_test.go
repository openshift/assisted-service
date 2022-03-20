package v1beta1

import (
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	apiserver "github.com/openshift/generic-admission-server/pkg/apiserver"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("agent classification web hook init", func() {
	It("ValidatingResource", func() {
		data := NewAgentClassificationValidatingAdmissionHook(createDecoder())
		expectedPlural := schema.GroupVersionResource{
			Group:    "admission.agentinstall.openshift.io",
			Version:  "v1",
			Resource: "agentclassificationvalidators",
		}
		expectedSingular := "agentclassificationvalidator"

		plural, singular := data.ValidatingResource()
		Expect(plural).To(Equal(expectedPlural))
		Expect(singular).To(Equal(expectedSingular))

	})

	It("Initialize", func() {
		data := NewAgentClassificationValidatingAdmissionHook(createDecoder())
		err := data.Initialize(nil, nil)
		Expect(err).To(BeNil())
	})

	It("Check implements interface ", func() {
		var hook interface{} = NewAgentClassificationValidatingAdmissionHook(createDecoder())
		_, ok := hook.(apiserver.ValidatingAdmissionHookV1)
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("agent classification web validate", func() {
	var (
		validKey     = "size"
		invalidKey   = "s!ze"
		validValue   = "medium"
		invalidValue = "med!um"
		validQuery   = ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592"
		invalidQuery = ".cpu.count == 2 and"
	)
	cases := []struct {
		name            string
		newSpec         v1beta1.AgentClassificationSpec
		newObjectRaw    []byte
		oldSpec         v1beta1.AgentClassificationSpec
		oldObjectRaw    []byte
		operation       admissionv1.Operation
		expectedAllowed bool
		gvr             *metav1.GroupVersionResource
	}{
		{
			name: "Test doesn't validate with right version and resource, but wrong group",
			gvr: &metav1.GroupVersionResource{
				Group:    "not the right group",
				Version:  "v1beta1",
				Resource: "agentclassifications",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and resource, wrong version",
			gvr: &metav1.GroupVersionResource{
				Group:    "agent-install.openshift.io",
				Version:  "not the right version",
				Resource: "agentclassifications",
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
			name: "Test AgentClassification Spec is valid on create",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      validQuery,
			},
			oldSpec:         v1beta1.AgentClassificationSpec{},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "Test AgentClassification label key is invalid on create",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   invalidKey,
				LabelValue: validValue,
				Query:      validQuery,
			},
			oldSpec:         v1beta1.AgentClassificationSpec{},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification label value is invalid on create",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: invalidValue,
				Query:      validQuery,
			},
			oldSpec:         v1beta1.AgentClassificationSpec{},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification label value is query error on create",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: "QUERYERROR-foo",
				Query:      validQuery,
			},
			oldSpec:         v1beta1.AgentClassificationSpec{},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification query is invalid on create",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      invalidQuery,
			},
			oldSpec:         v1beta1.AgentClassificationSpec{},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification label key is changed on update",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   "newkey",
				LabelValue: validValue,
				Query:      validQuery,
			},
			oldSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      validQuery,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification label value is changed on update",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: "newvalue",
				Query:      validQuery,
			},
			oldSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      validQuery,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test AgentClassification query is changed on update",
			newSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      validQuery,
			},
			oldSpec: v1beta1.AgentClassificationSpec{
				LabelKey:   validKey,
				LabelValue: validValue,
				Query:      ".cpu.count == 2",
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
	}

	for i := range cases {
		tc := cases[i]
		It(tc.name, func() {
			data := NewAgentClassificationValidatingAdmissionHook(createDecoder())
			newObject := &v1beta1.AgentClassification{
				Spec: tc.newSpec,
			}
			oldObject := &v1beta1.AgentClassification{
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
					Resource: "agentclassifications",
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
