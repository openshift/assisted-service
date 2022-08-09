package usage

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("Feature Usage", func() {
	var (
		db        *gorm.DB
		manager   *UsageManager
		clusterID strfmt.UUID
	)

	var _ = BeforeEach(func() {
		db, _ = common.PrepareTestDB()
		manager = NewManager(logrus.WithField("pkg", "usage"))
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
		},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	readUsages := func() map[string]models.Usage {
		var cluster common.Cluster
		db.First(&cluster, "id = ?", clusterID)
		result, err := Unmarshal(cluster.Cluster.FeatureUsage)
		Expect(err).ShouldNot(HaveOccurred())
		return result
	}

	It("add new feature usage with no data", func() {
		usages := map[string]models.Usage{
			"feat1": {Name: "feat1"},
		}
		manager.Add(usages, "feat2", nil)
		Expect(usages).To(ContainElement(models.Usage{Name: "feat2", Data: nil, ID: "FEAT2"}))
		manager.Save(db, clusterID, usages)
		Expect(len(readUsages())).To(Equal(2))
	})

	It("update feature usage with data", func() {
		usages := map[string]models.Usage{
			"feat1": {Name: "feat1"},
		}
		manager.Add(usages, "feat1", &map[string]interface{}{"prop1": "p1"})
		manager.Save(db, clusterID, usages)
		savedUsages := readUsages()
		Expect(len(savedUsages)).To(Equal(1))
		Expect(savedUsages["feat1"].Data).NotTo(BeNil())
	})
})
