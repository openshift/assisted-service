package auth

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

// #nosec
const ()

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
		ctx         = context.Background()
		dbName      = "authorizer"
		db          *gorm.DB
		authHandler *AuthHandler
		allowedUser bool
		clustersAPI = "/api/assisted-install/v1/clusters/"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		authHandler = &AuthHandler{
			EnableAuth: true,
			db:         db,
			log:        logrus.New().WithField("pkg", "auth"),
		}
		accessReviewMock = func(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
			return allowedUser, nil
		}
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("Unauthorized User", func() {
		It("Empty context", func() {
			ctx = context.WithValue(ctx, restapi.AuthKey, nil)
			err := authHandler.Authorizer(getRequestWithContext(ctx, ""))

			Expect(err).Should(BeNil())
		})
		It("Empty payload", func() {
			ctx = context.WithValue(ctx, restapi.AuthKey, &ocm.AuthPayload{})
			err := authHandler.Authorizer(getRequestWithContext(ctx, ""))

			Expect(err).Should(BeNil())
		})
		It("User unallowed to access installer", func() {
			mockOCMClient(authHandler)
			allowedUser = false

			payload := &ocm.AuthPayload{}
			payload.Username = "unallowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			err := authHandler.Authorizer(getRequestWithContext(ctx, ""))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(Equal("method is not allowed"))
		})
		It("User unallowed to access cluster", func() {
			mockOCMClient(authHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "unallowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			req := getRequestWithContext(ctx, clustersAPI+uuid.New().String())
			err := authHandler.Authorizer(req)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(Equal("method is not allowed"))
		})
	})

	Context("Authorized User", func() {
		It("User allowed to access owned cluster", func() {
			mockOCMClient(authHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "allowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			clusterID := strfmt.UUID(uuid.New().String())
			req := getRequestWithContext(ctx, clustersAPI+clusterID.String())

			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				UserName: payload.Username,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			err = authHandler.Authorizer(req)
			Expect(err).ToNot(HaveOccurred())
		})
		It("User allowed to access non cluster context API", func() {
			mockOCMClient(authHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "allowed@user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			req := getRequestWithContext(ctx, "/api/assisted-install/v1/events/")

			err := authHandler.Authorizer(req)
			Expect(err).ToNot(HaveOccurred())
		})
		It("Admin allowed all endpoints", func() {
			mockOCMClient(authHandler)
			allowedUser = true

			payload := &ocm.AuthPayload{}
			payload.Username = "admin@user"
			payload.IsAdmin = true
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)

			clusterID := strfmt.UUID(uuid.New().String())
			req := getRequestWithContext(ctx, clustersAPI+clusterID.String())

			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				UserName: "nonadmin@user",
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			err = authHandler.Authorizer(req)
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

func mockOCMClient(authHandler *AuthHandler) {
	authHandler.client = &ocm.Client{
		Authorization: &mockOCMAuthorization{},
		Cache:         cache.New(1*time.Hour, 30*time.Minute),
	}
}
