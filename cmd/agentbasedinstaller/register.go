package agentbasedinstaller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"regexp"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/manifests"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	errorutil "github.com/openshift/assisted-service/pkg/error"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

func GetPullSecret(pullSecretPath string) (string, error) {
	var secret corev1.Secret
	if err := getFileData(pullSecretPath, &secret); err != nil {
		return "", err
	}

	pullSecret := secret.StringData[".dockerconfigjson"]
	return pullSecret, nil
}

func RegisterCluster(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, pullSecret string, clusterDeploymentPath string,
	agentClusterInstallPath string, clusterImageSetPath string, releaseImageMirror string) (*models.Cluster, error) {

	var result *models.Cluster
	log.Info("Registering cluster")

	var cd hivev1.ClusterDeployment
	if cdErr := getFileData(clusterDeploymentPath, &cd); cdErr != nil {
		return nil, cdErr
	}

	var aci hiveext.AgentClusterInstall
	if aciErr := getFileData(agentClusterInstallPath, &aci); aciErr != nil {
		return nil, aciErr
	}

	desiredApiVips, err := validations.HandleApiVipBackwardsCompatibility(
		nil,
		aci.Spec.APIVIP,
		controllers.ApiVipsEntriesToArray(aci.Spec.APIVIPs))
	if err != nil {
		return nil, err
	}
	aci.Spec.APIVIPs = controllers.ApiVipsArrayToStrings(desiredApiVips)
	aci.Spec.APIVIP = ""

	desiredIngressVips, err := validations.HandleIngressVipBackwardsCompatibility(
		nil,
		aci.Spec.IngressVIP,
		controllers.IngressVipsEntriesToArray(aci.Spec.IngressVIPs))
	if err != nil {
		return nil, err
	}
	aci.Spec.IngressVIPs = controllers.IngressVipsArrayToStrings(desiredIngressVips)
	aci.Spec.IngressVIP = ""

	releaseImage, releaseError := getReleaseVersion(clusterImageSetPath)
	if releaseError != nil {
		return nil, releaseError
	}
	releaseImageVersion, releaseImageCPUArch, versionArchError := getReleaseVersionAndCpuArch(log, releaseImage, releaseImageMirror, pullSecret)
	if versionArchError != nil {
		return nil, versionArchError
	}
	log.Info("releaseImage: " + releaseImage)
	log.Infof("releaseImage version %s cpuarch %s", releaseImageVersion, releaseImageCPUArch)

	clusterParams := controllers.CreateClusterParams(&cd, &aci, pullSecret, releaseImageVersion, releaseImageCPUArch, nil)

	if aci.Spec.Networking.NetworkType != "" {
		clusterParams.NetworkType = &aci.Spec.Networking.NetworkType
	}

	clientClusterParams := &installer.V2RegisterClusterParams{
		NewClusterParams: clusterParams,
	}
	clusterResult, registerClusterErr := bmInventory.Installer.V2RegisterCluster(ctx, clientClusterParams)
	if registerClusterErr != nil {
		return nil, errorutil.GetAssistedError(registerClusterErr)
	}
	result = clusterResult.GetPayload()

	log.Infof("Registered cluster with id: %s", clusterResult.Payload.ID)

	annotations := aci.GetAnnotations()
	if installConfigOverrides, ok := annotations[controllers.InstallConfigOverrides]; ok {
		var reJsonField = regexp.MustCompile(`(?i)"([^"]*(password)[^"]*)":\s*"(\\{2}|\\"|[^"])*"`)
		updateInstallConfigParams := &installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           *clusterResult.Payload.ID,
			InstallConfigParams: installConfigOverrides,
		}
		_, updateClusterErr := bmInventory.Installer.V2UpdateClusterInstallConfig(ctx, updateInstallConfigParams)
		if updateClusterErr != nil {
			return nil, errorutil.GetAssistedError(updateClusterErr)
		}

		filteredICOverrides := reJsonField.ReplaceAllString(installConfigOverrides, fmt.Sprintf(`"$1":"%s"`, "[redacted]"))
		log.Infof("Updated cluster %s with installConfigOverrides %s", clusterResult.Payload.ID, filteredICOverrides)

		// Need to GET cluster again so we can give a proper return value
		getClusterResult, err := bmInventory.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{
			ClusterID: *clusterResult.Payload.ID,
		})

		if err != nil {
			log.Warnf("Updated cluster %s with installConfigOverrides %s", clusterResult.Payload.ID, filteredICOverrides)
		} else {
			result = getClusterResult.GetPayload()
		}
	}

	return result, nil
}

func RegisterInfraEnv(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, pullSecret string, modelsCluster *models.Cluster,
	infraEnvPath string, nmStateConfigPath string, imageTypeISO string, additionalTrustBundle string) (*models.InfraEnv, error) {

	log.Info("Registering infraenv")

	var infraEnv aiv1beta1.InfraEnv
	if infraenvErr := getFileData(infraEnvPath, &infraEnv); infraenvErr != nil {
		return nil, infraenvErr
	}

	infraEnvParams := controllers.CreateInfraEnvParams(&infraEnv, models.ImageType(imageTypeISO), pullSecret, modelsCluster.ID, modelsCluster.OpenshiftVersion)

	var nmStateConfig aiv1beta1.NMStateConfig

	fileInfo, _ := os.Stat(nmStateConfigPath)
	if fileInfo != nil {
		if nmStateErr := getFileData(nmStateConfigPath, &nmStateConfig); nmStateErr != nil {
			return nil, nmStateErr
		}

		staticNetworkConfig, processErr := processNMStateConfig(log, infraEnv, nmStateConfig)
		if processErr != nil {
			return nil, processErr
		}

		if len(staticNetworkConfig) > 0 {
			log.Infof("Added %d nmstateconfigs", len(staticNetworkConfig))
			infraEnvParams.InfraenvCreateParams.StaticNetworkConfig = staticNetworkConfig
		}
	}

	clientInfraEnvParams := &installer.RegisterInfraEnvParams{
		InfraenvCreateParams: infraEnvParams.InfraenvCreateParams,
	}
	infraEnvResult, registerInfraEnvErr := bmInventory.Installer.RegisterInfraEnv(ctx, clientInfraEnvParams)
	if registerInfraEnvErr != nil {
		return nil, errorutil.GetAssistedError(registerInfraEnvErr)
	}

	infraEnvID := infraEnvResult.Payload.ID.String()
	log.Info("Registered infraenv with id: " + infraEnvID)

	return infraEnvResult.Payload, nil
}

func RegisterExtraManifests(fsys fs.FS, ctx context.Context, log *log.Logger, client *manifests.Client, cluster *models.Cluster) error {

	extras, err := fs.Glob(fsys, "*.y*ml")
	if err != nil {
		return err
	}

	if len(extras) == 0 {
		return nil
	}

	log.Info("Registering extra manifests")

	extraManifestsFolder := "openshift"

	for _, f := range extras {
		extraManifestFileName := f
		bytes, err := fs.ReadFile(fsys, extraManifestFileName)
		if err != nil {
			return err
		}

		extraManifestContent := base64.StdEncoding.EncodeToString(bytes)
		params := manifests.NewV2CreateClusterManifestParams().
			WithClusterID(*cluster.ID).
			WithCreateManifestParams(&models.CreateManifestParams{
				FileName: &extraManifestFileName,
				Folder:   &extraManifestsFolder,
				Content:  &extraManifestContent,
			})

		_, err = client.V2CreateClusterManifest(ctx, params)
		if err != nil {
			return errorutil.GetAssistedError(err)
		}
	}

	return nil
}

func GetCluster(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall) (cluster *models.Cluster, err error) {
	list, err := bmInventory.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
	if err != nil {
		return nil, err
	}
	clusterList := list.Payload
	numClusters := len(clusterList)
	if numClusters > 1 {
		errorMessage := "found multiple clusters registered in assisted-service"
		return nil, errors.New(errorMessage)
	}
	if numClusters == 0 {
		return nil, errors.New("No clusters registered in assisted-service")
	}
	return clusterList[0], nil
}

func GetInfraEnv(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall) (infraEnv *models.InfraEnv, err error) {
	list, err := bmInventory.Installer.ListInfraEnvs(ctx, &installer.ListInfraEnvsParams{})
	if err != nil {
		return nil, err
	}
	infraEnvList := list.Payload
	numInfraEnvs := len(infraEnvList)
	if numInfraEnvs > 1 {
		errorMessage := "found multiple infraenvs registered in assisted-service"
		return nil, errors.New(errorMessage)
	}
	if numInfraEnvs == 0 {
		return nil, errors.New("No infraenvs registered in assisted-service")
	}
	return infraEnvList[0], nil
}

// Read a Yaml file and unmarshal the contents
func getFileData(filePath string, output interface{}) error {

	contents, err := os.ReadFile(filePath)
	if err != nil {
		err = fmt.Errorf("error reading file %s: %w", filePath, err)
	} else if err = yaml.Unmarshal(contents, output); err != nil {
		err = fmt.Errorf("error unmarshalling contents of %s: %w", filePath, err)
	}

	return err
}

func getReleaseVersion(clusterImageSetPath string) (string, error) {
	var clusterImageSet hivev1.ClusterImageSet
	if err := getFileData(clusterImageSetPath, &clusterImageSet); err != nil {
		return "", err
	}
	return clusterImageSet.Spec.ReleaseImage, nil
}

func getReleaseVersionAndCpuArch(log *log.Logger, releaseImage string, releaseMirror string, pullSecret string) (string, string, error) {
	// releaseImage is in the form: quay.io:443/openshift-release-dev/ocp-release:4.9.17-x86_64
	mirrorRegistriesBuilder := mirrorregistries.New()
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay}, mirrorRegistriesBuilder)

	version, versionError := releaseHandler.GetOpenshiftVersion(log, releaseImage, releaseMirror, pullSecret)
	if versionError != nil {
		return "", "", versionError
	}

	cpuArchs, archError := releaseHandler.GetReleaseArchitecture(log, releaseImage, releaseMirror, pullSecret)
	if archError != nil {
		return "", "", archError
	}

	// This is a safety compatibility handler. GetReleaseArchitecture() should never return nil without an error
	// but given the caller of this function here does not check that, we want to explicitly handle this scenario.
	if len(cpuArchs) == 0 {
		return "", "", errors.New("could not get release architecture")
	}
	return version, cpuArchs[0], nil
}

func validateNMStateConfigAndInfraEnv(nmStateConfig aiv1beta1.NMStateConfig, infraEnv aiv1beta1.InfraEnv) error {
	if len(nmStateConfig.ObjectMeta.Labels) == 0 {
		return errors.Errorf("nmstateconfig should have at least one label set matching the infra-env label selector")
	}

	if len(infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels) == 0 {
		return errors.Errorf("infraenv does not have any labels set with NMStateConfigLabelSelector.MatchLabels")
	}

	if !reflect.DeepEqual(infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels, nmStateConfig.ObjectMeta.Labels) {
		return errors.Errorf("infraenv and nmstateconfig labels do not match")
	}

	return nil
}

func processNMStateConfig(log log.FieldLogger, infraEnv aiv1beta1.InfraEnv, nmStateConfig aiv1beta1.NMStateConfig) ([]*models.HostStaticNetworkConfig, error) {

	err := validateNMStateConfigAndInfraEnv(nmStateConfig, infraEnv)
	if err != nil {
		return nil, err
	}

	var staticNetworkConfig []*models.HostStaticNetworkConfig
	staticNetworkConfig = append(staticNetworkConfig, &models.HostStaticNetworkConfig{
		MacInterfaceMap: controllers.BuildMacInterfaceMap(log, nmStateConfig),
		NetworkYaml:     string(nmStateConfig.Spec.NetConfig.Raw),
	})
	return staticNetworkConfig, nil
}
