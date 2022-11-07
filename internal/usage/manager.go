package usage

import (
	"encoding/json"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ API = &UsageManager{}

type FeatureUsage map[string]models.Usage

//go:generate mockgen -source=manager.go -package=usage -destination=mock_usage_manager.generated_go
type API interface {
	Add(usages FeatureUsage, Name string, data *map[string]interface{})
	Remove(usages FeatureUsage, name string)
	Save(db *gorm.DB, clusterId strfmt.UUID, usages FeatureUsage)
}

type UsageManager struct {
	log logrus.FieldLogger
}

func NewManager(log logrus.FieldLogger) *UsageManager {
	return &UsageManager{
		log: log,
	}
}

func (m *UsageManager) Add(usages FeatureUsage, name string, data *map[string]interface{}) {
	id := UsageNameToID(name)
	usage := models.Usage{
		Name: name,
		ID:   id,
	}
	if data != nil {
		usage.Data = *data
	}
	//UPSERT the usage record since feature usage is measured once per cluster
	usages[name] = usage
}

func (m *UsageManager) Remove(usages FeatureUsage, name string) {
	delete(usages, name)
}

func (m *UsageManager) Save(db *gorm.DB, clusterId strfmt.UUID, usages FeatureUsage) {
	b, err := json.Marshal(usages)
	if err == nil {
		err = db.Model(&common.Cluster{}).Where("id = ?", clusterId).Update("feature_usage", string(b)).Error
	}
	if err != nil {
		m.log.WithError(err).Errorf("Failed to update usages %v", usages)
	}
}

func Unmarshal(str string) (FeatureUsage, error) {
	var result FeatureUsage = make(map[string]models.Usage)
	if str == "" {
		return result, nil
	}
	err := json.Unmarshal([]byte(str), &result)
	return result, err
}

func UsageNameToID(featureKey string) string {
	name := featureKey
	r := strings.NewReplacer(" ", "_", ".", "")
	id := strings.ToUpper(r.Replace(name))
	return id
}
