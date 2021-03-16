package common

import (
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

type Cluster struct {
	models.Cluster
	// The pull secret that obtained from the Pull Secret page on the Red Hat OpenShift Cluster Manager site.
	PullSecret string `json:"pull_secret" gorm:"type:TEXT"`

	// The compute hash value of the http-proxy, https-proxy and no-proxy attributes, used internally to indicate
	// if the proxy settings were changed while downloading ISO
	ProxyHash string `json:"proxy_hash"`

	// Used to detect if DHCP allocation task is timed out
	MachineNetworkCidrUpdatedAt time.Time

	// The lease acquired for API vip
	ApiVipLease string `gorm:"type:text"`

	// The lease acquired for API vip
	IngressVipLease string `gorm:"type:text"`

	// Name of the KubeAPI resource
	KubeKeyName string `json:"kube_key_name"`

	// Namespace of the KubeAPI resource
	KubeKeyNamespace string `json:"kube_key_namespace"`

	// The ID of the subscription created in AMS
	AmsSubscriptionID strfmt.UUID `json:"ams_subscription_id"`

	// ImageGenerated indicates if the discovery image was generated successfully. It will be used internally
	// when an image needs to be generated. In case the user request to generate an image with custom parameters,
	// and the generation failed, the value of ImageGenerated will be set to 'false'. In that case, providing the
	// same request with the same custom parameters will re-attempt to generate the image.
	ImageGenerated bool `json:"image_generated"`
}

type Event struct {
	gorm.Model
	models.Event
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&models.MonitoredOperator{}, &Host{}, &Cluster{}, &Event{}).Error
}

type Host struct {
	models.Host
	Approved bool `json:"approved"`
}

type EagerLoadingState bool

const (
	UseEagerLoading  EagerLoadingState = true
	SkipEagerLoading EagerLoadingState = false
)

type DeleteRecordsState bool

const (
	IncludeDeletedRecords DeleteRecordsState = true
	SkipDeletedRecords    DeleteRecordsState = false
)

const (
	HostsTable              = "Hosts"
	MonitoredOperatorsTable = "MonitoredOperators"
)

var ClusterSubTables = [...]string{HostsTable, MonitoredOperatorsTable}

func LoadTableFromDB(db *gorm.DB, tableName string, conditions ...interface{}) *gorm.DB {
	return db.Preload(tableName, conditions...)
}

func GetClusterFromDB(db *gorm.DB, clusterId strfmt.UUID, eagerLoading EagerLoadingState) (*Cluster, error) {
	c, err := GetClusterFromDBWhere(db, eagerLoading, SkipDeletedRecords, "id = ?", clusterId.String())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get cluster %s", clusterId.String())
	}

	return c, nil
}

func GetClusterFromDBWithoutDisabledHosts(db *gorm.DB, clusterId strfmt.UUID) (*Cluster, error) {
	db = LoadTableFromDB(db, HostsTable, "status <> ?", models.HostStatusDisabled)
	db = LoadTableFromDB(db, MonitoredOperatorsTable)
	return GetClusterFromDB(db, clusterId, SkipEagerLoading)
}

func prepareClusterDB(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState) *gorm.DB {
	if includeDeleted {
		db = db.Unscoped()
	}

	if eagerLoading {
		for _, tableName := range ClusterSubTables {
			db = LoadTableFromDB(db, tableName, func(db *gorm.DB) *gorm.DB {
				if includeDeleted {
					return db.Unscoped()
				}
				return db
			})
		}
	}

	return db
}

func GetClusterFromDBWhere(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, where ...interface{}) (*Cluster, error) {
	var cluster Cluster

	db = prepareClusterDB(db, eagerLoading, includeDeleted)
	err := db.Take(&cluster, where...).Error
	return &cluster, err
}

func GetClustersFromDBWhere(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, where ...interface{}) ([]*Cluster, error) {
	var clusters []*Cluster

	db = prepareClusterDB(db, eagerLoading, includeDeleted)
	err := db.Find(&clusters, where...).Error
	return clusters, err
}

func GetHostFromDB(db *gorm.DB, clusterId, hostId string) (*Host, error) {
	var host Host
	err := db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get host %s in cluster %s", hostId, clusterId)
	}
	return &host, nil
}

func DeleteRecordsByClusterID(db *gorm.DB, clusterID strfmt.UUID, value interface{}, where ...interface{}) error {
	return db.Where("cluster_id = ?", clusterID).Delete(value, where...).Error
}
