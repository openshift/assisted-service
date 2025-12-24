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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Disconnected Ignition", func() {
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
		cluster              *common.Cluster
		mockRelease          *installercache.Release
		tempInstallerFile    *os.File
		generator            *DisconnectedIgnitionGenerator
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
		workDir, err = os.MkdirTemp("", "test-disconnected-ignition-*")
		Expect(err).NotTo(HaveOccurred())

		tempInstallerFile, err = os.CreateTemp("", "mock-openshift-install-*")
		Expect(err).NotTo(HaveOccurred())
		tempInstallerFile.Close()

		mockRelease = installercache.NewMockRelease(tempInstallerFile.Name(), mockEvents)

		generator = &DisconnectedIgnitionGenerator{
			executer:               mockExecuter,
			mirrorRegistriesConfig: mockMirrorRegistries,
			installerCache:         mockInstallerCache,
			versionsHandler:        mockVersionsHandler,
			log:                    log,
			workDir:                workDir,
		}

		id := strfmt.UUID("test-infra-env-id")
		clusterID := strfmt.UUID("test-cluster-id")
		infraEnv = &common.InfraEnv{
			PullSecret: `{"auths":{"test.registry.com":{"auth":"dGVzdDp0ZXN0"}}}`,
			InfraEnv: models.InfraEnv{
				ID:               &id,
				ClusterID:        clusterID,
				Name:             swag.String("test-infra-env"),
				OpenshiftVersion: "4.16.0",
				CPUArchitecture:  common.DefaultCPUArchitecture,
				SSHAuthorizedKey: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC...",
			},
		}

		hostID := strfmt.UUID("test-host-id")
		cluster = &common.Cluster{
			Cluster: models.Cluster{
				ID:               &clusterID,
				Name:             "test-cluster",
				OpenshiftVersion: "4.16.0",
				Hosts: []*models.Host{
					{
						ID:        &hostID,
						Bootstrap: true,
						Inventory: `{"interfaces":[{"ipv4_addresses":["192.168.1.10/24"],"ipv6_addresses":["fe80::1/64"],"mac_address":"52:54:00:aa:bb:cc"}]}`,
					},
				},
			},
		}

		clusterVersion = "4.16.0"
	})

	AfterEach(func() {
		ctrl.Finish()
		os.RemoveAll(workDir)
		os.Remove(tempInstallerFile.Name())
	})

	Context("GenerateDisconnectedIgnition", func() {
		It("should generate disconnected ignition successfully", func() {
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, clusterVersion, common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)

			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), clusterVersion, infraEnv.ClusterID).Return(mockRelease, nil)

			baseIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[{"path":"/etc/hostname","contents":{"source":"data:,test-node"}}]}}`
			// expectedIgnition includes all default empty fields added by the ignition library when parsing and re-marshaling
			expectedIgnition := `{"ignition":{"config":{"replace":{"verification":{}}},"proxy":{},"security":{"tls":{}},"timeouts":{},"version":"3.2.0"},"passwd":{},"storage":{"files":[{"group":{},"path":"/etc/hostname","user":{},"contents":{"source":"data:,test-node","verification":{}}},{"group":{},"overwrite":true,"path":"/etc/assisted/interactive-ui","user":{"name":"root"},"contents":{"source":"data:,","verification":{}},"mode":420}]},"systemd":{}}`

			mockExecuter.EXPECT().Execute(
				mockRelease.Path,
				"agent",
				"create",
				"unconfigured-ignition",
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]
				err := os.WriteFile(filepath.Join(oveDir, "unconfigured-agent.ign"), []byte(baseIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())
				return "success", "", 0
			})

			result, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(MatchJSON(expectedIgnition))
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
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

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
				Expect(string(registriesContent)).To(Equal(constants.DisconnectedRegistriesConf))

				ignitionContent := `{"ignition":{"version":"3.2.0"}}`
				err = os.WriteFile(filepath.Join(oveDir, "unconfigured-agent.ign"), []byte(ignitionContent), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail when ClusterID is empty", func() {
			infraEnv.ClusterID = ""

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("InfraEnv test-infra-env-id is not bound to a cluster, which is required for disconnected ignition generation"))
		})

		It("should fail when cluster version is empty", func() {
			emptyCluster := &common.Cluster{
				Cluster: models.Cluster{
					ID:               cluster.ID,
					Name:             "test-cluster",
					OpenshiftVersion: "",
				},
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(nil, errors.New("invalid openshiftVersion"))

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, emptyCluster.OpenshiftVersion, emptyCluster.Name)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get release image"))
		})

		It("should fail when release image is not found", func() {
			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(nil, errors.New("release image not found"))

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
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

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
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

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", "error generating ignition", 1)

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
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
				"--dir",
				gomock.Any(),
			).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

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
				Expect(string(registriesContent)).To(Equal(constants.DisconnectedRegistriesConf))

				// Create a mock unconfigured-agent.ign file so the generator can read it
				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			result, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring(`"path":"/etc/assisted/interactive-ui"`))
			Expect(result).To(ContainSubstring(`"version":"3.2.0"`))
		})

		It("should include proxy and NTP configuration in InfraEnv manifest", func() {
			infraEnv.Proxy = &models.Proxy{
				HTTPProxy:  swag.String("http://proxy.example.com:8080"),
				HTTPSProxy: swag.String("https://proxy.example.com:8443"),
				NoProxy:    swag.String("localhost,127.0.0.1"),
			}
			infraEnv.AdditionalNtpSources = "ntp1.example.com,ntp2.example.com"

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying InfraEnv manifest includes proxy and NTP")
				infraEnvContent, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var infraEnvManifest v1beta1.InfraEnv
				err = yaml.Unmarshal(infraEnvContent, &infraEnvManifest)
				Expect(err).NotTo(HaveOccurred())

				Expect(infraEnvManifest.TypeMeta.APIVersion).To(Equal(v1beta1.GroupVersion.String()))
				Expect(infraEnvManifest.TypeMeta.Kind).To(Equal("InfraEnv"))

				Expect(infraEnvManifest.Spec.Proxy).NotTo(BeNil())
				Expect(infraEnvManifest.Spec.Proxy.HTTPProxy).To(Equal("http://proxy.example.com:8080"))
				Expect(infraEnvManifest.Spec.Proxy.HTTPSProxy).To(Equal("https://proxy.example.com:8443"))
				Expect(infraEnvManifest.Spec.Proxy.NoProxy).To(Equal("localhost,127.0.0.1"))

				Expect(infraEnvManifest.Spec.AdditionalNTPSources).To(Equal([]string{"ntp1.example.com", "ntp2.example.com"}))

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should trim whitespace from NTP sources", func() {
			infraEnv.AdditionalNtpSources = "ntp1.example.com, ntp2.example.com , ntp3.example.com,  , ntp4.example.com"

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying NTP sources are trimmed and empty entries removed")
				infraEnvContent, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var infraEnvManifest v1beta1.InfraEnv
				err = yaml.Unmarshal(infraEnvContent, &infraEnvManifest)
				Expect(err).NotTo(HaveOccurred())

				Expect(infraEnvManifest.Spec.AdditionalNTPSources).To(Equal([]string{
					"ntp1.example.com",
					"ntp2.example.com",
					"ntp3.example.com",
					"ntp4.example.com",
				}))

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create NMStateConfig manifests from static network config", func() {
			infraEnv.StaticNetworkConfig = `[
				{
					"mac_interface_map": [
						{"mac_address": "52:54:00:aa:bb:cc", "logical_nic_name": "eth0"}
					],
					"network_yaml": "interfaces:\n- name: eth0\n  type: ethernet\n  state: up\n  ipv4:\n    enabled: true\n    address:\n    - ip: 192.168.1.10\n      prefix-length: 24\n    dhcp: false\n"
				}
			]`

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying NMStateConfig manifest was created")
				nmstateConfigContent, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "nmstateconfig-0.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var nmstateConfig v1beta1.NMStateConfig
				err = yaml.Unmarshal(nmstateConfigContent, &nmstateConfig)
				Expect(err).NotTo(HaveOccurred())

				Expect(nmstateConfig.APIVersion).To(Equal("agent-install.openshift.io/v1beta1"))
				Expect(nmstateConfig.Kind).To(Equal("NMStateConfig"))
				Expect(nmstateConfig.ObjectMeta.Name).To(Equal("nmstate-config-0"))
				Expect(nmstateConfig.ObjectMeta.Labels).To(HaveKeyWithValue(nmStateConfigInfraEnvLabelKey, infraEnv.ID.String()))
				Expect(nmstateConfig.Spec.Interfaces).To(HaveLen(1))
				Expect(nmstateConfig.Spec.Interfaces[0].Name).To(Equal("eth0"))
				Expect(nmstateConfig.Spec.Interfaces[0].MacAddress).To(Equal("52:54:00:aa:bb:cc"))

				By("Verifying InfraEnv manifest selector references NMStateConfig labels")
				infraEnvContent, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var infraEnvManifest v1beta1.InfraEnv
				err = yaml.Unmarshal(infraEnvContent, &infraEnvManifest)
				Expect(err).NotTo(HaveOccurred())
				Expect(infraEnvManifest.Spec.NMStateConfigLabelSelector.MatchLabels).To(HaveKeyWithValue(nmStateConfigInfraEnvLabelKey, infraEnv.ID.String()))

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should skip NMStateConfig manifests when static network config is whitespace", func() {
			infraEnv.StaticNetworkConfig = "   "

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying no NMStateConfig manifest was created")
				_, err := os.Stat(filepath.Join(oveDir, "cluster-manifests", "nmstateconfig-0.yaml"))
				Expect(os.IsNotExist(err)).To(BeTrue())

				By("Verifying InfraEnv manifest does not set NMStateConfig selector")
				infraEnvContent, err := os.ReadFile(filepath.Join(oveDir, "cluster-manifests", "infraenv.yaml"))
				Expect(err).NotTo(HaveOccurred())

				var infraEnvManifest v1beta1.InfraEnv
				err = yaml.Unmarshal(infraEnvContent, &infraEnvManifest)
				Expect(err).NotTo(HaveOccurred())

				Expect(infraEnvManifest.Spec.NMStateConfigLabelSelector.MatchLabels).To(BeEmpty())

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create agent-config.yaml when rendezvous IP is provided", func() {
			infraEnv.RendezvousIP = swag.String("192.168.1.100")

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying agent-config.yaml was created")
				agentConfigPath := filepath.Join(oveDir, "agent-config.yaml")
				agentConfigContent, err := os.ReadFile(agentConfigPath)
				Expect(err).NotTo(HaveOccurred())

				var agentConfig AgentConfig
				err = yaml.Unmarshal(agentConfigContent, &agentConfig)
				Expect(err).NotTo(HaveOccurred())

				Expect(agentConfig.APIVersion).To(Equal("v1beta1"))
				Expect(agentConfig.Kind).To(Equal("AgentConfig"))
				Expect(agentConfig.Metadata.Name).To(Equal("test-cluster"))
				Expect(agentConfig.RendezvousIP).To(Equal("192.168.1.100"))

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not create agent-config.yaml when rendezvous IP is not provided", func() {
			infraEnv.RendezvousIP = nil

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(common.DefaultCPUArchitecture),
				OpenshiftVersion: swag.String("4.16.0"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-x86_64"),
				Version:          swag.String("4.16.0"),
			}

			mockVersionsHandler.EXPECT().GetReleaseImage(ctx, "4.16.0", common.DefaultCPUArchitecture, infraEnv.PullSecret).Return(releaseImage, nil)
			mockEvents.EXPECT().V2AddMetricsEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallerCache.EXPECT().Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, gomock.Any(), "4.16.0", infraEnv.ClusterID).Return(mockRelease, nil)

			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(command string, args ...string) (string, string, int) {
				oveDir := args[4]

				By("Verifying agent-config.yaml was NOT created")
				agentConfigPath := filepath.Join(oveDir, "agent-config.yaml")
				_, err := os.Stat(agentConfigPath)
				Expect(os.IsNotExist(err)).To(BeTrue(), "agent-config.yaml should not exist when rendezvous IP is not provided")

				ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
				mockIgnition := `{"ignition":{"version":"3.2.0"},"storage":{"files":[]}}`
				err = os.WriteFile(ignitionPath, []byte(mockIgnition), 0600)
				Expect(err).NotTo(HaveOccurred())

				return "success", "", 0
			})

			_, err := generator.GenerateDisconnectedIgnition(ctx, infraEnv, cluster.OpenshiftVersion, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
		})

	})
})
