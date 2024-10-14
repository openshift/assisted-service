package common

import (
	"time"

	"gorm.io/gorm"
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

type MonitorInitialQueryBuilder func(db *gorm.DB) *gorm.DB

type MonitorQuery interface {
	Next() ([]*Cluster, error)
}

type fullQuery struct {
	lastId            string
	db                *gorm.DB
	buildInitialQuery MonitorInitialQueryBuilder
	eof               bool
	batchSize         int
}

/*
Full scan (backward compatible) query.  Instead of using offset, the query uses the lastId to indicate where to start the next batch
*/
func (f *fullQuery) Next() ([]*Cluster, error) {
	var clusters []*Cluster
	if f.eof {
		return clusters, nil
	}
	if err := f.buildInitialQuery(f.db).Where("id > ?", f.lastId).Order("id").Limit(f.batchSize).Find(&clusters).Error; err != nil {
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
	buildInitialQuery MonitorInitialQueryBuilder

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
			err = t.buildInitialQuery(t.db).Where("id in (?)", t.ids[t.offset:nextOffset]).Find(&clusters).Error
			if err != nil {
				return clusters, err
			}
			t.offset = nextOffset
		}
	}
	return clusters, nil
}

type MonitorClusterQueryGenerator struct {
	lastInvokeTime    time.Time
	calls             int64
	db                *gorm.DB
	buildInitialQuery MonitorInitialQueryBuilder
	batchSize         int
}

func NewMonitorQueryGenerator(db *gorm.DB, buildInitialQuery MonitorInitialQueryBuilder, batchSize int) *MonitorClusterQueryGenerator {
	if batchSize < 1 {
		batchSize = DefaultBatchSize
	}
	return &MonitorClusterQueryGenerator{
		db:                db,
		buildInitialQuery: buildInitialQuery,
		batchSize:         batchSize,
	}
}

func timeForDuration(d time.Duration) time.Time {
	return time.Now().Add(-d)
}

func (m *MonitorClusterQueryGenerator) NewClusterQuery() MonitorQuery {
	newInvokeTime := time.Now()
	defer func() {
		m.lastInvokeTime = newInvokeTime
		m.calls++
	}()
	if m.calls == 0 ||
		m.lastInvokeTime.Minute()/5 != newInvokeTime.Minute()/5 {
		return &fullQuery{
			db:                m.db,
			buildInitialQuery: m.buildInitialQuery,
			batchSize:         m.batchSize,
		}
	}

	if m.lastInvokeTime.Minute() != newInvokeTime.Minute() {
		return &timedQuery{
			db:                m.db,
			buildInitialQuery: m.buildInitialQuery,
			timeToCompare:     timeForDuration(15 * time.Minute),
			batchSize:         m.batchSize,
		}
	}
	return &timedQuery{
		db:                m.db,
		buildInitialQuery: m.buildInitialQuery,
		timeToCompare:     timeForDuration(5 * time.Minute),
		batchSize:         m.batchSize,
	}
}

type MonitorInfraEnvQuery interface {
	Next() ([]*InfraEnv, error)
}

type dbQuery interface {
	query(lastId string) *gorm.DB
	preload() *gorm.DB
}

type fullDbQuery struct {
	db *gorm.DB
}

func (d *fullDbQuery) query(lastId string) *gorm.DB {
	return d.db.Raw("select distinct(infra_env_id) as id from hosts where (hosts.cluster_id = '' or hosts.cluster_id is null) and infra_env_id > ? order by id limit ?", lastId, IdsQuerySize)
}

func (d *fullDbQuery) preload() *gorm.DB {
	return d.db.Preload("Hosts", "cluster_id = '' or cluster_id is null")
}

type timedDbQuery struct {
	db *gorm.DB

	// The time to compare to the trigger_monitor_timestamp field
	timeToCompare time.Time
}

func (t *timedDbQuery) query(lastId string) *gorm.DB {
	return t.db.Raw("select distinct(infra_env_id) as id from hosts where (hosts.cluster_id = '' or hosts.cluster_id is null) and infra_env_id > ? and trigger_monitor_timestamp > ? order by id limit ?", lastId, t.timeToCompare, IdsQuerySize)
}

func (t timedDbQuery) preload() *gorm.DB {
	return t.db.Preload("Hosts", "trigger_monitor_timestamp > ? and (cluster_id = '' or cluster_id is null)", t.timeToCompare)
}

type infraEnvQuery struct {
	// Where to start the next query for ids
	lastId string

	// db connection to query ids
	dbQuery dbQuery

	// The relevant infra-env ids
	ids []string

	// True if last id was already received
	eof bool

	// Offset in the id list
	offset int

	// Max batch size (limit)
	batchSize int
}

/*
Full scan (backward compatible) query.  Instead of using offset, the query uses the lastId to indicate where to start the next batch
*/
func (f *infraEnvQuery) Next() ([]*InfraEnv, error) {
	var (
		infraEnvs []*InfraEnv
		err       error
	)
	if f.eof && f.offset == len(f.ids) {
		return infraEnvs, nil
	}
	for (!f.eof || f.offset < len(f.ids)) && len(infraEnvs) == 0 {
		if f.offset == len(f.ids) {
			f.ids = nil
			// Retrieve cluster ids that the related cluster or hosts have been updated after the timeToCompare
			err = f.dbQuery.query(f.lastId).Pluck("id", &f.ids).Error
			if err != nil {
				return infraEnvs, err
			}
			if len(f.ids) < IdsQuerySize {
				f.eof = true
			}
			if len(f.ids) > 0 {
				f.lastId = f.ids[len(f.ids)-1]
			}
			f.offset = 0
		}
		for len(infraEnvs) == 0 && f.offset < len(f.ids) {
			nextOffset := min(f.offset+f.batchSize, len(f.ids))

			// Query according to moving range on the id slice
			err = f.dbQuery.preload().Where("id in (?)", f.ids[f.offset:nextOffset]).Find(&infraEnvs).Error
			if err != nil {
				return infraEnvs, err
			}
			f.offset = nextOffset
		}
	}
	return infraEnvs, nil
}

type MonitorInfraEnvQueryGenerator struct {
	lastInvokeTime time.Time
	calls          int64
	db             *gorm.DB
	batchSize      int
}

func (m *MonitorInfraEnvQueryGenerator) NewInfraEnvQuery() MonitorInfraEnvQuery {
	newInvokeTime := time.Now()
	defer func() {
		m.lastInvokeTime = newInvokeTime
		m.calls++
	}()
	if m.calls == 0 ||
		m.lastInvokeTime.Minute()/5 != newInvokeTime.Minute()/5 {
		return &infraEnvQuery{
			dbQuery: &fullDbQuery{
				db: m.db,
			},
			batchSize: m.batchSize,
		}
	}

	if m.lastInvokeTime.Minute() != newInvokeTime.Minute() {
		return &infraEnvQuery{
			dbQuery: &timedDbQuery{
				db:            m.db,
				timeToCompare: timeForDuration(15 * time.Minute),
			},
			batchSize: m.batchSize,
		}
	}
	return &infraEnvQuery{
		dbQuery: &timedDbQuery{
			db:            m.db,
			timeToCompare: timeForDuration(5 * time.Minute),
		},
		batchSize: m.batchSize,
	}
}

func NewInfraEnvMonitorQueryGenerator(db *gorm.DB, batchSize int) *MonitorInfraEnvQueryGenerator {
	if batchSize < 1 {
		batchSize = DefaultBatchSize
	}
	return &MonitorInfraEnvQueryGenerator{
		db:        db,
		batchSize: batchSize,
	}
}
