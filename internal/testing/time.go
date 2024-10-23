package testing

import (
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/onsi/gomega/types"
)

// EqualTime creates a matcher that checks that the actual value is equal to the given expected value, even if they are
// expressed using different time zones. Actual and expected values should be time.Time or strfmt.DateTime.
func EqualTime(expected any) types.GomegaMatcher {
	return &equalTimeMatcher{
		expected: expected,
	}
}

type equalTimeMatcher struct {
	expected any
}

func (m *equalTimeMatcher) Match(actual any) (success bool, err error) {
	var actualTime time.Time
	switch value := actual.(type) {
	case time.Time:
		actualTime = value
	case strfmt.DateTime:
		actualTime = time.Time(value)
	default:
		err = fmt.Errorf(
			"actual value is of unsupported type %T, supported types are time.Time and strfmt.DateTime",
			actual,
		)
	}
	var expectedTime time.Time
	switch value := m.expected.(type) {
	case time.Time:
		expectedTime = value
	case strfmt.DateTime:
		expectedTime = time.Time(value)
	default:
		err = fmt.Errorf(
			"expected value is of unsupported type %T, supported types are time.Time and strfmt.DateTime",
			actual,
		)
	}
	success = actualTime.Equal(expectedTime)
	return
}

func (m *equalTimeMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf(
		"Expected time\n\t%s\nto equal\n\t%s",
		actual, m.expected,
	)
}

func (m *equalTimeMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf(
		"Expected time\n\t%s\nto not equal\n\t%s",
		actual, m.expected,
	)
}
