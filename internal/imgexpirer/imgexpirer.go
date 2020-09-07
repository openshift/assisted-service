package imgexpirer

import (
	"context"
	"regexp"
	"time"

	"github.com/openshift/assisted-service/pkg/leader"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

const imagePrefix = "discovery-image-"
const imageRegex = imagePrefix + `(?P<uuid>[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}).iso`

var (
	//Image name format is "discovery-image-<clusterID>.iso"
	uuidRegex = regexp.MustCompile(imageRegex)
)

type Manager struct {
	objectHandler s3wrapper.API
	eventsHandler events.Handler
	deleteTime    time.Duration
	leaderElector leader.Leader
}

func NewManager(objectHandler s3wrapper.API, eventsHandler events.Handler, deleteTime time.Duration, leaderElector leader.ElectorInterface) *Manager {
	return &Manager{
		objectHandler: objectHandler,
		eventsHandler: eventsHandler,
		deleteTime:    deleteTime,
		leaderElector: leaderElector,
	}
}

func (m *Manager) ExpirationTask() {
	if !m.leaderElector.IsLeader() {
		return
	}
	ctx := requestid.ToContext(context.Background(), requestid.NewID())
	m.objectHandler.ExpireObjects(ctx, imagePrefix, m.deleteTime, m.DeletedImageCallback)
}

func (m *Manager) DeletedImageCallback(ctx context.Context, log logrus.FieldLogger, objectName string) {
	if !m.leaderElector.IsLeader() {
		return
	}
	matches := uuidRegex.FindStringSubmatch(objectName)
	if len(matches) != 2 {
		log.Errorf("Cannot find cluster ID in object name: %s", objectName)
		return
	}
	clusterID := strfmt.UUID(matches[1])
	m.eventsHandler.AddEvent(ctx, clusterID, nil, models.EventSeverityInfo,
		"Deleted image from backend because it expired. It may be generated again at any time.", time.Now())
}
