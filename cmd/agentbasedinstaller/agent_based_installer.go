package agentbasedinstaller

import (
	"context"
	"fmt"
	"os"
	"reflect"

	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/controller/controllers"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"
)

func RegisterCluster(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, pullSecret string, clusterDeploymentPath string,
	agentClusterInstallPath string, clusterImageSetPath string, releaseImageMirror string) (*models.Cluster, error) {
	var cd hivev1.ClusterDeployment
	if cdErr := GetFileData(clusterDeploymentPath, &cd); cdErr != nil {
		return nil, cdErr
	}

	var aci hiveext.AgentClusterInstall
	if aciErr := GetFileData(agentClusterInstallPath, &aci); aciErr != nil {
		return nil, aciErr
	}

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
	clientClusterParams := &installer.V2RegisterClusterParams{
		NewClusterParams: clusterParams,
	}
	clusterResult, registerClusterErr := bmInventory.Installer.V2RegisterCluster(ctx, clientClusterParams)
	if registerClusterErr != nil {
		return nil, registerClusterErr
	}
	return clusterResult.Payload, nil
}

func RegisterInfraEnv(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall, pullSecret string, modelsCluster *models.Cluster,
	infraEnvPath string, nmStateConfigPath string, imageTypeISO string) (*models.InfraEnv, error) {
	var infraEnv aiv1beta1.InfraEnv
	if infraenvErr := GetFileData(infraEnvPath, &infraEnv); infraenvErr != nil {
		return nil, infraenvErr
	}

	var nmStateConfig aiv1beta1.NMStateConfig
	if nmStateErr := GetFileData(nmStateConfigPath, &nmStateConfig); nmStateErr != nil {
		return nil, nmStateErr
	}

	staticNetworkConfig, processErr := processNMStateConfig(log, infraEnv, nmStateConfig)
	if processErr != nil {
		return nil, processErr
	}

	infraEnvParams := controllers.CreateInfraEnvParams(&infraEnv, models.ImageType(imageTypeISO), pullSecret, modelsCluster.ID, modelsCluster.OpenshiftVersion)

	if len(staticNetworkConfig) > 0 {
		log.Infof("Added %d nmstateconfigs", len(staticNetworkConfig))
		infraEnvParams.InfraenvCreateParams.StaticNetworkConfig = staticNetworkConfig
	}

	clientInfraEnvParams := &installer.RegisterInfraEnvParams{
		InfraenvCreateParams: infraEnvParams.InfraenvCreateParams,
	}
	infraEnvResult, registerInfraEnvErr := bmInventory.Installer.RegisterInfraEnv(ctx, clientInfraEnvParams)
	if registerInfraEnvErr != nil {
		return nil, registerInfraEnvErr
	}
	return infraEnvResult.Payload, nil
}

// Read a Yaml file and unmarshal the contents
func GetFileData(filePath string, output interface{}) error {

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
	if err := GetFileData(clusterImageSetPath, &clusterImageSet); err != nil {
		return "", err
	}
	return clusterImageSet.Spec.ReleaseImage, nil
}

func getReleaseVersionAndCpuArch(log *log.Logger, releaseImage string, releaseMirror string, pullSecret string) (string, string, error) {
	// releaseImage is in the form: quay.io:443/openshift-release-dev/ocp-release:4.9.17-x86_64
	releaseHandler := oc.NewRelease(&executer.CommonExecuter{},
		oc.Config{MaxTries: oc.DefaultTries, RetryDelay: oc.DefaltRetryDelay})

	version, versionError := releaseHandler.GetOpenshiftVersion(log, releaseImage, releaseMirror, pullSecret)
	if versionError != nil {
		return "", "", versionError
	}

	cpuArch, archError := releaseHandler.GetReleaseArchitecture(log, releaseImage, pullSecret)
	if archError != nil {
		return "", "", archError
	}

	return version, cpuArch, nil
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
