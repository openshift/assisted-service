package v1beta1

import (
	"encoding/json"
	"time"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("ACI mutator hook", func() {
	cases := []struct {
		name            string
		newSpec         hiveext.AgentClusterInstallSpec
		conditions      []hivev1.ClusterInstallCondition
		oldSpec         hiveext.AgentClusterInstallSpec
		newObjectRaw    []byte
		oldObjectRaw    []byte
		operation       admissionv1.Operation
		expectedAllowed bool
		patched         bool
		patchedValue    bool
		deleted         bool
		gvr             *metav1.GroupVersionResource
	}{
		{
			name: "ACI create with userManagedNetworking not set with SNO --> patch to true",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			patched:         true,
			patchedValue:    true,
		},
		{
			name: "ACI create with userManagedNetworking not set with multinodes --> patch to false",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 3},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			patched:         true,
			patchedValue:    false,
		},
		{
			name: "ACI create with userManagedNetworking set to false with SNO --> no change",
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{UserManagedNetworking: swag.Bool(false)},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			operation:       admissionv1.Create,
			expectedAllowed: true,
			patched:         false,
		},
		{
			name: "ACI updated after install start --> no change",
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{
					UserManagedNetworking: swag.Bool(true),
				},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			conditions: []hivev1.ClusterInstallCondition{
				{
					Type:   hiveext.ClusterCompletedCondition,
					Reason: hiveext.ClusterInstallationInProgressReason,
				},
			},
			operation:       admissionv1.Update,
			expectedAllowed: true,
			patched:         false,
		},
		{
			name: "ACI updated after install start --> no change",
			oldSpec: hiveext.AgentClusterInstallSpec{
				Networking:            hiveext.Networking{},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			newSpec: hiveext.AgentClusterInstallSpec{
				Networking: hiveext.Networking{
					UserManagedNetworking: swag.Bool(true),
				},
				ProvisionRequirements: hiveext.ProvisionRequirements{ControlPlaneAgents: 1},
			},
			operation:       admissionv1.Update,
			deleted:         true,
			expectedAllowed: true,
			patched:         false,
		},
	}

	for i := range cases {
		tc := cases[i]
		It(tc.name, func() {
			data := NewAgentClusterInstallMutatingAdmissionHook(createDecoder())
			newObject := &hiveext.AgentClusterInstall{
				Spec: tc.newSpec,
			}
			newObject.Status.Conditions = tc.conditions
			oldObject := &hiveext.AgentClusterInstall{
				Spec: tc.oldSpec,
			}

			if tc.deleted {
				t := metav1.NewTime(time.Now())
				newObject.DeletionTimestamp = &t
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

			response := data.Admit(request)
			Expect(response.Allowed).To(Equal(tc.expectedAllowed))
			if tc.patched {
				Expect(decodeUserManagedNetworking(response.Patch)).To(Equal(tc.patchedValue))
			}
		})
	}

})

func decodeUserManagedNetworking(body []byte) bool {
	var pl = make([]map[string]interface{}, 0)
	err := json.Unmarshal(body, &pl)
	Expect(err).NotTo(HaveOccurred())
	Expect(pl).NotTo(BeEmpty())
	return pl[0]["value"].(bool)
}
