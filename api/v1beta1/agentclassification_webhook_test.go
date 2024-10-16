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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	validKey      = "size"
	invalidKey    = "s!ze"
	keyWithPrefix = "a/size"
	validValue    = "medium"
	invalidValue  = "med!um"
	validQuery    = ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592"
	invalidQuery  = ".cpu.count == 2 and"
)

var _ = Describe("ValidateCreate", func() {

	var (
		agentClassification     *AgentClassification
		agentClassificationName = "agent-classification-name"
		namespace               = "agent-classification-namespace"
	)
	BeforeEach(func() {
		agentClassification = &AgentClassification{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentClassificationName,
				Namespace: namespace,
			},
		}
	})
	It("fails if label key is invalid", func() {
		agentClassification.Spec.LabelKey = invalidKey
		agentClassification.Spec.LabelValue = validValue
		agentClassification.Spec.Query = validQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label key has a prefix", func() {
		agentClassification.Spec.LabelKey = keyWithPrefix
		agentClassification.Spec.LabelValue = validValue
		agentClassification.Spec.Query = validQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label value is invalid", func() {
		agentClassification.Spec.LabelKey = validKey
		agentClassification.Spec.LabelValue = invalidValue
		agentClassification.Spec.Query = validQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label value starts with QUERYERROR", func() {
		agentClassification.Spec.LabelKey = validKey
		agentClassification.Spec.LabelValue = "QUERYERROR-foo"
		agentClassification.Spec.Query = validQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label query is invalid", func() {
		agentClassification.Spec.LabelKey = validKey
		agentClassification.Spec.LabelValue = validValue
		agentClassification.Spec.Query = invalidQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if everything is invalid", func() {
		agentClassification.Spec.LabelKey = invalidKey
		agentClassification.Spec.LabelValue = invalidValue
		agentClassification.Spec.Query = invalidQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("succeeds if everything is valid", func() {
		agentClassification.Spec.LabelKey = validKey
		agentClassification.Spec.LabelValue = validValue
		agentClassification.Spec.Query = validQuery
		warn, err := agentClassification.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})

})

var _ = Describe("ValidateUpdate", func() {

	var (
		oldAgentClassification  *AgentClassification
		agentClassificationName = "agent-classification-name"
		namespace               = "agent-classification-namespace"
	)
	BeforeEach(func() {
		oldAgentClassification = &AgentClassification{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentClassificationName,
				Namespace: namespace,
			},
		}
	})
	It("fails if label key is changed", func() {
		oldAgentClassification.Spec.LabelKey = validKey
		oldAgentClassification.Spec.LabelValue = validValue
		oldAgentClassification.Spec.Query = validQuery
		newAgentClassification := oldAgentClassification.DeepCopy()
		newAgentClassification.Spec.LabelKey = "newKey"
		warn, err := newAgentClassification.ValidateUpdate(oldAgentClassification)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label value is changed", func() {
		oldAgentClassification.Spec.LabelKey = validKey
		oldAgentClassification.Spec.LabelValue = validValue
		oldAgentClassification.Spec.Query = validQuery
		newAgentClassification := oldAgentClassification.DeepCopy()
		newAgentClassification.Spec.LabelValue = "newvalue"
		warn, err := newAgentClassification.ValidateUpdate(oldAgentClassification)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if label query is changed", func() {
		oldAgentClassification.Spec.LabelKey = validKey
		oldAgentClassification.Spec.LabelValue = validValue
		oldAgentClassification.Spec.Query = ".cpu.count == 2"
		newAgentClassification := oldAgentClassification.DeepCopy()
		newAgentClassification.Spec.Query = validQuery
		warn, err := newAgentClassification.ValidateUpdate(oldAgentClassification)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
})
