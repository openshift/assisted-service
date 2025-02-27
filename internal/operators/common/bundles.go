package common

import (
	"github.com/openshift/assisted-service/models"
)

// BundleVirtualization contains the basic information of the 'virtualization' bundle.
var BundleVirtualization = &models.Bundle{
	ID:          "virtualization",
	Title:       "Virtualization",
	Description: "Run virtual machines alongside containers on one platform.",
}

// BundleOpenShiftAINVIDIA contains the basic information of the 'openshift-ai-nvidia' bundle.
var BundleOpenShiftAINVIDIA = &models.Bundle{
	ID:          "openshift-ai-nvidia",
	Title:       "OpenShift AI (NVIDIA)",
	Description: "Train, serve, monitor and manage AI/ML models and applications using NVIDIA GPUs.",
}

// BundleOpenShiftAIAMD contains the basic information of the 'openshift-ai-amd' bundle.
var BundleOpenShiftAIAMD = &models.Bundle{
	ID:          "openshift-ai-amd",
	Title:       "OpenShift AI (AMD)",
	Description: "Train, serve, monitor and manage AI/ML models and applications using AMD GPUs.",
}

// Bundles is the list of valid bundles. Note that this list contains the basic information of the
// bundle, like identifier title and description, but it doesn't contain the list of operators that
// is part of the bundle. That is calculated dynamically scanning the operators.
var Bundles = []*models.Bundle{
	BundleVirtualization,
	BundleOpenShiftAINVIDIA,
	BundleOpenShiftAIAMD,
}
