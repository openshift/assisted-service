package thread

import (
	"testing"
	"time"

	"github.com/openshift/assisted-service/pkg/leader"
	log "github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Monitor tests")
}

type Test struct {
	index int
}

func (t *Test) IncreaseIndex() {
	t.index += 1
}

var _ = Describe("Monitor tests", func() {
	It("testing monitor runner", func() {
		test1 := Test{}
		test2 := Test{}
		dummy := leader.DummyElector{}
		monitors := []Monitor{
			{Name: "Test", Interval: 100 * time.Millisecond, Exec: test1.IncreaseIndex},
			{Name: "Test2", Interval: 400 * time.Millisecond, Exec: test2.IncreaseIndex},
		}
		monitorsRunner := NewMonitorRunnerWithLeader(log.WithField("pkg", "cluster-monitor"), monitors, &dummy)
		_ = monitorsRunner.Start()
		time.Sleep(1 * time.Second)
		monitorsRunner.Stop()
		time.Sleep(500 * time.Millisecond)
		index := test1.index
		Expect(test1.index > 7 && test1.index < 12).Should(Equal(true))
		index2 := test2.index
		Expect(test2.index > 1 && test2.index < 4).Should(Equal(true))

		By("Verify monitor was stopped")

		time.Sleep(500 * time.Millisecond)
		Expect(test1.index).Should(Equal(index))
		Expect(test2.index).Should(Equal(index2))
	})
})
