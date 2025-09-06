package ignition

import (
	"context"
	"os"
	"path/filepath"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("OVE Ignition", func() {
	var (
		ctrl                 *gomock.Controller
		mockExecuter         *executer.MockExecuter
		mockMirrorRegistries *mirrorregistries.MockServiceMirrorRegistriesConfigBuilder
		mockInstallerCache   *installercache.MockInstallerCache
		mockVersionsHandler  *versions.MockHandler
		mockEvents           *eventsapi.MockHandler
		ctx                  context.Context
		log                  logrus.FieldLogger
		workDir              string
		infraEnv             *common.InfraEnv
		mockRelease          *installercache.Release
		tempInstallerFile    *os.File
		generator            *OVEIgnitionGenerator
		clusterVersion       string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		mockMirrorRegistries = mirrorregistries.NewMockServiceMirrorRegistriesConfigBuilder(ctrl)
		mockInstallerCache = installercache.NewMockInstallerCache(ctrl)
		mockVersionsHandler = versions.NewMockHandler(ctrl)
		mockEvents = eventsapi.NewMockHandler(ctrl)
		ctx = context.Background()
		log = logrus.New()

		var err error
		workDir, err = os.MkdirTemp("", "test-ove-ignition-*")
		Expect(err).NotTo(HaveOccurred())

		tempInstallerFile, err = os.CreateTemp("", "mock-openshift-install-*")
		Expect(err).NotTo(HaveOccurred())
		tempInstallerFile.Close()

		mockRelease = installercache.NewMockRelease(tempInstallerFile.Name(), mockEvents)

		generator = &OVEIgnitionGenerator{
			executer:               mockExecuter,
			mirrorRegistriesConfig: mockMirrorRegistries,
			installerCache:         mockInstallerCache,
			versionsHandler:        mockVersionsHandler,
			log:                    log,
			workDir:                workDir,
		}

		id := strfmt.UUID("test-infra-env-id")
		infraEnv = &common.InfraEnv{
			PullSecret: `{"auths":{"test.registry.com":{"auth":"dGVzdDp0ZXN0"}}}`,
			InfraEnv: models.InfraEnv{
				ID:               &id,
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: "4.16.0",
				CPUArchitecture:  common.DefaultCPUArchitecture,
				SSHAuthorizedKey: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC...",
				ClusterID:        strfmt.UUID("test-cluster-id"),
			},
		}

		clusterVersion = "4.16.0"
	})

	AfterEach(func() {
		ctrl.Finish()
		os.RemoveAll(workDir)
		os.Remove(tempInstallerFile.Name())
	})

	Context("GenerateOVEIgnition", func() {
		It("should generate OVE ignition successfully", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)

			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), clusterVersion, infraEnv.ClusterID).Return(mockRelease, nil)

			expectedIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[{"path":"/etc/hostname","contents":{"source":"data:,test-node"}}]}}`

			mockExecuter.EXPECT().Execute(
				mockRelease.Path,
				"agent",
				"create",
				"unconfigured-ignition",
				"--interactive",
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[5]
				err := os.WriteFile(filepath.Join(oveDir, "unconfigured-agent.ign"), []byte(expectedIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())
				return "success", "", 0
			})

			result, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expectedIgnition))
		})

		It("should create correct directory structure", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)

			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), clusterVersion, infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(
				mockRelease.Path,
				"agent",
				"create",
				"unconfigured-ignition",
				"--interactive",
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[5]

				_, err := os.Stat(filepath.Join(oveDir, "cluster-manifests"))
				Expect(err).NotTo(HaveOccurred())
				_, err = os.Stat(filepath.Join(oveDir, "mirror"))
				Expect(err).NotTo(HaveOccurred())

				_, err = os.Stat(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())
				_, err = os.Stat(filepath.Join(oveDir, "cluster-manifests", "pull-secret.yaml"))
				Expect(err).NotTo(HaveOccurred())
				_, err = os.Stat(filepath.Join(oveDir, "mirror", "registries.conf"))
				Expect(err).NotTo(HaveOccurred())

				registriesContent, err := os.ReadFile(filepath.Join(oveDir, "mirror", "registries.conf"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(registriesContent)).To(Equal(constants.OVERegistriesConf))

				ignitionContent := `{"ignition":{"version":"3.2.0"}}`
				err = os.WriteFile(filepath.Join(oveDir, "unconfigured-agent.ign"), []byte(ignitionContent), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail when ClusterID is empty", func() {
			infraEnv.ClusterID = ""

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("InfraEnv test-infra-env-id is not bound to a cluster, which is required for OVE ignition generation"))
		})

		It("should fail when cluster version is empty", func() {
			emptyVersion := ""

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, emptyVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(nil, errors.New("invalid openshiftVersion"))

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, emptyVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get release image"))
		})

		It("should fail when release image is not found", func() {
			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(nil, errors.New("release image not found"))

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get release image"))
		})

		It("should fail when installer cache fails", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(nil, errors.New("cache error"))

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get installer from cache"))
		})

		It("should fail when openshift-install command fails", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)

			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), clusterVersion, infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", "error generating ignition", 1)

			_, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to generate unconfigured-ignition"))
		})

		It("should verify manifest files are created with correct content", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)

			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), clusterVersion, infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(
				tempInstallerFile.Name(),
				"agent",
				"create",
				"unconfigured-ignition",
				"--interactive",
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[5]

				By("Verifying infraenv.yaml was created with correct content")
				infraEnvYAML, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var infraEnvManifest v1beta1.InfraEnv
				err = yaml.Unmarshal(infraEnvYAML, &infraEnvManifest)
				Expect(err).NotTo(HaveOccurred())

				Expect(infraEnvManifest.Name).To(Equal("test-infra-env"))
				Expect(infraEnvManifest.Spec.SSHAuthorizedKey).To(Equal("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC..."))
				Expect(infraEnvManifest.Spec.PullSecretRef).NotTo(BeNil())
				Expect(infraEnvManifest.Spec.PullSecretRef.Name).To(Equal("pull-secret"))
				Expect(infraEnvManifest.Spec.CpuArchitecture).To(Equal(common.DefaultCPUArchitecture))

				By("Verifying pull-secret.yaml was created with correct content")
				pullSecretYAML, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "pull-secret.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var pullSecret corev1.Secret
				err = yaml.Unmarshal(pullSecretYAML, &pullSecret)
				Expect(err).NotTo(HaveOccurred())

				Expect(pullSecret.Name).To(Equal("pull-secret"))
				Expect(pullSecret.Type).To(Equal(corev1.SecretTypeDockerConfigJson))
				Expect(pullSecret.StringData).To(HaveKey(corev1.DockerConfigJsonKey))
				Expect(pullSecret.StringData[corev1.DockerConfigJsonKey]).To(Equal(infraEnv.PullSecret))

				By("Verifying registries.conf was created")
				registriesContent, err := os.ReadFile(filepath.Join(oveDir, "mirror", "registries.conf"))
				Expect(err).NotTo(HaveOccurred())
				Expect(string(registriesContent)).To(Equal(constants.OVERegistriesConf))

				// Create a mock unconfigured-agent.ign file so the generator can read it
				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				err = os.WriteFile(ignitionPath, []byte("mock-ignition-content"), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			result, err := generator.GenerateOVEIgnition(ctx, infraEnv, clusterVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("mock-ignition-content"))
		})

	})
})
