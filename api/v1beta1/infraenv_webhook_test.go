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

var _ = Describe("ValidateCreate", func() {
	var (
		infraEnv     *InfraEnv
		infraEnvName = "test-infraenv"
		namespace    = "test-infraenv-namespace"
	)
	BeforeEach(func() {
		infraEnv = &InfraEnv{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infraEnvName,
				Namespace: namespace,
			},
		}
	})
	It("fails if both ClusterRef and OsImageVersion are set", func() {
		infraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "test",
			Namespace: namespace,
		}
		infraEnv.Spec.OSImageVersion = "4.14"
		warn, err := infraEnv.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("succeeds if only ClusterRef is set", func() {
		infraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "test",
			Namespace: namespace,
		}

		warn, err := infraEnv.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds if only OsImageVersion is set", func() {
		infraEnv.Spec.OSImageVersion = "4.14"
		warn, err := infraEnv.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds if neither ClusterRef or OsImageVersion is set", func() {
		warn, err := infraEnv.ValidateCreate()
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})
})

var _ = Describe("ValidateUpdate", func() {
	var (
		oldInfraEnv  *InfraEnv
		infraEnvName = "test-infraenv"
		namespace    = "test-infraenv-namespace"
	)
	BeforeEach(func() {
		oldInfraEnv = &InfraEnv{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infraEnvName,
				Namespace: namespace,
			},
		}
	})
	It("fails if ClusterRef was not set initially but added later", func() {
		newInfraEnv := oldInfraEnv.DeepCopy()
		newInfraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if ClusterRef was set initially but removed later", func() {
		oldInfraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		newInfraEnv := oldInfraEnv.DeepCopy()
		newInfraEnv.Spec.ClusterRef = nil
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})
	It("fails if ClusterRef was set and changed", func() {
		oldInfraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		newInfraEnv := oldInfraEnv.DeepCopy()
		newInfraEnv.Spec.ClusterRef = &ClusterReference{
			Name:      "new-cluster-deployment",
			Namespace: namespace,
		}
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("fails if ClusterRef.Name was set and changed", func() {
		cdRef := &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		oldInfraEnv.Spec.ClusterRef = cdRef
		newInfraEnv := oldInfraEnv.DeepCopy()
		cdRef.Name = "new-cluster-deployment"
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("fails if ClusterRef.Namespace was set and changed", func() {
		cdRef := &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		oldInfraEnv.Spec.ClusterRef = cdRef
		newInfraEnv := oldInfraEnv.DeepCopy()
		cdRef.Namespace = "new-cluster-deployment-namespace"
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("succeeds if ClusterRef was set and not changed", func() {
		cdRef := &ClusterReference{
			Name:      "cluster-deployment",
			Namespace: namespace,
		}
		oldInfraEnv.Spec.ClusterRef = cdRef
		newInfraEnv := oldInfraEnv.DeepCopy()
		newInfraEnv.Spec.SSHAuthorizedKey = "different-ssh-key"
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds if ClusterRef was not set and is not changed", func() {
		newInfraEnv := oldInfraEnv.DeepCopy()
		newInfraEnv.Spec.SSHAuthorizedKey = "different-ssh-key"
		warn, err := newInfraEnv.ValidateUpdate(oldInfraEnv)
		Expect(warn).To(BeNil())
		Expect(err).To(BeNil())
	})
})
