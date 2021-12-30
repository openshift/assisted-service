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
	AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})
	V2AddEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{})

	//Add metric-related event. These events are hidden from the user and has 'metrics' Category field
	AddMetricsEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})
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
	//Get a list of events. Events can be filtered by category. if no filter is specified, events with the default category are returned
	GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID, categories ...string) ([]*common.Event, error)
	V2GetEvents(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, categories ...string) ([]*common.Event, error)
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
