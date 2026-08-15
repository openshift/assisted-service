package controllers

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PropagateInfraEnvNodeLabels", func() {
	var log logrus.FieldLogger

	BeforeEach(func() {
		log = logrus.New()
	})

	Context("when InfraEnv has no propagation annotation", func() {
		It("does not modify the agent", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				nil,
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
		})

		It("removes previously inherited labels when annotation is removed", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				nil,
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "custom": "keep-me"},
				map[string]string{InheritedNodeLabelsAnnotation: "site"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("site"))
			Expect(agent.Spec.NodeLabels["custom"]).To(Equal("keep-me"))
			Expect(agent.GetAnnotations()).NotTo(HaveKey(InheritedNodeLabelsAnnotation))
		})
	})

	Context("when InfraEnv has an empty propagation annotation", func() {
		It("does not propagate any labels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: ""},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
		})
	})

	Context("when InfraEnv has a valid propagation annotation", func() {
		It("propagates only designated labels to Agent.spec.nodeLabels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "internal": "do-not-propagate"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "site-a"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "zone-1"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("internal"))

			inherited := agent.GetAnnotations()[InheritedNodeLabelsAnnotation]
			Expect(inherited).To(ContainSubstring("site"))
			Expect(inherited).To(ContainSubstring("zone"))
		})

		It("preserves existing user-set nodeLabels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"custom-label": "user-value"},
				nil,
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["custom-label"]).To(Equal("user-value"))
		})

		It("does not overwrite user-set labels on conflict", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "user-site-b"},
				nil,
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(agent.Spec.NodeLabels["site"]).To(Equal("user-site-b"))
			Expect(modified).To(BeFalse())
			Expect(agent.GetAnnotations()).NotTo(HaveKey(InheritedNodeLabelsAnnotation))
		})

		It("updates inherited labels when InfraEnv value changes", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-b"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{InheritedNodeLabelsAnnotation: "site"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-b"))
		})

		It("removes labels dropped from propagation list", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{InheritedNodeLabelsAnnotation: "site,zone"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "site-a"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("zone"))
		})

		It("skips label keys not present on InfraEnv", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,missing-key"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("missing-key"))
		})

		It("handles whitespace in annotation values", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: " site , zone "},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("zone-1"))
		})

		It("is idempotent when no changes needed", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "region": "us-east"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone,region"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "region": "us-east"},
				map[string]string{InheritedNodeLabelsAnnotation: "region,site,zone"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("zone-1"))
			Expect(agent.Spec.NodeLabels["region"]).To(Equal("us-east"))
		})

		It("propagates topology.kubernetes.io labels correctly", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{
					"topology.kubernetes.io/zone":   "us-east-1a",
					"topology.kubernetes.io/region": "us-east-1",
					"site":                          "nyc-dc1",
				},
				map[string]string{PropagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,topology.kubernetes.io/region,site"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["topology.kubernetes.io/zone"]).To(Equal("us-east-1a"))
			Expect(agent.Spec.NodeLabels["topology.kubernetes.io/region"]).To(Equal("us-east-1"))
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("nyc-dc1"))
		})
	})

	Context("multi-site deployment scenario", func() {
		It("propagates different labels from different InfraEnvs to different agents", func() {
			infraEnvA := newInfraEnvForLabels(
				map[string]string{"topology.kubernetes.io/zone": "zone-a", "site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,site"},
			)
			infraEnvB := newInfraEnvForLabels(
				map[string]string{"topology.kubernetes.io/zone": "zone-b", "site": "site-b"},
				map[string]string{PropagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,site"},
			)

			agentA := newAgentForLabels(nil, nil)
			agentB := newAgentForLabels(nil, nil)

			PropagateInfraEnvNodeLabels(log, infraEnvA, agentA)
			PropagateInfraEnvNodeLabels(log, infraEnvB, agentB)

			Expect(agentA.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agentA.Spec.NodeLabels["topology.kubernetes.io/zone"]).To(Equal("zone-a"))
			Expect(agentB.Spec.NodeLabels["site"]).To(Equal("site-b"))
			Expect(agentB.Spec.NodeLabels["topology.kubernetes.io/zone"]).To(Equal("zone-b"))
		})
	})
})

var _ = Describe("getPropagationKeys", func() {
	It("returns nil when no annotations", func() {
		infraEnv := newInfraEnvForLabels(nil, nil)
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("returns nil for empty annotation", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{PropagateNodeLabelsAnnotation: ""})
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("returns nil for whitespace-only annotation", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{PropagateNodeLabelsAnnotation: "  "})
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("parses a single key", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{PropagateNodeLabelsAnnotation: "site"})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site"}))
	})

	It("parses multiple keys with whitespace", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{PropagateNodeLabelsAnnotation: " site , zone , region "})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site", "zone", "region"}))
	})

	It("handles trailing comma", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{PropagateNodeLabelsAnnotation: "site,zone,"})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site", "zone"}))
	})
})

func newInfraEnvForLabels(labels map[string]string, annotations map[string]string) *aiv1beta1.InfraEnv {
	return &aiv1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-infraenv",
			Namespace:   "test-ns",
			Labels:      labels,
			Annotations: annotations,
		},
	}
}

func newAgentForLabels(nodeLabels map[string]string, annotations map[string]string) *aiv1beta1.Agent {
	return &aiv1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-agent",
			Namespace:   "test-ns",
			Annotations: annotations,
		},
		Spec: aiv1beta1.AgentSpec{
			NodeLabels: nodeLabels,
		},
	}
}
