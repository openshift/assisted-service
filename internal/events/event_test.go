package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Events library", func() {
	var (
		db        *gorm.DB
		theEvents *events.Events
		dbName    string
		cluster1  = strfmt.UUID("46a8d745-dfce-4fd8-9df0-549ee8eabb3d")
		cluster2  = strfmt.UUID("60415d9c-7c44-4978-89f5-53d510b03a47")
		host      = strfmt.UUID("1e45d128-4a69-4e71-9b50-a0c627217f3e")
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		theEvents = events.New(db, logrus.WithField("pkg", "events"))
	})
	numOfEvents := func(clusterID *strfmt.UUID, hostID *strfmt.UUID, infraEnvID *strfmt.UUID) int {
		evs, err := theEvents.V2GetEvents(clusterID, hostID, infraEnvID)
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
			theEvents.AddEvent(context.TODO(), cluster1, nil, models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(0))

			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			theEvents.AddEvent(context.TODO(), cluster2, nil, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(1))
		})

		It("Adding a host event ", func() {
			theEvents.AddEvent(context.TODO(), cluster1, nil, models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(0))

			theEvents.AddEvent(context.TODO(), cluster1, &host, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(2))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(1))
		})

		It("Adding same event multiple times", func() {
			t1 := time.Now().Add(-1 * time.Second) // 1 second ago
			theEvents.AddEvent(context.TODO(), cluster1, nil, models.EventSeverityInfo, "event1", t1)
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			t2 := time.Now()
			theEvents.AddEvent(context.TODO(), cluster1, nil, models.EventSeverityInfo, "event1", t2)
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(2))

			evs, err = theEvents.V2GetEvents(&cluster1, nil, nil)
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
			theEvents.AddEvent(ctx, cluster1, &host, models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(1))

			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			evs, err = theEvents.V2GetEvents(&cluster1, &host, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))
		})
	})

	Context("additional properties", func() {
		It("multiple properties", func() {
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "e1", time.Now(),
				"p1", "abcd", "p2", 6.0)
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
			Expect(evs[0]).Should(WithProperty("p2", 6.0))
		})

		It("map properties", func() {
			var props = map[string]interface{}{"p1": "abcd"}
			theEvents.AddEvent(context.TODO(), cluster1, nil, models.EventSeverityInfo, "e1", time.Now(),
				props)
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
		})

		It("bad properties", func() {
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "e1", time.Now(),
				"p1")
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0].Props).Should(Equal(""))
		})
	})

	Context("event category", func() {
		BeforeEach(func() {
			for _, s := range events.DefaultEventCategories {
				theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, s, time.Now())
			}
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "metrics", time.Now())
		})
		It("GetEvents with default category", func() {
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(len(events.DefaultEventCategories)))
		})
		It("GetEvents with selected category", func() {
			evs, err := theEvents.V2GetEvents(&cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(*evs[0].Message).Should(Equal("metrics"))
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
