package common

import (
	"fmt"
	"strings"

	"github.com/go-openapi/strfmt"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	"github.com/openshift/assisted-service/models"
)

func PlatformTypeToPlatform(platformType hiveext.PlatformType) *models.Platform {
	pType := strings.ToLower(string(platformType))
	platform := &models.Platform{Type: PlatformTypePtr(models.PlatformType(pType))}
	return platform
}

func PlatformTypePtr(p models.PlatformType) *models.PlatformType {
	return &p
}

func PlatformTypeValue(p *models.PlatformType) models.PlatformType {
	if p == nil {
		return ""
	}
	return *p
}

func ImageTypePtr(i models.ImageType) *models.ImageType {
	return &i
}

func ImageTypeValue(i *models.ImageType) models.ImageType {
	if i == nil {
		return ""
	}
	return *i
}

func LogStatePtr(l models.LogsState) *models.LogsState {
	return &l
}

func LogStateValue(l *models.LogsState) models.LogsState {
	if l == nil {
		return ""
	}
	return *l
}

func StrFmtUUIDVal(u *strfmt.UUID) strfmt.UUID {
	if u == nil {
		return ""
	}
	return *u
}

func StrFmtUUIDPtr(u strfmt.UUID) *strfmt.UUID {
	if u.String() == "" {
		return nil
	}
	return &u
}

func VipVerificationPtr(v models.VipVerification) *models.VipVerification {
	return &v
}

func BoolPtrForLog(b *bool) string {
	if b == nil {
		return "nil"
	}
	return fmt.Sprintf("%t", *b)
}
