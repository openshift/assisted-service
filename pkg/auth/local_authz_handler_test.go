package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/gencrypto"
	ctxparams "github.com/openshift/assisted-service/pkg/context"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/sirupsen/logrus"
)

func setPayloadInContext(ctx context.Context, payload *ocm.AuthPayload) context.Context {
	return context.WithValue(ctx, restapi.AuthKey, payload)
}

func buildRequest(ctx context.Context, urlStr string) *http.Request {
	u, err := url.Parse(urlStr)
	Expect(err).ToNot(HaveOccurred())
	req := &http.Request{URL: u}
	return req.WithContext(ctx)
}

var _ = Describe("LocalAuthzHandler", func() {
	var (
		handler *LocalAuthzHandler
		log     logrus.FieldLogger
	)

	BeforeEach(func() {
		l := logrus.New()
		l.SetOutput(io.Discard)
		log = l
	})

	Describe("IsAdmin", func() {
		It("returns true for unscoped payloads when enforcement enabled", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log)

			payload := ocm.AdminPayload()
			ctx := setPayloadInContext(context.Background(), payload)
			Expect(handler.IsAdmin(ctx)).To(BeTrue())
		})

		It("returns false for scoped payloads when enforcement enabled", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log)

			payload := ocm.AdminPayload()
			payload.ResourceType = gencrypto.InfraEnvKey
			payload.ResourceID = uuid.New().String()
			ctx := setPayloadInContext(context.Background(), payload)
			Expect(handler.IsAdmin(ctx)).To(BeFalse())
		})

		It("returns true for scoped payloads when enforcement disabled", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: false}
			handler = NewLocalAuthzHandler(cfg, log)

			payload := ocm.AdminPayload()
			payload.ResourceType = gencrypto.InfraEnvKey
			payload.ResourceID = uuid.New().String()
			ctx := setPayloadInContext(context.Background(), payload)
			Expect(handler.IsAdmin(ctx)).To(BeTrue())
		})
	})

	Describe("NewAuthzHandler returns LocalAuthzHandler for TypeLocal", func() {
		It("creates a LocalAuthzHandler", func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			authzr := NewAuthzHandler(cfg, nil, log, nil)
			_, ok := authzr.(*LocalAuthzHandler)
			Expect(ok).To(BeTrue())
		})
	})

	Describe("authorizerMiddleware scheme gating", func() {
		var scopedPayload *ocm.AuthPayload

		BeforeEach(func() {
			cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
			handler = NewLocalAuthzHandler(cfg, log)

			scopedPayload = ocm.AdminPayload()
			scopedPayload.ResourceType = gencrypto.InfraEnvKey
			scopedPayload.ResourceID = uuid.New().String()
		})

		authorizer := func(scheme string) func(*http.Request) error {
			handler.schemeFor = func(*http.Request) string { return scheme }
			return handler.CreateAuthorizer()
		}

		for _, scheme := range []string{"agentAuth", "userAuth", "imageAuth", "imageURLAuth"} {
			It("passes through without scope check for "+scheme, func() {
				ctx := setPayloadInContext(context.Background(), scopedPayload)
				req := buildRequest(ctx, "http://localhost/v2/clusters")
				Expect(authorizer(scheme)(req)).To(Succeed())
			})
		}

		It("passes urlAuth through when no path params are present", func() {
			ctx := setPayloadInContext(context.Background(), scopedPayload)
			req := buildRequest(ctx, "http://localhost/v2/events")
			Expect(authorizer("urlAuth")(req)).To(Succeed())
		})

		It("passes urlAuth through when enforcement is disabled", func() {
			handler.enforceScope = false
			ctx := setPayloadInContext(context.Background(), scopedPayload)
			req := buildRequest(ctx, "http://localhost/v2/events")
			Expect(authorizer("urlAuth")(req)).To(Succeed())
		})
	})

	Describe("verifyResourceScope", func() {
		Context("with enforcement enabled", func() {
			BeforeEach(func() {
				cfg := &Config{AuthType: TypeLocal, LocalAuthEnforceResourceScope: true}
				handler = NewLocalAuthzHandler(cfg, log)
			})

			It("allows matching infra_env_id path param", func() {
				infraEnvID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = gencrypto.InfraEnvKey
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
				payload.ResourceType = gencrypto.InfraEnvKey
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
				payload.ResourceType = gencrypto.InfraEnvKey
				payload.ResourceID = infraEnvID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.ClusterId, clusterID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/clusters/%s/logs", clusterID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("allows matching cluster_id path param", func() {
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = gencrypto.ClusterKey
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
				payload.ResourceType = gencrypto.ClusterKey
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
				payload.ResourceType = gencrypto.ClusterKey
				payload.ResourceID = clusterID

				ctx := ctxparams.SetParam(context.Background(), ctxparams.InfraEnvId, infraEnvID)
				req := buildRequest(ctx, fmt.Sprintf("http://localhost/v2/infra-envs/%s/downloads/files", infraEnvID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})

			It("passes through when no path params present to allow handler-level scope enforcement", func() {
				clusterID := uuid.New().String()
				payload := ocm.AdminPayload()
				payload.ResourceType = gencrypto.ClusterKey
				payload.ResourceID = clusterID

				req := buildRequest(context.Background(), fmt.Sprintf("http://localhost/v2/events?cluster_id=%s", clusterID))
				err := handler.verifyResourceScope(req, payload)
				Expect(err).ToNot(HaveOccurred())
			})

			It("errors on unrecognized resource type", func() {
				payload := ocm.AdminPayload()
				payload.ResourceType = "unknown_key"
				payload.ResourceID = uuid.New().String()

				ctx := ctxparams.SetParam(context.Background(), ctxparams.InfraEnvId, uuid.New().String())
				req := buildRequest(ctx, "http://localhost/v2/infra-envs")
				err := handler.verifyResourceScope(req, payload)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
