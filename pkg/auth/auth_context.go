package auth

import (
	"context"

	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/restapi"
)

// LocalAuthPayload wraps AuthPayload and includes the raw JWT token
// for use with LocalAuthenticator-based authentication.
type LocalAuthPayload struct {
	*ocm.AuthPayload
	Token string
}

// GetAuthPayload implements ocm.AuthPayloadProvider interface
func (p *LocalAuthPayload) GetAuthPayload() *ocm.AuthPayload {
	return p.AuthPayload
}

// GetAuthTokenFromContext retrieves the raw JWT token from the context.
// For LocalAuthenticator, the principal stored in context is LocalAuthPayload
// which includes the token. For other authenticators, this returns empty string.
func GetAuthTokenFromContext(ctx context.Context) (string, bool) {
	principal := ctx.Value(restapi.AuthKey)
	if principal == nil {
		return "", false
	}

	// Check if principal is LocalAuthPayload (from LocalAuthenticator)
	if localPayload, ok := principal.(*LocalAuthPayload); ok {
		return localPayload.Token, true
	}

	return "", false
}
