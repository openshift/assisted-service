package secretdump

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
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

		It("should be as expected", func() {
			nested1 := Nested{
				D: "World",
				E: 6,
				F: "ThisIsAnotherSecret",
			}

			nested2 := Nested{
				D: "!",
				E: 7,
				F: "ThisIsAnotherAnotherSecret",
			}

			testIntValue := 10

			testExample := Example{
				A:       "Hello",
				B:       5,
				C:       "ThisIsASecret",
				N:       nested1,       // Tests nested structs
				Ppn:     nil,           // Tests nil pointers to primitives
				Ppv:     &testIntValue, // Tests real pointers to primitives
				Psn:     nil,           // Tests nil pointers to structs
				Psv:     &nested2,      // Tests real pointers to structs
				private: 2,             // Tests not crashing on unexported fields
			}

			expected := strings.TrimSpace(`
struct Example {
	A: "Hello",
	B: 5,
	C: <SECRET>,
	N: struct Nested {
		D: "World",
		E: 6,
		F: <SECRET>,
	},
	Ppn: <*int>,
	Ppv: <*int>,
	Psn: <*secretdump.Nested>,
	Psv: <*secretdump.Nested>,
	private: <PRIVATE>,
}
`)
			actual := DumpSecretStruct(testExample)

			Expect(actual).To(Equal(expected))
		})
	})
})
