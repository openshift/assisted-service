package api

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

type Sender interface {
	V2AddEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})

	//Add metric-related event. These events are hidden from the user and has 'metrics' Category field
	AddMetricsEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})
	V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{})

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
