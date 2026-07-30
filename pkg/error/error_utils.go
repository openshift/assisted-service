package error

import (
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"k8s.io/utils/ptr"
)

type AssistedServiceErrorAPI interface {
	Error() string
	GetPayload() *models.Error
}

type AssistedServiceInfraErrorAPI interface {
	Error() string
	GetPayload() *models.InfraError
}

func assistedErrorToError(err AssistedServiceErrorAPI) error {
	payload := err.GetPayload()
	return errors.Errorf(
		"AssistedServiceError Code: %s Href: %s ID: %d Kind: %s Reason: %s",
		ptr.Deref(payload.Code, ""),
		ptr.Deref(payload.Href, ""),
		ptr.Deref(payload.ID, 0),
		ptr.Deref(payload.Kind, ""),
		ptr.Deref(payload.Reason, ""))
}

func infraErrorToError(err AssistedServiceInfraErrorAPI) error {
	payload := err.GetPayload()
	return errors.Errorf(
		"AssistedServiceInfraError Code: %d Message: %s",
		ptr.Deref(payload.Code, 0),
		ptr.Deref(payload.Message, ""))
}

func GetAssistedError(err error) error {
	switch err := err.(type) {
	case AssistedServiceErrorAPI:
		return assistedErrorToError(err)
	case AssistedServiceInfraErrorAPI:
		return infraErrorToError(err)
	default:
		return err
	}
}
