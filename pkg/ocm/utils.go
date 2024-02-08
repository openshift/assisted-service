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
	MultiarchCapabilityName         string = "bare_metal_installer_multiarch"
	PlatformOciCapabilityName       string = "bare_metal_installer_platform_oci"
	PlatformExternalCapabilityName  string = "bare_metal_installer_platform_external"
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

// PayloadFromContext returns auth payload from the specified context
func PayloadFromContext(ctx context.Context) *AuthPayload {
	payload := ctx.Value(restapi.AuthKey)
	if payload == nil {
		// fallback to system-admin
		return AdminPayload()
	}
	return payload.(*AuthPayload)
}

// UserNameFromContext returns username from the specified context
func UserNameFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	return payload.Username
}

// OrgIDFromContext returns org ID from the specified context
func OrgIDFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
	return payload.Organization
}

// EmailFromContext returns email from the specified context
func EmailFromContext(ctx context.Context) string {
	payload := PayloadFromContext(ctx)
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
