package auth

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
)

var _ = Describe("GetAuthTokenFromContext", func() {
	It("returns token when context contains LocalAuthPayload", func() {
		expectedToken := "test-jwt-token-12345" // #nosec G101 - test data only
		payload := &LocalAuthPayload{
			AuthPayload: ocm.AdminPayload(),
			Token:       expectedToken,
		}

		ctx := context.WithValue(context.Background(), restapi.AuthKey, payload)

		token, ok := GetAuthTokenFromContext(ctx)
		Expect(ok).To(BeTrue())
		Expect(token).To(Equal(expectedToken))
	})

	It("returns empty when context contains regular AuthPayload", func() {
		payload := ocm.AdminPayload()
		ctx := context.WithValue(context.Background(), restapi.AuthKey, payload)

		token, ok := GetAuthTokenFromContext(ctx)
		Expect(ok).To(BeFalse())
		Expect(token).To(BeEmpty())
	})

	It("returns empty when context has no auth payload", func() {
		ctx := context.Background()

		token, ok := GetAuthTokenFromContext(ctx)
		Expect(ok).To(BeFalse())
		Expect(token).To(BeEmpty())
	})

	It("returns empty when context has nil auth value", func() {
		ctx := context.WithValue(context.Background(), restapi.AuthKey, nil)

		token, ok := GetAuthTokenFromContext(ctx)
		Expect(ok).To(BeFalse())
		Expect(token).To(BeEmpty())
	})
})

var _ = Describe("LocalAuthPayload", func() {
	It("implements GetAuthPayload correctly", func() {
		basePayload := ocm.AdminPayload()
		localPayload := &LocalAuthPayload{
			AuthPayload: basePayload,
			Token:       "test-token",
		}

		result := localPayload.GetAuthPayload()
		Expect(result).To(Equal(basePayload))
	})
})
