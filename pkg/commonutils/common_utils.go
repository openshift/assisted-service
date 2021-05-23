package commonutils

import (
	"time"

	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/sirupsen/logrus"
)

func MeasureOperation(operation string, log logrus.FieldLogger, metricsApi metrics.API) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		log.Errorf("%s took : %v", operation, duration)
		if metricsApi != nil {
			metricsApi.Duration(operation, duration)
		}
	}
}
