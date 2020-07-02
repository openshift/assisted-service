package metrics

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/metrics/matchedRouteContext"
	"github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/slok/go-http-metrics/metrics"
)

// Config has the dependencies and values of the recorder.
type Config struct {
	Log     logrus.FieldLogger
	Service string
	// Prefix is the prefix that will be set on the metrics, by default it will be empty.
	Prefix string
	// DurationBuckets are the buckets used by Prometheus for the HTTP request duration metrics,
	// by default uses Prometheus default buckets (from 5ms to 10s).
	DurationBuckets []float64
	// SizeBuckets are the buckets used by Prometheus for the HTTP response size metrics,
	// by default uses a exponential buckets from 100B to 1GB.
	SizeBuckets []float64
	// Registry is the registry that will be used by the recorder to store the metrics,
	// if the default registry is not used then it will use the default one.
	Registry prometheus.Registerer
	// HandlerIDLabel is the name that will be set to the handler ID label, by default is `handler`.
	HandlerIDLabel string
	// StatusCodeLabel is the name that will be set to the status code label, by default is `code`.
	StatusCodeLabel string
	// MethodLabel is the name that will be set to the method label, by default is `method`.
	MethodLabel string
	// ServiceLabel is the name that will be set to the service label, by default is `service`.
	ServiceLabel string
	// IDLabel is the name that will be set to the ID label. ny default is 'id'.
	IDLabel string
}

func (c *Config) defaults() {
	if len(c.DurationBuckets) == 0 {
		c.DurationBuckets = prometheus.DefBuckets
	}

	if len(c.SizeBuckets) == 0 {
		c.SizeBuckets = prometheus.ExponentialBuckets(100, 10, 8)
	}

	if c.HandlerIDLabel == "" {
		c.HandlerIDLabel = "handler"
	}

	if c.StatusCodeLabel == "" {
		c.StatusCodeLabel = "code"
	}

	if c.MethodLabel == "" {
		c.MethodLabel = "method"
	}

	if c.ServiceLabel == "" {
		c.ServiceLabel = "service"
	}
	if c.IDLabel == "" {
		c.IDLabel = "id"
	}
}

type recorder struct {
	config                    Config
	httpRequestDurHistogram   *prometheus.HistogramVec
	httpResponseSizeHistogram *prometheus.HistogramVec
	httpRequestsInflight      *prometheus.GaugeVec
}

// NewRecorder returns a new metrics recorder that implements the recorder
// using Prometheus as the backend.
func NewRecorder(cfg Config) metrics.Recorder {
	cfg.defaults()

	r := &recorder{
		config: cfg,
		httpRequestDurHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "The latency of the HTTP requests.",
			Buckets:   cfg.DurationBuckets,
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel, cfg.IDLabel}),

		httpResponseSizeHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "The size of the HTTP responses.",
			Buckets:   cfg.SizeBuckets,
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel, cfg.IDLabel}),

		httpRequestsInflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "requests_inflight",
			Help:      "The number of inflight requests being handled at the same time.",
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.IDLabel}),
	}

	cfg.Registry.MustRegister(
		r.httpRequestDurHistogram,
		r.httpResponseSizeHistogram,
		r.httpRequestsInflight,
	)

	return r
}

type metricLabels struct {
	clusterID string
	path      string
	method    string
}

func extractRequestLabels(ctx context.Context) metricLabels {
	ret := metricLabels{
		clusterID: "",
		path:      "",
		method:    "",
	}
	mr, method := matchedRouteContext.FromContext(ctx)
	if mr != nil {
		ret.path = mr.PathPattern

		if v, _, ok := mr.Params.GetOK("cluster_id"); ok {
			if len(v) == 1 {
				ret.clusterID = v[0]
			}
		}
	}
	ret.method = method
	return ret
}

func (r recorder) ObserveHTTPRequestDuration(ctx context.Context, p metrics.HTTPReqProperties, duration time.Duration) {
	labels := extractRequestLabels(ctx)
	r.httpRequestDurHistogram.WithLabelValues(p.Service, labels.path, labels.method, p.Code, labels.clusterID).Observe(duration.Seconds())
}

func (r recorder) ObserveHTTPResponseSize(ctx context.Context, p metrics.HTTPReqProperties, sizeBytes int64) {
	labels := extractRequestLabels(ctx)
	r.httpResponseSizeHistogram.WithLabelValues(p.Service, labels.path, labels.method, p.Code, labels.clusterID).Observe(float64(sizeBytes))
}

func (r recorder) AddInflightRequests(ctx context.Context, p metrics.HTTPProperties, quantity int) {
	labels := extractRequestLabels(ctx)
	r.httpRequestsInflight.WithLabelValues(p.Service, labels.path, labels.method, labels.clusterID).Add(float64(quantity))
}
