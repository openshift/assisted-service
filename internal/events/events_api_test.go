package events

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/events"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func newScopedPayload(resourceType gencrypto.LocalJWTKeyType, resourceID string) *ocm.AuthPayload {
	p := ocm.AdminPayload()
	p.ResourceType = resourceType
	p.ResourceID = resourceID
	return p
}

func ctxWithPayload(payload *ocm.AuthPayload) context.Context {
	return context.WithValue(context.Background(), restapi.AuthKey, payload)
}

func uuidStr() string { return uuid.New().String() }

func uuidPtr(s string) *strfmt.UUID {
	id := strfmt.UUID(s)
	return &id
}

var _ = Describe("events scope enforcement", func() {
	var (
		api    *Api
		db     *gorm.DB
		dbName string
		log    logrus.FieldLogger
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		l := logrus.New()
		l.SetOutput(io.Discard)
		log = l
		api = &Api{log: log, db: db}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Describe("V2ListEvents resource isolation", func() {
		var (
			ctx                    context.Context
			infraEnvID, otherEnvID strfmt.UUID
			hostID, otherHostID    strfmt.UUID
			clusterID              strfmt.UUID
		)

		BeforeEach(func() {
			infraEnvID, otherEnvID = strfmt.UUID(uuidStr()), strfmt.UUID(uuidStr())
			hostID, otherHostID = strfmt.UUID(uuidStr()), strfmt.UUID(uuidStr())
			clusterID = strfmt.UUID(uuidStr())
			ctx = ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID.String()))
			api.handler = New(db, auth.NewLocalAuthzHandler(&auth.Config{LocalAuthEnforceResourceScope: true}, log), nil, log)

			Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error).NotTo(HaveOccurred())
			for _, ids := range []struct{ infraEnvID, hostID strfmt.UUID }{
				{infraEnvID, hostID},
				{otherEnvID, otherHostID},
			} {
				Expect(db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &ids.infraEnvID}}).Error).NotTo(HaveOccurred())
				Expect(db.Create(&common.Host{Host: models.Host{
					ID: &ids.hostID, InfraEnvID: ids.infraEnvID, ClusterID: &clusterID,
				}}).Error).NotTo(HaveOccurred())
				Expect(db.Create(&common.Event{Event: models.Event{
					HostID: &ids.hostID, InfraEnvID: &ids.infraEnvID, ClusterID: &clusterID,
					Category: models.EventCategoryUser, Severity: swag.String(models.EventSeverityInfo),
				}}).Error).NotTo(HaveOccurred())
			}
		})

		DescribeTable("checks host ownership with and without an InfraEnv filter",
			func(filter string, foreignHost, withInfraEnv bool) {
				requestedHost := hostID
				if foreignHost {
					requestedHost = otherHostID
				}
				params := events.V2ListEventsParams{}
				if withInfraEnv {
					params.InfraEnvID = &infraEnvID
				}
				switch filter {
				case "host_id":
					params.HostID = &requestedHost
				case "uppercase_host_id":
					uppercaseHost := strfmt.UUID(strings.ToUpper(requestedHost.String()))
					params.HostID = &uppercaseHost
				case "host_ids":
					params.HostIds = []strfmt.UUID{requestedHost}
				case "uppercase_host_ids":
					params.HostIds = []strfmt.UUID{strfmt.UUID(strings.ToUpper(requestedHost.String()))}
				case "both":
					params.HostID = &requestedHost
					params.HostIds = []strfmt.UUID{hostID}
				case "mixed_case_duplicate":
					uppercaseHost := strfmt.UUID(strings.ToUpper(requestedHost.String()))
					params.HostID = &uppercaseHost
					params.HostIds = []strfmt.UUID{hostID}
				}
				response := api.V2ListEvents(ctx, params)
				if foreignHost {
					Expect(response).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
					Expect(response.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
					return
				}
				Expect(response).To(BeAssignableToTypeOf(&events.V2ListEventsOK{}))
				result := response.(*events.V2ListEventsOK)
				Expect(result.Payload).To(HaveLen(1))
				Expect(result.Payload[0].InfraEnvID).To(Equal(&infraEnvID))
				Expect(result.Payload[0].HostID).To(Equal(&hostID))
				Expect(result.EventCount).To(Equal(int64(1)))
			},
			Entry("own legacy host with InfraEnv", "host_id", false, true),
			Entry("foreign legacy host with InfraEnv", "host_id", true, true),
			Entry("own host list with InfraEnv", "host_ids", false, true),
			Entry("foreign host list with InfraEnv", "host_ids", true, true),
			Entry("mixed own and foreign hosts with InfraEnv", "both", true, true),
			Entry("duplicate own host with InfraEnv", "both", false, true),
			Entry("mixed-case duplicate own host with InfraEnv", "mixed_case_duplicate", false, true),
			Entry("mixed-case foreign host with InfraEnv", "mixed_case_duplicate", true, true),
			Entry("own legacy host without InfraEnv", "host_id", false, false),
			Entry("foreign legacy host without InfraEnv", "host_id", true, false),
			Entry("own uppercase legacy host without InfraEnv", "uppercase_host_id", false, false),
			Entry("own host list without InfraEnv", "host_ids", false, false),
			Entry("foreign host list without InfraEnv", "host_ids", true, false),
			Entry("own uppercase host list without InfraEnv", "uppercase_host_ids", false, false),
			Entry("duplicate own host without InfraEnv", "both", false, false),
			Entry("mixed-case duplicate own host without InfraEnv", "mixed_case_duplicate", false, false),
		)

		DescribeTable("rejects cluster selectors for InfraEnv tokens",
			func(withInfraEnv, withHost bool) {
				params := events.V2ListEventsParams{ClusterID: &clusterID}
				if withInfraEnv {
					params.InfraEnvID = &infraEnvID
				}
				if withHost {
					params.HostIds = []strfmt.UUID{hostID}
				}
				response := api.V2ListEvents(ctx, params)
				Expect(response).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				Expect(response.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
			},
			Entry("matching InfraEnv", true, false),
			Entry("owned host", false, true),
			Entry("matching InfraEnv and owned host", true, true),
		)

		DescribeTable("restricts host event history to the token's InfraEnv",
			func(withInfraEnv bool) {
				Expect(db.Create(&common.Event{Event: models.Event{
					HostID: &hostID, InfraEnvID: &otherEnvID,
					Category: models.EventCategoryUser, Severity: swag.String(models.EventSeverityInfo),
				}}).Error).NotTo(HaveOccurred())
				params := events.V2ListEventsParams{HostIds: []strfmt.UUID{hostID}}
				if withInfraEnv {
					params.InfraEnvID = &infraEnvID
				}
				response := api.V2ListEvents(ctx, params)
				Expect(response).To(BeAssignableToTypeOf(&events.V2ListEventsOK{}))
				result := response.(*events.V2ListEventsOK)
				Expect(result.Payload).To(HaveLen(1))
				Expect(result.Payload[0].InfraEnvID).To(Equal(&infraEnvID))
				Expect(result.EventCount).To(Equal(int64(1)))
			},
			Entry("with InfraEnv filter", true),
			Entry("without InfraEnv filter", false),
		)

		It("rejects a foreign InfraEnv even when the host belongs to the token's InfraEnv", func() {
			response := api.V2ListEvents(ctx, events.V2ListEventsParams{InfraEnvID: &otherEnvID, HostID: &hostID})
			Expect(response).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(response.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
		})

		It("preserves cluster-scoped access to hosts across InfraEnvs", func() {
			ctx = ctxWithPayload(newScopedPayload(gencrypto.ClusterKey, clusterID.String()))
			response := api.V2ListEvents(ctx, events.V2ListEventsParams{
				ClusterID: &clusterID, HostIds: []strfmt.UUID{hostID, otherHostID},
			})
			Expect(response).To(BeAssignableToTypeOf(&events.V2ListEventsOK{}))
			Expect(response.(*events.V2ListEventsOK).Payload).To(HaveLen(2))
		})
	})

	Describe("enforceEventsScopeFromPayload", func() {
		Context("unscoped token", func() {
			It("allows request with no ResourceType", func() {
				ctx := ctxWithPayload(ocm.AdminPayload())
				params := events.V2ListEventsParams{}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).To(BeNil())
			})
		})

		Context("infra_env_id token", func() {
			var infraEnvID string

			BeforeEach(func() {
				infraEnvID = uuidStr()
			})

			It("allows matching infra_env_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
				params := events.V2ListEventsParams{InfraEnvID: uuidPtr(infraEnvID)}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).To(BeNil())
			})

			It("denies mismatched infra_env_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
				params := events.V2ListEventsParams{InfraEnvID: uuidPtr(uuidStr())}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})

			It("denies infra_env_id token with only cluster_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
				params := events.V2ListEventsParams{ClusterID: uuidPtr(uuidStr())}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})

			It("denies infra_env_id token with no filter params", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
				params := events.V2ListEventsParams{}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})

			Context("host_ids filter", func() {
				var hostID strfmt.UUID

				BeforeEach(func() {
					infraEnvUUID := strfmt.UUID(infraEnvID)
					hostID = strfmt.UUID(uuidStr())

					infraEnv := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvUUID}}
					Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())

					status := "known"
					host := &common.Host{Host: models.Host{
						ID:         &hostID,
						InfraEnvID: infraEnvUUID,
						Status:     &status,
					}}
					Expect(db.Create(host).Error).ToNot(HaveOccurred())
				})

				It("allows host_ids that belong to the token's infra_env", func() {
					ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
					params := events.V2ListEventsParams{HostIds: []strfmt.UUID{hostID}}
					Expect(api.enforceEventsScopeFromPayload(ctx, params)).To(BeNil())
				})

				It("denies host_ids that belong to a different infra_env", func() {
					otherInfraEnvID := strfmt.UUID(uuidStr())
					ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, otherInfraEnvID.String()))
					params := events.V2ListEventsParams{HostIds: []strfmt.UUID{hostID}}
					Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
				})

				It("denies non-existent host_ids", func() {
					nonExistent := strfmt.UUID(uuidStr())
					ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
					params := events.V2ListEventsParams{HostIds: []strfmt.UUID{nonExistent}}
					Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
				})

				It("denies when any host_id does not belong to the infra_env", func() {
					nonExistent := strfmt.UUID(uuidStr())
					ctx := ctxWithPayload(newScopedPayload(gencrypto.InfraEnvKey, infraEnvID))
					params := events.V2ListEventsParams{HostIds: []strfmt.UUID{hostID, nonExistent}}
					Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
				})
			})
		})

		Context("cluster_id token", func() {
			var clusterID string

			BeforeEach(func() {
				clusterID = uuidStr()
			})

			It("allows matching cluster_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.ClusterKey, clusterID))
				params := events.V2ListEventsParams{ClusterID: uuidPtr(clusterID)}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).To(BeNil())
			})

			It("denies mismatched cluster_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.ClusterKey, clusterID))
				params := events.V2ListEventsParams{ClusterID: uuidPtr(uuidStr())}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})

			It("denies cluster_id token with only infra_env_id query param", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.ClusterKey, clusterID))
				params := events.V2ListEventsParams{InfraEnvID: uuidPtr(uuidStr())}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})

			It("denies cluster_id token with no filter params", func() {
				ctx := ctxWithPayload(newScopedPayload(gencrypto.ClusterKey, clusterID))
				params := events.V2ListEventsParams{}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})
		})

		Context("unknown resource type", func() {
			It("denies tokens with unrecognized resource type", func() {
				ctx := ctxWithPayload(newScopedPayload("unknown_key", uuidStr()))
				params := events.V2ListEventsParams{}
				Expect(api.enforceEventsScopeFromPayload(ctx, params)).ToNot(BeNil())
			})
		})
	})
})
