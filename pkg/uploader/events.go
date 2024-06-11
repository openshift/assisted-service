package uploader

import (
	"context"
	"io"

	up "github.com/openshift/assisted-service/internal/uploader"
	"github.com/openshift/assisted-service/pkg/uploader/models"
)

func ExtractEvents(ctx context.Context, r io.Reader) ([]models.Events, error) {
	return up.ExtractEvents(r)
}
