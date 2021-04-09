package virt

import (
	"github.com/openshift/assisted-service/models"
	"github.com/thoas/go-funk"
)

const (
	intelVirtCpuFlag = "vmx"
	amdVirtCpuFlag   = "svm"
)

func IsVirtSupported(inventory *models.Inventory) bool {
	return funk.Contains(inventory.CPU.Flags, intelVirtCpuFlag) || funk.Contains(inventory.CPU.Flags, amdVirtCpuFlag)
}
