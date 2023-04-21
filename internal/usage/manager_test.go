package usage

import (
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestUsageEvents(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "Usage test Suite")
}

var _ = Describe("Feature Usage", func() {
	var (
		db        *gorm.DB
		dbName    string
		manager   *UsageManager
		clusterID strfmt.UUID
		ctrl      *gomock.Controller
	)

	var _ = BeforeSuite(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		manager = NewManager(logrus.WithField("pkg", "usage"), commontesting.GetDummyNotificationStream(ctrl))
		clusterID = strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID: &clusterID,
		},
		}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
	})

	var _ = AfterSuite(func() {
		common.DeleteTestDB(db, dbName)
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
