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

type Config struct {
	LeaseDuration time.Duration `envconfig:"LEADER_LEASE_DURATION" default:"15s"`
	RetryInterval time.Duration `envconfig:"LEADER_RETRY_INTERVAL" default:"2s"`
	RenewDeadline time.Duration `envconfig:"LEADER_RENEW_DEADLINE" default:"10s"`
	Namespace     string        `envconfig:"NAMESPACE" default:"assisted-installer"`
}

//go:generate mockgen -source=leaderelector.go -package=leader -destination=mock_leader_elector.go

type Leader interface {
	IsLeader() bool
}
type ElectorInterface interface {
	Leader
	StartLeaderElection(ctx context.Context) error
}

type DummyElector struct{}

func (f *DummyElector) StartLeaderElection(ctx context.Context) error {
	return nil
}

func (f *DummyElector) IsLeader() bool {
	return true
}

var _ ElectorInterface = &Elector{}

type Elector struct {
	log           logrus.FieldLogger
	config        Config
	kube          *kubernetes.Clientset
	isLeader      bool
	configMapName string
}

func NewElector(kubeClient *kubernetes.Clientset, config Config, configMapName string, logger logrus.FieldLogger) *Elector {
	return &Elector{log: logger, config: config, kube: kubeClient, configMapName: configMapName, isLeader: false}
}

func (l *Elector) IsLeader() bool {
	return l.isLeader
}

func (l *Elector) StartLeaderElection(ctx context.Context) error {

	resourceLock, err := l.createResourceLock(l.configMapName)
	if err != nil {
		return err
	}

	leaderElector, err := l.createLeaderElector(resourceLock)
	if err != nil {
		return err
	}

	l.log.Info("Attempting to acquire leader lease")
	// Running loop cause leaderElector.Run is blocking
	// and needs to be restarted while leader is lost
	go func() {
		for {
			l.log.Infof("Starting leader elections process")
			leaderElector.Run(ctx)
		}
	}()

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

func (l *Elector) createLeaderElector(resourceLock resourcelock.Interface) (*leaderelection.LeaderElector, error) {
	return leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          resourceLock,
		LeaseDuration: l.config.LeaseDuration,
		RenewDeadline: l.config.RenewDeadline,
		RetryPeriod:   l.config.RetryInterval,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ context.Context) {
				l.log.Info("Successfully acquired leadership lease")
				l.isLeader = true
			},
			OnStoppedLeading: func() {
				l.log.Infof("NO LONGER LEADER.")
				l.isLeader = false
			},
		},
	})
}
