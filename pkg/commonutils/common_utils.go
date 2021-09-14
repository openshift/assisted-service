package commonutils

import (
	"time"

	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/sirupsen/logrus"
)

func MeasureOperation(operation string, log logrus.FieldLogger, metricsApi metrics.API) func() {
	start := time.Now()
	log.Debugf("%s started at: %v", operation, start)
	return func() {
		duration := time.Since(start)
		log.Debugf("%s took : %v", operation, duration)
		if metricsApi != nil {
			metricsApi.Duration(operation, duration)
		}
	}
}

func MeasureOperationWithThresholdAndId(operation string, log logrus.FieldLogger, metricsApi metrics.API, thresholdInSec float64, objectId string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		if duration.Seconds() >= thresholdInSec {
			log.Warning("%s for %s took : %v", operation, objectId, duration)
			if metricsApi != nil {
				metricsApi.DurationWithThreshold(operation, thresholdInSec, duration)
			}
		}
	}
}
