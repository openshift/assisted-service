package common

import (
	"net/http"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
)

func GenerateError(id int32, err error) *models.Error {
	return &models.Error{
		Code:   swag.String(string(id)),
		Href:   swag.String(""),
		ID:     swag.Int32(id),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
	}
}

func GenerateInternalFromError(err error) *models.Error {
	return &models.Error{
		Code:   swag.String(string(http.StatusInternalServerError)),
		Href:   swag.String(""),
		ID:     swag.Int32(http.StatusInternalServerError),
		Kind:   swag.String("Error"),
		Reason: swag.String(err.Error()),
	}
}
