package identity

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
)

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "identity_test")
}

var _ = Describe("Identity", func() {
	var (
		ctx = context.Background()
	)

	BeforeEach(func() {
	})

	Context("IsAdmin", func() {
		It("admin user", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			isAdmin := IsAdmin(ctx)

			Expect(isAdmin).Should(Equal(true))
		})
		It("non-admin user", func() {
			payload := &ocm.AuthPayload{}
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			isAdmin := IsAdmin(ctx)

			Expect(isAdmin).Should(Equal(false))
		})
	})

	Context("AddUserFilter", func() {
		It("admin user - empty query", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddUserFilter(ctx, "")

			Expect(query).Should(Equal(""))
		})
		It("admin user - non-empty query", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddUserFilter(ctx, "id = ?")

			Expect(query).Should(Equal("id = ?"))
		})
		It("non-admin user - empty query", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddUserFilter(ctx, "")

			Expect(query).Should(Equal("user_name = 'test_user'"))
		})
		It("non-admin user - non-empty query", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddUserFilter(ctx, "id = ?")

			Expect(query).Should(Equal("id = ? and user_name = 'test_user'"))
		})
	})

	Context("AddOwnerFilter", func() {
		It("admin user - empty query - org tenancy disabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "", false, "")
			Expect(query).Should(Equal(""))
		})
		It("admin user - non-empty query - org tenancy disabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "id = ?", false, "")
			Expect(query).Should(Equal("id = ?"))
		})
		It("non-admin user - empty query - org tenancy disabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "", false, "")
			Expect(query).Should(Equal("user_name = 'test_user'"))
		})
		It("non-admin user - non-empty query - org tenancy disabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "id = ?", false, "")
			Expect(query).Should(Equal("id = ? and user_name = 'test_user'"))
		})
		It("admin user - empty query - org tenancy enabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "", true, "")
			Expect(query).Should(Equal(""))
		})
		It("admin user - non-empty query - org tenancy enabled", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.AdminRole
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "id = ?", true, "")
			Expect(query).Should(Equal("id = ?"))
		})
		It("non-admin user - empty query - filter by org", func() {
			payload := &ocm.AuthPayload{}
			payload.Organization = "test_org"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "", true, "")
			Expect(query).Should(Equal("org_id = 'test_org'"))
		})
		It("non-admin user - non-empty query - filter by org", func() {
			payload := &ocm.AuthPayload{}
			payload.Organization = "test_org"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "id = ?", true, "")
			Expect(query).Should(Equal("id = ? and org_id = 'test_org'"))
		})
		It("non-admin user - empty query - filter by org and by user", func() {
			payload := &ocm.AuthPayload{}
			payload.Organization = "test_org"
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "", true, payload.Username)
			Expect(query).Should(Equal("org_id = 'test_org' and user_name = 'test_user'"))
		})
		It("non-admin user - non-empty query - filter by org and by user", func() {
			payload := &ocm.AuthPayload{}
			payload.Organization = "test_org"
			payload.Username = "test_user"
			ctx = context.WithValue(ctx, restapi.AuthKey, payload)
			query := AddOwnerFilter(ctx, "id = ?", true, payload.Username)
			Expect(query).Should(Equal("id = ? and org_id = 'test_org' and user_name = 'test_user'"))
		})
	})
})
