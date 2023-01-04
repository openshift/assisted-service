package v1beta1

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	apiserver "github.com/openshift/generic-admission-server/pkg/apiserver"
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
	decoder, err := admission.NewDecoder(scheme)
	Expect(err).To(BeNil())
	return decoder
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
