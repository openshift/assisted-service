package ignition

import (
	"context"
	"os"
	"path/filepath"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/installercache"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/system"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/pkg/executer"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OVEIgnitionGenerator struct {
	executer               executer.Executer
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder
	installerCache         installercache.InstallerCache
	versionsHandler        versions.Handler
	log                    logrus.FieldLogger
	workDir                string
}

func NewOVEIgnitionGenerator(
	executer executer.Executer,
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder,
	installerCache installercache.InstallerCache,
	versionsHandler versions.Handler,
	log logrus.FieldLogger,
	workDir string,
) *OVEIgnitionGenerator {
	return &OVEIgnitionGenerator{
		executer:               executer,
		mirrorRegistriesConfig: mirrorRegistriesConfig,
		installerCache:         installerCache,
		versionsHandler:        versionsHandler,
		log:                    log,
		workDir:                workDir,
	}
}

// GenerateOVEIgnition generates the unconfigured-ignition content for OVE images
func (g *OVEIgnitionGenerator) GenerateOVEIgnition(ctx context.Context, infraEnv *common.InfraEnv, clusterVersion string) (string, error) {
	log := logutil.FromContext(ctx, g.log)
	log.Infof("GenerateOVEIgnition called for infraEnv %s", *infraEnv.ID)

	// For OVE, we require the infraEnv to be bound to a cluster
	if infraEnv.ClusterID == "" {
		return "", errors.Errorf("InfraEnv %s is not bound to a cluster, which is required for OVE ignition generation", *infraEnv.ID)
	}

	oveDir, err := createOveDir(g.workDir, infraEnv)
	if err != nil {
		return "", errors.Wrap(err, "failed to create OVE directory")
	}
	defer func() {
		if removeErr := os.RemoveAll(oveDir); removeErr != nil {
			log.WithError(removeErr).Warnf("Failed to clean up OVE work directory %s", oveDir)
		}
	}()

	if err = createManifests(infraEnv, oveDir); err != nil {
		return "", errors.Wrap(err, "failed to create manifests")
	}

	if err = createMirrorConfig(oveDir); err != nil {
		return "", errors.Wrap(err, "failed to create mirror config")
	}

	release, err := getInstallerRelease(ctx, infraEnv, clusterVersion, infraEnv.ClusterID, g.versionsHandler, g.executer, g.mirrorRegistriesConfig, g.installerCache)
	if err != nil {
		return "", errors.Wrap(err, "failed to get installer release")
	}
	defer func() {
		if e := release.Cleanup(ctx); e != nil {
			log.WithError(e).Warnf("Failed to clean up installer release %s", release.Path)
		}
	}()

	ignitionContent, err := generateIgnition(g.executer, release.Path, oveDir, log)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate ignition")
	}

	log.Infof("Generated unconfigured-ignition for OVE image (infraEnv: %s, arch: %s) using installer %s",
		*infraEnv.ID, infraEnv.CPUArchitecture, release.Path)

	return ignitionContent, nil
}

func createPullSecretManifest(infraEnv *common.InfraEnv, manifestsDir string) error {
	pullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pull-secret",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		StringData: map[string]string{
			corev1.DockerConfigJsonKey: infraEnv.PullSecret,
		},
	}

	pullSecretYAML, err := yaml.Marshal(pullSecret)
	if err != nil {
		return errors.Wrap(err, "failed to marshal pull secret YAML")
	}
	if err = os.WriteFile(filepath.Join(manifestsDir, "pull-secret.yaml"), pullSecretYAML, 0600); err != nil {
		return errors.Wrap(err, "failed to write pull-secret.yaml")
	}
	return nil
}

func createInfraEnvManifest(infraEnv *common.InfraEnv, manifestsDir string) error {
	infraEnvManifest := &v1beta1.InfraEnv{
		ObjectMeta: metav1.ObjectMeta{
			Name: *infraEnv.Name,
		},
		Spec: v1beta1.InfraEnvSpec{
			PullSecretRef: &corev1.LocalObjectReference{
				Name: "pull-secret",
			},
			SSHAuthorizedKey: infraEnv.SSHAuthorizedKey,
			CpuArchitecture:  infraEnv.CPUArchitecture,
		},
	}

	infraEnvYAML, err := yaml.Marshal(infraEnvManifest)
	if err != nil {
		return errors.Wrap(err, "failed to marshal infraEnv YAML")
	}
	if err = os.WriteFile(filepath.Join(manifestsDir, "infraenv.yaml"), infraEnvYAML, 0600); err != nil {
		return errors.Wrap(err, "failed to write infraenv.yaml")
	}
	return nil
}

func createOveDir(workDir string, infraEnv *common.InfraEnv) (string, error) {
	oveWorkDir := filepath.Join(workDir, "ove-ignition")
	err := os.MkdirAll(oveWorkDir, 0755)
	if err != nil {
		return "", errors.Wrap(err, "failed to create OVE work directory")
	}

	oveDir, err := os.MkdirTemp(oveWorkDir, infraEnv.ID.String())
	if err != nil {
		return "", errors.Wrap(err, "failed to create OVE temp directory")
	}
	return oveDir, nil
}

func createManifests(infraEnv *common.InfraEnv, oveDir string) error {
	manifestsDir := filepath.Join(oveDir, "cluster-manifests")
	if err := os.MkdirAll(manifestsDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create manifests directory")
	}

	if err := createInfraEnvManifest(infraEnv, manifestsDir); err != nil {
		return errors.Wrap(err, "failed to create infraEnv manifest")
	}

	if err := createPullSecretManifest(infraEnv, manifestsDir); err != nil {
		return errors.Wrap(err, "failed to create pull secret manifest")
	}

	return nil
}

func createMirrorConfig(oveDir string) error {
	mirrorDir := filepath.Join(oveDir, "mirror")
	if err := os.MkdirAll(mirrorDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create mirror directory")
	}

	if err := os.WriteFile(filepath.Join(mirrorDir, "registries.conf"), []byte(constants.OVERegistriesConf), 0600); err != nil {
		return errors.Wrap(err, "failed to write registries.conf")
	}

	return nil
}

func getInstallerRelease(
	ctx context.Context,
	infraEnv *common.InfraEnv,
	openshiftVersion string,
	clusterID strfmt.UUID,
	versionsHandler versions.Handler,
	executer executer.Executer,
	mirrorRegistriesConfig mirrorregistries.ServiceMirrorRegistriesConfigBuilder,
	installerCache installercache.InstallerCache,
) (*installercache.Release, error) {
	releaseImage, err := versionsHandler.GetReleaseImage(ctx, openshiftVersion, infraEnv.CPUArchitecture, infraEnv.PullSecret)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get release image for version %s", openshiftVersion)
	}

	ocRelease := oc.NewRelease(
		executer,
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay},
		mirrorRegistriesConfig,
		system.NewLocalSystemInfo(),
	)

	release, err := installerCache.Get(ctx, *releaseImage.URL, "", infraEnv.PullSecret, ocRelease, openshiftVersion, clusterID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get installer from cache")
	}

	return release, nil
}

func generateIgnition(executer executer.Executer, releasePath string, oveDir string, log logrus.FieldLogger) (string, error) {
	stdout, stderr, exitCode := executer.Execute(releasePath, "agent", "create", "unconfigured-ignition", "--interactive", "--dir", oveDir)
	if exitCode != 0 {
		log.Errorf("error running %s agent create unconfigured-ignition --interactive, stdout: %s, stderr: %s, exit code: %d", releasePath, stdout, stderr, exitCode)
		return "", errors.Errorf("failed to generate unconfigured-ignition: %s", stderr)
	}

	ignitionPath := filepath.Join(oveDir, "unconfigured-agent.ign")
	ignitionContent, err := os.ReadFile(ignitionPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read generated unconfigured-ignition")
	}

	return string(ignitionContent), nil
}
