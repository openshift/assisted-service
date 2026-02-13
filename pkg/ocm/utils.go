package ocm

import (
	"context"
	"net/http"
	"strings"

	sdkClient "github.com/openshift-online/ocm-sdk-go"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/restapi"
)

const (
	BareMetalClusterResource        string = "BareMetalCluster"
	AMSActionCreate                 string = "create"
	AMSActionUpdate                 string = "update"
	AMSActionDelete                 string = "delete"
	BareMetalCapabilityName         string = "bare_metal_installer_admin"
	SoftTimeoutsCapabilityName      string = "bare_metal_installer_soft_timeouts"
	AccountCapabilityType           string = "Account"
	OrganizationCapabilityType      string = "Organization"
	Subscription                    string = "Subscription"
	EmailDelimiter                  string = "@"
	IgnoreValidationsCapabilityName string = "ignore_validations"

	// AdminUsername for disabled auth
	AdminUsername string = "admin"

	// UnknownEmailDomain for disabled auth or invalid emails
	UnknownEmailDomain string = "Unknown"
)

type response interface {
	Status() int
}

func AdminPayload() *AuthPayload {
	return &AuthPayload{Role: AdminRole, Username: AdminUsername}
}

// AuthPayloadProvider is an interface for types that can provide an AuthPayload.
// This allows LocalAuthPayload (which embeds *AuthPayload) to be used interchangeably
// with *AuthPayload in context-based payload extraction.
type AuthPayloadProvider interface {
	GetAuthPayload() *AuthPayload
}

// GetAuthPayload implements AuthPayloadProvider for *AuthPayload
func (p *AuthPayload) GetAuthPayload() *AuthPayload {
	return p
}

// PayloadFromContext returns auth payload from the specified context.
//
// The nil return for unexpected principal types is intentional security behavior.
// Unknown auth payload types should not be granted any permissions - this prevents
// privilege escalation if a new auth mechanism is added without proper authorization handling.
//
// Returns nil if the context contains an unexpected principal type (e.g., jwt.MapClaims
// from image auth). Callers should handle nil appropriately.
func PayloadFromContext(ctx context.Context) *AuthPayload {
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		// fallback to system-admin for unauthenticated contexts
		return AdminPayload()
	}

	// Try direct *AuthPayload first
	if authPayload, ok := payload.(*AuthPayload); ok {
		return authPayload
	}

	// Try AuthPayloadProvider interface (handles LocalAuthPayload and similar wrappers)
	if provider, ok := payload.(AuthPayloadProvider); ok {
		return provider.GetAuthPayload()
	}

	// For any other type (e.g., jwt.MapClaims from image auth), return nil
	// to prevent privilege escalation. Callers must handle nil appropriately.
	return nil
}

// UserNameFromContext returns username from the specified context.
// Returns empty string if payload is nil (e.g., for image auth contexts).
func UserNameFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	if payload == nil {
		return ""
	}
	return payload.Username
}

// OrgIDFromContext returns org ID from the specified context.
// Returns empty string if payload is nil (e.g., for image auth contexts).
func OrgIDFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	if payload == nil {
		return ""
	}
	return payload.Organization
}

// EmailFromContext returns email from the specified context.
// Returns empty string if payload is nil (e.g., for image auth contexts).
func EmailFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	if payload == nil {
		return ""
	}
	return payload.Email
}

// EmailDomainFromContext returns email Domain from the specified context
func EmailDomainFromContext(ctx context.Context) string {
	domain := UnknownEmailDomain
	email := EmailFromContext(ctx)
	delimiterIdx := strings.LastIndex(email, EmailDelimiter)
	if delimiterIdx >= 0 {
		emailElements := strings.Split(email, EmailDelimiter)
		domain = emailElements[len(emailElements)-1]
	}
	return domain
}

func HandleOCMResponse(ctx context.Context, log sdkClient.Logger, response response, requestType string, err error) error {
	if err != nil {
		log.Error(ctx, "Failed to send %s request. Error: %v", requestType, err)
		if response != nil {
			log.Error(ctx, "Failed to send %s request. Response: %v", requestType, response)
			if response.Status() >= 400 && response.Status() < 500 {
				return common.NewInfraError(http.StatusUnauthorized, err)
			}
		}
		return common.NewApiError(http.StatusServiceUnavailable, err)
	}
	return nil
}
