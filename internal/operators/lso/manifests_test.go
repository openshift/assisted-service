package lso

import (
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"sigs.k8s.io/yaml"
)

var _ = Describe("LSO manifest generation", func() {
	var (
		mockCtrl           *gomock.Controller
		mockVersionHandler *versions.MockHandler
	)

	mockCtrl = gomock.NewController(GinkgoT())
	mockVersionHandler = versions.NewMockHandler(mockCtrl)
	operator := NewLSOperator(common.GetTestLog(), mockVersionHandler)
	clusterId := strfmt.UUID(uuid.New().String())
	cluster := common.Cluster{Cluster: models.Cluster{
		OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
		CPUArchitecture:  common.X86CPUArchitecture,
		ID:               &clusterId,
	}}

	Context("LSO Manifest", func() {
		It("Create LSO Manifests", func() {
			mockVersionHandler.EXPECT().GetDefaultReleaseImage(gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil)

			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-lso_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-lso_operator_group.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-lso_subscription.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Extra manifest on pre release", func() {
			cluster.OpenshiftVersion = "4.14"
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				OpenshiftVersion: swag.String("4.13"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.4-x86_64"),
				Version:          swag.String("4.13.4"),
				CPUArchitectures: []string{common.X86CPUArchitecture},
			}
			mockVersionHandler.EXPECT().GetDefaultReleaseImage(gomock.Any()).Return(releaseImage, nil)
			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(4))

			Expect(yaml.YAMLToJSON(openshiftManifests["50_openshift-lso_catalog_source.yaml"])).NotTo(HaveLen(0))
			Expect(yaml.YAMLToJSON(openshiftManifests["50_openshift-lso_ns.yaml"])).NotTo(HaveLen(0))
			Expect(yaml.YAMLToJSON(openshiftManifests["50_openshift-lso_operator_group.yaml"])).NotTo(HaveLen(0))
			Expect(yaml.YAMLToJSON(openshiftManifests["50_openshift-lso_subscription.yaml"])).NotTo(HaveLen(0))

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
