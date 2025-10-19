package kubedescheduler

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"

	"github.com/openshift/assisted-service/internal/common"
	operatorsCommon "github.com/openshift/assisted-service/internal/operators/common"
)

const (
	CNVKubeDeschedulerTemplatePath     = "custom/kube_descheduler_cnv.yaml"
	DefaultKubeDeschedulerTemplatePath = "custom/kube_descheduler.yaml"
	PSIKArgMachineConfigTemplatePath   = "custom/psi_kernel_arg_machine_config.yaml"
)

// GenerateManifests generates manifests for the operator.
func (o *operator) GenerateManifests(cluster *common.Cluster) (openshiftManifests map[string][]byte, customManifests []byte, err error) {
	// Generate the openshift manifests using the common function
	openshiftManifests = map[string][]byte{}
	openshiftTemplatePaths, err := fs.Glob(templatesRoot, "openshift/*.yaml")
	if err != nil {
		return
	}
	for _, openshiftTemplatePath := range openshiftTemplatePaths {
		manifestName := path.Base(openshiftTemplatePath)
		var manifestContent []byte
		manifestContent, err = operatorsCommon.ExecuteTemplate(
			openshiftTemplatePath, o.templates, nil, &Operator,
		)
		if err != nil {
			return
		}
		openshiftManifests[manifestName] = manifestContent
	}

	// Handle custom manifests with version-specific logic
	customManifests, err = o.generateCustomManifests(cluster)
	if err != nil {
		return
	}

	return
}

func (o *operator) generateCustomManifests(cluster *common.Cluster) ([]byte, error) {
	customManifestsBuffer := &bytes.Buffer{}

	isCNVEnabled := operatorsCommon.HasOperator(cluster.MonitoredOperators, "cnv")

	if isCNVEnabled {
		// Create template configuration based on OCP version
		templateConfig, err := o.createTemplateConfig(cluster.OpenshiftVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to create template config: %w", err)
		}

		manifestContent, err := operatorsCommon.ExecuteTemplate(
			CNVKubeDeschedulerTemplatePath, o.templates, templateConfig, &Operator,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to render CNV template: %w", err)
		}

		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(manifestContent)
		customManifestsBuffer.WriteString("\n")

		if o.requiresPSIKernelArg(cluster) {
			o.log.Info("Adding PSI kernel argument MachineConfig for PrometheusCPUCombined profile")

			psiManifestContent, err := operatorsCommon.ExecuteTemplate(
				PSIKArgMachineConfigTemplatePath, o.templates, nil, &Operator,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to execute PSI MachineConfig template: %w", err)
			}

			customManifestsBuffer.WriteString("---\n")
			customManifestsBuffer.Write(psiManifestContent)
			customManifestsBuffer.WriteString("\n")
		}
	} else {
		// Use default template when CNV is not enabled
		manifestContent, err := operatorsCommon.ExecuteTemplate(
			DefaultKubeDeschedulerTemplatePath, o.templates, nil, &Operator,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to execute default template: %w", err)
		}

		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(manifestContent)
		customManifestsBuffer.WriteString("\n")
	}

	return customManifestsBuffer.Bytes(), nil
}

// TemplateConfig holds version-specific configuration for template rendering
type TemplateConfig struct {
	Profiles              []string
	ProfileCustomizations map[string]interface{}
}

func (o *operator) createTemplateConfig(openshiftVersion string) (*TemplateConfig, error) {
	config := &TemplateConfig{
		ProfileCustomizations: make(map[string]interface{}),
	}

	// Determine profiles and customizations based on version
	if isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual("4.20", openshiftVersion); err != nil {
		return nil, fmt.Errorf("invalid OpenShift version %s: %w", openshiftVersion, err)
	} else if isGreaterOrEqual {
		config.Profiles = []string{"KubeVirtRelieveAndMigrate"}
		return config, nil
	}

	if isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual("4.19", openshiftVersion); err != nil {
		return nil, fmt.Errorf("invalid OpenShift version %s: %w", openshiftVersion, err)
	} else if isGreaterOrEqual {
		config.Profiles = []string{"DevKubeVirtRelieveAndMigrate"}
		return config, nil
	}

	if isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual("4.18", openshiftVersion); err != nil {
		return nil, fmt.Errorf("invalid OpenShift version %s: %w", openshiftVersion, err)
	} else if isGreaterOrEqual {
		config.Profiles = []string{"DevKubeVirtRelieveAndMigrate"}
		config.ProfileCustomizations["devEnableSoftTainter"] = true
		config.ProfileCustomizations["devDeviationThresholds"] = "AsymmetricLow"
		config.ProfileCustomizations["devActualUtilizationProfile"] = "PrometheusCPUCombined"
		config.ProfileCustomizations["devEnableEvictionsInBackground"] = true
		return config, nil
	}

	if isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual("4.17", openshiftVersion); err != nil {
		return nil, fmt.Errorf("invalid OpenShift version %s: %w", openshiftVersion, err)
	} else if isGreaterOrEqual {
		config.Profiles = []string{"LongLifecycle", "EvictPodsWithPVC", "EvictPodsWithLocalStorage"}
		config.ProfileCustomizations["devEnableEvictionsInBackground"] = true
	}

	// Default to 4.16 and below
	config.Profiles = []string{"DevPreviewLongLifecycle", "EvictPodsWithPVC", "EvictPodsWithLocalStorage"}
	config.ProfileCustomizations["devEnableEvictionsInBackground"] = true
	return config, nil
}

func (o *operator) requiresPSIKernelArg(cluster *common.Cluster) bool {
	isCNVEnabled := operatorsCommon.HasOperator(cluster.MonitoredOperators, "cnv")
	if !isCNVEnabled {
		return false
	}

	// Only OCP >= 4.18 uses PrometheusCPUCombined which requires PSI
	isGreaterOrEqual, err := common.BaseVersionGreaterOrEqual("4.18", cluster.OpenshiftVersion)
	if err != nil {
		o.log.WithError(err).Warnf("Invalid OpenShift version %s", cluster.OpenshiftVersion)
		return false
	}

	return isGreaterOrEqual
}
