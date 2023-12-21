package v1beta1

import (
	"strings"

	"github.com/go-openapi/swag"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

func platformTypeToPlatform(platformType hiveext.PlatformType) *models.Platform {
	pType := strings.ToLower(string(platformType))
	platform := &models.Platform{Type: common.PlatformTypePtr(models.PlatformType(pType))}
	platform.IsExternal = swag.Bool(common.IsPlatformExternal(platform))
	return platform
}
