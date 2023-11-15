package controllers

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type controllerEventsWrapper struct {
	events           eventsapi.Handler
	crdEventsHandler CRDEventsHandler
	db               *gorm.DB
	log              logrus.FieldLogger
}

var _ eventsapi.Handler = &controllerEventsWrapper{}

func NewControllerEventsWrapper(crdEventsHandler CRDEventsHandler, events eventsapi.Handler, db *gorm.DB, log logrus.FieldLogger) *controllerEventsWrapper {
	return &controllerEventsWrapper{crdEventsHandler: crdEventsHandler,
		events: events, db: db, log: log}
}

func (c *controllerEventsWrapper) V2AddEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	c.events.V2AddEvent(ctx, clusterID, hostID, infraEnvID, name, severity, msg, eventTime, props)

	if hostID != nil {
		c.NotifyKubeApiHostEvent(common.StrFmtUUIDVal(infraEnvID), common.StrFmtUUIDVal(hostID))
	} else {
		c.NotifyKubeApiClusterEvent(common.StrFmtUUIDVal(clusterID))
	}
}

func (c *controllerEventsWrapper) NotifyInternalEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, msg string) {
	c.log.Debugf("Notifying internal event %s", msg)
	if hostID != nil {
		c.NotifyKubeApiHostEvent(common.StrFmtUUIDVal(infraEnvID), common.StrFmtUUIDVal(hostID))
	} else {
		c.NotifyKubeApiClusterEvent(common.StrFmtUUIDVal(clusterID))
	}
}

func (c *controllerEventsWrapper) V2AddMetricsEvent(ctx context.Context, clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, name string, severity string, msg string, eventTime time.Time, props ...interface{}) {
	// Disable metrics event for the controller since the current operator installations do not work with ELK
}

func (c *controllerEventsWrapper) V2GetEvents(ctx context.Context, params *common.V2GetEventsParams) (*common.V2GetEventsResponse, error) {
	return c.events.V2GetEvents(
		ctx,
		params,
	)
}

func (c *controllerEventsWrapper) SendClusterEvent(ctx context.Context, event eventsapi.ClusterEvent) {
	c.events.SendClusterEvent(ctx, event)

	c.NotifyKubeApiClusterEvent(event.GetClusterId())
}

func (c *controllerEventsWrapper) SendClusterEventAtTime(ctx context.Context, event eventsapi.ClusterEvent, eventTime time.Time) {
	c.events.SendClusterEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiClusterEvent(event.GetClusterId())
}

func (c *controllerEventsWrapper) SendHostEvent(ctx context.Context, event eventsapi.HostEvent) {
	c.events.SendHostEvent(ctx, event)

	c.NotifyKubeApiHostEvent(event.GetInfraEnvId(), event.GetHostId())
}

func (c *controllerEventsWrapper) SendHostEventAtTime(ctx context.Context, event eventsapi.HostEvent, eventTime time.Time) {
	c.events.SendHostEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiHostEvent(event.GetInfraEnvId(), event.GetHostId())
}

func (c *controllerEventsWrapper) SendInfraEnvEvent(ctx context.Context, event eventsapi.InfraEnvEvent) {
	c.events.SendInfraEnvEvent(ctx, event)

	c.NotifyKubeApiInfraEnvEvent(event.GetInfraEnvId())
}

func (c *controllerEventsWrapper) SendInfraEnvEventAtTime(ctx context.Context, event eventsapi.InfraEnvEvent, eventTime time.Time) {
	c.events.SendInfraEnvEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiInfraEnvEvent(event.GetInfraEnvId())
}

func (c *controllerEventsWrapper) NotifyKubeApiClusterEvent(clusterID strfmt.UUID) {
	if clusterID == "" {
		return
	}
	cluster, err := common.GetClusterFromDB(c.db, clusterID, common.SkipEagerLoading)
	if err != nil {
		return
	}

	c.log.Debugf("Pushing cluster event %s %s", cluster.KubeKeyName, cluster.KubeKeyNamespace)
	c.crdEventsHandler.NotifyClusterDeploymentUpdates(cluster.KubeKeyName, cluster.KubeKeyNamespace)
}

func (c *controllerEventsWrapper) NotifyKubeApiHostEvent(infraEnvID strfmt.UUID, hostID strfmt.UUID) {
	if infraEnvID == "" || hostID == "" {
		return
	}
	host, err := common.GetHostFromDB(c.db.Unscoped(), infraEnvID.String(), hostID.String())
	if err != nil {
		return
	}

	c.log.Debugf("Pushing event for host %q %s", hostID, host.KubeKeyNamespace)
	c.crdEventsHandler.NotifyAgentUpdates(hostID.String(), host.KubeKeyNamespace)

	if host.ClusterID != nil {
		c.NotifyKubeApiClusterEvent(*host.ClusterID)
	}
}

func (c *controllerEventsWrapper) NotifyKubeApiInfraEnvEvent(infraEnvId strfmt.UUID) {
	if infraEnvId == "" {
		return
	}
	ie, err := common.GetInfraEnvFromDB(c.db, infraEnvId)
	if err != nil {
		return
	}

	c.log.Debugf("Pushing InfraEnv event %s %s", swag.StringValue(ie.Name), ie.KubeKeyNamespace)
	c.crdEventsHandler.NotifyInfraEnvUpdates(swag.StringValue(ie.Name), ie.KubeKeyNamespace)
}
