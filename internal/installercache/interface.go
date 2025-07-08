package installercache

import (
	"context"

	"github.com/go-openapi/strfmt"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/oc"
)

//go:generate mockgen -source=interface.go -package=installercache -destination=mock_installercache.go

// InstallerCache defines the interface for the installer cache
type InstallerCache interface {
	// Get retrieves an installer binary from cache or downloads it if not present
	Get(ctx context.Context, releaseID, releaseIDMirror, pullSecret string, ocRelease oc.Release, ocpVersion string, clusterID strfmt.UUID) (*Release, error)
}

// NewMockRelease creates a Release suitable for testing
func NewMockRelease(path string, eventsHandler eventsapi.Handler) *Release {
	return &Release{
		Path:          path,
		eventsHandler: eventsHandler,
	}
}
