package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	ctxparams "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func setPayloadInContext(ctx context.Context, payload *ocm.AuthPayload) context.Context {
	return context.WithValue(ctx, restapi.AuthKey, payload)
}

func buildRequest(ctx context.Context, urlStr string) *http.Request {
	u, _ := url.Parse(urlStr)
	req := &http.Request{URL: u}
	return req.WithContext(ctx)
}

var _ = Describe("LocalAuthzHandler", func() {
	var (
		handler *LocalAuthzHandler
		db      *gorm.DB
		dbName  string
		log     logrus.FieldLogger
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		l := logrus.New()
		l.SetOutput(io.Discard)
		log = l
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Describe("IsAdmin", func() {
		It("returns true for unscoped payloads", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log, db)

			payload := ocm.AdminPayload()
			ctx := setPayloadInContext(context.Background(), payload)
			Expect(handler.IsAdmin(ctx)).To(BeTrue())
		})

		It("returns false for scoped payloads", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log, db)

			payload := ocm.AdminPayload()
			payload.ResourceType = string(gencrypto.InfraEnvKey)
			payload.ResourceID = uuid.New().String()
			ctx := setPayloadInContext(context.Background(), payload)
			Expect(handler.IsAdmin(ctx)).To(BeFalse())
		})
	})

	Describe("NewAuthzHandler returns LocalAuthzHandler for TypeLocal", func() {
		It("creates a LocalAuthzHandler", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			authzr := NewAuthzHandler(cfg, nil, log, db)
			_, ok := authzr.(*LocalAuthzHandler)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("verifyResourceScope", func() {
		Context("with enforcement enabled", func() {
			BeforeEach(func() {
				cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
				handler = NewLocalAuthzHandler(cfg, log, db)
			})

			It("allows matching infra_env_id path param", func() {
				infraEnvID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.InfraEnvId, infraEnvID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/infra-envs/%s/downloads/files", infraEnvID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).ToNot(HaveOccurred())
			})

			It("denies mismatched infra_env_id path param", func() {
				infraEnvID := uuid.New().String()
				otherID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.InfraEnvId, otherID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/infra-envs/%s/downloads/files", otherID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("denies infra_env_id token on cluster path param", func() {
				infraEnvID := uuid.New().String()
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.ClusterId, clusterID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/clusters/%s/logs", clusterID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("allows matching cluster_id path param", func() {
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.ClusterId, clusterID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/clusters/%s/logs", clusterID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).ToNot(HaveOccurred())
			})

			It("denies mismatched cluster_id path param", func() {
				clusterID := uuid.New().String()
				otherClusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.ClusterId, otherClusterID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/clusters/%s/logs", otherClusterID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("denies cluster_id token on infra_env path param", func() {
				clusterID := uuid.New().String()
				infraEnvID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.InfraEnvId, infraEnvID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/infra-envs/%s/downloads/files", infraEnvID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("verifyEventsScopeFromQuery", func() {
		Context("with enforcement enabled", func() {
			BeforeEach(func() {
				cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
				handler = NewLocalAuthzHandler(cfg, log, db)
			})

			It("allows infra_env_id token with matching infra_env_id query", func() {
				infraEnvID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?infra_env_id=%s", infraEnvID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).ToNot(HaveOccurred())
			})

			It("denies infra_env_id token with mismatched infra_env_id query", func() {
				infraEnvID := uuid.New().String()
				otherID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?infra_env_id=%s", otherID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("denies infra_env_id token querying by cluster_id", func() {
				infraEnvID := uuid.New().String()
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?cluster_id=%s", clusterID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("allows cluster_id token with matching cluster_id query", func() {
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?cluster_id=%s", clusterID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).ToNot(HaveOccurred())
			})

			It("denies cluster_id token with mismatched cluster_id query", func() {
				clusterID := uuid.New().String()
				otherID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?cluster_id=%s", otherID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("denies cluster_id token querying by host_id", func() {
				clusterID := uuid.New().String()
				hostID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.ClusterKey)
				payload.ResourceID = clusterID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?host_id=%s", hostID))
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("denies scoped token with no filter parameters", func() {
				infraEnvID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = string(gencrypto.InfraEnvKey)
				payload.ResourceID = infraEnvID

				req := buildRequest(context.Background(), "http://localhost/v2/events")
				err := handler.verifyEventsScopeFromQuery(req, payload)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("host-to-infraenv verification", func() {
		BeforeEach(func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log, db)
		})

		It("allows when host belongs to infra_env", func() {
			infraEnvID := strfmt.UUID(uuid.New().String())
			hostID := strfmt.UUID(uuid.New().String())

			infraEnv := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}
			Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())

			host := &common.Host{Host: models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
				Status:     swagStrPtr("known"),
			}}
			Expect(db.Create(host).Error).ToNot(HaveOccurred())

			err := handler.verifyHostBelongsToInfraEnv(infraEnvID.String(), hostID.String())
			Expect(err).ToNot(HaveOccurred())
		})

		It("denies when host belongs to different infra_env", func() {
			infraEnvID1 := strfmt.UUID(uuid.New().String())
			infraEnvID2 := strfmt.UUID(uuid.New().String())
			hostID := strfmt.UUID(uuid.New().String())

			infraEnv1 := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID1}}
			Expect(db.Create(infraEnv1).Error).ToNot(HaveOccurred())
			infraEnv2 := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID2}}
			Expect(db.Create(infraEnv2).Error).ToNot(HaveOccurred())

			host := &common.Host{Host: models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID2,
				Status:     swagStrPtr("known"),
			}}
			Expect(db.Create(host).Error).ToNot(HaveOccurred())

			err := handler.verifyHostBelongsToInfraEnv(infraEnvID1.String(), hostID.String())
			Expect(err).To(HaveOccurred())
		})

		It("denies when host does not exist", func() {
			infraEnvID := strfmt.UUID(uuid.New().String())
			hostID := strfmt.UUID(uuid.New().String())

			infraEnv := &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}
			Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())

			err := handler.verifyHostBelongsToInfraEnv(infraEnvID.String(), hostID.String())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("LocalAuthenticator propagates resource claims", func() {
		var (
			a        *LocalAuthenticator
			infraEnv *common.InfraEnv
		)

		BeforeEach(func() {
			infraEnvID := strfmt.UUID(uuid.New().String())
			infraEnv = &common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}
			Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())
		})

		It("sets ResourceType and ResourceID for infra_env_id token", func() {
			pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).ToNot(HaveOccurred())

			cfg := &Config{ECPublicKeyPEM: pubKey}
			token, err := gencrypto.LocalJWTForKey(infraEnv.ID.String(), privKey, gencrypto.InfraEnvKey)
			Expect(err).ToNot(HaveOccurred())

			a, err = NewLocalAuthenticator(cfg, logrus.New(), db)
			Expect(err).ToNot(HaveOccurred())

			result, err := a.AuthAgentAuth(token)
			Expect(err).ToNot(HaveOccurred())

			payload, ok := result.(*ocm.AuthPayload)
			Expect(ok).To(BeTrue())
			Expect(payload.ResourceType).To(Equal(string(gencrypto.InfraEnvKey)))
			Expect(payload.ResourceID).To(Equal(infraEnv.ID.String()))
		})

		It("sets ResourceType and ResourceID for cluster_id token", func() {
			clusterID := strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{ID: &clusterID}}
			Expect(db.Create(cluster).Error).ShouldNot(HaveOccurred())

			pubKey, privKey, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).ToNot(HaveOccurred())

			cfg := &Config{ECPublicKeyPEM: pubKey}
			token, err := gencrypto.LocalJWTForKey(clusterID.String(), privKey, gencrypto.ClusterKey)
			Expect(err).ToNot(HaveOccurred())

			a, err = NewLocalAuthenticator(cfg, logrus.New(), db)
			Expect(err).ToNot(HaveOccurred())

			result, err := a.AuthAgentAuth(token)
			Expect(err).ToNot(HaveOccurred())

			payload, ok := result.(*ocm.AuthPayload)
			Expect(ok).To(BeTrue())
			Expect(payload.ResourceType).To(Equal(string(gencrypto.ClusterKey)))
			Expect(payload.ResourceID).To(Equal(clusterID.String()))
		})
	})
})

func swagStrPtr(s string) *string {
	return &s
}
