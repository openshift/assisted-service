package thread

import (
	"context"
	"sync"
	"time"

	"github.com/openshift/assisted-service/pkg/leader"

	"github.com/sirupsen/logrus"
)

type Monitor struct {
	Name     string
	Interval time.Duration
	Exec     func()
}

type MonitorRunnerWithLeader struct {
	log      logrus.FieldLogger
	done     chan struct{}
	monitors []Monitor
	elector  leader.ElectorInterface
	ctx      context.Context
}

func NewMonitorRunnerWithLeader(log logrus.FieldLogger, monitors []Monitor, elector leader.ElectorInterface) *MonitorRunnerWithLeader {
	return &MonitorRunnerWithLeader{
		log:      log,
		done:     make(chan struct{}),
		monitors: monitors,
		elector:  elector,
		ctx:      context.Background(),
	}
}

// Start monitors thread
func (m *MonitorRunnerWithLeader) Start() error {
	m.log.Infof("Started monitors thread")
	return m.elector.StartLeaderElection(m.ctx, m.runMonitors, m.Stop)
}

// Stop monitors
func (m *MonitorRunnerWithLeader) Stop() {
	m.log.Infof("Stopping all monitors")
	close(m.done)
	m.log.Infof("Stopped all monitors")
}

func (m *MonitorRunnerWithLeader) runMonitors() {
	var wg sync.WaitGroup
	wg.Add(len(m.monitors))
	for _, monitor := range m.monitors {
		go m.monitorRunnerLoop(m.done, &wg, monitor)
	}
	m.log.Infof("Waiting for all monitors to finish")
	wg.Wait()
}

func (m *MonitorRunnerWithLeader) monitorRunnerLoop(done <-chan struct{}, wg *sync.WaitGroup, monitor Monitor) {
	m.log.Infof("Start running %s monitor", monitor.Name)
	defer wg.Done()
	intervalTimer := time.NewTimer(0)
	defer intervalTimer.Stop()

	for {
		select {
		case <-done:
			m.log.Infof("Stop running %s monitor", monitor.Name)
			return
		case <-intervalTimer.C:
			monitor.Exec()
			intervalTimer.Reset(monitor.Interval)
		}
	}
}
