package controllers

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
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

	cluster, err := common.GetClusterFromDB(c.db, clusterID, common.SkipEagerLoading)
	if err != nil {
		return
	}

	c.log.Debugf("Pushing cluster event %s %s", cluster.KubeKeyName, cluster.KubeKeyNamespace)
	c.crdEventsHandler.NotifyClusterDeploymentUpdates(cluster.KubeKeyName, cluster.KubeKeyNamespace)
	if hostID != nil {
		// TODO once host will have infraEnv params we need to use common.GetHostFromDB()
		// till then we will use same namespace as cluster deployment
		c.log.Debugf("Pushing event for host %q %s %s", hostID, cluster.KubeKeyName, cluster.KubeKeyNamespace)
		c.crdEventsHandler.NotifyAgentUpdates(hostID.String(), cluster.KubeKeyNamespace)
	}
}

func (c *controllerEventsWrapper) AddMetricsEvent(ctx context.Context, clusterID strfmt.UUID, hostID *strfmt.UUID, severity string, msg string, eventTime time.Time, props ...interface{}) {
	// Disable metrics event for the controller since the current operator installations do not work with ELK
}

func (c *controllerEventsWrapper) GetEvents(clusterID strfmt.UUID, hostID *strfmt.UUID, categories ...string) ([]*common.Event, error) {
	return c.events.GetEvents(clusterID, hostID, categories...)
}
