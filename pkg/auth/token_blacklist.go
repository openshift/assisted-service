package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// TokenBlacklist provides server-side token revocation for logout functionality.
//
// Uses dual-layer storage:
//   - In-memory cache for fast lookups during authentication
//   - Database persistence to survive service restarts
//
// Tokens are stored as SHA-256 hashes to avoid keeping sensitive values.
// The cleanup job runs hourly to remove entries for tokens that have naturally expired.
type TokenBlacklist struct {
	db       *gorm.DB
	log      logrus.FieldLogger
	cache    *cache.Cache
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewTokenBlacklist creates a new TokenBlacklist instance.
func NewTokenBlacklist(db *gorm.DB, log logrus.FieldLogger) *TokenBlacklist {
	return &TokenBlacklist{
		db:     db,
		log:    log,
		cache:  cache.New(10*time.Minute, 30*time.Minute),
		stopCh: make(chan struct{}),
	}
}

// hashToken returns the SHA-256 hash of a token as a hex string.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// Revoke adds a token to the blacklist.
func (b *TokenBlacklist) Revoke(token string, expiresAt time.Time, entityID, entityType, reason string) error {
	tokenHash := hashToken(token)

	entry := RevokedToken{
		TokenHash:  tokenHash,
		RevokedAt:  time.Now(),
		ExpiresAt:  expiresAt,
		EntityID:   entityID,
		EntityType: entityType,
		Reason:     reason,
	}

	// Add to database
	if err := b.db.Create(&entry).Error; err != nil {
		// Check if it's a duplicate key error (token already revoked)
		if isDuplicateKeyError(err) {
			b.log.Debugf("Token already revoked: %s", tokenHash[:8])
			return nil
		}
		b.log.WithError(err).Error("Failed to add token to blacklist")
		return err
	}

	// Add to cache
	b.cache.Set(tokenHash, true, cache.DefaultExpiration)

	b.log.Debugf("Token revoked successfully: %s (entity: %s/%s, reason: %s)",
		tokenHash[:8], entityType, entityID, reason)

	return nil
}

// IsRevoked checks if a token has been revoked.
func (b *TokenBlacklist) IsRevoked(token string) (bool, error) {
	tokenHash := hashToken(token)

	// Check cache first
	if _, found := b.cache.Get(tokenHash); found {
		return true, nil
	}

	// Check database
	var count int64
	err := b.db.Model(&RevokedToken{}).
		Where("token_hash = ?", tokenHash).
		Count(&count).Error

	if err != nil {
		b.log.WithError(err).Warn("Failed to check token revocation status in database")
		return false, err
	}

	if count > 0 {
		// Add to cache for faster subsequent lookups
		b.cache.Set(tokenHash, true, cache.DefaultExpiration)
		return true, nil
	}

	return false, nil
}

// CleanupExpired removes expired entries from the blacklist.
// Expired entries are those where the token's natural expiration time has passed,
// meaning the token would be invalid anyway.
func (b *TokenBlacklist) CleanupExpired() (int64, error) {
	result := b.db.Where("expires_at < ?", time.Now()).Delete(&RevokedToken{})
	if result.Error != nil {
		b.log.WithError(result.Error).Error("Failed to cleanup expired revoked tokens")
		return 0, result.Error
	}

	if result.RowsAffected > 0 {
		b.log.Infof("Cleaned up %d expired revoked tokens", result.RowsAffected)
	}

	return result.RowsAffected, nil
}

// StartCleanupJob starts a background goroutine that periodically cleans up
// expired entries from the blacklist.
func (b *TokenBlacklist) StartCleanupJob(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := b.CleanupExpired(); err != nil {
					b.log.WithError(err).Warn("Cleanup job failed")
				}
			case <-b.stopCh:
				b.log.Info("Token blacklist cleanup job stopped")
				return
			}
		}
	}()

	b.log.Infof("Token blacklist cleanup job started with interval %v", interval)
}

// StopCleanupJob stops the background cleanup job.
// This method is safe to call multiple times.
func (b *TokenBlacklist) StopCleanupJob() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
}

// InvalidateCache removes a token from the in-memory cache.
// This can be used when a token's entity is deleted and we want to
// ensure the cache is updated.
func (b *TokenBlacklist) InvalidateCache(token string) {
	tokenHash := hashToken(token)
	b.cache.Delete(tokenHash)
}

// isDuplicateKeyError checks if an error is a duplicate key constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// PostgreSQL duplicate key error contains this substring
	return containsAny(err.Error(),
		"duplicate key",
		"UNIQUE constraint failed",
		"violates unique constraint",
	)
}

func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
