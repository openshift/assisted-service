package system

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("FIPSEnabled",
	func(fileContent []byte, readError error, expectedEnabled bool, expectedError bool) {
		reader := func(name string) ([]byte, error) {
			Expect(name).To(Equal(fipsFile))
			return fileContent, readError
		}
		sys := localSystemInfo{fileReader: reader}
		enabled, err := sys.FIPSEnabled()
		if expectedError {
			Expect(err).ToNot(BeNil())
			return
		}
		Expect(err).To(BeNil())
		Expect(enabled).To(Equal(expectedEnabled))
	},
	Entry("returns true when the file exists with 1", []byte("1"), nil, true, false),
	Entry("returns true when the file exists with 1 with whitespace", []byte("1\n"), nil, true, false),
	Entry("returns false when the file exists with 0", []byte("0"), nil, false, false),
	Entry("returns false when the file exists with 0 with whitespace", []byte("0 \n"), nil, false, false),
	Entry("returns false when the file does not exist", []byte(""), os.ErrNotExist, false, false),
	Entry("returns an error when a error other than NotExist is created", []byte(""), fmt.Errorf("some error"), false, true),
)

func TestSystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "system tests")
}
