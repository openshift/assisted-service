package openshiftai

import (
	"bytes"
	"errors"
	"io"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/operators/amdgpu"
	"github.com/openshift/assisted-service/internal/operators/nvidiagpu"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/jq"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Manifest generation", func() {
	var (
		jqTool   *jq.Tool
		cluster  *common.Cluster
		operator *operator
	)

	// decodeManifests decodes a list of YAML documents separated by '---'.
	decodeManifests := func(manifests []byte) []any {
		reader := bytes.NewReader(manifests)
		decoder := yaml.NewDecoder(reader)
		var objects []any
		for {
			var object any
			err := decoder.Decode(&object)
			if errors.Is(err, io.EOF) {
				break
			}
			Expect(err).ToNot(HaveOccurred())
			objects = append(objects, object)
		}
		return objects
	}

	BeforeEach(func() {
		var err error

		jqTool, err = jq.NewTool().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())

		cluster = &common.Cluster{
			Cluster: models.Cluster{
				OpenshiftVersion: "4.12.0",
			},
		}

		operator = NewOpenShiftAIOperator(common.GetTestLog())
	})

	It("Generates the required OpenShift manifests", func() {
		manifests, _, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(manifests).To(HaveKey("50_openshift_ai_namespace.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_subscription.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_operatorgroup.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_setup_clusterrole.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_setup_clusterrolebinding.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_setup_serviceaccount.yaml"))
		Expect(manifests).To(HaveKey("50_openshift_ai_setup_job.yaml"))
	})

	It("Generates valid YAML", func() {
		openShiftManifests, customManifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		for _, openShiftManifest := range openShiftManifests {
			var object any
			err = yaml.Unmarshal(openShiftManifest, &object)
			Expect(err).ToNot(HaveOccurred())
		}
		_ = decodeManifests(customManifests)
	})

	It("Includes the data science cluster", func() {
		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the data science cluster:
		var clusters []any
		err = jqTool.Evaluate(
			`.[] | select(.kind == "DataScienceCluster")`,
			objects,
			&clusters,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusters).To(HaveLen(1))
	})

	It("Includes the NVIDIA GPU accelerator profile if the NVIDIA GPU operator is enabled", func() {
		// Enable the NVIDIA GPU operator:
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{
				Name: nvidiagpu.Operator.Name,
			},
		}

		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the accelerator profile:
		var profiles []any
		err = jqTool.Evaluate(
			`.[] | select(.kind == "AcceleratorProfile") | .metadata.name`,
			objects,
			&profiles,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(profiles).To(ContainElement("nvidia-gpu"))
	})

	It("Doesn't include the NVIDIA GPU accelerator profile if the NVIDIA GPU operator isn't enabled", func() {
		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the accelerator profile:
		var names []any
		err = jqTool.Evaluate(
			`.[] | select(.kind == "AcceleratorProfile") | .metadata.name`,
			objects,
			&names,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(names).ToNot(ContainElement("nvidia-gpu"))
	})

	It("Includes the AMD GPU accelerator profile if the AMD GPU operator is enabled", func() {
		// Enable the AMD GPU operator:
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{
				Name: amdgpu.Operator.Name,
			},
		}

		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the accelerator profile:
		var names []string
		err = jqTool.Evaluate(
			`.[] | select(.kind == "AcceleratorProfile") | .metadata.name`,
			objects,
			&names,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(names).To(ContainElement("amd-gpu"))
	})

	It("Doesn't include the AMD GPU accelerator profile if the AMD GPU operator isn't enabled", func() {
		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the accelerator profile:
		var names []string
		err = jqTool.Evaluate(
			`.[] | select(.kind == "AcceleratorProfile") | .metadata.name`,
			objects,
			&names,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(names).ToNot(ContainElement("amd-gpu"))
	})

	It("Includes all accelerator profiles if all GPU operators are enabled", func() {
		// Enable the AMD GPU operator:
		cluster.MonitoredOperators = []*models.MonitoredOperator{
			{
				Name: nvidiagpu.Operator.Name,
			},
			{
				Name: amdgpu.Operator.Name,
			},
		}
		// Convert the manifests into a list of objects:
		_, manifests, err := operator.GenerateManifests(cluster)
		Expect(err).ToNot(HaveOccurred())
		objects := decodeManifests(manifests)

		// Try to find the accelerator profile:
		var names []string
		err = jqTool.Evaluate(
			`.[] | select(.kind == "AcceleratorProfile") | .metadata.name`,
			objects,
			&names,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(names).To(ConsistOf("nvidia-gpu", "amd-gpu"))
	})
})
