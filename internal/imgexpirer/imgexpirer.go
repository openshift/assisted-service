package imgexpirer

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
)

const imagePrefix = "discovery-image-"
const imagePrefixLen = len(imagePrefix)

type Manager struct {
	objectHandler s3wrapper.API
	eventsHandler events.Handler
	deleteTime    time.Duration
}

func NewManager(objectHandler s3wrapper.API, eventsHandler events.Handler, deleteTime time.Duration) *Manager {
	return &Manager{
		objectHandler: objectHandler,
		eventsHandler: eventsHandler,
		deleteTime:    deleteTime,
	}
}

func (m *Manager) ExpirationTask() {
	ctx := requestid.ToContext(context.Background(), requestid.NewID())
	m.objectHandler.ExpireObjects(ctx, imagePrefix, m.deleteTime, m.DeletedImageCallback)
}

func (m *Manager) DeletedImageCallback(ctx context.Context, objectName string) {
	m.eventsHandler.AddEvent(ctx, clusterIDFromImageName(objectName), nil, models.EventSeverityInfo,
		"Deleted image from backend because it expired. It may be generated again at any time.", time.Now())
}

func clusterIDFromImageName(imgName string) strfmt.UUID {
	//Image name format is "discovery-image-<clusterID>"
	return strfmt.UUID(imgName[imagePrefixLen:])
}
