package auth

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

type mockOCMAuthorization struct {
	ocm.OCMAuthorization
}

var accessReviewMock func(ctx context.Context, username, action, resourceType string) (allowed bool, err error)

var capabilityReviewMock = func(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	return true, nil
}

func (m *mockOCMAuthorization) AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
	return accessReviewMock(ctx, username, action, resourceType)
}

func (m *mockOCMAuthorization) CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	return capabilityReviewMock(ctx, username, capabilityName, capabilityType)
}

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "authorizer_test")
}

var _ = Describe("Authorizer", func() {
	var (
		ctx          = context.Background()
		authzHandler *AuthzHandler
		allowedUser  bool
		clustersAPI  = "/api/assisted-install/v1/clusters/"
	)

	BeforeEach(func() {
		authzHandler = &AuthzHandler{
			EnableAuth: true,
			log:        logrus.New().WithField("pkg", "authz"),
		}
		accessReviewMock = func(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
			return allowedUser, nil
		}
	})

	Context("Unauthorized User", func() {
		It("User unallowed to access installer", func() {
			mockOCMClient(authzHandler)
			allowedUser = false

			payload := &ocm.AuthPayload{}
			payload.Username = "unallowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			err := authzHandler.Authorizer(getRequestWithContext(ctx, ""))

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(Equal("method is not allowed"))
		})
	})

	Context("Authorized User", func() {
		It("User allowed to access API", func() {
			mockOCMClient(authzHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "allowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			req := getRequestWithContext(ctx, clustersAPI)
			err := authzHandler.Authorizer(req)

			Expect(err).ToNot(HaveOccurred())
		})
		It("Admin allowed to access API", func() {
			mockOCMClient(authzHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "admin@user"
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			req := getRequestWithContext(ctx, clustersAPI)
			err := authzHandler.Authorizer(req)

			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func getRequestWithContext(ctx context.Context, urlPath string) *http.Request {
	req := &http.Request{}
	req.URL = &url.URL{}
	req.URL.Path = urlPath
	return req.WithContext(ctx)
}

func mockOCMClient(authzHandler *AuthzHandler) {
	authzHandler.client = &ocm.Client{
		Authorization: &mockOCMAuthorization{},
		Cache:         cache.New(1*time.Hour, 30*time.Minute),
	}
}
