package virt

import (
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/thoas/go-funk"
)

const (
	intelVirtCpuFlag = "vmx"
	amdVirtCpuFlag   = "svm"
)

func IsVirtSupported(inventory *models.Inventory) bool {
	return funk.Contains(inventory.CPU.Flags, intelVirtCpuFlag) || funk.Contains(inventory.CPU.Flags, amdVirtCpuFlag)
}
