package auth

import (
	"time"
)

// RevokedToken represents a token that has been revoked and should no longer be valid.
// Tokens are stored by their SHA-256 hash to avoid storing raw token values.
// Note: This model uses hard deletes (no DeletedAt field) to ensure revoked tokens
// cannot be soft-undeleted. Cleanup is performed based on ExpiresAt timestamp.
type RevokedToken struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"index"`

	// TokenHash is the SHA-256 hash of the revoked token (hex encoded)
	TokenHash string `gorm:"type:varchar(64);not null;uniqueIndex:idx_revoked_tokens_hash"`

	// RevokedAt is the timestamp when the token was revoked
	RevokedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`

	// ExpiresAt is the timestamp when the token would naturally expire.
	// Used for cleanup of expired entries from the blacklist.
	ExpiresAt time.Time `gorm:"not null;index:idx_revoked_tokens_expires"`

	// EntityID is the ID of the entity (cluster or infraenv) associated with the token
	EntityID string `gorm:"type:varchar(255)"`

	// EntityType is the type of entity (cluster_id or infra_env_id)
	EntityType string `gorm:"type:varchar(50)"`

	// Reason describes why the token was revoked (e.g., "user_logout", "security_revocation")
	Reason string `gorm:"type:varchar(255)"`
}

// TableName specifies the table name for GORM
func (RevokedToken) TableName() string {
	return "revoked_tokens"
}
