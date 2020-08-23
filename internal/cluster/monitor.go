package cluster

import (
	"context"
	"net/http"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/pkg/requestid"

	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func (m *Manager) ClusterMonitoring() {
	var (
		clusters  []*common.Cluster
		requestID = requestid.NewID()
		ctx       = requestid.ToContext(context.Background(), requestID)
		log       = requestid.RequestIDLogger(m.log, requestID)
		err       error
	)
	if err = m.db.Preload("Hosts").Find(&clusters).Error; err != nil {
		log.WithError(err).Errorf("failed to get clusters")
		return
	}
	for _, cluster := range clusters {
		if err = m.UpdateHostsAndClusterStatus(ctx, cluster, m.db, log); err != nil {
			log.WithError(err).Errorf("failed to refresh cluster %s state", cluster.ID)
			continue
		}
	}
}

func (m *Manager) UpdateHostsAndClusterStatus(ctx context.Context, cluster *common.Cluster, db *gorm.DB, log logrus.FieldLogger) error {
	err := m.refreshClusterHosts(ctx, cluster, db, log)
	if err != nil {
		return err
	}

	clusterAfterRefresh, refreshErr := m.RefreshStatus(ctx, cluster, db)
	if refreshErr != nil {
		log.WithError(refreshErr).Errorf("failed to validate or update cluster %s state", cluster.ID)
		return common.NewApiError(http.StatusInternalServerError, refreshErr)
	}

	if swag.StringValue(clusterAfterRefresh.Status) != swag.StringValue(cluster.Status) {
		log.Infof("cluster %s updated status from %s to %s via monitor", cluster.ID,
			swag.StringValue(cluster.Status), swag.StringValue(clusterAfterRefresh.Status))
	}
	return nil
}

func (m *Manager) refreshClusterHosts(ctx context.Context, cluster *common.Cluster, tx *gorm.DB, log logrus.FieldLogger) error {
	for _, h := range cluster.Hosts {
		var host models.Host
		var err error
		if err = tx.Take(&host, "id = ? and cluster_id = ?",
			h.ID.String(), cluster.ID.String()).Error; err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				h.ID.String(), cluster.ID.String())
			return common.NewApiError(http.StatusNotFound, err)
		}
		if err = m.hostAPI.RefreshStatus(ctx, &host, tx); err != nil {
			log.WithError(err).Errorf("failed to refresh state of host %s cluster %s", *h.ID, cluster.ID.String())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}
