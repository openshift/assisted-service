package v1beta1

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	apiserver "github.com/openshift/generic-admission-server/pkg/apiserver"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func createDecoder() *admission.Decoder {
	scheme := runtime.NewScheme()
	err := v1beta1.AddToScheme(scheme)
	Expect(err).To(BeNil())
	return admission.NewDecoder(scheme)
}

var _ = Describe("infraenv web hook init", func() {
	It("ValidatingResource", func() {
		data := NewInfraEnvValidatingAdmissionHook(createDecoder())
		expectedPlural := schema.GroupVersionResource{
			Group:    "admission.agentinstall.openshift.io",
			Version:  "v1",
			Resource: "infraenvvalidators",
		}
		expectedSingular := "infraenvvalidator"

		plural, singular := data.ValidatingResource()
		Expect(plural).To(Equal(expectedPlural))
		Expect(singular).To(Equal(expectedSingular))

	})

	It("Initialize", func() {
		data := NewInfraEnvValidatingAdmissionHook(createDecoder())
		err := data.Initialize(nil, nil)
		Expect(err).To(BeNil())
	})

	It("Check implements interface ", func() {
		var hook interface{} = NewInfraEnvValidatingAdmissionHook(createDecoder())
		_, ok := hook.(apiserver.ValidatingAdmissionHookV1)
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("infraenv web validate", func() {
	cases := []struct {
		name            string
		newSpec         v1beta1.InfraEnvSpec
		oldSpec         v1beta1.InfraEnvSpec
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
				Resource: "infraenvs",
			},
			expectedAllowed: true,
		},
		{
			name: "Test doesn't validate with right group and resource, wrong version",
			gvr: &metav1.GroupVersionResource{
				Group:    "agent-install.openshift.io",
				Version:  "not the right version",
				Resource: "infraenvs",
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
			name: "Test InfraEnv.Spec.ClusterRef is immutable",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv.Spec.ClusterRef.Name is immutable",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "oldNamespace",
				},
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv.Spec.ClusterRef.Namespace is immutable",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv.Spec.ClusterRef can't change from nil to not nil",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: nil,
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "newNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv.Spec.ClusterRef can't change from not nil to nil",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "newNamespace",
				},
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: nil,
			},
			operation:       admissionv1.Update,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv update does not fail when ClusterReference is set and remains the same",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "oldName",
					Namespace: "oldNamespace",
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test InfraEnv update does not fail when ClusterReference is nil and remains the same",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: nil,
			},
			oldSpec: v1beta1.InfraEnvSpec{
				ClusterRef: nil,
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
		},
		{
			name: "Test can't specify both Spec.ClusterRef and Spec.OSImageVersion",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "newName",
				},
				OSImageVersion: "4.14",
			},
			operation:       admissionv1.Create,
			expectedAllowed: false,
		},
		{
			name: "Test InfraEnv create does not fail when only Spec.ClusterRef is specified",
			newSpec: v1beta1.InfraEnvSpec{
				ClusterRef: &v1beta1.ClusterReference{
					Name:      "newName",
					Namespace: "newName",
				},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
		{
			name: "Test InfraEnv create does not fail when only Spec.OSImageVersion is specified",
			newSpec: v1beta1.InfraEnvSpec{
				OSImageVersion: "4.14",
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
		},
	}

	for i := range cases {
		tc := cases[i]
		It(tc.name, func() {
			data := NewInfraEnvValidatingAdmissionHook(createDecoder())
			newObject := &v1beta1.InfraEnv{
				Spec: tc.newSpec,
			}
			oldObject := &v1beta1.InfraEnv{
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
					Resource: "infraenvs",
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
