package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/requestid"
	"github.com/openshift/assisted-service/restapi"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var _ = Describe("Events library", func() {
	var (
		db        *gorm.DB
		theEvents eventsapi.Handler
		dbName    string
		cluster1  = strfmt.UUID("46a8d745-dfce-4fd8-9df0-549ee8eabb3d")
		cluster2  = strfmt.UUID("60415d9c-7c44-4978-89f5-53d510b03a47")
		infraEnv1 = strfmt.UUID("46a8d745-dfce-4a69-9df0-a0c627217f3e")
		host      = strfmt.UUID("1e45d128-4a69-4e71-9b50-a0c627217f3e")
	)
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		theEvents = events.New(db, logrus.WithField("pkg", "events"))
		c1 := common.Cluster{Cluster: models.Cluster{ID: &cluster1, OpenshiftClusterID: strfmt.UUID(uuid.New().String())}}
		Expect(db.Create(&c1).Error).ShouldNot(HaveOccurred())
		c2 := common.Cluster{Cluster: models.Cluster{ID: &cluster2, OpenshiftClusterID: strfmt.UUID(uuid.New().String())}}
		Expect(db.Create(&c2).Error).ShouldNot(HaveOccurred())
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
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "the event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster2, nil, nil)).Should(Equal(0))

			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("the event1")))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			theEvents.V2AddEvent(context.TODO(), &cluster2, nil, nil, models.EventSeverityInfo, "event2", time.Now())
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
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "event1", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(0))

			theEvents.V2AddEvent(context.TODO(), &cluster1, &host, nil, models.EventSeverityInfo, "event2", time.Now())
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(2))
			Expect(numOfEvents(&cluster1, &host, nil)).Should(Equal(1))
		})

		It("Adding same event multiple times", func() {
			t1 := time.Now().Add(-1 * time.Second) // 1 second ago
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "event1", t1)
			Expect(numOfEvents(&cluster1, nil, nil)).Should(Equal(1))
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil)
			Expect(err).Should(BeNil())
			Expect(evs[0]).Should(WithMessage(swag.String("event1")))
			Expect(evs[0]).Should(WithTime(t1))
			Expect(evs[0]).Should(WithSeverity(swag.String(models.EventSeverityInfo)))

			t2 := time.Now()
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "event1", t2)
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
			theEvents.V2AddEvent(ctx, &cluster1, &host, nil, models.EventSeverityInfo, "event1", time.Now())
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
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "e1", time.Now(),
				"p1", "abcd", "p2", 6.0)
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryMetrics)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
			Expect(evs[0]).Should(WithProperty("p2", 6.0))
		})

		It("map properties", func() {
			var props = map[string]interface{}{"p1": "abcd"}
			theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "e1", time.Now(),
				props)
			evs, err := theEvents.V2GetEvents(context.TODO(), &cluster1, nil, nil, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			Expect(evs[0]).Should(WithProperty("p1", "abcd"))
		})

		It("bad properties", func() {
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "e1", time.Now(),
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
				theEvents.V2AddEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, s, time.Now())
			}
			theEvents.V2AddMetricsEvent(context.TODO(), &cluster1, nil, nil, models.EventSeverityInfo, "metrics", time.Now())
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

	Context("events query filtering", func() {

		Context("query with no filters - expect no valid transaction error", func() {
			tests := []struct {
				name    string
				context context.Context
			}{
				{
					name:    "query by user with no filters - expect no valid transaction error",
					context: context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
				},
				{
					name:    "query with no user and with no filters - expect no valid transaction error",
					context: context.TODO(),
				},
			}

			for i := range tests {
				test := tests[i]
				It(test.name, func() {
					evs, err := theEvents.V2GetEvents(test.context, nil, nil, nil, models.EventCategoryUser)
					Expect(err).ShouldNot(BeNil())
					Expect(errors.Is(err, gorm.ErrInvalidTransaction)).Should(Equal(true))
					Expect(len(evs)).Should(Equal(0))
				})
			}
		})

		Context("query filtering by cluster_id", func() {
			tests := []struct {
				name          string
				username      string
				context       context.Context
				createCluster bool
				querySucess   bool
				err           error
			}{
				{
					name:          "filter by user owned cluster - user with user role",
					username:      "test_user1",
					context:       context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createCluster: true,
					querySucess:   true,
					err:           nil,
				},
				{
					name:          "filter by user owned cluster - user with admin role",
					username:      "admin_user1",
					context:       context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createCluster: true,
					querySucess:   true,
					err:           nil,
				},
				{
					name:          "filter by user which does not own the cluster - user with user role",
					username:      "test_user2",
					context:       context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createCluster: true,
					querySucess:   false,
					err:           gorm.ErrRecordNotFound,
				},
				{
					name:          "filter by user which does not own the cluster - user with admin role",
					username:      "admin_user1",
					context:       context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createCluster: true,
					querySucess:   true,
					err:           nil,
				},
				{
					name:          "filter by cluster_id - no user in context",
					username:      "",
					context:       context.TODO(),
					createCluster: true,
					querySucess:   true,
					err:           nil,
				},
				{
					name:          "filter by an invalid cluster_id - no user in context",
					username:      "",
					context:       context.TODO(),
					createCluster: false,
					querySucess:   false,
					err:           gorm.ErrRecordNotFound,
				},
				{
					name:          "filter by an invalid cluster_id - user with user role",
					username:      "test_user1",
					context:       context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createCluster: false,
					querySucess:   false,
					err:           gorm.ErrRecordNotFound,
				},
			}
			for i := range tests {
				test := tests[i]
				It(test.name, func() {
					clusterId := strfmt.UUID(uuid.New().String())
					if test.createCluster {
						c := common.Cluster{Cluster: models.Cluster{
							ID:                 &clusterId,
							UserName:           test.username,
							OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
						}}
						Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
					}
					theEvents.V2AddEvent(test.context, &clusterId, nil, nil, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
					evs, err := theEvents.V2GetEvents(test.context, &clusterId, nil, nil, models.EventCategoryUser)
					if test.querySucess {
						Expect(err).Should(BeNil())
						Expect(len(evs)).Should(Equal(1))
					} else {
						Expect(err).ShouldNot(BeNil())
						Expect(errors.Is(err, test.err)).Should(Equal(true))
						Expect(len(evs)).Should(Equal(0))
					}
				})
			}
		})

		Context("query filtering by infra_env_id", func() {
			tests := []struct {
				name           string
				username       string
				context        context.Context
				createInfraEnv bool
				querySucess    bool
				err            error
			}{
				{
					name:           "filter by user owned infra_env - user with user role",
					username:       "test_user1",
					context:        context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createInfraEnv: true,
					querySucess:    true,
					err:            nil,
				},
				{
					name:           "filter by user owned infra_env - user with admin role",
					username:       "admin_user1",
					context:        context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createInfraEnv: true,
					querySucess:    true,
					err:            nil,
				},
				{
					name:           "filter by user which does not own the infra_env - user with user role",
					username:       "test_user2",
					context:        context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createInfraEnv: true,
					querySucess:    false,
					err:            gorm.ErrRecordNotFound,
				},
				{
					name:           "filter by user which does not own the infra_env - user with admin role",
					username:       "admin_user1",
					context:        context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createInfraEnv: true,
					querySucess:    true,
					err:            nil,
				},
				{
					name:           "filter by infra_env_id - no user in context",
					username:       "",
					context:        context.TODO(),
					createInfraEnv: true,
					querySucess:    true,
					err:            nil,
				},
				{
					name:           "filter by an invalid infra_env_id - no user in context",
					username:       "",
					context:        context.TODO(),
					createInfraEnv: false,
					querySucess:    false,
					err:            gorm.ErrRecordNotFound,
				},
				{
					name:           "filter by an invalid infra_env_id - user with user role",
					username:       "test_user1",
					context:        context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createInfraEnv: false,
					querySucess:    false,
					err:            gorm.ErrRecordNotFound,
				},
			}
			for i := range tests {
				test := tests[i]
				It(test.name, func() {
					infraEnvId := strfmt.UUID(uuid.New().String())
					if test.createInfraEnv {
						infraEnvName := "test_infra_env"
						infraEnv := &common.InfraEnv{
							KubeKeyNamespace: "test_namespace",
							InfraEnv: models.InfraEnv{
								Name:     &infraEnvName,
								ID:       &infraEnvId,
								UserName: test.username,
							},
						}
						Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
					}
					theEvents.V2AddEvent(test.context, nil, nil, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
					evs, err := theEvents.V2GetEvents(test.context, nil, nil, &infraEnvId, models.EventCategoryUser)
					if test.querySucess {
						Expect(err).Should(BeNil())
						Expect(len(evs)).Should(Equal(1))
					} else {
						Expect(err).ShouldNot(BeNil())
						Expect(errors.Is(err, test.err)).Should(Equal(true))
						Expect(len(evs)).Should(Equal(0))
					}
				})
			}
		})

		Context("query filtering by host_id", func() {
			tests := []struct {
				name        string
				username    string
				context     context.Context
				createHost  bool
				querySucess bool
				err         error
			}{
				{
					name:        "filter by user owned host - user with user role",
					username:    "test_user1",
					context:     context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createHost:  true,
					querySucess: true,
					err:         nil,
				},
				{
					name:        "filter by user owned host - user with admin role",
					username:    "admin_user1",
					context:     context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createHost:  true,
					querySucess: true,
					err:         nil,
				},
				{
					name:        "filter by user which does not own the host - user with user role",
					username:    "test_user2",
					context:     context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createHost:  true,
					querySucess: false,
					err:         gorm.ErrRecordNotFound,
				},
				{
					name:        "filter by user which does not own the host - user with admin role",
					username:    "admin_user1",
					context:     context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "admin_user1", Role: ocm.AdminRole}),
					createHost:  true,
					querySucess: true,
					err:         nil,
				},
				{
					name:        "filter by host_id - no user in context",
					username:    "",
					context:     context.TODO(),
					createHost:  true,
					querySucess: true,
					err:         nil,
				},
				{
					name:        "filter by an invalid host_id - no user in context",
					username:    "",
					context:     context.TODO(),
					createHost:  false,
					querySucess: false,
					err:         gorm.ErrRecordNotFound,
				},
				{
					name:        "filter by an invalid host_id - user with user role",
					username:    "test_user1",
					context:     context.WithValue(context.Background(), restapi.AuthKey, &ocm.AuthPayload{Username: "test_user1", Role: ocm.UserRole}),
					createHost:  false,
					querySucess: false,
					err:         gorm.ErrRecordNotFound,
				},
			}
			for i := range tests {
				test := tests[i]
				It(test.name, func() {
					hostId := strfmt.UUID(uuid.New().String())
					if test.createHost {
						test_host := &common.Host{
							KubeKeyNamespace: "test_namespace",
							Host: models.Host{
								ID:         &hostId,
								InfraEnvID: infraEnv1,
								UserName:   test.username,
							},
						}
						Expect(db.Create(&test_host).Error).ShouldNot(HaveOccurred())
					}
					theEvents.V2AddEvent(test.context, nil, &hostId, nil, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
					evs, err := theEvents.V2GetEvents(test.context, nil, &hostId, nil, models.EventCategoryUser)
					if test.querySucess {
						Expect(err).Should(BeNil())
						Expect(len(evs)).Should(Equal(1))
					} else {
						Expect(err).ShouldNot(BeNil())
						Expect(errors.Is(err, test.err)).Should(Equal(true))
						Expect(len(evs)).Should(Equal(0))
					}
				})
			}
		})

		It("use multiple filters in the same query", func() {
			user1 := "test_user1"
			clusterId := strfmt.UUID(uuid.New().String())
			infraEnvId := strfmt.UUID(uuid.New().String())
			hostId1 := strfmt.UUID(uuid.New().String())
			hostId2 := strfmt.UUID(uuid.New().String())
			hostId3 := strfmt.UUID(uuid.New().String())
			payload := &ocm.AuthPayload{}
			payload.Username = user1
			ctx := context.WithValue(context.Background(), restapi.AuthKey, payload)
			c := common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterId,
				UserName:           user1,
				Status:             swag.String(models.ClusterStatusError),
				OpenshiftClusterID: strfmt.UUID(uuid.New().String()),
			}}
			infraEnvName := "test_infra_env"
			infraEnv := &common.InfraEnv{
				KubeKeyNamespace: "test_namespace",
				InfraEnv: models.InfraEnv{
					Name:     &infraEnvName,
					ID:       &infraEnvId,
					UserName: user1,
				},
			}
			testHost1 := &common.Host{
				KubeKeyNamespace: "test_namespace",
				Host: models.Host{
					ID:         &hostId1,
					InfraEnvID: infraEnvId,
					UserName:   user1,
				},
			}
			testHost2 := &common.Host{
				KubeKeyNamespace: "test_namespace",
				Host: models.Host{
					ID:         &hostId2,
					InfraEnvID: infraEnvId,
					UserName:   user1,
				},
			}
			testHost3 := &common.Host{
				KubeKeyNamespace: "test_namespace",
				Host: models.Host{
					ID:         &hostId3,
					InfraEnvID: infraEnvId,
					UserName:   user1,
				},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&testHost1).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&testHost2).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&testHost3).Error).ShouldNot(HaveOccurred())

			theEvents.V2AddEvent(ctx, &clusterId, nil, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
			theEvents.V2AddEvent(ctx, nil, nil, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
			theEvents.V2AddEvent(ctx, nil, &hostId1, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
			theEvents.V2AddEvent(ctx, nil, &hostId2, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())
			theEvents.V2AddEvent(ctx, nil, &hostId3, &infraEnvId, models.EventSeverityInfo, models.EventCategoryUser, time.Now())

			evs, err := theEvents.V2GetEvents(ctx, &clusterId, nil, &infraEnvId, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			evs, err = theEvents.V2GetEvents(ctx, nil, nil, &infraEnvId, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(5))
			evs, err = theEvents.V2GetEvents(ctx, nil, &hostId1, &infraEnvId, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(1))
			evs, err = theEvents.V2GetEvents(ctx, &clusterId, &hostId1, nil, models.EventCategoryUser)
			Expect(err).Should(BeNil())
			Expect(len(evs)).Should(Equal(0))
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
