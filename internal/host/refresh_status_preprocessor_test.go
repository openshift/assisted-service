package host

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = Describe("Disabled host validations", func() {

	validations := []validation{{id: HasMinCPUCores}, {id: HasMinMemory}}
	var (
		log  *logrus.Logger
		hook *test.Hook
	)

	BeforeEach(func() {
		log, hook = test.NewNullLogger()
	})

	When("filtering host validations", func() {
		tests := []struct {
			name                    string
			disabledHostValidations []string
			expectedValidations     []validation
			expectedLogEntry        bool
			expectedLogLevel        string
			expectedLogMessage      string
		}{
			{
				name:                    "Nominal: empty list",
				disabledHostValidations: []string{},
				expectedValidations:     validations,
			},
			{
				name:                    "Nominal: 1 host validation filtered",
				disabledHostValidations: []string{string(HasMinCPUCores)},
				expectedValidations:     []validation{{id: HasMinMemory}},
			},
			{
				name:                    "KO: unknown host validation",
				disabledHostValidations: []string{"unknown-host-validation"},
				expectedValidations:     validations,
				expectedLogEntry:        true,
				expectedLogLevel:        logrus.WarnLevel.String(),
				expectedLogMessage:      "Unable to find host validation IDs: unknown-host-validation",
			},
		}
		for _, t := range tests {
			t := t
			It(t.name, func() {
				baseValidations := filterHostValidations(t.disabledHostValidations, log, validations)
				if t.expectedLogEntry {
					Expect(len(hook.Entries)).To(Equal(1))
					Expect(hook.LastEntry().Level).To(BeEquivalentTo(logrus.WarnLevel))
					Expect(hook.LastEntry().Message).To(BeEquivalentTo(t.expectedLogMessage))
				}
				Expect(baseValidations).To(BeEquivalentTo(t.expectedValidations))
			})
		}
	})

})
