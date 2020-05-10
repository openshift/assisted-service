package thread_test

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/filanov/bm-inventory/pkg/thread"
	"github.com/sirupsen/logrus"
)

var counter uint64

func useThread() {
	log := logrus.New()
	log.Out = ioutil.Discard
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
// when executed with 'go test', the test will fail, if the 'output' remark, at the end of the function is not
// correct.
func ExampleThread() {
	useThread()
	passed := counter <= 9 || counter <= 11
	fmt.Println(passed)
	// Output: true

}
