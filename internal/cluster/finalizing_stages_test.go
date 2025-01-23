package cluster

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
)

var _ = DescribeTable("finalizing stage timeouts",
	func(stage models.FinalizingStage, operators []*models.MonitoredOperator, softTimeoutEnabled bool, envValue string, expected time.Duration) {
		if envValue != "" {
			envKey := convertStageToEnvVar(stage)
			_ = os.Setenv(envKey, envValue)
			defer func() {
				_ = os.Unsetenv(envKey)
			}()
		}
		Expect(finalizingStageTimeout(stage, operators, softTimeoutEnabled, logrus.New())).To(Equal(expected))
	},
	func() []TableEntry {
		// Variables for test setup
		var (
			ret       []TableEntry
			log       = logrus.New()
			olmStages = []models.FinalizingStage{
				models.FinalizingStageWaitingForOlmOperatorsCsvInitialization,
				models.FinalizingStageWaitingForOlmOperatorsCsv,
			}
			nonOlmStages = funk.Subtract(finalizingStages, olmStages).([]models.FinalizingStage)
			toSeconds    = func(d time.Duration) int64 {
				return int64(d.Seconds())
			}
			operators = []*models.MonitoredOperator{
				{
					OperatorType:   models.OperatorTypeBuiltin,
					TimeoutSeconds: toSeconds(22 * time.Hour),
				},
				{
					OperatorType:   models.OperatorTypeOlm,
					TimeoutSeconds: toSeconds(20 * time.Hour),
				},
				{
					OperatorType:   models.OperatorTypeOlm,
					TimeoutSeconds: toSeconds(21 * time.Hour),
				},
			}
			shortTimeoutOperator = []*models.MonitoredOperator{
				{
					OperatorType:   models.OperatorTypeOlm,
					TimeoutSeconds: toSeconds(21 * time.Second),
				},
			}
		)
		// End of variables declaration section

		// Test cases for all stages without operators
		for _, softTimeoutEnabled := range []bool{false, true} {
			timeoutStr := lo.Ternary(softTimeoutEnabled, "with soft timeouts", "with hard timeouts")
			for _, stage := range finalizingStages {
				defaultTimeout := finalizingStageDefaultTimeout(stage, softTimeoutEnabled, log)
				ret = append(ret,
					Entry(fmt.Sprintf("uses the default timeout in stage '%s' without environment setting and without operators %s", stage, timeoutStr), stage, nil, softTimeoutEnabled, "", defaultTimeout))
				ret = append(ret,
					Entry(fmt.Sprintf("uses the environment setting in stage '%s' with environment setting and without operators %s", stage, timeoutStr), stage, nil, softTimeoutEnabled, "123m", 123*time.Minute))
			}

			// Test cases for non OLM stages with operators.  Should behave the same as if operators were not provided
			for _, stage := range nonOlmStages {
				defaultTimeout := finalizingStageDefaultTimeout(stage, softTimeoutEnabled, log)
				ret = append(ret,
					Entry(fmt.Sprintf("uses the default timeout in stage '%s' without environment setting and with operators %s", stage, timeoutStr), stage, operators, softTimeoutEnabled, "", defaultTimeout))
				ret = append(ret,
					Entry(fmt.Sprintf("uses the environment setting in stage '%s' with environment setting and with operators %s", stage, timeoutStr), stage, operators, softTimeoutEnabled, "123m", 123*time.Minute))
			}

			// Test cases for OLM stages with operators.  Should return the maximum of default timeout and the timeout specified in the operators
			for _, stage := range olmStages {
				ret = append(ret,
					Entry(fmt.Sprintf("uses the operator timeout in stage '%s' without environment setting and with operators %s", stage, timeoutStr), stage, operators, softTimeoutEnabled, "", 21*time.Hour))
				ret = append(ret,
					Entry(fmt.Sprintf("uses the operator timeout  in stage '%s' with environment setting and with operators %s", stage, timeoutStr), stage, operators, softTimeoutEnabled, "123m", 21*time.Hour))
			}

			// Test cases that use the default timeout because operator timeout is too short
			for _, stage := range olmStages {
				defaultTimeout := finalizingStageDefaultTimeout(stage, softTimeoutEnabled, log)
				ret = append(ret,
					Entry(fmt.Sprintf("uses the default timeout in stage '%s' without environment setting and with short timeout operator %s", stage, timeoutStr), stage, shortTimeoutOperator, softTimeoutEnabled, "", defaultTimeout))
				ret = append(ret,
					Entry(fmt.Sprintf("uses the environment setting in stage '%s' with environment setting and with short timeout operator %s", stage, timeoutStr), stage, shortTimeoutOperator, softTimeoutEnabled, "123m", 123*time.Minute))
			}
		}
		return ret
	}()...,
)
