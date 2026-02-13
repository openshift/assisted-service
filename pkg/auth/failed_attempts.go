package auth

import (
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdentifierType represents the type of identifier being tracked for failed attempts.
type IdentifierType string

const (
	// IdentifierTypeUsername tracks failed attempts by username.
	IdentifierTypeUsername IdentifierType = "username"

	// IdentifierTypeIP tracks failed attempts by IP address.
	IdentifierTypeIP IdentifierType = "ip"

	// IdentifierTypeUserIP tracks failed attempts by username+IP combination.
	IdentifierTypeUserIP IdentifierType = "user_ip"
)

// FailedLoginAttempt represents a record of failed login attempts in the database.
type FailedLoginAttempt struct {
	ID             uint       `gorm:"primaryKey"`
	Identifier     string     `gorm:"size:255;not null;uniqueIndex:idx_failed_attempts_unique"`
	IdentifierType string     `gorm:"size:20;not null;uniqueIndex:idx_failed_attempts_unique"`
	AttemptCount   int        `gorm:"default:1"`
	FirstAttempt   time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP"`
	LastAttempt    time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP"`
	LockedUntil    *time.Time `gorm:"index"`
}

// TableName specifies the table name for FailedLoginAttempt.
func (FailedLoginAttempt) TableName() string {
	return "failed_login_attempts"
}

// FailedAttemptTracker manages tracking and querying of failed login attempts.
type FailedAttemptTracker struct {
	db     *gorm.DB
	policy LockoutPolicy
	log    logrus.FieldLogger
}

// NewFailedAttemptTracker creates a new tracker with the given database and policy.
func NewFailedAttemptTracker(db *gorm.DB, policy LockoutPolicy, log logrus.FieldLogger) *FailedAttemptTracker {
	return &FailedAttemptTracker{
		db:     db,
		policy: policy,
		log:    log,
	}
}

// RecordFailure records a failed login attempt and returns the current attempt count
// and any lockout duration that should be applied.
// This method uses an atomic upsert operation to prevent race conditions where
// two concurrent requests might both try to create or update a record for the same identifier.
func (t *FailedAttemptTracker) RecordFailure(identifier string, identifierType IdentifierType) (int, time.Duration) {
	if t.db == nil || !t.policy.Enabled {
		return 0, 0
	}

	now := time.Now()
	windowCutoff := now.Add(-t.policy.WindowDuration)

	// Prepare the attempt record for upsert
	attempt := FailedLoginAttempt{
		Identifier:     identifier,
		IdentifierType: string(identifierType),
		AttemptCount:   1,
		FirstAttempt:   now,
		LastAttempt:    now,
	}

	// Use atomic upsert with CASE expressions to handle window expiration atomically.
	// This prevents race conditions where concurrent requests could all read the same
	// stale state and incorrectly reset the counter, losing increments.
	// Note: Column references are qualified with the table name to avoid PostgreSQL
	// ambiguity errors in ON CONFLICT DO UPDATE clauses (SQLSTATE 42702).
	result := t.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "identifier"}, {Name: "identifier_type"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"attempt_count": gorm.Expr(
				"CASE WHEN failed_login_attempts.last_attempt < ? THEN 1 ELSE failed_login_attempts.attempt_count + 1 END",
				windowCutoff,
			),
			"first_attempt": gorm.Expr(
				"CASE WHEN failed_login_attempts.last_attempt < ? THEN ? ELSE failed_login_attempts.first_attempt END",
				windowCutoff, now,
			),
			"last_attempt": gorm.Expr("EXCLUDED.last_attempt"),
			"locked_until": gorm.Expr(
				"CASE WHEN failed_login_attempts.last_attempt < ? THEN NULL ELSE failed_login_attempts.locked_until END",
				windowCutoff,
			),
		}),
	}).Create(&attempt)

	if result.Error != nil {
		t.log.WithError(result.Error).Warnf("Failed to upsert failed login attempt record for %s", identifier)
		return 0, 0
	}

	// Fetch the updated record to get the current attempt count
	if err := t.db.Where("identifier = ? AND identifier_type = ?", identifier, string(identifierType)).First(&attempt).Error; err != nil {
		t.log.WithError(err).Warnf("Failed to query failed login attempts after upsert for %s", identifier)
		return 1, 0
	}

	// Calculate lockout based on current attempt count
	lockDuration := t.policy.CalculateLockout(attempt.AttemptCount)
	if lockDuration > 0 {
		lockedUntil := now.Add(lockDuration)
		attempt.LockedUntil = &lockedUntil

		// Update the locked_until field
		if updateErr := t.db.Model(&attempt).Update("locked_until", lockedUntil).Error; updateErr != nil {
			t.log.WithError(updateErr).Warnf("Failed to set lockout time for %s", identifier)
		}
	}

	return attempt.AttemptCount, lockDuration
}

// IsLocked checks if the given identifier is currently locked out.
// Returns the lock status and the time when the lock expires.
func (t *FailedAttemptTracker) IsLocked(identifier string, identifierType IdentifierType) (bool, time.Time) {
	if t.db == nil || !t.policy.Enabled {
		return false, time.Time{}
	}

	var attempt FailedLoginAttempt
	err := t.db.Where("identifier = ? AND identifier_type = ?", identifier, string(identifierType)).First(&attempt).Error

	if err != nil {
		return false, time.Time{}
	}

	if attempt.LockedUntil != nil && attempt.LockedUntil.After(time.Now()) {
		return true, *attempt.LockedUntil
	}

	return false, time.Time{}
}

// Reset clears the failed attempt record for the given identifier.
// This should be called after a successful login.
func (t *FailedAttemptTracker) Reset(identifier string, identifierType IdentifierType) {
	if t.db == nil || !t.policy.Enabled {
		return
	}

	if err := t.db.Where("identifier = ? AND identifier_type = ?", identifier, string(identifierType)).Delete(&FailedLoginAttempt{}).Error; err != nil {
		t.log.WithError(err).Warnf("Failed to reset failed login attempts for %s", identifier)
	}
}

// CleanupExpired removes expired lockout records from the database.
// This can be called periodically to keep the table clean.
func (t *FailedAttemptTracker) CleanupExpired() error {
	if t.db == nil {
		return nil
	}

	// Delete records where:
	// 1. The lockout has expired AND the window has passed (no recent activity)
	// 2. The window has passed without reaching lockout (stale records)
	cutoff := time.Now().Add(-t.policy.WindowDuration)

	return t.db.Where(
		"(locked_until IS NOT NULL AND locked_until < ?) OR (locked_until IS NULL AND last_attempt < ?)",
		time.Now(), cutoff,
	).Delete(&FailedLoginAttempt{}).Error
}

// GetAttemptCount returns the current attempt count for an identifier.
func (t *FailedAttemptTracker) GetAttemptCount(identifier string, identifierType IdentifierType) int {
	if t.db == nil || !t.policy.Enabled {
		return 0
	}

	var attempt FailedLoginAttempt
	err := t.db.Where("identifier = ? AND identifier_type = ?", identifier, string(identifierType)).First(&attempt).Error

	if err != nil {
		return 0
	}

	// Check if window expired
	if time.Since(attempt.LastAttempt) > t.policy.WindowDuration {
		return 0
	}

	return attempt.AttemptCount
}
