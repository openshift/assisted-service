package garbagecollector

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	clusterPkg "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/sirupsen/logrus"
)

type Config struct {
	DeletedUnregisteredAfter time.Duration `envconfig:"DELETED_UNREGISTERED_AFTER" default:"72h"` // 3d
	DeregisterInactiveAfter  time.Duration `envconfig:"DELETED_INACTIVE_AFTER" default:"480h"`    // 20d
	MaxGCClustersPerInterval int           `envconfig:"MAX_GC_CLUSTERS_PER_INTERVAL" default:"100"`
}

type GarbageCollectors interface {
	PermanentClustersDeletion(ctx context.Context, olderThan strfmt.DateTime, objectHandler s3wrapper.API) error
	DeregisterClusterInternal(ctx context.Context, params installer.DeregisterClusterParams) error
}

func NewGarbageCollectors(
	Config Config,
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi clusterPkg.API,
	objectHandler s3wrapper.API,
	leaderElector leader.Leader,

) *garbageCollector {
	return &garbageCollector{
		Config:        Config,
		db:            db,
		log:           log,
		hostApi:       hostApi,
		clusterApi:    clusterApi,
		objectHandler: objectHandler,
		leaderElector: leaderElector,
	}
}

type garbageCollector struct {
	Config
	db            *gorm.DB
	log           logrus.FieldLogger
	hostApi       host.API
	clusterApi    clusterPkg.API
	objectHandler s3wrapper.API
	leaderElector leader.Leader
}

func (g garbageCollector) DeregisterInactiveClusters() {
	if !g.leaderElector.IsLeader() {
		return
	}

	olderThan := strfmt.DateTime(time.Now().Add(-g.Config.DeregisterInactiveAfter))
	if err := g.clusterApi.DeregisterInactiveCluster(context.Background(), g.MaxGCClustersPerInterval, olderThan); err != nil {
		g.log.WithError(err).Errorf("Failed deregister inactive clusters")
		return
	}
}

func (g garbageCollector) PermanentlyDeleteUnregisteredClustersAndHosts() {
	if !g.leaderElector.IsLeader() {
		return
	}

	olderThan := strfmt.DateTime(time.Now().Add(-g.Config.DeletedUnregisteredAfter))
	if err := g.clusterApi.PermanentClustersDeletion(context.Background(), olderThan, g.objectHandler); err != nil {
		g.log.WithError(err).Errorf("Failed deleting de-registered clusters")
		return
	}

	g.log.Debugf(
		"Permanently deleting all hosts that were soft-deleted before %s",
		olderThan)
	if err := g.hostApi.PermanentHostsDeletion(olderThan); err != nil {
		g.log.WithError(err).Errorf("Failed deleting soft-deleted hosts")
		return
	}
}
