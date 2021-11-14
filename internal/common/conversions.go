package common

import (
	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/models"
)

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
