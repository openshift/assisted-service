package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/pborman/uuid"

	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

/*
Given events library
	Initially
		There are no events
*/
var _ = Describe("Events library", func() {
	var (
		db        *gorm.DB
		theEvents *events.Events
		dbName    = "events_test"
	)
	BeforeEach(func() {
		db = common.PrepareTestDB(dbName, &events.Event{})
		theEvents = events.New(db, logrus.WithField("pkg", "events"))
	})
	numOfEvents := func(id string) int {
		evs, err := theEvents.GetEvents(id)
		Expect(err).Should(BeNil())
		return len(evs)
	}

	Context("Initially", func() {
		It("No events for id '1' ", func() {
			nEvents := numOfEvents("1")
			Expect(nEvents).Should(Equal(0))
		})
		It("No events for id '2' ", func() {
			nEvents := numOfEvents("2")
			Expect(nEvents).Should(Equal(0))
		})

	})

	Context("With events", func() {
		It("Adding a single event", func() {
			theEvents.AddEvent(context.TODO(), "1", models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents("1")).Should(Equal(1))
			Expect(numOfEvents("2")).Should(Equal(0))
			Expect(numOfEvents("3")).Should(Equal(0))

			evs, err := theEvents.GetEvents("1")
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			theEvents.AddEvent(context.TODO(), "2", models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents("1")).Should(Equal(1))
			Expect(numOfEvents("2")).Should(Equal(1))
			Expect(numOfEvents("3")).Should(Equal(0))
		})

		It("Adding events for multiple ids ", func() {
			theEvents.AddEvent(context.TODO(), "1", models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents("1")).Should(Equal(1))
			Expect(numOfEvents("2")).Should(Equal(0))
			Expect(numOfEvents("3")).Should(Equal(0))
			theEvents.AddEvent(context.TODO(), "2", models.EventSeverityInfo, "event2", time.Now(), "1", "3")
			Expect(numOfEvents("1")).Should(Equal(2))
			Expect(numOfEvents("2")).Should(Equal(1))
			Expect(numOfEvents("3")).Should(Equal(1))
		})

		It("Adding same event multiple times", func() {
			t1 := time.Now()
			theEvents.AddEvent(context.TODO(), "1", models.EventSeverityInfo, "event1", t1)
			Expect(numOfEvents("1")).Should(Equal(1))
			evs, err := theEvents.GetEvents("1")
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			t2 := time.Now()
			theEvents.AddEvent(context.TODO(), "1", models.EventSeverityInfo, "event1", t2)
			Expect(numOfEvents("1")).Should(Equal(2))

			evs, err = theEvents.GetEvents("1")
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t2))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			Expect(numOfEvents("2")).Should(Equal(0))
			Expect(numOfEvents("3")).Should(Equal(0))
		})
	})

	Context("events with request ID", func() {
		It("events with request ID", func() {
			ctx := context.Background()
			rid1 := uuid.NewRandom().String()
			ctx = requestid.ToContext(ctx, rid1)
			theEvents.AddEvent(ctx, "1", models.EventSeverityInfo, "event1", time.Now(), "2")
			Expect(numOfEvents("1")).Should(Equal(1))

			evs, err := theEvents.GetEvents("1")
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			evs, err = theEvents.GetEvents("2")
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithRequestID(rid1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))
		})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

})

func WithRequestID(requestID string) types.GomegaMatcher {
	return WithTransform(func(e *events.Event) string {
		return e.RequestID.String()
	}, Equal(requestID))
}

func WithMessage(msg *string) types.GomegaMatcher {
	return WithTransform(func(e *events.Event) *string {
		return e.Message
	}, Equal(msg))
}

func WithSeverity(severity *string) types.GomegaMatcher {
	return WithTransform(func(e *events.Event) *string {
		return e.Severity
	}, Equal(severity))
}

func WithTime(t time.Time) types.GomegaMatcher {
	return WithTransform(func(e *events.Event) time.Time {
		return time.Time(*e.EventTime)
	}, BeTemporally("~", t, time.Millisecond*100))
}

func TestEvents(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "Events test Suite")
}
