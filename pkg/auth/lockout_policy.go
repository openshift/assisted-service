package auth

import (
	"math"
	"time"
)

// LockoutPolicy defines the parameters for account lockout behavior.
type LockoutPolicy struct {
	// MaxAttempts is the number of failed attempts before lockout is triggered.
	// Default: 5
	MaxAttempts int

	// LockoutDuration is the base duration for which an account is locked.
	// Default: 15 minutes
	LockoutDuration time.Duration

	// WindowDuration is the time window in which failed attempts are counted.
	// After this period of inactivity, the counter resets.
	// Default: 5 minutes
	WindowDuration time.Duration

	// UseExponential enables exponential backoff for repeated lockouts.
	// When enabled, lockout duration increases exponentially with each lockout.
	UseExponential bool

	// Enabled determines whether account lockout is active.
	// Default: true for RHSSO auth
	Enabled bool
}

// DefaultLockoutPolicy returns the default lockout policy settings.
func DefaultLockoutPolicy() LockoutPolicy {
	return LockoutPolicy{
		MaxAttempts:     5,
		LockoutDuration: 15 * time.Minute,
		WindowDuration:  5 * time.Minute,
		UseExponential:  true,
		Enabled:         true,
	}
}

// CalculateLockout returns the lockout duration based on the number of failed attempts.
// Returns 0 if the attempt count is below the threshold.
//
// Exponential backoff formula: duration = baseDuration * 2^(lockoutCount - 1)
//
// This progressively increases lockout duration:
//   - 1st lockout (5 failed attempts):  15 minutes
//   - 2nd lockout (6 failed attempts):  30 minutes
//   - 3rd lockout (7 failed attempts):  1 hour
//   - 4th lockout (8 failed attempts):  2 hours
//   - ...capped at ~10 days to prevent indefinite lockouts
//
// This discourages persistent attackers while allowing legitimate users
// to recover from occasional mistakes after a reasonable wait.
func (p *LockoutPolicy) CalculateLockout(attemptCount int) time.Duration {
	if attemptCount < p.MaxAttempts {
		return 0
	}

	if !p.UseExponential {
		return p.LockoutDuration
	}

	// Exponential backoff: base * 2^(attempts - maxAttempts)
	// This gives: 15min, 30min, 60min, 120min, etc.
	exponent := attemptCount - p.MaxAttempts
	if exponent > 10 {
		exponent = 10 // Cap at approximately 10 days
	}

	multiplier := math.Pow(2, float64(exponent))
	return time.Duration(float64(p.LockoutDuration) * multiplier)
}

// ShouldLock returns true if the given attempt count should trigger a lockout.
func (p *LockoutPolicy) ShouldLock(attemptCount int) bool {
	return attemptCount >= p.MaxAttempts
}

// RemainingAttempts returns the number of attempts remaining before lockout.
func (p *LockoutPolicy) RemainingAttempts(currentCount int) int {
	remaining := p.MaxAttempts - currentCount
	if remaining < 0 {
		return 0
	}
	return remaining
}
