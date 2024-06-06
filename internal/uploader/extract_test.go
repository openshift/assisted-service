package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/versions"
	eventModels "github.com/openshift/assisted-service/pkg/uploader/models"
)

var _ = Describe("Trying to extract events", func() {
	var buffer *bytes.Buffer

	When("the reader is nil", func() {
		It("should not fail", func() {
			events, err := ExtractEvents(nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(len(events)).To(Equal(0))
		})
	})

	When("the reader is empty", func() {
		BeforeEach(func() {
			buffer = &bytes.Buffer{}
		})

		It("should not fail", func() {
			events, err := ExtractEvents(buffer)

			Expect(err).NotTo(HaveOccurred())
			Expect(len(events)).To(Equal(0))
		})
	})

	When("the reader contains random data", func() {
		BeforeEach(func() {
			buffer = bytes.NewBufferString("whatever")
		})

		It("should fail", func() {
			_, err := ExtractEvents(buffer)
			Expect(err).To(HaveOccurred())
		})
	})

	var (
		gz *gzip.Writer
		tw *tar.Writer
	)

	When("the reader contains valid data", func() {
		BeforeEach(func() {
			buffer = &bytes.Buffer{}
			gz = gzip.NewWriter(buffer)
			tw = tar.NewWriter(gz)
		})

		Context("of 1 cluster", func() {
			clusterID := strfmt.UUID("cluster1")

			var config Config

			When("the config is incomplete", func() {
				BeforeEach(func() {
					config = Config{
						DeploymentType: "depType",
						Versions: versions.Versions{
							SelfVersion:    "1.2.3",
							InstallerImage: "4.5.6",
						},
					}

					metadataFile(tw, &clusterID, config)

					Expect(tw.Close()).To(Succeed())
					Expect(gz.Close()).To(Succeed())
				})

				It("should find this single metadata information for this cluster", func() {
					events, err := ExtractEvents(buffer)

					Expect(err).NotTo(HaveOccurred())
					Expect(len(events)).To(Equal(1))
					Expect(events[0]).To(Equal(generateExpectedEvents(clusterID.String(), config)))
				})
			})

			When("the config is complete", func() {
				BeforeEach(func() {
					config = Config{
						DeploymentType:         "depType",
						DeploymentVersion:      "depVersion",
						AssistedServiceVersion: "abcdef",
						Versions: versions.Versions{
							SelfVersion:     "1.2.3",
							AgentDockerImg:  "4.5.6",
							InstallerImage:  "7.8.9",
							ControllerImage: "a.b.c",
						},
					}

					metadataFile(tw, &clusterID, config)

					Expect(tw.Close()).To(Succeed())
					Expect(gz.Close()).To(Succeed())
				})

				It("should find this single metadata information for this cluster", func() {
					events, err := ExtractEvents(buffer)

					Expect(err).NotTo(HaveOccurred())
					Expect(len(events)).To(Equal(1))
					Expect(events[0]).To(Equal(generateExpectedEvents(clusterID.String(), config)))
				})
			})
		})

		Context("of 2 different clusters", func() {
			clusterID1 := strfmt.UUID("cluster1")
			clusterID2 := strfmt.UUID("cluster2")

			var config1, config2 Config

			When("the configs are complete", func() {
				BeforeEach(func() {
					config1 = Config{
						DeploymentType:         "SaaS",
						DeploymentVersion:      "1.1.1",
						AssistedServiceVersion: "latest",
						Versions: versions.Versions{
							SelfVersion:     "quay.io/app-sre/assisted-service:latest",
							AgentDockerImg:  "quay.io/edge-infrastructure/assisted-installer-agent:latest",
							InstallerImage:  "quay.io/edge-infrastructure/assisted-installer:latest",
							ControllerImage: "quay.io/edge-infrastructure/assisted-installer-controller:latest",
						},
					}

					config2 = Config{
						DeploymentType:         "SaaS",
						DeploymentVersion:      "Unknown",
						AssistedServiceVersion: "2455b9bb394f6fa896131e2a97b34b8b53a23653",
						Versions: versions.Versions{
							SelfVersion:     "quay.io/app-sre/assisted-service:2455b9bb394f6fa896131e2a97b34b8b53a23653",
							AgentDockerImg:  "registry.redhat.io/rhai-tech-preview/assisted-installer-agent-rhel8:v1.0.0-315",
							InstallerImage:  "registry.redhat.io/rhai-tech-preview/assisted-installer-rhel8:v1.0.0-340",
							ControllerImage: "registry.redhat.io/rhai-tech-preview/assisted-installer-reporter-rhel8:v1.0.0-418",
						},
					}

					metadataFile(tw, &clusterID1, config1)
					metadataFile(tw, &clusterID2, config2)

					Expect(tw.Close()).To(Succeed())
					Expect(gz.Close()).To(Succeed())
				})

				It("should find both metadata information for each cluster", func() {
					events, err := ExtractEvents(buffer)

					Expect(err).NotTo(HaveOccurred())
					Expect(len(events)).To(Equal(2))
					Expect(events).To(Equal([]eventModels.Events{
						generateExpectedEvents(clusterID1.String(), config1),
						generateExpectedEvents(clusterID2.String(), config2),
					}))
				})
			})
		})
	})

})
