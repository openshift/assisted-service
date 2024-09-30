package virt

import (
	"slices"

	"github.com/openshift/assisted-service/models"
)

const (
	intelVirtCpuFlag = "vmx"
	amdVirtCpuFlag   = "svm"
)

// TODO: check ARM feature flag when it's available.
func IsVirtSupported(inventory *models.Inventory) bool {
	return slices.Contains(inventory.CPU.Flags, intelVirtCpuFlag) || slices.Contains(inventory.CPU.Flags, amdVirtCpuFlag) || inventory.CPU.Architecture == models.ClusterCPUArchitectureAarch64
}
