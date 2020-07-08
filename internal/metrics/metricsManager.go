package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=metricsManager.go -package=metrics -destination=mock_netricsManager_api.go

//////////////////////////////////////////
// counters name and description
/////////////////////////////////////////
const (
	counterClusterCreation            = "assisted_installer_cluster_creations"
	counterClusterInstallationStarted = "assisted_installer_cluster_installation_started"
)

const (
	counterDescriptionClusterCreation            = "Number of cluster resources created, by version"
	counterDescriptionClusterInstallationStarted = "Number of clusters that entered installing state, by version"
)

const (
	namespace             = ""
	subsystem             = "service"
	openshiftVersionLabel = "openshiftVersion"
)

type API interface {
	ClusterRegistered(clusterVersion string)
	InstallationStarted(clusterVersion string)
}

type MetricsManager struct {
	log      logrus.FieldLogger
	registry prometheus.Registerer

	serviceLogicClusterCreation            *prometheus.CounterVec
	serviceLogicClusterInstallationStarted *prometheus.CounterVec
}

func NewMetricsManager(log logrus.FieldLogger, registry prometheus.Registerer) *MetricsManager {

	m := &MetricsManager{
		log:      log,
		registry: registry,

		serviceLogicClusterCreation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterCreation,
				Help:      counterDescriptionClusterCreation,
			}, []string{openshiftVersionLabel}),

		serviceLogicClusterInstallationStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      counterClusterInstallationStarted,
				Help:      counterDescriptionClusterInstallationStarted,
			}, []string{openshiftVersionLabel}),
	}

	registry.MustRegister(
		m.serviceLogicClusterCreation,
		m.serviceLogicClusterInstallationStarted,
	)
	return m
}

func (m *MetricsManager) ClusterRegistered(clusterVersion string) {
	m.serviceLogicClusterCreation.WithLabelValues(clusterVersion).Inc()
}
func (m *MetricsManager) InstallationStarted(clusterVersion string) {
	m.serviceLogicClusterInstallationStarted.WithLabelValues(clusterVersion).Inc()
}
