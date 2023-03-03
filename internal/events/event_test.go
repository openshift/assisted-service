package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/restapi"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

var _ = Describe("Events library", func() {
	var (
		ctrl      *gomock.Controller
		db        *gorm.DB
		theEvents eventsapi.Handler
		dbName    string
		cluster1  = strfmt.UUID("46a8d745-dfce-4fd8-9df0-549ee8eabb3d")
		cluster2  = strfmt.UUID("60415d9c-7c44-4978-89f5-53d510b03a47")
		infraEnv1 = strfmt.UUID("46a8d745-dfce-4a69-9df0-a0c627217f3e")
		infraEnv2 = strfmt.UUID("705c994b-eaa0-4b77-880b-66d4cd34cb4e")
		host      = strfmt.UUID("1e45d128-4a69-4e71-9b50-a0c627217f3e")
		host2     = strfmt.UUID("ad647798-a7af-4b1d-b96e-69f1beb9b4c3")
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		theEvents = New(db, nil, commontesting.GetDummyNotificationStream(ctrl), logrus.WithField("pkg", "events"))
		c1 := common.Cluster{Cluster: models.Cluster{ID: &cluster1, OpenshiftClusterID: strfmt.UUID(uuid.New().String()), UserName: "user1", OrgID: "org1"}}
		Expect(db.Create(&c1).Error).ShouldNot(HaveOccurred())
		c2 := common.Cluster{Cluster: models.Cluster{ID: &cluster2, OpenshiftClusterID: strfmt.UUID(uuid.New().String()), UserName: "user2", OrgID: "org1"}}
		Expect(db.Create(&c2).Error).ShouldNot(HaveOccurred())
		i1 := common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnv1, UserName: "user1", OrgID: "org1"}}
		Expect(db.Create(&i1).Error).ShouldNot(HaveOccurred())
		i2 := common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnv2, UserName: "user2", OrgID: "org1"}}
		Expect(db.Create(&i2).Error).ShouldNot(HaveOccurred())
	})
	numOfEvents := func(clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID) int {
		evs, err := theEvents.V2GetEvents(context.TODO(), clusterID, hostID, infraEnvID)
		Expect(err).Should(BeNil())
		return len(evs)
	}

	Context("Initially", func() {
		It("No events for cluster1 ", func() {
			nEvents := numOfEvents(&cluster1, nil, nil)
			Expect(nEvents).Should(Equal(0))
		})
		It("No events for cluster2 ", func() {
			nEvents := numOfEvents(&cluster2, nil, nil)
			Expect(nEvents).Should(Equal(0))
		})

	})

	Context("With events", func() {
		It("Adding a cluster event", func() {
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(0))

			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			theEvents.V2AddEvent(context.TODO(), &cluster2, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(1))
		})

		It("Adding a host event ", func() {
			test_host := &common.Host{
				KubeKeyNamespace: "test_namespace",
				Host: models.Host{
					ID:         &host,
					InfraEnvID: infraEnv1,
				},
			}
			Expect(db.Create(&test_host).Error).ShouldNot(HaveOccurred())
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil,
				eventgen.HostRegistrationSucceededEventName, models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(0))

			theEvents.V2AddEvent(context.TODO(), &cluster1, &host, nil, eventgen.HostRegistrationSucceededEventName, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(2))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(1))
		})

		It("Adding same event multiple times", func() {
			t1 := time.Now().Add(-1 * time.Second) // 1 second ago
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "event1", t1)
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			t2 := time.Now()
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil,
				eventgen.ClusterRegistrationSucceededEventName, models.EventSeverityInfo, "event1", t2)
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(2))

			evs, err = theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(0))
		})
	})

	Context("events with request ID", func() {
		It("events with request ID", func() {
			ctx := context.Background()
			rid1 := uuid.New().String()
			ctx = requestid.ToContext(ctx, rid1)
			test_host := &common.Host{
				KubeKeyNamespace: "test_namespace",
				Host: models.Host{
					ID:         &host,
					InfraEnvID: infraEnv1,
				},
			}
			Expect(db.Create(&test_host).Error).ShouldNot(HaveOccurred())
			theEvents.V2AddEvent(ctx, &cluster1, &host, nil, eventgen.HostRegistrationSucceededEventName, models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(1))

			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			evs, err = theEvents.V2GetEvents(context.TODO(), &cluster1, &host, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))
		})
	})

	Context("additional properties", func() {
		It("multiple properties", func() {
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, "fake_event", models.EventSeverityInfo, "e1", time.Now(),
				"p1", "abcd", "p2", 6.0)
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
			Expect(evs[0]).Should(WithProperty("p2", 6.0))
		})

		It("map properties", func() {
			var props = map[string]interface{}{"p1": "abcd"}
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, "fake_event", models.EventSeverityInfo, "e1", time.Now(),
				props)
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
		})

		It("bad properties", func() {
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, "fake_event", models.EventSeverityInfo, "e1", time.Now(),
				"p1")
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0].Props).Should(Equal(""))
		})
	})

	Context("event category", func() {
		BeforeEach(func() {
			for _, s := range eventsapi.DefaultEventCategories {
				theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, "fake_event", models.EventSeverityInfo, s, time.Now())
			}
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, "fake_event", models.EventSeverityInfo, "metrics", time.Now())
		})
		It("GetEvents with default category", func() {
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(len(eventsapi.DefaultEventCategories)))
		})
		It("GetEvents with selected category", func() {
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(*evs[0].Message).Should(Equal("metrics"))
		})
	})

	Context("authorization", func() {
		var ctx context.Context
		var cluster3 strfmt.UUID

		BeforeEach(func() {
			ctx = context.TODO()
			cluster3 = strfmt.UUID(uuid.New().String())
			c3 := common.Cluster{Cluster: models.Cluster{ID: &cluster3, OpenshiftClusterID: strfmt.UUID(uuid.New().String()), UserName: "user1", OrgID: "org3"}}
			Expect(db.Create(&c3).Error).ShouldNot(HaveOccurred())

			host1 := common.Host{Host: models.Host{ID: &host, InfraEnvID: infraEnv1}}
			Expect(db.Create(&host1).Error).ShouldNot(HaveOccurred())

			theEvents.V2AddEvent(ctx, &cluster1, nil, nil,
				"cluster1-org1", models.EventSeverityInfo, "msg", time.Now())
			theEvents.V2AddEvent(ctx, &cluster2, nil, nil,
				"cluster2-org1", models.EventSeverityInfo, "msg", time.Now())
			theEvents.V2AddEvent(ctx, &cluster3, nil, nil,
				"cluster3-org3", models.EventSeverityInfo, "msg", time.Now())
			theEvents.V2AddEvent(ctx, nil, nil, &infraEnv1,
				"unbound-infra1-org1", models.EventSeverityInfo, "msg", time.Now())
			theEvents.V2AddEvent(ctx, nil, &host, &infraEnv1,
				"unbound-host-infra1-org1", models.EventSeverityInfo, "msg", time.Now())
			theEvents.V2AddEvent(ctx, &cluster1, &host, &infraEnv1,
				"bound-host-on-cluster1-infra1-org1", models.EventSeverityInfo, "msg", time.Now())
		})

		JustBeforeEach(func() {
			//inject RHSSO authorizer to the event handler
			cfg := &auth.Config{AuthType: auth.TypeRHSSO, EnableOrgTenancy: true}
			authz_handler := auth.NewAuthzHandler(cfg, nil, logrus.New(), db)
			theEvents.(*Events).authz = authz_handler
		})

		hasEvent := func(events []*common.Event, name string) bool {
			eventNames := funk.Map(events, func(ev *common.Event) string {
				return ev.Name
			}).([]string)
			return funk.ContainsString(eventNames, name)
		}
		Context("with admin role", func() {
			BeforeEach(func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.AdminRole
				ctx = context.WithValue(context.TODO(), restapi.AuthKey, payload)
			})
			It("gets all events", func() {
				evs, err := theEvents.V2GetEvents(ctx, nil, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(6))
			})

			It("gets cluster's events when specifying cluster", func() {
				evs, err := theEvents.V2GetEvents(ctx, &cluster1, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(2))
				Expect(hasEvent(evs, "cluster1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("gets infra-env's events when specifying infra-env", func() {
				evs, err := theEvents.V2GetEvents(ctx, nil, nil, &infraEnv1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(3))
				Expect(hasEvent(evs, "unbound-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "unbound-host-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())

			})
			It("gets hosts's events when specifying host", func() {
				evs, err := theEvents.V2GetEvents(ctx, nil, &host, &infraEnv1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(2))
				Expect(hasEvent(evs, "unbound-host-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("non-existing id returns empty list", func() {
				id := strfmt.UUID(uuid.New().String())
				evs, err := theEvents.V2GetEvents(ctx, &id, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(0))
			})
		})

		Context("with user role", func() {
			BeforeEach(func() {
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.UserRole
				payload.Username = "user1"
				payload.Organization = "org1"
				ctx = context.WithValue(context.TODO(), restapi.AuthKey, payload)
			})
			It("gets events on own clusters", func() {
				By("strictly own cluster")
				evs, err := theEvents.V2GetEvents(ctx, &cluster1, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(2))
				Expect(hasEvent(evs, "cluster1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())

				By("cluster owned by another user on the same org")
				evs, err = theEvents.V2GetEvents(ctx, &cluster2, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(1))
				Expect(hasEvent(evs, "cluster2-org1")).To(BeTrue())
			})

			It("cannot get events across orgs", func() {
				evs, err := theEvents.V2GetEvents(ctx, &cluster3, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(0))
			})

			It("get events on own infra_env", func() {
				evs, err := theEvents.V2GetEvents(ctx, nil, nil, &infraEnv1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(3))
				Expect(hasEvent(evs, "unbound-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "unbound-host-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("gets own events on bound host", func() {
				evs, err := theEvents.V2GetEvents(ctx, &cluster1, &host, &infraEnv1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(1))
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("gets own events on host with non bound infra-env", func() {
				//returns all events of host (bound and unbound)
				evs, err := theEvents.V2GetEvents(ctx, nil, &host, &infraEnv1)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(2))
				Expect(hasEvent(evs, "unbound-host-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("get own events on host by query the host id alone", func() {
				evs, err := theEvents.V2GetEvents(ctx, nil, &host, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(2))
				Expect(hasEvent(evs, "unbound-host-infra1-org1")).To(BeTrue())
				Expect(hasEvent(evs, "bound-host-on-cluster1-infra1-org1")).To(BeTrue())
			})

			It("can not get all events", func() {
				//This kind of query is restricted to admins only.
				//In reality, it only used by the ELK server
				evs, err := theEvents.V2GetEvents(ctx, nil, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(0))
			})

			It("non-existing returns empty list", func() {
				id := strfmt.UUID(uuid.New().String())
				evs, err := theEvents.V2GetEvents(ctx, &id, nil, nil)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(len(evs)).To(Equal(0))
			})
		})
	})

	Context("Limits", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		DescribeTable(
			"Discards event if sent multiple times",
			func(count int) {
				for i := 0; i < count; i++ {
					theEvents.V2AddEvent(
						ctx,
						&cluster1,
						&host,
						&infraEnv1,
						eventgen.UpgradeAgentFailedEventName,
						models.EventSeverityError,
						"Upgrade failed",
						time.Now(),
					)
				}
				events, err := theEvents.V2GetEvents(
					ctx,
					&cluster1,
					&host,
					&infraEnv1,
					models.EventCategoryUser,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(events).To(HaveLen(1))
			},
			Entry("Twice", 2),
			Entry("Ten times", 10),
			Entry("Many times", 42),
		)

		It("Doesn't discard events from different clusters", func() {
			theEvents.V2AddEvent(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				eventgen.UpgradeAgentFailedEventName,
				models.EventSeverityError,
				"Upgrade failed",
				time.Now(),
			)
			theEvents.V2AddEvent(
				ctx,
				&cluster2,
				&host2,
				&infraEnv2,
				eventgen.UpgradeAgentFailedEventName,
				models.EventSeverityError,
				"Upgrade failed",
				time.Now(),
			)
			events1, err := theEvents.V2GetEvents(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				models.EventCategoryUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(events1).To(HaveLen(1))
			events2, err := theEvents.V2GetEvents(
				ctx,
				&cluster2,
				&host2,
				&infraEnv2,
				models.EventCategoryUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(events2).To(HaveLen(1))
		})

		It("Doesn't discard event that doesn't exceed limit", func() {
			theEvents.V2AddEvent(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				eventgen.UpgradeAgentFailedEventName,
				models.EventSeverityError,
				"Upgrade failed",
				time.Now().Add(-2*time.Hour),
			)
			theEvents.V2AddEvent(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				eventgen.UpgradeAgentFailedEventName,
				models.EventSeverityError,
				"Upgrade failed",
				time.Now(),
			)
			events, err := theEvents.V2GetEvents(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				models.EventCategoryUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(events).To(HaveLen(2))
		})

		It("Doesn't discard events that don't have limits", func() {
			for i := 0; i < 2; i++ {
				theEvents.V2AddEvent(
					ctx,
					&cluster1,
					&host,
					&infraEnv1,
					eventgen.ClusterInstallationCompletedEventName,
					models.EventSeverityInfo,
					"Installation completed",
					time.Now(),
				)
			}
			events, err := theEvents.V2GetEvents(
				ctx,
				&cluster1,
				&host,
				&infraEnv1,
				models.EventCategoryUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(events).To(HaveLen(2))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
})

func WithRequestID(requestID string) types.GomegaMatcher {
	return WithTransform(func(e *common.Event) string {
		return e.RequestID.String()
	}, Equal(requestID))
}

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

func WithProperty(name string, value interface{}) types.GomegaMatcher {
	return WithTransform(func(e *common.Event) interface{} {
		props := make(map[string]interface{})
		_ = json.Unmarshal([]byte(e.Props), &props)
		return props[name]
	}, Equal(value))
}

func WithTime(t time.Time) types.GomegaMatcher {
	return WithTransform(func(e *common.Event) time.Time {
		return time.Time(*e.EventTime)
	}, BeTemporally("~", t, time.Millisecond*100))
}

func TestEvents(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "Events test Suite")
}
