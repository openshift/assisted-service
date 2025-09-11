package kubedescheduler

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"

	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	operatorsCommon "github.com/openshift/assisted-service/internal/operators/common"
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

	// Handle custom manifests with platform and version-specific logic
	customManifests, err = o.generateCustomManifests(cluster)
	if err != nil {
		return
	}

	return
}

func (o *operator) generateCustomManifests(cluster *common.Cluster) ([]byte, error) {
	customManifestsBuffer := &bytes.Buffer{}

	// Determine if CNV (Container Native Virtualization) operator is enabled
	// This indicates the cluster is using KubeVirt/OpenShift Virtualization
	isCNVEnabled := operatorsCommon.HasOperator(cluster.MonitoredOperators, "cnv")

	if isCNVEnabled {
		// Select template based on OCP version for CNV/Virtualization
		templateFile, err := o.selectVirtualizationTemplate(cluster.OpenshiftVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to select virtualization template: %w", err)
		}

		manifestContent, err := operatorsCommon.ExecuteTemplate(
			templateFile, o.templates, nil, &Operator,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to execute KubeVirt template %s: %w", templateFile, err)
		}

		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(manifestContent)
		customManifestsBuffer.WriteString("\n")

		// ADD PSI MACHINECONFIG IF REQUIRED
		if o.requiresPSIKernelArg(cluster) {
			o.log.Info("Adding PSI kernel argument MachineConfig for PrometheusCPUCombined profile")

			psiManifestContent, err := operatorsCommon.ExecuteTemplate(
				"custom/psi_kernel_args_machineconfig.yaml", o.templates, nil, &Operator,
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
			"custom/kube_descheduler.yaml", o.templates, nil, &Operator,
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

func (o *operator) selectVirtualizationTemplate(openshiftVersion string) (string, error) {
	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", fmt.Errorf("invalid OpenShift version %s: %w", openshiftVersion, err)
	}

	// Version constraints
	v420, err := version.NewConstraint(">= 4.20")
	if err != nil {
		return "", err
	}

	v419, err := version.NewConstraint(">= 4.19, < 4.20")
	if err != nil {
		return "", err
	}

	// Select template based on version
	if v420.Check(v) {
		return "custom/kube_descheduler_kubevirt_4_20_plus.yaml", nil
	} else if v419.Check(v) {
		return "custom/kube_descheduler_kubevirt_4_19.yaml", nil
	} else {
		// OCP <= 4.18
		return "custom/kube_descheduler_kubevirt_4_18_below.yaml", nil
	}
}

// Add this function after the existing selectVirtualizationTemplate function

func (o *operator) requiresPSIKernelArg(cluster *common.Cluster) bool {
	// Check if CNV is enabled and we're using OCP 4.19 which has PrometheusCPUCombined
	isCNVEnabled := operatorsCommon.HasOperator(cluster.MonitoredOperators, "cnv")
	if !isCNVEnabled {
		return false
	}

	v, err := version.NewVersion(cluster.OpenshiftVersion)
	if err != nil {
		o.log.WithError(err).Warnf("Invalid OpenShift version %s", cluster.OpenshiftVersion)
		return false
	}

	// Only OCP >= 4.19 uses PrometheusCPUCombined which requires PSI
	v419Plus, err := version.NewConstraint(">= 4.19")
	if err != nil {
		return false
	}

	return v419Plus.Check(v)
}
