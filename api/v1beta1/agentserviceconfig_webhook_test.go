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
		asc       *AgentServiceConfig
		name      = "my-acs"
		namespace = "my-namespace"
	)
	BeforeEach(func() {
		asc = &AgentServiceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
	})
	Context("when validating creation", func() {
		It("succeeds", func() {
			Expect(asc.ValidateCreate(ctx, asc)).To(Succeed())
		})
	})

	Context("when validating update", func() {
		It("succeeds when no annotations are set in old and new object", func() {
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			Expect(asc.ValidateUpdate(ctx, asc, newAsc)).To(Succeed())
		})
		It("succeeds when no forbidden annotations are added in the new object", func() {
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						"foobar.openshift.io/annotation": "value",
					},
				},
			}
			Expect(asc.ValidateUpdate(ctx, asc, newAsc)).To(Succeed())
		})
		It("succeeds when both old and new objects have the same forbidden annotation", func() {
			asc.Annotations[PVCPrefixAnnotation] = "my-prefix-"
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						PVCPrefixAnnotation: "my-prefix-",
					},
				},
			}
			Expect(asc.ValidateUpdate(ctx, asc, newAsc)).To(Succeed())
		})
		It("succeeds when both old and new objects have secrets prefix forbidden annotation", func() {
			asc.Annotations[SecretsPrefixAnnotation] = "my-prefix-"
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						SecretsPrefixAnnotation: "my-prefix-",
					},
				},
			}
			Expect(asc.ValidateUpdate(ctx, asc, newAsc)).To(Succeed())
		})
		It("fails when secrets prefix annotation is removed", func() {
			asc.Annotations[SecretsPrefixAnnotation] = "my-prefix-"
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			_, err := asc.ValidateUpdate(ctx, asc, newAsc)
			Expect(err).To(HaveOccurred())
		})
		It("fails when pvc prefix annotation is removed", func() {
			asc.Annotations[PVCPrefixAnnotation] = "my-prefix-"
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
			_, err := asc.ValidateUpdate(ctx, asc, newAsc)
			Expect(err).To(HaveOccurred())
		})
		It("fails when secrets prefix annotation is added", func() {
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						SecretsPrefixAnnotation: "my-prefix-",
					},
				},
			}
			_, err := asc.ValidateUpdate(ctx, asc, newAsc)
			Expect(err).To(HaveOccurred())
		})
		It("fails when PVC prefix annotation is added", func() {
			newAsc := &AgentServiceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					Annotations: map[string]string{
						PVCPrefixAnnotation: "my-prefix-",
					},
				},
			}
			_, err := asc.ValidateUpdate(ctx, asc, newAsc)
			Expect(err).To(HaveOccurred())
		})
	})
})
