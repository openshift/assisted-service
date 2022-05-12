package hostcommands

import (
	"context"
	"sync"
	"time"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/connectivity"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const maxConnectivityRequestsPerMinute = 150

type connectivityCheckCmd struct {
	baseCmd
	db                     *gorm.DB
	connectivityValidator  connectivity.Validator
	connectivityCheckImage string
	queue                  common.ExpiringCache
}

func NewConnectivityCheckCmd(log logrus.FieldLogger, db *gorm.DB, connectivityValidator connectivity.Validator, connectivityCheckImage string) *connectivityCheckCmd {
	return &connectivityCheckCmd{
		baseCmd:                baseCmd{log: log},
		db:                     db,
		connectivityValidator:  connectivityValidator,
		connectivityCheckImage: connectivityCheckImage,
		queue:                  common.NewExpiringCache(5*time.Minute, time.Minute),
	}
}

type clusterQueue struct {
	mutex         sync.Mutex
	admittedQueue []time.Time
	waitingQueue  []string
}

// Throttle the pace of connectivity check requests up to maxConnectivityRequestsPerMinute (150) requests per minute
// It maintains instance of clusterQueue per cluster id that its requests need to be throttled.
// 2 queues are kept as part of clusterQueue:
// - waitingQueue: Identification of hosts that previously requested admition, and were denied
// - admittedQueue: Timestamps when hosts got admitted during the last minute
// The function returns true if the index of the host in waiting queue is less than  maxConnectivityRequestsPerMinute - len(admittedQueue)
// If the host is admitted, its id is removed from the waitingQueue, and the current time is added to admittedQueue
func (c *connectivityCheckCmd) isAdmitted(host *models.Host) bool {
	valIntf, _ := c.queue.GetOrInsert(host.ClusterID.String(), &clusterQueue{})
	q := valIntf.(*clusterQueue)
	q.mutex.Lock()
	defer q.mutex.Unlock()
	firstValidIndex := 0
	for ; firstValidIndex < len(q.admittedQueue) && time.Since(q.admittedQueue[firstValidIndex]) > time.Minute; firstValidIndex++ {
	}
	q.admittedQueue = q.admittedQueue[firstValidIndex:]
	key := common.GetHostKey(host)
	index := funk.IndexOfString(q.waitingQueue, key)
	if index == -1 {
		q.waitingQueue = append(q.waitingQueue, key)
		index = len(q.waitingQueue) - 1
	}
	if index < (maxConnectivityRequestsPerMinute - len(q.admittedQueue)) {
		q.waitingQueue = append(q.waitingQueue[:index], q.waitingQueue[index+1:]...)
		q.admittedQueue = append(q.admittedQueue, time.Now())
		return true
	}
	return false
}

func (c *connectivityCheckCmd) GetSteps(ctx context.Context, host *models.Host) ([]*models.Step, error) {

	var hosts []*models.Host
	if err := c.db.Select("id", "inventory", "status").Find(&hosts, "cluster_id = ?", host.ClusterID).Error; err != nil {
		c.log.WithError(err).Errorf("failed to get list of hosts for cluster %s", host.ClusterID)
		return nil, err
	}
	if len(hosts) <= maxConnectivityRequestsPerMinute || c.isAdmitted(host) {
		hostsData, err := convertHostsToConnectivityCheckParams(host.ID, hosts, c.connectivityValidator)
		if err != nil {
			c.log.WithError(err).Errorf("failed to convert hosts to connectivity params for host %s cluster %s", host.ID, host.ClusterID)
			return nil, err
		}

		// Skip this step in case there is no hosts to check
		if hostsData != "" {
			step := &models.Step{
				StepType: models.StepTypeConnectivityCheck,
				Args: []string{
					hostsData,
				},
			}
			return []*models.Step{step}, nil
		}
	}
	return nil, nil
}
