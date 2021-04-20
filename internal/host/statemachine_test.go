package host

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

var _ = Describe("Disabled host validations", func() {

	validationIDs := []validationID{HasMinCPUCores, HasMinMemory}
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
			expectedStateCount      int
			expectedLogEntry        bool
			expectedLogLevel        string
			expectedLogMessage      string
		}{
			{
				name:                    "Nominal: empty list",
				disabledHostValidations: []string{},
				expectedStateCount:      2,
			},
			{
				name:                    "Nominal: 1 host validation ID filtered",
				disabledHostValidations: []string{string(HasMinCPUCores)},
				expectedStateCount:      1,
			},
			{
				name:                    "KO: unknown host validation ID",
				disabledHostValidations: []string{"unknown-host-validation"},
				expectedStateCount:      2,
				expectedLogEntry:        true,
				expectedLogLevel:        logrus.WarnLevel.String(),
				expectedLogMessage:      "Unable to find host validation IDs: unknown-host-validation",
			},
		}
		for _, t := range tests {
			t := t
			It(t.name, func() {
				ret := filterHostStateMachine(t.disabledHostValidations, log, validationIDs...)
				if t.expectedLogEntry {
					Expect(len(hook.Entries)).To(Equal(1))
					Expect(hook.LastEntry().Level).To(BeEquivalentTo(logrus.WarnLevel))
					Expect(hook.LastEntry().Message).To(BeEquivalentTo(t.expectedLogMessage))
				}
				Expect(len(ret)).To(Equal(t.expectedStateCount))
			})
		}
	})

})
