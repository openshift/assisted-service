package identity

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/ocm"
	restapi "github.com/openshift/assisted-service/restapi/restapi_v1"
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
})
