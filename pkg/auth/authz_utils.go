package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/pkg/errors"
)

func ValidateAccessToCPUArchitecture(ctx context.Context, authzHandler Authorizer, cpuArchitecture string) error {
	var err error
	var armArchAllowed bool

	switch cpuArchitecture {
	case common.ARM64CPUArchitecture:

		armArchAllowed, err = authzHandler.HasOrgBasedCapability(ctx, ocm.ArmCapabilityName)
		if err != nil {
			return common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error getting user %s capability, error: %w", ocm.ArmCapabilityName, err))
		}
		if !armArchAllowed {
			return common.NewApiError(http.StatusBadRequest, errors.Errorf("%s is not a valid CPU Architecture", common.ARM64CPUArchitecture))
		}
		return nil
	case common.X86CPUArchitecture:
		return nil
	}
	return nil
}
