package garbagecollector

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	clusterPkg "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/infraenv"
	"github.com/openshift/assisted-service/pkg/leader"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Config struct {
	DeletedUnregisteredAfter    time.Duration `envconfig:"DELETED_UNREGISTERED_AFTER" default:"72h"`       // 3d
	DeregisterInactiveAfter     time.Duration `envconfig:"DELETED_INACTIVE_AFTER" default:"480h"`          // 20d
	InfraenvDeleteInactiveAfter time.Duration `envconfig:"INFRAENV_DELETED_INACTIVE_AFTER" default:"480h"` // 20d
	MaxGCClustersPerInterval    int           `envconfig:"MAX_GC_CLUSTERS_PER_INTERVAL" default:"100"`
	MaxGCInfraEnvsPerInterval   int           `envconfig:"MAX_GC_INFRAENVS_PER_INTERVAL" default:"100"`
}

func NewGarbageCollectors(
	Config Config,
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi clusterPkg.API,
	infraEnvApi infraenv.API,
	objectHandler s3wrapper.API,
	leaderElector leader.Leader,

) *garbageCollector {
	return &garbageCollector{
		Config:        Config,
		db:            db,
		log:           log,
		hostApi:       hostApi,
		clusterApi:    clusterApi,
		infraEnvApi:   infraEnvApi,
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
	infraEnvApi   infraenv.API
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

func (g garbageCollector) DeleteOrphans() {
	if !g.leaderElector.IsLeader() {
		return
	}
	olderThan := strfmt.DateTime(time.Now().Add(-g.Config.InfraenvDeleteInactiveAfter))
	g.log.Debugf(
		"Permanently deleting all infraenv that were not updated before %s",
		olderThan)
	ctx := context.Background()
	if err := g.infraEnvApi.DeleteOrphanInfraEnvs(ctx, g.MaxGCInfraEnvsPerInterval, olderThan); err != nil {
		g.log.WithError(err).Errorf("Failed to delete orphan infraenvs")
	}

	g.log.Debug("Permanently deleting all orphan hosts (hosts with no valid infraenv)")
	if err := g.hostApi.DeleteOrphanHosts(ctx); err != nil {
		g.log.WithError(err).Errorf("Failed to delete orphan hosts")
	}
}
