package events

import (
	"context"
	"io"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
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
