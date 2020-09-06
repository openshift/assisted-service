package leader

import (
	"context"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	configMapName = "assisted-service-leader-election-helper"
)

type Config struct {
	LeaseDuration time.Duration `envconfig:"LEADER_LEASE_DURATION" default:"15s"`
	RetryInterval time.Duration `envconfig:"LEADER_RETRY_INTERVAL" default:"2s"`
	RenewDeadline time.Duration `envconfig:"LEADER_RENEW_DEADLINE" default:"10s"`
	Namespace     string        `envconfig:"NAMESPACE" default:"assisted-installer"`
}

type ElectorInterface interface {
	StartLeaderElection(ctx context.Context, leaderFunc func(), stopLeaderFunc func()) error
}

type DummyElector struct{}

func (f *DummyElector) StartLeaderElection(ctx context.Context, leaderFunc func(), stopLeaderFunc func()) error {
	go leaderFunc()
	return nil
}

var _ ElectorInterface = &Elector{}

type Elector struct {
	log      logrus.FieldLogger
	config   Config
	kube     *kubernetes.Clientset
	isLeader bool
}

func NewElector(kubeClient *kubernetes.Clientset, config Config, logger logrus.FieldLogger) *Elector {
	return &Elector{log: logger, config: config, kube: kubeClient, isLeader: false}
}

func (l *Elector) IsLeader() bool {
	return l.isLeader
}

func (l *Elector) StartLeaderElection(ctx context.Context, leaderFunc func(), stopLeaderFunc func()) error {

	resourceLock, err := l.createResourceLock(configMapName)
	if err != nil {
		return err
	}

	leaderElector, err := l.createLeaderElector(resourceLock, leaderFunc, stopLeaderFunc)
	if err != nil {
		return err
	}

	l.log.Info("Attempting to acquire leader lease")
	go leaderElector.Run(ctx)

	return nil
}

func (l *Elector) createResourceLock(name string) (resourcelock.Interface, error) {
	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	id = id + "_" + string(uuid.NewUUID())

	return resourcelock.New(resourcelock.ConfigMapsResourceLock,
		l.config.Namespace,
		name,
		l.kube.CoreV1(),
		l.kube.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: id,
		})
}

func (l *Elector) createLeaderElector(resourceLock resourcelock.Interface, leaderFunc func(), stopLeaderFunc func()) (*leaderelection.LeaderElector, error) {
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          resourceLock,
		LeaseDuration: l.config.LeaseDuration,
		RenewDeadline: l.config.RenewDeadline,
		RetryPeriod:   l.config.RetryInterval,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ context.Context) {
				l.log.Info("Successfully acquired leadership lease")
				l.isLeader = true
				leaderFunc()
			},
			OnStoppedLeading: func() {
				l.log.Infof("NO LONGER LEADER, closing monitors")
				stopLeaderFunc()
				l.isLeader = false
			},
		},
	})
}
