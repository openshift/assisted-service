package api

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type Sender interface {
	// AddEvent add an event for and entityID.
	// Since events, might relate to multiple entities, for example:
	//     host added to cluster, we have the host-ID as the main entityID and
	//     the cluster-ID as another ID that this event should be related to
	// Use the prop field to add list of arbitrary key value pairs when additional information is needed (for example: "vendor": "RedHat")
	V2AddEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{})
	// Used for events that are not interesting for metrics or for users, but we still
	// want to raise them for internal notification between systems. e.g. notify kube-api
	// that a cluster / host validation status changed so the CR conditions messages could
	// be updated, but the status that changed is trivial (e.g. from pending to success) so it's
	// not interesting to display to user or to be included in metrics
	NotifyInternalEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, msg string)

	//Add metric-related event. These events are hidden from the user and has 'metrics' Category field
	V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{})

	SendClusterEvent(ctx context.Context, event ClusterEvent)
	SendClusterEventAtTime(ctx context.Context, event ClusterEvent, eventTime time.Time)
	SendHostEvent(ctx context.Context, event HostEvent)
	SendHostEventAtTime(ctx context.Context, event HostEvent, eventTime time.Time)
	SendInfraEnvEvent(ctx context.Context, event InfraEnvEvent)
	SendInfraEnvEventAtTime(ctx context.Context, event InfraEnvEvent, eventTime time.Time)
}

//go:generate mockgen -source=event.go -package=api -destination=mock_event.go
type Handler interface {
	Sender
	V2GetEvents(ctx context.Context, params *common.V2GetEventsParams) (*common.V2GetEventsResponse, error)
}

var DefaultEventCategories = []string{
	models.EventCategoryUser,
}

type BaseEvent interface {
	GetName() string
	GetSeverity() string
	FormatMessage() string
}

type ClusterEvent interface {
	BaseEvent
	GetClusterId() strfmt.UUID
}

type HostEvent interface {
	BaseEvent
	GetClusterId() *strfmt.UUID
	GetHostId() strfmt.UUID
	GetInfraEnvId() strfmt.UUID
}

type InfraEnvEvent interface {
	BaseEvent
	GetInfraEnvId() strfmt.UUID
	GetClusterId() *strfmt.UUID
}

type InfoEvent interface {
	BaseEvent
	GetInfo() string
}
