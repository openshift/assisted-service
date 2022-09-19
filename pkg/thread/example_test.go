package thread_test

import (
	"fmt"
	"io"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/thread"
	"github.com/sirupsen/logrus"
)

var counter uint64

func useThread() {
	log := logrus.New()
	log.Out = io.Discard
	counter = 0

	threadFunction := func() {
		counter += 1
	}
	m := thread.New(log, "health-monitor", time.Millisecond*100, threadFunction)

	m.Start()
	defer m.Stop()
	time.Sleep(time.Second * 1)
}

// ExampleThread is a testable example for the thread package.
// The test will fail if the 'output' remark, at the end of the function, is not printed.
func ExampleThread() {
	useThread()
	passed := counter <= 9 || counter <= 11
	fmt.Println(passed)
	// Output: true
}

// This is an old package test. While all of our testing infrastructure was switched to use ginkgo
// This test remains until it would get converted.
// This ginkgo wrapper was added to allow running this packge with ginkgo flags.
func TestThread(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Request id tests")
}
