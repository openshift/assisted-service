/*
Copyright 2020.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ValidateUpdate", func() {
	var (
		oldAgent  *Agent
		agentName = "test-agent"
		namespace = "test-agent-namespace"
	)
	BeforeEach(func() {
		oldAgent = &Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentName,
				Namespace: namespace,
			},
		}
	})
	It("fails when Agent is in installing state and ClusterReference changes", func() {
		oldAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "old-cluster-deployment",
			Namespace: namespace,
		}
		oldAgent.Status = AgentStatus{DebugInfo: DebugInfo{State: models.HostStatusInstalling}}
		newAgent := oldAgent.DeepCopy()
		newAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "new-cluster-deployment",
			Namespace: namespace,
		}
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("succeeds when Agent has no state and ClusterReference changes", func() {
		oldAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "old-cluster-deployment",
			Namespace: namespace,
		}
		newAgent := oldAgent.DeepCopy()
		newAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "new-cluster-deployment",
			Namespace: namespace,
		}
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("succeeds when Agent is in known state and ClusterReference changes", func() {
		oldAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "old-cluster-deployment",
			Namespace: namespace,
		}
		oldAgent.Status = AgentStatus{DebugInfo: DebugInfo{State: models.HostStatusKnown}}
		newAgent := oldAgent.DeepCopy()
		newAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "new-cluster-deployment",
			Namespace: namespace,
		}
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds if Agent's ClusterReference is nil and remains the same", func() {
		newAgent := oldAgent.DeepCopy()
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds if Agent's ClusterReference is set and remains the same", func() {
		oldAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "old-cluster-deployment",
			Namespace: namespace,
		}
		newAgent := oldAgent.DeepCopy()
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("succeeds if Agent's ClusterReference is set and remains the same while installing", func() {
		oldAgent.Spec.ClusterDeploymentName = &ClusterReference{
			Name:      "old-cluster-deployment",
			Namespace: namespace,
		}
		oldAgent.Status = AgentStatus{DebugInfo: DebugInfo{State: models.HostStatusInstalling}}
		newAgent := oldAgent.DeepCopy()
		warns, err := newAgent.ValidateUpdate(oldAgent)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
})
