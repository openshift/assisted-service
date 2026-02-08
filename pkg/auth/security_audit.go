package auth

import (
	"time"

	"github.com/sirupsen/logrus"
)

// SecurityEventType represents the type of security event being logged.
type SecurityEventType string

const (
	// SecurityEventLoginSuccess indicates a successful login.
	SecurityEventLoginSuccess SecurityEventType = "login_success"

	// SecurityEventLoginFailure indicates a failed login attempt.
	SecurityEventLoginFailure SecurityEventType = "login_failure"

	// SecurityEventAccountLocked indicates an account was locked due to too many failures.
	SecurityEventAccountLocked SecurityEventType = "account_locked"

	// SecurityEventLockedLoginAttempt indicates a login attempt on a locked account.
	SecurityEventLockedLoginAttempt SecurityEventType = "locked_login_attempt"

	// SecurityEventIPLocked indicates an IP was locked due to too many failures.
	SecurityEventIPLocked SecurityEventType = "ip_locked"

	// SecurityEventLockedIPAttempt indicates a login attempt from a locked IP.
	SecurityEventLockedIPAttempt SecurityEventType = "locked_ip_attempt"

	// SecurityEventLockoutExpired indicates a lockout period has expired.
	SecurityEventLockoutExpired SecurityEventType = "lockout_expired"
)

// SecurityAuditLogger logs security-related events for authentication.
type SecurityAuditLogger struct {
	log logrus.FieldLogger
}

// NewSecurityAuditLogger creates a new security audit logger.
func NewSecurityAuditLogger(log logrus.FieldLogger) *SecurityAuditLogger {
	return &SecurityAuditLogger{
		log: log.WithField("component", "security_audit"),
	}
}

// LogSuccessfulLogin logs a successful login event.
func (l *SecurityAuditLogger) LogSuccessfulLogin(username, clientIP string) {
	l.log.WithFields(logrus.Fields{
		"event":     SecurityEventLoginSuccess,
		"username":  username,
		"client_ip": clientIP,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}).Info("Successful login")
}

// LogFailedLogin logs a failed login attempt.
func (l *SecurityAuditLogger) LogFailedLogin(username, clientIP string, attemptCount int, reason string) {
	l.log.WithFields(logrus.Fields{
		"event":         SecurityEventLoginFailure,
		"username":      username,
		"client_ip":     clientIP,
		"attempt_count": attemptCount,
		"reason":        reason,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}).Warn("Failed login attempt")
}

// LogAccountLocked logs when an account is locked due to too many failures.
func (l *SecurityAuditLogger) LogAccountLocked(username, clientIP string, attemptCount int, lockedUntil time.Time) {
	l.log.WithFields(logrus.Fields{
		"event":         SecurityEventAccountLocked,
		"username":      username,
		"client_ip":     clientIP,
		"attempt_count": attemptCount,
		"locked_until":  lockedUntil.UTC().Format(time.RFC3339),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}).Warn("Account locked due to excessive failed attempts")
}

// LogLockedLoginAttempt logs a login attempt on a locked account.
func (l *SecurityAuditLogger) LogLockedLoginAttempt(username, clientIP string, lockedUntil time.Time) {
	l.log.WithFields(logrus.Fields{
		"event":        SecurityEventLockedLoginAttempt,
		"username":     username,
		"client_ip":    clientIP,
		"locked_until": lockedUntil.UTC().Format(time.RFC3339),
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}).Warn("Login attempt on locked account")
}

// LogIPLocked logs when an IP is locked due to too many failures.
func (l *SecurityAuditLogger) LogIPLocked(clientIP string, attemptCount int, lockedUntil time.Time) {
	l.log.WithFields(logrus.Fields{
		"event":         SecurityEventIPLocked,
		"client_ip":     clientIP,
		"attempt_count": attemptCount,
		"locked_until":  lockedUntil.UTC().Format(time.RFC3339),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}).Warn("IP locked due to excessive failed attempts")
}

// LogLockedIPAttempt logs a login attempt from a locked IP.
func (l *SecurityAuditLogger) LogLockedIPAttempt(clientIP string, lockedUntil time.Time) {
	l.log.WithFields(logrus.Fields{
		"event":        SecurityEventLockedIPAttempt,
		"client_ip":    clientIP,
		"locked_until": lockedUntil.UTC().Format(time.RFC3339),
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}).Warn("Login attempt from locked IP")
}

// LogLockoutExpired logs when a lockout period has expired.
func (l *SecurityAuditLogger) LogLockoutExpired(identifier, identifierType string) {
	l.log.WithFields(logrus.Fields{
		"event":           SecurityEventLockoutExpired,
		"identifier":      identifier,
		"identifier_type": identifierType,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	}).Info("Lockout period expired")
}
