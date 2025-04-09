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

// BundleOpenShiftAI contains the basic information of the 'openshift-ai' bundle.
var BundleOpenShiftAI = &models.Bundle{
	ID:          "openshift-ai",
	Title:       "OpenShift AI",
	Description: "Train, serve, monitor and manage AI/ML models and applications using GPUs.",
}

// Bundles is the list of valid bundles. Note that this list contains the basic information of the
// bundle, like identifier title and description, but it doesn't contain the list of operators that
// is part of the bundle. That is calculated dynamically scanning the operators.
var Bundles = []*models.Bundle{
	BundleVirtualization,
	BundleOpenShiftAI,
}
