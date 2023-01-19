package controllers

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("Controller events wrapper", func() {
	var (
		db                   *gorm.DB
		cluster1             *common.Cluster
		cluster2             *common.Cluster
		infraEnv1            *common.InfraEnv
		theEvents            eventsapi.Handler
		cEventsWrapper       *controllerEventsWrapper
		mockCtrl             *gomock.Controller
		mockCRDEventsHandler *MockCRDEventsHandler
		dbName               string
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		mockCtrl = gomock.NewController(GinkgoT())
		theEvents = events.New(db, nil, nil, logrus.WithField("pkg", "events"))
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
		err := db.Create(&cluster1).Error
		Expect(err).ShouldNot(HaveOccurred())

		clusterID2 := strfmt.UUID(uuid.New().String())
		cluster2 = &common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID2,
			},
			KubeKeyName:      "cluster2",
			KubeKeyNamespace: "cluster2Nm",
		}
		err = db.Create(&cluster2).Error
		Expect(err).ShouldNot(HaveOccurred())

		infraEnvId := strfmt.UUID(uuid.New().String())
		infraEnvName := "infraEnv1"
		infraEnv1 = &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:   &infraEnvId,
				Name: &infraEnvName,
			},
			KubeKeyNamespace: "infraEnv1Nm",
		}
		err = db.Create(&infraEnv1).Error
		Expect(err).ShouldNot(HaveOccurred())
	})
	numOfEvents := func(clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID) int {
		evs, err := cEventsWrapper.V2GetEvents(context.TODO(), clusterID, hostID, infraEnvID)
		Expect(err).Should(BeNil())
		return len(evs)
	}

	Context("Initially", func() {
		It("No events for cluster1 ", func() {
			nEvents := numOfEvents(cluster1.ID, nil, nil)
			Expect(nEvents).Should(Equal(0))
		})
		It("No events for cluster2 ", func() {
			nEvents := numOfEvents(cluster2.ID, nil, nil)
			Expect(nEvents).Should(Equal(0))
		})

	})

	Context("With events", func() {
		It("Adding a cluster event", func() {
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.V2AddEvent(context.TODO(), cluster1.ID, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(0))

			evs, err := cEventsWrapper.V2GetEvents(context.TODO(), cluster1.ID, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster2.KubeKeyName, cluster2.KubeKeyNamespace).Times(1)
			cEventsWrapper.V2AddEvent(context.TODO(), cluster2.ID, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(1))
		})

		It("Adding a host event ", func() {
			hostID1 := strfmt.UUID(uuid.New().String())
			host1 := common.Host{
				Host: models.Host{
					ID:         &hostID1,
					InfraEnvID: *infraEnv1.ID,
					ClusterID:  cluster1.ID,
					Status:     swag.String(models.HostStatusKnown),
					Kind:       swag.String(models.HostKindHost),
				},
				KubeKeyNamespace: "hostNm",
			}
			err := db.Create(&host1).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(host1.ID.String(), host1.KubeKeyNamespace).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.V2AddEvent(context.TODO(), nil, host1.ID, infraEnv1.ID,
				eventgen.HostRegistrationSucceededEventName, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(nil, host1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(nil, host1.ID, infraEnv1.ID)).Should(Equal(1))
		})

		It("Sending a cluster event", func() {
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendClusterEvent(context.TODO(),
				eventgen.NewInactiveClustersDeregisteredEvent(*cluster1.ID, "event1"))
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(0))

			evs, err := cEventsWrapper.V2GetEvents(context.TODO(), cluster1.ID, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster2.KubeKeyName, cluster2.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendClusterEvent(context.TODO(),
				eventgen.NewInactiveClustersDeregisteredEvent(*cluster2.ID, "event2"))
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(1))
		})

		It("Sending a cluster event with time", func() {
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendClusterEventAtTime(context.TODO(),
				eventgen.NewInactiveClustersDeregisteredEvent(*cluster1.ID, "event1"), time.Now())
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(0))

			evs, err := cEventsWrapper.V2GetEvents(context.TODO(), cluster1.ID, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster2.KubeKeyName, cluster2.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendClusterEventAtTime(context.TODO(),
				eventgen.NewInactiveClustersDeregisteredEvent(*cluster2.ID, "event2"), time.Now())
			Expect(numOfEvents(cluster1.ID, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(cluster2.ID, nil, nil)).Should(Equal(1))
		})

		It("Sending a host event", func() {
			hostID1 := strfmt.UUID(uuid.New().String())
			host1 := common.Host{
				Host: models.Host{
					ID:         &hostID1,
					InfraEnvID: *infraEnv1.ID,
					ClusterID:  cluster1.ID,
					Status:     swag.String(models.HostStatusKnown),
					Kind:       swag.String(models.HostKindHost),
				},
				KubeKeyNamespace: "hostNm",
			}
			err := db.Create(&host1).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(host1.ID.String(), host1.KubeKeyNamespace).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendHostEvent(context.TODO(),
				eventgen.NewHostRegistrationFailedEvent(*host1.ID, *infraEnv1.ID, cluster1.ID, "event1"))
			Expect(numOfEvents(nil, host1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(nil, host1.ID, infraEnv1.ID)).Should(Equal(1))
		})

		It("Sending a host event with time", func() {
			hostID1 := strfmt.UUID(uuid.New().String())
			host1 := common.Host{
				Host: models.Host{
					ID:         &hostID1,
					InfraEnvID: *infraEnv1.ID,
					ClusterID:  cluster1.ID,
					Status:     swag.String(models.HostStatusKnown),
					Kind:       swag.String(models.HostKindHost),
				},
				KubeKeyNamespace: "hostNm",
			}
			err := db.Create(&host1).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(host1.ID.String(), host1.KubeKeyNamespace).Times(1)
			mockCRDEventsHandler.EXPECT().NotifyClusterDeploymentUpdates(cluster1.KubeKeyName, cluster1.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendHostEventAtTime(context.TODO(),
				eventgen.NewHostRegistrationFailedEvent(*host1.ID, *infraEnv1.ID, cluster1.ID, "event1"), time.Now())
			Expect(numOfEvents(nil, host1.ID, nil)).Should(Equal(1))
			Expect(numOfEvents(nil, host1.ID, infraEnv1.ID)).Should(Equal(1))
		})

		It("Sending a host event - unbound", func() {
			hostID1 := strfmt.UUID(uuid.New().String())
			host1 := common.Host{
				Host: models.Host{
					ID:         &hostID1,
					InfraEnvID: *infraEnv1.ID,
					Status:     swag.String(models.HostStatusKnown),
					Kind:       swag.String(models.HostKindHost),
				},
				KubeKeyNamespace: "hostNm",
			}
			err := db.Create(&host1).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(host1.ID.String(), host1.KubeKeyNamespace).Times(1)
			cEventsWrapper.SendHostEvent(context.TODO(),
				eventgen.NewHostRegistrationFailedEvent(*host1.ID, *infraEnv1.ID, cluster1.ID, "event1"))
			Expect(numOfEvents(cluster1.ID, host1.ID, infraEnv1.ID)).Should(Equal(1))

		})
	})

	It("Sending a host event with time - unbound", func() {
		hostID1 := strfmt.UUID(uuid.New().String())
		host1 := common.Host{
			Host: models.Host{
				ID:         &hostID1,
				InfraEnvID: *infraEnv1.ID,
				Status:     swag.String(models.HostStatusKnown),
				Kind:       swag.String(models.HostKindHost),
			},
			KubeKeyNamespace: "hostNm",
		}
		err := db.Create(&host1).Error
		Expect(err).ShouldNot(HaveOccurred())

		mockCRDEventsHandler.EXPECT().NotifyAgentUpdates(host1.ID.String(), host1.KubeKeyNamespace).Times(1)
		cEventsWrapper.SendHostEventAtTime(context.TODO(),
			eventgen.NewHostRegistrationFailedEvent(*host1.ID, *infraEnv1.ID, cluster1.ID, "event1"), time.Now())
		Expect(numOfEvents(cluster1.ID, host1.ID, infraEnv1.ID)).Should(Equal(1))
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		mockCtrl.Finish()
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
