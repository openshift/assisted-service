package common

import (
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/transaction"
	"github.com/pkg/errors"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	InstallationPreparationSucceeded = "success"
	InstallationPreparationFailed    = "failed"

	ProgressWeightPreparingForInstallationStage float64 = 0.1
	ProgressWeightInstallingStage               float64 = 0.7
	ProgressWeightFinalizingStage               float64 = 0.15

	NotificationTypeCluster  = "ClusterState"
	NotificationTypeEvent    = "Event"
	NotificationTypeHost     = "HostState"
	NotificationTypeInfraEnv = "InfraEnv"
)

type Notifiable interface {
	GetClusterID() *strfmt.UUID
	GetInfraEnvID() *strfmt.UUID
	GetHostID() *strfmt.UUID
	NotificationType() string
	Payload() any
}

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

	// Indication if we updated console_url in AMS subscription
	IsAmsSubscriptionConsoleUrlSet bool `json:"is_ams_subscription_console_url_set"`

	// ImageGenerated indicates if the discovery image was generated successfully. It will be used internally
	// when an image needs to be generated. In case the user request to generate an image with custom parameters,
	// and the generation failed, the value of ImageGenerated will be set to 'false'. In that case, providing the
	// same request with the same custom parameters will re-attempt to generate the image.
	ImageGenerated bool `json:"image_generated"`

	// Timestamp to trigger monitor. Monitor will be triggered if timestamp is recent
	TriggerMonitorTimestamp time.Time

	// StaticNetworkConfigured indicates if static network configuration was set for the ISO used by clusters' nodes
	StaticNetworkConfigured bool `json:"static_network_configured"`

	IgnoredClusterValidations string `gorm:"type:text"`
	IgnoredHostValidations    string `gorm:"type:text"`
	// Indicates if the cluster's event data has been uploaded
	Uploaded bool `json:"uploaded"`

	// A JSON blob in which cluster UI settings will be stored.
	UISettings string `json:"ui_settings"`
}

func (c *Cluster) GetClusterID() *strfmt.UUID {
	return c.Cluster.ID
}
func (c *Cluster) GetInfraEnvID() *strfmt.UUID {
	return nil
}
func (c *Cluster) GetHostID() *strfmt.UUID {
	return nil
}

func (c *Cluster) NotificationType() string {
	return NotificationTypeCluster
}

func (c *Cluster) Payload() any {
	return &c.Cluster
}

type Event struct {
	gorm.Model
	models.Event
}

func (e *Event) GetClusterID() *strfmt.UUID {
	return e.ClusterID
}
func (e *Event) GetInfraEnvID() *strfmt.UUID {
	return e.InfraEnvID
}
func (e *Event) GetHostID() *strfmt.UUID {
	return e.HostID
}

func (e *Event) NotificationType() string {
	return NotificationTypeEvent
}

func (e *Event) Payload() any {
	return &e.Event
}

type Host struct {
	models.Host
	Approved bool `json:"approved"`

	// Namespace of the KubeAPI resource
	KubeKeyNamespace string `json:"kube_key_namespace"`

	// Timestamp to trigger monitor. Monitor will be triggered if timestamp is recent
	TriggerMonitorTimestamp time.Time

	// A string which will be used as Authorization Bearer token to fetch the ignition from ignition_endpoint_url.
	IgnitionEndpointToken string `json:"ignition_endpoint_token" gorm:"type:TEXT"`

	// Json formatted string of the additional HTTP headers when fetching the ignition.
	IgnitionEndpointHTTPHeaders string `json:"ignition_endpoint_http_headers,omitempty" gorm:"type:TEXT"`
}

func (h *Host) GetClusterID() *strfmt.UUID {
	return h.ClusterID
}
func (h *Host) GetInfraEnvID() *strfmt.UUID {
	return &h.InfraEnvID
}
func (h *Host) GetHostID() *strfmt.UUID {
	return h.ID
}

func (h *Host) NotificationType() string {
	return NotificationTypeHost
}

func (h *Host) Payload() any {
	return &h.Host
}

type InfraEnv struct {
	models.InfraEnv

	// The pull secret that obtained from the Pull Secret page on the Red Hat OpenShift Cluster Manager site.
	PullSecret string `json:"pull_secret" gorm:"type:TEXT"`

	// Namespace of the KubeAPI resource
	KubeKeyNamespace string `json:"kube_key_namespace"`

	ProxyHash string `json:"proxy_hash"`

	// Generated indicates if the discovery image was generated successfully. It will be used internally
	// when an image needs to be generated. In case the user request to generate an image with custom parameters,
	// and the generation failed, the value of Generated will be set to 'false'. In that case, providing the
	// same request with the same custom parameters will re-attempt to generate the image.
	Generated bool `json:"generated"`

	// Timestamp set for time when image ws actually generated
	GeneratedAt strfmt.DateTime `json:"generated_at" gorm:"type:timestamp with time zone"`

	// Timestamp for expiration of the image
	ImageExpiresAt strfmt.DateTime `json:"image_expires_at" gorm:"type:timestamp with time zone"`

	// Hosts relationship
	// TODO Add a helper function(s) to load InfraEnv(s) with eager-loading parameter
	Hosts []*Host `json:"hosts" gorm:"foreignkey:InfraEnvID;references:ID"`

	ImageTokenKey string `json:"image_token_key"`

	// Json formatted string containing internal overrides for the default ignition config.
	// This is used for adding ironic ignition config to the assisted ignition config
	InternalIgnitionConfigOverride string `json:"internal_ignition_config_override,omitempty"`
}

func (i *InfraEnv) GetClusterID() *strfmt.UUID {
	return &i.ClusterID
}
func (i *InfraEnv) GetInfraEnvID() *strfmt.UUID {
	return i.ID
}
func (i *InfraEnv) GetHostID() *strfmt.UUID {
	return nil
}

func (i *InfraEnv) NotificationType() string {
	return NotificationTypeInfraEnv
}

func (i *InfraEnv) Payload() any {
	return &i.InfraEnv
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
	ClusterNetworksTable    = "ClusterNetworks"
	ServiceNetworksTable    = "ServiceNetworks"
	MachineNetworksTable    = "MachineNetworks"
	APIVIPsTable            = "APIVips"
	IngressVIPsTable        = "IngressVips"
)

var ClusterSubTables = [...]string{
	HostsTable,
	MonitoredOperatorsTable,
	ClusterNetworksTable,
	ServiceNetworksTable,
	MachineNetworksTable,
	APIVIPsTable,
	IngressVIPsTable,
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&models.MonitoredOperator{},
		&Host{},
		&Cluster{},
		&Event{},
		&InfraEnv{},
		&models.ReleaseImage{},
		&models.ClusterNetwork{},
		&models.ServiceNetwork{},
		&models.MachineNetwork{},
		&models.APIVip{},
		&models.IngressVip{},
	)
}

func LoadTableFromDB(db *gorm.DB, tableName string, conditions ...interface{}) *gorm.DB {
	return db.Preload(tableName, conditions...)
}

func LoadClusterTablesFromDB(db *gorm.DB, excludeTables ...string) *gorm.DB {
	for _, subTable := range ClusterSubTables {
		if funk.Contains(excludeTables, subTable) {
			continue
		}
		db = LoadTableFromDB(db, subTable)
	}
	return db
}

func GetClusterFromDB(db *gorm.DB, clusterId strfmt.UUID, eagerLoading EagerLoadingState) (*Cluster, error) {
	c, err := GetClusterFromDBWhere(db, eagerLoading, SkipDeletedRecords, "id = ?", clusterId.String())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get cluster %s", clusterId.String())
	}

	return c, nil
}

func GetClusterFromDBForUpdate(db *gorm.DB, clusterId strfmt.UUID, eagerLoading EagerLoadingState) (*Cluster, error) {
	c, err := GetClusterFromDBWhereForUpdate(db, eagerLoading, SkipDeletedRecords, "id = ?", clusterId.String())
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get cluster %s", clusterId.String())
	}

	return c, nil
}

func GetClusterFromDBWithHosts(db *gorm.DB, clusterId strfmt.UUID) (*Cluster, error) {
	db = LoadTableFromDB(db, HostsTable)
	db = LoadClusterTablesFromDB(db, HostsTable)

	return GetClusterFromDB(db, clusterId, SkipEagerLoading)
}

func GetClusterFromDBWithVips(db *gorm.DB, clusterId strfmt.UUID) (*Cluster, error) {
	db = LoadTableFromDB(db, APIVIPsTable)
	db = LoadTableFromDB(db, IngressVIPsTable)
	return GetClusterFromDB(db, clusterId, SkipEagerLoading)
}

func prepareClusterDB(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, conditions ...interface{}) *gorm.DB {
	if includeDeleted {
		db = db.Unscoped()
	}

	conditions = append(conditions, func(db *gorm.DB) *gorm.DB {
		if includeDeleted {
			return db.Unscoped()
		}
		return db
	})

	if eagerLoading {
		for _, tableName := range ClusterSubTables {
			db = LoadTableFromDB(db, tableName, conditions...)
		}
	}

	return db
}

func GetClusterFromDBWhere(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, where ...interface{}) (*Cluster, error) {
	var cluster Cluster

	db = prepareClusterDB(db, eagerLoading, includeDeleted)
	err := db.Take(&cluster, where...).Error
	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func GetClusterFromDBWhereForUpdate(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, where ...interface{}) (*Cluster, error) {
	var cluster Cluster

	forUpdateCondition := func(db *gorm.DB) *gorm.DB {
		return transaction.AddForUpdateQueryOption(db)
	}

	db = prepareClusterDB(transaction.AddForUpdateQueryOption(db), eagerLoading, includeDeleted, forUpdateCondition)
	err := db.Take(&cluster, where...).Error
	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func GetClustersFromDBWhere(db *gorm.DB, eagerLoading EagerLoadingState, includeDeleted DeleteRecordsState, where ...interface{}) ([]*Cluster, error) {
	var clusters []*Cluster

	db = prepareClusterDB(db, eagerLoading, includeDeleted)
	err := db.Find(&clusters, where...).Error
	if err != nil {
		return nil, err
	}
	return clusters, nil
}

func GetHostFromDB(db *gorm.DB, infraEnvId, hostId string) (*Host, error) {
	var host Host

	err := db.First(&host, "id = ? and infra_env_id = ?", hostId, infraEnvId).Error
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get host %s in infra_env %s", hostId, infraEnvId)
	}
	return &host, nil
}

func GetHostFromDBbyHostId(db *gorm.DB, hostId strfmt.UUID) (*Host, error) {
	var host Host

	err := db.First(&host, "id = ?", hostId.String()).Error
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get host %s", hostId)
	}
	return &host, nil
}
func GetClusterHostFromDB(db *gorm.DB, clusterId, hostId string) (*Host, error) {
	var host Host

	err := db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get host %s in cluster %s", hostId, clusterId)
	}
	return &host, nil
}

func GetHostFromDBWhere(db *gorm.DB, where ...interface{}) (*Host, error) {
	var host Host

	err := db.Take(&host, where...).Error
	if err != nil {
		return nil, err
	}
	return &host, nil
}

func GetHostsFromDBWhere(db *gorm.DB, where ...interface{}) ([]*Host, error) {
	var hosts []*Host

	err := db.Find(&hosts, where...).Error
	if err != nil {
		return nil, err
	}
	return hosts, nil
}

func DeleteHostFromDB(db *gorm.DB, hostId, infraEnvId string) error {
	return db.Where("id = ? and infra_env_id = ?", hostId, infraEnvId).Delete(&Host{}).Error
}

func GetInfraEnvFromDB(db *gorm.DB, infraEnvID strfmt.UUID) (*InfraEnv, error) {
	var infraEnv InfraEnv

	err := db.First(&infraEnv, "id = ?", infraEnvID.String()).Error
	if err != nil {
		return nil, err
	}
	return &infraEnv, nil
}

func GetInfraEnvHostsFromDB(db *gorm.DB, infraEnvID strfmt.UUID) ([]*Host, error) {
	return GetHostsFromDBWhere(db, "infra_env_id = ?", infraEnvID)
}

func GetInfraEnvsFromDBWhere(db *gorm.DB, where ...interface{}) ([]*InfraEnv, error) {
	var infraEnvs []*InfraEnv

	err := db.Find(&infraEnvs, where...).Error
	if err != nil {
		return nil, err
	}
	return infraEnvs, nil
}

func DeleteRecordsByClusterID(db *gorm.DB, clusterID strfmt.UUID, values []interface{}, where ...interface{}) error {
	for _, value := range values {
		if err := db.Where("cluster_id = ?", clusterID).Delete(value, where...).Error; err != nil {
			return err
		}
	}

	return nil
}

func GetInfraEnvFromDBWhere(db *gorm.DB, where ...interface{}) (*InfraEnv, error) {
	var infraEnv InfraEnv

	err := db.Take(&infraEnv, where...).Error
	if err != nil {
		return nil, err
	}
	return &infraEnv, nil
}

func ResetAutoAssignRoles(db *gorm.DB, onClusters interface{}) (int, error) {
	if db == nil {
		return 0, nil
	}

	reply := db.Model(&models.Host{}).Where("role = ?", models.HostRoleAutoAssign).
		Where("cluster_id in (?)", onClusters).
		Update("suggested_role", models.HostRoleAutoAssign)

	if err := reply.Error; err != nil {
		return 0, err
	}
	return int(reply.RowsAffected), nil
}

func ToModelsHosts(hosts []*Host) []*models.Host {
	ret := make([]*models.Host, 0)
	for _, h := range hosts {
		ret = append(ret, &h.Host)
	}
	return ret
}

func (c *Cluster) AfterFind(db *gorm.DB) error {
	for _, h := range c.Hosts {
		if h.Status == nil {
			continue
		}
		if *h.Status == models.HostStatusKnown {
			c.ReadyHostCount++
			c.EnabledHostCount++
			continue
		}
		c.EnabledHostCount++
	}
	c.TotalHostCount = int64(len(c.Hosts))
	return nil
}

func CreateInfraEnvForCluster(db *gorm.DB, cluster *Cluster, imageType models.ImageType) error {
	// generate key for signing rhsso image auth tokens
	imageTokenKey, err := gencrypto.HMACKey(32)
	if err != nil {
		return err
	}

	proxy := models.Proxy{
		HTTPProxy:  swag.String(cluster.HTTPProxy),
		HTTPSProxy: swag.String(cluster.HTTPSProxy),
		NoProxy:    swag.String(cluster.NoProxy),
	}
	infraEnv := &InfraEnv{InfraEnv: models.InfraEnv{
		ID:               cluster.ID,
		ClusterID:        *cluster.ID,
		OpenshiftVersion: cluster.OpenshiftVersion,
		PullSecretSet:    true,
		Proxy:            &proxy,
		CPUArchitecture:  cluster.CPUArchitecture,
		EmailDomain:      cluster.EmailDomain,
		OrgID:            cluster.OrgID,
		UserName:         cluster.UserName,
		Type:             ImageTypePtr(imageType),
	},
		PullSecret:    cluster.PullSecret,
		Generated:     false,
		ImageTokenKey: imageTokenKey,
	}
	return db.Create(infraEnv).Error
}

func CloseDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	_ = sqlDB.Close()
}
