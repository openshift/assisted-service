package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=metricsManager.go -package=metrics -destination=mock_netricsManager_api.go

const (
	CounterClusterCreation            = "assisted_installer_cluster_creations"
	CounterClusterInstallationStarted = "assisted_installer_cluster_installation_started"
)

const (
	CounterDescriptionClusterCreation            = "Number of cluster resources created, by version"
	CounterDescriptionClusterInstallationStarted = "Number of clusters that entered installing state, by version"
)

type API interface {
	RegisterCluster(clusterVersion string)
	InstallationStarted(clusterVersion string)
}

type ServiceConfig struct {
	Log logrus.FieldLogger
	// Registry is the registry that will be used by the recorder to store the metrics,
	// if the default registry is not used then it will use the default one.
	Registry       prometheus.Registerer
	Namespace      string
	ClusterVersion string
}

type MetricsManager struct {
	config ServiceConfig

	serviceLogicClusterCreation            *prometheus.CounterVec
	serviceLogicClusterInstallationStarted *prometheus.CounterVec
}

func NewMetricsManager(log logrus.FieldLogger, registry prometheus.Registerer) *MetricsManager {
	cfg := ServiceConfig{
		Log:            log,
		Registry:       registry,
		ClusterVersion: "openshiftVersion"}

	m := &MetricsManager{
		config: cfg,

		serviceLogicClusterCreation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: cfg.Namespace,
				Subsystem: "service",
				Name:      CounterClusterCreation,
				Help:      CounterDescriptionClusterCreation,
			}, []string{cfg.ClusterVersion}),

		serviceLogicClusterInstallationStarted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: cfg.Namespace,
				Subsystem: "service",
				Name:      CounterClusterInstallationStarted,
				Help:      CounterDescriptionClusterInstallationStarted,
			}, []string{cfg.ClusterVersion}),
	}

	cfg.Registry.MustRegister(
		m.serviceLogicClusterCreation,
		m.serviceLogicClusterInstallationStarted,
	)
	return m
}

func (m *MetricsManager) RegisterCluster(clusterVersion string) {
	m.serviceLogicClusterCreation.WithLabelValues(clusterVersion).Inc()
}
func (m *MetricsManager) InstallationStarted(clusterVersion string) {
	m.serviceLogicClusterInstallationStarted.WithLabelValues(clusterVersion).Inc()
}
