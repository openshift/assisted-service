package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/security"
	"github.com/go-openapi/strfmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

// hashToken creates a SHA256 hash of a token for use as an identifier.
//
// Identities are stored as SHA-256 hashes rather than plaintext to:
//  1. Avoid storing PII (tokens, which may contain user info) in the security tracking table
//  2. Prevent information disclosure if the database table is compromised
//  3. Maintain consistent key length (32 hex chars) for indexing efficiency
//
// The hash is sufficient for lockout tracking - we don't need to reverse it.
// Uses 16 bytes (128 bits) of the hash for strong collision resistance.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes for 128-bit identifiers
}

// clientIPContextKey is the context key for storing client IP address.
type clientIPContextKey struct{}

// GetClientIPFromContext extracts the client IP address from context.
// Returns empty string if not available.
// The client IP should be set by middleware using SetClientIPInContext.
func GetClientIPFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(clientIPContextKey{}).(string); ok {
		return ip
	}
	return ""
}

// SetClientIPInContext returns a new context with the client IP address stored.
// This should be called by HTTP middleware to make the IP available to auth handlers.
func SetClientIPInContext(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPContextKey{}, ip)
}

// extractIPFromRemoteAddr extracts the IP address from RemoteAddr (strips port if present).
func extractIPFromRemoteAddr(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx > 0 {
		// Check if it's IPv6 (contains multiple colons)
		if strings.Count(remoteAddr, ":") > 1 {
			// IPv6: [::1]:port or just ::1
			if strings.HasPrefix(remoteAddr, "[") {
				if endBracket := strings.Index(remoteAddr, "]"); endBracket > 0 {
					return remoteAddr[1:endBracket]
				}
			}
			return remoteAddr
		}
		return remoteAddr[:idx]
	}
	return remoteAddr
}

// ExtractClientIPFromRequest extracts the client IP from an HTTP request.
// It only trusts X-Forwarded-For and X-Real-IP headers when the request comes
// from a trusted proxy. If trustedProxyChecker is nil or the immediate peer
// is not in the trusted proxy list, it falls back to RemoteAddr.
func ExtractClientIPFromRequest(r *http.Request, trustedProxyChecker *TrustedProxyChecker) string {
	remoteIP := extractIPFromRemoteAddr(r.RemoteAddr)

	// Only trust forwarded headers if the request comes from a trusted proxy
	if trustedProxyChecker != nil && trustedProxyChecker.IsTrusted(remoteIP) {
		// Check X-Forwarded-For header (may contain multiple IPs, first is client)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For can be comma-separated list
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}

		// Check X-Real-IP header
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Fall back to RemoteAddr
	return remoteIP
}

type RHSSOAuthenticator struct {
	KeyMap                       map[string]*rsa.PublicKey
	AdminUsers                   []string
	OrgTenancyEnabled            bool
	OrgBasedFunctionalityEnabled bool
	utils                        AUtilsInteface
	log                          logrus.FieldLogger
	client                       *ocm.Client
	db                           *gorm.DB
	failedAttempts               *FailedAttemptTracker
	auditLog                     *SecurityAuditLogger
	trustedProxyChecker          *TrustedProxyChecker
}

// isLockoutEnabled returns true if lockout tracking is available and should be used.
// This provides nil-safety for tests that create RHSSOAuthenticator without full initialization.
func (a *RHSSOAuthenticator) isLockoutEnabled() bool {
	return a.failedAttempts != nil && a.auditLog != nil
}

func NewRHSSOAuthenticator(cfg *Config, ocmCLient *ocm.Client, log logrus.FieldLogger, db *gorm.DB) *RHSSOAuthenticator {
	lockoutPolicy := cfg.GetLockoutPolicy()
	trustedProxyChecker := NewTrustedProxyChecker(cfg.TrustedProxyCIDRs)
	a := &RHSSOAuthenticator{
		AdminUsers:                   cfg.AdminUsers,
		OrgTenancyEnabled:            cfg.EnableOrgTenancy,
		OrgBasedFunctionalityEnabled: cfg.EnableOrgBasedFeatureGates,
		utils:                        NewAuthUtils(cfg.JwkCert, cfg.JwkCertURL),
		client:                       ocmCLient,
		log:                          log,
		db:                           db,
		failedAttempts:               NewFailedAttemptTracker(db, lockoutPolicy, log),
		auditLog:                     NewSecurityAuditLogger(log),
		trustedProxyChecker:          trustedProxyChecker,
	}
	err := a.populateKeyMap()
	if err != nil {
		log.Fatalln("Failed to init auth handler,", err)
	}
	if lockoutPolicy.Enabled {
		log.Infof("Account lockout enabled: max %d attempts, %v lockout duration, %v window",
			lockoutPolicy.MaxAttempts, lockoutPolicy.LockoutDuration, lockoutPolicy.WindowDuration)
	}
	if trustedProxyChecker.HasTrustedProxies() {
		log.Infof("Trusted proxy validation enabled for X-Forwarded-For/X-Real-IP headers")
	} else {
		log.Infof("No trusted proxies configured; X-Forwarded-For/X-Real-IP headers will be ignored")
	}
	return a
}

var _ Authenticator = &RHSSOAuthenticator{}

func (a *RHSSOAuthenticator) AuthType() AuthType {
	return TypeRHSSO
}

func (a *RHSSOAuthenticator) EnableOrgTenancy() bool {
	return a.OrgTenancyEnabled
}

func (a *RHSSOAuthenticator) EnableOrgBasedFeatureGates() bool {
	return a.OrgBasedFunctionalityEnabled
}

func (a *RHSSOAuthenticator) populateKeyMap() error {
	// Load the trusted CA certificates:
	trustedCAs, err := x509.SystemCertPool()
	if err != nil {
		return errors.Errorf("can't load system trusted CAs: %v", err)
	}

	// Try to read the JWT public key object file.
	a.KeyMap, err = a.utils.proccessPublicKeys(trustedCAs)
	return err
}

func (a *RHSSOAuthenticator) getValidationToken(token *jwt.Token) (interface{}, error) {
	// Try to get the token kid.
	kid, ok := token.Header["kid"]
	if !ok {
		return nil, errors.Errorf("no kid found in jwt token")
	}

	// Try to get correct cert from certs map.
	key, ok := a.KeyMap[kid.(string)]
	if !ok {
		return nil, errors.Errorf("No matching key in auth keymap for key id [%v]", kid)
	}

	return key, nil
}

// AuthAgentAuth implements the Authenticator interface for backward compatibility.
// It delegates to authAgentAuthWithContext with an empty client IP.
func (a *RHSSOAuthenticator) AuthAgentAuth(token string) (interface{}, error) {
	return a.authAgentAuthWithContext(context.Background(), token, "")
}

// authAgentAuthWithContext performs agent authentication with client IP for audit logging.
func (a *RHSSOAuthenticator) authAgentAuthWithContext(ctx context.Context, token, clientIP string) (interface{}, error) {
	if a.client == nil {
		a.log.Error("OCM client unavailable")
		return nil, errors.Errorf("OCM client unavailable")
	}

	// Use a hash of the token as identifier to avoid storing sensitive data
	tokenIdentifier := hashToken(token)

	// Check lockout status if enabled
	if a.isLockoutEnabled() {
		// Check if the client IP is locked out
		if clientIP != "" {
			if locked, until := a.failedAttempts.IsLocked(clientIP, IdentifierTypeIP); locked {
				a.auditLog.LogLockedIPAttempt(clientIP, until)
				return nil, common.ApiErrorWithDefaultInfraError(
					errors.Errorf("IP locked until %s due to too many failed attempts", until.Format(time.RFC3339)),
					http.StatusTooManyRequests)
			}
		}

		// Check if this token is locked out
		if locked, until := a.failedAttempts.IsLocked(tokenIdentifier, IdentifierTypeUsername); locked {
			a.auditLog.LogLockedLoginAttempt(tokenIdentifier, clientIP, until)
			return nil, common.ApiErrorWithDefaultInfraError(
				errors.Errorf("authentication locked until %s due to too many failed attempts", until.Format(time.RFC3339)),
				http.StatusTooManyRequests)
		}
	}

	authUser, found := a.client.Cache.Get(token)
	if found {
		// Successful authentication from cache should reset counters and log success
		if a.isLockoutEnabled() {
			a.failedAttempts.Reset(tokenIdentifier, IdentifierTypeUsername)
			if clientIP != "" {
				a.failedAttempts.Reset(clientIP, IdentifierTypeIP)
			}
			a.auditLog.LogSuccessfulLogin(tokenIdentifier, clientIP)
		}
		return authUser, nil
	}
	user, err := a.client.Authentication.AuthenticatePullSecret(ctx, token)
	if err != nil {
		a.log.Errorf("Error Authenticating PullSecret token: %v", err)
		// Record failed attempt for token identifier
		if a.isLockoutEnabled() {
			count, lockDuration := a.failedAttempts.RecordFailure(tokenIdentifier, IdentifierTypeUsername)
			a.auditLog.LogFailedLogin(tokenIdentifier, clientIP, count, "invalid pull secret")
			if lockDuration > 0 {
				a.auditLog.LogAccountLocked(tokenIdentifier, clientIP, count, time.Now().Add(lockDuration))
			}
			// Also record failed attempt for IP
			if clientIP != "" {
				ipCount, ipLock := a.failedAttempts.RecordFailure(clientIP, IdentifierTypeIP)
				if ipLock > 0 {
					a.auditLog.LogIPLocked(clientIP, ipCount, time.Now().Add(ipLock))
				}
			}
		}
		return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
	}
	err = a.storeRoleInPayload(user)

	if shouldStorePayloadInCache(err) {
		a.client.Cache.Set(token, user, cache.DefaultExpiration)
	}

	if err != nil {
		a.log.Errorf("Unable to fetch user's capabilities: %v", err)
		return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
	}

	// Reset failed attempts on successful auth
	if a.isLockoutEnabled() {
		a.failedAttempts.Reset(tokenIdentifier, IdentifierTypeUsername)
		if clientIP != "" {
			a.failedAttempts.Reset(clientIP, IdentifierTypeIP)
		}
		a.auditLog.LogSuccessfulLogin(tokenIdentifier, clientIP)
	}

	return user, nil
}

func parseOCMPayload(userToken *jwt.Token) (*ocm.AuthPayload, error) {
	claims, ok := userToken.Claims.(jwt.MapClaims)
	if !ok {
		err := errors.Errorf("Unable to parse JWT token claims")
		return nil, err
	}

	payload := &ocm.AuthPayload{}
	// default to the values we expect from RHSSO
	payload.Username, _ = claims["username"].(string)
	payload.FirstName, _ = claims["first_name"].(string)
	payload.LastName, _ = claims["last_name"].(string)
	payload.Organization, _ = claims["org_id"].(string)
	payload.Email, _ = claims["email"].(string)

	// The `clientId` claim was replaced by `client_id` in order to be compliant with the OAuth2
	// specification. We will still try to use the old `clientId` claim to support older
	// environments where the change hasn't been made yet.
	payload.ClientID, _ = claims["client_id"].(string)
	if payload.ClientID == "" {
		payload.ClientID, _ = claims["clientId"].(string)
	}

	// Check values, if empty, use alternative claims from RHD
	if payload.Username == "" {
		payload.Username, _ = claims["preferred_username"].(string)
	}

	if payload.FirstName == "" {
		payload.FirstName, _ = claims["given_name"].(string)
	}

	if payload.LastName == "" {
		payload.LastName, _ = claims["family_name"].(string)
	}

	// If given and family names are not present, use the name field
	if payload.FirstName == "" || payload.LastName == "" {
		name, _ := claims["name"].(string)
		names := strings.Split(name, " ")
		if len(names) > 1 {
			payload.FirstName = names[0]
			payload.LastName = names[1]
		} else {
			payload.FirstName = names[0]
		}
	}
	return payload, nil
}

func isValidationErrorIssuedAt(err error) bool {
	e, ok := err.(*jwt.ValidationError)
	if !ok {
		return false
	}
	return e.Errors == jwt.ValidationErrorIssuedAt
}

// recordAuthFailure records a failed authentication attempt for both the identifier (username/token)
// and the client IP, and logs the appropriate audit events.
func (a *RHSSOAuthenticator) recordAuthFailure(identifier, clientIP, reason string) {
	if !a.isLockoutEnabled() {
		return
	}
	count, lockDuration := a.failedAttempts.RecordFailure(identifier, IdentifierTypeUsername)
	a.auditLog.LogFailedLogin(identifier, clientIP, count, reason)
	if lockDuration > 0 {
		a.auditLog.LogAccountLocked(identifier, clientIP, count, time.Now().Add(lockDuration))
	}
	if clientIP != "" {
		ipCount, ipLock := a.failedAttempts.RecordFailure(clientIP, IdentifierTypeIP)
		if ipLock > 0 {
			a.auditLog.LogIPLocked(clientIP, ipCount, time.Now().Add(ipLock))
		}
	}
}

// AuthUserAuth implements the Authenticator interface for backward compatibility.
// It delegates to authUserAuthWithContext with an empty client IP.
func (a *RHSSOAuthenticator) AuthUserAuth(token string) (interface{}, error) {
	return a.authUserAuthWithContext(token, "")
}

// authUserAuthWithContext performs user authentication with client IP for audit logging.
func (a *RHSSOAuthenticator) authUserAuthWithContext(token, clientIP string) (interface{}, error) {
	// Handle Bearer
	authHeaderParts := strings.Fields(token)
	if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
		return nil, common.ApiErrorWithDefaultInfraError(errors.Errorf("Authorization header format must be Bearer {token}"), http.StatusUnauthorized)
	}

	// Use token hash for initial lockout check (before we know the username)
	tokenHash := hashToken(authHeaderParts[1])

	// IMPORTANT: Check lockout status BEFORE attempting authentication.
	// This prevents:
	//  1. Unnecessary load on RHSSO during brute force attacks
	//  2. Timing side-channels that could reveal account existence
	//  3. Wasted resources validating credentials for locked accounts
	// The lockout check returns 429 (Too Many Requests) with a message indicating when to retry.
	if a.isLockoutEnabled() {
		// Check if the client IP is locked out
		if clientIP != "" {
			if locked, until := a.failedAttempts.IsLocked(clientIP, IdentifierTypeIP); locked {
				a.auditLog.LogLockedIPAttempt(clientIP, until)
				return nil, common.ApiErrorWithDefaultInfraError(
					errors.Errorf("IP locked until %s due to too many failed attempts", until.Format(time.RFC3339)),
					http.StatusTooManyRequests)
			}
		}

		if locked, until := a.failedAttempts.IsLocked(tokenHash, IdentifierTypeUsername); locked {
			a.auditLog.LogLockedLoginAttempt(tokenHash, clientIP, until)
			return nil, common.ApiErrorWithDefaultInfraError(
				errors.Errorf("authentication locked until %s due to too many failed attempts", until.Format(time.RFC3339)),
				http.StatusTooManyRequests)
		}
	}

	// Now parse the token
	parsedToken, err := jwt.Parse(authHeaderParts[1], a.getValidationToken)

	// Check if there was an error in parsing and if the parsed token is valid
	if err != nil || !parsedToken.Valid {
		// Don't report error "Token used before issued"
		// TODO: This validation is going to be removed in jwt-go v4, once jwt-go v4
		// is released and we start using it, this validation-skip can be removed.
		if !isValidationErrorIssuedAt(err) {
			a.recordAuthFailure(tokenHash, clientIP, "invalid or expired token")
			return nil, common.ApiErrorWithDefaultInfraError(errors.Errorf("Error parsing token or token is invalid"), http.StatusUnauthorized)
		}
	}

	if jwt.SigningMethodRS256 != nil && jwt.SigningMethodRS256.Alg() != parsedToken.Header["alg"] {
		message := fmt.Sprintf("Expected %s signing method but token specified %s",
			jwt.SigningMethodRS256.Alg(),
			parsedToken.Header["alg"])
		a.log.Errorf("Error validating token algorithm: %s", message)
		a.recordAuthFailure(tokenHash, clientIP, "invalid signing algorithm")
		return nil, common.ApiErrorWithDefaultInfraError(errors.Errorf("Error validating token algorithm: %s", message), http.StatusUnauthorized)
	}

	payload, err := parseOCMPayload(parsedToken)
	if err != nil {
		a.log.Error("Failed parse payload,", err)
		a.recordAuthFailure(tokenHash, clientIP, "failed to parse payload")
		return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
	}

	if payload.Username == "" {
		a.log.Error("Missing username in token")
		a.recordAuthFailure(tokenHash, clientIP, "missing username in token")
		return nil, common.ApiErrorWithDefaultInfraError(errors.Errorf("Missing username in token"), http.StatusUnauthorized)
	}

	// Now that we have the username, check if this specific user is locked out
	if a.isLockoutEnabled() {
		if locked, until := a.failedAttempts.IsLocked(payload.Username, IdentifierTypeUsername); locked {
			a.auditLog.LogLockedLoginAttempt(payload.Username, clientIP, until)
			return nil, common.ApiErrorWithDefaultInfraError(
				errors.Errorf("account locked until %s due to too many failed attempts", until.Format(time.RFC3339)),
				http.StatusTooManyRequests)
		}
	}

	payloadKey := payload.Username + "_is_admin"
	if payloadFromCache, existInCache := a.client.Cache.Get(payloadKey); existInCache {
		payload.Role = payloadFromCache.(*ocm.AuthPayload).Role
	} else {
		err := a.storeRoleInPayload(payload)

		if shouldStorePayloadInCache(err) {
			a.client.Cache.Set(payloadKey, payload, cache.DefaultExpiration)
		}

		if err != nil {
			a.log.Errorf("Unable to fetch user's role: %v", err)
			a.recordAuthFailure(payload.Username, clientIP, "failed to fetch user role")
			return nil, common.ApiErrorWithDefaultInfraError(err, http.StatusUnauthorized)
		}
	}

	// Successful authentication - reset failed attempts for token hash, username, and IP
	if a.isLockoutEnabled() {
		a.failedAttempts.Reset(tokenHash, IdentifierTypeUsername)
		a.failedAttempts.Reset(payload.Username, IdentifierTypeUsername)
		if clientIP != "" {
			a.failedAttempts.Reset(clientIP, IdentifierTypeIP)
		}
		a.auditLog.LogSuccessfulLogin(payload.Username, clientIP)
	}

	return payload, nil
}

func (a RHSSOAuthenticator) storeRoleInPayload(payload *ocm.AuthPayload) error {
	role, err := a.getRole(payload)
	if err != nil {
		return err
	}
	payload.Role = role
	return nil
}

func (a RHSSOAuthenticator) getRole(payload *ocm.AuthPayload) (ocm.RoleType, error) {
	if funk.Contains(a.AdminUsers, payload.Username) {
		return ocm.AdminRole, nil
	}
	isReadOnly, err := a.isReadOnlyAdmin(payload.Username)
	if err != nil {
		return ocm.UserRole, err
	}
	if isReadOnly {
		return ocm.ReadOnlyAdminRole, nil
	}
	return ocm.UserRole, nil
}

func (a *RHSSOAuthenticator) isReadOnlyAdmin(username string) (bool, error) {
	cacheKey := fmt.Sprintf("%s-%s-%s", username, ocm.BareMetalCapabilityName, ocm.AccountCapabilityType)
	if cacheData, existInCache := a.client.Cache.Get(cacheKey); existInCache {
		isAllowed, ok := cacheData.(bool)
		if !ok {
			return false, errors.New("Expected from cache data to be from bool kind")
		}
		return isAllowed, nil
	}

	isAllowed, err := a.client.Authorization.CapabilityReview(context.Background(), fmt.Sprint(username), ocm.BareMetalCapabilityName, ocm.AccountCapabilityType)
	if shouldStorePayloadInCache(err) {
		a.client.Cache.Set(cacheKey, isAllowed, 1*time.Minute)
	}
	return isAllowed, err
}

func (a *RHSSOAuthenticator) AuthURLAuth(_ string) (interface{}, error) {
	return nil, errors.Errorf("URL Authentication not allowed for rhsso auth")
}

func (a *RHSSOAuthenticator) getInfraEnvKey(token *jwt.Token) (interface{}, error) {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.Errorf("malformed token claims")
	}

	infraEnvID, ok := claims["sub"].(string)
	if !ok {
		return nil, errors.Errorf("token missing 'sub' claim")
	}

	infraEnv, err := common.GetInfraEnvFromDB(a.db, strfmt.UUID(infraEnvID))
	if err != nil {
		return nil, err
	}

	return []byte(infraEnv.ImageTokenKey), nil
}

func (a *RHSSOAuthenticator) AuthImageAuth(token string) (interface{}, error) {
	parsedToken, err := jwt.Parse(token, a.getInfraEnvKey)
	if err != nil {
		return nil, common.NewInfraError(http.StatusUnauthorized, err)
	}

	return parsedToken.Claims, nil
}

func (a *RHSSOAuthenticator) AuthWatcherAuth(_ string) (interface{}, error) {
	return nil, common.NewInfraError(http.StatusUnauthorized, errors.Errorf("Watcher Authentication not allowed for RHSSO auth"))
}

// CreateAuthenticator returns a custom authenticator factory that extracts
// client IP from the HTTP request and passes it to the authentication methods
// for security audit logging. This replaces the default security.APIKeyAuth
// to enable IP-based tracking of failed login attempts.
func (a *RHSSOAuthenticator) CreateAuthenticator() func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator {
	return func(name, in string, authenticate security.TokenAuthentication) runtime.Authenticator {
		return security.HttpAuthenticator(func(r *http.Request) (bool, interface{}, error) {
			// Extract token from request based on location (header/query)
			var token string
			if in == "header" {
				token = r.Header.Get(name)
			} else if in == "query" {
				token = r.URL.Query().Get(name)
			}

			if token == "" {
				return false, nil, nil
			}

			// Extract client IP for audit logging, validating against trusted proxies
			clientIP := ExtractClientIPFromRequest(r, a.trustedProxyChecker)

			// Route to the appropriate context-aware authentication method based on header name
			var principal interface{}
			var err error

			switch name {
			case "X-Secret-Key":
				// Agent authentication
				principal, err = a.authAgentAuthWithContext(r.Context(), token, clientIP)
			case "Authorization":
				// User authentication
				principal, err = a.authUserAuthWithContext(token, clientIP)
			default:
				// Fall back to the callback for other auth types (image, watcher, etc.)
				// These don't require lockout tracking
				principal, err = authenticate(token)
			}

			if err != nil {
				return true, nil, err
			}

			return true, principal, nil
		})
	}
}
