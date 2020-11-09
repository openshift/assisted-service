package secretdump_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/openshift/assisted-service/pkg/secretdump"

	"strings"
	"testing"
)

func TestSecretdump(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secretdump Suite")
}

var _ = Describe("Secretdump", func() {
	Context("Dump secret structs", func() {
		type Nested struct {
			D string `secret:"false"`   // Tests that only "true" secrets get redacted
			E int    `otherTag:"value"` // Tests that not all tags get redacted
			F string `secret:"true"`    // Tests that even nested struct secrets get redacted
		}

		type Example struct {
			A       string
			B       int
			C       string `secret:"true"` // Tests that fields marked secret get redacted. Top level.
			N       Nested
			Ppn     *int
			Ppv     *int
			Psn     *Nested
			Psv     *Nested
			private int
		}

		test1Nested1 := Nested{
			D: "World",
			E: 6,
			F: "ThisIsAnotherSecret",
		}

		test1Nested2 := Nested{
			D: "!",
			E: 7,
			F: "ThisIsAnotherAnotherSecret",
		}

		test1IntValue := 10

		test1 := Example{
			A:       "Hello",
			B:       5,
			C:       "ThisIsASecret",
			N:       test1Nested1,   // Tests nested structs
			Ppn:     nil,            // Tests nil pointers to primitives
			Ppv:     &test1IntValue, // Tests real pointers to primitives
			Psn:     nil,            // Tests nil pointers to structs
			Psv:     &test1Nested2,  // Tests real pointers to structs
			private: 2,              // Tests not crashing on unexported fields
		}

		test1Expected := strings.TrimSpace(`
struct Example {
	A: "Hello",
	B: 5,
	C: <REDACTED>,
	N: struct Nested {
		D: "World",
		E: 6,
		F: <REDACTED>,
	},
	Ppn: <*int>,
	Ppv: <*int>,
	Psn: <*secretdump_test.Nested>,
	Psv: <*secretdump_test.Nested>,
	private: <PRIVATE>,
}
`)
		test1Actual := DumpSecretStruct(test1)

		It("should be as expected", func() {
			Expect(test1Actual).To(Equal(test1Expected))
		})
	})
})
