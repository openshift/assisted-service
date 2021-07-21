package error

import (
	"github.com/go-openapi/swag"
	models "github.com/openshift/assisted-service/models/v1"
	"github.com/pkg/errors"
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
		swag.StringValue(payload.Code),
		swag.StringValue(payload.Href),
		swag.Int32Value(payload.ID),
		swag.StringValue(payload.Kind),
		swag.StringValue(payload.Reason))
}

func infraErrorToError(err AssistedServiceInfraErrorAPI) error {
	payload := err.GetPayload()
	return errors.Errorf(
		"AssistedServiceInfraError Code: %d Message: %s",
		swag.Int32Value(payload.Code),
		swag.StringValue(payload.Message))
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
