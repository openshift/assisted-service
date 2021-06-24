package common

import (
	"time"

	"github.com/jinzhu/gorm"
)

/*
   Support querying for cluster or host monitoring. Implementation for both full query, and timed query according to
   updated_at field.
   Querying is done at cluster level.  Checking the trigger_monitor_timestamp field is done both for hosts and clusters
*/
const (
	DefaultBatchSize = 100
	IdsQuerySize     = 10000
)

type MonitorQuery interface {
	Next() ([]*Cluster, error)
}

type fullQuery struct {
	lastId          string
	dbWithCondition *gorm.DB
	eof             bool
	batchSize       int
}

/*
  Full scan (backward compatible) query.  Instead of using offset, the query uses the lastId to indicate where to start the next batch
*/
func (f *fullQuery) Next() ([]*Cluster, error) {
	var clusters []*Cluster
	if f.eof {
		return clusters, nil
	}
	if err := f.dbWithCondition.Where("id > ?", f.lastId).Order("id").Limit(f.batchSize).Find(&clusters).Error; err != nil {
		return clusters, err
	}
	if len(clusters) < f.batchSize {
		f.eof = true
	}
	if len(clusters) > 0 {
		f.lastId = clusters[len(clusters)-1].ID.String()
	}
	return clusters, nil
}

/*
  Timed query which queries according to the trigger_monitor_timestamp field
*/
type timedQuery struct {
	// Where to start the next query for ids
	lastId string

	// db connection to query ids
	db *gorm.DB

	// db connection to query the clusters
	dbWithCondition *gorm.DB

	// The time to compare to the trigger_monitor_timestamp field
	timeToCompare time.Time

	// The relevant cluster ids
	ids []string

	// True if last id was already received
	eof bool

	// Offset in the id list
	offset int

	// Max batch size (limit)
	batchSize int
}

func min(i, j int) int {
	if i < j {
		return i
	}
	return j
}

func (t *timedQuery) Next() ([]*Cluster, error) {
	var (
		clusters []*Cluster
		err      error
	)
	if t.eof && t.offset == len(t.ids) {
		return clusters, nil
	}
	for (!t.eof || t.offset < len(t.ids)) && len(clusters) == 0 {
		if t.offset == len(t.ids) {
			t.ids = nil
			// Retrieve cluster ids that the related cluster or hosts have been updated after the timeToCompare
			err = t.db.Raw("select distinct(cid) as id from (select id as cid from clusters where trigger_monitor_timestamp > ?  and clusters.id > ? union select cluster_id as cid from hosts where trigger_monitor_timestamp > ? and hosts.cluster_id > ?) as t order by id limit ?",
				t.timeToCompare, t.lastId, t.timeToCompare, t.lastId, IdsQuerySize).Pluck("id", &t.ids).Error
			if err != nil {
				return clusters, err
			}
			if len(t.ids) < IdsQuerySize {
				t.eof = true
			}
			if len(t.ids) > 0 {
				t.lastId = t.ids[len(t.ids)-1]
			}
			t.offset = 0
		}
		for len(clusters) == 0 && t.offset < len(t.ids) {
			nextOffset := min(t.offset+t.batchSize, len(t.ids))

			// Query according to moving range on the id slice
			err = t.dbWithCondition.Where("id in (?)", t.ids[t.offset:nextOffset]).Find(&clusters).Error
			if err != nil {
				return clusters, err
			}
			t.offset = nextOffset
		}
	}
	return clusters, nil
}

type MonitorQueryGenerator struct {
	lastInvokeTime  time.Time
	calls           int64
	db              *gorm.DB
	dbWithCondition *gorm.DB
	batchSize       int
}

func NewMonitorQueryGenerator(db, dbWithCondition *gorm.DB, batchSize int) *MonitorQueryGenerator {
	if batchSize < 1 {
		batchSize = DefaultBatchSize
	}
	return &MonitorQueryGenerator{
		db:              db,
		dbWithCondition: dbWithCondition,
		batchSize:       batchSize,
	}
}

func timeForDuration(d time.Duration) time.Time {
	return time.Now().Add(-d)
}

func (m *MonitorQueryGenerator) NewQuery() MonitorQuery {
	newInvokeTime := time.Now()
	defer func() {
		m.lastInvokeTime = newInvokeTime
		m.calls++
	}()
	if m.calls == 0 ||
		m.lastInvokeTime.Minute()/5 != newInvokeTime.Minute()/5 {
		return &fullQuery{
			dbWithCondition: m.dbWithCondition,
			batchSize:       m.batchSize,
		}
	}

	if m.lastInvokeTime.Minute() != newInvokeTime.Minute() {
		return &timedQuery{
			db:              m.db,
			dbWithCondition: m.dbWithCondition,
			timeToCompare:   timeForDuration(15 * time.Minute),
			batchSize:       m.batchSize,
		}
	}
	return &timedQuery{
		db:              m.db,
		dbWithCondition: m.dbWithCondition,
		timeToCompare:   timeForDuration(5 * time.Minute),
		batchSize:       m.batchSize,
	}
}
