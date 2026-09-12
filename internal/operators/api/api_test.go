package api

import (
	"testing"

	"github.com/openshift/assisted-service/models"
)

func TestHostRequirementsForOperator(t *testing.T) {
	requirements := &models.ClusterHostRequirements{
		Ocp: &models.ClusterHostRequirementsDetails{CPUCores: 2, RAMMib: 8192},
		Operators: []*models.OperatorHostRequirements{
			{OperatorName: "lvm", Requirements: &models.ClusterHostRequirementsDetails{CPUCores: 1, RAMMib: 100}},
			{OperatorName: "other", Requirements: &models.ClusterHostRequirementsDetails{CPUCores: 4, RAMMib: 200}},
		},
	}

	result := HostRequirementsForOperator(requirements, "lvm", &models.ClusterHostRequirementsDetails{DiskSizeGb: 20})

	if result.CPUCores != 3 || result.RAMMib != 8292 || result.DiskSizeGb != 20 {
		t.Fatalf("unexpected requirements: %+v", result)
	}
}
