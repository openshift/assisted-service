package controllers

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("propagateInfraEnvNodeLabels", func() {
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

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

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
				map[string]string{inheritedNodeLabelsAnnotation: "site"},
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("site"))
			Expect(agent.Spec.NodeLabels["custom"]).To(Equal("keep-me"))
			Expect(agent.GetAnnotations()).NotTo(HaveKey(inheritedNodeLabelsAnnotation))
		})
	})

	Context("when InfraEnv has an empty propagation annotation", func() {
		It("does not propagate any labels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{propagateNodeLabelsAnnotation: ""},
			)
			agent := newAgentForLabels(nil, nil)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
		})
	})

	Context("when InfraEnv has a valid propagation annotation", func() {
		It("propagates only designated labels to Agent.spec.nodeLabels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "internal": "do-not-propagate"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "site-a"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "zone-1"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("internal"))

			inherited := agent.GetAnnotations()[inheritedNodeLabelsAnnotation]
			Expect(inherited).To(ContainSubstring("site"))
			Expect(inherited).To(ContainSubstring("zone"))
		})

		It("preserves existing user-set nodeLabels", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"custom-label": "user-value"},
				nil,
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["custom-label"]).To(Equal("user-value"))
		})

		It("does not overwrite user-set labels on conflict", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "user-site-b"},
				nil,
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(agent.Spec.NodeLabels["site"]).To(Equal("user-site-b"))
			Expect(modified).To(BeFalse())
			Expect(agent.GetAnnotations()).NotTo(HaveKey(inheritedNodeLabelsAnnotation))
		})

		It("updates inherited labels when InfraEnv value changes", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-b"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{inheritedNodeLabelsAnnotation: "site"},
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-b"))
		})

		It("removes labels dropped from propagation list", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{propagateNodeLabelsAnnotation: "site"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{inheritedNodeLabelsAnnotation: "site,zone"},
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", "site-a"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("zone"))
		})

		It("skips label keys not present on InfraEnv", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{propagateNodeLabelsAnnotation: "site,missing-key"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("missing-key"))
		})

		It("handles whitespace in annotation values", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{propagateNodeLabelsAnnotation: " site , zone "},
			)
			agent := newAgentForLabels(nil, nil)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("zone-1"))
		})

		It("is idempotent when no changes needed", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "region": "us-east"},
				map[string]string{propagateNodeLabelsAnnotation: "site,zone,region"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1", "region": "us-east"},
				map[string]string{inheritedNodeLabelsAnnotation: "region,site,zone"},
			)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

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
				map[string]string{propagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,topology.kubernetes.io/region,site"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := propagateInfraEnvNodeLabels(log, infraEnv, agent)

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
				map[string]string{propagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,site"},
			)
			infraEnvB := newInfraEnvForLabels(
				map[string]string{"topology.kubernetes.io/zone": "zone-b", "site": "site-b"},
				map[string]string{propagateNodeLabelsAnnotation: "topology.kubernetes.io/zone,site"},
			)

			agentA := newAgentForLabels(nil, nil)
			agentB := newAgentForLabels(nil, nil)

			propagateInfraEnvNodeLabels(log, infraEnvA, agentA)
			propagateInfraEnvNodeLabels(log, infraEnvB, agentB)

			Expect(agentA.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agentA.Spec.NodeLabels["topology.kubernetes.io/zone"]).To(Equal("zone-a"))
			Expect(agentB.Spec.NodeLabels["site"]).To(Equal("site-b"))
			Expect(agentB.Spec.NodeLabels["topology.kubernetes.io/zone"]).To(Equal("zone-b"))
		})
	})

	Context("corner cases and edge conditions", func() {
		It("propagates labels with empty string values", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "", "zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("site", ""))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "zone-1"))
		})

		It("handles duplicate keys in propagation annotation", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone,site"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("zone-1"))
			inherited := agent.GetAnnotations()[InheritedNodeLabelsAnnotation]
			Expect(strings.Count(inherited, "site")).To(Equal(1), "inherited annotation should not contain duplicate keys")
		})

		It("handles long label keys (253 chars)", func() {
			longKey := strings.Repeat("a", 253)
			infraEnv := newInfraEnvForLabels(
				map[string]string{longKey: "value"},
				map[string]string{PropagateNodeLabelsAnnotation: longKey},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue(longKey, "value"))
		})

		It("handles label keys with slashes and dots", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{
					"app.kubernetes.io/part-of": "my-app",
					"node.openshift.io/os_id":   "rhcos",
					"failure-domain.beta/zone":  "us-east-1a",
				},
				map[string]string{PropagateNodeLabelsAnnotation: "app.kubernetes.io/part-of,node.openshift.io/os_id,failure-domain.beta/zone"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["app.kubernetes.io/part-of"]).To(Equal("my-app"))
			Expect(agent.Spec.NodeLabels["node.openshift.io/os_id"]).To(Equal("rhcos"))
			Expect(agent.Spec.NodeLabels["failure-domain.beta/zone"]).To(Equal("us-east-1a"))
		})

		It("self-heals when inherited annotation exists but nodeLabels are missing", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(
				nil,
				map[string]string{InheritedNodeLabelsAnnotation: "site,zone"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels["site"]).To(Equal("site-a"))
			Expect(agent.Spec.NodeLabels["zone"]).To(Equal("zone-1"))
		})

		It("handles InfraEnv with nil labels map but propagation annotation set", func() {
			infraEnv := newInfraEnvForLabels(
				nil,
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
		})

		It("handles annotation value with only commas", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"site": "site-a"},
				map[string]string{PropagateNodeLabelsAnnotation: ",,,,"},
			)
			agent := newAgentForLabels(nil, nil)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeFalse())
			Expect(agent.Spec.NodeLabels).To(BeEmpty())
		})

		It("removes inherited label when key is removed from InfraEnv labels but still in propagation list", func() {
			infraEnv := newInfraEnvForLabels(
				map[string]string{"zone": "zone-1"},
				map[string]string{PropagateNodeLabelsAnnotation: "site,zone"},
			)
			agent := newAgentForLabels(
				map[string]string{"site": "site-a", "zone": "zone-1"},
				map[string]string{InheritedNodeLabelsAnnotation: "site,zone"},
			)

			modified := PropagateInfraEnvNodeLabels(log, infraEnv, agent)

			Expect(modified).To(BeTrue())
			Expect(agent.Spec.NodeLabels).NotTo(HaveKey("site"))
			Expect(agent.Spec.NodeLabels).To(HaveKeyWithValue("zone", "zone-1"))
			inherited := agent.GetAnnotations()[InheritedNodeLabelsAnnotation]
			Expect(inherited).To(Equal("zone"))
		})
	})
})

var _ = Describe("getPropagationKeys", func() {
	It("returns nil when no annotations", func() {
		infraEnv := newInfraEnvForLabels(nil, nil)
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("returns nil for empty annotation", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{propagateNodeLabelsAnnotation: ""})
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("returns nil for whitespace-only annotation", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{propagateNodeLabelsAnnotation: "  "})
		Expect(getPropagationKeys(infraEnv)).To(BeNil())
	})

	It("parses a single key", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{propagateNodeLabelsAnnotation: "site"})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site"}))
	})

	It("parses multiple keys with whitespace", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{propagateNodeLabelsAnnotation: " site , zone , region "})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site", "zone", "region"}))
	})

	It("handles trailing comma", func() {
		infraEnv := newInfraEnvForLabels(nil, map[string]string{propagateNodeLabelsAnnotation: "site,zone,"})
		Expect(getPropagationKeys(infraEnv)).To(Equal([]string{"site", "zone"}))
	})
})

var _ = Describe("parseCommaSeparatedKeys", func() {
	It("returns nil for empty string", func() {
		Expect(parseCommaSeparatedKeys("")).To(BeNil())
	})

	It("returns nil for only commas", func() {
		Expect(parseCommaSeparatedKeys(",,,,")).To(BeNil())
	})

	It("returns nil for commas with whitespace", func() {
		Expect(parseCommaSeparatedKeys(" , , , ")).To(BeNil())
	})

	It("handles keys containing slashes and dots", func() {
		result := parseCommaSeparatedKeys("app.kubernetes.io/name,topology.kubernetes.io/zone")
		Expect(result).To(Equal([]string{"app.kubernetes.io/name", "topology.kubernetes.io/zone"}))
	})

	It("deduplicates implicitly via caller but returns raw list", func() {
		result := parseCommaSeparatedKeys("site,zone,site")
		Expect(result).To(Equal([]string{"site", "zone", "site"}))
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
