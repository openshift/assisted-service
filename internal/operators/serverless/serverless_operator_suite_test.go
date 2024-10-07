package serverless

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestServerLessOperator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Serverless")
}
