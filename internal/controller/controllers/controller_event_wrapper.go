package controllers

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/sirupsen/logrus"
)

type controllerEventsWrapper struct {
	events           *events.Events
	crdEventsHandler CRDEventsHandler
	db               *gorm.DB
	log              logrus.FieldLogger
}

var _ events.Handler = &controllerEventsWrapper{}

func NewControllerEventsWrapper(crdEventsHandler CRDEventsHandler, events *events.Events, db *gorm.DB, log logrus.FieldLogger) *controllerEventsWrapper {
	return &controllerEventsWrapper{crdEventsHandler: crdEventsHandler,
		events: events, db: db, log: log}
}

func (c *controllerEventsWrapper) AddEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	c.events.AddEvent(ctx, clusterID, hostID, severity, msg, eventTime, props)

	if hostID != nil {
		c.NotifyKubeApiHostEvent(clusterID, *hostID)
	} else {
		c.NotifyKubeApiClusterEvent(clusterID)
	}
}

func (c *controllerEventsWrapper) AddMetricsEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	// Disable metrics event for the controller since the current operator installations do not work with ELK
}

func (c *controllerEventsWrapper) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	return c.events.GetEvents(clusterID, hostID, categories...)
}

func (c *controllerEventsWrapper) V2GetEvents(clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	return c.events.V2GetEvents(clusterID, hostID, infraEnvID, categories...)
}

func (c *controllerEventsWrapper) SendClusterEvent(ctx context.Context, event events.ClusterEvent) {
	c.events.SendClusterEvent(ctx, event)

	c.NotifyKubeApiClusterEvent(event.GetClusterId())
}

func (c *controllerEventsWrapper) SendClusterEventAtTime(ctx context.Context, event events.ClusterEvent, eventTime time.Time) {
	c.events.SendClusterEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiClusterEvent(event.GetClusterId())
}

func (c *controllerEventsWrapper) SendHostEvent(ctx context.Context, event events.HostEvent) {
	c.events.SendHostEvent(ctx, event)

	c.NotifyKubeApiHostEvent(event.GetInfraEnvId(), event.GetHostId())
}

func (c *controllerEventsWrapper) SendHostEventAtTime(ctx context.Context, event events.HostEvent, eventTime time.Time) {
	c.events.SendHostEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiHostEvent(event.GetInfraEnvId(), event.GetHostId())
}

func (c *controllerEventsWrapper) SendInfraEnvEvent(ctx context.Context, event events.InfraEnvEvent) {
	c.events.SendInfraEnvEvent(ctx, event)

	c.NotifyKubeApiInfraEnvEvent(event.GetInfraEnvId())
}

func (c *controllerEventsWrapper) SendInfraEnvEventAtTime(ctx context.Context, event events.InfraEnvEvent, eventTime time.Time) {
	c.events.SendInfraEnvEventAtTime(ctx, event, eventTime)

	c.NotifyKubeApiInfraEnvEvent(event.GetInfraEnvId())
}

func (c *controllerEventsWrapper) NotifyKubeApiClusterEvent(clusterID strfmt.UUID) {
	cluster, err := common.GetClusterFromDB(c.db, clusterID, common.SkipEagerLoading)
	if err != nil {
		return
	}

	c.log.Debugf("Pushing cluster event %s %s", cluster.KubeKeyName, cluster.KubeKeyNamespace)
	c.crdEventsHandler.NotifyClusterDeploymentUpdates(cluster.KubeKeyName, cluster.KubeKeyNamespace)
}

func (c *controllerEventsWrapper) NotifyKubeApiHostEvent(clusterID strfmt.UUID, hostID strfmt.UUID) {
	host, err := common.GetHostFromDB(c.db.Unscoped(), clusterID.String(), hostID.String())
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
	ie, err := common.GetInfraEnvFromDB(c.db, infraEnvId)
	if err != nil {
		return
	}

	c.log.Debugf("Pushing InfraEnv event %s %s", ie.Name, ie.KubeKeyNamespace)
	c.crdEventsHandler.NotifyInfraEnvUpdates(swag.StringValue(ie.Name), ie.KubeKeyNamespace)
}
