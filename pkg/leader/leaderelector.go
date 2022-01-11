package leader

import (
	"context"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	coordv1 "k8s.io/api/coordination/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	RunWithLeader(ctx context.Context, run func() error) error
}

type DummyElector struct{}

func (f *DummyElector) StartLeaderElection(ctx context.Context) error {
	return nil
}

func (f *DummyElector) IsLeader() bool {
	return true
}

func (f *DummyElector) RunWithLeader(ctx context.Context, run func() error) error {
	return run()
}

var _ ElectorInterface = &Elector{}

type Elector struct {
	log      logrus.FieldLogger
	config   Config
	kube     *kubernetes.Clientset
	lockName string
	isLeader bool
}

func NewElector(kubeClient *kubernetes.Clientset, config Config, lockName string, logger logrus.FieldLogger) *Elector {
	logger = logger.WithField("lock", lockName)
	return &Elector{log: logger, config: config, kube: kubeClient, lockName: lockName, isLeader: false}
}

func (l *Elector) IsLeader() bool {
	return l.isLeader
}

func (l *Elector) setLeader(status bool) {
	l.isLeader = status
}

// Wait for leader, run given function, drop leader and exit.
func (l *Elector) RunWithLeader(ctx context.Context, run func() error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	err := l.StartLeaderElection(ctx)
	if err != nil {
		return err
	}
	err = l.waitForLeader(ctx)
	if err != nil {
		return err
	}
	return run()
}

func (l *Elector) waitForLeader(ctx context.Context) error {
	ticker := time.NewTicker(l.config.RetryInterval)
	l.log.Infof("Start waiting for leader")
	for {
		select {
		case <-ctx.Done(): // Done returns a channel that's closed when work done on behalf of this context is canceled
			return errors.Errorf("cancelled while waiting for leader")
		case <-ticker.C:
			if l.IsLeader() {
				l.log.Infof("Got leader, stop waiting")
				return nil
			}
		}
	}
}

/*
 * StartLeaderElection generates the resource on which the leader lock
 * will be attempted, generates the lock itself and runs the elector.
 *
 * Since in k8s 1.17 the ConfigMap lock was deprecated, this function
 * first attempt to clear the deprecated resources, to avoid having
 * multiple leaders, each locked on a different resource.
 *
 * With the next roll out version, the cleaning code can be removed.
 */
func (l *Elector) StartLeaderElection(ctx context.Context) error {
	// delete deprecated resources
	l.clean()

	// create an underlying lease resource for the resource lock
	err := l.createLease()
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return errors.Wrapf(err, "failed to create lock resource %s in namespace %s", l.lockName, l.config.Namespace)
	}
	l.log.Infof("create lease %s in namespace %s", l.lockName, l.config.Namespace)

	resourceLock, err := l.createResourceLock()
	if err != nil {
		return errors.Wrapf(err, "failed to create lock %s in namespace %s", l.lockName, l.config.Namespace)
	}

	leaderElector, err := l.createLeaderElector(resourceLock)
	if err != nil {
		return errors.Wrapf(err, "failed to create elector")
	}

	// Running loop cause leaderElector.Run is blocking
	// and needs to be restarted while leader is lost
	// will exit if context was cancelled
	go func() {
		for {
			l.log.Infof("Starting leader elections process")
			if ctx.Err() != nil {
				l.log.Infof("Given context was cancelled, exiting leader elector")
				return
			}
			leaderElector.Run(ctx)
		}
	}()
	return nil
}

func (l *Elector) clean() {
	err := l.kube.CoreV1().ConfigMaps(l.config.Namespace).Delete(context.Background(),
		l.lockName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		l.log.WithError(err).Errorf("Failed to delete config map for leader lock %s", l.lockName)
	}
}

func (l *Elector) createLease() error {
	lease := &coordv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      l.lockName,
			Namespace: l.config.Namespace,
		},
	}
	_, err := l.kube.CoordinationV1().Leases(l.config.Namespace).Create(context.Background(), lease, metav1.CreateOptions{})
	return err
}

func (l *Elector) createResourceLock() (resourcelock.Interface, error) {
	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	id = id + "_" + string(uuid.NewUUID())

	return resourcelock.New(resourcelock.LeasesResourceLock,
		l.config.Namespace,
		l.lockName,
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
				l.setLeader(true)
				l.log.Infof("Successfully acquired leadership lease")
			},
			OnStoppedLeading: func() {
				l.setLeader(false)
				l.log.Infof("NO LONGER LEADER")
			},
		},
	})
}
