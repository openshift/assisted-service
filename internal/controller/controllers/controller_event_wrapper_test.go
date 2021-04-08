package controllers

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Controller events wrapper", func() {
	var (
		db                   *gorm.DB
		cluster1             *common.Cluster
		cluster2             *common.Cluster
		theEvents            *events.Events
		cEventsWrapper       *controllerEventsWrapper
		mockCtrl             *gomock.Controller
		mockCRDEventsHandler *MockCRDEventsHandler
		dbName               string
		host                 = strfmt.UUID("1e45d128-4a69-4e71-9b50-a0c627217f3e")
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		mockCtrl = gomock.NewController(GinkgoT())
		theEvents = events.New(db, logrus.WithField("pkg", "events"))
		mockCRDEventsHandler = NewMockCRDEventsHandler(mockCtrl)
		cEventsWrapper = NewControllerEventsWrapper(mockCRDEventsHandler, theEvents, db, logrus.New())
		// create simple cluster
		clusterID1 := strfmt.UUID(uuid.New().String())
		cluster1 = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID1,
			},
			KubeKeyName:      "cluster1",
			KubeKeyNamespace: "cluster1Nm",
		}

		clusterID2 := strfmt.UUID(uuid.New().String())
		cluster2 = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID2,
			},
			KubeKeyName:      "cluster2",
			KubeKeyNamespace: "cluster2Nm",
		}

	})
	numOfEvents := func(clusterID strfmt.UUID, hostID *strfmt.UUID) int {
		evs, err := cEventsWrapper.GetEvents(clusterID, hostID)
		Expect(err).Should(BeNil())
		return len(evs)
	}

	Context("Initially", func() {
		It("No events for cluster1 ", func() {
			nEvents := numOfEvents(*cluster1.ID, nil)
			Expect(nEvents).Should(Equal(0))
		})
		It("No events for cluster2 ", func() {
			nEvents := numOfEvents(*cluster2.ID, nil)
			Expect(nEvents).Should(Equal(0))
		})

	})

	Context("With events", func() {
		It("Adding a cluster event", func() {
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.AddEvent(context.TODO(), *cluster1.ID, nil, models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents(*cluster1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(*cluster2.ID, nil)).Should(Equal(0))

			evs, err := cEventsWrapper.GetEvents(*cluster1.ID, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster2.KubeKeyName, cluster2.KubeKeyNamespace).Times(1)
			cEventsWrapper.AddEvent(context.TODO(), *cluster2.ID, nil, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(*cluster1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(*cluster2.ID, nil)).Should(Equal(1))
		})

		It("Adding a host event ", func() {
			mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(&host, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.AddEvent(context.TODO(), *cluster1.ID, &host, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(*cluster1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(*cluster1.ID, &host)).Should(Equal(1))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})

func WithMessage(msg *string) types.GomegaMatcher {
	return WithTransform(func(e *common.Event) *string {
		return e.Message
	}, Equal(msg))
}

func WithSeverity(severity *string) types.GomegaMatcher {
	return WithTransform(func(e *common.Event) *string {
		return e.Severity
	}, Equal(severity))
}
