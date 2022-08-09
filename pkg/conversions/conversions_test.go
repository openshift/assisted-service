package conversions

import (
	"math"
	"testing"

	"github.com/alecthomas/units"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConversions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Conversions tests Suite")
}

var _ = Describe("bytes to string conversion", func() {

	It("1 byte", func() {
		Expect(BytesToString(1)).To(Equal("1 bytes"))
	})
	It("1 KiB", func() {
		Expect(BytesToString(int64(units.KiB))).To(Equal("1 KiB"))
	})
	It("1 MiB", func() {
		Expect(BytesToString(MibToBytes(1))).To(Equal("1 MiB"))
	})
	It("1 GiB", func() {
		Expect(BytesToString(GibToBytes(1))).To(Equal("1.00 GiB"))
	})
	It("1 TiB", func() {
		Expect(BytesToString(int64(units.TiB))).To(Equal("1.00 TiB"))
	})
	It("1 PiB", func() {
		Expect(BytesToString(int64(units.PiB))).To(Equal("1.00 PiB"))
	})
	It("1.28 GiB", func() {
		Expect(BytesToString(int64(math.Round(float64(1.28) * float64(units.GiB))))).To(Equal("1.28 GiB"))
	})
	It("4.25 TiB", func() {
		Expect(BytesToString(int64(math.Round(float64(4.25) * float64(units.TiB))))).To(Equal("4.25 TiB"))
	})
	It("8.74 PiB", func() {
		Expect(BytesToString(int64(math.Round(float64(8.74) * float64(units.PiB))))).To(Equal("8.74 PiB"))
	})

})
